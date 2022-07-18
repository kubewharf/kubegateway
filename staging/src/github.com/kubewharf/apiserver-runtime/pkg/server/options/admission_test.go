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

package options

import (
	"fmt"
	"testing"

	"k8s.io/apimachinery/pkg/util/sets"
)

func TestEnabledPluginNames(t *testing.T) {
	tests := []struct {
		expectedPluginNames       []string
		setDefaultOffPlugins      sets.String
		setRecommendedPluginOrder []string
		setEnablePlugins          []string
		setDisablePlugins         []string
		setAdmissionControl       []string
	}{
		// scenario 0: check if a call to enabledPluginNames sets expected values.
		{
			expectedPluginNames: []string{"NamespaceLifecycle"},
		},
	}

	// act
	for i := range tests {
		test := tests[i]
		t.Run(fmt.Sprintf("scenario %d", i), func(t *testing.T) {
			target := NewAdmissionOptions()

			if test.setDefaultOffPlugins != nil {
				target.DefaultOffPlugins = test.setDefaultOffPlugins
			}
			if test.setRecommendedPluginOrder != nil {
				target.RecommendedPluginOrder = test.setRecommendedPluginOrder
			}
			if test.setEnablePlugins != nil {
				target.EnablePlugins = test.setEnablePlugins
			}
			if test.setDisablePlugins != nil {
				target.DisablePlugins = test.setDisablePlugins
			}
			if test.setAdmissionControl != nil {
				target.EnablePlugins = test.setAdmissionControl
			}

			actualPluginNames := enabledPluginNames(target)

			if len(actualPluginNames) != len(test.expectedPluginNames) {
				t.Fatalf("incorrect number of items, got %d, expected = %d", len(actualPluginNames), len(test.expectedPluginNames))
			}
			for i := range actualPluginNames {
				if test.expectedPluginNames[i] != actualPluginNames[i] {
					t.Errorf("missmatch at index = %d, got = %s, expected = %s", i, actualPluginNames[i], test.expectedPluginNames[i])
				}
			}
		})
	}
}
