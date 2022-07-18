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

import "github.com/spf13/pflag"

type LoggingOptions struct {
	EnableProxyAccessLog bool
}

func NewLoggingOptions() *LoggingOptions {
	return &LoggingOptions{
		EnableProxyAccessLog: false,
	}
}

func (o *LoggingOptions) Validate() []error {
	return nil
}

func (o *LoggingOptions) AddFlags(fs *pflag.FlagSet) {
	fs.BoolVar(&o.EnableProxyAccessLog, "enable-proxy-access-log", o.EnableProxyAccessLog, "Enable proxy access log")
}
