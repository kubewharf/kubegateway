// Copyright 2022 ByteDance and its affiliates.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package registry

import (
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apiserver/pkg/registry/generic"
	genericapiserver "k8s.io/apiserver/pkg/server"
	serverstorage "k8s.io/apiserver/pkg/server/storage"
	"k8s.io/kubernetes/pkg/master"
)

var _ master.RESTStorageProvider = &restStorageProvider{}

type restStorageProvider struct {
	scheme         *runtime.Scheme
	codecs         serializer.CodecFactory
	parameterCodec runtime.ParameterCodec
	group          string
	// version to options
	versionMappings map[string][]RESTStorageOptions
	cache           map[schema.GroupVersionKind]*ResourceREST
}

func NewRESTStorageProvider(scheme *runtime.Scheme, group string, options ...RESTStorageOptions) master.RESTStorageProvider {
	p := &restStorageProvider{
		scheme:          scheme,
		codecs:          serializer.NewCodecFactory(scheme),
		parameterCodec:  runtime.NewParameterCodec(scheme),
		group:           group,
		versionMappings: make(map[string][]RESTStorageOptions),
		cache:           make(map[schema.GroupVersionKind]*ResourceREST),
	}

	for _, o := range options {
		if o.GVKR.Group != group {
			continue
		}
		p.versionMappings[o.GVKR.Version] = append(p.versionMappings[o.GVKR.Version], o)
	}
	return p
}

func (p *restStorageProvider) GroupName() string {
	return p.group
}

func (p *restStorageProvider) NewRESTStorage(apiResourceConfigSource serverstorage.APIResourceConfigSource, restOptionsGetter generic.RESTOptionsGetter) (genericapiserver.APIGroupInfo, bool, error) {
	apiGroupInfo := genericapiserver.NewDefaultAPIGroupInfo(p.group, p.scheme, p.parameterCodec, p.codecs)

	for version := range p.versionMappings {
		if !apiResourceConfigSource.VersionEnabled(schema.GroupVersion{Group: p.group, Version: version}) {
			// skip disabled version
			continue
		}
		for _, option := range p.versionMappings[version] {
			hubGVK := option.HubGroupVersion.WithKind(option.GVKR.Kind)
			// it is useful when more than one external version refers to one internal version
			// such as apps/v1,v1beta1.Deployment use the same internal version Deployment
			resourceREST, ok := p.cache[hubGVK]
			if !ok {
				var err error
				// create rest storage for resource
				resourceREST, err = NewResourceREST(p.scheme, restOptionsGetter, option)
				if err != nil {
					return genericapiserver.APIGroupInfo{}, false, err
				}
				p.cache[hubGVK] = resourceREST
			}
			storageMap := resourceREST.ToStorageMap()
			if apiGroupInfo.VersionedResourcesStorageMap[version] == nil {
				apiGroupInfo.VersionedResourcesStorageMap[version] = storageMap
			} else {
				for k, v := range storageMap {
					apiGroupInfo.VersionedResourcesStorageMap[version][k] = v
				}
			}
		}
	}
	return apiGroupInfo, true, nil
}
