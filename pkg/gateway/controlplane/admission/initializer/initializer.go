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

package initializer

import (
	"k8s.io/apiserver/pkg/admission"

	gatewayinformers "github.com/kubewharf/kubegateway/pkg/client/informers"
	gatewayclientset "github.com/kubewharf/kubegateway/pkg/client/kubernetes"
)

// WantsGatewayResourceClientSet defines a function which sets external ClientSet for admission plugins that need it
type WantsGatewayResourceClientSet interface {
	SetGatewayResourceClientSet(gatewayclientset.Interface)
	admission.InitializationValidator
}

// WantsGatewayResourceInformerFactory defines a function which sets InformerFactory for admission plugins that need it
type WantsGatewayResourceInformerFactory interface {
	SetGatewayResourceInformerFactory(gatewayinformers.SharedInformerFactory)
	admission.InitializationValidator
}

// New creates an instance of admission plugins initializer.
func New(
	gatewayclientset gatewayclientset.Interface,
	gatewayinformers gatewayinformers.SharedInformerFactory,
) pluginInitializer {
	return pluginInitializer{
		gatewayclientset: gatewayclientset,
		gatewayinformers: gatewayinformers,
	}
}

type pluginInitializer struct {
	gatewayclientset gatewayclientset.Interface
	gatewayinformers gatewayinformers.SharedInformerFactory
}

func (i pluginInitializer) Initialize(plugin admission.Interface) {
	if wants, ok := plugin.(WantsGatewayResourceClientSet); ok {
		wants.SetGatewayResourceClientSet(i.gatewayclientset)
	}
	if wants, ok := plugin.(WantsGatewayResourceInformerFactory); ok {
		wants.SetGatewayResourceInformerFactory(i.gatewayinformers)
	}
}
