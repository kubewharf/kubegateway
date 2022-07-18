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

package modes

import (
	"k8s.io/apimachinery/pkg/util/sets"
	authzmodes "k8s.io/kubernetes/pkg/kubeapiserver/authorizer/modes"
)

const (
	// ModeAlwaysAllow is the mode to set all requests as authorized
	ModeAlwaysAllow string = authzmodes.ModeAlwaysAllow
	// ModeAlwaysDeny is the mode to set no requests as authorized
	ModeAlwaysDeny string = authzmodes.ModeAlwaysDeny
	// ModeWebhook is the mode to make an external webhook call to authorize
	ModeWebhook string = authzmodes.ModeWebhook
	// ModeRBAC is the mode to use Role Based Access Control to authorize
	ModeRBAC string = authzmodes.ModeRBAC
)

// AuthorizationModeChoices is the list of supported authorization modes
var AuthorizationModeChoices = []string{ModeAlwaysAllow, ModeAlwaysDeny, ModeWebhook, ModeRBAC}

// IsValidAuthorizationMode returns true if the given authorization mode is a valid one for the apiserver
func IsValidAuthorizationMode(authzMode string) bool {
	return sets.NewString(AuthorizationModeChoices...).Has(authzMode)
}
