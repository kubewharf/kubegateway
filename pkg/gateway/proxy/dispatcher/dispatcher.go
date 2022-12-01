// Copyright 2022 ByteDance and its affiliates.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package dispatcher

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/gobeam/stringy"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/util/httpstream"
	utilnet "k8s.io/apimachinery/pkg/util/net"
	"k8s.io/apiserver/pkg/endpoints/filters"
	"k8s.io/apiserver/pkg/endpoints/handlers/responsewriters"
	genericapirequest "k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/apiserver/pkg/endpoints/responsewriter"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog"

	"github.com/kubewharf/kubegateway/pkg/clusters"
	"github.com/kubewharf/kubegateway/pkg/gateway/endpoints/request"
	"github.com/kubewharf/kubegateway/pkg/gateway/metrics"
	"github.com/kubewharf/kubegateway/pkg/gateway/net"
)

var (
	retryAfter = 1
)

type dispatcher struct {
	clusters.Manager
	codecs          serializer.CodecFactory
	enableAccessLog bool
}

func NewDispatcher(clusterManager clusters.Manager, enableAccessLog bool) http.Handler {
	return &dispatcher{
		Manager:         clusterManager,
		codecs:          scheme.Codecs,
		enableAccessLog: enableAccessLog,
	}
}

// TODO: add metrics
func (d *dispatcher) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	user, ok := genericapirequest.UserFrom(ctx)
	if !ok {
		d.responseError(errors.NewInternalError(fmt.Errorf("no user info found in request context")), w, req, statusReasonInvalidRequestContext)
		return
	}
	extraInfo, ok := request.ExtraReqeustInfoFrom(ctx)
	if !ok {
		d.responseError(errors.NewInternalError(fmt.Errorf("no extra request info found in request context")), w, req, statusReasonInvalidRequestContext)
		return
	}
	requestInfo, ok := genericapirequest.RequestInfoFrom(ctx)
	if !ok {
		d.responseError(errors.NewInternalError(fmt.Errorf("no request info found in request context")), w, req, statusReasonInvalidRequestContext)
		return
	}
	cluster, ok := d.Get(extraInfo.Hostname)
	if !ok {
		d.responseError(errors.NewServiceUnavailable(fmt.Sprintf("the request cluster(%s) is not being proxied", extraInfo.Hostname)), w, req, statusReasonClusterNotBeingProxied)
		return
	}
	requestAttributes, err := filters.GetAuthorizerAttributes(ctx)
	if err != nil {
		d.responseError(errors.NewInternalError(err), w, req, statusReasonInvalidRequestContext)
		return
	}
	endpointPicker, err := cluster.MatchAttributes(requestAttributes)
	if err != nil {
		d.responseError(errors.NewInternalError(err), w, req, normalizeErrToReason(err))
		return
	}

	flowcontrol := endpointPicker.FlowControl()
	if !flowcontrol.TryAcquire() {
		//TODO: exempt master request and long running request
		// add metrics
		d.responseError(errors.NewTooManyRequests(fmt.Sprintf("too many requests for cluster(%s), limited by flowControl(%v)", extraInfo.Hostname, flowcontrol.String()), retryAfter), w, req, statusReasonRateLimited)
		return
	}
	defer flowcontrol.Release()

	endpoint, err := endpointPicker.Pop()
	if err != nil {
		d.responseError(errors.NewServiceUnavailable(err.Error()), w, req, statusReasonNoReadyEndpoints)
		return
	}

	transport := endpoint.ProxyTransport
	if httpstream.IsUpgradeRequest(req) {
		transport = endpoint.PorxyUpgradeTransport
	}

	ep, err := url.Parse(endpoint.Endpoint)
	if err != nil {
		d.responseError(errors.NewInternalError(err), w, req, statusReasonInvalidEndpoint)
		return
	}

	location := &url.URL{}
	location.Scheme = ep.Scheme
	location.Host = ep.Host
	location.Path = req.URL.Path
	location.RawQuery = req.URL.Query().Encode()

	newReq, cancel := newRequestForProxy(location, req, extraInfo.Hostname)
	// close this request if endpoint is stoped
	go func() {
		select {
		case <-newReq.Context().Done():
			// this context comes from incoming server requests, and then we use
			// it as proxy client context to control cancellation
			//
			// For incoming server requests, the context is canceled when the
			// client's connection closes, the request is canceled (with HTTP/2),
			// or when the ServeHTTP method returns.
		case <-endpoint.Context().Done():
			// when endpoint stopping, we should cancel the context to close proxy request
			cancel()
		}
	}()

	logging := d.enableAccessLog && endpointPicker.EnableLog()
	delegate := decorateResponseWriter(req, w, logging, requestInfo, extraInfo.Hostname, endpoint.Endpoint, user, extraInfo.Impersonator)
	delegate.MonitorBeforeProxy()
	defer delegate.MonitorAfterProxy()

	rw := responsewriter.WrapForHTTP1Or2(delegate)

	proxyHandler := NewUpgradeAwareHandler(location, transport, false, false, d)
	proxyHandler.ServeHTTP(rw, newReq)
}

func (d *dispatcher) responseError(err *errors.StatusError, w http.ResponseWriter, req *http.Request, reason string) {
	gv := schema.GroupVersion{Group: "", Version: "v1"}
	if errors.IsTooManyRequests(err) {
		w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
	}
	responsewriters.ErrorNegotiated(err, d.codecs, gv, w, req)
	code := int(err.Status().Code)
	metrics.RecordProxyRequestTermination(req, code, reason)
	if captureErrorOutput(code) {
		klog.Errorf("[logging denied] method=%q host=%q URI=%q resp=%v reason=%q message=[%v]", req.Method, net.HostWithoutPort(req.Host), req.RequestURI, code, reason, err.Error())
	}
}

// newRequestForProxy returns a shallow copy of the original request with a context that may include a timeout for discovery requests
func newRequestForProxy(location *url.URL, req *http.Request, _ string) (*http.Request, context.CancelFunc) {
	ctx := req.Context()
	newCtx, cancel := context.WithCancel(ctx)

	// WithContext creates a shallow clone of the request with the same context.
	newReq := req.WithContext(newCtx)
	newReq.Header = utilnet.CloneHeader(req.Header)
	newReq.URL = location

	return newReq, cancel
}

// implements k8s.io/apimachinery/pkg/util/proxy.ErrorResponder interface
func (d *dispatcher) Error(w http.ResponseWriter, req *http.Request, err error) {
	status := errorToProxyStatus(err)
	reason := statusReasonUpgradeAwareHandlerError
	if status.Code == http.StatusBadGateway {
		reason = statusReasonReverseProxyError
	}
	d.responseError(&errors.StatusError{ErrStatus: *status}, w, req, reason)
}

func normalizeErrToReason(err error) string {
	str := stringy.New(err.Error())
	return str.SnakeCase().ToLower()
}
