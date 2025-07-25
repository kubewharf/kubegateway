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
	"net/http"
	"strings"
	"time"

	utilnet "k8s.io/apimachinery/pkg/util/net"
	"k8s.io/apiserver/pkg/authentication/user"
	"k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/klog"

	"github.com/kubewharf/apiserver-runtime/pkg/server"
	gatewayrequest "github.com/kubewharf/kubegateway/pkg/gateway/endpoints/request"
	"github.com/kubewharf/kubegateway/pkg/gateway/metrics"
	"github.com/kubewharf/kubegateway/pkg/util/tracing"
)

// proxy log and metrics
type proxyRequestMonitor struct {
	startTime       time.Time
	logging         bool
	host            string
	endpoint        string
	flowControlName string
	user            user.Info
	impersonator    user.Info

	req              *http.Request
	requestInfo      *request.RequestInfo
	extraRequestInfo *gatewayrequest.ExtraRequestInfo
	w                http.ResponseWriter
}

func requestMonitor(
	req *http.Request,
	w http.ResponseWriter,
	logging bool,
	requestInfo *request.RequestInfo,
	extraInfo *gatewayrequest.ExtraRequestInfo,
	endpoint string,
	user, impersonator user.Info,
	flowControlName string,
) *proxyRequestMonitor {
	return &proxyRequestMonitor{
		startTime:        time.Now(),
		req:              req,
		w:                w,
		logging:          logging,
		requestInfo:      requestInfo,
		extraRequestInfo: extraInfo,
		host:             extraInfo.Hostname,
		endpoint:         endpoint,
		flowControlName:  flowControlName,
		user:             user,
		impersonator:     impersonator,
	}
}

func (rw *proxyRequestMonitor) Elapsed() time.Duration {
	return time.Since(rw.startTime)
}

func (rw *proxyRequestMonitor) isWatch() bool {
	return rw.requestInfo.IsResourceRequest && rw.requestInfo.Verb == "watch"
}

func (rw *proxyRequestMonitor) MonitorBeforeProxy() {
	if rw.isWatch() {
		metrics.RecordWatcherRegistered(rw.host, rw.endpoint, rw.requestInfo.Resource)
		//TODO: log watch requests before proxy
	}
	// TODO: add a metrics before request forwarded
}

func (rw *proxyRequestMonitor) MonitorAfterProxy() {
	if rw.isWatch() {
		metrics.RecordWatcherUnregistered(rw.host, rw.endpoint, rw.requestInfo.Resource)
	}

	if !gatewayrequest.IsProxyForwarded(rw.req.Context()) {
		return
	}

	userInfo := rw.user
	if rw.impersonator != nil {
		userInfo = rw.impersonator
	}

	var requestSize int
	var responseSize int
	var status int
	if readerWriter := rw.extraRequestInfo.ReaderWriter; readerWriter != nil {
		requestSize = readerWriter.RequestSize()
		responseSize = readerWriter.ResponseSize()
		status = readerWriter.Status()
	}

	// we only monitor forwarded proxy reqeust here
	metrics.MonitorProxyRequest(
		rw.req,
		rw.host,
		rw.endpoint,
		rw.flowControlName,
		rw.requestInfo,
		userInfo,
		rw.extraRequestInfo.IsLongRunningRequest,
		rw.w.Header().Get("Content-Type"),
		status,
		requestSize,
		responseSize,
		rw.Elapsed(),
	)
	rw.Log()
}

// Log is intended to be called once at the end of your request handler, via defer
func (rw *proxyRequestMonitor) Log() {
	latency := rw.Elapsed()
	logging := rw.logging
	verb := strings.ToUpper(rw.requestInfo.Verb)
	isLongRunning := server.DefaultLongRunningFunc(rw.req, rw.requestInfo)

	var status int
	var addedInfo string
	if readerWriter := rw.extraRequestInfo.ReaderWriter; readerWriter != nil {
		status = readerWriter.Status()
		addedInfo = readerWriter.AddedInfo()
	}

	if proxyLogPred(status, verb, latency, isLongRunning) {
		logging = true
	}
	if !logging {
		return
	}
	sourceIPs := utilnet.SourceIPs(rw.req)
	traceId := tracing.TraceID(rw.req.Context())

	if rw.impersonator != nil {
		klog.Infof("verb=%q host=%q endpoint=%q URI=%q latency=%v resp=%v user=%q userGroup=%v userAgent=%q impersonator=%q impersonatorGroup=%v srcIP=%v traceId=%v: %v",
			verb,
			rw.host,
			rw.endpoint,
			rw.req.RequestURI,
			latency,
			status,
			rw.user.GetName(),
			rw.user.GetGroups(),
			rw.req.UserAgent(),
			rw.impersonator.GetName(),
			rw.impersonator.GetGroups(),
			sourceIPs,
			traceId,
			addedInfo,
		)
	} else {
		klog.Infof("verb=%q host=%q endpoint=%q URI=%q latency=%v resp=%v user=%q userGroup=%v userAgent=%q srcIP=%v traceId=%v: %v",
			verb,
			rw.host,
			rw.endpoint,
			rw.req.RequestURI,
			latency,
			status,
			rw.user.GetName(),
			rw.user.GetGroups(),
			rw.req.UserAgent(),
			sourceIPs,
			traceId,
			addedInfo,
		)
	}
}

func captureErrorOutput(code int) bool {
	return code >= http.StatusInternalServerError
}

func proxyLogPred(status int, verb string, latency time.Duration, isLongRunning bool) bool {
	if klog.V(5) {
		return true
	}

	if !isLongRunning && latency > gatewayrequest.LogThreshold(verb) {
		return true
	}

	if isLongRunning && status == 0 {
		return false
	}

	return (status < http.StatusOK || status >= http.StatusInternalServerError) && status != http.StatusSwitchingProtocols
}
