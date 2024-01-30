// Copyright 2023 ByteDance and its affiliates.
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
	"fmt"

	"github.com/spf13/pflag"
)

type ServerRunOptions struct {
	MaxInflightThreshold     int32
	MaxQPSThreshold          int32
	MaxThroughputMBThreshold int32
	GoawayChance             float64
}

func NewServerRunOptions() *ServerRunOptions {
	return &ServerRunOptions{
		GoawayChance:         0,
		MaxInflightThreshold: 0,
	}
}

// Validate checks validation of ServerRunOptions
func (s *ServerRunOptions) Validate() []error {
	errors := []error{}

	if s.GoawayChance < 0 {
		errors = append(errors, fmt.Errorf("--proxy-goaway-chance can not be less than 0"))
	}

	if s.MaxInflightThreshold < 0 {
		errors = append(errors, fmt.Errorf("--proxy-max-inflight-threshold can not be less than 0"))
	}

	if s.MaxQPSThreshold < 0 {
		errors = append(errors, fmt.Errorf("--proxy-max-qps-threshold can not be less than 0"))
	}

	if s.MaxThroughputMBThreshold < 0 {
		errors = append(errors, fmt.Errorf("--proxy-max-throughput-mb-threshold can not be less than 0"))
	}

	return errors
}

// AddFlags adds flags to the specified FlagSet
func (s *ServerRunOptions) AddFlags(fs *pflag.FlagSet) {
	fs.Int32Var(&s.MaxInflightThreshold, "proxy-max-inflight-threshold", s.MaxInflightThreshold, ""+
		"The load pressure threshold of requests number in flight at a given time. When proxy has load pressure, "+
		"it randomly close a connection (GOAWAY) to prevent HTTP/2 clients from getting stuck on a single kube-gateway proxy")

	fs.Int32Var(&s.MaxQPSThreshold, "proxy-max-qps-threshold", s.MaxQPSThreshold, ""+
		"The load pressure threshold of requests qps. When proxy has load pressure, "+
		"it randomly close a connection (GOAWAY) to prevent HTTP/2 clients from getting stuck on a single kube-gateway proxy")

	fs.Int32Var(&s.MaxThroughputMBThreshold, "proxy-max-throughput-mb-threshold", s.MaxThroughputMBThreshold, ""+
		"The load pressure threshold of requests throughput im MB. When proxy has load pressure, "+
		"it randomly close a connection (GOAWAY) to prevent HTTP/2 clients from getting stuck on a single kube-gateway proxy")

	fs.Float64Var(&s.GoawayChance, "proxy-goaway-chance", s.GoawayChance, ""+
		"To prevent HTTP/2 clients from getting stuck on a single kube-gateway, randomly close a connection (GOAWAY) when proxy has load pressure. "+
		"The client's other in-flight requests won't be affected, and the client will reconnect, likely landing on a different kube-gateway after going through the load balancer again. "+
		"This argument sets the fraction of requests that will be sent a GOAWAY. Clusters with single kube-gateway should NOT enable this. "+
		"Min is 0 (off), 0.001 (1/1000) is a recommended starting point. This value should not be too big in product environment")
}
