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
	"os"
	"strconv"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	compbasemetrics "k8s.io/component-base/metrics"

	metricsregistry "github.com/kubewharf/kubegateway/pkg/gateway/metrics/registry"
	"github.com/kubewharf/kubegateway/pkg/util/tracing"
)

const (
	namespace = "kubegateway"
	subsystem = "proxy"
)

func init() {
	if os.Getenv("ENABLE_REQUEST_METRIC_WITH_USER") == "true" {
		enableRequestMetricByUser = true
	}
}

var (
	enableRequestMetricByUser = false

	proxyPid = strconv.Itoa(os.Getpid())

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
		[]string{"pid", "serverName", "endpoint", "verb", "resource", "code", "flowcontrol"},
	)

	proxyUserRequestCounter = compbasemetrics.NewCounterVec(
		&compbasemetrics.CounterOpts{
			Namespace:      namespace,
			Subsystem:      subsystem,
			Name:           "apiserver_request_total_with_user",
			Help:           "Counter of proxied apiserver requests by user, it is recorded when this proxied request ends",
			StabilityLevel: compbasemetrics.ALPHA,
		},
		[]string{"serverName", "verb", "resource", "code", "flowcontrol", "user"},
	)

	proxyUserRequestLoad = compbasemetrics.NewCounterVec(
		&compbasemetrics.CounterOpts{
			Namespace:      namespace,
			Subsystem:      subsystem,
			Name:           "apiserver_request_load_with_user",
			Help:           "Total time cost of proxied apiserver requests by user, it is recorded when this proxied request ends",
			StabilityLevel: compbasemetrics.ALPHA,
		},
		[]string{"serverName", "verb", "resource", "code", "flowcontrol", "user"},
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
			Buckets:        []float64{0.05, 0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 1.0, 5, 10, 30, 60, 120, 180, 600},
			StabilityLevel: compbasemetrics.ALPHA,
		},
		[]string{"pid", "serverName", "endpoint", "verb", "resource"},
	)
	proxyRequestLatenciesWithUser = compbasemetrics.NewHistogramVec(
		&compbasemetrics.HistogramOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "apiserver_request_duration_seconds_with_user",
			Help:      "Response latency distribution in seconds for each serverName, endpoint, verb, resource, user.",
			// This metric is used for verifying api call latencies SLO,
			// as well as tracking regressions in this aspects.
			// Thus we customize buckets significantly, to empower both usecases.
			Buckets:        []float64{0.01, 0.05, 0.1, 0.5, 1.0, 5, 10, 30, 60, 120, 180, 600},
			StabilityLevel: compbasemetrics.ALPHA,
		},
		[]string{"serverName", "verb", "resource", "flowcontrol", "user", "priority"},
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
		[]string{"pid", "serverName", "verb", "code", "reason", "resource", "flowcontrol"},
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

	proxyRateLimiterRequestCounter = compbasemetrics.NewCounterVec(
		&compbasemetrics.CounterOpts{
			Namespace:      namespace,
			Subsystem:      subsystem,
			Name:           "ratelimter_request_total",
			Help:           "Counter of ratelimter request, it is recorded when request ends",
			StabilityLevel: compbasemetrics.ALPHA,
		},
		[]string{"pid", "serverName", "method", "result", "flowcontrol"},
	)

	proxyGlobalFlowControlRequestCounter = compbasemetrics.NewCounterVec(
		&compbasemetrics.CounterOpts{
			Namespace:      namespace,
			Subsystem:      subsystem,
			Name:           "global_flowcontrol_acquire_total",
			Help:           "Counter of global flowcontrol request, it is recorded when request ends",
			StabilityLevel: compbasemetrics.ALPHA,
		},
		[]string{"pid", "serverName", "type", "limitMethod", "flowcontrol"},
	)

	// proxyRequestInflight is http requests number currently inflight
	proxyRequestInflight = compbasemetrics.NewGaugeVec(
		&compbasemetrics.GaugeOpts{
			Namespace:      namespace,
			Subsystem:      subsystem,
			Name:           "http_request_inflight",
			Help:           "Number of requests currently inflight",
			StabilityLevel: compbasemetrics.ALPHA,
		},
		[]string{"pid"},
	)

	// proxyRequestRate is http requests rate
	proxyRequestRate = compbasemetrics.NewGaugeVec(
		&compbasemetrics.GaugeOpts{
			Namespace:      namespace,
			Subsystem:      subsystem,
			Name:           "http_request_rate",
			Help:           "Number of requests currently inflight",
			StabilityLevel: compbasemetrics.ALPHA,
		},
		[]string{"pid"},
	)

	// proxyRequestTotalDataSize is the total http data size request and response in bytes
	proxyRequestTotalDataSize = compbasemetrics.NewGaugeVec(
		&compbasemetrics.GaugeOpts{
			Namespace:      namespace,
			Subsystem:      subsystem,
			Name:           "http_data_size_bytes_total",
			Help:           "The total http data size request and response in bytes",
			StabilityLevel: compbasemetrics.ALPHA,
		},
		[]string{"pid", "type"},
	)

	// proxyRequestDataSizeWithUser is the http data size request and response in bytes
	proxyRequestDataSizeWithUser = compbasemetrics.NewGaugeVec(
		&compbasemetrics.GaugeOpts{
			Namespace:      namespace,
			Subsystem:      subsystem,
			Name:           "http_data_size_bytes_with_user",
			Help:           "The http data size for each serverName, verb, resource, user",
			StabilityLevel: compbasemetrics.ALPHA,
		},
		[]string{"type", "serverName", "verb", "resource", "flowcontrol", "user"},
	)

	proxyHandlingLatencies = compbasemetrics.NewHistogramVec(
		&compbasemetrics.HistogramOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "handling_duration_seconds",
			Help:      "Proxy handling latency distribution in seconds for each serverName, endpoint, verb, resource, tracing stage.",
			// Start with 1ms with the last bucket being [~10s, Inf)
			Buckets:        []float64{0.001, 0.002, 0.005, 0.01, 0.02, 0.05, 0.1, 0.2, 0.5, 1.0, 5, 10},
			StabilityLevel: compbasemetrics.ALPHA,
		},
		[]string{"pid", "serverName", "verb", "resource", "stage"},
	)

	localMetrics = []compbasemetrics.Registerable{
		proxyReceiveRequestCounter,
		proxyRequestCounter,
		proxyRequestLatencies,
		proxyResponseSizes,
		proxyUpstreamUnhealthy,
		proxyRequestTerminationsTotal,
		proxyRegisteredWatchers,
		proxyRateLimiterRequestCounter,
		proxyGlobalFlowControlRequestCounter,
		proxyRequestInflight,
		proxyRequestRate,
		proxyRequestTotalDataSize,
		proxyRequestDataSizeWithUser,
		proxyHandlingLatencies,
		proxyUserRequestCounter,
		proxyUserRequestLoad,
		proxyRequestLatenciesWithUser,
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

		ProxyUpstreamUnhealthyObservers.AddObserver(&unhealthyUpstreamObserver{})
		ProxyReceiveRequestCounterObservers.AddObserver(&proxyReceiveRequestCounterObserver{})
		ProxyRequestCounterObservers.AddObserver(&proxyRequestCounterObserver{})
		ProxyRequestLatenciesObservers.AddObserver(&proxyRequestLatenciesObserver{})
		ProxyResponseSizesObservers.AddObserver(&proxyResponseSizesObserver{})
		ProxyRequestTerminationsObservers.AddObserver(&proxyRequestTerminationsObserver{})
		ProxyWatcherRegisteredObservers.AddObserver(&proxyWatcherRegisteredObserver{})
		ProxyWatcherUnregisteredObservers.AddObserver(&proxyWatcherUnregisteredObserver{})
		ProxyRateLimiterRequestCounterObservers.AddObserver(&proxyRateLimiterRequestCounterObserver{})
		ProxyGlobalFlowControlAcquireObservers.AddObserver(&proxyGlobalFlowControlAcquireObserver{})
		ProxyRequestInflightObservers.AddObserver(&proxyRequestInflightObserver{})
		ProxyRequestTotalDataSizeObservers.AddObserver(&proxyRequestTotalDataSizeObserver{})
		ProxyRequestDataSizeObservers.AddObserver(&proxyRequestDataSizeObserver{})
		ProxyHandlingLatencyObservers.AddObserver(&proxyHandlingLatenciesObserver{})
	})
}

type unhealthyUpstreamObserver struct{}

func (o *unhealthyUpstreamObserver) Observe(metric MetricInfo) {
	proxyUpstreamUnhealthy.WithLabelValues(proxyPid, metric.ServerName, metric.Endpoint, metric.Reason).Inc()
}

type proxyReceiveRequestCounterObserver struct{}

func (o *proxyReceiveRequestCounterObserver) Observe(metric MetricInfo) {
	proxyReceiveRequestCounter.WithLabelValues(proxyPid, metric.ServerName, metric.Verb, metric.Resource).Inc()
}

type proxyRequestCounterObserver struct{}

func (o *proxyRequestCounterObserver) Observe(metric MetricInfo) {
	proxyRequestCounter.WithLabelValues(proxyPid, metric.ServerName, metric.Endpoint, metric.Verb, metric.Resource, metric.HttpCode, metric.FlowControl).Inc()

	if enableRequestMetricByUser {
		proxyUserRequestCounter.WithLabelValues(metric.ServerName, metric.Verb, metric.Resource, metric.HttpCode, metric.FlowControl, metric.UserName).Inc()
		if !metric.IsLongRunning {
			proxyUserRequestLoad.WithLabelValues(metric.ServerName, metric.Verb, metric.Resource, metric.HttpCode, metric.FlowControl, metric.UserName).Add(metric.Latency)
		}
	}
}

type proxyRequestLatenciesObserver struct{}

func (o *proxyRequestLatenciesObserver) Observe(metric MetricInfo) {
	proxyRequestLatencies.WithLabelValues(proxyPid, metric.ServerName, metric.Endpoint, metric.Verb, metric.Resource).Observe(metric.Latency)

	if enableRequestMetricByUser {
		proxyRequestLatenciesWithUser.WithLabelValues(metric.ServerName, metric.Verb, metric.Resource, metric.FlowControl, metric.UserName, "default").Observe(metric.Latency)
	}
}

type proxyResponseSizesObserver struct{}

func (o *proxyResponseSizesObserver) Observe(metric MetricInfo) {
	proxyResponseSizes.WithLabelValues(proxyPid, metric.ServerName, metric.Endpoint, metric.Verb, metric.Resource).Observe(float64(metric.ResponseSize))
}

type proxyRequestTerminationsObserver struct{}

func (o *proxyRequestTerminationsObserver) Observe(metric MetricInfo) {
	proxyRequestTerminationsTotal.WithLabelValues(proxyPid, metric.ServerName, metric.Verb, metric.HttpCode, metric.Reason, metric.Resource, metric.FlowControl).Inc()
}

type proxyWatcherRegisteredObserver struct{}

func (o *proxyWatcherRegisteredObserver) Observe(metric MetricInfo) {
	proxyRegisteredWatchers.WithLabelValues(proxyPid, metric.ServerName, metric.Endpoint, metric.Resource).Inc()
}

type proxyWatcherUnregisteredObserver struct{}

func (o *proxyWatcherUnregisteredObserver) Observe(metric MetricInfo) {
	proxyRegisteredWatchers.WithLabelValues(proxyPid, metric.ServerName, metric.Endpoint, metric.Resource).Dec()
}

type proxyRateLimiterRequestCounterObserver struct{}

func (o *proxyRateLimiterRequestCounterObserver) Observe(metric MetricInfo) {
	proxyRateLimiterRequestCounter.WithLabelValues(proxyPid, metric.ServerName, metric.Method, metric.Result, metric.FlowControl).Inc()
}

type proxyGlobalFlowControlAcquireObserver struct{}

func (o *proxyGlobalFlowControlAcquireObserver) Observe(metric MetricInfo) {
	proxyGlobalFlowControlRequestCounter.WithLabelValues(proxyPid, metric.ServerName, metric.Type, metric.LimitMethod, metric.FlowControl).Inc()
}

type proxyRequestInflightObserver struct{}

func (o *proxyRequestInflightObserver) Observe(metric MetricInfo) {
	proxyRequestInflight.WithLabelValues(proxyPid).Set(metric.Inflight)
	proxyRequestRate.WithLabelValues(proxyPid).Set(metric.Rate)
}

type proxyRequestTotalDataSizeObserver struct{}

func (o *proxyRequestTotalDataSizeObserver) Observe(metric MetricInfo) {
	proxyRequestTotalDataSize.WithLabelValues(proxyPid, "request").Set(float64(metric.RequestSize))
	proxyRequestTotalDataSize.WithLabelValues(proxyPid, "response").Set(float64(metric.ResponseSize))
}

type proxyRequestDataSizeObserver struct{}

func (o *proxyRequestDataSizeObserver) Observe(metric MetricInfo) {
	if enableRequestMetricByUser {
		proxyRequestDataSizeWithUser.WithLabelValues("request", metric.ServerName, metric.Verb, metric.Resource, metric.FlowControl, metric.UserName).Set(float64(metric.RequestSize))
		proxyRequestDataSizeWithUser.WithLabelValues("response", metric.ServerName, metric.Verb, metric.Resource, metric.FlowControl, metric.UserName).Set(float64(metric.ResponseSize))
	}
}

type proxyHandlingLatenciesObserver struct{}

func (o *proxyHandlingLatenciesObserver) Observe(metric MetricInfo) {
	proxyHandlingLatencies.WithLabelValues(proxyPid, metric.ServerName, metric.Verb, metric.Resource, tracing.MetricStageHandlingDelay).
		Observe(metric.TraceLatencies[tracing.MetricStageHandlingDelay].Seconds())
}
