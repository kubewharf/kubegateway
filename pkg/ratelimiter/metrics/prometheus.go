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
	compbasemetrics "k8s.io/component-base/metrics"
	"k8s.io/component-base/metrics/legacyregistry"
	"sync"
)

const (
	namespace = "kubegateway"
	subsystem = "ratelimiter"
)

var (
	RateLimiterRequestCounterObservers        = newUnionObserver()
	RateLimiterRequestLatenciesObservers      = newUnionObserver()
	RateLimiterResponseSizesObservers         = newUnionObserver()
	RateLimiterAllocatedLimitsObservers       = newUnionObserver()
	RateLimiterAllocatedTokenCounterObservers = newUnionObserver()
	RateLimiterLeaderElectionObservers        = newUnionObserver()
)

var (
	rateLimiterRequestCounter = compbasemetrics.NewCounterVec(
		&compbasemetrics.CounterOpts{
			Namespace:      namespace,
			Name:           "ratelimiter_request_total",
			Help:           "Counter of ratelimiter requests, it is recorded when request ends",
			StabilityLevel: compbasemetrics.ALPHA,
		},
		[]string{"serverName", "clientId", "verb", "resource", "code"},
	)
	rateLimiterRequestLatencies = compbasemetrics.NewHistogramVec(
		&compbasemetrics.HistogramOpts{
			Namespace: namespace,
			Name:      "ratelimiter_request_duration_seconds",
			Help:      "Response latency distribution in seconds for each serverName, clientId, verb, resource, code.",
			// This metric is used for verifying api call latencies SLO,
			// as well as tracking regressions in this aspects.
			// Thus we customize buckets significantly, to empower both usecases.
			Buckets: []float64{0.05, 0.1, 0.15, 0.2, 0.25, 0.3, 0.35, 0.4, 0.45, 0.5, 0.6, 0.7, 0.8, 0.9, 1.0,
				1.25, 1.5, 1.75, 2.0, 2.5, 3.0, 3.5, 4.0, 4.5, 5, 6, 7, 8, 9, 10, 15, 20, 25, 30, 40, 50, 60, 120, 180, 240, 300},
			StabilityLevel: compbasemetrics.ALPHA,
		},
		[]string{"serverName", "clientId", "verb", "resource"},
	)

	rateLimiterResponseSizeTotal = compbasemetrics.NewCounterVec(
		&compbasemetrics.CounterOpts{
			Namespace:      namespace,
			Name:           "ratelimiter_response_size_bytes",
			Help:           "Counter of ratelimiter requests, it is recorded when request ends",
			StabilityLevel: compbasemetrics.ALPHA,
		},
		[]string{"serverName", "clientId", "verb", "resource"},
	)

	rateLimiterAllocatedLimits = compbasemetrics.NewGaugeVec(
		&compbasemetrics.GaugeOpts{
			Namespace:      namespace,
			Name:           "ratelimiter_allocated_limit",
			Help:           "Allocated limits token bucket for each serverName, clientId, flowcontrol, type, strategy, method.",
			StabilityLevel: compbasemetrics.ALPHA,
		},
		[]string{"serverName", "clientId", "flowcontrol", "type", "strategy", "method"},
	)

	rateLimiterAllocatedTokenCounter = compbasemetrics.NewCounterVec(
		&compbasemetrics.CounterOpts{
			Namespace:      namespace,
			Name:           "ratelimiter_allocated_token_total",
			Help:           "Total allocated tokens for each serverName, clientId, flowcontrol, type, strategy, method.",
			StabilityLevel: compbasemetrics.ALPHA,
		},
		[]string{"serverName", "clientId", "flowcontrol", "type", "strategy", "method"},
	)

	rateLimiterLeaderElection = compbasemetrics.NewGaugeVec(
		&compbasemetrics.GaugeOpts{
			Namespace: namespace,
			Name:      "ratelimiter_leader_election_status",
			Help:      "Gauge of ratelimter leader election info",
		},
		[]string{"shard", "leader"},
	)

	localMetrics = []compbasemetrics.Registerable{
		rateLimiterRequestCounter,
		rateLimiterRequestLatencies,
		rateLimiterResponseSizeTotal,
		rateLimiterAllocatedLimits,
		rateLimiterAllocatedTokenCounter,
		rateLimiterLeaderElection,
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
			legacyregistry.MustRegister(metric)
		}

		RateLimiterRequestCounterObservers.AddObserver(&rateLimiterRequestCounterObserver{})
		RateLimiterRequestLatenciesObservers.AddObserver(&rateLimiterRequestLatenciesObserver{})
		RateLimiterResponseSizesObservers.AddObserver(&rateLimiterResponseSizesObserver{})
		RateLimiterAllocatedLimitsObservers.AddObserver(&rateLimiterAllocatedLimitsObserver{})
		RateLimiterAllocatedTokenCounterObservers.AddObserver(&rateLimiterAllocatedTokenCounterObserver{})
		RateLimiterLeaderElectionObservers.AddObserver(&rateLimiterLeaderElectionObserver{})
	})
}

type rateLimiterRequestCounterObserver struct{}

func (o *rateLimiterRequestCounterObserver) Observe(metric MetricInfo) {
	rateLimiterRequestCounter.WithLabelValues(metric.ServerName, metric.ClientID, metric.Verb, metric.Resource, metric.HttpCode).Inc()
}

type rateLimiterRequestLatenciesObserver struct{}

func (o *rateLimiterRequestLatenciesObserver) Observe(metric MetricInfo) {
	rateLimiterRequestLatencies.WithLabelValues(metric.ServerName, metric.ClientID, metric.Verb, metric.Resource).Observe(metric.Latency)
}

type rateLimiterResponseSizesObserver struct{}

func (o *rateLimiterResponseSizesObserver) Observe(metric MetricInfo) {
	rateLimiterResponseSizeTotal.WithLabelValues(metric.ServerName, metric.ClientID, metric.Verb, metric.Resource).Add(metric.ResponseSize)
}

type rateLimiterAllocatedLimitsObserver struct{}

func (o *rateLimiterAllocatedLimitsObserver) Observe(metric MetricInfo) {
	rateLimiterAllocatedLimits.WithLabelValues(metric.ServerName, metric.ClientID, metric.FlowControl, metric.FlowControlType, metric.Strategy, metric.Method).Set(metric.Limit)
}

type rateLimiterAllocatedTokenCounterObserver struct{}

func (o *rateLimiterAllocatedTokenCounterObserver) Observe(metric MetricInfo) {
	rateLimiterAllocatedTokenCounter.WithLabelValues(metric.ServerName, metric.ClientID, metric.FlowControl, metric.FlowControlType, metric.Strategy, metric.Method).Add(metric.Limit)
}

type rateLimiterLeaderElectionObserver struct{}

func (o *rateLimiterLeaderElectionObserver) Observe(metric MetricInfo) {
	rateLimiterLeaderElection.WithLabelValues(metric.Shard, metric.Leader).Set(float64(metric.LeaderState))
}
