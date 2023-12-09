package config

import (
	"github.com/kubewharf/kubegateway/pkg/ratelimiter/endpoints"
	"k8s.io/apimachinery/pkg/util/sets"
	apirequest "k8s.io/apiserver/pkg/endpoints/request"
	apiserver "k8s.io/apiserver/pkg/server"
	"k8s.io/apiserver/pkg/server/healthz"
	serverstorage "k8s.io/apiserver/pkg/server/storage"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	componentbaseconfig "k8s.io/component-base/config"

	"github.com/kubewharf/kubegateway/pkg/apis/proxy/v1alpha1"
	gatewayclientset "github.com/kubewharf/kubegateway/pkg/client/kubernetes"
	"github.com/kubewharf/kubegateway/pkg/ratelimiter"
	"github.com/kubewharf/kubegateway/pkg/ratelimiter/limiter"
)

// Config has all the context to run a rate limiter
type Config struct {
	SecureServing   *apiserver.SecureServingInfo
	InsecureServing *apiserver.DeprecatedInsecureServingInfo
	Authentication  apiserver.AuthenticationInfo
	Authorization   apiserver.AuthorizationInfo
	Debugging       *componentbaseconfig.DebuggingConfiguration

	Client           kubernetes.Interface
	InformerFactory  informers.SharedInformerFactory
	GatewayClientset gatewayclientset.Interface
	RateLimiter      limiter.RateLimiter
}

// CompletedConfig same as Config, just to swap private object.
type CompletedConfig struct {
	*Config
}

// Complete fills in any fields not set that are required to have valid data. It's mutating the receiver.
func (c *Config) Complete() CompletedConfig {
	return CompletedConfig{c}
}

func (c *CompletedConfig) New(name string) (*ratelimiter.Server, error) {
	rateLimiter := &ratelimiter.Server{
		Name:            name,
		SecureServing:   c.Config.SecureServing,
		InsecureServing: c.Config.InsecureServing,
		Client:          c.Client,
		InformerFactory: c.InformerFactory,
		RateLimiter:     c.RateLimiter,
	}

	requestInfoResolver := &apirequest.RequestInfoFactory{
		APIPrefixes:          sets.NewString("api", "apis"),
		GrouplessAPIPrefixes: sets.NewString("api"),
	}

	// TODO setup healthz checks.
	var checks []healthz.HealthChecker

	unsecuredMux := endpoints.NewBaseHandler(c.Debugging, checks...)
	if c.SecureServing != nil {
		rateLimiter.SecureHandler = endpoints.BuildHandlerChain(unsecuredMux, c.RateLimiter, &c.Authorization, &c.Authentication, requestInfoResolver)
	}

	if c.InsecureServing != nil {
		insecureSuperuserAuthn := apiserver.AuthenticationInfo{Authenticator: &apiserver.InsecureSuperuser{}}
		rateLimiter.InsecureHandler = endpoints.BuildHandlerChain(unsecuredMux, c.RateLimiter, nil, &insecureSuperuserAuthn, requestInfoResolver)
	}

	return rateLimiter, nil
}

func defaultAPIResourceConfigSource() *serverstorage.ResourceConfig {
	ret := serverstorage.NewResourceConfig()
	// NOTE: GroupVersions listed here will be enabled by default. Don't put alpha versions in the list.
	ret.EnableVersions(
		v1alpha1.SchemeGroupVersion,
	)

	return ret
}
