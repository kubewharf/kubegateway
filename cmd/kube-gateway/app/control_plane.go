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
	"fmt"
	"net"

	"github.com/kubewharf/apiserver-runtime/pkg/scheme"
	apiserver "github.com/kubewharf/apiserver-runtime/pkg/server"
	utilwait "k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apiserver/pkg/admission"
	"k8s.io/apiserver/pkg/storage/etcd3/preflight"
	"k8s.io/client-go/rest"
	"k8s.io/component-base/metrics"
	_ "k8s.io/component-base/metrics/prometheus/workqueue" // for workqueue metric registration
	"k8s.io/kube-openapi/pkg/common"
	"k8s.io/kubernetes/pkg/capabilities"

	"github.com/kubewharf/kubegateway/cmd/kube-gateway/app/options"
	"github.com/kubewharf/kubegateway/pkg/apis/generated/openapi"
	gatewayinformers "github.com/kubewharf/kubegateway/pkg/client/informers"
	gatewayclientset "github.com/kubewharf/kubegateway/pkg/client/kubernetes"
	controlplaneserver "github.com/kubewharf/kubegateway/pkg/gateway/controlplane"
	controlplaneadmission "github.com/kubewharf/kubegateway/pkg/gateway/controlplane/admission/initializer"
)

// CreateControlPlaneConfig creates all the resources for running the API server, but runs none of them
func CreateControlPlaneConfig(s *options.ControlPlaneServerRunOptions) (*controlplaneserver.Config, error) {
	config, err := buildControlPlaneConfig(s)
	if err != nil {
		return nil, err
	}

	if _, port, err := net.SplitHostPort(s.Etcd.StorageConfig.Transport.ServerList[0]); err == nil && port != "0" && len(port) != 0 {
		if err := utilwait.PollImmediate(etcdRetryInterval, etcdRetryLimit*etcdRetryInterval, preflight.EtcdConnection{ServerList: s.Etcd.StorageConfig.Transport.ServerList}.CheckEtcdServers); err != nil {
			return nil, fmt.Errorf("error waiting for etcd connection: %v", err)
		}
	}

	capabilities.Initialize(capabilities.Capabilities{
		PerConnectionBandwidthLimitBytesPerSec: s.MaxConnectionBytesPerSec,
	})

	if len(s.ShowHiddenMetricsForVersion) > 0 {
		metrics.SetShowHidden()
	}

	return config, nil
}

func buildControlPlaneConfig(o *options.ControlPlaneServerRunOptions) (serverConfig *controlplaneserver.Config, lastErr error) {
	recommendedConfig := apiserver.NewRecommendedConfig(scheme.Scheme, scheme.Codecs)
	recommendedConfig.Config.MergedResourceConfig = controlplaneserver.DefaultAPIResourceConfigSource()
	//TODO: fix openapi.GetOpenAPIDefinitions
	recommendedConfig.WithOpenapiConfig("KubeGateway", GetControlPlaneOpenAPIDefinitions)

	genericConfig := &recommendedConfig.Config

	// Use protobufs for self-communication.
	// Since not every generic apiserver has to support protobufs, we
	// cannot default to it in generic apiserver and need to explicitly
	// set it in kube-apiserver.
	tweakLoopbackConfig := func(cfg *rest.Config) {
		cfg.ContentConfig.ContentType = "application/vnd.kubernetes.protobuf"
	}

	// apply secure serving config firstly to get loopback client config
	if lastErr = o.ApplySecureServingTo(recommendedConfig, tweakLoopbackConfig); lastErr != nil {
		return
	}

	gatewayClient, err := gatewayclientset.NewForConfig(genericConfig.LoopbackClientConfig)
	if err != nil {
		lastErr = err
		return
	}
	gatewayInformer := gatewayinformers.NewSharedInformerFactory(gatewayClient, 0)
	pluginInitializers := []admission.PluginInitializer{
		controlplaneadmission.New(gatewayClient, gatewayInformer),
	}

	if lastErr = o.ApplyTo(recommendedConfig, nil, controlplaneserver.DefaultAPIResourceConfigSource(), pluginInitializers...); lastErr != nil {
		return
	}

	serverConfig = &controlplaneserver.Config{
		RecommendedConfig: recommendedConfig,
		ExtraConfig: controlplaneserver.ExtraConfig{
			GatewayClientset:             gatewayClient,
			GatewaySharedInformerFactory: gatewayInformer,
		},
	}
	return serverConfig, nil
}

func GetControlPlaneOpenAPIDefinitions(ref common.ReferenceCallback) map[string]common.OpenAPIDefinition {
	nativedef := GetNativeOpenAPIDefinitions(ref)
	for k, v := range openapi.GetOpenAPIDefinitions(ref) {
		nativedef[k] = v
	}
	return nativedef
}
