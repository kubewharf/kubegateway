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
)

func TestEndpointInfo_ReadyAndReason(t *testing.T) {
	tests := []struct {
		name      string
		status    *endpointStatus
		wantReady bool
		want      string
	}{
		{
			"ready",
			&endpointStatus{
				Disabled: false,
				Healthy:  true,
			},
			true,
			"",
		},
		{
			"disabled",
			&endpointStatus{
				Disabled: true,
				Healthy:  true,
			},
			false,
			`endpoint="" is disabled.`,
		},
		{
			"unhealthy",
			&endpointStatus{
				Disabled: false,
				Healthy:  false,
				Reason:   "Timeout",
				Message:  "request timeout",
			},
			false,
			`endpoint="" is unhealthy, reason="Timeout", message="request timeout".`,
		},
	}
	for i := range tests {
		tt := tests[i]
		t.Run(tt.name, func(t *testing.T) {
			e := &EndpointInfo{
				Endpoint: "",
				status:   tt.status,
			}
			if got := e.IsReady(); got != tt.wantReady {
				t.Errorf("EndpointInfo.IsReady() = %v, want %v", got, tt.wantReady)
			}
			if got := e.UnreadyReason(); got != tt.want {
				t.Errorf("EndpointInfo.UnreadyReason() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEndpointInfo_UnhealthyCount(t *testing.T) {
	e := &EndpointInfo{
		Endpoint: "",
		status: &endpointStatus{
			Disabled: true,
			Healthy:  true,
		},
	}
	if e.GetUnhealthyCount() != 0 {
		t.Errorf("unhealthy count should be 0, actual: %d", e.GetUnhealthyCount())
	}

	for i := 0; i < 2; i++ {
		e.UpdateStatus(false, "mock error", "mock error message")
		if e.GetUnhealthyCount() != i+1 {
			t.Errorf("unhealthy count should be %d, actual: %d", i+1, e.GetUnhealthyCount())
		}
	}
	e.UpdateStatus(true, "", "")
	if e.GetUnhealthyCount() != 0 {
		t.Errorf("unhealthy count should be 0, actual: %d", e.GetUnhealthyCount())
	}

	for i := 0; i < 5; i++ {
		e.UpdateStatus(false, "mock error", "mock error message")
		if e.GetUnhealthyCount() != i+1 {
			t.Errorf("unhealthy count should be %d, actual: %d", i+1, e.GetUnhealthyCount())
		}
	}
}
