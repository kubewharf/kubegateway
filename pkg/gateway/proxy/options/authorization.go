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
	"time"

	"github.com/spf13/pflag"
	genericserver "k8s.io/apiserver/pkg/server"

	"github.com/kubewharf/kubegateway/pkg/clusters"
	"github.com/kubewharf/kubegateway/pkg/gateway/proxy/authorizer"
)

type AuthorizationOptions struct {
	CacheAuthorizedTTL   time.Duration
	CacheUnauthorizedTTL time.Duration
}

func NewAuthorizationOptions() *AuthorizationOptions {
	o := &AuthorizationOptions{
		CacheAuthorizedTTL:   5 * time.Minute,
		CacheUnauthorizedTTL: 30 * time.Second,
	}
	return o
}

func (o *AuthorizationOptions) Validate() []error {
	return nil
}

func (o *AuthorizationOptions) ToAuthorizationConfig(clientProvider clusters.ClientProvider) *authorizer.AuthorizerConfig {
	return &authorizer.AuthorizerConfig{
		CacheAuthorizedTTL:    o.CacheAuthorizedTTL,
		CacheUnauthorizedTTL:  o.CacheUnauthorizedTTL,
		ClusterClientProvider: clientProvider,
	}
}

func (o *AuthorizationOptions) AddFlags(fs *pflag.FlagSet) {
	fs.DurationVar(&o.CacheAuthorizedTTL, "proxy-authorization-cache-authorized-ttl",
		o.CacheAuthorizedTTL,
		"The duration to cache 'authorized' responses from the subject request authorizer.")
	fs.DurationVar(&o.CacheUnauthorizedTTL,
		"proxy-authorization-cache-unauthorized-ttl", o.CacheUnauthorizedTTL,
		"The duration to cache 'unauthorized' responses from the subject request authorizer.")
}

func (o *AuthorizationOptions) ApplyTo(
	genericConfig *genericserver.Config,
	clientProvider clusters.ClientProvider,
) error {
	cfg := o.ToAuthorizationConfig(clientProvider)
	authorizer, _, err := cfg.New()
	if err != nil {
		return err
	}
	genericConfig.Authorization.Authorizer = authorizer
	return nil
}
