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

package server

import (
	"github.com/kubewharf/apiserver-runtime/pkg/server"
	apiserver "github.com/kubewharf/apiserver-runtime/pkg/server"
	corerestplugin "github.com/kubewharf/apiserver-runtime/plugin/registry/core/rest"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	genericapiserver "k8s.io/apiserver/pkg/server"
	serverstorage "k8s.io/apiserver/pkg/server/storage"
	"k8s.io/kubernetes/pkg/master"

	proxyv1alpha1 "github.com/kubewharf/kubegateway/pkg/apis/proxy/v1alpha1"
	gatewayinformers "github.com/kubewharf/kubegateway/pkg/client/informers"
	gatewayclientset "github.com/kubewharf/kubegateway/pkg/client/kubernetes"
	"github.com/kubewharf/kubegateway/pkg/gateway/controlplane/bootstrap"

	// RESTStorage installers
	rbacrest "k8s.io/kubernetes/pkg/registry/rbac/rest"

	proxyrest "github.com/kubewharf/kubegateway/pkg/gateway/controlplane/registry/proxy/rest"
)

type Config struct {
	RecommendedConfig *apiserver.RecommendedConfig
	ExtraConfig       ExtraConfig
}

type ExtraConfig struct {
	GatewayClientset             gatewayclientset.Interface
	GatewaySharedInformerFactory gatewayinformers.SharedInformerFactory
}

// Complete fills in any fields not set that are required to have valid data. It's mutating the receiver.
func (c *Config) Complete() *CompletedConfig {
	cfg := completedConfig{
		GenericConfig: c.RecommendedConfig.Complete(),
		ExtraConfig:   &c.ExtraConfig,
	}
	return &CompletedConfig{&cfg}
}

type completedConfig struct {
	GenericConfig apiserver.CompletedConfig
	ExtraConfig   *ExtraConfig
}

// CompletedConfig embeds a private pointer that cannot be instantiated outside of this package
type CompletedConfig struct {
	*completedConfig
}

func (c *CompletedConfig) New(delegationTarget genericapiserver.DelegationTarget) (*apiserver.GenericServer, error) {
	name := "kube-gateway"
	s, err := c.GenericConfig.New(name, delegationTarget)
	if err != nil {
		return nil, err
	}

	if c.ExtraConfig.GatewaySharedInformerFactory != nil {
		startGatewaySharedInformers := "kube-gateway-start-imformers"
		err := s.AddPostStartHook(startGatewaySharedInformers, func(context genericapiserver.PostStartHookContext) error {
			c.ExtraConfig.GatewaySharedInformerFactory.Start(context.StopCh)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	// Install Legacy APIs
	// we only need namespace, secret, serviceaccount and rbac in control plane registry
	legacyRESTStorageProviders := []server.LegecyRESTStorageProvider{
		corerestplugin.NamepsaceLegacyRESTStorageProvider{},
		corerestplugin.SecretLegacyRESTStorageProvider{},
		corerestplugin.ServiceAccountLegacyRESTStorageProvider{},
	}
	if err := apiserver.InstallLegacyAPI(s, c.GenericConfig.MergedResourceConfig, c.GenericConfig.RESTOptionsGetter, legacyRESTStorageProviders...); err != nil {
		return nil, err
	}

	restStorageProviders := []master.RESTStorageProvider{
		// Install Other group APIs
		rbacrest.RESTStorageProvider{Authorizer: c.GenericConfig.Authorization.Authorizer},
		// Install Gateway APIs
		proxyrest.NewRESTStorageProviderOrDie(c.GenericConfig.Scheme, c.GenericConfig.RESTStorageOptionsFactory),
	}

	err = apiserver.InstallAPIs(s, c.GenericConfig.MergedResourceConfig, c.GenericConfig.RESTOptionsGetter, restStorageProviders...)
	if err != nil {
		return nil, err
	}

	for hookName, hook := range bootstrap.PostStartHooks() {
		if !c.GenericConfig.Config.DisabledPostStartHooks.Has(hookName) {
			s.AddPostStartHookOrDie(hookName, hook)
		}
	}

	return apiserver.New(name, s), nil
}

// DefaultAPIResourceConfigSource returns default configuration for an APIResource.
func DefaultAPIResourceConfigSource() *serverstorage.ResourceConfig {
	resourceConfig := serverstorage.NewResourceConfig()
	resourceConfig.EnableVersions(
		// enable some native apis
		corev1.SchemeGroupVersion,
		rbacv1.SchemeGroupVersion,
		// add gateway apis here
		proxyv1alpha1.SchemeGroupVersion,
	)

	return resourceConfig
}
