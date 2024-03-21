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

	"github.com/gobeam/stringy"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	utilnet "k8s.io/apimachinery/pkg/util/net"
	"k8s.io/apiserver/pkg/endpoints/filters"
	genericapirequest "k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/apiserver/pkg/endpoints/responsewriter"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog"

	"github.com/kubewharf/kubegateway/pkg/clusters"
	"github.com/kubewharf/kubegateway/pkg/clusters/features"
	"github.com/kubewharf/kubegateway/pkg/gateway/endpoints/request"
	"github.com/kubewharf/kubegateway/pkg/gateway/endpoints/response"
	"github.com/kubewharf/kubegateway/pkg/gateway/net"
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
	extraInfo, ok := request.ExtraRequestInfoFrom(ctx)
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

	if cluster.FeatureEnabled(features.CloseConnectionWhenIdle) {
		// Send a GOAWAY and tear down the TCP connection when idle.
		w.Header().Set("Connection", "close")
	}

	if cluster.FeatureEnabled(features.DenyAllRequests) {
		d.responseError(errors.NewServiceUnavailable(fmt.Sprintf("request for %v denied by featureGate(DenyAllRequests)", extraInfo.Hostname)), w, req, statusReasonCircuitBreaker)
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

	_ = request.SetFlowControl(req.Context(), endpointPicker.FlowControlName())

	flowcontrol := endpointPicker.FlowControl()
	if !flowcontrol.TryAcquire() {
		//TODO: exempt master request and long running request
		// add metrics
		d.responseError(errors.NewTooManyRequests(fmt.Sprintf("too many requests for cluster(%s), limited by flowControl(%v)", extraInfo.Hostname, flowcontrol.String()), response.RetryAfter), w, req, statusReasonRateLimited)
		return
	}
	defer flowcontrol.Release()

	endpoint, err := endpointPicker.Pop()
	if err != nil {
		d.responseError(errors.NewServiceUnavailable(err.Error()), w, req, statusReasonNoReadyEndpoints)
		return
	}

	ep, err := url.Parse(endpoint.Endpoint)
	if err != nil {
		d.responseError(errors.NewInternalError(err), w, req, statusReasonInvalidEndpoint)
		return
	}

	// mark this proxy request forwarded
	if err := request.SetProxyForwarded(req.Context(), endpoint.Endpoint); err != nil {
		d.responseError(errors.NewInternalError(err), w, req, statusReasonInvalidRequestContext)
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
	delegate := decorateResponseWriter(req, w, logging, requestInfo, extraInfo.Hostname, endpoint.Endpoint, user, extraInfo.Impersonator, endpointPicker.FlowControlName())
	delegate.MonitorBeforeProxy()
	defer delegate.MonitorAfterProxy()

	rw := responsewriter.WrapForHTTP1Or2(delegate)

	proxyHandler := NewUpgradeAwareHandler(location, endpoint.ProxyTransport, endpoint.PorxyUpgradeTransport, false, false, d, endpoint)
	proxyHandler.ServeHTTP(rw, newReq)
}

func (d *dispatcher) responseError(err *errors.StatusError, w http.ResponseWriter, req *http.Request, reason string) {
	code := int(err.Status().Code)
	if captureErrorReason(reason) || bool(klog.V(5)) {
		var urlHost string
		if req.URL != nil {
			// url.Host is different from req.Host when caller is reverse proxy.
			// we need this host to determine which endpoint it is if possible.
			urlHost = req.URL.Host
		}
		klog.Errorf("[proxy termination] method=%q host=%q uri=%q url.host=%v remoteAddr=%v resp=%v reason=%q message=[%v]",
			req.Method, net.HostWithoutPort(req.Host), req.RequestURI, urlHost, req.RemoteAddr, code, reason, err.Error())
	}
	response.TerminateWithError(d.codecs, err, reason, w, req)
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
