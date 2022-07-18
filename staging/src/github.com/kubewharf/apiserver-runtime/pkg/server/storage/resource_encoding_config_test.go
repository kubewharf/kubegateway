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

package storage

import (
	"reflect"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	examplev1 "k8s.io/apiserver/pkg/apis/example/v1"
	serverstorage "k8s.io/apiserver/pkg/server/storage"

	"k8s.io/kubernetes/pkg/apis/apps"
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

func TestResrouceEncodingConifgWrapper_InMemoryEncodingFor(t *testing.T) {
	resourceConfig := serverstorage.NewDefaultResourceEncodingConfig(testscheme)
	type args struct {
		gr schema.GroupResource
	}
	tests := []struct {
		name    string
		args    args
		want    schema.GroupVersion
		wantErr bool
	}{
		{
			"no internal version",
			args{
				examplev1.Resource("test"),
			},
			schema.GroupVersion{},
			true,
		},
		{
			"pods",
			args{
				appsv1.Resource("deployments"),
			},
			apps.SchemeGroupVersion,
			false,
		},
	}
	for i := range tests {
		tt := tests[i]
		t.Run(tt.name, func(t *testing.T) {
			w := &ResrouceEncodingConifgWrapper{
				DefaultResourceEncodingConfig: resourceConfig,
				Scheme:                        testscheme,
			}

			got, err := w.InMemoryEncodingFor(tt.args.gr)
			if (err != nil) != tt.wantErr {
				t.Errorf("ResrouceEncodingConifgWrapper.InMemoryEncodingFor() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ResrouceEncodingConifgWrapper.InMemoryEncodingFor() = %v, want %v", got, tt.want)
			}
		})
	}
}
