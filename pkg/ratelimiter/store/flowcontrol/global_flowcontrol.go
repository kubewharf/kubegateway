package flowcontrol

import (
	"errors"
	proxyv1alpha1 "github.com/kubewharf/kubegateway/pkg/apis/proxy/v1alpha1"
	"k8s.io/klog"
)

var (
	RequestIDTooOld = errors.New("RequestIDTooOld")
)

type GlobalFlowControl interface {
	TryAcquireN(instance string, token int32) bool
	SetState(instance string, requestId int64, current int32) (int32, error)
	String() string
	Resize(n int32, burst int32) bool
	Type() proxyv1alpha1.FlowControlSchemaType
}

func NewGlobalFlowControl(schema proxyv1alpha1.FlowControlSchema) GlobalFlowControl {
	name := schema.Name
	switch {
	case schema.GlobalMaxRequestsInflight != nil:
		return newMaxInflightFlowControl(name, proxyv1alpha1.MaxRequestsInflight, schema.GlobalMaxRequestsInflight.Max)
	case schema.GlobalTokenBucket != nil:
		return newTokenBucketFlowControl(name, proxyv1alpha1.TokenBucket, schema.GlobalTokenBucket.QPS, schema.GlobalTokenBucket.Burst)
	}
	return nil
}

func ResizeGlobalFlowControl(fc GlobalFlowControl, schema proxyv1alpha1.FlowControlSchema, upstream string) {
	switch {
	case schema.GlobalMaxRequestsInflight != nil:
		if fc.Resize(schema.GlobalMaxRequestsInflight.Max, 0) {
			klog.Infof("[global limiter] cluster=%q resize flowcontrol schema=%q", upstream, fc.String())
		}
	case schema.GlobalTokenBucket != nil:
		if fc.Resize(schema.GlobalTokenBucket.QPS, schema.GlobalTokenBucket.Burst) {
			klog.Infof("[global limiter] cluster=%q resize flowcontrol schema=%q", upstream, fc.String())
		}
	}
}
