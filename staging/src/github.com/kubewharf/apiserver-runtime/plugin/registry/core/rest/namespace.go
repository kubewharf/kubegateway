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
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apiserver/pkg/registry/generic"
	"k8s.io/apiserver/pkg/registry/rest"
	genericapiserver "k8s.io/apiserver/pkg/server"
	serverstorage "k8s.io/apiserver/pkg/server/storage"
	"k8s.io/client-go/kubernetes"
	"k8s.io/kubernetes/pkg/master"
	namespacestore "k8s.io/kubernetes/pkg/registry/core/namespace/storage"
	"k8s.io/kubernetes/pkg/util/async"
)

type NamepsaceLegacyRESTStorageProvider struct {
}

func (NamepsaceLegacyRESTStorageProvider) ResourceName() string {
	return "namespaces"
}

func (NamepsaceLegacyRESTStorageProvider) NewRESTStorage(apiResourceConfigSource serverstorage.APIResourceConfigSource, restOptionsGetter generic.RESTOptionsGetter) (map[string]rest.Storage, bool, error) {
	if !apiResourceConfigSource.ResourceEnabled(corev1.SchemeGroupVersion.WithResource("namespaces")) {
		return nil, false, nil
	}
	namespaceStorage, namespaceStatusStorage, namespaceFinalizeStorage, err := namespacestore.NewREST(restOptionsGetter)
	if err != nil {
		return nil, false, err
	}

	restStorage := map[string]rest.Storage{
		"namespaces":          namespaceStorage,
		"namespaces/status":   namespaceStatusStorage,
		"namespaces/finalize": namespaceFinalizeStorage,
	}
	return restStorage, true, nil
}

func (NamepsaceLegacyRESTStorageProvider) PostStartHook() (string, genericapiserver.PostStartHookFunc, error) {
	nsInit := NewNamespaceInitializer()
	return "ensure-system-namespaces", nsInit.PostStartHook, nil
}

func NewNamespaceInitializer() *NamespaceInitializer {
	systemNamespaces := []string{metav1.NamespaceSystem, metav1.NamespacePublic, corev1.NamespaceNodeLease}

	return &NamespaceInitializer{
		controller: &master.Controller{
			SystemNamespaces:         systemNamespaces,
			SystemNamespacesInterval: 1 * time.Minute,
		},
	}
}

type NamespaceInitializer struct {
	controller *master.Controller

	runner *async.Runner
}

func (c *NamespaceInitializer) PostStartHook(hookContext genericapiserver.PostStartHookContext) error {
	if c.runner != nil {
		return nil
	}
	clientsets, err := kubernetes.NewForConfig(hookContext.LoopbackClientConfig)
	if err != nil {
		return err
	}
	c.controller.NamespaceClient = clientsets.CoreV1()
	c.runner = async.NewRunner(c.controller.RunKubernetesNamespaces)
	c.runner.Start()

	go func() {
		<-hookContext.StopCh
		c.runner.Stop()
	}()
	return nil
}
