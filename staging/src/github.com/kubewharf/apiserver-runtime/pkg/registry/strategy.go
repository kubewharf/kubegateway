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
	"reflect"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/apiserver/pkg/registry/rest"
	"k8s.io/apiserver/pkg/storage/names"

	"github.com/kubewharf/apiserver-runtime/pkg/scheme"
)

var (
	NamespacedStorageStrategySingleton         = NewDefaultRESTStrategy(true, true)
	ClusterScopeStorageStrategySingleton       = NewDefaultRESTStrategy(false, true)
	NamespacedStatusStorageStrategySingleton   = NewDefaultStatusRESTStrategy(true)
	ClusterScopeStatusStorageStrategySingleton = NewDefaultStatusRESTStrategy(false)
)

var _ rest.RESTCreateStrategy = DefaultRESTStrategy{}
var _ rest.RESTUpdateStrategy = DefaultRESTStrategy{}
var _ rest.RESTDeleteStrategy = DefaultRESTStrategy{}

type DefaultRESTStrategy struct {
	runtime.ObjectTyper
	names.NameGenerator
	namespaced bool
	subStatus  bool
}

func NewDefaultRESTStrategy(namespaced, subStatus bool) DefaultRESTStrategy {
	return DefaultRESTStrategy{
		scheme.Scheme,
		names.SimpleNameGenerator,
		namespaced,
		subStatus,
	}
}

func (s DefaultRESTStrategy) NamespaceScoped() bool {
	return s.namespaced
}

func (DefaultRESTStrategy) AllowCreateOnUpdate() bool {
	return true
}

func (DefaultRESTStrategy) AllowUnconditionalUpdate() bool {
	return true
}

func (DefaultRESTStrategy) Canonicalize(obj runtime.Object) {
}

func HasObjectMetaSpecStatus(obj runtime.Object) (hasMeta bool, hasSpec bool, hasStatus bool) {
	_, err := meta.Accessor(obj)
	if err == nil {
		hasMeta = true
	}

	objT := reflect.TypeOf(obj)
	if objT.Kind() != reflect.Ptr {
		return
	}
	objT = objT.Elem()
	if objT.Kind() != reflect.Struct {
		return
	}

	_, hasSpec = objT.FieldByName("Spec")
	_, hasStatus = objT.FieldByName("Status")
	return
}

func (s DefaultRESTStrategy) PrepareForCreate(ctx context.Context, obj runtime.Object) {
	hasMeta, _, hasStatus := HasObjectMetaSpecStatus(obj)

	if s.subStatus && hasStatus {
		statusType, _ := reflect.TypeOf(obj).Elem().FieldByName("Status")
		// clear status
		objV := reflect.ValueOf(obj).Elem()
		objV.FieldByName("Status").Set(reflect.New(statusType.Type).Elem())
	}

	// set generation
	if hasMeta {
		accessor, err := meta.Accessor(obj)
		if err != nil {
			return
		}
		accessor.SetGeneration(1)
	}
}

func (s DefaultRESTStrategy) PrepareForUpdate(ctx context.Context, obj, old runtime.Object) {
	hasMeta, hasSpec, hasStatus := HasObjectMetaSpecStatus(obj)
	if !hasStatus {
		return
	}

	if s.subStatus && hasStatus {
		// Don't update the status if the resource has a Status
		objV := reflect.ValueOf(obj).Elem()
		oldV := reflect.ValueOf(old).Elem()
		objV.FieldByName("Status").Set(oldV.FieldByName("Status"))
	}

	if hasMeta && hasSpec {
		accessorNew, _ := meta.Accessor(obj)
		accessorOld, _ := meta.Accessor(old)

		specNew := reflect.ValueOf(obj).Elem().FieldByName("Spec")
		specOld := reflect.ValueOf(old).Elem().FieldByName("Spec")

		// Spec and annotation updates bump the generation.
		if !reflect.DeepEqual(specNew, specOld) ||
			!reflect.DeepEqual(accessorNew.GetAnnotations(), accessorOld.GetAnnotations()) {
			accessorNew.SetGeneration(accessorOld.GetGeneration() + int64(1))
		}
	}
}

func (DefaultRESTStrategy) Validate(ctx context.Context, obj runtime.Object) field.ErrorList {
	return field.ErrorList{}
}

func (DefaultRESTStrategy) ValidateUpdate(ctx context.Context, obj, old runtime.Object) field.ErrorList {
	return field.ErrorList{}
}

type DefaultStatusRESTStrategy struct {
	rest.RESTCreateUpdateStrategy
}

func NewDefaultStatusRESTStrategy(namespaced bool) DefaultStatusRESTStrategy {
	return DefaultStatusRESTStrategy{
		NewDefaultRESTStrategy(namespaced, true),
	}
}

func (DefaultStatusRESTStrategy) PrepareForUpdate(ctx context.Context, obj, old runtime.Object) {
	hasMeta, hasSpec, hasStatus := HasObjectMetaSpecStatus(obj)
	if !hasStatus {
		// status must be in this Object
		return
	}

	if hasSpec {
		// Don't update the spec
		objV := reflect.ValueOf(obj).Elem()
		oldV := reflect.ValueOf(old).Elem()
		objV.FieldByName("Spec").Set(oldV.FieldByName("Spec"))
	}

	if hasMeta {
		// Don't update labels
		accessorNew, _ := meta.Accessor(obj)
		accessorOld, _ := meta.Accessor(old)
		accessorNew.SetLabels(accessorOld.GetLabels())
	}
}
