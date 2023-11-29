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
	genericoptions "k8s.io/apiserver/pkg/server/options"
	cliflag "k8s.io/component-base/cli/flag"

	proxyoptions "github.com/kubewharf/kubegateway/pkg/gateway/proxy/options"
)

type ProxyOptions struct {
	Authentication *proxyoptions.AuthenticationOptions
	Authorization  *proxyoptions.AuthorizationOptions
	SecureServing  *proxyoptions.SecureServingOptions
	ProcessInfo    *genericoptions.ProcessInfo
	Logging        *proxyoptions.LoggingOptions
	RateLimiter    *proxyoptions.RateLimiterOptions
}

func NewProxyOptions() *ProxyOptions {
	return &ProxyOptions{
		Authentication: proxyoptions.NewAuthenticationOptions(),
		Authorization:  proxyoptions.NewAuthorizationOptions(),
		SecureServing:  proxyoptions.NewSecureServingOptions(),
		ProcessInfo:    genericoptions.NewProcessInfo("kube-gateway-proxy", "kube-system"),
		Logging:        proxyoptions.NewLoggingOptions(),
		RateLimiter:    proxyoptions.NewRateLimiterOptions(),
	}
}

// Flags returns flags for a proxy by section name
func (s *ProxyOptions) Flags() (fss cliflag.NamedFlagSets) {
	fs := fss.FlagSet("proxy")
	s.Authentication.AddFlags(fs)
	s.Authorization.AddFlags(fs)
	s.SecureServing.AddFlags(fs)
	s.Logging.AddFlags(fs)
	s.RateLimiter.AddFlags(fs)
	return
}
