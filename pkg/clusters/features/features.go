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
	"strings"

	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/component-base/featuregate"
)

const (
	// Close connection if necessary
	CloseConnectionWhenIdle featuregate.Feature = "CloseConnectionWhenIdle"

	// Deny all reqeusts and make cluster temporary down
	DenyAllRequests featuregate.Feature = "DenyAllRequests"

	// GlobalRateLimiter enable remote limiter for proxy
	GlobalRateLimiter featuregate.Feature = "GlobalRateLimiter"
)

var (
	// DefaultMutableFeatureGate is a mutable version of DefaultFeatureGate.
	// Only top-level commands/options setup and the k8s.io/component-base/featuregate/testing package should make use of this.
	// Tests that need to modify feature gates for the duration of their test should use:
	//   defer featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.<FeatureName>, <value>)()
	DefaultMutableFeatureGate featuregate.MutableFeatureGate = featuregate.NewFeatureGate()

	// DefaultFeatureGate is a shared global FeatureGate.
	// Top-level commands/options setup that needs to modify this feature gate should use DefaultMutableFeatureGate.
	DefaultFeatureGate featuregate.FeatureGate = DefaultMutableFeatureGate

	// featureGate annotation key
	FeatureGateAnnotationKey = "proxy.kubegateway.io/feature-gates"
)

var (
	// defaultFeatureGates consists of all known feature keys.
	// To add a new feature, define a key for it above and add it here.
	defaultFeatureGates = map[featuregate.Feature]featuregate.FeatureSpec{
		CloseConnectionWhenIdle: {Default: false, PreRelease: featuregate.Alpha},
		DenyAllRequests:         {Default: false, PreRelease: featuregate.Alpha},
		GlobalRateLimiter:       {Default: false, PreRelease: featuregate.Alpha},
	}

	defaultKnownFeatures []string
)

func init() {
	runtime.Must(DefaultMutableFeatureGate.Add(defaultFeatureGates))
	defaultKnownFeatures = DefaultMutableFeatureGate.KnownFeatures()
}

func IsDefault(fg featuregate.FeatureGate) bool {
	// output features is already sorted
	inputFeatures := fg.KnownFeatures()
	if len(defaultKnownFeatures) != len(inputFeatures) {
		return false
	}

	for i, featureDesc := range defaultKnownFeatures {
		// feature is described as DenyAllRequests=true|false (ALPHA - default=false)
		if featureDesc != inputFeatures[i] {
			return false
		}

		token := strings.SplitN(featureDesc, "=", 2)
		key := featuregate.Feature(token[0])
		if DefaultFeatureGate.Enabled(key) != fg.Enabled(key) {
			return false
		}
	}
	return true
}
