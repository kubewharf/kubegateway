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

package clusters

import (
	"fmt"
	"net"
	"net/url"
	"time"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/flowcontrol"

	proxyv1alpha1 "github.com/kubewharf/kubegateway/pkg/apis/proxy/v1alpha1"
)

func buildClusterRESTConfig(cluster *proxyv1alpha1.UpstreamCluster) (*rest.Config, error) {
	httpScheme := "https"
	if len(cluster.Spec.Servers) > 0 {
		server := cluster.Spec.Servers[0]
		u, err := url.Parse(server.Endpoint)
		if err != nil {
			err = fmt.Errorf("failed to parse endpoint=%q, err: %v", server.Endpoint, err)
			return nil, err
		}
		httpScheme = u.Scheme
	}

	cfg := newRESTConfig()
	cfg.BearerToken = string(cluster.Spec.ClientConfig.BearerToken)

	if cluster.Spec.ClientConfig.QPS > 0 {
		qps := calQPS(cluster.Spec.ClientConfig.QPS, cluster.Spec.ClientConfig.QPSDivisor)
		cfg.RateLimiter = flowcontrol.NewTokenBucketRateLimiter(qps, int(cluster.Spec.ClientConfig.Burst))
	}

	if httpScheme == "https" {
		tlsCfg := rest.TLSClientConfig{
			ServerName: cluster.Name,
			KeyData:    cluster.Spec.ClientConfig.KeyData,
			CertData:   cluster.Spec.ClientConfig.CertData,
			CAData:     cluster.Spec.ClientConfig.CAData,
			Insecure:   cluster.Spec.ClientConfig.Insecure,
		}
		cfg.TLSClientConfig = tlsCfg
	}
	return cfg, nil
}

func calQPS(qps int32, qpsDivisor int32) float32 {
	ret := float32(qps)
	if qpsDivisor > 1 {
		ret /= float32(qpsDivisor)
	}
	return ret
}

func newRESTConfig() *rest.Config {
	cfg := &rest.Config{
		Timeout:     5 * time.Second,
		RateLimiter: flowcontrol.NewFakeAlwaysRateLimiter(),
		Dial: (&net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
	}

	rest.AddUserAgent(cfg, "kube-gateway")
	return cfg
}
