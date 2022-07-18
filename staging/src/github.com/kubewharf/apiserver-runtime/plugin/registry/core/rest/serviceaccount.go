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

package rest

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apiserver/pkg/registry/generic"
	"k8s.io/apiserver/pkg/registry/rest"
	serverstorage "k8s.io/apiserver/pkg/server/storage"
	serviceaccountstore "k8s.io/kubernetes/pkg/registry/core/serviceaccount/storage"
)

type ServiceAccountLegacyRESTStorageProvider struct {
}

func (ServiceAccountLegacyRESTStorageProvider) ResourceName() string {
	return "serviceaccounts"
}

func (ServiceAccountLegacyRESTStorageProvider) NewRESTStorage(apiResourceConfigSource serverstorage.APIResourceConfigSource, restOptionsGetter generic.RESTOptionsGetter) (map[string]rest.Storage, bool, error) {
	if !apiResourceConfigSource.ResourceEnabled(corev1.SchemeGroupVersion.WithResource("serviceaccounts")) {
		return nil, false, nil
	}

	serviceAccountStorage, err := serviceaccountstore.NewREST(restOptionsGetter, nil, nil, 0, nil, nil)
	if err != nil {
		return nil, false, err
	}

	restStorage := map[string]rest.Storage{
		"serviceaccounts": serviceAccountStorage,
	}
	return restStorage, true, nil
}
