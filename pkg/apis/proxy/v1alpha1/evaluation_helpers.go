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

package v1alpha1

import (
	"strings"
)

const (
	ServiceAccountUsernamePrefix    = "system:serviceaccount:"
	ServiceAccountUsernameSeparator = ":"
)

// MakeServiceAccountUsername generates a username from the given namespace and ServiceAccount name.
// The resulting username can be passed to SplitUsername to extract the original namespace and ServiceAccount name.
func MakeServiceAccountUsername(namespace, name string) string {
	return ServiceAccountUsernamePrefix + namespace + ServiceAccountUsernameSeparator + name
}

func VerbMatches(verbs []string, request string) bool {
	return simpleMatches(verbs, []string{request})
}

func APIGroupMatches(apiGroups []string, request string) bool {
	return simpleMatches(apiGroups, []string{request})
}

func ResourceMatches(resources []string, combinedRequestedResource, requestedSubresource string) bool {
	return simpleMatches(resources, []string{combinedRequestedResource}, func(m matcher) bool {
		// We can also match a */subresource.
		// if there isn't a subresource, then continue
		if len(requestedSubresource) == 0 {
			// continue
			return false
		}

		// if the rule isn't in the format */subresource, then we don't match, continue
		if strings.HasPrefix(m.value, "*/") {
			allSubresource := "*/" + requestedSubresource
			if (!m.reverse && m.value == allSubresource) ||
				(m.reverse && m.value != allSubresource) {
				return true
			}
		}
		return false
	})
}

func ResourceNameMatches(resourceNames []string, request string) bool {
	if len(resourceNames) == 0 {
		// matche all names
		return true
	}
	return simpleMatches(resourceNames, []string{request})
}

func UserOrServiceAccountMatches(users []string, serviceAccounts []ServiceAccountRef, requestUser string) bool {
	if len(users) == 0 && len(serviceAccounts) == 0 {
		return true
	}

	if simpleMatches(users, []string{requestUser}) {
		return true
	}

	for _, sa := range serviceAccounts {
		if len(sa.Namespace) == 0 || len(sa.Name) == 0 {
			continue
		}
		if MakeServiceAccountUsername(sa.Namespace, sa.Name) == requestUser {
			return true
		}
	}
	return false
}

func UserGroupMatches(userGroups []string, requestGroups []string) bool {
	if len(userGroups) == 0 {
		return true
	}
	return simpleMatches(userGroups, requestGroups)
}

func NonResourceURLMatches(nonResourceURLs []string, request string) bool {
	filtered, matchAll := filterRules(nonResourceURLs)
	if matchAll {
		return true
	}

	for _, v := range filtered {
		if v.reverse {
			// ignore reversed rules
			continue
		}
		if v.match(request) {
			return true
		}
		if strings.HasSuffix(v.value, "*") && strings.HasPrefix(request, strings.TrimRight(v.value, "*")) {
			return true
		}
	}
	return false
}

func simpleMatches(rules []string, requests []string, matchFn ...func(m matcher) bool) bool {
	filtered, matchAll := filterRules(rules)
	if matchAll {
		return true
	}

	for _, v := range filtered {
		for _, request := range requests {
			if v.match(request) {
				return true
			}
		}
		for _, match := range matchFn {
			if match(v) {
				return true
			}
		}
	}
	return false
}

type matcher struct {
	reverse bool
	value   string
}

func (m matcher) match(request string) bool {
	if m.reverse {
		return m.value != request
	}
	return m.value == request
}

func filterRules(rules []string) (filtered []matcher, matchAll bool) {
	reversed := []matcher{}
	for _, r := range rules {
		if r == MatchAll {
			matchAll = true
			break
		}
		// legecy group will be ""
		if len(r) > 0 && r[0] == '-' {
			reversed = append(reversed, matcher{true, r[1:]})
		} else {
			filtered = append(filtered, matcher{false, r})
		}
	}
	if len(filtered) > 0 {
		// if filtered is not empty drop reversed
		return
	}
	filtered = reversed
	return
}
