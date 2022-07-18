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
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/util/sets"
	openapinamer "k8s.io/apiserver/pkg/endpoints/openapi"
	genericapiserver "k8s.io/apiserver/pkg/server"
	"k8s.io/apiserver/pkg/server/filters"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
	openapicommon "k8s.io/kube-openapi/pkg/common"

	"github.com/kubewharf/apiserver-runtime/pkg/registry"
	"github.com/kubewharf/apiserver-runtime/pkg/scheme"
	"github.com/kubewharf/apiserver-runtime/pkg/server/storage"
)

type RecommendedConfig struct {
	genericapiserver.Config

	extraConfig
}

type extraConfig struct {
	Scheme *runtime.Scheme
	Codecs serializer.CodecFactory

	// please get LoopbackClientConfig from genericapiserver.Config
	// LoopbackClientConfig *restclient.Config

	// LoopbackClientset holds the privileged privileges and knows how to communicate with local apiserver
	LoopbackClientset kubernetes.Interface

	// LoopbackSharedInformerFactory provides shared informers for Kubernetes resources in local apiserver.
	// This value is set by RecommendedOptions.CoreAPI.ApplyTo called by RecommendedOptions.ApplyTo. It uses
	// an in-cluster client config. by default, or the kubeconfig given with kubeconfig command line flag.
	LoopbackSharedInformerFactory informers.SharedInformerFactory

	// BackendClientConfig holds the kubernetes client configuration for connection to backend API server.
	// This value is set by RecommendedOptions.CoreAPI.ApplyTo called by RecommendedOptions.ApplyTo.
	// By default in-cluster client config is used.
	BackendClientConfig *restclient.Config

	// // BackendClientset knows how to communicate with backend API server.
	// BackendClientset kubernetes.Interface

	// // BackendSharedInformerFactory provides shared informers for Kubernetes resources in backend apiserver.
	// // This value is set by RecommendedOptions.CoreAPI.ApplyTo called by RecommendedOptions.ApplyTo. It uses
	// // an in-cluster client config. by default, or the kubeconfig given with kubeconfig command line flag.
	// BackendSharedInformerFactory informers.SharedInformerFactory

	// StorageFactory is the interface to locate the storage for a given GroupResource
	// It extend apiserver storage.StorageFactory interface to expose more function of
	// DefaultStorageFactory
	StorageFactory storage.StorageFactory

	// RESTStorageOptionsFactory knowns how to create rest storage
	RESTStorageOptionsFactory *registry.RESTStorageOptionsFactory
}

// apiserver-runtime/pkg/scheme.Scheme and Codecs are recommended
func NewRecommendedConfig(scheme *runtime.Scheme, codecs serializer.CodecFactory) *RecommendedConfig {
	recommended := &RecommendedConfig{
		Config: *genericapiserver.NewConfig(codecs),
		extraConfig: extraConfig{
			Scheme: scheme,
			Codecs: codecs,
		},
	}
	recommended.Config.LongRunningFunc = DefaultLongRunningFunc

	return recommended
}

func (c *RecommendedConfig) WithOpenapiConfig(title string, def openapicommon.GetOpenAPIDefinitions) *RecommendedConfig {
	c.Config.OpenAPIConfig = genericapiserver.DefaultOpenAPIConfig(def, openapinamer.NewDefinitionNamer(scheme.Scheme))
	c.Config.OpenAPIConfig.Info.Title = title
	return c
}

type CompletedConfig struct {
	*completedConfig
}

type completedConfig struct {
	*genericapiserver.CompletedConfig
	*extraConfig
}

func (c *RecommendedConfig) Complete() CompletedConfig {
	cfg := c.Config.Complete(nil)
	return CompletedConfig{&completedConfig{
		&cfg,
		&c.extraConfig,
	}}
}

func (c *CompletedConfig) New(name string, delegationTarget genericapiserver.DelegationTarget) (*genericapiserver.GenericAPIServer, error) {
	genericServer, err := c.CompletedConfig.New(name, delegationTarget)
	if err != nil {
		return nil, err
	}

	if c.LoopbackSharedInformerFactory != nil {
		startLoopbackImformersHookName := "apiserver-start-loopback-imformers"
		err := genericServer.AddPostStartHook(startLoopbackImformersHookName, func(context genericapiserver.PostStartHookContext) error {
			c.LoopbackSharedInformerFactory.Start(context.StopCh)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	return genericServer, nil
}

var (
	DefaultLongRunningFunc = filters.BasicLongRunningRequestCheck(
		sets.NewString("watch", "proxy"),
		sets.NewString("attach", "exec", "proxy", "log", "portforward"),
	)
)
