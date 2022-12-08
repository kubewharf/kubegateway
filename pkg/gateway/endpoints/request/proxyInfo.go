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

package request

import (
	"context"
	"fmt"
)

// ProxyInfo contains information that indicates if the request is proxied
type ProxyInfo struct {
	Forwarded bool
	Endpoint  string
	Reason    string
}

func NewProxyInfo() *ProxyInfo {
	return &ProxyInfo{
		Forwarded: false,
	}
}

// WithProxyInfo returns a copy of parent in which the ProxyInfo value is set
func WithProxyInfo(parent context.Context, info *ProxyInfo) context.Context {
	return context.WithValue(parent, proxyInfoKey, info)
}

// ExtraProxyInfoFrom returns the value of the ExtraRequestInfo key on the ctx
func ExtraProxyInfoFrom(ctx context.Context) (*ProxyInfo, bool) {
	info, ok := ctx.Value(proxyInfoKey).(*ProxyInfo)
	return info, ok
}

func SetProxyForwarded(ctx context.Context, endpoint string) error {
	info, ok := ExtraProxyInfoFrom(ctx)
	if !ok {
		return fmt.Errorf("no proxy info found in context")
	}
	info.Forwarded = true
	info.Endpoint = endpoint
	return nil
}

func SetProxyTerminated(ctx context.Context, reason string) error {
	info, ok := ExtraProxyInfoFrom(ctx)
	if !ok {
		return fmt.Errorf("no proxy info found in context")
	}
	info.Forwarded = false
	info.Reason = reason
	return nil
}

func IsProxyForwarded(ctx context.Context) bool {
	proxyInfo, ok := ExtraProxyInfoFrom(ctx)
	if !ok {
		return false
	}
	return proxyInfo.Forwarded
}
