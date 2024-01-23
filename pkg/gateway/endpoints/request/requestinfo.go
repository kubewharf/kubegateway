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

package request

import (
	"context"
	"net/http"

	authenticationv1 "k8s.io/api/authentication/v1"
	"k8s.io/apiserver/pkg/authentication/user"

	"github.com/kubewharf/kubegateway/pkg/gateway/net"
)

type key int

const (
	// requestInfoKey is the context key for the extra request info.
	requestInfoKey key = iota

	// proxyInfoKey is the context key for the proxy info.
	proxyInfoKey key = iota
)

type ExtraRequestInfoResolver interface {
	NewExtraRequestInfo(req *http.Request) (*ExtraRequestInfo, error)
}

type ExtraRequestInfoFactory struct{}

func (f *ExtraRequestInfoFactory) NewExtraRequestInfo(req *http.Request) (*ExtraRequestInfo, error) {
	isImpersonate := len(req.Header.Get(authenticationv1.ImpersonateUserHeader)) > 0
	hostname := net.HostWithoutPort(req.Host)

	return &ExtraRequestInfo{
		Scheme:               req.URL.Scheme,
		Hostname:             hostname,
		IsImpersonateRequest: isImpersonate,
	}, nil
}

type ExtraRequestInfo struct {
	Scheme               string
	Hostname             string // hostname without port
	IsImpersonateRequest bool
	Impersonator         user.Info
}

// WithExtraRequestInfo returns a copy of parent in which the ExtraRequestInfo value is set
func WithExtraRequestInfo(parent context.Context, info *ExtraRequestInfo) context.Context {
	return context.WithValue(parent, requestInfoKey, info)
}

// ExtraRequestInfoFrom returns the value of the ExtraRequestInfo key on the ctx
func ExtraRequestInfoFrom(ctx context.Context) (*ExtraRequestInfo, bool) {
	info, ok := ctx.Value(requestInfoKey).(*ExtraRequestInfo)
	return info, ok
}
