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

package authorizer

import (
	"time"

	"k8s.io/apiserver/pkg/authorization/authorizer"

	"github.com/kubewharf/kubegateway/pkg/clusters"
	authrizationwebhook "github.com/kubewharf/kubegateway/pkg/gateway/authorization/webhook"
)

type AuthorizerConfig struct {
	CacheAuthorizedTTL    time.Duration
	CacheUnauthorizedTTL  time.Duration
	ClusterClientProvider clusters.ClientProvider
}

func (c *AuthorizerConfig) New() (authorizer.Authorizer, authorizer.RuleResolver, error) {
	var remoteAuthorizer authorizer.Authorizer
	if c.ClusterClientProvider != nil {
		remoteAuthorizer = authrizationwebhook.NewMultiClusterSubjectAccessReviewAuthorizer(
			c.ClusterClientProvider,
			c.CacheAuthorizedTTL,
			c.CacheUnauthorizedTTL,
		)
	}
	return remoteAuthorizer, nil, nil
}
