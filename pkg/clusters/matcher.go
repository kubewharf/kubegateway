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

package clusters

import (
	"k8s.io/apiserver/pkg/authorization/authorizer"

	proxyv1alpha1 "github.com/kubewharf/kubegateway/pkg/apis/proxy/v1alpha1"
)

func MatchPolicies(requestAttributes authorizer.Attributes, policies []proxyv1alpha1.DispatchPolicy) *proxyv1alpha1.DispatchPolicy {
	for i := range policies {
		if PolicyMatches(requestAttributes, &policies[i]) {
			return &policies[i]
		}
	}
	return nil
}

func PolicyMatches(requestAttributes authorizer.Attributes, policy *proxyv1alpha1.DispatchPolicy) bool {
	for i := range policy.Rules {
		if RuleMatches(requestAttributes, &policy.Rules[i]) {
			return true
		}
	}
	return false
}

func RuleMatches(requestAttributes authorizer.Attributes, rule *proxyv1alpha1.DispatchPolicyRule) bool {
	basicMatch := proxyv1alpha1.VerbMatches(rule.Verbs, requestAttributes.GetVerb()) &&
		proxyv1alpha1.UserOrServiceAccountMatches(rule.Users, rule.ServiceAccounts, requestAttributes.GetUser().GetName()) &&
		proxyv1alpha1.UserGroupMatches(rule.UserGroups, requestAttributes.GetUser().GetGroups())

	if !basicMatch {
		return false
	}

	if requestAttributes.IsResourceRequest() {
		combinedResource := requestAttributes.GetResource()
		if len(requestAttributes.GetSubresource()) > 0 {
			combinedResource = requestAttributes.GetResource() + "/" + requestAttributes.GetSubresource()
		}
		return proxyv1alpha1.APIGroupMatches(rule.APIGroups, requestAttributes.GetAPIGroup()) &&
			proxyv1alpha1.ResourceMatches(rule.Resources, combinedResource, requestAttributes.GetSubresource()) &&
			proxyv1alpha1.ResourceNameMatches(rule.ResourceNames, requestAttributes.GetName())
	}
	return proxyv1alpha1.NonResourceURLMatches(rule.NonResourceURLs, requestAttributes.GetPath())
}
