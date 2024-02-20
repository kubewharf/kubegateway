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

package app

import (
	"bytes"
	"log"
	"net/http"

	"github.com/kubewharf/apiserver-runtime/pkg/scheme"
	apiserver "github.com/kubewharf/apiserver-runtime/pkg/server"
	recommendedoptions "github.com/kubewharf/apiserver-runtime/pkg/server/options"
	genericapifilters "k8s.io/apiserver/pkg/endpoints/filters"
	genericapiserver "k8s.io/apiserver/pkg/server"
	genericfilters "k8s.io/apiserver/pkg/server/filters"
	_ "k8s.io/component-base/metrics/prometheus/workqueue" // for workqueue metric registration
	"k8s.io/klog"
	"k8s.io/kube-openapi/pkg/common"

	"github.com/kubewharf/kubegateway/cmd/kube-gateway/app/options"
	"github.com/kubewharf/kubegateway/pkg/clusters"
	"github.com/kubewharf/kubegateway/pkg/gateway/controllers"
	controlplaneserver "github.com/kubewharf/kubegateway/pkg/gateway/controlplane"
	gatewayfilters "github.com/kubewharf/kubegateway/pkg/gateway/endpoints/filters"
	"github.com/kubewharf/kubegateway/pkg/gateway/endpoints/monitor"
	"github.com/kubewharf/kubegateway/pkg/gateway/endpoints/request"
	proxyserver "github.com/kubewharf/kubegateway/pkg/gateway/proxy"
	proxydispatcher "github.com/kubewharf/kubegateway/pkg/gateway/proxy/dispatcher"
	nativeopenapi "github.com/kubewharf/kubegateway/staging/src/k8s.io/openapi/generated/openapi"
)

func CreateProxyConfig(
	o *options.ProxyOptions,
	controlplaneOptions *options.ControlPlaneServerRunOptions,
	controlplaneServerConfig *controlplaneserver.Config,
) (serverConfig *proxyserver.Config, lastErr error) {
	recommendedConfig := apiserver.NewRecommendedConfig(scheme.Scheme, scheme.Codecs)
	// NOTE: set loopback client config ortherwise error will occur when creating a new generic apiserver
	recommendedConfig.LoopbackClientConfig = controlplaneServerConfig.RecommendedConfig.LoopbackClientConfig
	// enable all master default api resources
	recommendedConfig.Config.MergedResourceConfig = proxyserver.DefaultAPIResourceConfigSource()
	// openapi
	recommendedConfig.WithOpenapiConfig("KubeGatewayProxy", GetNativeOpenAPIDefinitions)

	// disable default metric, metrics api will be installed when new proxy server
	recommendedConfig.EnableMetrics = false

	if lastErr = o.SecureServing.ApplyTo(&recommendedConfig.SecureServing, *controlplaneOptions.SecureServing); lastErr != nil {
		return
	}

	// customize http error log to filter out some noisy log
	// referred to k8s.io/component-base/logs/logs.go#InitLogs()
	controlplaneServerConfig.RecommendedConfig.SecureServing.ErrorLog = log.New(proxyHTTPErrorLogWriter{}, "", 0)
	log.SetOutput(proxyHTTPErrorLogWriter{})

	// create upstream controller
	clusterController := controllers.NewUpstreamClusterController(
		controlplaneServerConfig.ExtraConfig.GatewaySharedInformerFactory.Proxy().V1alpha1().UpstreamClusters(),
		o.RateLimiter,
	)
	// Dynamic SNI for upstream cluster
	recommendedConfig.Config.SecureServing.DynamicClientConfig = clusterController
	// Proxy handler
	oo := &proxyHandlerOptions{
		clusterManager:           clusterController,
		enableProxyTracing:       o.Tracing.EnableProxyTracing,
		enableAccessLog:          o.Logging.EnableProxyAccessLog,
		maxInflightThreshold:     o.ServerRun.MaxInflightThreshold,
		maxQPSThreshold:          o.ServerRun.MaxQPSThreshold,
		maxThroughputMBThreshold: o.ServerRun.MaxThroughputMBThreshold,
		goawayChance:             o.ServerRun.GoawayChance,
	}
	recommendedConfig.Config.BuildHandlerChainFunc = buildProxyHandlerChainFunc(oo)

	// Proxy authentication
	if lastErr = o.Authentication.ApplyTo(
		&recommendedConfig.Authentication,
		recommendedConfig.SecureServing,
		recommendedConfig.OpenAPIConfig,
		clusterController,
		clusterController,
		controlplaneOptions.Authentication,
	); lastErr != nil {
		return
	}

	// Proxy authorization
	if lastErr = o.Authorization.ApplyTo(&recommendedConfig.Config, clusterController); lastErr != nil {
		return
	}

	// apply other useful options
	recommenedOptions := buildProxyRecommenedOptions(o, controlplaneOptions)
	if lastErr = recommenedOptions.ApplyTo(recommendedConfig, nil, nil); lastErr != nil {
		return
	}

	serverConfig = &proxyserver.Config{
		RecommendedConfig: recommendedConfig,
		ExtraConfig: proxyserver.ExtraConfig{
			UpstreamClusterController: clusterController,
		},
	}
	return serverConfig, nil
}

func buildProxyRecommenedOptions(o *options.ProxyOptions, controlplaneOptions *options.ControlPlaneServerRunOptions) *recommendedoptions.RecommendedOptions {
	recommenedOptions := recommendedoptions.NewRecommendedOptions().WithProcessInfo(o.ProcessInfo)
	recommenedOptions.ServerRun = controlplaneOptions.ServerRun
	recommenedOptions.FeatureGate = controlplaneOptions.FeatureGate
	recommenedOptions.Features = controlplaneOptions.Features
	// TODO: add other config
	return recommenedOptions
}

type proxyHandlerOptions struct {
	clusterManager           clusters.Manager
	enableProxyTracing       bool
	enableAccessLog          bool
	maxInflightThreshold     int32
	maxQPSThreshold          int32
	maxThroughputMBThreshold int32
	goawayChance             float64
}

func buildProxyHandlerChainFunc(o *proxyHandlerOptions) func(apiHandler http.Handler, c *genericapiserver.Config) http.Handler {
	return func(apiHandler http.Handler, c *genericapiserver.Config) http.Handler {
		// new gateway handler chain
		handler := gatewayfilters.WithDispatcher(apiHandler, proxydispatcher.NewDispatcher(o.clusterManager, o.enableAccessLog))
		// without impersonation log
		handler = gatewayfilters.WithNoLoggingImpersonation(handler, c.Authorization.Authorizer, c.Serializer)
		// new gateway handler chain, add impersonator userInfo
		handler = gatewayfilters.WithImpersonator(handler)
		handler = genericapifilters.WithAudit(handler, c.AuditBackend, c.AuditPolicyChecker, c.LongRunningFunc)
		failedHandler := genericapifilters.Unauthorized(c.Serializer, c.Authentication.SupportsBasicAuth)
		failedHandler = genericapifilters.WithFailedAuthenticationAudit(failedHandler, c.AuditBackend, c.AuditPolicyChecker)
		handler = genericapifilters.WithAuthentication(handler, c.Authentication.Authenticator, failedHandler, c.Authentication.APIAudiences)
		handler = genericfilters.WithCORS(handler, c.CorsAllowedOriginList, nil, nil, nil, "true")
		// disabel timeout, let upstream cluster handle it
		// handler = gatewayfilters.WithTimeoutForNonLongRunningRequests(handler, c.LongRunningFunc, c.RequestTimeout)
		handler = genericfilters.WithWaitGroup(handler, c.LongRunningFunc, c.HandlerChainWaitGroup)
		// new gateway handler chain

		// rate and throughput monitor
		throughputMonitor := monitor.NewThroughputMonitor()
		rateMonitor := monitor.NewRateMonitor()
		handler = gatewayfilters.WithRequestThroughput(handler, throughputMonitor)
		handler = gatewayfilters.WithRequestRate(handler, c.LongRunningFunc, rateMonitor)

		handler = gatewayfilters.WithPreProcessingMetrics(handler)
		handler = gatewayfilters.WithTraceLog(handler, o.enableProxyTracing, c.LongRunningFunc)
		handler = gatewayfilters.WithUpstreamInfo(handler, o.clusterManager, c.Serializer)
		handler = gatewayfilters.WithExtraRequestInfo(handler, &request.ExtraRequestInfoFactory{}, c.Serializer)
		handler = gatewayfilters.WithTerminationMetrics(handler)
		handler = gatewayfilters.WithRequestInfo(handler, c.RequestInfoResolver)
		if c.SecureServing != nil && !c.SecureServing.DisableHTTP2 && o.goawayChance > 0 {
			handler = gatewayfilters.WithLoadPressureGoaway(handler, o.maxInflightThreshold, o.maxQPSThreshold, o.maxThroughputMBThreshold, o.goawayChance, rateMonitor, throughputMonitor)
		}
		handler = genericapifilters.WithCacheControl(handler)
		handler = gatewayfilters.WithNoLoggingPanicRecovery(handler)
		return handler
	}
}

func GetNativeOpenAPIDefinitions(ref common.ReferenceCallback) map[string]common.OpenAPIDefinition {
	return nativeopenapi.GetOpenAPIDefinitions(ref)
}

// proxyHTTPErrorLogWriter serves as a bridge between the standard log package and the klog package.
// It also filter out some noisy http error log
type proxyHTTPErrorLogWriter struct{}

// Write implements the io.Writer interface.
func (writer proxyHTTPErrorLogWriter) Write(data []byte) (n int, err error) {
	if bytes.HasPrefix(data, []byte("http: TLS handshake error from")) {
		return 0, nil
	}
	klog.InfoDepth(4, string(data))
	return len(data), nil
}
