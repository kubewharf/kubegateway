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

package webhook

import (
	"context"
	"fmt"
	"sync"
	"time"

	"k8s.io/apiserver/pkg/authentication/authenticator"
	tokencache "k8s.io/apiserver/pkg/authentication/token/cache"
	webhooktoken "k8s.io/apiserver/plugin/pkg/authenticator/token/webhook"

	"github.com/kubewharf/kubegateway/pkg/clusters"
	"github.com/kubewharf/kubegateway/pkg/gateway/endpoints/request"
)

type multiClusterTokenReviewAuthenticator struct {
	tokenSuccessCacheTTL time.Duration
	tokenFailureCacheTTL time.Duration
	implicitAuds         authenticator.Audiences

	clientProvider clusters.ClientProvider
	caches         sync.Map
}

func NewMultiClusterTokenReviewAuthenticator(clientProvider clusters.ClientProvider, tokenSuccessCacheTTL, tokenFailureCacheTTL time.Duration, implicitAuds authenticator.Audiences) authenticator.Token {
	return &multiClusterTokenReviewAuthenticator{
		tokenSuccessCacheTTL: tokenSuccessCacheTTL,
		tokenFailureCacheTTL: tokenFailureCacheTTL,
		clientProvider:       clientProvider,
		caches:               sync.Map{},
		implicitAuds:         implicitAuds,
	}
}

func (a *multiClusterTokenReviewAuthenticator) AuthenticateToken(ctx context.Context, token string) (*authenticator.Response, bool, error) {
	info, ok := request.ExtraRequestInfoFrom(ctx)
	if !ok {
		return nil, false, fmt.Errorf("failed to get extra request info from context")
	}
	host := info.Hostname

	cluster, _, err := a.clientProvider.ClientFor(host)
	if err != nil {
		return nil, false, err
	}

	var tokenAuth authenticator.Token
	if a.tokenFailureCacheTTL == 0 && a.tokenSuccessCacheTTL == 0 {
		// if token cache ttl is 0, call upstream cluster directly
		tokenAuth = a.authenticateTokenForHost(host)
	} else {
		// split cache by host
		cache, loaded := a.caches.Load(host)
		if !loaded {
			// use token cache, if no cache is hit, authenticateToken() will be called
			// tokencache use a new context inheriting from context.Background() without all value of req.Context.
			cache, loaded = a.caches.LoadOrStore(host, tokencache.New(a.authenticateTokenForHost(host), false, a.tokenSuccessCacheTTL, a.tokenFailureCacheTTL))
			// destry cache when cluster stopped
			if !loaded {
				go func() {
					<-cluster.Context().Done()
					a.caches.Delete(host)
				}()
			}
		}
		tokenAuth = cache.(authenticator.Token)
	}
	return tokenAuth.AuthenticateToken(ctx, token)
}

// authenticate token by webhook.
func (a *multiClusterTokenReviewAuthenticator) authenticateTokenForHost(host string) authenticator.TokenFunc {
	return authenticator.TokenFunc(func(ctx context.Context, token string) (*authenticator.Response, bool, error) {
		_, client, err := a.clientProvider.ClientFor(host)
		if err != nil {
			return nil, false, err
		}
		// err is always nil, can be ignored
		tokenauth, _ := webhooktoken.NewFromInterface(client.AuthenticationV1().TokenReviews(), a.implicitAuds)
		newCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		return tokenauth.AuthenticateToken(newCtx, token)
	})
}
