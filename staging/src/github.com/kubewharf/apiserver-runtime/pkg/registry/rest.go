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
	"context"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apiserver/pkg/registry/generic"
	genericregistry "k8s.io/apiserver/pkg/registry/generic/registry"
	"k8s.io/apiserver/pkg/registry/rest"
	"k8s.io/klog"
)

type ResourceREST struct {
	// indicate in memory group version resource
	HubGVR           schema.GroupVersionResource
	ObjectREST       rest.Storage
	SubresourcesREST map[string]rest.Storage
}

func NewResourceREST(scheme *runtime.Scheme, restOptionsGetter generic.RESTOptionsGetter, o RESTStorageOptions) (*ResourceREST, error) {
	internalGVK := o.HubGroupVersion.WithKind(o.GVKR.Kind)
	internalListGVK := o.HubGroupVersion.WithKind(o.ListKind)
	obj, err := scheme.New(internalGVK)
	if err != nil {
		return nil, err
	}
	_, err = scheme.New(internalListGVK)
	if err != nil {
		return nil, err
	}
	_, _, hasStatus := HasObjectMetaSpecStatus(obj)

	store := &genericregistry.Store{
		NewFunc: func() runtime.Object {
			obj, _ := scheme.New(internalGVK)
			return obj
		},
		NewListFunc: func() runtime.Object {
			obj, _ := scheme.New(internalListGVK)
			return obj
		},
		DefaultQualifiedResource: schema.GroupResource{Group: internalGVK.Group, Resource: o.GVKR.Resource},

		CreateStrategy: o.RESTStrategy,
		UpdateStrategy: o.RESTStrategy,
		DeleteStrategy: o.RESTStrategy,

		InMemoryVersioner: o.HubGroupVersion,

		// TableConvertor: printerstorage.TableConvertor{TableGenerator: printers.NewTableGenerator().With(printersinternal.AddHandlers)},
	}
	options := &generic.StoreOptions{RESTOptions: restOptionsGetter}
	if err := store.CompleteWithOptions(options); err != nil {
		return nil, err
	}

	storage := &ResourceREST{
		HubGVR: schema.GroupVersionResource{
			Group:    o.HubGroupVersion.Group,
			Version:  o.HubGroupVersion.Version,
			Resource: o.GVKR.Resource,
		},
		ObjectREST: &ObjectREST{
			shortNames: o.ShortNames,
			Store:      store,
		},
		SubresourcesREST: make(map[string]rest.Storage),
	}

	subresources := []string{}
	if hasStatus {
		statusStore := *store
		statusStore.UpdateStrategy = DefaultStatusRESTStrategy{o.RESTStrategy}
		storage.SubresourcesREST["status"] = &StatusREST{Store: &statusStore}
		subresources = append(subresources, "status")
	}

	if o.SubresourceRESTStoreCreater != nil {
		// shallow copy
		subresourceStore := *store
		storageMap := o.SubresourceRESTStoreCreater(&subresourceStore)
		for key, v := range storageMap {
			subresources = append(subresources, key)
			storage.SubresourcesREST[key] = v
		}
	}

	klog.Infof("create generic rest storage for gvk=%q, in memory gv=%q, with subresources=%q", o.GVKR.GroupVersionKind(), o.HubGroupVersion, strings.Join(subresources, ", "))
	return storage, nil
}

func (p *ResourceREST) ToStorageMap() map[string]rest.Storage {
	ret := make(map[string]rest.Storage)
	ret[p.HubGVR.Resource] = p.ObjectREST
	for name, store := range p.SubresourcesREST {
		ret[p.HubGVR.Resource+"/"+name] = store
	}
	return ret
}

// ObjectREST implements the REST endpoint for changing a object
type ObjectREST struct {
	*genericregistry.Store
	shortNames []string
	categories []string
}

// Implement ShortNamesProvider
var _ rest.ShortNamesProvider = &ObjectREST{}

// ShortNames implements the ShortNamesProvider interface. Returns a list of short names for a resource.
func (r *ObjectREST) ShortNames() []string {
	if len(r.shortNames) > 0 {
		return r.shortNames
	}
	return nil
}

func (r *ObjectREST) WithShortNames(shortNames []string) *ObjectREST {
	r.shortNames = shortNames
	return r
}

// Implement CategoriesProvider
var _ rest.CategoriesProvider = &ObjectREST{}

// Categories implements the CategoriesProvider interface. Returns a list of categories a resource is part of.
func (r *ObjectREST) Categories() []string {
	return r.categories
}

func (r *ObjectREST) WithCategories(categories []string) *ObjectREST {
	r.categories = categories
	return r
}

var _ rest.Storage = &StatusREST{}
var _ rest.Getter = &StatusREST{}
var _ rest.Updater = &StatusREST{}

// StatusREST implements the REST endpoint for changing the status of a object
type StatusREST struct {
	Store *genericregistry.Store
}

func (r *StatusREST) New() runtime.Object {
	return r.Store.NewFunc()
}

// Get retrieves the object from the storage. It is required to support Patch.
func (r *StatusREST) Get(ctx context.Context, name string, options *metav1.GetOptions) (runtime.Object, error) {
	return r.Store.Get(ctx, name, options)
}

// Update alters the status subset of an object.
func (r *StatusREST) Update(ctx context.Context, name string, objInfo rest.UpdatedObjectInfo, createValidation rest.ValidateObjectFunc, updateValidation rest.ValidateObjectUpdateFunc, forceAllowCreate bool, options *metav1.UpdateOptions) (runtime.Object, bool, error) {
	// We are explicitly setting forceAllowCreate to false in the call to the underlying storage because
	// subresources should never allow create on update.
	return r.Store.Update(ctx, name, objInfo, createValidation, updateValidation, false, options)
}
