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

package apihelpers

import "testing"

func TestIsProtectedGroup(t *testing.T) {
	tests := []struct {
		group string
		want  bool
	}{
		{
			"",
			true,
		},
		{
			"k8s.io",
			true,
		},
		{
			"xx.k8s.io",
			true,
		},
		{
			"kubernetes.io",
			true,
		},
		{
			"xx.kubernetes.io",
			true,
		},
		{
			"xxk8s.io",
			false,
		},
		{
			"xxkubernetes.io",
			false,
		},
	}
	for i := range tests {
		tt := tests[i]
		t.Run(tt.group, func(t *testing.T) {
			if got := IsProtectedGroup(tt.group); got != tt.want {
				t.Errorf("IsProtectedGroup() = %v, want %v", got, tt.want)
			}
		})
	}
}
