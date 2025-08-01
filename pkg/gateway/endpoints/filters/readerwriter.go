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
	"github.com/kubewharf/kubegateway/pkg/gateway/endpoints/request"
	"github.com/kubewharf/kubegateway/pkg/gateway/metrics"
)

const (
	throughputMetricRecordPeriod = time.Second * 10
)

var startThroughputMetricOnce sync.Once

// WithRequestReaderWriterWrapper record request input and output throughput
func WithRequestReaderWriterWrapper(
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
		delegate := &responseWriterWrapper{
			ResponseWriter:    w,
			throughputMonitor: throughputMonitor,
		}
		rw := responsewriter.WrapForHTTP1Or2(delegate)

		rd := &requestReaderWrapper{
			ReadCloser:        req.Body,
			throughputMonitor: throughputMonitor,
		}
		req.Body = rd

		info, ok := request.ExtraRequestInfoFrom(req.Context())
		if !ok {
			handler.ServeHTTP(w, req)
			return
		}
		info.ReaderWriter = &readerWriter{
			requestReaderWrapper:  rd,
			responseWriterWrapper: delegate,
		}
		req = req.WithContext(request.WithExtraRequestInfo(req.Context(), info))
		handler.ServeHTTP(rw, req)
	})
}

var _ request.RequestReaderWriterWrapper = &readerWriter{}

type readerWriter struct {
	*requestReaderWrapper
	*responseWriterWrapper
}

var _ io.ReadCloser = &requestReaderWrapper{}

type requestReaderWrapper struct {
	io.ReadCloser
	read              int
	throughputMonitor *monitor.ThroughputMonitor
}

// Write implements io.ReadCloser
func (r *requestReaderWrapper) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	r.read += n

	r.throughputMonitor.RecordRequest(int64(n))
	return n, err
}

func (r *requestReaderWrapper) RequestSize() int {
	return r.read
}

var _ http.ResponseWriter = &responseWriterWrapper{}
var _ responsewriter.UserProvidedDecorator = &responseWriterWrapper{}

type responseWriterWrapper struct {
	statusRecorded     bool
	status             int
	addedInfo          string
	startTime          time.Time
	captureErrorOutput bool
	written            int64
	read               int

	http.ResponseWriter
	throughputMonitor *monitor.ThroughputMonitor
}

func (w *responseWriterWrapper) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

// WriteHeader implements http.ResponseWriter.
func (w *responseWriterWrapper) WriteHeader(status int) {
	w.recordStatus(status)
	w.ResponseWriter.WriteHeader(status)
}

func (w *responseWriterWrapper) recordStatus(status int) {
	w.status = status
	w.statusRecorded = true
	w.captureErrorOutput = captureErrorOutput(status)
}

func captureErrorOutput(code int) bool {
	return code >= http.StatusInternalServerError
}

// Write implements http.ResponseWriter.
func (w *responseWriterWrapper) Write(b []byte) (int, error) {
	if !w.statusRecorded {
		w.recordStatus(http.StatusOK) // Default if WriteHeader hasn't been called
	}
	if w.captureErrorOutput {
		w.debugf("logging error output: %q\n", string(b))
	}

	n, err := w.ResponseWriter.Write(b)
	w.written += int64(n)

	w.throughputMonitor.RecordResponse(int64(n))
	return n, err
}

// debugf adds additional data to be logged with this request.
func (w *responseWriterWrapper) debugf(format string, data ...interface{}) {
	w.addedInfo += "\n" + fmt.Sprintf(format, data...)
}

// Hijack implements http.Hijacker. If the underlying ResponseWriter is a
// Hijacker, its Hijack method is returned. Otherwise an error is returned.
func (w *responseWriterWrapper) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hj, ok := w.ResponseWriter.(http.Hijacker); ok {
		return hj.Hijack()
	}
	return nil, nil, fmt.Errorf("http.Hijacker interface is not supported")
}

func (w *responseWriterWrapper) ResponseSize() int {
	return int(w.written)
}

func (w *responseWriterWrapper) Status() int {
	return w.status
}

func (w *responseWriterWrapper) AddedInfo() string {
	return w.addedInfo
}

func startRecordingThroughputMetric(throughputMonitor *monitor.ThroughputMonitor) {
	go func() {
		wait.Forever(func() {
			metrics.RecordRequestThroughput(throughputMonitor.RequestSize(), throughputMonitor.ResponseSize())
		}, throughputMetricRecordPeriod)
	}()
}
