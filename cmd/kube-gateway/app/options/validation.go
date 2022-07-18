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

package options

import (
	"github.com/kubewharf/apiserver-runtime/pkg/scheme"
	apiextensionsapiserver "k8s.io/apiextensions-apiserver/pkg/apiserver"
	"k8s.io/component-base/metrics"
	aggregatorscheme "k8s.io/kube-aggregator/pkg/apiserver/scheme"
)

func (s *ControlPlaneServerRunOptions) Validate() []error {
	var errs []error
	errs = append(errs, s.ControlPlaneOptions.Validate()...)
	errs = append(errs, s.Authentication.Validate()...)
	errs = append(errs, s.Authorization.Validate()...)
	errs = append(errs, s.APIEnablement.Validate(scheme.Scheme, apiextensionsapiserver.Scheme, aggregatorscheme.Scheme)...)
	errs = append(errs, metrics.ValidateShowHiddenMetricsVersion(s.ShowHiddenMetricsForVersion)...)

	return errs
}

func (o *ProxyOptions) Validate(controlplane *ControlPlaneServerRunOptions) []error {
	var errs []error
	errs = append(errs, o.Authentication.Validate()...)
	errs = append(errs, o.Authorization.Validate()...)
	errs = append(errs, o.SecureServing.ValidateWith(*controlplane.SecureServing)...)
	return errs
}

func (o *Options) Validate() []error {
	var errs []error
	errs = append(errs, o.ControlPlane.Validate()...)
	errs = append(errs, o.Proxy.Validate(o.ControlPlane)...)
	return errs
}
