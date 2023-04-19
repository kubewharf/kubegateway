// Copyright 2022 ByteDance and its affiliates.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package features

import (
	"testing"

	"k8s.io/component-base/featuregate"
)

func TestIsDefault(t *testing.T) {
	tests := []struct {
		name string
		fg   featuregate.FeatureGate
		want bool
	}{
		{
			name: "deepcopy",
			fg:   DefaultFeatureGate.DeepCopy(),
			want: true,
		},
		{
			name: "empty",
			fg:   featuregate.NewFeatureGate(),
			want: false,
		},
		{
			name: "feature value changed",
			fg: func() featuregate.FeatureGate {
				fg := DefaultFeatureGate.DeepCopy()
				fg.Set("DenyAllRequests=true")
				return fg
			}(),
			want: false,
		},
		{
			name: "feature key changed",
			fg: func() featuregate.FeatureGate {
				fg := featuregate.NewFeatureGate()
				fg.Add(map[featuregate.Feature]featuregate.FeatureSpec{
					"Test1": {Default: false, PreRelease: featuregate.Alpha},
					"Test2": {Default: false, PreRelease: featuregate.Alpha},
				})
				return fg
			}(),
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsDefault(tt.fg); got != tt.want {
				t.Errorf("IsDefault() = %v, want %v", got, tt.want)
			}
		})
	}
}
