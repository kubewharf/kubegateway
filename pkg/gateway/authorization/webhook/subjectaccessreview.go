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

package subjectaccessreview

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	authorizationv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/cache"
	"k8s.io/apiserver/pkg/authorization/authorizer"
	"k8s.io/apiserver/pkg/util/webhook"
	"k8s.io/klog"

	"github.com/kubewharf/kubegateway/pkg/clusters"
	"github.com/kubewharf/kubegateway/pkg/gateway/endpoints/request"
)

const (
	retryBackoff = 500 * time.Millisecond
	// The maximum length of requester-controlled attributes to allow caching.
	maxControlledAttrCacheSize = 10000
)

var _ authorizer.Authorizer = &MultiClusterSubjectAccessReviewAuthorizer{}

type MultiClusterSubjectAccessReviewAuthorizer struct {
	// allowCacheTTL is the length of time that a successful authorization response will be cached
	allowCacheTTL time.Duration
	// denyCacheTTL is the length of time that an unsuccessful authorization response will be cached.
	// You generally want more responsive, "deny, try again" flows.
	denyCacheTTL time.Duration

	clientProvider  clusters.ClientProvider
	caches          sync.Map
	initialBackoff  time.Duration
	decisionOnError authorizer.Decision
}

func NewMultiClusterSubjectAccessReviewAuthorizer(clientProvider clusters.ClientProvider, allowCacheTTL, denyCacheTTL time.Duration) authorizer.Authorizer {
	return &MultiClusterSubjectAccessReviewAuthorizer{
		clientProvider:  clientProvider,
		allowCacheTTL:   allowCacheTTL,
		denyCacheTTL:    denyCacheTTL,
		initialBackoff:  retryBackoff,
		caches:          sync.Map{},
		decisionOnError: authorizer.DecisionDeny,
	}
}

func (a *MultiClusterSubjectAccessReviewAuthorizer) Authorize(ctx context.Context, attr authorizer.Attributes) (authorized authorizer.Decision, reason string, err error) {
	info, ok := request.ExtraReqeustInfoFrom(ctx)
	if !ok {
		return a.decisionOnError, "", fmt.Errorf("failed to get request host from context")
	}
	host := info.Hostname

	cluster, client, err := a.clientProvider.ClientFor(host)
	if err != nil {
		return a.decisionOnError, "", err
	}

	c, loaded := a.caches.Load(host)
	if !loaded {
		c, loaded = a.caches.LoadOrStore(host, cache.NewLRUExpireCache(8192))
		// destry cache when cluster stopped
		if !loaded {
			go func() {
				<-cluster.Context().Done()
				a.caches.Delete(host)
			}()
		}
	}
	cache := c.(*cache.LRUExpireCache)

	r := a.subjectAccessReviewFromAttributes(attr)
	key, err := json.Marshal(r.Spec)
	if err != nil {
		return a.decisionOnError, "", err
	}

	if entry, ok := cache.Get(string(key)); ok {
		r.Status = entry.(authorizationv1.SubjectAccessReviewStatus)
	} else {
		var result *authorizationv1.SubjectAccessReview
		err := webhook.WithExponentialBackoff(ctx, a.initialBackoff, func() error {
			var nerr error
			result, nerr = client.AuthorizationV1().SubjectAccessReviews().Create(ctx, r, metav1.CreateOptions{})
			return nerr
		}, webhook.DefaultShouldRetry)
		if err != nil {
			// An error here indicates bad configuration or an outage. Log for debugging.
			klog.Errorf("Failed to make webhook authorizer request: %v", err)
			return a.decisionOnError, "", err
		}
		r.Status = result.Status
		if shouldCache(attr) {
			if r.Status.Allowed {
				cache.Add(string(key), r.Status, a.allowCacheTTL)
			} else {
				cache.Add(string(key), r.Status, a.denyCacheTTL)
			}
		}
	}
	switch {
	case r.Status.Denied && r.Status.Allowed:
		return authorizer.DecisionDeny, r.Status.Reason, fmt.Errorf("webhook subject access review returned both allow and deny response")
	case r.Status.Denied:
		return authorizer.DecisionDeny, r.Status.Reason, nil
	case r.Status.Allowed:
		return authorizer.DecisionAllow, r.Status.Reason, nil
	default:
		return authorizer.DecisionNoOpinion, r.Status.Reason, nil
	}

}

func (a *MultiClusterSubjectAccessReviewAuthorizer) subjectAccessReviewFromAttributes(attr authorizer.Attributes) *authorizationv1.SubjectAccessReview {
	r := &authorizationv1.SubjectAccessReview{}
	if user := attr.GetUser(); user != nil {
		r.Spec = authorizationv1.SubjectAccessReviewSpec{
			User:   user.GetName(),
			UID:    user.GetUID(),
			Groups: user.GetGroups(),
			Extra:  convertToSARExtra(user.GetExtra()),
		}
	}

	if attr.IsResourceRequest() {
		r.Spec.ResourceAttributes = &authorizationv1.ResourceAttributes{
			Namespace:   attr.GetNamespace(),
			Verb:        attr.GetVerb(),
			Group:       attr.GetAPIGroup(),
			Version:     attr.GetAPIVersion(),
			Resource:    attr.GetResource(),
			Subresource: attr.GetSubresource(),
			Name:        attr.GetName(),
		}
	} else {
		r.Spec.NonResourceAttributes = &authorizationv1.NonResourceAttributes{
			Path: attr.GetPath(),
			Verb: attr.GetVerb(),
		}
	}
	return r
}

func convertToSARExtra(extra map[string][]string) map[string]authorizationv1.ExtraValue {
	if extra == nil {
		return nil
	}
	ret := map[string]authorizationv1.ExtraValue{}
	for k, v := range extra {
		ret[k] = authorizationv1.ExtraValue(v)
	}

	return ret
}

// shouldCache determines whether it is safe to cache the given request attributes. If the
// requester-controlled attributes are too large, this may be a DoS attempt, so we skip the cache.
func shouldCache(attr authorizer.Attributes) bool {
	controlledAttrSize := int64(len(attr.GetNamespace())) +
		int64(len(attr.GetVerb())) +
		int64(len(attr.GetAPIGroup())) +
		int64(len(attr.GetAPIVersion())) +
		int64(len(attr.GetResource())) +
		int64(len(attr.GetSubresource())) +
		int64(len(attr.GetName())) +
		int64(len(attr.GetPath()))
	return controlledAttrSize < maxControlledAttrCacheSize
}
