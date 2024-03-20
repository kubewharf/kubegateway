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

	authorizermodes "github.com/kubewharf/apiserver-runtime/pkg/server/authorizer/modes"
	"github.com/spf13/pflag"
	genericserver "k8s.io/apiserver/pkg/server"
	"k8s.io/client-go/informers"
	kubeauthorizer "k8s.io/kubernetes/pkg/kubeapiserver/authorizer"
	"k8s.io/kubernetes/pkg/kubeapiserver/options"
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
	return o.BuiltInAuthorizationOptions.ToAuthorizationConfig(versionedInformerFactory)
}

func (o *AuthorizationOptions) AddFlags(fs *pflag.FlagSet) {
	fs.StringSliceVar(&o.Modes, "authorization-mode", o.Modes, ""+
		"Ordered list of plug-ins to do authorization on secure port. Comma-delimited list of: "+
		strings.Join(authorizermodes.AuthorizationModeChoices, ",")+".")
}

func (o *AuthorizationOptions) Validate() []error {
	return o.BuiltInAuthorizationOptions.Validate()
}

func (o *AuthorizationOptions) ApplyTo(
	c *genericserver.AuthorizationInfo,
	versionedInformerFactory informers.SharedInformerFactory,
) error {
	cfg := o.ToAuthorizationConfig(versionedInformerFactory)
	authorizer, _, err := cfg.New()
	if err != nil {
		return err
	}
	c.Authorizer = authorizer
	return nil
}
