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
	"reflect"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	example "k8s.io/apiserver/pkg/apis/example"
	examplev1 "k8s.io/apiserver/pkg/apis/example/v1"
	"k8s.io/apiserver/pkg/registry/generic"
	"k8s.io/apiserver/pkg/registry/generic/registry"
	"k8s.io/apiserver/pkg/registry/rest"

	runtimeschema "github.com/kubewharf/apiserver-runtime/pkg/schema"

	"k8s.io/kubernetes/pkg/apis/apps"
	"k8s.io/kubernetes/pkg/registry/registrytest"
)

var (
	v1GroupVersion = schema.GroupVersion{Group: "", Version: "v1"}
	testscheme     = runtime.NewScheme()
)

func init() {
	metav1.AddToGroupVersion(testscheme, metav1.SchemeGroupVersion)
	testscheme.AddUnversionedTypes(v1GroupVersion,
		&metav1.Status{},
		&metav1.APIVersions{},
		&metav1.APIGroupList{},
		&metav1.APIGroup{},
		&metav1.APIResourceList{},
	)

	examplev1.AddToScheme(testscheme)
	appsv1.AddToScheme(testscheme)
	apps.AddToScheme(testscheme)
}

func TestNewResourceREST(t *testing.T) {
	etcdStorage, server := registrytest.NewEtcdStorage(t, apps.GroupName)
	defer server.Terminate(t)
	restOptionsGetter := generic.RESTOptions{StorageConfig: etcdStorage, Decorator: generic.UndecoratedStorage, DeleteCollectionWorkers: 1, ResourcePrefix: "/registry"}

	type args struct {
		o RESTStorageOptions
	}
	tests := []struct {
		name    string
		args    args
		want    *ResourceREST
		wantErr bool
	}{
		{
			"not registered hub object",
			args{
				o: RESTStorageOptions{
					GVKR: runtimeschema.GroupVersionKindResource{
						Group:    examplev1.SchemeGroupVersion.Group,
						Version:  examplev1.SchemeGroupVersion.Version,
						Kind:     "Pod",
						Resource: "pods",
					},
					HubGroupVersion: example.SchemeGroupVersion,
					ListKind:        "PodList",
					RESTStrategy:    NamespacedStorageStrategySingleton,
				},
			},
			nil,
			true,
		},
		{
			"not registered hub object list",
			args{
				o: RESTStorageOptions{
					GVKR: runtimeschema.GroupVersionKindResource{
						Group:    appsv1.SchemeGroupVersion.Group,
						Version:  appsv1.SchemeGroupVersion.Version,
						Kind:     "Deployment",
						Resource: "deployments",
					},
					HubGroupVersion: apps.SchemeGroupVersion,
					ListKind:        "DeploymentXXX",
					RESTStrategy:    NamespacedStorageStrategySingleton,
				},
			},
			nil,
			true,
		},
	}
	for i := range tests {
		tt := tests[i]
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewResourceREST(testscheme, restOptionsGetter, tt.args.o)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewGenericRESTStore() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("NewGenericRESTStore() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNewResourceREST_ToStorageMap(t *testing.T) {
	etcdStorage, server := registrytest.NewEtcdStorage(t, apps.GroupName)
	defer server.Terminate(t)
	restOptionsGetter := generic.RESTOptions{StorageConfig: etcdStorage, Decorator: generic.UndecoratedStorage, DeleteCollectionWorkers: 1, ResourcePrefix: "deployments"}
	type args struct {
		o RESTStorageOptions
	}
	tests := []struct {
		name string
		args args
		want sets.String
	}{
		{
			"deployments and deployments/status",
			args{
				o: RESTStorageOptions{
					GVKR: runtimeschema.GroupVersionKindResource{
						Group:    appsv1.SchemeGroupVersion.Group,
						Version:  appsv1.SchemeGroupVersion.Version,
						Kind:     "Deployment",
						Resource: "deployments",
					},
					HubGroupVersion: apps.SchemeGroupVersion,
					ListKind:        "DeploymentList",
					RESTStrategy:    NamespacedStorageStrategySingleton,
				},
			},
			sets.NewString("deployments", "deployments/status"),
		},
		{
			"deployments and deployments/status, deployments/scale",
			args{
				o: RESTStorageOptions{
					GVKR: runtimeschema.GroupVersionKindResource{
						Group:    appsv1.SchemeGroupVersion.Group,
						Version:  appsv1.SchemeGroupVersion.Version,
						Kind:     "Deployment",
						Resource: "deployments",
					},
					HubGroupVersion: apps.SchemeGroupVersion,
					ListKind:        "DeploymentList",
					RESTStrategy:    NamespacedStorageStrategySingleton,
					SubresourceRESTStoreCreater: func(store *registry.Store) map[string]rest.Storage {
						return map[string]rest.Storage{
							"scale": nil,
							"xxx":   nil,
						}
					},
				},
			},
			sets.NewString("deployments", "deployments/status", "deployments/scale", "deployments/xxx"),
		},
	}
	for i := range tests {
		tt := tests[i]
		t.Run(tt.name, func(t *testing.T) {
			resourceREST, err := NewResourceREST(testscheme, restOptionsGetter, tt.args.o)
			if err != nil {
				t.Errorf("NewGenericRESTStore() error = %v", err)
			}
			storeMap := resourceREST.ToStorageMap()
			got := sets.StringKeySet(storeMap)
			if !got.Equal(tt.want) {
				t.Errorf("NewGenericRESTStore().ToStorageMap = %v, want %v", got, tt.want)
			}
		})
	}
}
