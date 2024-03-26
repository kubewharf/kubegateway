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
	"github.com/spf13/pflag"
)

type TracingOptions struct {
	EnableProxyTracing bool
}

func NewTracingOptions() *TracingOptions {
	return &TracingOptions{
		EnableProxyTracing: false,
	}
}

func (o *TracingOptions) Validate() []error {
	return nil
}

func (o *TracingOptions) AddFlags(fs *pflag.FlagSet) {
	fs.BoolVar(&o.EnableProxyTracing, "enable-proxy-tracing", o.EnableProxyTracing, "Enable proxy tracing for local trace log and trace metric")
}
