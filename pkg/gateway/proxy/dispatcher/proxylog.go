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

package dispatcher

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	utilnet "k8s.io/apimachinery/pkg/util/net"
	"k8s.io/apiserver/pkg/authentication/user"
	"k8s.io/apiserver/pkg/endpoints/responsewriter"
	"k8s.io/klog"
)

var _ http.ResponseWriter = &responseWriterDelegator{}
var _ responsewriter.UserProvidedDecorator = &responseWriterDelegator{}

// Add a layer on top of ResponseWriter, so we can track latency, statusCode
// and error message sources.
type responseWriterDelegator struct {
	statusRecorded     bool
	status             int
	addedInfo          string
	startTime          time.Time
	captureErrorOutput bool

	logging      bool
	verb         string
	host         string
	endpoint     string
	user         user.Info
	impersonator user.Info

	req *http.Request
	w   http.ResponseWriter

	written int64
}

func decorateResponseWriter(
	req *http.Request,
	w http.ResponseWriter,
	logging bool,
	verb, host, endpoint string,
	user, impersonator user.Info,
) *responseWriterDelegator {
	return &responseWriterDelegator{
		startTime:    time.Now(),
		req:          req,
		w:            w,
		logging:      logging,
		verb:         verb,
		host:         host,
		endpoint:     endpoint,
		user:         user,
		impersonator: impersonator,
	}
}

func (rw *responseWriterDelegator) Unwrap() http.ResponseWriter {
	return rw.w
}

// Header implements http.ResponseWriter.
func (rw *responseWriterDelegator) Header() http.Header {
	return rw.w.Header()
}

// WriteHeader implements http.ResponseWriter.
func (rw *responseWriterDelegator) WriteHeader(status int) {
	rw.recordStatus(status)
	rw.w.WriteHeader(status)
}

// Write implements http.ResponseWriter.
func (rw *responseWriterDelegator) Write(b []byte) (int, error) {
	if !rw.statusRecorded {
		rw.recordStatus(http.StatusOK) // Default if WriteHeader hasn't been called
	}
	if rw.captureErrorOutput {
		rw.debugf("logging error output: %q\n", string(b))
	}
	n, err := rw.w.Write(b)
	rw.written += int64(n)
	return n, err
}

func (rw *responseWriterDelegator) Status() int {
	return rw.status
}

func (rw *responseWriterDelegator) ContentLength() int {
	return int(rw.written)
}

func (rw *responseWriterDelegator) Elapsed() time.Duration {
	return time.Since(rw.startTime)
}

// Log is intended to be called once at the end of your request handler, via defer
func (rw *responseWriterDelegator) Log() {
	if !rw.logging {
		return
	}
	latency := rw.Elapsed()
	sourceIPs := utilnet.SourceIPs(rw.req)
	verb := strings.ToUpper(rw.verb)
	if rw.impersonator != nil {
		klog.Infof("verb=%q host=%q endpoint=%q URI=%q latency=%v resp=%v user=%q userGroup=%v userAgent=%q impersonator=%q impersonatorGroup=%v srcIP=%v: %v",
			verb,
			rw.host,
			rw.endpoint,
			rw.req.RequestURI,
			latency,
			rw.status,
			rw.user.GetName(),
			rw.user.GetGroups(),
			rw.req.UserAgent(),
			rw.impersonator.GetName(),
			rw.impersonator.GetGroups(),
			sourceIPs,
			rw.addedInfo,
		)
	} else {
		klog.Infof("verb=%q host=%q endpoint=%q URI=%q latency=%v resp=%v user=%q userGroup=%v userAgent=%q srcIP=%v: %v",
			verb,
			rw.host,
			rw.endpoint,
			rw.req.RequestURI,
			latency,
			rw.status,
			rw.user.GetName(),
			rw.user.GetGroups(),
			rw.req.UserAgent(),
			sourceIPs,
			rw.addedInfo,
		)
	}
}

func (rw *responseWriterDelegator) recordStatus(status int) {
	rw.status = status
	rw.statusRecorded = true
	rw.captureErrorOutput = captureErrorOutput(status)
}

// debugf adds additional data to be logged with this request.
func (rw *responseWriterDelegator) debugf(format string, data ...interface{}) {
	rw.addedInfo += "\n" + fmt.Sprintf(format, data...)
}

func captureErrorOutput(code int) bool {
	return code >= http.StatusInternalServerError
}
