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
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	serverstorage "k8s.io/apiserver/pkg/server/storage"

	"github.com/kubewharf/apiserver-runtime/pkg/apihelpers"
)

// StorageFactory is the interface to locate the storage for a given GroupResource
// It extend apiserver storage.StorageFactory interface to expose more function of
// DefaultStorageFactory
type StorageFactory interface {
	serverstorage.StorageFactory

	SetSerializer(groupResource schema.GroupResource, mediaType string, serializer runtime.StorageSerializer)
	SetResourceEtcdPrefix(groupResource schema.GroupResource, prefix string)

	GetResourceEncodingConfig() ResourceEncodingConfig
}

var _ StorageFactory = &DefaultStorageFactory{}

// DefaultStorageFactory wraps *serverstorage.DefaultStorageFactory and ResourceEncodingConfig and
// changes the ResourcePrefix behavior
type DefaultStorageFactory struct {
	*serverstorage.DefaultStorageFactory
	ResourceEncodingConfig ResourceEncodingConfig
}

func NewStorageFactory(f *serverstorage.DefaultStorageFactory) (StorageFactory, error) {
	ret := &DefaultStorageFactory{
		DefaultStorageFactory: f,
	}
	if rec, ok := f.ResourceEncodingConfig.(ResourceEncodingConfig); !ok {
		return nil, fmt.Errorf("DefaultStorageFactory.ResourceEncodingConfig does not have SetResourceEncoding() func")
	} else {
		ret.ResourceEncodingConfig = rec
	}
	return ret, nil
}

// ResourcePrefix add resource group to prefix if the original resourcePrefix is equals to resource name
func (s *DefaultStorageFactory) ResourcePrefix(groupResource schema.GroupResource) string {
	resourcePrefix := s.DefaultStorageFactory.ResourcePrefix(groupResource)
	if apihelpers.IsProtectedGroup(groupResource.Group) {
		// pretected k8s native resource
		return resourcePrefix
	}
	if resourcePrefix == strings.ToLower(groupResource.Resource) {
		// add group prefix for orhters
		resourcePrefix = groupResource.Group + "/" + resourcePrefix
	}
	return resourcePrefix
}

func (s *DefaultStorageFactory) GetResourceEncodingConfig() ResourceEncodingConfig {
	return s.ResourceEncodingConfig
}
