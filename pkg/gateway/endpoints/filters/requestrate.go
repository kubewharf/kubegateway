/*
Copyright 2024 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package filters

import (
	"errors"
	"net/http"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apiserver/pkg/endpoints/handlers/responsewriters"
	apirequest "k8s.io/apiserver/pkg/endpoints/request"

	"github.com/kubewharf/kubegateway/pkg/gateway/endpoints/monitor"
	"github.com/kubewharf/kubegateway/pkg/gateway/endpoints/request"
	"github.com/kubewharf/kubegateway/pkg/gateway/endpoints/response"
	"github.com/kubewharf/kubegateway/pkg/gateway/metrics"
)

const (
	rateMetricRecordPeriod = time.Second * 10
)

var startRateMetricOnce sync.Once

// WithRequestRate record request rate and inflight
func WithRequestRate(
	handler http.Handler,
	longRunningRequestCheck apirequest.LongRunningRequestCheck,
	rateMonitor *monitor.RateMonitor,
) http.Handler {
	if rateMonitor == nil {
		return handler
	}

	startRateMetricOnce.Do(func() {
		startRecordingRateMetric(rateMonitor)
	})

	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		ctx := req.Context()
		requestInfo, ok := apirequest.RequestInfoFrom(ctx)
		if !ok {
			// if this happens, the handler chain isn't setup correctly because there is no request info
			responsewriters.InternalError(w, req, errors.New("no RequestInfo found in the context"))
			return
		}

		defer func() {
			proxyInfo, ok := request.ExtraProxyInfoFrom(req.Context())
			if ok && !proxyInfo.Forwarded && proxyInfo.Reason == response.TerminationReasonRateLimited {
				rateMonitor.RequestAdd(-1)
			}
		}()

		// Skip tracking long running requests.
		if longRunningRequestCheck != nil && longRunningRequestCheck(req, requestInfo) {
			rateMonitor.RequestAdd(1)
			handler.ServeHTTP(w, req)
			return
		}

		rateMonitor.RequestStart()
		defer rateMonitor.RequestEnd()

		handler.ServeHTTP(w, req)
	})
}

func startRecordingRateMetric(rateMonitor *monitor.RateMonitor) {
	go func() {
		wait.Forever(func() {
			metrics.RecordProxyRateAndInflight(rateMonitor.Rate(), rateMonitor.Inflight())
		}, rateMetricRecordPeriod)
	}()
}
