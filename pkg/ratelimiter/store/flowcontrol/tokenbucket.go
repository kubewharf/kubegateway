package flowcontrol

import (
	"fmt"
	"time"

	"golang.org/x/time/rate"

	proxyv1alpha1 "github.com/kubewharf/kubegateway/pkg/apis/proxy/v1alpha1"
)

// newTokenBucketFlowControl creates a token bucket flow control.
func newTokenBucketFlowControl(name string, typ proxyv1alpha1.FlowControlSchemaType, qps int32, burst int32) GlobalFlowControl {
	limiter := rate.NewLimiter(rate.Limit(qps), int(burst))

	return &globalTokenBucket{
		limiter: limiter,
		name:    name,
		typ:     typ,
		qps:     qps,
		burst:   burst,
	}
}

type globalTokenBucket struct {
	limiter *rate.Limiter
	name    string
	typ     proxyv1alpha1.FlowControlSchemaType
	qps     int32
	burst   int32
}

func (f *globalTokenBucket) Type() proxyv1alpha1.FlowControlSchemaType {
	return f.typ
}

func (f *globalTokenBucket) TryAcquireN(instance string, n int32) bool {
	return f.limiter.AllowN(time.Now(), int(n))
}

func (f *globalTokenBucket) String() string {
	return fmt.Sprintf("name=%v,type=%v,qps=%v,burst=%v", f.name, f.typ, f.qps, f.burst)
}

func (f *globalTokenBucket) Resize(n int32, burst int32) bool {
	resized := false
	if f.qps != n || f.burst != burst {
		f.limiter = rate.NewLimiter(rate.Limit(n), int(burst))
		f.qps = n
		f.burst = burst
		resized = true
	}
	return resized
}

func (f *globalTokenBucket) SetState(instance string, current int32) int32 {
	return -1
}
