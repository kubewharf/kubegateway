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
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apiserver/pkg/endpoints/responsewriter"

	"github.com/kubewharf/kubegateway/pkg/gateway/endpoints/monitor"
	"github.com/kubewharf/kubegateway/pkg/gateway/metrics"
)

const (
	throughputMetricRecordPeriod = time.Second * 10
)

var startThroughputMetricOnce sync.Once

// WithRequestThroughput record request input and output throughput
func WithRequestThroughput(
	handler http.Handler,
	throughputMonitor *monitor.ThroughputMonitor,
) http.Handler {
	if throughputMonitor == nil {
		return handler
	}

	startThroughputMetricOnce.Do(func() {
		startRecordingThroughputMetric(throughputMonitor)
	})

	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		delegate := &throughputResponseWriter{
			ResponseWriter:    w,
			throughputMonitor: throughputMonitor,
		}
		rw := responsewriter.WrapForHTTP1Or2(delegate)

		rd := &throughputRequestReader{
			ReadCloser:        req.Body,
			throughputMonitor: throughputMonitor,
		}
		req.Body = rd

		handler.ServeHTTP(rw, req)
	})
}

var _ io.ReadCloser = &throughputRequestReader{}

type throughputRequestReader struct {
	io.ReadCloser
	read              int
	throughputMonitor *monitor.ThroughputMonitor
}

// Write implements io.ReadCloser
func (r *throughputRequestReader) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	r.read += n

	r.throughputMonitor.RecordRequest(int64(n))
	return n, err
}

func (r *throughputRequestReader) RequestSize() int {
	return r.read
}

var _ http.ResponseWriter = &throughputResponseWriter{}
var _ responsewriter.UserProvidedDecorator = &throughputResponseWriter{}

type throughputResponseWriter struct {
	http.ResponseWriter
	written           int64
	throughputMonitor *monitor.ThroughputMonitor
}

func (w *throughputResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

// Write implements http.ResponseWriter.
func (w *throughputResponseWriter) Write(b []byte) (int, error) {
	n, err := w.ResponseWriter.Write(b)
	w.written += int64(n)

	w.throughputMonitor.RecordResponse(int64(n))
	return n, err
}

// Hijack implements http.Hijacker. If the underlying ResponseWriter is a
// Hijacker, its Hijack method is returned. Otherwise an error is returned.
func (w *throughputResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hj, ok := w.ResponseWriter.(http.Hijacker); ok {
		return hj.Hijack()
	}
	return nil, nil, fmt.Errorf("http.Hijacker interface is not supported")
}

func (w *throughputResponseWriter) ResponseSize() int {
	return int(w.written)
}

func startRecordingThroughputMetric(throughputMonitor *monitor.ThroughputMonitor) {
	go func() {
		wait.Forever(func() {
			metrics.RecordRequestThroughput(throughputMonitor.RequestSize(), throughputMonitor.ResponseSize())
		}, throughputMetricRecordPeriod)
	}()
}
