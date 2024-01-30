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

package response

import (
	"net/http"
	"strconv"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apiserver/pkg/endpoints/handlers/responsewriters"

	"github.com/kubewharf/kubegateway/pkg/gateway/endpoints/request"
)

const (
	RetryAfter            = 1
	UnavailableRetryAfter = 30

	TerminationReasonRateLimited = "rate_limited"
)

func TerminateWithError(codecs serializer.CodecFactory, err *errors.StatusError, reason string, w http.ResponseWriter, req *http.Request) {
	if errors.IsTooManyRequests(err) {
		w.Header().Set("Retry-After", strconv.Itoa(RetryAfter))
	} else if errors.IsServiceUnavailable(err) {
		w.Header().Set("Retry-After", strconv.Itoa(UnavailableRetryAfter))
	}
	gv := schema.GroupVersion{Group: "", Version: "v1"}
	request.SetProxyTerminated(req.Context(), reason) // nolint:errcheck
	responsewriters.ErrorNegotiated(err, codecs, gv, w, req)
}
