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
	"reflect"
	"testing"
	"time"

	"github.com/kubewharf/apiserver-runtime/pkg/server/authorizer/modes"
	"k8s.io/kubernetes/pkg/kubeapiserver/options"
)

func TestNewAuthorizationOptions(t *testing.T) {
	tests := []struct {
		name string
		want *AuthorizationOptions
	}{
		{
			name: "",
			want: &AuthorizationOptions{
				BuiltInAuthorizationOptions: &options.BuiltInAuthorizationOptions{
					Modes:                       []string{modes.ModeAlwaysAllow},
					WebhookVersion:              "v1",
					WebhookCacheAuthorizedTTL:   5 * time.Minute,
					WebhookCacheUnauthorizedTTL: 30 * time.Second,
				},
			},
		},
	}
	for i := range tests {
		tt := tests[i]
		t.Run(tt.name, func(t *testing.T) {
			if got := NewAuthorizationOptions(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("NewAuthorizationOptions() = %v, want %v", got, tt.want)
			}
		})
	}
}
