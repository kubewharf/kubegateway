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

package authenticator

import (
	"reflect"
	"testing"

	"k8s.io/apiserver/pkg/server/dynamiccertificates"
	kubeauthenticator "k8s.io/kubernetes/pkg/kubeapiserver/authenticator"
)

func TestConfig_GetClientCert(t *testing.T) {
	type fields struct {
		config kubeauthenticator.Config
	}
	tests := []struct {
		name   string
		fields fields
		want   *ClientCertAuthenticationConfig
	}{
		{
			"",
			fields{
				config: kubeauthenticator.Config{
					ClientCAContentProvider: &dynamiccertificates.DynamicFileCAContent{},
				},
			},
			&ClientCertAuthenticationConfig{
				CAContentProvider: &dynamiccertificates.DynamicFileCAContent{},
			},
		},
		{
			"nil",
			fields{
				config: kubeauthenticator.Config{},
			},
			nil,
		},
	}
	for i := range tests {
		tt := tests[i]
		t.Run(tt.name, func(t *testing.T) {
			config := Config{
				config: tt.fields.config,
			}
			if got := config.GetClientCert(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Config.GetClientCert() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConfig_GetBootstrapTokenConfig(t *testing.T) {
	type fields struct {
		config kubeauthenticator.Config
	}
	tests := []struct {
		name   string
		fields fields
		want   *BootstrapTokenAuthenticationConfig
	}{
		{
			"nil",
			fields{
				config: kubeauthenticator.Config{},
			},
			nil,
		},
		{
			"",
			fields{
				config: kubeauthenticator.Config{
					BootstrapToken: true,
				},
			},
			&BootstrapTokenAuthenticationConfig{
				TokenAuthenticator: nil,
			},
		},
	}
	for i := range tests {
		tt := tests[i]
		t.Run(tt.name, func(t *testing.T) {
			config := Config{
				config: tt.fields.config,
			}
			if got := config.GetBootstrapTokenConfig(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Config.GetBootstrapTokenConfig() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConfig_GetServiceAccounts(t *testing.T) {
	type fields struct {
		config kubeauthenticator.Config
	}
	tests := []struct {
		name   string
		fields fields
		want   *ServiceAccountAuthenticationConfig
	}{
		{
			"nil",
			fields{
				config: kubeauthenticator.Config{},
			},
			nil,
		},
		{
			"",
			fields{
				config: kubeauthenticator.Config{
					ServiceAccountKeyFiles: []string{"xxxx"},
					ServiceAccountLookup:   true,
					ServiceAccountIssuer:   "xxxx",
				},
			},
			&ServiceAccountAuthenticationConfig{
				KeyFiles: []string{"xxxx"},
				Lookup:   true,
				Issuer:   "xxxx",
			},
		},
	}
	for i := range tests {
		tt := tests[i]
		t.Run(tt.name, func(t *testing.T) {
			config := Config{
				config: tt.fields.config,
			}
			if got := config.GetServiceAccounts(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Config.GetServiceAccounts() = %v, want %v", got, tt.want)
			}
		})
	}
}
