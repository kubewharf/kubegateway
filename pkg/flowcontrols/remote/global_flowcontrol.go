package remote

import (
	"k8s.io/klog"
	"os"
	"strconv"
	"sync"
	"sync/atomic"

	proxyv1alpha1 "github.com/kubewharf/kubegateway/pkg/apis/proxy/v1alpha1"
	"github.com/kubewharf/kubegateway/pkg/flowcontrols/flowcontrol"
)

var (
	// all percent range is (1, 100)

	GlobalTokenBucketBurstPercent         = int32(5)
	GlobalTokenBucketBurstMinTokens       = int32(1)
	GlobalTokenBucketBatchAcquiredPercent = int32(10)
	GlobalTokenBucketBatchAcquireMin      = int32(1)

	GlobalMaxInflightBurstPercent        = int32(2)
	GlobalMaxInflightBurstMinInflight    = int32(1)
	GlobalMaxInflightBatchAcquirePercent = int32(10)
	GlobalMaxInflightBatchAcquireMin     = int32(1)
)

func init() {
	if val := os.Getenv("GLOBAL_MAXINFLIGHT_BURST_PERCENT"); len(val) > 0 {
		i, err := strconv.Atoi(val)
		if err != nil {
			klog.Warningf("Illegal GLOBAL_MAXINFLIGHT_BURST_PERCENT(%q): %v."+
				" Default value %d is used", val, err, GlobalMaxInflightBurstPercent)
		} else {
			GlobalMaxInflightBurstPercent = int32(i)
		}
	}

	if val := os.Getenv("GLOBAL_TOKENBUCKET_BURST_PERCENT"); len(val) > 0 {
		i, err := strconv.Atoi(val)
		if err != nil {
			klog.Warningf("Illegal GLOBAL_TOKENBUCKET_BURST_PERCENT(%q): %v."+
				" Default value %d is used", val, err, GlobalTokenBucketBurstPercent)
		} else {
			GlobalTokenBucketBurstPercent = int32(i)
		}
	}
}

func newFlowControlCounter(limitItem proxyv1alpha1.RateLimitItemConfiguration,
	fc flowcontrol.FlowControl,
	meter *meter,
	counter CounterFun,
) GlobalCounterFlowControl {
	if limitItem.Strategy != proxyv1alpha1.GlobalCountLimit {
		return emptyGlobalWrapper{fc}
	}

	switch fc.Type() {
	case proxyv1alpha1.MaxRequestsInflight:
		w := &maxInflightWrapper{
			FlowControl: fc,
			meter:       meter,
			counter:     counter,
			max:         limitItem.MaxRequestsInflight.Max,
		}
		w.Resize(uint32(limitItem.MaxRequestsInflight.Max), 0)
		return w
	case proxyv1alpha1.TokenBucket:
		w := &tokenBucketWrapper{
			FlowControl: fc,
			meter:       meter,
			counter:     counter,
		}
		w.Resize(uint32(limitItem.TokenBucket.QPS), uint32(limitItem.TokenBucket.Burst))
		return w
	}
	return &tokenBucketWrapper{FlowControl: fc}
}

type CounterFun func(int32)

type GlobalCounterFlowControl interface {
	flowcontrol.FlowControl
	SetLimit(result *proxyv1alpha1.RateLimitAcquireResult) bool
	ExpectToken() int32
	CurrentToken() int32
}

type maxInflightWrapper struct {
	flowcontrol.FlowControl
	meter   *meter
	counter CounterFun
	lock    sync.Mutex

	serverUnavailable bool
	max               int32
	reserve           int32

	acquiredMaxInflight int32
	overLimited         int32
}

func (m *maxInflightWrapper) ExpectToken() int32 {
	inflight := atomic.LoadInt32(&m.meter.inflight)

	acquire := int32(0)
	limitMax := atomic.LoadInt32(&m.acquiredMaxInflight)
	overLimited := atomic.LoadInt32(&m.overLimited)

	if overLimited < 1 && inflight <= limitMax && inflight > 0 {
		acquire = m.reserve * GlobalMaxInflightBatchAcquirePercent / 100
		if acquire < GlobalMaxInflightBatchAcquireMin {
			acquire = GlobalMaxInflightBatchAcquireMin
		}
	}
	acquire += inflight
	return acquire
}
func (m *maxInflightWrapper) CurrentToken() int32 {
	limitMax := atomic.LoadInt32(&m.acquiredMaxInflight)
	return limitMax
}

func (m *maxInflightWrapper) SetLimit(result *proxyv1alpha1.RateLimitAcquireResult) bool {
	if len(result.Error) != 0 {
		inflight := atomic.LoadInt32(&m.meter.inflight)
		m.FlowControl.Resize(uint32(inflight), 0)
		m.serverUnavailable = true
		return false
	}

	m.lock.Lock()
	defer m.lock.Unlock()
	if result.Accept {
		m.serverUnavailable = false

		limit := result.Limit
		if limit < m.reserve {
			limit = m.reserve
		}
		if limit > m.max {
			limit = m.max
		}
		atomic.StoreInt32(&m.overLimited, 0)
		atomic.StoreInt32(&m.acquiredMaxInflight, limit)
		m.FlowControl.Resize(uint32(limit), 0)
	} else {
		atomic.StoreInt32(&m.overLimited, 1)
		atomic.StoreInt32(&m.acquiredMaxInflight, result.Limit)
		m.FlowControl.Resize(uint32(result.Limit), 0)
	}
	return false
}

func (m *maxInflightWrapper) Resize(max uint32, burst uint32) bool {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.reserve = int32(max) * GlobalMaxInflightBurstPercent / 100
	if m.reserve < GlobalMaxInflightBurstMinInflight {
		m.reserve = GlobalMaxInflightBurstMinInflight
	}

	m.max = int32(max)

	if !m.serverUnavailable {
		return m.FlowControl.Resize(uint32(m.reserve), 0)
	}
	return true
}

func (m *maxInflightWrapper) TryAcquire() bool {
	acquire := m.FlowControl.TryAcquire()
	if m.counter != nil {
		if acquire {
			m.counter(1)
		} else {
			m.counter(0)
		}
	}

	return acquire
}

func (m *maxInflightWrapper) Release() {
	m.FlowControl.Release()
	if m.counter != nil {
		m.counter(-1)
	}
}

type tokenBucketWrapper struct {
	flowcontrol.FlowControl
	meter   *meter
	counter CounterFun
	lock    sync.Mutex

	serverUnavailable bool
	tokens            int32
	reserve           int32
	tokenBatch        int32

	qps   uint32
	burst uint32
}

func (m *tokenBucketWrapper) ExpectToken() int32 {
	token := atomic.LoadInt32(&m.tokens)
	expect := m.reserve - token

	batch := m.tokenBatch
	lastQPS := m.meter.rate()
	if lastQPS > float64(m.tokenBatch) {
		batch = int32(lastQPS) * GlobalTokenBucketBatchAcquiredPercent / 100
		if batch < GlobalTokenBucketBatchAcquireMin {
			batch = GlobalTokenBucketBatchAcquireMin
		}
	}

	if expect > batch {
		expect = batch
	}
	return expect
}

func (m *tokenBucketWrapper) CurrentToken() int32 {
	token := atomic.LoadInt32(&m.tokens)
	return token
}

func (m *tokenBucketWrapper) SetLimit(result *proxyv1alpha1.RateLimitAcquireResult) bool {
	if len(result.Error) != 0 {
		lastQPS := m.meter.rate()
		m.FlowControl.Resize(uint32(lastQPS), uint32(lastQPS))
		m.lock.Lock()
		m.serverUnavailable = true
		m.lock.Unlock()
		return m.ExpectToken() > 0
	}
	if result.Accept {
		if m.serverUnavailable {
			m.lock.Lock()
			if m.serverUnavailable {
				m.FlowControl.Resize(m.qps, m.burst)
				m.serverUnavailable = false
			}
			m.lock.Unlock()
		}
		token := result.Limit
		if token > m.reserve {
			token = m.reserve
		}
		atomic.AddInt32(&m.tokens, result.Limit)
	}
	return m.ExpectToken() > 0
}

func (m *tokenBucketWrapper) Resize(qps uint32, burst uint32) bool {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.reserve = int32(qps) * GlobalTokenBucketBurstPercent / 100
	if m.reserve < GlobalTokenBucketBurstMinTokens {
		m.reserve = GlobalTokenBucketBurstMinTokens
	}

	currentToken := atomic.LoadInt32(&m.tokens)
	overflow := currentToken - m.reserve
	if overflow > 0 {
		atomic.AddInt32(&m.tokens, -overflow)
	}

	m.tokenBatch = m.reserve * GlobalTokenBucketBatchAcquiredPercent / 100
	if m.tokenBatch < GlobalTokenBucketBatchAcquireMin {
		m.tokenBatch = GlobalTokenBucketBatchAcquireMin
	}
	m.qps = qps
	m.burst = qps
	if !m.serverUnavailable {
		return m.FlowControl.Resize(qps, burst)
	}
	return false
}

func (m *tokenBucketWrapper) TryAcquire() bool {
	if m.serverUnavailable {
		return m.FlowControl.TryAcquire()
	}

	acquire := false
	token := atomic.AddInt32(&m.tokens, -1)
	if token < 0 {
		atomic.AddInt32(&m.tokens, 1)
	} else {
		acquire = m.FlowControl.TryAcquire()
	}

	if m.counter != nil {
		if acquire {
			m.counter(1)
		} else {
			m.counter(0)
		}
	}

	return acquire
}

type emptyGlobalWrapper struct {
	flowcontrol.FlowControl
}

func (f emptyGlobalWrapper) CurrentToken() int32 {
	return 0
}

func (f emptyGlobalWrapper) ExpectToken() int32 {
	return 0
}

func (f emptyGlobalWrapper) SetLimit(limit *proxyv1alpha1.RateLimitAcquireResult) bool {
	return false
}
