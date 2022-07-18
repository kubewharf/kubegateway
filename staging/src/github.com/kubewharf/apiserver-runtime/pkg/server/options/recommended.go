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

package options

import (
	"fmt"
	"net"
	"os"
	"strings"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/admission"
	"k8s.io/apiserver/pkg/features"
	genericoptions "k8s.io/apiserver/pkg/server/options"
	serveroptions "k8s.io/apiserver/pkg/server/options"
	serverstorage "k8s.io/apiserver/pkg/server/storage"
	"k8s.io/apiserver/pkg/storage/storagebackend"
	"k8s.io/apiserver/pkg/util/feature"
	utilflowcontrol "k8s.io/apiserver/pkg/util/flowcontrol"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	cliflag "k8s.io/component-base/cli/flag"
	"k8s.io/component-base/featuregate"
	"k8s.io/component-base/version"
	"k8s.io/klog"
	"k8s.io/kubernetes/pkg/kubeapiserver"
	"k8s.io/kubernetes/pkg/registry/cachesize"

	"github.com/kubewharf/apiserver-runtime/pkg/registry"
	"github.com/kubewharf/apiserver-runtime/pkg/scheme"
	"github.com/kubewharf/apiserver-runtime/pkg/server"
	"github.com/kubewharf/apiserver-runtime/pkg/server/storage"
)

const (
	DefaultEtcdPathPrefix = "/registry/apiserver-runtime"
)

// RecommendedOptions contains the recommended options for running an API server.
// Each of them can be nil to leave the feature unconfigured on ApplyTo.
// This options omit authentication and authorization options from genericoptions.RecommendedOptions
// You should wrap this options to construct your own
type RecommendedOptions struct {
	Admission      *genericoptions.AdmissionOptions
	APIEnablement  *genericoptions.APIEnablementOptions
	Audit          *genericoptions.AuditOptions
	Authentication *AuthenticationOptions
	Authorization  *AuthorizationOptions

	BackendAPI *BackendAPIOptions

	Etcd *genericoptions.EtcdOptions
	// API Server Egress Selector is used to control outbound traffic from the API Server
	EgressSelector *genericoptions.EgressSelectorOptions

	Features *genericoptions.FeatureOptions
	// FeatureGate is a way to plumb feature gate through if you have them.
	FeatureGate featuregate.FeatureGate

	// ProcessInfo is used to identify events created by the server.
	ProcessInfo *genericoptions.ProcessInfo

	ServerRun     *genericoptions.ServerRunOptions
	SecureServing *genericoptions.SecureServingOptionsWithLoopback

	Webhook *genericoptions.WebhookOptions
}

func NewRecommendedOptions() *RecommendedOptions {
	return new(RecommendedOptions)
}

func (o *RecommendedOptions) WithAll() *RecommendedOptions {
	return o.
		WithAdmission().
		WithAPIEnablement().
		WithAudit().
		WithAuthentication().
		WithAuthorization().
		WithBackendAPI().
		WithEgressSelector().
		WithEtcd(DefaultEtcdPathPrefix, nil).
		WithFeatures().
		WithFeatureGate().
		WithProcessInfo(genericoptions.NewProcessInfo("apiserver-runtime", "kube-system")).
		WithServerRun().
		WithSecureServing().
		WithWebhook()
}

func (o *RecommendedOptions) WithEtcd(prefix string, codec runtime.Codec) *RecommendedOptions {
	o.Etcd = genericoptions.NewEtcdOptions(storagebackend.NewDefaultConfig(prefix, codec))
	return o
}

func (o *RecommendedOptions) WithProcessInfo(processInfo *genericoptions.ProcessInfo) *RecommendedOptions {
	o.ProcessInfo = processInfo
	return o
}

func (o *RecommendedOptions) WithServerRun() *RecommendedOptions {
	o.ServerRun = genericoptions.NewServerRunOptions()
	return o
}

func (o *RecommendedOptions) WithSecureServing() *RecommendedOptions {
	sso := genericoptions.NewSecureServingOptions()

	// We are composing recommended options for an aggregated api-server,
	// whose client is typically a proxy multiplexing many operations ---
	// notably including long-running ones --- into one HTTP/2 connection
	// into this server.  So allow many concurrent operations.
	sso.HTTP2MaxStreamsPerConnection = 1000

	o.SecureServing = sso.WithLoopback()
	return o
}

func (o *RecommendedOptions) WithAuthentication() *RecommendedOptions {
	o.Authentication = NewAuthenticationOptions().WithAll()
	return o
}

func (o *RecommendedOptions) WithAuthorization() *RecommendedOptions {
	o.Authorization = NewAuthorizationOptions()
	return o
}

func (o *RecommendedOptions) WithAPIEnablement() *RecommendedOptions {
	o.APIEnablement = genericoptions.NewAPIEnablementOptions()
	return o
}

func (o *RecommendedOptions) WithAdmission() *RecommendedOptions {
	o.Admission = NewAdmissionOptions()
	return o
}

func (o *RecommendedOptions) WithAudit() *RecommendedOptions {
	o.Audit = genericoptions.NewAuditOptions()
	return o
}

func (o *RecommendedOptions) WithBackendAPI() *RecommendedOptions {
	o.BackendAPI = NewBackendAPIOptions()
	return o
}
func (o *RecommendedOptions) WithFeatures() *RecommendedOptions {
	o.Features = genericoptions.NewFeatureOptions()
	return o
}

func (o *RecommendedOptions) WithFeatureGate() *RecommendedOptions {
	// Wired a global by default that sadly people will abuse to have different meanings in different repos.
	// Please consider creating your own FeatureGate so you can have a consistent meaning for what a variable contains
	// across different repos.  Future you will thank you.
	o.FeatureGate = feature.DefaultFeatureGate
	return o
}

func (o *RecommendedOptions) WithWebhook() *RecommendedOptions {
	o.Webhook = genericoptions.NewWebhookOptions()
	return o
}

func (o *RecommendedOptions) WithEgressSelector() *RecommendedOptions {
	o.EgressSelector = genericoptions.NewEgressSelectorOptions()
	return o
}

func (o *RecommendedOptions) Flags() (fss cliflag.NamedFlagSets) {
	if o.Admission != nil {
		o.Admission.AddFlags(fss.FlagSet("admission"))
	}
	if o.APIEnablement != nil {
		o.APIEnablement.AddFlags(fss.FlagSet("api enablement"))
	}
	if o.Authentication != nil {
		o.Authentication.AddFlags(fss.FlagSet("authentication"))
	}
	if o.Authorization != nil {
		o.Authorization.AddFlags(fss.FlagSet("authorization"))
	}
	if o.Audit != nil {
		o.Audit.AddFlags(fss.FlagSet("audit"))
	}
	if o.BackendAPI != nil {
		o.BackendAPI.AddFlags(fss.FlagSet("backend api"))
	}
	if o.EgressSelector != nil {
		o.EgressSelector.AddFlags(fss.FlagSet("egress selector"))
	}
	if o.Etcd != nil {
		o.Etcd.AddFlags(fss.FlagSet("etcd"))
	}
	if o.Features != nil {
		o.Features.AddFlags(fss.FlagSet("features"))
	}
	// featureGate flags added in server run
	// if o.FeatureGate != nil {
	// }
	if o.SecureServing != nil {
		o.SecureServing.AddFlags(fss.FlagSet("secure serving"))
	}
	if o.ServerRun != nil {
		o.ServerRun.AddUniversalFlags(fss.FlagSet("server run"))
	}

	return fss
}

func (o *RecommendedOptions) Complete() error {
	if o.APIEnablement != nil && o.APIEnablement.RuntimeConfig != nil {
		for key, value := range o.APIEnablement.RuntimeConfig {
			if key == "v1" || strings.HasPrefix(key, "v1/") ||
				key == "api/v1" || strings.HasPrefix(key, "api/v1/") {
				delete(o.APIEnablement.RuntimeConfig, key)
				o.APIEnablement.RuntimeConfig["/v1"] = value
			}
			if key == "api/legacy" {
				delete(o.APIEnablement.RuntimeConfig, key)
			}
		}
	}

	if o.Etcd != nil {
		// switching pagination according to the feature-gate
		o.Etcd.StorageConfig.Paging = feature.DefaultFeatureGate.Enabled(features.APIListChunking)
		if o.Etcd.EnableWatchCache {
			klog.V(2).Infof("Initializing cache sizes based on %dMB limit", o.ServerRun.TargetRAMMB)
			sizes := cachesize.NewHeuristicWatchCacheSizes(o.ServerRun.TargetRAMMB)
			if userSpecified, err := serveroptions.ParseWatchCacheSizes(o.Etcd.WatchCacheSizes); err == nil {
				for resource, size := range userSpecified {
					sizes[resource] = size
				}
			}
			var err error
			o.Etcd.WatchCacheSizes, err = serveroptions.WriteWatchCacheSizes(sizes)
			if err != nil {
				return err
			}
		}
	}

	if o.ServerRun != nil {
		if o.SecureServing != nil {
			if err := o.ServerRun.DefaultAdvertiseAddress(o.SecureServing.SecureServingOptions); err != nil {
				return err
			}
		}
		if len(o.ServerRun.ExternalHost) == 0 {
			if len(o.ServerRun.AdvertiseAddress) > 0 {
				o.ServerRun.ExternalHost = o.ServerRun.AdvertiseAddress.String()
			} else {
				if hostname, err := os.Hostname(); err == nil {
					o.ServerRun.ExternalHost = hostname
				} else {
					return fmt.Errorf("error finding host name: %v", err)
				}
			}
			klog.Infof("external host was not specified, using %v", o.ServerRun.ExternalHost)
		}
	}

	if o.SecureServing != nil {
		if err := o.SecureServing.MaybeDefaultWithSelfSignedCerts(o.ServerRun.AdvertiseAddress.String(), []string{"kubernetes.default.svc", "kubernetes.default", "kubernetes"}, []net.IP{}); err != nil {
			return fmt.Errorf("error creating self-signed certificates: %v", err)
		}
	}

	return nil
}

// Sometimes you need to apply secure serving options firstly to get loopbackClientConfig
func (o *RecommendedOptions) ApplySecureServingTo(recommended *server.RecommendedConfig, tweakLoopbackConfig func(*rest.Config)) error {
	if o.SecureServing == nil {
		return nil
	}
	err := o.SecureServing.ApplyTo(&recommended.SecureServing, &recommended.LoopbackClientConfig)
	if err != nil {
		return err
	}

	// loopback client and backend client
	if recommended.LoopbackClientConfig != nil {
		// Disable compression for self-communication, since we are going to be
		// on a fast local network
		recommended.Config.LoopbackClientConfig.DisableCompression = true

		if tweakLoopbackConfig != nil {
			tweakLoopbackConfig(recommended.Config.LoopbackClientConfig)
		}
		loopbackclient, err := kubernetes.NewForConfig(recommended.Config.LoopbackClientConfig)
		if err != nil {
			return err
		}
		recommended.LoopbackClientset = loopbackclient
		recommended.LoopbackSharedInformerFactory = informers.NewSharedInformerFactory(loopbackclient, 0)
	}
	return nil
}

func (o *RecommendedOptions) ApplyEtcdTo(recommended *server.RecommendedConfig) error {
	if o.Etcd == nil {
		return nil
	}
	// Etcd and storage factory
	storageFactoryConfig := kubeapiserver.NewStorageFactoryConfig()
	// NOTE: kubeapiserver use legecyscheme.Codecs as serializer, we should replace it by ours
	storageFactoryConfig.Serializer = recommended.Codecs
	// kubeapiserver's DefaultResourceEncoding use legecyscheme.Scheme. we should replace it
	storageFactoryConfig.DefaultResourceEncoding = serverstorage.NewDefaultResourceEncodingConfig(recommended.Scheme)
	storageFactoryConfig.APIResourceConfig = recommended.Config.MergedResourceConfig
	completedStorageFactoryConfig, err := storageFactoryConfig.Complete(o.Etcd)
	if err != nil {
		return err
	}
	storageFactory, err := completedStorageFactoryConfig.New()
	if err != nil {
		return err
	}
	if recommended.Config.EgressSelector != nil {
		storageFactory.StorageConfig.Transport.EgressLookup = recommended.Config.EgressSelector.Lookup
	}
	// wrap storage factory
	sf, err := storage.NewStorageFactory(storageFactory)
	if err != nil {
		return err
	}
	if err := o.Etcd.ApplyWithStorageFactoryTo(sf, &recommended.Config); err != nil {
		return err
	}
	recommended.StorageFactory = sf
	return nil
}

// ApplyTo adds RecommendedOptions to the server configuration.
// pluginInitializers can be empty, it is only need for additional initializers.
func (o *RecommendedOptions) ApplyTo(
	recommended *server.RecommendedConfig,
	tweakLoopbackConfig func(*rest.Config),
	defaultResourceConfig *serverstorage.ResourceConfig,
	pluginInitializers ...admission.PluginInitializer,
) error {
	// version
	kubeVersion := version.Get()
	recommended.Config.Version = &kubeVersion

	// server run
	if o.ServerRun != nil {
		if err := o.ServerRun.ApplyTo(&recommended.Config); err != nil {
			return err
		}
	}

	// egressSelector
	if o.EgressSelector != nil {
		if err := o.EgressSelector.ApplyTo(&recommended.Config); err != nil {
			return err
		}
	}

	if err := o.ApplyEtcdTo(recommended); err != nil {
		return err
	}

	// NOTE: secure serving maybe applied previously in order to get loopbackClientConfig, avoid appling twice
	if recommended.Config.SecureServing == nil && recommended.Config.LoopbackClientConfig == nil {
		if err := o.ApplySecureServingTo(recommended, tweakLoopbackConfig); err != nil {
			return err
		}
	}

	if o.BackendAPI != nil {
		if err := o.BackendAPI.ApplyTo(recommended); err != nil {
			return err
		}
	}

	clientConfig := recommended.LoopbackClientConfig
	clientset := recommended.LoopbackClientset
	sharedInformers := recommended.LoopbackSharedInformerFactory

	// authentication
	if o.Authentication != nil {
		if err := o.Authentication.ApplyTo(&recommended.Authentication, recommended.SecureServing, recommended.OpenAPIConfig, clientset, sharedInformers); err != nil {
			return err
		}
	}

	// authorization
	if o.Authorization != nil {
		if err := o.Authorization.ApplyTo(&recommended.Config, sharedInformers); err != nil {
			return err
		}
	}
	// audit
	if o.Audit != nil {
		if err := o.Audit.ApplyTo(&recommended.Config, clientConfig, sharedInformers, o.ProcessInfo, o.Webhook); err != nil {
			return err
		}
	}

	// apiEnablement
	if o.APIEnablement != nil {
		if err := o.APIEnablement.ApplyTo(&recommended.Config, defaultResourceConfig, scheme.Scheme); err != nil {
			return err
		}
	}

	// admission
	if o.Admission != nil {
		if err := o.Admission.ApplyTo(&recommended.Config, sharedInformers, clientConfig, o.FeatureGate, pluginInitializers...); err != nil {
			return err
		}
	}

	// features
	if o.Features != nil {
		if err := o.Features.ApplyTo(&recommended.Config); err != nil {
			return err
		}
	}

	// flow control
	if feature.DefaultFeatureGate.Enabled(features.APIPriorityAndFairness) {
		recommended.FlowControl = utilflowcontrol.New(
			sharedInformers,
			clientset.FlowcontrolV1alpha1(),
			recommended.MaxRequestsInFlight+recommended.MaxMutatingRequestsInFlight,
			recommended.RequestTimeout/4,
		)
	}

	// set resource encoding config
	if recommended.StorageFactory != nil {
		recommended.RESTStorageOptionsFactory = registry.NewRESTStorageOptionsFactory(recommended.StorageFactory)
	}

	return nil
}

func (o *RecommendedOptions) Validate() []error {
	errors := []error{}
	if o.Admission != nil {
		errors = append(errors, o.Admission.Validate()...)
	}
	if o.APIEnablement != nil {
		errors = append(errors, o.APIEnablement.Validate()...)
	}
	if o.Audit != nil {
		errors = append(errors, o.Audit.Validate()...)
	}
	if o.Authentication != nil {
		errors = append(errors, o.Authentication.Validate()...)
	}
	if o.Authorization != nil {
		errors = append(errors, o.Authorization.Validate()...)
	}
	if o.BackendAPI != nil {
		errors = append(errors, o.BackendAPI.Validate()...)
	}
	if o.Etcd != nil {
		errors = append(errors, o.Etcd.Validate()...)
	}
	if o.EgressSelector != nil {
		errors = append(errors, o.EgressSelector.Validate()...)
	}
	if o.Features != nil {
		errors = append(errors, o.Features.Validate()...)
	}
	if o.ServerRun != nil {
		errors = append(errors, o.ServerRun.Validate()...)
	}
	if o.SecureServing != nil {
		errors = append(errors, o.SecureServing.Validate()...)
	}

	return errors
}
