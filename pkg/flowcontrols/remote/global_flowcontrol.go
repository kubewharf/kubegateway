package remote

import (
	"k8s.io/klog"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	proxyv1alpha1 "github.com/kubewharf/kubegateway/pkg/apis/proxy/v1alpha1"
	"github.com/kubewharf/kubegateway/pkg/flowcontrols/flowcontrol"
	"github.com/kubewharf/kubegateway/pkg/gateway/metrics"
)

var (
	// all percent range is (1, 100)

	GlobalTokenBucketBurstPercent         = int32(5)
	GlobalTokenBucketBurstMinTokens       = int32(1)
	GlobalTokenBucketBatchAcquiredPercent = int32(10)
	GlobalTokenBucketBatchAcquireMin      = int32(1)

	GlobalMaxInflightBurstPercent        = int32(2)
	GlobalMaxInflightBurstMinInflight    = int32(1)
	GlobalMaxInflightBatchAcquirePercent = int32(20)
	GlobalMaxInflightBatchAcquireMin     = int32(1)
)

const (
	waitAcquireTimeout = time.Millisecond * 300
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
	flowControlCache *flowControlCache,
	counter CounterFun,
) GlobalCounterFlowControl {
	if limitItem.Strategy != proxyv1alpha1.GlobalCountLimit {
		return &emptyGlobalWrapper{fcc: flowControlCache, FlowControl: fc}
	}

	switch fc.Type() {
	case proxyv1alpha1.MaxRequestsInflight:
		w := &maxInflightWrapper{
			FlowControl: fc,
			fcc:         flowControlCache,
			meter:       flowControlCache.meter,
			counter:     counter,
			max:         limitItem.MaxRequestsInflight.Max,
			cond:        sync.NewCond(&sync.Mutex{}),
		}
		w.Resize(uint32(limitItem.MaxRequestsInflight.Max), 0)
		return w
	case proxyv1alpha1.TokenBucket:
		w := &tokenBucketWrapper{
			FlowControl: fc,
			fcc:         flowControlCache,
			meter:       flowControlCache.meter,
			counter:     counter,
			cond:        sync.NewCond(&sync.Mutex{}),
		}
		w.Resize(uint32(limitItem.TokenBucket.QPS), uint32(limitItem.TokenBucket.Burst))
		return w
	}

	// default
	w := &tokenBucketWrapper{
		FlowControl: fc,
		fcc:         flowControlCache,
		meter:       flowControlCache.meter,
		counter:     counter,
		cond:        sync.NewCond(&sync.Mutex{}),
	}
	w.Resize(uint32(limitItem.TokenBucket.QPS), uint32(limitItem.TokenBucket.Burst))
	return w
}

type CounterFun func(int32)

type GlobalCounterFlowControl interface {
	flowcontrol.FlowControl
	SetLimit(result *AcquireResult) bool
	ExpectToken() int32
	AddAcquiring(n int32)
	CurrentToken() int32
}

type maxInflightWrapper struct {
	flowcontrol.FlowControl
	fcc     *flowControlCache
	meter   *meter
	counter CounterFun
	lock    sync.Mutex
	cond    *sync.Cond

	lastAcquireTime   int64
	serverUnavailable uint32
	max               int32
	reserve           int32

	acquiredMaxInflight int32
	overLimited         int32
	waitInflight        int32
}

func (m *maxInflightWrapper) ExpectToken() int32 {
	inflight := m.meter.currentInflight()

	acquire := int32(0)
	overLimited := atomic.LoadInt32(&m.overLimited)

	wait := atomic.LoadInt32(&m.waitInflight)
	if overLimited < 1 && acquire < wait {
		acquire = m.max * GlobalMaxInflightBatchAcquirePercent / 100
		if acquire < GlobalMaxInflightBatchAcquireMin {
			acquire = GlobalMaxInflightBatchAcquireMin
		}
		if acquire > wait {
			acquire = wait
		}
	}

	acquire += inflight

	max := atomic.LoadInt32(&m.max)
	if acquire > max {
		acquire = max
		klog.Warningf("[global maxInflight cluster=%q flowcontrol=%s inflight=%v waitInflight=%v", m.fcc.cluster, m.fcc.name, inflight, wait)
	}

	return acquire
}

func (m *maxInflightWrapper) AddAcquiring(n int32) {
}

func (m *maxInflightWrapper) CurrentToken() int32 {
	limitMax := atomic.LoadInt32(&m.acquiredMaxInflight)
	return limitMax
}

func (m *maxInflightWrapper) SetLimit(acquireResult *AcquireResult) bool {
	lastAcquireTime := atomic.LoadInt64(&m.lastAcquireTime)
	if acquireResult.requestTime > 0 && acquireResult.requestTime <= lastAcquireTime {
		// skip for request time too old
		return false
	}

	result := acquireResult.result
	if len(result.Error) != 0 {
		if result.Error == "RequestIDTooOld" {
			// skip for RequestIDTooOld
			return false
		}

		m.lock.Lock()
		if atomic.LoadUint32(&m.serverUnavailable) == 0 {
			inflight := m.meter.maxInflight()
			localMax := m.fcc.local.localConfig.MaxRequestsInflight.Max
			if inflight < localMax {
				inflight = localMax
			}
			klog.V(2).Infof("[global maxInflight] cluster=%q resize flowcontrol=%s max=%v for error: %v",
				m.fcc.cluster, m.fcc.name, inflight, result.Error)
			m.FlowControl.Resize(uint32(inflight), 0)
			atomic.StoreUint32(&m.serverUnavailable, 1)
		}
		m.lock.Unlock()
		return false
	}

	m.lock.Lock()
	defer m.lock.Unlock()
	if result.Accept {
		if atomic.LoadUint32(&m.serverUnavailable) == 1 {
			atomic.StoreUint32(&m.serverUnavailable, 0)
		}

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

	atomic.StoreInt64(&m.lastAcquireTime, acquireResult.requestTime)
	m.cond.Broadcast()
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

	if atomic.LoadUint32(&m.serverUnavailable) == 0 {
		return m.FlowControl.Resize(uint32(m.reserve), 0)
	}
	return true
}

func (m *maxInflightWrapper) TryAcquire() bool {
	method := "async"
	start := time.Now()
	defer func() {
		metrics.RecordGlobalFlowControlAcquire(m.fcc.cluster, "MaxInflight", method, m.fcc.name, time.Since(start))
	}()

	waitInflight := atomic.LoadInt32(&m.waitInflight) + 1

	if m.counter == nil || atomic.LoadUint32(&m.serverUnavailable) == 1 {
		return m.FlowControl.TryAcquire()
	}

	currentInflight := atomic.LoadInt32(&m.meter.inflight)
	naxInflight := atomic.LoadInt32(&m.acquiredMaxInflight)
	overLimited := atomic.LoadInt32(&m.overLimited)
	max := atomic.LoadInt32(&m.max)

	if overLimited > 0 || waitInflight+currentInflight > max {
		return m.FlowControl.TryAcquire()
	}

	acquire := false
	if waitInflight+currentInflight <= naxInflight {
		acquire = m.FlowControl.TryAcquire()
	}

	if !acquire {
		atomic.AddInt32(&m.waitInflight, 1)
		requestTime := time.Now().UnixNano()
		m.counter(0)
		waitAcquire(m.cond, requestTime, &m.lastAcquireTime)
		acquire = m.FlowControl.TryAcquire()
		atomic.AddInt32(&m.waitInflight, -1)
		method = "sync"
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

func (m *maxInflightWrapper) Release() {
	m.FlowControl.Release()
	if m.counter != nil {
		m.counter(-1)
	}
}

type tokenBucketWrapper struct {
	flowcontrol.FlowControl
	fcc     *flowControlCache
	meter   *meter
	counter CounterFun
	lock    sync.Mutex
	cond    *sync.Cond

	lastAcquireTime   int64
	serverUnavailable uint32
	tokens            int32
	reserve           int32
	tokenBatch        int32
	tokenInflight     int32

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

	acquireInflight := atomic.LoadInt32(&m.tokenInflight)
	expect -= acquireInflight
	if expect < 0 {
		expect = 0
	}
	if expect > batch {
		expect = batch
	}

	return expect
}

func (m *tokenBucketWrapper) AddAcquiring(n int32) {
	atomic.AddInt32(&m.tokenInflight, n)
}

func (m *tokenBucketWrapper) expectMore() bool {
	token := atomic.LoadInt32(&m.tokens)
	acquireInflight := atomic.LoadInt32(&m.tokenInflight)
	expect := m.reserve - token - acquireInflight
	return expect > 0
}

func (m *tokenBucketWrapper) CurrentToken() int32 {
	token := atomic.LoadInt32(&m.tokens)
	return token
}

func (m *tokenBucketWrapper) SetLimit(acquireResult *AcquireResult) bool {
	if acquireResult.request != nil {
		atomic.AddInt32(&m.tokenInflight, -acquireResult.request.Tokens)
	}

	result := acquireResult.result
	if len(result.Error) != 0 {
		if result.Error == "RequestIDTooOld" {
			// skip for RequestIDTooOld
			return m.expectMore()
		}
		m.lock.Lock()
		if atomic.LoadUint32(&m.serverUnavailable) == 0 {
			lastQPS := m.meter.rate()
			localQPS := m.fcc.local.localConfig.TokenBucket.QPS
			if lastQPS < float64(localQPS) {
				lastQPS = float64(localQPS)
			}
			klog.V(2).Infof("[global tokenBucket] cluster=%q resize flowcontrol=%s qps=%v for error: %v",
				m.fcc.cluster, m.fcc.name, lastQPS, result.Error)

			m.FlowControl.Resize(uint32(lastQPS), uint32(lastQPS))
			atomic.StoreUint32(&m.serverUnavailable, 1)
		}
		m.lock.Unlock()
		return m.expectMore()
	}
	if result.Accept {
		if atomic.LoadUint32(&m.serverUnavailable) == 1 {
			m.lock.Lock()
			if atomic.LoadUint32(&m.serverUnavailable) == 1 {
				m.FlowControl.Resize(m.qps, m.burst)
				atomic.StoreUint32(&m.serverUnavailable, 0)
			}
			m.lock.Unlock()
		}
		token := result.Limit
		if token > m.reserve {
			token = m.reserve
		}
		atomic.AddInt32(&m.tokens, result.Limit)
	}

	atomic.StoreInt64(&m.lastAcquireTime, acquireResult.requestTime)
	m.cond.Broadcast()
	return m.expectMore()
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
	if atomic.LoadUint32(&m.serverUnavailable) == 0 {
		return m.FlowControl.Resize(qps, burst)
	}
	return false
}

func (m *tokenBucketWrapper) TryAcquire() bool {
	method := "async"
	start := time.Now()
	defer func() {
		metrics.RecordGlobalFlowControlAcquire(m.fcc.cluster, "TokenBucket", method, m.fcc.name, time.Since(start))
	}()

	acquire := m.FlowControl.TryAcquire()

	if !acquire || atomic.LoadUint32(&m.serverUnavailable) == 1 {
		return acquire
	}

	tryAcquire := func() bool {
		token := atomic.AddInt32(&m.tokens, -1)
		if token < 0 {
			atomic.AddInt32(&m.tokens, 1)
			return false
		}
		return true
	}

	acquire = tryAcquire()
	if !acquire && m.counter != nil {
		requestTime := time.Now().UnixNano()
		m.counter(0)
		waitAcquire(m.cond, requestTime, &m.lastAcquireTime)
		acquire = tryAcquire()
		method = "sync"
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
	fcc *flowControlCache
	flowcontrol.FlowControl
}

func (f *emptyGlobalWrapper) TryAcquire() bool {
	return f.FlowControl.TryAcquire()
}

func (f *emptyGlobalWrapper) AddAcquiring(n int32) {
}

func (f *emptyGlobalWrapper) CurrentToken() int32 {
	return 0
}

func (f *emptyGlobalWrapper) ExpectToken() int32 {
	return 0
}

func (f *emptyGlobalWrapper) SetLimit(limit *AcquireResult) bool {
	return false
}

func waitAcquire(cond *sync.Cond, requestTime int64, lastAcquireTime *int64) {
	start := time.Now()
	wait := make(chan struct{})
	var acquireTime int64
	waitCount := 0
	go func() {
		for i := 0; i < 10; i++ {
			cond.L.Lock()
			cond.Wait()
			cond.L.Unlock()

			acquireTime = atomic.LoadInt64(lastAcquireTime)
			if acquireTime > requestTime {
				break
			}
			waitCount++
		}
		wait <- struct{}{}
	}()

	select {
	case <-wait:
	case <-time.After(waitAcquireTimeout):
	}

	cost := time.Since(start)

	// TODO metric

	if cost > time.Millisecond*100 {
		klog.V(4).Infof("wait acquire cost %v, start: %v, acquire: %v", cost, requestTime, acquireTime)
	}
}
