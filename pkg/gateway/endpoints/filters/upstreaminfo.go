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

package filters

import (
	"fmt"
	"github.com/kubewharf/kubegateway/pkg/clusters"
	"github.com/kubewharf/kubegateway/pkg/clusters/features"
	"github.com/kubewharf/kubegateway/pkg/gateway/endpoints/response"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"net"
	"net/http"

	"github.com/kubewharf/kubegateway/pkg/gateway/endpoints/request"
)

// WithUpstreamInfo attaches upstream cluster info to ExtraRequestInfo
func WithUpstreamInfo(handler http.Handler, clusterManager clusters.Manager, s runtime.NegotiatedSerializer) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		ctx := req.Context()

		info, ok := request.ExtraRequestInfoFrom(ctx)
		if !ok {
			handler.ServeHTTP(w, req)
			return
		}

		if ip := net.ParseIP(info.Hostname); ip == nil {
			info.IsProxyRequest = true
			cluster, ok := clusterManager.Get(info.Hostname)
			if !ok {
				response.TerminateWithError(s,
					errors.NewServiceUnavailable(fmt.Sprintf("the request cluster(%s) is not being proxied", info.Hostname)),
					response.TerminationReasonClusterNotBeingProxied, w, req)
				return
			}
			info.UpstreamCluster = cluster

			if cluster.FeatureEnabled(features.CloseConnectionWhenIdle) {
				// Send a GOAWAY and tear down the TCP connection when idle.
				w.Header().Set("Connection", "close")
			}

			if cluster.FeatureEnabled(features.DenyAllRequests) {
				response.TerminateWithError(s, errors.NewServiceUnavailable(fmt.Sprintf("request for %v denied by featureGate(DenyAllRequests)", info.Hostname)),
					response.TerminationReasonCircuitBreaker, w, req)
				return
			}
		}

		req = req.WithContext(request.WithExtraRequestInfo(ctx, info))
		handler.ServeHTTP(w, req)
	})
}
