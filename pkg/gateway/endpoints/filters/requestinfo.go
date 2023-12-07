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
	"net/http"

	genericapifilters "k8s.io/apiserver/pkg/endpoints/filters"
	"k8s.io/apiserver/pkg/endpoints/handlers/responsewriters"
	genericapirequest "k8s.io/apiserver/pkg/endpoints/request"

	"github.com/kubewharf/kubegateway/pkg/gateway/endpoints/request"
)

// WithHost attaches a request host to the context.
func WithExtraRequestInfo(handler http.Handler, resolver request.ExtraRequestInfoResolver) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		ctx := req.Context()
		info, err := resolver.NewExtraRequestInfo(req)
		if err != nil {
			responsewriters.InternalError(w, req, fmt.Errorf("failed to create ExtraRequestInfo: %v", err))
			return
		}
		req = req.WithContext(request.WithExtraReqeustInfo(ctx, info))
		handler.ServeHTTP(w, req)
	})
}

// record impersonator because request user will be replaced by impersonatee
func WithImpersonator(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		ctx := req.Context()
		user, ok := genericapirequest.UserFrom(ctx)
		if !ok {
			handler.ServeHTTP(w, req)
			return
		}
		info, ok := request.ExtraReqeustInfoFrom(ctx)
		if ok && info.IsImpersonateRequest {
			info.Impersonator = user
		}
		req = req.WithContext(request.WithExtraReqeustInfo(ctx, info))
		handler.ServeHTTP(w, req)
	})
}

var WithRequestInfo = genericapifilters.WithRequestInfo
