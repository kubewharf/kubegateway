// Copyright 2022 ByteDance and its affiliates.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package options

import (
	"fmt"
	"net"

	"github.com/kubewharf/apiserver-runtime/pkg/server"
	apiserveroptions "github.com/kubewharf/apiserver-runtime/pkg/server/options"
	"k8s.io/apiserver/pkg/admission"
	genericoptions "k8s.io/apiserver/pkg/server/options"
	serverstorage "k8s.io/apiserver/pkg/server/storage"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	cliflag "k8s.io/component-base/cli/flag"
)

// ControlPlaneOptions contains RecommendedOptions and use its own
// secure serving options with reuse port and proxy protocol
type ControlPlaneOptions struct {
	*apiserveroptions.RecommendedOptions

	SecureServing *SecureServingOptions
}

// NewControlPlaneOptions return a new controle plane options
func NewControlPlaneOptions() *ControlPlaneOptions {
	recommended := apiserveroptions.NewRecommendedOptions().
		WithAll().
		WithProcessInfo(genericoptions.NewProcessInfo("kube-gateway", "kube-system"))
	// with out secure serving

	recommended.SecureServing = nil
	return &ControlPlaneOptions{
		RecommendedOptions: recommended,
		SecureServing:      NewSecureServingOptions(),
	}
}

func (o *ControlPlaneOptions) Flags() (fss cliflag.NamedFlagSets) {
	fss = o.RecommendedOptions.Flags()
	if o.SecureServing != nil {
		o.SecureServing.AddFlags(fss.FlagSet("secure serving"))
	}
	return fss
}

func (o *ControlPlaneOptions) Complete() error {
	if err := o.RecommendedOptions.Complete(); err != nil {
		return err
	}

	if o.ServerRun != nil {
		if err := o.ServerRun.DefaultAdvertiseAddress(o.SecureServing.SecureServingOptions); err != nil {
			return err
		}
	}
	if o.SecureServing != nil {
		if err := o.SecureServing.MaybeDefaultWithSelfSignedCerts(o.ServerRun.AdvertiseAddress.String(), []string{"kubernetes.default.svc", "kubernetes.default", "kubernetes"}, []net.IP{}); err != nil {
			return fmt.Errorf("error creating self-signed certificates: %v", err)
		}
	}

	return nil
}

// Sometimes you need to apply secure serving options firstly to get loopbackClientConfig
func (o *ControlPlaneOptions) ApplySecureServingTo(recommended *server.RecommendedConfig, tweakLoopbackConfig func(*rest.Config)) error {
	if o.SecureServing == nil {
		return nil
	}
	err := o.SecureServing.ApplyTo(&recommended.SecureServing, &recommended.LoopbackClientConfig)
	if err != nil {
		return err
	}

	// loopback client and backend client
	if recommended.LoopbackClientConfig != nil {
		// Disable compression for self-communication, since we are going to be
		// on a fast local network
		recommended.Config.LoopbackClientConfig.DisableCompression = true

		if tweakLoopbackConfig != nil {
			tweakLoopbackConfig(recommended.Config.LoopbackClientConfig)
		}
		loopbackclient, err := kubernetes.NewForConfig(recommended.Config.LoopbackClientConfig)
		if err != nil {
			return err
		}
		recommended.LoopbackClientset = loopbackclient
		recommended.LoopbackSharedInformerFactory = informers.NewSharedInformerFactory(loopbackclient, 0)
	}

	return nil
}

// ApplyTo adds RecommendedOptions to the server configuration.
// pluginInitializers can be empty, it is only need for additional initializers.
func (o *ControlPlaneOptions) ApplyTo(
	recommended *server.RecommendedConfig,
	tweakLoopbackConfig func(*rest.Config),
	defaultResourceConfig *serverstorage.ResourceConfig,
	pluginInitializers ...admission.PluginInitializer,
) error {
	if recommended.Config.SecureServing == nil && recommended.Config.LoopbackClientConfig == nil {
		err := o.ApplySecureServingTo(recommended, tweakLoopbackConfig)
		if err != nil {
			return err
		}
	}
	return o.RecommendedOptions.ApplyTo(recommended, tweakLoopbackConfig, defaultResourceConfig, pluginInitializers...)
}

func (o *ControlPlaneOptions) Validate() []error {
	errors := []error{}
	errors = append(errors, o.RecommendedOptions.Validate()...)
	if o.SecureServing != nil {
		errors = append(errors, o.SecureServing.Validate()...)
	}
	return errors
}
