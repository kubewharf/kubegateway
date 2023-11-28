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
	"github.com/prometheus/client_golang/prometheus"
	compbasemetrics "k8s.io/component-base/metrics"
	"os"
	"strconv"
	"sync"

	metricsregistry "github.com/kubewharf/kubegateway/pkg/gateway/metrics/registry"
)

const (
	namespace = "kubegateway"
	subsystem = "proxy"
)

var (
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
		[]string{"pid", "serverName", "verb", "path", "code", "reason", "resource"},
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

		ProxyUpstreamUnhealthyObservers.AddObserver(&unhealthyUpstreamObserver{})
		ProxyReceiveRequestCounterObservers.AddObserver(&proxyReceiveRequestCounterObserver{})
		ProxyRequestCounterObservers.AddObserver(&proxyRequestCounterObserver{})
		ProxyRequestLatenciesObservers.AddObserver(&proxyRequestLatenciesObserver{})
		ProxyResponseSizesObservers.AddObserver(&proxyResponseSizesObserver{})
		ProxyRequestTerminationsObservers.AddObserver(&proxyRequestTerminationsObserver{})
		ProxyWatcherRegisteredObservers.AddObserver(&proxyWatcherRegisteredObserver{})
		ProxyWatcherUnregisteredObservers.AddObserver(&proxyWatcherUnregisteredObserver{})

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
	proxyRequestCounter.WithLabelValues(proxyPid, metric.ServerName, metric.Endpoint, metric.Verb, metric.Resource, metric.HttpCode).Inc()
}

type proxyRequestLatenciesObserver struct{}

func (o *proxyRequestLatenciesObserver) Observe(metric MetricInfo) {
	proxyRequestLatencies.WithLabelValues(proxyPid, metric.ServerName, metric.Endpoint, metric.Verb, metric.Resource).Observe(metric.Latency)
}

type proxyResponseSizesObserver struct{}

func (o *proxyResponseSizesObserver) Observe(metric MetricInfo) {
	proxyResponseSizes.WithLabelValues(proxyPid, metric.ServerName, metric.Endpoint, metric.Verb, metric.Resource).Observe(metric.ResponseSize)
}

type proxyRequestTerminationsObserver struct{}

func (o *proxyRequestTerminationsObserver) Observe(metric MetricInfo) {
	proxyRequestTerminationsTotal.WithLabelValues(proxyPid, metric.ServerName, metric.Verb, metric.Path, metric.HttpCode, metric.Reason, metric.Resource).Inc()
}

type proxyWatcherRegisteredObserver struct{}

func (o *proxyWatcherRegisteredObserver) Observe(metric MetricInfo) {
	proxyRegisteredWatchers.WithLabelValues(proxyPid, metric.ServerName, metric.Endpoint, metric.Resource).Inc()
}

type proxyWatcherUnregisteredObserver struct{}

func (o *proxyWatcherUnregisteredObserver) Observe(metric MetricInfo) {
	proxyRegisteredWatchers.WithLabelValues(proxyPid, metric.ServerName, metric.Endpoint, metric.Resource).Dec()
}
