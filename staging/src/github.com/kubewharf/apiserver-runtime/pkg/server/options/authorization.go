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
	"strings"

	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/util/sets"
	genericserver "k8s.io/apiserver/pkg/server"
	"k8s.io/client-go/informers"
	kubeauthorizer "k8s.io/kubernetes/pkg/kubeapiserver/authorizer"
	"k8s.io/kubernetes/pkg/kubeapiserver/options"
	rbacrest "k8s.io/kubernetes/pkg/registry/rbac/rest"

	authorizermodes "github.com/kubewharf/apiserver-runtime/pkg/server/authorizer/modes"
)

// AuthorizationOptions wraps BuiltInAuthorizationOptions and mask some unused options
// It only use subset of master's authorization builtin options
type AuthorizationOptions struct {
	*options.BuiltInAuthorizationOptions
}

func NewAuthorizationOptions() *AuthorizationOptions {
	o := &AuthorizationOptions{
		BuiltInAuthorizationOptions: options.NewBuiltInAuthorizationOptions(),
	}
	o.WebhookVersion = "v1"
	return o
}

func (o *AuthorizationOptions) ToAuthorizationConfig(
	versionedInformerFactory informers.SharedInformerFactory,
) kubeauthorizer.Config {
	// disable abac config
	o.PolicyFile = ""

	return o.BuiltInAuthorizationOptions.ToAuthorizationConfig(versionedInformerFactory)
}

func (o *AuthorizationOptions) AddFlags(fs *pflag.FlagSet) {
	o.BuiltInAuthorizationOptions.AddFlags(fs)
	// change desciption
	fs.Lookup("authorization-mode").Usage = "Ordered list of plug-ins to do authorization on secure port. Comma-delimited list of: " +
		strings.Join(authorizermodes.AuthorizationModeChoices, ",") + "."

	// hide not used flag
	fs.MarkHidden("authorization-policy-file") //nolint
}

func (o *AuthorizationOptions) ApplyTo(
	genericConfig *genericserver.Config,
	versionedInformerFactory informers.SharedInformerFactory,
) error {
	cfg := o.ToAuthorizationConfig(versionedInformerFactory)
	authorizer, ruleResolver, err := cfg.New()
	if err != nil {
		return err
	}
	genericConfig.Authorization.Authorizer = authorizer
	genericConfig.RuleResolver = ruleResolver

	if !sets.NewString(cfg.AuthorizationModes...).Has(authorizermodes.ModeRBAC) {
		genericConfig.DisabledPostStartHooks.Insert(rbacrest.PostStartHookName)
	}

	return nil
}
