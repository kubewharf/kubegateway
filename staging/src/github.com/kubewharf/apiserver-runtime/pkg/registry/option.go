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
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	genericregistry "k8s.io/apiserver/pkg/registry/generic/registry"
	"k8s.io/apiserver/pkg/registry/rest"

	runtimeschema "github.com/kubewharf/apiserver-runtime/pkg/schema"
	"github.com/kubewharf/apiserver-runtime/pkg/server/storage"
)

type RESTStorageOptionsGetter interface {
	GetRESTStorageOptions(gvkr runtimeschema.GroupVersionKindResource) (RESTStorageOptions, error)
}

type RESTStorageOptions struct {
	GVKR         runtimeschema.GroupVersionKindResource
	ListKind     string
	ShortNames   []string
	RESTStrategy rest.RESTCreateUpdateStrategy
	// HubGroupVersion indicates what version objects read from etcd or incoming requests should be converted to for in-memory handling.
	HubGroupVersion             schema.GroupVersion
	SubresourceRESTStoreCreater func(store *genericregistry.Store) map[string]rest.Storage
}

func (o RESTStorageOptions) Validate() (errList field.ErrorList) {
	errList = append(errList, o.GVKR.Validate()...)

	if len(o.ListKind) == 0 {
		errList = append(errList, field.Required(field.NewPath("ListKind"), ""))
	}
	if o.RESTStrategy == nil {
		errList = append(errList, field.Required(field.NewPath("RESTStrategy"), ""))
	}
	if o.HubGroupVersion.Empty() {
		errList = append(errList, field.Required(field.NewPath("HubGroupVersion"), ""))
	}
	return errList
}

type groupResourceOverrides struct {
	shortNames                  []string
	restStrategy                rest.RESTCreateUpdateStrategy
	subresourceRESTStoreCreater func(store *genericregistry.Store) map[string]rest.Storage
}

func (o groupResourceOverrides) Apply(in *RESTStorageOptions) {
	if len(o.shortNames) > 0 {
		in.ShortNames = o.shortNames
	}
	if o.restStrategy != nil {
		in.RESTStrategy = o.restStrategy
	}
	if o.subresourceRESTStoreCreater != nil {
		in.SubresourceRESTStoreCreater = o.subresourceRESTStoreCreater
	}
}

var _ RESTStorageOptionsGetter = &RESTStorageOptionsFactory{}

type RESTStorageOptionsFactory struct {
	storageFactory storage.StorageFactory
	overrides      map[runtimeschema.GroupVersionKindResource]groupResourceOverrides
}

func NewRESTStorageOptionsFactory(s storage.StorageFactory) *RESTStorageOptionsFactory {
	return &RESTStorageOptionsFactory{
		storageFactory: s,
		overrides:      make(map[runtimeschema.GroupVersionKindResource]groupResourceOverrides),
	}
}

func (f *RESTStorageOptionsFactory) newRESTStorageOptions(gvkr runtimeschema.GroupVersionKindResource) RESTStorageOptions {
	return RESTStorageOptions{
		GVKR:         gvkr,
		ListKind:     gvkr.Kind + "List",
		RESTStrategy: NamespacedStorageStrategySingleton,
	}
}

func (f *RESTStorageOptionsFactory) GetRESTStorageOptions(gvkr runtimeschema.GroupVersionKindResource) (RESTStorageOptions, error) {
	if errs := gvkr.Validate(); len(errs) > 0 {
		return RESTStorageOptions{}, errs.ToAggregate()
	}

	options := f.newRESTStorageOptions(gvkr)

	hubGV, err := f.storageFactory.GetResourceEncodingConfig().InMemoryEncodingFor(gvkr.GroupResource())
	if err != nil {
		return RESTStorageOptions{}, err
	}
	options.HubGroupVersion = hubGV

	overrides := f.overrides[gvkr]
	overrides.Apply(&options)

	return options, nil
}

func (f *RESTStorageOptionsFactory) SetShortName(gvkr runtimeschema.GroupVersionKindResource, shortNames []string) {
	overrides := f.overrides[gvkr]
	overrides.shortNames = shortNames
	f.overrides[gvkr] = overrides
}

func (f *RESTStorageOptionsFactory) SetRESTStrategy(gvkr runtimeschema.GroupVersionKindResource, strategy rest.RESTCreateUpdateStrategy) {
	overrides := f.overrides[gvkr]
	overrides.restStrategy = strategy
	f.overrides[gvkr] = overrides
}

func (f *RESTStorageOptionsFactory) SetStorageMediaType(gvkr runtimeschema.GroupVersionKindResource, mediaType string) {
	f.storageFactory.SetSerializer(gvkr.GroupResource(), mediaType, nil)
}

func (f *RESTStorageOptionsFactory) SetSubresourceRESTStorageCreater(gvkr runtimeschema.GroupVersionKindResource, fn func(store *genericregistry.Store) map[string]rest.Storage) {
	overrides := f.overrides[gvkr]
	overrides.subresourceRESTStoreCreater = fn
	f.overrides[gvkr] = overrides
}

func (f *RESTStorageOptionsFactory) SetHubGroupVersion(gvkr runtimeschema.GroupVersionKindResource, hub schema.GroupVersion) {
	f.storageFactory.GetResourceEncodingConfig().SetResourceEncoding(gvkr.GroupResource(), gvkr.GroupVersion(), hub)
}
