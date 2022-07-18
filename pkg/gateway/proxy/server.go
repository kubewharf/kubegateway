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
	apiserver "github.com/kubewharf/apiserver-runtime/pkg/server"
	genericapiserver "k8s.io/apiserver/pkg/server"
	serverstorage "k8s.io/apiserver/pkg/server/storage"
	"k8s.io/kubernetes/pkg/master"

	"github.com/kubewharf/kubegateway/pkg/gateway/controllers"
	// RESTStorage installers
)

type Config struct {
	RecommendedConfig *apiserver.RecommendedConfig
	ExtraConfig       ExtraConfig
}

type ExtraConfig struct {
	UpstreamClusterController *controllers.UpstreamClusterController
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
	name := "kube-gateway-proxy"
	s, err := c.GenericConfig.New(name, delegationTarget)
	if err != nil {
		return nil, err
	}

	if c.ExtraConfig.UpstreamClusterController != nil {
		// start upstream controller
		startUpstreamControllerHookName := "kube-gateway-start-upstream-controller"
		err := s.AddPostStartHook(startUpstreamControllerHookName, func(context genericapiserver.PostStartHookContext) error {
			go c.ExtraConfig.UpstreamClusterController.Run(context.StopCh)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	return apiserver.New(name, s), nil
}

// DefaultAPIResourceConfigSource returns default configuration for an APIResource.
func DefaultAPIResourceConfigSource() *serverstorage.ResourceConfig {
	resourceConfig := master.DefaultAPIResourceConfigSource()
	return resourceConfig
}
