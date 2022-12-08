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

package filters

import (
	"net/http"

	"github.com/gobeam/stringy"
	"github.com/kubewharf/kubegateway/pkg/gateway/endpoints/request"
	"github.com/kubewharf/kubegateway/pkg/gateway/metrics"
	"k8s.io/apiserver/pkg/endpoints/responsewriter"
)

func WithTerminationMetrics(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		req = req.WithContext(request.WithProxyInfo(req.Context(), request.NewProxyInfo()))

		tmw := &terminationMetricsWriter{w: w}
		defer tmw.recordMetrics(req)

		rw := responsewriter.WrapForHTTP1Or2(tmw)
		handler.ServeHTTP(rw, req)
	})
}

type terminationMetricsWriter struct {
	status         int
	statusRecorded bool
	w              http.ResponseWriter
}

func (rw *terminationMetricsWriter) Unwrap() http.ResponseWriter {
	return rw.w
}

// Header implements http.ResponseWriter.
func (rw *terminationMetricsWriter) Header() http.Header {
	return rw.w.Header()
}

// WriteHeader implements http.ResponseWriter.
func (rw *terminationMetricsWriter) WriteHeader(status int) {
	rw.recordStatus(status)
	rw.w.WriteHeader(status)
}

// Write implements http.ResponseWriter.
func (rw *terminationMetricsWriter) Write(b []byte) (int, error) {
	if !rw.statusRecorded {
		rw.recordStatus(http.StatusOK) // Default if WriteHeader hasn't been called
	}
	return rw.w.Write(b)
}

func (rw *terminationMetricsWriter) recordStatus(status int) {
	rw.status = status
	rw.statusRecorded = true
}

func (rw *terminationMetricsWriter) recordMetrics(req *http.Request) {
	proxyInfo, ok := request.ExtraProxyInfoFrom(req.Context())
	var forwarded bool
	var reason string
	if ok {
		forwarded = proxyInfo.Forwarded
		reason = proxyInfo.Reason
	}
	if !forwarded && rw.status >= http.StatusBadRequest {
		if len(reason) == 0 {
			// use status text when reason is empty
			reason = stringy.New(http.StatusText(rw.status)).SnakeCase().ToLower()
		}
		metrics.RecordProxyRequestTermination(req, rw.status, reason)
	}
}
