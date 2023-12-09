package endpoints

import (
	"github.com/kubewharf/kubegateway/pkg/ratelimiter/endpoints/dispather"
	"github.com/kubewharf/kubegateway/pkg/ratelimiter/endpoints/filters"
	"net/http"
	goruntime "runtime"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	genericapifilters "k8s.io/apiserver/pkg/endpoints/filters"
	apirequest "k8s.io/apiserver/pkg/endpoints/request"
	apiserver "k8s.io/apiserver/pkg/server"
	genericfilters "k8s.io/apiserver/pkg/server/filters"
	"k8s.io/apiserver/pkg/server/healthz"
	"k8s.io/apiserver/pkg/server/mux"
	"k8s.io/apiserver/pkg/server/routes"
	componentbaseconfig "k8s.io/component-base/config"
	"k8s.io/component-base/metrics/legacyregistry"
	"k8s.io/kubernetes/pkg/api/legacyscheme"
	"k8s.io/kubernetes/pkg/util/configz"

	"github.com/kubewharf/kubegateway/pkg/ratelimiter/limiter"
)

// BuildHandlerChain builds a handler chain with a base handler and CompletedConfig.
func BuildHandlerChain(apiHandler http.Handler, rateLimiter limiter.RateLimiter,
	authorizationInfo *apiserver.AuthorizationInfo, authenticationInfo *apiserver.AuthenticationInfo, requestInfoResolver apirequest.RequestInfoResolver) http.Handler {
	handler := apiHandler

	handler = dispather.WithLimiterDispatcher(handler, rateLimiter)

	if authorizationInfo != nil {
		handler = genericapifilters.WithAuthorization(handler, authorizationInfo.Authorizer, legacyscheme.Codecs)
	}
	if authenticationInfo != nil {
		failedHandler := genericapifilters.Unauthorized(legacyscheme.Codecs, false)
		handler = genericapifilters.WithAuthentication(handler, authenticationInfo.Authenticator, failedHandler, nil)
	}

	handler = filters.WithRequestMetric(handler)
	handler = filters.WithExtraRequestInfo(handler)
	handler = filters.WithRequestInfo(handler, requestInfoResolver)
	handler = genericapifilters.WithCacheControl(handler)
	handler = genericfilters.WithPanicRecovery(handler)
	return handler
}

// NewBaseHandler takes in CompletedConfig and returns a handler.
func NewBaseHandler(c *componentbaseconfig.DebuggingConfiguration, checks ...healthz.HealthChecker) *mux.PathRecorderMux {
	mux := mux.NewPathRecorderMux("rate-limiter")
	healthz.InstallHandler(mux, checks...)
	if c.EnableProfiling {
		routes.Profiling{}.Install(mux)
		if c.EnableContentionProfiling {
			goruntime.SetBlockProfileRate(1)
		}
	}
	configz.InstallHandler(mux)
	//lint:ignore SA1019 See the Metrics Stability Migration KEP
	mux.Handle("/metrics", promhttp.HandlerFor(legacyregistry.DefaultGatherer, promhttp.HandlerOpts{}))

	return mux
}
