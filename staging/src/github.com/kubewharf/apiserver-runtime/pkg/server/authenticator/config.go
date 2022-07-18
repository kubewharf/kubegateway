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

package authenticator

import (
	"time"

	"github.com/go-openapi/spec"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apiserver/pkg/authentication/authenticator"
	"k8s.io/apiserver/pkg/authentication/authenticatorfactory"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	v1listers "k8s.io/client-go/listers/core/v1"
	serviceaccountcontroller "k8s.io/kubernetes/pkg/controller/serviceaccount"
	"k8s.io/kubernetes/pkg/features"
	kubeauthenticator "k8s.io/kubernetes/pkg/kubeapiserver/authenticator"
	"k8s.io/kubernetes/pkg/serviceaccount"
	"k8s.io/kubernetes/plugin/pkg/auth/authenticator/token/bootstrap"
)

type BootstrapTokenAuthenticationConfig struct {
	TokenAuthenticator authenticator.Token
}

type ClientCertAuthenticationConfig struct {
	// CAContentProvider are the options for verifying incoming connections using mTLS and directly assigning to users.
	// Generally this is the CA bundle file used to authenticate client certificates
	// If this is nil, then mTLS will not be used.
	CAContentProvider authenticatorfactory.CAContentProvider
}

type ServiceAccountAuthenticationConfig struct {
	KeyFiles    []string
	Lookup      bool
	Issuer      string
	TokenGetter serviceaccount.ServiceAccountTokenGetter
}

type OIDCAuthenticationConfig struct {
	IssuerURL      string
	ClientID       string
	CAFile         string
	UsernameClaim  string
	UsernamePrefix string
	GroupsClaim    string
	GroupsPrefix   string
	SigningAlgs    []string
	RequiredClaims map[string]string
}

type WebhookAuthenticationConfig struct {
	ConfigFile string
	Version    string
	CacheTTL   time.Duration
}

// Config is the minimal configuration needed to create an authenticator
// built to authenticate for control plane server
//
// It is a subset of https://github.com/kubernetes/kubernetes/blob/master/pkg/kubeapiserver/authenticator/config.go
type Config struct {
	config kubeauthenticator.Config
}

func NewFrom(c kubeauthenticator.Config) Config {
	return Config{
		config: c,
	}
}

func (config *Config) Complete(extclient kubernetes.Interface,
	versionedInformer informers.SharedInformerFactory) {
	if config.config.BootstrapToken {
		config.config.BootstrapTokenAuthenticator = bootstrap.NewTokenAuthenticator(
			versionedInformer.Core().V1().Secrets().Lister().Secrets(metav1.NamespaceSystem),
		)
	}

	if config.config.ServiceAccountLookup || utilfeature.DefaultFeatureGate.Enabled(features.TokenRequest) {
		config.config.ServiceAccountTokenGetter = NewServiceAccountTokenGetterWithoutPod(
			extclient,
			versionedInformer.Core().V1().Secrets().Lister(),
			versionedInformer.Core().V1().ServiceAccounts().Lister(),
		)
	}
}

func (config Config) GetBuiltinConfig() kubeauthenticator.Config {
	return config.config
}

func (config Config) GetRequestHeaderConfig() *authenticatorfactory.RequestHeaderConfig {
	return config.config.RequestHeaderConfig
}

func (config Config) GetAnonymous() bool {
	return config.config.Anonymous
}

func (config Config) GetClientCert() *ClientCertAuthenticationConfig {
	if config.config.ClientCAContentProvider != nil {
		return &ClientCertAuthenticationConfig{
			CAContentProvider: config.config.ClientCAContentProvider,
		}
	}
	return nil
}

func (config Config) GetBootstrapTokenConfig() *BootstrapTokenAuthenticationConfig {
	if config.config.BootstrapToken {
		return &BootstrapTokenAuthenticationConfig{
			TokenAuthenticator: config.config.BootstrapTokenAuthenticator,
		}
	}
	return nil
}

func (config Config) GetAPIAudiences() authenticator.Audiences {
	return config.config.APIAudiences
}

func (config Config) GetTokenCacheTTL() (success, failure time.Duration) {
	return config.config.TokenSuccessCacheTTL, config.config.TokenFailureCacheTTL
}

func (config Config) GetServiceAccounts() *ServiceAccountAuthenticationConfig {
	if len(config.config.ServiceAccountKeyFiles) > 0 {
		return &ServiceAccountAuthenticationConfig{
			KeyFiles:    config.config.ServiceAccountKeyFiles,
			Lookup:      config.config.ServiceAccountLookup,
			Issuer:      config.config.ServiceAccountIssuer,
			TokenGetter: config.config.ServiceAccountTokenGetter,
		}
	}
	return nil
}

func (config Config) GetOIDC() *OIDCAuthenticationConfig {
	if len(config.config.OIDCIssuerURL) > 0 && len(config.config.OIDCClientID) > 0 {
		return &OIDCAuthenticationConfig{
			CAFile:         config.config.OIDCCAFile,
			ClientID:       config.config.OIDCClientID,
			GroupsClaim:    config.config.OIDCGroupsClaim,
			GroupsPrefix:   config.config.OIDCGroupsPrefix,
			IssuerURL:      config.config.OIDCIssuerURL,
			UsernameClaim:  config.config.OIDCUsernameClaim,
			UsernamePrefix: config.config.OIDCUsernamePrefix,
			SigningAlgs:    config.config.OIDCSigningAlgs,
			RequiredClaims: config.config.OIDCRequiredClaims,
		}
	}
	return nil
}

func (config Config) GetWebhook() *WebhookAuthenticationConfig {
	if len(config.config.WebhookTokenAuthnConfigFile) > 0 {
		return &WebhookAuthenticationConfig{
			ConfigFile: config.config.WebhookTokenAuthnConfigFile,
			Version:    config.config.WebhookTokenAuthnVersion,
			CacheTTL:   config.config.WebhookTokenAuthnCacheTTL,
		}
	}
	return nil
}

func (config Config) New() (authenticator.Request, *spec.SecurityDefinitions, error) {
	return config.config.New()
}

type serviceAccountTokenGetterWithoutPod struct {
	serviceaccount.ServiceAccountTokenGetter
}

func NewServiceAccountTokenGetterWithoutPod(
	c kubernetes.Interface,
	secretLister v1listers.SecretLister,
	serviceAccountLister v1listers.ServiceAccountLister,
) serviceaccount.ServiceAccountTokenGetter {
	return &serviceAccountTokenGetterWithoutPod{
		ServiceAccountTokenGetter: serviceaccountcontroller.NewGetterFromClient(
			c,
			secretLister,
			serviceAccountLister,
			nil,
		),
	}
}

// always return not found
func (g *serviceAccountTokenGetterWithoutPod) GetPod(namespace, name string) (*corev1.Pod, error) {
	return nil, errors.NewNotFound(corev1.Resource("pods"), name)
}
