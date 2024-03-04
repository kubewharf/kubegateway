// Copyright 2023 ByteDance and its affiliates.
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

package filters

import (
	"fmt"
	"net/http"

	"k8s.io/apiserver/pkg/endpoints/handlers/responsewriters"
	genericapirequest "k8s.io/apiserver/pkg/endpoints/request"

	"github.com/kubewharf/kubegateway/pkg/gateway/endpoints/request"
	"github.com/kubewharf/kubegateway/pkg/gateway/metrics"
)

// WithPreProcessingMetrics is a filter that record metrics before request being processed.
func WithPreProcessingMetrics(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		ctx := req.Context()
		info, ok := genericapirequest.RequestInfoFrom(ctx)
		if !ok {
			responsewriters.InternalError(w, req, fmt.Errorf("failed to get request info from context"))
			return
		}
		extraInfo, ok := request.ExtraRequestInfoFrom(ctx)
		if !ok {
			responsewriters.InternalError(w, req, fmt.Errorf("failed to get extra request info from context"))
			return
		}
		metrics.RecordProxyRequestReceived(req, extraInfo.Hostname, info)

		handler.ServeHTTP(w, req)
	})
}
