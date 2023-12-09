package remote

import (
	"math"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"k8s.io/klog"

	proxyv1alpha1 "github.com/kubewharf/kubegateway/pkg/apis/proxy/v1alpha1"
	"github.com/kubewharf/kubegateway/pkg/flowcontrols/flowcontrol"
)

var (
	FlowControlMeterWindow = time.Second * 3

	MeterTickDuration = time.Second
)

type FlowControlCache interface {
	FlowControl() RemoteFlowControlWrapper
	EnableRemoteFlowControl()
	LocalFlowControl() LocalFlowControlWrapper
	Strategy() proxyv1alpha1.LimitStrategy
	Rate() float64
	Inflight() int32
	Stop()
}

type LocalFlowControlWrapper interface {
	flowcontrol.FlowControl
	Sync(proxyv1alpha1.FlowControlSchema)
	Config() proxyv1alpha1.FlowControlSchema
}

type RemoteFlowControlWrapper interface {
	GlobalCounterFlowControl
	Sync(proxyv1alpha1.RateLimitItemConfiguration)
	Config() proxyv1alpha1.RateLimitItemConfiguration
	Done() <-chan struct{}
}

func NewFlowControlCache(cluster, name, clientID string, globalCounterProvider GlobalCounterProvider) FlowControlCache {
	timeWindow := FlowControlMeterWindow
	tickDuration := MeterTickDuration
	bucketsLen := int(timeWindow / tickDuration)
	buckets := make([]float64, bucketsLen)

	stopCh := make(chan struct{})

	clientIdSlices := strings.Split(clientID, "-")
	id := clientIdSlices[len(clientIdSlices)-1]

	f := &flowControlCache{
		cluster: cluster,
		name:    name,
		meter: &meter{
			cluster: cluster,
			name:    name,
			stopCh:  stopCh,
			buckets: buckets,
			ticker:  time.NewTicker(tickDuration),
			last:    time.Now(),
			mu:      sync.Mutex{},
		},
		globalCounter: globalCounterProvider,
		clientID:      id,
	}

	f.local = &localWrapper{flowControlCache: f}

	go f.meter.tick()
	return f
}

type flowControlCache struct {
	local   *localWrapper
	remote  *remoteWrapper
	cluster string
	name    string

	globalCounter GlobalCounterProvider
	meter         *meter

	clientID string
}

func (f *flowControlCache) FlowControl() RemoteFlowControlWrapper {
	if f.remote == nil {
		return nil
	}
	return f.remote

}

func (f *flowControlCache) EnableRemoteFlowControl() {
	if f.remote == nil {
		stopCh := make(chan struct{})
		f.remote = &remoteWrapper{
			flowControlCache: f,
			stopCh:           stopCh,
		}
	}
}

func (f *flowControlCache) LocalFlowControl() LocalFlowControlWrapper {
	return f.local
}

func (f *flowControlCache) Strategy() proxyv1alpha1.LimitStrategy {
	return f.local.localConfig.Strategy
}

func (f *flowControlCache) Rate() float64 {
	return f.meter.rate()
}

func (f *flowControlCache) Inflight() int32 {
	return atomic.LoadInt32(&f.meter.inflight)
}

func (f *flowControlCache) Stop() {
	close(f.meter.stopCh)
	if f.remote != nil {
		close(f.remote.stopCh)
	}
}

func (f *flowControlCache) stopRemoteWrapper() {
	if f.remote != nil {
		close(f.remote.stopCh)
	}
	f.remote = nil
}

func (f *flowControlCache) newMeterFlowControl(schema proxyv1alpha1.FlowControlSchema) flowcontrol.FlowControl {
	fc := flowcontrol.NewFlowControl(schema)
	meterFc := &meterWrapper{
		FlowControl: fc,
		meter:       f.meter,
	}
	return meterFc
}

type localWrapper struct {
	flowcontrol.FlowControl
	localConfig      proxyv1alpha1.FlowControlSchema
	flowControlCache *flowControlCache
}

func (f *localWrapper) Config() proxyv1alpha1.FlowControlSchema {
	return f.localConfig
}

func (f *localWrapper) Sync(schema proxyv1alpha1.FlowControlSchema) {
	if reflect.DeepEqual(schema, f.localConfig) {
		return
	}
	f.localConfig = schema

	newType := flowcontrol.GuessFlowControlSchemaType(schema)
	if f.FlowControl == nil || f.Type() != newType {
		f.FlowControl = f.flowControlCache.newMeterFlowControl(schema)
		klog.Infof("[local limiter] cluster=%q ensure flowcontrol schema %v id=%v", f.flowControlCache.cluster, f.String(), f.flowControlCache.clientID)
		return
	}

	switch newType {
	case proxyv1alpha1.MaxRequestsInflight:
		if f.Resize(uint32(schema.MaxRequestsInflight.Max), 0) {
			klog.Infof("[local limiter] cluster=%q resize flowcontrol schema=%q", f.flowControlCache.cluster, f.String())
		}
	case proxyv1alpha1.TokenBucket:
		if f.Resize(uint32(schema.TokenBucket.QPS), uint32(schema.TokenBucket.Burst)) {
			klog.Infof("[local limiter] cluster=%q resize flowcontrol schema=%q", f.flowControlCache.cluster, f.String())
		}
	}

	if !EnableGlobalFlowControl(schema) {
		f.flowControlCache.stopRemoteWrapper()
	}

	return
}

type remoteWrapper struct {
	GlobalCounterFlowControl
	remoteConfig     proxyv1alpha1.RateLimitItemConfiguration
	flowControlCache *flowControlCache
	stopCh           chan struct{}
}

func (f *remoteWrapper) Config() proxyv1alpha1.RateLimitItemConfiguration {
	return f.remoteConfig
}

func (f *remoteWrapper) Sync(limitItem proxyv1alpha1.RateLimitItemConfiguration) {
	if reflect.DeepEqual(limitItem, f.remoteConfig) {
		return
	}

	defer func() {
		f.remoteConfig = limitItem
	}()

	newType := flowcontrol.GetFlowControlTypeFromLimitItem(limitItem.LimitItemDetail)
	klog.V(5).Infof("[remote limiter] cluster=%q name=%q sync flowcontrol", f.flowControlCache.cluster, limitItem.Name)

	if f.GlobalCounterFlowControl == nil || f.Type() != newType || f.remoteConfig.Strategy != limitItem.Strategy {
		f.GlobalCounterFlowControl = f.newFlowControl(limitItem, newType)
		klog.Infof("[remote limiter] cluster=%q ensure flowcontrol schema %v", f.flowControlCache.cluster, f.String())
		return
	}

	switch {
	case limitItem.MaxRequestsInflight != nil && f.Type() == proxyv1alpha1.MaxRequestsInflight:
		max := limitItem.MaxRequestsInflight.Max
		globalMax := f.flowControlCache.local.Config().GlobalMaxRequestsInflight.Max
		if max > globalMax {
			max = globalMax
		}

		f.Resize(uint32(max), 0)
		klog.V(2).Infof("[remote limiter] cluster=%q resize flowcontrol schema=[%s], inflight=%v, id=%v",
			f.flowControlCache.cluster, f.String(), f.flowControlCache.Inflight(), f.flowControlCache.clientID)
	case limitItem.TokenBucket != nil && f.Type() == proxyv1alpha1.TokenBucket:
		qps := limitItem.TokenBucket.QPS
		globalQPS := f.flowControlCache.local.Config().GlobalTokenBucket.QPS
		if qps > globalQPS {
			qps = globalQPS
		}

		f.Resize(uint32(qps), uint32(limitItem.TokenBucket.Burst))
		klog.V(2).Infof("[remote limiter] cluster=%q resize flowcontrol schema=[%s], rate=%.1f, id=%v",
			f.flowControlCache.cluster, f.String(), f.flowControlCache.Rate(), f.flowControlCache.clientID)
	default:
		f.GlobalCounterFlowControl = f.newFlowControl(limitItem, newType)
	}
}

func (f *remoteWrapper) newFlowControl(limitItem proxyv1alpha1.RateLimitItemConfiguration, newType proxyv1alpha1.FlowControlSchemaType) GlobalCounterFlowControl {
	f.flowControlCache.globalCounter.Stop(limitItem.Name)

	fc := f.flowControlCache.newMeterFlowControl(toFlowControlSchema(limitItem))

	var counterFun CounterFun
	if limitItem.Strategy == proxyv1alpha1.GlobalCountLimit {
		counter := f.flowControlCache.globalCounter.Add(limitItem.Name, newType, f)
		counterFun = counter.Count
	}

	return newFlowControlCounter(limitItem, fc, f.flowControlCache.meter, counterFun)
}

func (f *remoteWrapper) ExpectToken() int32 {
	if f.GlobalCounterFlowControl != nil {
		return f.GlobalCounterFlowControl.ExpectToken()
	}
	return -1
}

func (f *remoteWrapper) CurrentToken() int32 {
	if f.GlobalCounterFlowControl != nil {
		return f.GlobalCounterFlowControl.CurrentToken()
	}
	return -1
}

func (f *remoteWrapper) SetLimit(result *proxyv1alpha1.RateLimitAcquireResult) bool {
	if f.GlobalCounterFlowControl != nil {
		return f.GlobalCounterFlowControl.SetLimit(result)
	}
	return false
}

func (f *remoteWrapper) Done() <-chan struct{} {
	return f.stopCh
}

type meterWrapper struct {
	flowcontrol.FlowControl
	meter *meter
}

func (f *meterWrapper) TryAcquire() bool {
	acquire := f.FlowControl.TryAcquire()
	if acquire {
		atomic.AddInt32(&f.meter.inflight, 1)
		atomic.AddInt64(&f.meter.uncounted, 1)
	}
	return acquire
}

func (f *meterWrapper) Release() {
	atomic.AddInt32(&f.meter.inflight, -1)
	f.FlowControl.Release()
}

type meter struct {
	cluster string
	name    string

	stopCh       chan struct{}
	ticker       *time.Ticker
	uncounted    int64
	inflight     int32
	currentIndex int
	rateAvg      float64
	last         time.Time
	buckets      []float64
	mu           sync.Mutex
}

func (m *meter) tick() {
	defer m.ticker.Stop()
	for {
		select {
		case <-m.ticker.C:
			latestRate := m.latestRate()

			m.mu.Lock()
			lastRate := m.buckets[m.currentIndex]
			if lastRate == math.NaN() {
				lastRate = 0
			}

			rateAvg := m.rateAvg + (latestRate-lastRate)/float64(len(m.buckets))
			m.rateAvg = rateAvg
			m.buckets[m.currentIndex] = latestRate
			m.currentIndex = (m.currentIndex + 1) % len(m.buckets)
			m.mu.Unlock()

			klog.V(6).Infof("FlowControl %s/%s tick: latestRate %v, rateAvg %v, currentIndex %v, buckets %v",
				m.cluster, m.name, latestRate, m.rateAvg, m.currentIndex, m.buckets)
		case <-m.stopCh:
			return
		}
	}
}

func (m *meter) latestRate() float64 {
	count := atomic.LoadInt64(&m.uncounted)
	atomic.AddInt64(&m.uncounted, -count)
	m.mu.Lock()
	last := m.last
	now := time.Now()
	timeWindow := float64(now.Sub(last)) / float64(time.Second)
	instantRate := float64(count) / timeWindow
	m.last = now
	m.mu.Unlock()

	klog.V(6).Infof("FlowControl %s/%s latestRate: count %v, timeWindow %v, rate %v",
		m.cluster, m.name, count, timeWindow, instantRate)

	return instantRate
}

func (m *meter) rate() float64 {
	return m.rateAvg
}

func EnableGlobalFlowControl(schema proxyv1alpha1.FlowControlSchema) bool {
	switch schema.Strategy {
	case proxyv1alpha1.GlobalAllocateLimit, proxyv1alpha1.GlobalCountLimit:
		if schema.GlobalTokenBucket != nil || schema.GlobalMaxRequestsInflight != nil {
			return true
		}
	default:
		return false
	}
	return false
}

func toFlowControlSchema(limitItemConfig proxyv1alpha1.RateLimitItemConfiguration) proxyv1alpha1.FlowControlSchema {
	schema := proxyv1alpha1.FlowControlSchema{
		Name:     limitItemConfig.Name,
		Strategy: limitItemConfig.Strategy,
	}
	switch {
	case limitItemConfig.MaxRequestsInflight != nil:
		schema.MaxRequestsInflight = &proxyv1alpha1.MaxRequestsInflightFlowControlSchema{
			Max: limitItemConfig.MaxRequestsInflight.Max,
		}
	case limitItemConfig.TokenBucket != nil:
		schema.TokenBucket = &proxyv1alpha1.TokenBucketFlowControlSchema{
			QPS:   limitItemConfig.TokenBucket.QPS,
			Burst: limitItemConfig.TokenBucket.Burst,
		}
	}

	return schema
}
