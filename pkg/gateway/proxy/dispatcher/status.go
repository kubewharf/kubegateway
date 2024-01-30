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
	"context"
	"errors"
	"net/http"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	utilnet "k8s.io/apimachinery/pkg/util/net"
	"k8s.io/apimachinery/pkg/util/proxy"
	"k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/klog"

	"github.com/kubewharf/kubegateway/pkg/clusters"
	"github.com/kubewharf/kubegateway/pkg/gateway/endpoints/response"
	"github.com/kubewharf/kubegateway/pkg/gateway/net"
)

var (
	statusReasonNoReadyEndpoints         = "no_ready_endpoints"
	statusReasonClusterNotBeingProxied   = "cluster_not_being_proxied"
	statusReasonInvalidRequestContext    = "invalid_request_context"
	statusReasonCircuitBreaker           = "circuit_breaker"
	statusReasonRateLimited              = response.TerminationReasonRateLimited
	statusReasonInvalidEndpoint          = "invalid_endpoint"
	statusReasonUpgradeAwareHandlerError = "upgrade_aware_handler_error"
	statusReasonReverseProxyError        = "reverse_proxy_error"
)

func newErrorResponder(
	codecs serializer.CodecFactory,
	endpoint *clusters.EndpointInfo,

	requestInfo *request.RequestInfo,
	statusRecorder statusRecorder,
) *proxyErrorResponder {
	return &proxyErrorResponder{
		codecs:         codecs,
		endpoint:       endpoint,
		requestInfo:    requestInfo,
		statusRecorder: statusRecorder,
	}
}

type statusRecorder interface {
	Status() int
}

var _ proxy.ErrorResponder = &proxyErrorResponder{}

type proxyErrorResponder struct {
	codecs         serializer.CodecFactory
	endpoint       *clusters.EndpointInfo
	statusRecorder statusRecorder
	requestInfo    *request.RequestInfo
}

// implements k8s.io/apimachinery/pkg/util/proxy.ErrorResponder interface
func (r *proxyErrorResponder) Error(w http.ResponseWriter, req *http.Request, err error) {
	if utilnet.IsConnectionRefused(err) {
		klog.Errorf("connection refused err: %v, trigger healthcheck", err)
		r.endpoint.TriggerHealthCheck()
	}

	if errors.Is(err, http.ErrAbortHandler) {
		responseErr := false

		var logLevel klog.Level = 4
		err = errors.Unwrap(err)
		switch {
		case strings.Contains(err.Error(), "http2: server sent GOAWAY and closed the connection"):
			w.Header().Set("Connection", "close")
		case errors.Is(err, context.Canceled),
			strings.Contains(err.Error(), "client disconnected"),
			strings.Contains(err.Error(), "http2: client connection lost"):
			// ignore request canceled or client disconnected
			logLevel = 5
		default:
			responseErr = true
		}

		if r.isWatch() && r.statusRecorder.Status() == http.StatusOK {
			// skip write response err message when watching was established with StatusOK (200),
			// otherwise client informers will relist for error ""unable to decode an event"
			responseErr = false
		}

		if klog.V(logLevel) {
			var urlHost string
			if req.URL != nil {
				urlHost = req.URL.Host
			}
			klog.Errorf("request abort: verb=%q host=%q endpoint=%q remoteAddr=%q resp=%v uri=%q, responseError=%v, err=[%v]",
				r.requestInfo.Verb, net.HostWithoutPort(req.Host), urlHost, req.RemoteAddr, r.statusRecorder.Status(), req.RequestURI, responseErr, err)
		}
		
		if !responseErr {
			return
		}
	}

	status := errorToProxyStatus(err)
	reason := statusReasonUpgradeAwareHandlerError
	if status.Code == http.StatusBadGateway {
		reason = statusReasonReverseProxyError
	}
	responseError(r.codecs, &apierrors.StatusError{ErrStatus: *status}, w, req, reason)
}

func (r *proxyErrorResponder) isWatch() bool {
	return r.requestInfo.IsResourceRequest && r.requestInfo.Verb == "watch"
}

func responseError(codecs serializer.CodecFactory, err *apierrors.StatusError, w http.ResponseWriter, req *http.Request, reason string) {
	code := int(err.Status().Code)
	if captureErrorReason(reason) || bool(klog.V(5)) {
		var urlHost string
		if req.URL != nil {
			// url.Host is different from req.Host when caller is reverse proxy.
			// we need this host to determine which endpoint it is if possible.
			urlHost = req.URL.Host
		}
		klog.Errorf("[proxy termination] method=%q host=%q uri=%q url.host=%q remoteAddr=%q resp=%v reason=%q message=[%v]",
			req.Method, net.HostWithoutPort(req.Host), req.RequestURI, urlHost, req.RemoteAddr, code, reason, err.Error())
	}
	response.TerminateWithError(codecs, err, reason, w, req)
}

func captureErrorReason(reason string) bool {
	switch reason {
	case statusReasonUpgradeAwareHandlerError, statusReasonReverseProxyError, statusReasonInvalidEndpoint:
		return true
	}
	return false
}

// statusError is an object that can be converted into an metav1.Status
type statusError interface {
	Status() metav1.Status
}

// errorToProxyStatus converts an error to an metav1.Status object.
func errorToProxyStatus(err error) *metav1.Status {
	switch t := err.(type) {
	case statusError:
		status := t.Status()
		if len(status.Status) == 0 {
			status.Status = metav1.StatusFailure
		}
		switch status.Status {
		case metav1.StatusSuccess:
			if status.Code == 0 {
				status.Code = http.StatusOK
			}
		case metav1.StatusFailure:
			if status.Code == 0 {
				status.Code = http.StatusInternalServerError
			}
		default:
			if status.Code == 0 {
				status.Code = http.StatusInternalServerError
			}
		}
		status.Kind = "Status"
		status.APIVersion = "v1"
		//TODO: check for invalid responses
		return &status
	default:
		return &metav1.Status{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Status",
				APIVersion: "v1",
			},
			Status:  metav1.StatusFailure,
			Code:    int32(http.StatusBadGateway),
			Reason:  "KubeGatewayInternalError",
			Message: err.Error(),
		}
	}
}
