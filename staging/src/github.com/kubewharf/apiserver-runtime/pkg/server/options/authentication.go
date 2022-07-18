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
	genericserver "k8s.io/apiserver/pkg/server"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	openapicommon "k8s.io/kube-openapi/pkg/common"
	buildinoptions "k8s.io/kubernetes/pkg/kubeapiserver/options"

	"github.com/kubewharf/apiserver-runtime/pkg/server/authenticator"
)

// AuthenticationOptions wraps BuiltInAuthenticationOptions and mask some unused options
// It only use subset of master's authentication builtin options
type AuthenticationOptions struct {
	*buildinoptions.BuiltInAuthenticationOptions
}

func NewAuthenticationOptions() *AuthenticationOptions {
	o := &AuthenticationOptions{
		BuiltInAuthenticationOptions: buildinoptions.NewBuiltInAuthenticationOptions(),
	}
	return o
}

func (o *AuthenticationOptions) WithAnonymous() *AuthenticationOptions {
	o.BuiltInAuthenticationOptions = o.BuiltInAuthenticationOptions.WithAnonymous()
	return o
}

func (o *AuthenticationOptions) WithBootstrapToken() *AuthenticationOptions {
	o.BuiltInAuthenticationOptions = o.BuiltInAuthenticationOptions.WithBootstrapToken()
	return o
}

func (o *AuthenticationOptions) WithClientCert() *AuthenticationOptions {
	o.BuiltInAuthenticationOptions = o.BuiltInAuthenticationOptions.WithClientCert()
	return o
}

func (o *AuthenticationOptions) WithOIDC() *AuthenticationOptions {
	o.BuiltInAuthenticationOptions = o.BuiltInAuthenticationOptions.WithOIDC()
	return o
}

func (o *AuthenticationOptions) WithRequestHeader() *AuthenticationOptions {
	o.BuiltInAuthenticationOptions = o.BuiltInAuthenticationOptions.WithRequestHeader()
	return o
}

func (o *AuthenticationOptions) WithServiceAccounts() *AuthenticationOptions {
	o.BuiltInAuthenticationOptions = o.BuiltInAuthenticationOptions.WithServiceAccounts()
	return o
}

func (o *AuthenticationOptions) WithWebHook() *AuthenticationOptions {
	o.BuiltInAuthenticationOptions = o.BuiltInAuthenticationOptions.WithWebHook()
	o.BuiltInAuthenticationOptions.WebHook.Version = "v1"
	return o
}

func (o *AuthenticationOptions) WithAll() *AuthenticationOptions {
	return o.WithAnonymous().
		WithBootstrapToken().
		WithClientCert().
		WithOIDC().
		WithRequestHeader().
		WithServiceAccounts().
		WithWebHook()
}

func (o *AuthenticationOptions) AddFlags(fs *pflag.FlagSet) {
	fs.DurationVar(&o.TokenSuccessCacheTTL, "authentication-token-success-cache-ttl", o.TokenSuccessCacheTTL,
		"The duration to cache seccess responses from the webhook token authenticator.")
	fs.DurationVar(&o.TokenFailureCacheTTL, "authentication-token-failure-cache-ttl", o.TokenFailureCacheTTL,
		"The duration to cache failure responses from the webhook token authenticator.")

	o.BuiltInAuthenticationOptions.AddFlags(fs)
}

func (o *AuthenticationOptions) ToAuthenticationConfig() (authenticator.Config, error) {
	if o == nil {
		return authenticator.Config{}, nil
	}

	builtin, err := o.BuiltInAuthenticationOptions.ToAuthenticationConfig()
	if err != nil {
		return authenticator.Config{}, err
	}

	ret := authenticator.NewFrom(builtin)
	return ret, nil
}

func (o *AuthenticationOptions) ApplyTo(
	authenticationInfo *genericserver.AuthenticationInfo,
	servingInfo *genericserver.SecureServingInfo,
	openAPIConfig *openapicommon.Config,
	extclient kubernetes.Interface,
	versionedInformer informers.SharedInformerFactory,
) error {
	if o == nil {
		authenticationInfo.Authenticator = nil
		return nil
	}

	cfg, err := o.ToAuthenticationConfig()
	if err != nil {
		return err
	}
	cfg.Complete(extclient, versionedInformer)

	// get the clientCA information
	if clientCert := cfg.GetClientCert(); clientCert != nil {
		if err := authenticationInfo.ApplyClientCert(clientCert.CAContentProvider, servingInfo); err != nil {
			return fmt.Errorf("unable to assign  client CA file: %v", err)
		}
	}

	if requestHeaderConfig := cfg.GetRequestHeaderConfig(); requestHeaderConfig != nil {
		if err := authenticationInfo.ApplyClientCert(requestHeaderConfig.CAContentProvider, servingInfo); err != nil {
			return fmt.Errorf("unable to load request-header-client-ca-file: %v", err)
		}
	}

	// create authenticator
	authenticator, securityDefinitions, err := cfg.New()
	if err != nil {
		return err
	}
	authenticationInfo.Authenticator = authenticator
	if openAPIConfig != nil {
		openAPIConfig.SecurityDefinitions = securityDefinitions
	}

	authenticationInfo.APIAudiences = o.APIAudiences
	// disable basic auth
	authenticationInfo.SupportsBasicAuth = false

	return nil
}
