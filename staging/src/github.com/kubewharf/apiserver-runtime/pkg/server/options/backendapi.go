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

package options

import (
	"fmt"

	"github.com/spf13/pflag"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog"

	"github.com/kubewharf/apiserver-runtime/pkg/server"
)

// BackendAPIOptions contains options to configure the connection to a core API Kubernetes apiserver.
type BackendAPIOptions struct {
	// BackendAPIKubeconfigPath is a filename for a kubeconfig file to contact the core API server with.
	// If it is not set, the in cluster config is used if any.
	BackendAPIKubeconfigPath string
}

func NewBackendAPIOptions() *BackendAPIOptions {
	return &BackendAPIOptions{}
}

func (o *BackendAPIOptions) AddFlags(fs *pflag.FlagSet) {
	if o == nil {
		return
	}

	fs.StringVar(&o.BackendAPIKubeconfigPath, "backend-kubeconfig", o.BackendAPIKubeconfigPath,
		"kubeconfig file pointing at the 'core' kubernetes server.")
}

func (o *BackendAPIOptions) ApplyTo(config *server.RecommendedConfig) error {
	if o == nil {
		return nil
	}

	// create shared informer for Kubernetes APIs
	var kubeconfig *rest.Config
	var err error
	if len(o.BackendAPIKubeconfigPath) > 0 {
		loadingRules := &clientcmd.ClientConfigLoadingRules{ExplicitPath: o.BackendAPIKubeconfigPath}
		loader := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, &clientcmd.ConfigOverrides{})
		kubeconfig, err = loader.ClientConfig()
		if err != nil {
			return fmt.Errorf("failed to load kubeconfig at %q: %v", o.BackendAPIKubeconfigPath, err)
		}
	} else {
		// try to load in cluster config
		kubeconfig, err = rest.InClusterConfig()
		if err != nil {
			klog.V(3).Infof("failed to load in-cluster config for backend apiserver")
			return nil
		}
	}

	config.BackendClientConfig = kubeconfig

	return nil
}

func (o *BackendAPIOptions) Validate() []error {
	return nil
}
