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
	"errors"
	"time"

	"github.com/go-openapi/spec"
	"k8s.io/apiserver/pkg/authentication/authenticator"
	"k8s.io/apiserver/pkg/authentication/authenticatorfactory"
	"k8s.io/apiserver/pkg/authentication/group"
	"k8s.io/apiserver/pkg/authentication/request/anonymous"
	"k8s.io/apiserver/pkg/authentication/request/bearertoken"
	"k8s.io/apiserver/pkg/authentication/request/headerrequest"
	unionauth "k8s.io/apiserver/pkg/authentication/request/union"
	"k8s.io/apiserver/pkg/authentication/request/websocket"
	"k8s.io/apiserver/pkg/authentication/request/x509"

	"github.com/kubewharf/kubegateway/pkg/clusters"
	"github.com/kubewharf/kubegateway/pkg/gateway/authentication/token/webhook"
)

type ClientCertAuthenticationConfig struct {
	// CAContentProvider are the options for verifying incoming connections using mTLS and directly assigning to users.
	// Generally this is the CA bundle file used to authenticate client certificates
	// If this is nil, then mTLS will not be used.
	CAContentProvider authenticatorfactory.CAContentProvider
	// SNIVerifyOptionsPorvider provides dynamic verifyOptions for each sni hostname
	SNIVerifyOptionsPorvider x509.SNIVerifyOptionsProvider
}

func (c *ClientCertAuthenticationConfig) New() authenticator.Request {
	if c == nil {
		return nil
	}
	if c.CAContentProvider != nil && c.SNIVerifyOptionsPorvider != nil {
		return x509.NewSNIDynamic(c.SNIVerifyOptionsPorvider.SNIVerifyOptions, c.CAContentProvider.VerifyOptions, x509.CommonNameUserConversion)
	} else if c.CAContentProvider != nil && c.SNIVerifyOptionsPorvider == nil {
		return x509.NewDynamic(c.CAContentProvider.VerifyOptions, x509.CommonNameUserConversion)
	} else if c.CAContentProvider == nil && c.SNIVerifyOptionsPorvider != nil {
		return x509.NewSNIDynamic(c.SNIVerifyOptionsPorvider.SNIVerifyOptions, nil, x509.CommonNameUserConversion)
	}
	return nil
}

type TokenAuthenticationConfig struct {

	// remote cluster token auth
	ClusterClientProvider clusters.ClientProvider
}

// AuthenricatorConfig is the minimal configuration needed to create an authenticator
// built to delegate authentication to upstream kube API servers
type AuthenricatorConfig struct {
	RequestHeaderConfig *authenticatorfactory.RequestHeaderConfig

	ClientCert *ClientCertAuthenticationConfig

	// CacheTTL is the length of time that a token authentication answer will be cached.
	TokenSuccessCacheTTL time.Duration
	TokenFailureCacheTTL time.Duration
	APIAudiences         authenticator.Audiences

	TokenRequest *TokenAuthenticationConfig

	Anonymous bool
}

func (c AuthenricatorConfig) New() (authenticator.Request, *spec.SecurityDefinitions, error) {
	authenticators := []authenticator.Request{}
	securityDefinitions := spec.SecurityDefinitions{}

	// front-proxy first, then remote
	// Add the front proxy authenticator if requested
	if c.RequestHeaderConfig != nil {
		requestHeaderAuthenticator := headerrequest.NewDynamicVerifyOptionsSecure(
			c.RequestHeaderConfig.CAContentProvider.VerifyOptions,
			c.RequestHeaderConfig.AllowedClientNames,
			c.RequestHeaderConfig.UsernameHeaders,
			c.RequestHeaderConfig.GroupHeaders,
			c.RequestHeaderConfig.ExtraHeaderPrefixes,
		)
		authenticators = append(authenticators, requestHeaderAuthenticator)
	}

	// x509 client cert auth
	if c.ClientCert != nil {
		a := c.ClientCert.New()
		authenticators = append(authenticators, a)
	}

	if c.TokenRequest != nil {
		var tokenAuth authenticator.Token
		if c.TokenRequest.ClusterClientProvider != nil {
			tokenAuth = webhook.NewMultiClusterTokenReviewAuthenticator(c.TokenRequest.ClusterClientProvider, c.TokenSuccessCacheTTL, c.TokenFailureCacheTTL, c.APIAudiences)
		}
		if tokenAuth != nil {
			authenticators = append(authenticators, bearertoken.New(tokenAuth), websocket.NewProtocolAuthenticator(tokenAuth))
			securityDefinitions["BearerToken"] = &spec.SecurityScheme{
				SecuritySchemeProps: spec.SecuritySchemeProps{
					Type:        "apiKey",
					Name:        "authorization",
					In:          "header",
					Description: "Bearer Token authentication",
				},
			}
		}
	}

	if len(authenticators) == 0 {
		if c.Anonymous {
			return anonymous.NewAuthenticator(), &securityDefinitions, nil
		}
		return nil, nil, errors.New("No authentication method configured")
	}

	authenticator := group.NewAuthenticatedGroupAdder(unionauth.New(authenticators...))
	if c.Anonymous {
		authenticator = unionauth.NewFailOnError(authenticator, anonymous.NewAuthenticator())
	}
	return authenticator, &securityDefinitions, nil
}
