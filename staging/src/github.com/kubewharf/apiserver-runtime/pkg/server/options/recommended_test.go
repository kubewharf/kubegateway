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
	"testing"
)

func TestRecommendedOptions_WithAll(t *testing.T) {
	o := NewRecommendedOptions().WithAll()
	if o.APIEnablement == nil {
		t.Errorf("RecommendedOptions.WithAll() missing APIEnablement")
	}
	if o.Admission == nil {
		t.Errorf("RecommendedOptions.WithAll() missing Admission")
	}
	if o.Audit == nil {
		t.Errorf("RecommendedOptions.WithAll() missing Audit")
	}
	if o.Authentication == nil {
		t.Errorf("RecommendedOptions.WithAll() missing Authentication")
	}
	if o.Authorization == nil {
		t.Errorf("RecommendedOptions.WithAll() missing Authorization")
	}
	if o.BackendAPI == nil {
		t.Errorf("RecommendedOptions.WithAll() missing BackendAPI")
	}
	if o.Etcd == nil {
		t.Errorf("RecommendedOptions.WithAll() missing Etcd")
	}
	if o.EgressSelector == nil {
		t.Errorf("RecommendedOptions.WithAll() missing EgressSelector")
	}
	if o.Features == nil {
		t.Errorf("RecommendedOptions.WithAll() missing Features")
	}
	if o.FeatureGate == nil {
		t.Errorf("RecommendedOptions.WithAll() missing FeatureGate")
	}
	if o.ProcessInfo == nil {
		t.Errorf("RecommendedOptions.WithAll() missing ProcessInfo")
	}
	if o.ServerRun == nil {
		t.Errorf("RecommendedOptions.WithAll() missing ServerRun")
	}
	if o.SecureServing == nil {
		t.Errorf("RecommendedOptions.WithAll() missing SecureServing")
	}
	if o.Webhook == nil {
		t.Errorf("RecommendedOptions.WithAll() missing Webhook")
	}
}

func TestRecommendedOptions_Flags(t *testing.T) {
	o := NewRecommendedOptions().WithAll()
	fss := o.Flags()

	wantFss := 11
	got := len(fss.FlagSets)
	if got != wantFss {
		t.Errorf("RecommendedOptions.WithAll.Flags() len=%v, want=%v", got, wantFss)
	}
}
