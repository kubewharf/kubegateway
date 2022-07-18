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

package server

import (
	"fmt"

	"github.com/kubewharf/apiserver-runtime/pkg/scheme"
	"k8s.io/apiserver/pkg/registry/generic"
	"k8s.io/apiserver/pkg/registry/rest"
	genericapiserver "k8s.io/apiserver/pkg/server"
	serverstorage "k8s.io/apiserver/pkg/server/storage"
	"k8s.io/klog"
	"k8s.io/kubernetes/pkg/master"
)

type LegecyRESTStorageProvider interface {
	ResourceName() string
	NewRESTStorage(apiResourceConfigSource serverstorage.APIResourceConfigSource, restOptionsGetter generic.RESTOptionsGetter) (map[string]rest.Storage, bool, error)
}

type PreShutdownHookProvider interface {
	PreShutdownHook() (string, genericapiserver.PreShutdownHookFunc, error)
}

// InstallLegacyAPI will install the legacy APIs for the restStorageProviders if they are enabled.
func InstallLegacyAPI(genericAPIServer *genericapiserver.GenericAPIServer, apiResourceConfigSource serverstorage.APIResourceConfigSource, restOptionsGetter generic.RESTOptionsGetter, legacyRESTStorageProviders ...LegecyRESTStorageProvider) error {
	apiGroupInfo := genericapiserver.APIGroupInfo{
		PrioritizedVersions: scheme.Scheme.PrioritizedVersionsForGroup(""),
		VersionedResourcesStorageMap: map[string]map[string]rest.Storage{
			"v1": make(map[string]rest.Storage),
		},
		Scheme:               scheme.Scheme,
		ParameterCodec:       scheme.ParameterCodec,
		NegotiatedSerializer: scheme.Codecs,
	}

	for _, restStorageBuilder := range legacyRESTStorageProviders {
		resourceName := restStorageBuilder.ResourceName()
		restStore, ok, err := restStorageBuilder.NewRESTStorage(apiResourceConfigSource, restOptionsGetter)
		if err != nil {
			return fmt.Errorf("problem initializing legacy rest for resource %q", resourceName)
		}
		if !ok {
			klog.Warningf("Core API resource %q is not enabled, skipping.", resourceName)
			continue
		}
		klog.V(1).Infof("Enabling Core API resource %q.", resourceName)
		if postHookProvider, ok := restStorageBuilder.(genericapiserver.PostStartHookProvider); ok {
			name, hook, err := postHookProvider.PostStartHook()
			if err != nil {
				klog.Fatalf("Error building PostStartHook: %v", err)
			}
			genericAPIServer.AddPostStartHookOrDie(name, hook)
		}
		for k, v := range restStore {
			apiGroupInfo.VersionedResourcesStorageMap["v1"][k] = v
		}
	}

	if err := genericAPIServer.InstallLegacyAPIGroup(genericapiserver.DefaultLegacyAPIPrefix, &apiGroupInfo); err != nil {
		return fmt.Errorf("error in registering group versions: %v", err)
	}
	return nil
}

// InstallAPIs will install the APIs for the restStorageProviders if they are enabled.
func InstallAPIs(genericAPIServer *genericapiserver.GenericAPIServer, apiResourceConfigSource serverstorage.APIResourceConfigSource, restOptionsGetter generic.RESTOptionsGetter, restStorageProviders ...master.RESTStorageProvider) error {
	apiGroupsInfo := []*genericapiserver.APIGroupInfo{}

	for _, restStorageBuilder := range restStorageProviders {
		groupName := restStorageBuilder.GroupName()
		if !apiResourceConfigSource.AnyVersionForGroupEnabled(groupName) {
			klog.V(1).Infof("Skipping disabled API group %q.", groupName)
			continue
		}
		apiGroupInfo, enabled, err := restStorageBuilder.NewRESTStorage(apiResourceConfigSource, restOptionsGetter)
		if err != nil {
			return fmt.Errorf("problem initializing API group %q : %v", groupName, err)
		}
		if !enabled {
			klog.Warningf("API group %q is not enabled, skipping.", groupName)
			continue
		}
		klog.V(1).Infof("Enabling API group %q.", groupName)

		if postHookProvider, ok := restStorageBuilder.(genericapiserver.PostStartHookProvider); ok {
			name, hook, err := postHookProvider.PostStartHook()
			if err != nil {
				klog.Fatalf("Error building PostStartHook: %v", err)
			}
			genericAPIServer.AddPostStartHookOrDie(name, hook)
		}

		apiGroupsInfo = append(apiGroupsInfo, &apiGroupInfo)
	}

	if err := genericAPIServer.InstallAPIGroups(apiGroupsInfo...); err != nil {
		return fmt.Errorf("error in registering group versions: %v", err)
	}
	return nil
}
