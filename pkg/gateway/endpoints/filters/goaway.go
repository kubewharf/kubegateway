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

package filters

import (
	"net/http"
	"sync/atomic"

	"k8s.io/apiserver/pkg/server/filters"
	"k8s.io/klog"
)

// goaway send a GOAWAY to client according to loadPressureThreshold and
// goaway chance for HTTP2 requests
type goaway struct {
	goawayHandler http.Handler
	innerHandler  http.Handler

	loadPressureThreshold uint64
	curRequestsInflight   uint64
}

// WithLoadPressureGoaway returns an http.Handler that send GOAWAY probabilistically
// according to the given chance for HTTP2 requests when proxy server has load pressure.
// After client receive GOAWAY, the in-flight long-running requests will not be influenced,
// and the new requests will use a new TCP connection to re-balancing to another server behind
// the load balance.
func WithLoadPressureGoaway(inner http.Handler, loadPressureThreshold int, chance float64) http.Handler {
	if loadPressureThreshold <= 0 || chance <= 0.0 {
		// proxy goaway is disabled
		return inner
	}
	klog.V(3).Infof("proxy load pressure goaway handler is enabled, threshold=%v, chance=%v", loadPressureThreshold, chance)
	return &goaway{
		goawayHandler:         filters.WithProbabilisticGoaway(inner, chance),
		innerHandler:          inner,
		loadPressureThreshold: uint64(loadPressureThreshold),
		curRequestsInflight:   0,
	}
}

func (h *goaway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Proto != "HTTP/2.0" {
		h.innerHandler.ServeHTTP(w, r)
		return
	}
	hasPressure := h.hasLoadPressure()
	defer h.end()
	if hasPressure {
		h.goawayHandler.ServeHTTP(w, r)
	} else {
		h.innerHandler.ServeHTTP(w, r)
	}
}

func (h *goaway) hasLoadPressure() bool {
	count := atomic.AddUint64(&h.curRequestsInflight, 1)
	return count > h.loadPressureThreshold
}

func (h *goaway) end() {
	atomic.AddUint64(&h.curRequestsInflight, ^uint64(0))
}
