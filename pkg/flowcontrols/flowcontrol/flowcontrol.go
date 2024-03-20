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

package flowcontrol

import (
	"fmt"
	"github.com/zoumo/golib/lock/maxinflight"
	"k8s.io/client-go/util/flowcontrol"

	proxyv1alpha1 "github.com/kubewharf/kubegateway/pkg/apis/proxy/v1alpha1"
)

const (
	LocalFlowControls  = "local"
	RemoteFlowControls = "remote"
)

type FlowControl interface {
	// TryAcquire returns true if a token is taken immediately. Otherwise, it returns false.
	TryAcquire() bool
	// Release add a token back to the lock
	Release()
	// Resize changes the max in flight lock's capacity
	Resize(n uint32, burst uint32) bool
	// String returns human readable string.
	String() string
	// Type return flow control type
	Type() proxyv1alpha1.FlowControlSchemaType
}

type MaxInflightFlowControl interface {
	FlowControl
	MaxInflight() int32
}

type TokenBucketFlowControl interface {
	FlowControl
	QPS() int32
	Burst() int32
}

var (
	DefaultFlowControl = NewFlowControl(proxyv1alpha1.FlowControlSchema{
		Name: "system-default",
		FlowControlSchemaConfiguration: proxyv1alpha1.FlowControlSchemaConfiguration{
			Exempt: &proxyv1alpha1.ExemptFlowControlSchema{},
		},
	})
)

func GuessFlowControlSchemaType(config proxyv1alpha1.FlowControlSchema) proxyv1alpha1.FlowControlSchemaType {
	switch {
	case config.Exempt != nil:
		return proxyv1alpha1.Exempt
	case config.MaxRequestsInflight != nil, config.GlobalMaxRequestsInflight != nil:
		return proxyv1alpha1.MaxRequestsInflight
	case config.TokenBucket != nil, config.GlobalTokenBucket != nil:
		return proxyv1alpha1.TokenBucket
	}
	return proxyv1alpha1.Exempt
}

func GetFlowControlTypeFromLimitItem(config proxyv1alpha1.LimitItemDetail) proxyv1alpha1.FlowControlSchemaType {
	if config.MaxRequestsInflight != nil {
		return proxyv1alpha1.MaxRequestsInflight
	} else if config.TokenBucket != nil {
		return proxyv1alpha1.TokenBucket
	}
	return proxyv1alpha1.Unknown
}

func NewFlowControl(schema proxyv1alpha1.FlowControlSchema) FlowControl {
	name := schema.Name
	typ := GuessFlowControlSchemaType(schema)
	switch typ {
	case proxyv1alpha1.MaxRequestsInflight:
		return &flowControl{
			TokenBucket: maxinflight.New(uint32(schema.MaxRequestsInflight.Max)),
			name:        name,
			typ:         typ,
			max:         uint32(schema.MaxRequestsInflight.Max),
		}
	case proxyv1alpha1.TokenBucket:
		return &resizeableTokenBucket{
			rateLimiter: flowcontrol.NewTokenBucketRateLimiter(float32(schema.TokenBucket.QPS), int(schema.TokenBucket.Burst)),
			name:        name,
			typ:         typ,
			qps:         uint32(schema.TokenBucket.QPS),
			burst:       uint32(schema.TokenBucket.Burst),
		}
	}
	return &flowControl{
		TokenBucket: maxinflight.InfinityTokenBucket,
		name:        name,
		typ:         typ,
	}
}

type flowControl struct {
	maxinflight.TokenBucket
	name string
	typ  proxyv1alpha1.FlowControlSchemaType
	max  uint32
}

func (f *flowControl) Type() proxyv1alpha1.FlowControlSchemaType {
	return f.typ
}

func (f *flowControl) String() string {
	return fmt.Sprintf("name=%v,type=%v,size=%v", f.name, f.typ, f.max)
}

func (f *flowControl) Resize(n uint32, burst uint32) bool {
	resized := false
	if f.max != n {
		f.TokenBucket.Resize(n)
		f.max = n
		resized = true
	}
	return resized
}

func (f *flowControl) MaxInflight() int32 {
	return int32(f.max)
}

type resizeableTokenBucket struct {
	rateLimiter flowcontrol.RateLimiter
	name        string
	typ         proxyv1alpha1.FlowControlSchemaType
	qps         uint32
	burst       uint32
}

func (f *resizeableTokenBucket) Type() proxyv1alpha1.FlowControlSchemaType {
	return f.typ
}

func (f *resizeableTokenBucket) TryAcquire() bool {
	return f.rateLimiter.TryAccept()
}

func (f *resizeableTokenBucket) String() string {
	return fmt.Sprintf("name=%v,type=%v,qps=%v,burst=%v", f.name, f.typ, f.qps, f.burst)
}

func (f *resizeableTokenBucket) Resize(n uint32, burst uint32) bool {
	resized := false
	if f.qps != n || f.burst != burst {
		f.rateLimiter = flowcontrol.NewTokenBucketRateLimiter(float32(n), int(burst))
		f.qps = n
		f.burst = burst
		resized = true
	}
	return resized
}

func (f *resizeableTokenBucket) Release() {
}

func (f *resizeableTokenBucket) QPS() int32 {
	return int32(f.qps)
}

func (f *resizeableTokenBucket) Burst() int32 {
	return int32(f.burst)
}
