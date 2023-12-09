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

package options

import (
	limitconfig "github.com/kubewharf/kubegateway/pkg/ratelimiter/config"
	"github.com/kubewharf/kubegateway/pkg/ratelimiter/limiter"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"net"
	"time"

	apiserveroptions "k8s.io/apiserver/pkg/server/options"
	clientset "k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	cliflag "k8s.io/component-base/cli/flag"
	componentbaseconfig "k8s.io/component-base/config"
	"k8s.io/klog"

	gatewayclientset "github.com/kubewharf/kubegateway/pkg/client/kubernetes"
	"github.com/kubewharf/kubegateway/pkg/ratelimiter/options"
)

type Options struct {
	// ClientConnection specifies the kubeconfig file and client connection
	// settings for the proxy server to use when communicating with the apiserver.
	ClientConnection componentbaseconfig.ClientConnectionConfiguration
	// LimitServer defines the configuration of leader election client.
	LimitServer options.RateLimitOptions

	SecureServing *apiserveroptions.SecureServingOptions
	// TODO: remove insecure serving mode
	InsecureServing *apiserveroptions.DeprecatedInsecureServingOptions
	Authentication  *apiserveroptions.DelegatingAuthenticationOptions
	Authorization   *options.AuthorizationOptions

	Debugging *DebuggingOptions

	Master string

	ShardingIndex int
}

func NewOptions() *Options {
	o := Options{
		SecureServing: &apiserveroptions.SecureServingOptions{
			BindAddress: net.ParseIP("0.0.0.0"),
			BindPort:    0,
		},
		InsecureServing: &apiserveroptions.DeprecatedInsecureServingOptions{
			BindAddress: net.ParseIP("0.0.0.0"),
			BindPort:    18080,
			BindNetwork: "tcp",
		},
		Authentication: func() *apiserveroptions.DelegatingAuthenticationOptions {
			opt := apiserveroptions.NewDelegatingAuthenticationOptions()
			opt.RemoteKubeConfigFileOptional = true
			return opt
		}(),
		Authorization: options.NewAuthorizationOptions(),
		Debugging:     RecommendedDebuggingOptions(),
		LimitServer: options.RateLimitOptions{
			ShardingCount: 8,
			LimitStore:    "local",
			//Identity:                    "", // TODO
			K8sStoreSyncPeriod: time.Second * 30,
			LeaderElectionConfiguration: componentbaseconfig.LeaderElectionConfiguration{
				ResourceName:      "kube-gateway-ratelimiter",
				ResourceNamespace: "kube-gateway",
				ResourceLock:      "leases",
				LeaderElect:       true,
				LeaseDuration:     metav1.Duration{Duration: time.Millisecond * 3000}, // TODO
				RenewDeadline:     metav1.Duration{Duration: time.Millisecond * 2800},
				RetryPeriod:       metav1.Duration{Duration: time.Millisecond * 900},
			},
		},
	}

	// Set the PairName but leave certificate directory blank to generate in-memory by default
	o.SecureServing.ServerCert.CertDirectory = ""
	o.SecureServing.ServerCert.PairName = "kube-gateway-ratelimiter"

	o.ClientConnection.QPS = 10000
	o.ClientConnection.Burst = 10000

	return &o
}

func (o *Options) Flags() cliflag.NamedFlagSets {
	fss := cliflag.NamedFlagSets{}

	o.SecureServing.AddFlags(fss.FlagSet("secure serving"))
	o.InsecureServing.AddUnqualifiedFlags(fss.FlagSet("insecure serving"))
	o.Authentication.AddFlags(fss.FlagSet("authentication"))
	o.Authorization.AddFlags(fss.FlagSet("authorization"))

	o.LimitServer.AddFlags(fss.FlagSet("rate limiter"))

	o.Debugging.AddFlags(fss.FlagSet("debugging"))

	genericfs := fss.FlagSet("generic")
	genericfs.StringVar(&o.Master, "master", o.Master, "The address of the Kubernetes API server (overrides any value in kubeconfig).")
	genericfs.StringVar(&o.ClientConnection.Kubeconfig, "kubeconfig", o.ClientConnection.Kubeconfig, "Path to kubeconfig file with authorization and master location information.")
	genericfs.Float32Var(&o.ClientConnection.QPS, "kube-api-qps", o.ClientConnection.QPS, "QPS to use while talking with clientset apiserver.")
	genericfs.Int32Var(&o.ClientConnection.Burst, "kube-api-burst", o.ClientConnection.Burst, "Burst to use while talking with clientset apiserver.")

	return fss
}

func (o *Options) Config() (*limitconfig.Config, error) {
	c := &limitconfig.Config{Debugging: o.Debugging.DebuggingConfiguration}
	// Prepare kube clients.
	gatewayClient, client, err := createClients(o.ClientConnection, o.Master, o.LimitServer.RenewDeadline.Duration)
	if err != nil {
		return nil, err
	}
	c.Client = client
	c.GatewayClientset = gatewayClient

	informerFactory := informers.NewSharedInformerFactory(client, 0)
	c.InformerFactory = informerFactory

	c.RateLimiter, err = limiter.NewRateLimiter(gatewayClient, client, o.LimitServer)
	if err != nil {
		return nil, err
	}
	if err := o.ApplyTo(c); err != nil {
		return nil, err
	}

	return c, nil
}

func (o *Options) ApplyTo(c *limitconfig.Config) error {
	if err := o.InsecureServing.ApplyTo(&c.InsecureServing); err != nil {
		return err
	}
	if err := o.SecureServing.ApplyTo(&c.SecureServing); err != nil {
		return err
	}
	if o.SecureServing.BindPort != 0 || o.SecureServing.Listener != nil {
		if err := o.Authentication.ApplyTo(&c.Authentication, c.SecureServing, nil); err != nil {
			return err
		}
		if err := o.Authorization.ApplyTo(&c.Authorization, c.InformerFactory); err != nil {
			return err
		}
	}
	return nil
}

func (o *Options) Validate() []error {
	errors := []error{}
	errors = append(errors, o.SecureServing.Validate()...)
	errors = append(errors, o.InsecureServing.Validate()...)
	errors = append(errors, o.Debugging.Validate()...)
	errors = append(errors, o.Authentication.Validate()...)
	errors = append(errors, o.Authorization.Validate()...)

	return errors
}

// createClients creates a kube client and an event client from the given limitconfig and masterOverride.
func createClients(config componentbaseconfig.ClientConnectionConfiguration, masterOverride string, timeout time.Duration) (gatewayclientset.Interface, clientset.Interface, error) {
	if len(config.Kubeconfig) == 0 && len(masterOverride) == 0 {
		klog.Warningf("Neither --kubeconfig nor --master was specified. Using default API gatewayClient. This might not work.")
	}

	// This creates a gatewayClient, first loading any specified kubeconfig
	// file, and then overriding the Master flag, if non-empty.
	kubeConfig, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: config.Kubeconfig},
		&clientcmd.ConfigOverrides{ClusterInfo: clientcmdapi.Cluster{Server: masterOverride}}).ClientConfig()
	if err != nil {
		return nil, nil, err
	}

	kubeConfig.DisableCompression = true
	kubeConfig.AcceptContentTypes = config.AcceptContentTypes
	kubeConfig.ContentType = config.ContentType
	kubeConfig.QPS = config.QPS
	kubeConfig.Burst = int(config.Burst)

	gatewayClient, err := gatewayclientset.NewForConfig(restclient.AddUserAgent(kubeConfig, "kube-gateway-rate-limiter"))
	if err != nil {
		return nil, nil, err
	}

	client, err := clientset.NewForConfig(restclient.AddUserAgent(kubeConfig, "kube-gateway-rate-limiter"))
	if err != nil {
		return nil, nil, err
	}

	return gatewayClient, client, nil
}
