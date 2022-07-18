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
	"k8s.io/apimachinery/pkg/runtime"
	serverstorage "k8s.io/apiserver/pkg/server/storage"
	"k8s.io/apiserver/pkg/storage/storagebackend"
	"k8s.io/kubernetes/pkg/apis/apps"

	runtimeschema "github.com/kubewharf/apiserver-runtime/pkg/schema"
	"github.com/kubewharf/apiserver-runtime/pkg/server/storage"
)

func NewTestDefaultStorageFactory(scheme *runtime.Scheme) *storage.DefaultStorageFactory {
	resourceEncodingConfig := serverstorage.NewDefaultResourceEncodingConfig(scheme)
	storageFactory := serverstorage.NewDefaultStorageFactory(
		storagebackend.Config{Prefix: "/registry"},
		"test/test",
		nil,
		resourceEncodingConfig,
		serverstorage.NewResourceConfig(),
		nil,
	)
	return &storage.DefaultStorageFactory{
		DefaultStorageFactory:  storageFactory,
		ResourceEncodingConfig: resourceEncodingConfig,
	}
}

func TestRESTStorageOptionsFactory_GetRESTStorageOptions(t *testing.T) {
	dpv1GVKR := runtimeschema.GroupVersionKindResource{
		Group:    appsv1.SchemeGroupVersion.Group,
		Version:  appsv1.SchemeGroupVersion.Version,
		Kind:     "Deployment",
		Resource: "deployments",
	}
	type fields struct {
		storageFactory storage.StorageFactory
	}
	type args struct {
		gvkr runtimeschema.GroupVersionKindResource
	}

	tests := []struct {
		name      string
		fields    fields
		args      args
		overrides func(f *RESTStorageOptionsFactory)
		want      RESTStorageOptions
		wantErr   bool
	}{
		{
			"no overrides deployments",
			fields{
				storageFactory: NewTestDefaultStorageFactory(testscheme),
			},
			args{
				gvkr: dpv1GVKR,
			},
			nil,
			RESTStorageOptions{
				GVKR:            dpv1GVKR,
				ListKind:        "DeploymentList",
				RESTStrategy:    NamespacedStorageStrategySingleton,
				HubGroupVersion: apps.SchemeGroupVersion,
			},
			false,
		},
		{
			"override deployments",
			fields{
				storageFactory: NewTestDefaultStorageFactory(testscheme),
			},
			args{
				gvkr: dpv1GVKR,
			},
			func(f *RESTStorageOptionsFactory) {
				f.SetHubGroupVersion(dpv1GVKR, dpv1GVKR.GroupVersion())
				f.SetRESTStrategy(dpv1GVKR, ClusterScopeStorageStrategySingleton)
			},
			RESTStorageOptions{
				GVKR:            dpv1GVKR,
				ListKind:        "DeploymentList",
				RESTStrategy:    ClusterScopeStorageStrategySingleton,
				HubGroupVersion: dpv1GVKR.GroupVersion(),
			},
			false,
		},
	}
	for i := range tests {
		tt := tests[i]
		t.Run(tt.name, func(t *testing.T) {
			f := NewRESTStorageOptionsFactory(tt.fields.storageFactory)
			if tt.overrides != nil {
				tt.overrides(f)
			}
			got, err := f.GetRESTStorageOptions(tt.args.gvkr)
			if (err != nil) != tt.wantErr {
				t.Errorf("RESTStorageOptionsFactory.GetRESTStorageOptions() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("RESTStorageOptionsFactory.GetRESTStorageOptions() = %v, want %v", got, tt.want)
			}
		})
	}
}
