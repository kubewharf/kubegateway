/*
Copyright 2022 ByteDance and its affiliates.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!

// SetDefaults_UpstreamCluster set additional defaults compared to its counterpart
// nolint
func SetDefaults_UpstreamCluster(obj *UpstreamCluster) {
	for i := range obj.Spec.DispatchPolicies {
		if len(obj.Spec.DispatchPolicies[i].Strategy) == 0 {
			obj.Spec.DispatchPolicies[i].Strategy = RoundRobin
		}
	}
}
