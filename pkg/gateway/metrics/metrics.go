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
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/apimachinery/pkg/types"
	utilsets "k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apiserver/pkg/endpoints/request"
	genericapirequest "k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/apiserver/pkg/features"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	compbasemetrics "k8s.io/component-base/metrics"

	metricsregistry "github.com/kubewharf/kubegateway/pkg/gateway/metrics/registry"
	"github.com/kubewharf/kubegateway/pkg/gateway/net"
)

const (
	OtherRequestMethod string = "other"

	namespace = "kubegateway"
	subsystem = "proxy"
)

var (
	proxyPid = strconv.Itoa(os.Getpid())

	// these are the valid request methods which we report in our metrics. Any other request methods
	// will be aggregated under 'unknown'
	validRequestMethods = utilsets.NewString(
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
		"WATCHLIST")

	proxyReceiveRequestCounter = compbasemetrics.NewCounterVec(
		&compbasemetrics.CounterOpts{
			Namespace:      namespace,
			Subsystem:      subsystem,
			Name:           "received_apiserver_request_total",
			Help:           "Counter of received apiserver requests, it is recorded when this request occurs",
			StabilityLevel: compbasemetrics.ALPHA,
		},
		[]string{"pid", "serverName", "verb", "resource"},
	)
	proxyRequestCounter = compbasemetrics.NewCounterVec(
		&compbasemetrics.CounterOpts{
			Namespace:      namespace,
			Subsystem:      subsystem,
			Name:           "apiserver_request_total",
			Help:           "Counter of proxied apiserver requests, it is recorded when this proxied request ends",
			StabilityLevel: compbasemetrics.ALPHA,
		},
		[]string{"pid", "serverName", "endpoint", "verb", "resource", "code"},
	)
	proxyRequestLatencies = compbasemetrics.NewHistogramVec(
		&compbasemetrics.HistogramOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "apiserver_request_duration_seconds",
			Help:      "Response latency distribution in seconds for each serverName, endpoint, verb, resource.",
			// This metric is used for verifying api call latencies SLO,
			// as well as tracking regressions in this aspects.
			// Thus we customize buckets significantly, to empower both usecases.
			Buckets: []float64{0.05, 0.1, 0.15, 0.2, 0.25, 0.3, 0.35, 0.4, 0.45, 0.5, 0.6, 0.7, 0.8, 0.9, 1.0,
				1.25, 1.5, 1.75, 2.0, 2.5, 3.0, 3.5, 4.0, 4.5, 5, 6, 7, 8, 9, 10, 15, 20, 25, 30, 40, 50, 60, 120, 180, 240, 300},
			StabilityLevel: compbasemetrics.ALPHA,
		},
		[]string{"pid", "serverName", "endpoint", "verb", "resource"},
	)
	proxyResponseSizes = compbasemetrics.NewHistogramVec(
		&compbasemetrics.HistogramOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "apiserver_response_sizes",
			Help:      "Response size distribution in bytes for each group, version, verb, resource, subresource, scope and component.",
			// Use buckets ranging from 1000 bytes (1KB) to 10^9 bytes (1GB).
			Buckets:        prometheus.ExponentialBuckets(1000, 10.0, 7),
			StabilityLevel: compbasemetrics.ALPHA,
		},
		[]string{"pid", "serverName", "endpoint", "verb", "resource"},
	)
	proxyUpstreamUnhealthy = compbasemetrics.NewCounterVec(
		&compbasemetrics.CounterOpts{
			Namespace:      namespace,
			Subsystem:      subsystem,
			Name:           "upstream_unhealthy",
			Help:           "Number of unhealthy upstream endpoint detection",
			StabilityLevel: compbasemetrics.ALPHA,
		},
		[]string{"pid", "serverName", "endpoint", "reason"},
	)
	proxyRequestTerminationsTotal = compbasemetrics.NewCounterVec(
		&compbasemetrics.CounterOpts{
			Namespace:      namespace,
			Subsystem:      subsystem,
			Name:           "apiserver_request_terminations_total",
			Help:           "Number of requests which proxy terminated in self-defense.",
			StabilityLevel: compbasemetrics.ALPHA,
		},
		[]string{"pid", "serverName", "verb", "path", "code", "reason"},
	)
	// proxyRegisteredWatchers is a number of currently registered watchers splitted by resource.
	proxyRegisteredWatchers = compbasemetrics.NewGaugeVec(
		&compbasemetrics.GaugeOpts{
			Namespace:      namespace,
			Subsystem:      subsystem,
			Name:           "apiserver_registered_watchers",
			Help:           "Number of currently registered watchers for a given resources",
			StabilityLevel: compbasemetrics.ALPHA,
		},
		[]string{"pid", "serverName", "endpoint", "resource"},
	)

	localMetrics = []compbasemetrics.Registerable{
		proxyReceiveRequestCounter,
		proxyRequestCounter,
		proxyRequestLatencies,
		proxyResponseSizes,
		proxyUpstreamUnhealthy,
		proxyRequestTerminationsTotal,
		proxyRegisteredWatchers,
	}
)

var registerMetrics sync.Once

func init() {
	Register()
}

// Register all metrics.
func Register() {
	registerMetrics.Do(func() {
		for _, metric := range localMetrics {
			metricsregistry.MustRegister(metric)
		}
	})
}

// RecordUnhealthyUpstream records that the upstream endpoint is unhealthy.
func RecordUnhealthyUpstream(serverName string, endpoint string, reason string) {
	proxyUpstreamUnhealthy.WithLabelValues(proxyPid, serverName, endpoint, reason).Inc()
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
	proxyReceiveRequestCounter.WithLabelValues(proxyPid, serverName, verb, resource).Inc()
}

// MonitorProxyRequest handles standard transformations for client and the reported verb and then invokes Monitor to record
// a request. verb must be uppercase to be backwards compatible with existing monitoring tooling.
func MonitorProxyRequest(req *http.Request, serverName, endpoint string, requestInfo *request.RequestInfo, contentType string, httpCode, respSize int, elapsed time.Duration) {
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
	proxyRequestCounter.WithLabelValues(proxyPid, serverName, endpoint, verb, resource, codeToString(httpCode)).Inc()
	proxyRequestLatencies.WithLabelValues(proxyPid, serverName, endpoint, verb, resource).Observe(elapsedSeconds)
	// We are only interested in response sizes of read requests.
	// nolint:goconst
	if requestInfo.IsResourceRequest && (verb == "GET" || verb == "LIST") {
		proxyResponseSizes.WithLabelValues(proxyPid, serverName, endpoint, verb, resource).Observe(float64(respSize))
	}
}

// RecordProxyRequestTermination records that the request was terminated early as part of a resource
// preservation or apiserver self-defense mechanism (e.g. timeouts, maxinflight throttling,
// proxyHandler errors). RecordProxyRequestTermination should only be called zero or one times
// per request.
func RecordProxyRequestTermination(req *http.Request, code int, reason string) {
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
	if !validRequestMethods.Has(verb) {
		verb = OtherRequestMethod
	}
	serverName := net.HostWithoutPort(req.Host)
	proxyRequestTerminationsTotal.WithLabelValues(proxyPid, serverName, cleanVerb(verb, req), requestInfo.Path, codeToString(code), reason).Inc()
}

func RecordWatcherRegistered(serverName, endpoint, resource string) {
	proxyRegisteredWatchers.WithLabelValues(proxyPid, serverName, endpoint, resource).Inc()
}

func RecordWatcherUnregistered(serverName, endpoint, resource string) {
	proxyRegisteredWatchers.WithLabelValues(proxyPid, serverName, endpoint, resource).Dec()
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
	if validRequestMethods.Has(reportedVerb) {
		return reportedVerb
	}
	return OtherRequestMethod
}

// Small optimization over Itoa
func codeToString(s int) string {
	switch s {
	case 100:
		return "100"
	case 101:
		return "101"

	case 200:
		return "200"
	case 201:
		return "201"
	case 202:
		return "202"
	case 203:
		return "203"
	case 204:
		return "204"
	case 205:
		return "205"
	case 206:
		return "206"

	case 300:
		return "300"
	case 301:
		return "301"
	case 302:
		return "302"
	case 304:
		return "304"
	case 305:
		return "305"
	case 307:
		return "307"

	case 400:
		return "400"
	case 401:
		return "401"
	case 402:
		return "402"
	case 403:
		return "403"
	case 404:
		return "404"
	case 405:
		return "405"
	case 406:
		return "406"
	case 407:
		return "407"
	case 408:
		return "408"
	case 409:
		return "409"
	case 410:
		return "410"
	case 411:
		return "411"
	case 412:
		return "412"
	case 413:
		return "413"
	case 414:
		return "414"
	case 415:
		return "415"
	case 416:
		return "416"
	case 417:
		return "417"
	case 418:
		return "418"

	case 500:
		return "500"
	case 501:
		return "501"
	case 502:
		return "502"
	case 503:
		return "503"
	case 504:
		return "504"
	case 505:
		return "505"

	case 428:
		return "428"
	case 429:
		return "429"
	case 431:
		return "431"
	case 511:
		return "511"

	default:
		return strconv.Itoa(s)
	}
}
