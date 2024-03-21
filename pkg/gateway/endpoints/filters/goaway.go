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
	"fmt"
	"net/http"

	"github.com/kubewharf/kubegateway/pkg/gateway/endpoints/monitor"
	"k8s.io/apiserver/pkg/server/filters"
	"k8s.io/klog"
)

// goaway send a GOAWAY to client according to loadPressureThreshold and
// goaway chance for HTTP2 requests
type goaway struct {
	goawayHandler http.Handler
	innerHandler  http.Handler

	inflightThreshold        int32
	qpsThreshold             int32
	throughputThresholdBytes int32

	rateMonitor       *monitor.RateMonitor
	throughputMonitor *monitor.ThroughputMonitor
}

// WithLoadPressureGoaway returns an http.Handler that send GOAWAY probabilistically
// according to the given chance for HTTP2 requests when proxy server has load pressure.
// After client receive GOAWAY, the in-flight long-running requests will not be influenced,
// and the new requests will use a new TCP connection to re-balancing to another server behind
// the load balance.
func WithLoadPressureGoaway(
	inner http.Handler,
	inflightThreshold int32,
	qpsThreshold int32,
	throughputThresholdMB int32,
	chance float64,
	rateMonitor *monitor.RateMonitor,
	throughputMonitor *monitor.ThroughputMonitor,
) http.Handler {
	if chance <= 0.0 || (inflightThreshold <= 0 && qpsThreshold <= 0 && throughputThresholdMB <= 0) {
		// proxy goaway is disabled
		return inner
	}
	klog.V(2).Infof("proxy load pressure goaway handler is enabled, inflight-threshold=%v, qps-threshold=%v, throughput-threshold=%vMB, chance=%v",
		inflightThreshold, qpsThreshold, throughputThresholdMB, chance)

	return &goaway{
		goawayHandler:            filters.WithProbabilisticGoaway(inner, chance),
		innerHandler:             inner,
		inflightThreshold:        inflightThreshold,
		qpsThreshold:             qpsThreshold,
		throughputThresholdBytes: throughputThresholdMB << 20,
		rateMonitor:              rateMonitor,
		throughputMonitor:        throughputMonitor,
	}
}

func (h *goaway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Proto != "HTTP/2.0" {
		h.innerHandler.ServeHTTP(w, r)
		return
	}
	loadPressure := h.hasLoadPressure()

	if len(loadPressure) > 0 {
		h.goawayHandler.ServeHTTP(w, r)
		coonHeader := w.Header().Get("Connection")
		if coonHeader == "close" {
			klog.V(4).Infof("Send GOAWAY to connection, %v method=%q host=%q uri=%q remoteAddr=%q", loadPressure, r.Method, r.Host, r.RequestURI, r.RemoteAddr)
		}
	} else {
		h.innerHandler.ServeHTTP(w, r)
	}
}

func (h *goaway) hasLoadPressure() string {
	var pressure string
	if h.inflightThreshold > 0 {
		inflight := h.rateMonitor.Inflight()
		if inflight > h.inflightThreshold {
			pressure = fmt.Sprintf("inflight=%v", inflight)
		}
	}
	if h.qpsThreshold > 0 {
		rate := h.rateMonitor.Rate()
		if rate > float64(h.qpsThreshold) {
			pressure = fmt.Sprintf("rate=%.0f", rate)
		}
	}

	if h.throughputThresholdBytes > 0 {
		throughput := h.throughputMonitor.Throughput()
		if throughput > float64(h.throughputThresholdBytes) {
			pressure = fmt.Sprintf("throughput=%.0fMB", throughput/1024/1024)
		}
	}

	return pressure
}
