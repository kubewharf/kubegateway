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
	"testing"

	"k8s.io/apiserver/pkg/authentication/user"
	"k8s.io/apiserver/pkg/authorization/authorizer"

	proxyv1alpha1 "github.com/kubewharf/kubegateway/pkg/apis/proxy/v1alpha1"
)

func TestRuleMatches(t *testing.T) {
	tests := []struct {
		name              string
		requestAttributes authorizer.Attributes
		rule              []*proxyv1alpha1.DispatchPolicyRule
		want              []bool
	}{
		{
			"non resource urls",
			authorizer.AttributesRecord{
				Verb: "get",
				Path: "/healthz",
				User: &user.DefaultInfo{
					Name: "test",
				},
			},
			[]*proxyv1alpha1.DispatchPolicyRule{
				{
					Verbs: []string{"get"},
					NonResourceURLs: []string{
						"/healthz",
					},
					Users: []string{
						"test",
					},
				},
				{
					Verbs: []string{"get"},
					NonResourceURLs: []string{
						"/healthz",
					},
					Users: []string{
						"test2",
					},
				},
				{
					Verbs: []string{"get"},
					NonResourceURLs: []string{
						"/health",
					},
					Users: []string{
						"test",
					},
				},
			},
			[]bool{
				true,
				false,
				false,
			},
		},

		{
			"resource urls",
			authorizer.AttributesRecord{
				Verb:     "get",
				APIGroup: "apps",
				Resource: "deployments",
				User: &user.DefaultInfo{
					Name: "test",
				},
				ResourceRequest: true,
			},
			[]*proxyv1alpha1.DispatchPolicyRule{
				{
					Verbs: []string{"get"},
					APIGroups: []string{
						"apps",
					},
					Resources: []string{
						"deployments",
					},
					Users: []string{
						"test",
					},
				},
				{
					Verbs: []string{"get"},
					APIGroups: []string{
						"extensions",
					},
					Resources: []string{
						"deployments",
					},
					Users: []string{
						"test",
					},
				},
				{
					Verbs: []string{"get"},
					APIGroups: []string{
						"apps",
					},
					Resources: []string{
						"statefulsets",
					},
					Users: []string{
						"test",
					},
				},
			},
			[]bool{
				true,
				false,
				false,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for i, rule := range tt.rule {
				if got := RuleMatches(tt.requestAttributes, rule); got != tt.want[i] {
					t.Errorf("RuleMatches() = %v, want %v, index=%v", got, tt.want[i], i)
				}
			}
		})
	}
}
