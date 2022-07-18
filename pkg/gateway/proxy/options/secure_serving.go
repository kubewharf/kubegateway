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
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apiserver/pkg/server"
	"k8s.io/apiserver/pkg/server/options"

	contronplaneoptions "github.com/kubewharf/kubegateway/pkg/gateway/controlplane/options"
)

type SecureServingOptions struct {
	Ports []int
}

func NewSecureServingOptions() *SecureServingOptions {
	return &SecureServingOptions{}
}

func (s *SecureServingOptions) ValidateWith(controlplaneSecureServingOptions contronplaneoptions.SecureServingOptions) []error {
	if s == nil {
		return nil
	}
	errors := []error{}

	usedPorts := sets.NewInt(controlplaneSecureServingOptions.BindPort)
	for _, port := range controlplaneSecureServingOptions.OtherPorts {
		usedPorts.Insert(port)
	}

	if len(s.Ports) == 0 {
		errors = append(errors, fmt.Errorf("--proxy-secure-ports must be set"))
	}
	for _, port := range s.Ports {
		if port < 1 || port > 65535 {
			errors = append(errors, fmt.Errorf("ports in --proxy-secure-ports %v must be between 1 and 65535, inclusive. It cannot be turned off with 0", port))
		}
		if usedPorts.Has(port) {
			errors = append(errors, fmt.Errorf("ports in --proxy-secure-ports %v is duplicate in --secure-port or --other-secure-ports", port))
		} else {
			usedPorts.Insert(port)
		}
	}

	return errors
}

func (s *SecureServingOptions) AddFlags(fs *pflag.FlagSet) {
	if s == nil {
		return
	}
	fs.IntSliceVar(&s.Ports, "proxy-secure-ports", s.Ports, "A list of ports which to serve HTTPS for apiserver proxy with authentication and authorization.")
}

func (s *SecureServingOptions) ApplyTo(
	secureServingInfo **server.SecureServingInfo,
	controlplaneSecureServingOptions contronplaneoptions.SecureServingOptions,
) error {
	options := deepcopySecureServingOptions(controlplaneSecureServingOptions)

	options.BindPort = s.Ports[0]
	options.Listener = nil
	options.Required = true
	if len(s.Ports) > 1 {
		options.OtherPorts = s.Ports[1:]
	} else {
		options.OtherPorts = nil
	}

	// we don't need loopbackconfig for proxy
	return options.ApplyTo(secureServingInfo, nil)
}

func deepcopySecureServingOptions(in contronplaneoptions.SecureServingOptions) contronplaneoptions.SecureServingOptions {
	out := in
	if in.SecureServingOptionsWithLoopback != nil {
		in, out := &in.SecureServingOptionsWithLoopback, &out.SecureServingOptionsWithLoopback
		*out = new(options.SecureServingOptionsWithLoopback)
		**out = **in

		if (*in).SecureServingOptions != nil {
			in, out := &(*in).SecureServingOptions, &(*out).SecureServingOptions
			*out = new(options.SecureServingOptions)
			**out = **in
		}
	}
	return out
}
