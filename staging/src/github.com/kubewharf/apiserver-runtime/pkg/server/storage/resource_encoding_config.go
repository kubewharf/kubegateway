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

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	serverstorage "k8s.io/apiserver/pkg/server/storage"
)

// ResourceEncodingConfig extend serverstorage.ResourceEncodingConfig to expose more function of
// DefaultResourceEncodingConfig
type ResourceEncodingConfig interface {
	serverstorage.ResourceEncodingConfig

	SetResourceEncoding(resourceBeingStored schema.GroupResource, externalEncodingVersion, internalVersion schema.GroupVersion)
}

type ResrouceEncodingConifgWrapper struct {
	*serverstorage.DefaultResourceEncodingConfig
	Scheme *runtime.Scheme
}

// InMemoryEncodingFor returns the groupVersion for the in memory representation the storage should convert to.
// DefaultResourceEncodingConfig returns runtime.APIVersionInternal for groupResource if it is not overridden, the wrapper
// checks if the runtime.APIVersionInternal version is registered in scheme.
func (w *ResrouceEncodingConifgWrapper) InMemoryEncodingFor(gr schema.GroupResource) (schema.GroupVersion, error) {
	gv, err := w.DefaultResourceEncodingConfig.InMemoryEncodingFor(gr)
	if err != nil {
		return gv, err
	}

	if gv.Version == runtime.APIVersionInternal {
		// check internal version
		if isInternalVersionRegistered(w.Scheme, gv) {
			return schema.GroupVersion{}, fmt.Errorf("groupVersion %q is not registered in scheme", gv.String())
		}
	}
	return gv, nil
}

// we can not use IsVersionRegistered to check if internal version is registered
// because internal version is ignore by observedVersion in schema
func isInternalVersionRegistered(scheme *runtime.Scheme, gv schema.GroupVersion) bool {
	known := scheme.KnownTypes(gv)
	// ignore metav1.WatchEvent added by metav1.AddToGroupVersion()
	delete(known, "WatchEvent")
	return len(known) == 0
}
