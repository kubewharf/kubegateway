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
	"testing"
)

func TestVerbMatches(t *testing.T) {
	tests := []struct {
		name    string
		rule    []string
		request string
		want    bool
	}{
		{
			"empty matches nothing",
			[]string{},
			"core",
			false,
		},
		{
			"match all",
			[]string{"*"},
			"get",
			true,
		},
		{
			"match directly",
			[]string{"get"},
			"get",
			true,
		},
		{
			"do not match invertly",
			[]string{"-get"},
			"get",
			false,
		},
		{
			"match invertly",
			[]string{"-get"},
			"create",
			true,
		},
	}
	for i := range tests {
		tt := tests[i]
		t.Run(tt.name, func(t *testing.T) {
			if got := VerbMatches(tt.rule, tt.request); got != tt.want {
				t.Errorf("VerbMatches() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAPIGroupMatches(t *testing.T) {
	tests := []struct {
		name    string
		rule    []string
		request string
		want    bool
	}{
		{
			"empty matches nothing",
			[]string{},
			"apps",
			false,
		},
		{
			"match all",
			[]string{"*"},
			"apps",
			true,
		},
		{
			"match directly",
			[]string{"apps"},
			"apps",
			true,
		},
		{
			"do not match invertly",
			[]string{"-apps"},
			"apps",
			false,
		},
		{
			"match invertly",
			[]string{"-apps"},
			"batch",
			true,
		},
	}
	for i := range tests {
		tt := tests[i]
		t.Run(tt.name, func(t *testing.T) {
			if got := APIGroupMatches(tt.rule, tt.request); got != tt.want {
				t.Errorf("APIGroups() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestResourceMatches(t *testing.T) {
	type args struct {
		resources                 []string
		combinedRequestedResource string
		requestedSubresource      string
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			"",
			args{
				[]string{},
				"deployments",
				"",
			},
			false,
		},
		{
			"match all",
			args{
				[]string{"deployments"},
				"deployments",
				"",
			},
			true,
		},
		{
			"do not match invertly",
			args{
				[]string{"-deployments"},
				"deployments",
				"",
			},
			false,
		},
		{
			"match invertly",
			args{
				[]string{"-deployments"},
				"statefulsets",
				"",
			},
			true,
		},
		{
			"do not match subresource",
			args{
				[]string{"deployments/status"},
				"deployments",
				"",
			},
			false,
		},
		{
			"match subresource",
			args{
				[]string{"deployments/status"},
				"deployments/status",
				"status",
			},
			true,
		},
		{
			"do not match resource's all subresource",
			args{
				[]string{"deployments/*"},
				"deployments/status",
				"status",
			},
			false,
		},
		{
			"match all subresource",
			args{
				[]string{"*/status"},
				"deployments/status",
				"status",
			},
			true,
		},
	}
	for i := range tests {
		tt := tests[i]
		t.Run(tt.name, func(t *testing.T) {
			if got := ResourceMatches(tt.args.resources, tt.args.combinedRequestedResource, tt.args.requestedSubresource); got != tt.want {
				t.Errorf("ResourceMatches() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestResourceNameMatches(t *testing.T) {
	tests := []struct {
		name    string
		rule    []string
		request string
		want    bool
	}{
		{
			"empty matches everything",
			[]string{},
			"xxxx",
			true,
		},
		{
			"match all",
			[]string{"*"},
			"apps",
			true,
		},
	}
	for i := range tests {
		tt := tests[i]
		t.Run(tt.name, func(t *testing.T) {
			if got := ResourceNameMatches(tt.rule, tt.request); got != tt.want {
				t.Errorf("ResourceNameMatches() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestUserOrServiceAccountMatches(t *testing.T) {
	type args struct {
		users           []string
		serviceAccounts []ServiceAccountRef
		requestUser     string
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			"empty matches everything",
			args{
				nil,
				nil,
				"xxx",
			},
			true,
		},
		{
			"match user",
			args{
				[]string{"user"},
				nil,
				"user",
			},
			true,
		},
		{
			"match service account",
			args{
				[]string{},
				[]ServiceAccountRef{
					{
						Namespace: "default",
						Name:      "default",
					},
				},
				MakeServiceAccountUsername("default", "default"),
			},
			true,
		},
	}
	for i := range tests {
		tt := tests[i]
		t.Run(tt.name, func(t *testing.T) {
			if got := UserOrServiceAccountMatches(tt.args.users, tt.args.serviceAccounts, tt.args.requestUser); got != tt.want {
				t.Errorf("UserOrServiceAccountMatches() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestUserGroupMatches(t *testing.T) {
	tests := []struct {
		name    string
		rule    []string
		request []string
		want    bool
	}{
		{
			"empty matches everything",
			[]string{},
			[]string{"xxxx"},
			true,
		},
		{
			"match all",
			[]string{"*"},
			[]string{"apps"},
			true,
		},
	}
	for i := range tests {
		tt := tests[i]
		t.Run(tt.name, func(t *testing.T) {
			if got := UserGroupMatches(tt.rule, tt.request); got != tt.want {
				t.Errorf("UserGroupMatches() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNonResourceURLMatches(t *testing.T) {
	type args struct {
		nonResourceURLs []string
		request         string
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			"empty means nothing",
			args{
				nil,
				"xxx",
			},
			false,
		},
		{
			"match all",
			args{
				[]string{"*"},
				"/xxx",
			},
			true,
		},
		{
			"ignore invert rules",
			args{
				[]string{"-/healthz", "/healthz"},
				"/healthz",
			},
			true,
		},
		{
			"matches suffix",
			args{
				[]string{"/healthz*"},
				"/healthz/xxx",
			},
			true,
		},
	}
	for i := range tests {
		tt := tests[i]
		t.Run(tt.name, func(t *testing.T) {
			if got := NonResourceURLMatches(tt.args.nonResourceURLs, tt.args.request); got != tt.want {
				t.Errorf("NonResourceURLMatches() = %v, want %v", got, tt.want)
			}
		})
	}
}
