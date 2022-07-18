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

	genericoptions "k8s.io/apiserver/pkg/server/options"
)

func TestSecureServingOptions_Validate(t *testing.T) {
	type fields struct {
		SecureServingOptionsWithLoopback *genericoptions.SecureServingOptionsWithLoopback
		ReusePort                        bool
		OtherPorts                       []int
		LoopbackClientToken              string
	}
	tests := []struct {
		name    string
		fields  fields
		wantErr int
	}{
		{
			"reuse port without loopback client token",
			fields{
				SecureServingOptionsWithLoopback: genericoptions.NewSecureServingOptions().WithLoopback(),
				ReusePort:                        true,
				LoopbackClientToken:              "",
			},
			1,
		},
		{
			"duplicate ports",
			fields{
				SecureServingOptionsWithLoopback: genericoptions.NewSecureServingOptions().WithLoopback(),
				OtherPorts:                       []int{443, 2, 2},
			},
			2,
		},
		{
			"invalid other ports",
			fields{
				SecureServingOptionsWithLoopback: genericoptions.NewSecureServingOptions().WithLoopback(),
				OtherPorts:                       []int{0, 1000000},
			},
			2,
		},
	}
	for i := range tests {
		tt := tests[i]
		t.Run(tt.name, func(t *testing.T) {
			s := &SecureServingOptions{
				SecureServingOptionsWithLoopback: tt.fields.SecureServingOptionsWithLoopback,
				ReusePort:                        tt.fields.ReusePort,
				OtherPorts:                       tt.fields.OtherPorts,
				LoopbackClientToken:              tt.fields.LoopbackClientToken,
			}
			if got := s.Validate(); len(got) != tt.wantErr {
				t.Errorf("SecureServingOptions.Validate() = %v, want %v", got, tt.wantErr)
			}
		})
	}
}
