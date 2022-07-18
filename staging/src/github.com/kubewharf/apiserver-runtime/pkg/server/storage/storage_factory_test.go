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
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apiserver/pkg/apis/example"
	serverstorage "k8s.io/apiserver/pkg/server/storage"
	"k8s.io/apiserver/pkg/storage/storagebackend"
)

func NewTestDefaultStorageFactory(scheme *runtime.Scheme) *DefaultStorageFactory {
	resourceEncodingConfig := serverstorage.NewDefaultResourceEncodingConfig(scheme)
	storageFactory := serverstorage.NewDefaultStorageFactory(
		storagebackend.Config{Prefix: "/registry"},
		"test/test",
		nil,
		resourceEncodingConfig,
		serverstorage.NewResourceConfig(),
		nil,
	)
	return &DefaultStorageFactory{
		DefaultStorageFactory:  storageFactory,
		ResourceEncodingConfig: resourceEncodingConfig,
	}
}

func TestDefaultStorageFactory_ResourcePrefix(t *testing.T) {
	storageFactory := NewTestDefaultStorageFactory(testscheme)

	type args struct {
		groupResource schema.GroupResource
	}
	tests := []struct {
		name       string
		args       args
		notWrapped string
		wrapped    string
	}{
		{
			"protected k8s native resource prefix",
			args{
				groupResource: example.Resource("test"),
			},
			"test",
			"test",
		},
		{
			"add group prefix for other resource",
			args{
				groupResource: schema.GroupResource{Group: "test", Resource: "test"},
			},
			"test",
			"test/test",
		},
	}
	for i := range tests {
		tt := tests[i]
		t.Run(tt.name, func(t *testing.T) {
			if got := storageFactory.DefaultStorageFactory.ResourcePrefix(tt.args.groupResource); got != tt.notWrapped {
				t.Errorf("DefaultStorageFactory.DefaultStorageFactory.ResourcePrefix() = %v, want %v", got, tt.notWrapped)
			}
			if got := storageFactory.ResourcePrefix(tt.args.groupResource); got != tt.wrapped {
				t.Errorf("DefaultStorageFactory.ResourcePrefix() = %v, want %v", got, tt.wrapped)
			}
		})
	}
}
