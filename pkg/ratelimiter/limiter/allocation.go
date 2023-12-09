package limiter

import (
	proxyv1alpha1 "github.com/kubewharf/kubegateway/pkg/apis/proxy/v1alpha1"
	"github.com/kubewharf/kubegateway/pkg/flowcontrols/flowcontrol"
	"k8s.io/klog"
	"math"
)

const (
	ExpectUtilizationLevel   = 70 // (0,100)
	ExpectUtilizationPercent = ExpectUtilizationLevel / 100.0

	ReducePercent       = 0.2   // (0,1)
	IncreasePercent     = 0.3   // (0,1)
	MinimumQuotaPercent = 0.002 // (0,1)
	InitialQuotaPercent = 0.05  // (0,1)
)

func calculateNextQuota(
	upstreamTotal proxyv1alpha1.RateLimitItemConfiguration,
	upstreamUsed proxyv1alpha1.RateLimitItemStatus,
	flowControlConfig proxyv1alpha1.RateLimitItemConfiguration,
	flowControlStatus proxyv1alpha1.RateLimitItemStatus,
	clientCount int,
	condition *proxyv1alpha1.RateLimitCondition,
) proxyv1alpha1.RateLimitItemConfiguration {
	newCondition := flowControlConfig.DeepCopy()
	if flowControlConfig.Strategy == proxyv1alpha1.GlobalCountLimit {
		newCondition.MaxRequestsInflight = upstreamTotal.MaxRequestsInflight
		newCondition.TokenBucket = upstreamTotal.TokenBucket
		return *newCondition
	}

	flowControlType := flowcontrol.GetFlowControlTypeFromLimitItem(upstreamTotal.LimitItemDetail)

	current := float64(getLimitQuota(flowControlConfig.LimitItemDetail, flowControlType))
	used := float64(getLimitQuota(flowControlStatus.LimitItemDetail, flowControlType))

	total := float64(getLimitQuota(upstreamTotal.LimitItemDetail, flowControlType))
	allocated := float64(getLimitQuota(upstreamUsed.LimitItemDetail, flowControlType))
	remaining := total - allocated

	var next, burst float64

	if used <= current && flowControlStatus.RequestLevel > 100 {
		flowControlStatus.RequestLevel = int32(used * 100 / current)
	}
	allocatedPercent := allocated / total * 100

	reducingThreshold := upstreamUsed.RequestLevel - 5
	increasingThreshold := upstreamUsed.RequestLevel + 5

	// it is expected that every instance utilization is approaching to the global utilization,
	// but if global utilization is zero, the quota of single instance will increase infinitely to approach 0,
	// so utilization (0,100) is transformed to (5,100).
	expectTotalLevel := upstreamUsed.RequestLevel*95/100 + 5

	// linear mapping from total request level to allocation percent
	// allocate 50% when RequestLevel is 0, allocate 100% when RequestLevel is 100
	expectedAllocatePercent := upstreamUsed.RequestLevel/2 + 50

	targetLevel := expectTotalLevel * 100 / expectedAllocatePercent
	reducingThreshold = targetLevel - 5
	increasingThreshold = targetLevel + 5

	next = current

	switch {
	case current == 0:
		// init value for a new client
		if clientCount <= 10 {
			next = remaining * InitialQuotaPercent
		} else {
			next = remaining / float64(clientCount)
		}
	case flowControlStatus.RequestLevel == 0:
		// if no request, get more quota if global allocate percent is less than ExpectUtilizationPercent,
		// reduce quota if global allocate percent is more than ExpectUtilizationPercent
		upper := total * ExpectUtilizationPercent / float64(clientCount)
		upper = math.Round(upper)

		if allocatedPercent < float64(expectedAllocatePercent)-2 {
			next = current + remaining*IncreasePercent
			if next > upper {
				next = upper
			}
		} else if allocatedPercent > float64(expectedAllocatePercent)+2 || current > upper {
			reduce := current * ReducePercent
			minReduce := total / 100
			if reduce > 0 && reduce < minReduce {
				reduce = minReduce
			}
			if reduce > 0 && reduce < 1 {
				reduce = 1
			}
			next = current - reduce
		}

	case flowControlStatus.RequestLevel < reducingThreshold:
		// if request ratio less than reducingThreshold, reduce quota
		reduce := current * ReducePercent
		maxReduce := current - used
		if reduce > maxReduce {
			reduce = maxReduce
		}
		if reduce > 0 && reduce < 1 {
			reduce = 1
		}
		next = current - reduce
	case flowControlStatus.RequestLevel > increasingThreshold:
		// if request ratio more than increasingThreshold, get more quota
		delta := current * IncreasePercent
		if flowControlStatus.RequestLevel > 100 {
			delta = delta * 2
		}

		if delta < remaining {
			next = current + delta
		} else {
			next = current + remaining
		}
	}

	// The minimum limit quota is 1
	if next < 1 {
		next = 1
	}
	if next < total*MinimumQuotaPercent {
		next = total * MinimumQuotaPercent
	}

	if next-current > remaining {
		next = current + remaining
	}

	next = math.Ceil(next)

	if flowControlType == proxyv1alpha1.TokenBucket {
		burst = next / total * float64(upstreamTotal.LimitItemDetail.TokenBucket.Burst)
	}
	burst = math.Ceil(burst)

	klog.V(2).Infof("[allocate] fc=%s, total=%v, allocated=%v, totalReq=%v%%, last=%v, used=%v (%v%%), next=%v, threshold=(%v, %v), name=%s",
		flowControlConfig.Name, total, allocated, upstreamUsed.RequestLevel, current, used, flowControlStatus.RequestLevel, next, reducingThreshold, increasingThreshold, condition.Name)

	setFlowControlLimit(&newCondition.LimitItemDetail, flowControlType, next, burst)

	return *newCondition
}

func setFlowControlLimit(limit *proxyv1alpha1.LimitItemDetail, flowControlType proxyv1alpha1.FlowControlSchemaType, qps, burst float64) {
	switch flowControlType {
	case proxyv1alpha1.MaxRequestsInflight:
		if limit.MaxRequestsInflight == nil {
			limit.MaxRequestsInflight = &proxyv1alpha1.MaxRequestsInflightFlowControlSchema{}
		}
		limit.MaxRequestsInflight.Max = int32(qps)
	case proxyv1alpha1.TokenBucket:
		if limit.TokenBucket == nil {
			limit.TokenBucket = &proxyv1alpha1.TokenBucketFlowControlSchema{}
		}
		limit.TokenBucket.QPS = int32(qps)
		limit.TokenBucket.Burst = int32(burst)
	}
}

func getLimitQuota(limit proxyv1alpha1.LimitItemDetail, flowControlType proxyv1alpha1.FlowControlSchemaType) int32 {
	switch flowControlType {
	case proxyv1alpha1.MaxRequestsInflight:
		if limit.MaxRequestsInflight == nil {
			return 0
		}
		return limit.MaxRequestsInflight.Max
	case proxyv1alpha1.TokenBucket:
		if limit.TokenBucket == nil {
			return 0
		}
		return limit.TokenBucket.QPS
	}

	return -1
}
