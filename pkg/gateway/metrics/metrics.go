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

package metrics

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/types"
	utilsets "k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apiserver/pkg/endpoints/request"
	genericapirequest "k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/apiserver/pkg/features"
	utilfeature "k8s.io/apiserver/pkg/util/feature"

	"github.com/kubewharf/kubegateway/pkg/gateway/net"
)

const (
	OtherRequestMethod string = "other"
)

var (
	// ValidRequestMethods are the valid request methods which we report in our metrics.
	// Any other request methods will be aggregated under 'unknown'
	ValidRequestMethods = utilsets.NewString(
		"APPLY",
		"CONNECT",
		"CREATE",
		"DELETE",
		"DELETECOLLECTION",
		"GET",
		"LIST",
		"PATCH",
		"POST",
		"PROXY",
		"PUT",
		"UPDATE",
		"WATCH",
		"WATCHLIST",
	)
)

// RecordUnhealthyUpstream records that the upstream endpoint is unhealthy.
func RecordUnhealthyUpstream(serverName string, endpoint string, reason string) {
	ProxyUpstreamUnhealthyObservers.Observe(MetricInfo{
		ServerName: serverName,
		Endpoint:   endpoint,
		Reason:     reason,
	})
}

func RecordProxyRequestReceived(req *http.Request, serverName string, requestInfo *request.RequestInfo) {
	if requestInfo == nil {
		requestInfo = &request.RequestInfo{Verb: req.Method, Path: req.URL.Path}
	}
	scope := CleanScope(requestInfo)
	verb := canonicalVerb(requestInfo, scope)
	resource := "NonResourceRequest"
	if requestInfo.IsResourceRequest {
		resource = requestInfo.Resource
		if len(requestInfo.Subresource) > 0 {
			resource += "/" + requestInfo.Subresource
		}
	}

	ProxyReceiveRequestCounterObservers.Observe(MetricInfo{
		ServerName: serverName,
		Verb:       verb,
		Resource:   resource,
		Request:    req,
	})
}

// MonitorProxyRequest handles standard transformations for client and the reported verb and then invokes Monitor to record
// a request. verb must be uppercase to be backwards compatible with existing monitoring tooling.
func MonitorProxyRequest(req *http.Request, serverName, endpoint, flowControl string, requestInfo *request.RequestInfo, contentType string, httpCode, respSize int, elapsed time.Duration) {
	if requestInfo == nil {
		requestInfo = &request.RequestInfo{Verb: req.Method, Path: req.URL.Path}
	}

	scope := CleanScope(requestInfo)
	verb := canonicalVerb(requestInfo, scope)
	elapsedSeconds := elapsed.Seconds()
	resource := "NonResourceRequest"
	if requestInfo.IsResourceRequest {
		resource = requestInfo.Resource
		if len(requestInfo.Subresource) > 0 {
			resource += "/" + requestInfo.Subresource
		}
	}

	ProxyRequestCounterObservers.Observe(MetricInfo{
		ServerName:  serverName,
		Endpoint:    endpoint,
		FlowControl: flowControl,
		Verb:        verb,
		Resource:    resource,
		HttpCode:    codeToString(httpCode),
		Latency:     elapsedSeconds,
		Request:     req,
	})
	ProxyRequestLatenciesObservers.Observe(MetricInfo{
		ServerName:  serverName,
		Endpoint:    endpoint,
		FlowControl: flowControl,
		Verb:        verb,
		Resource:    resource,
		Latency:     elapsedSeconds,
		Request:     req,
	})

	// We are only interested in response sizes of read requests.
	// nolint:goconst
	if requestInfo.IsResourceRequest && (verb == "GET" || verb == "LIST") {
		ProxyResponseSizesObservers.Observe(MetricInfo{
			ServerName:   serverName,
			Endpoint:     endpoint,
			Verb:         verb,
			Resource:     resource,
			ResponseSize: float64(respSize),
			Request:      req,
		})
	}
}

// RecordProxyRequestTermination records that the request was terminated early as part of a resource
// preservation or apiserver self-defense mechanism (e.g. timeouts, maxinflight throttling,
// proxyHandler errors). RecordProxyRequestTermination should only be called zero or one times
// per request.
func RecordProxyRequestTermination(req *http.Request, code int, reason, flowControl string) {
	requestInfo, ok := genericapirequest.RequestInfoFrom(req.Context())
	if !ok {
		requestInfo = &request.RequestInfo{Verb: req.Method, Path: req.URL.Path}
	}
	scope := CleanScope(requestInfo)
	// We don't use verb from <requestInfo>, as for the healthy path
	// MonitorRequest is called from InstrumentRouteFunc which is registered
	// in installer.go with predefined list of verbs (different than those
	// translated to RequestInfo).
	// However, we need to tweak it e.g. to differentiate GET from LIST.
	verb := canonicalVerb(requestInfo, scope)
	// set verbs to a bounded set of known and expected verbs
	if !ValidRequestMethods.Has(verb) {
		verb = OtherRequestMethod
	}
	serverName := net.HostWithoutPort(req.Host)
	resource := cleanResource(requestInfo)

	ProxyRequestTerminationsObservers.Observe(MetricInfo{
		ServerName:  serverName,
		FlowControl: flowControl,
		Verb:        cleanVerb(verb, req),
		Path:        requestInfo.Path,
		HttpCode:    codeToString(code),
		Reason:      reason,
		Resource:    resource,
		Request:     req,
	})
}

func RecordWatcherRegistered(serverName, endpoint, resource string) {
	ProxyWatcherRegisteredObservers.Observe(MetricInfo{
		ServerName: serverName,
		Endpoint:   endpoint,
		Resource:   resource,
	})
}

func RecordWatcherUnregistered(serverName, endpoint, resource string) {
	ProxyWatcherUnregisteredObservers.Observe(MetricInfo{
		ServerName: serverName,
		Endpoint:   endpoint,
		Resource:   resource,
	})
}

// CleanScope returns the scope of the request.
func CleanScope(requestInfo *request.RequestInfo) string {
	if requestInfo.Name != "" || requestInfo.Verb == "create" {
		return "resource"
	}
	if requestInfo.Namespace != "" {
		return "namespace"
	}
	if requestInfo.IsResourceRequest {
		return "cluster"
	}
	// this is the empty scope
	return ""
}

func canonicalVerb(requestInfo *request.RequestInfo, scope string) string {
	verb := strings.ToUpper(requestInfo.Verb)
	if !requestInfo.IsResourceRequest {
		return verb
	}

	switch verb {
	case "GET", "HEAD":
		if scope != "resource" {
			return "LIST"
		}
		return "GET"
	default:
		return verb
	}
}

func cleanVerb(verb string, request *http.Request) string {
	reportedVerb := verb
	if verb == "LIST" {
		// see apimachinery/pkg/runtime/conversion.go Convert_Slice_string_To_bool
		if values := request.URL.Query()["watch"]; len(values) > 0 {
			if value := strings.ToLower(values[0]); value != "0" && value != "false" {
				reportedVerb = "WATCH"
			}
		}
	}
	// normalize the legacy WATCHLIST to WATCH to ensure users aren't surprised by metrics
	if verb == "WATCHLIST" {
		reportedVerb = "WATCH"
	}
	if verb == "PATCH" && request.Header.Get("Content-Type") == string(types.ApplyPatchType) && utilfeature.DefaultFeatureGate.Enabled(features.ServerSideApply) {
		reportedVerb = "APPLY"
	}
	if ValidRequestMethods.Has(reportedVerb) {
		return reportedVerb
	}
	return OtherRequestMethod
}

func cleanResource(requestInfo *request.RequestInfo) string {
	resource := "NonResourceRequest"
	if requestInfo.IsResourceRequest {
		resource = requestInfo.Resource
		if len(requestInfo.Subresource) > 0 {
			resource += "/" + requestInfo.Subresource
		}
	}
	return resource
}

// Small optimization over Itoa
func codeToString(s int) string {
	return strconv.Itoa(s)
}

// RecordRateLimiterRequest records ratelimiter requests
func RecordRateLimiterRequest(serverName string, method string, result string, flowControl string, elapsed time.Duration) {
	ProxyRateLimiterRequestCounterObservers.Observe(MetricInfo{
		ServerName:  serverName,
		Method:      method,
		Result:      result,
		FlowControl: flowControl,
		Latency:     elapsed.Seconds(),
	})
}

func RecordGlobalFlowControlAcquire(serverName string, flowControlType string, limitMethod string, flowControl string, elapsed time.Duration) {
	ProxyGlobalFlowControlAcquireObservers.Observe(MetricInfo{
		ServerName:  serverName,
		Type:        flowControlType,
		LimitMethod: limitMethod,
		FlowControl: flowControl,
		Latency:     elapsed.Seconds(),
	})
}
