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

package upstreamclusteradmission

import (
	"reflect"
	"testing"

	proxy "github.com/kubewharf/kubegateway/pkg/apis/proxy/v1alpha1"
)

func Test_normalizeRules(t *testing.T) {
	tests := []struct {
		name string
		in   proxy.DispatchPolicyRule
		want proxy.DispatchPolicyRule
	}{
		{
			in: proxy.DispatchPolicyRule{
				Verbs:         []string{"*", "get", "-delete"},
				APIGroups:     []string{"-apps", "rbac"},
				Resources:     []string{"-deployments", "-statefulsets"},
				ResourceNames: []string{"-apps", "rbac"},
				Users:         []string{"-apps", "rbac"},
				ServiceAccounts: []proxy.ServiceAccountRef{
					{
						Namespace: "default",
						Name:      "default",
					},
				},
				UserGroups:      []string{"-apps", "rbac"},
				NonResourceURLs: []string{"-apps", "rbac"},
			},
			want: proxy.DispatchPolicyRule{
				Verbs:         []string{"*"},
				APIGroups:     []string{"rbac"},
				Resources:     []string{"-deployments", "-statefulsets"},
				ResourceNames: []string{"rbac"},
				Users:         []string{"rbac"},
				ServiceAccounts: []proxy.ServiceAccountRef{
					{
						Namespace: "default",
						Name:      "default",
					},
				},
				UserGroups:      []string{"rbac"},
				NonResourceURLs: []string{"rbac"},
			},
		},
	}
	for i := range tests {
		tt := tests[i]
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeRules(tt.in); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("normalizeRules() = %v, want %v", got, tt.want)
			}
		})
	}
}
