package remote

import (
	"context"
	"github.com/kubewharf/kubegateway/pkg/gateway/metrics"
	"os"
	"sync"
	"sync/atomic"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/flowcontrol"
	"k8s.io/klog"

	proxyv1alpha1 "github.com/kubewharf/kubegateway/pkg/apis/proxy/v1alpha1"
	"github.com/kubewharf/kubegateway/pkg/flowcontrols/util"
	"github.com/kubewharf/kubegateway/pkg/ratelimiter/clientsets"
)

const (
	MaxIdealDuration    = time.Millisecond * 900
	MaxLimitRequestQPS  = 200
	LimitRequestTimeout = time.Millisecond * 290
)

func NewGlobalCounterProvider(ctx context.Context, cluster string, clientSets clientsets.ClientSets, clientID string) GlobalCounterProvider {
	return &globalCounterManager{
		ctx:        ctx,
		cluster:    cluster,
		clientSets: clientSets,
		counterMap: map[string]*globalCounter{},
		eventCh:    make(chan struct{}, 1),
		clientID:   clientID,
	}
}

type GlobalCounterProvider interface {
	Add(name string, typ proxyv1alpha1.FlowControlSchemaType, flowControl RemoteFlowControlWrapper) GlobalCounter
	Get(name string) GlobalCounter
	Stop(name string)
}

type GlobalCounter interface {
	Count(int32)
}

type globalCounterManager struct {
	ctx          context.Context
	cluster      string
	clientSets   clientsets.ClientSets
	eventCh      chan struct{}
	lock         sync.RWMutex
	counterMap   map[string]*globalCounter
	workerStopCh chan struct{}
	clientID     string
	acquireLock  sync.Mutex
}

func (g *globalCounterManager) Get(name string) GlobalCounter {
	g.lock.Lock()
	defer g.lock.Unlock()
	counter := g.counterMap[name]
	if counter != nil {
		return counter
	}
	return nil
}

func (g *globalCounterManager) Add(name string, typ proxyv1alpha1.FlowControlSchemaType, flowControl RemoteFlowControlWrapper) GlobalCounter {
	g.lock.Lock()
	defer g.lock.Unlock()
	counter := g.counterMap[name]
	if counter != nil {
		return nil
	}

	internalStopCh := make(chan struct{})
	counter = &globalCounter{
		name:        name,
		typ:         typ,
		eventCh:     make(chan struct{}, 1),
		manager:     g,
		flowControl: flowControl,
		stopCh:      internalStopCh,
	}
	g.counterMap[name] = counter

	go counter.resetCheck(internalStopCh)
	go counter.debugInfo(internalStopCh)

	stopCh := flowControl.Done()
	go func() {
		select {
		case <-stopCh:
			g.Stop(name)
			return
		case <-g.ctx.Done():
			g.Stop(name)
			return
		}
	}()

	g.ensureWorker()
	return counter
}

func (g *globalCounterManager) Stop(name string) {
	g.lock.Lock()
	defer g.lock.Unlock()
	c, ok := g.counterMap[name]
	if ok {
		close(c.stopCh)
		delete(g.counterMap, name)
	}
	g.ensureWorker()
}

func (g *globalCounterManager) mark() {
	select {
	case g.eventCh <- struct{}{}:
	default:
	}
}

func (g *globalCounterManager) ensureWorker() {
	counterNum := len(g.counterMap)
	if counterNum > 0 && g.workerStopCh == nil {
		stopCh := make(chan struct{})
		g.workerStopCh = stopCh
		go g.limitWorker(stopCh)
	} else if counterNum == 0 && g.workerStopCh != nil {
		stopCh := g.workerStopCh
		g.workerStopCh = nil
		close(stopCh)
	}
}

func (g *globalCounterManager) limitWorker(stopCh <-chan struct{}) {
	klog.Infof("[global counter] cluster=%q start worker", g.cluster)

	timer := time.NewTimer(MaxIdealDuration)
	defer timer.Stop()
	requestLimiter := flowcontrol.NewTokenBucketRateLimiter(MaxLimitRequestQPS, MaxLimitRequestQPS*5)
	for {
		select {
		case <-g.eventCh:
			requestLimiter.Accept()
			g.doAcquire()
		case <-timer.C:
			g.doAcquire()
		case <-stopCh:
			klog.Infof("[global counter] cluster=%q stop worker", g.cluster)
			return
		}
		timer.Reset(MaxIdealDuration)
	}
}

func (g *globalCounterManager) doAcquire() {
	g.acquireLock.Lock()
	defer g.acquireLock.Unlock()

	result := "unknown"
	interrupted := false

	start := time.Now()
	defer func() {
		if interrupted {
			metrics.RecordRateLimiterRequest(g.cluster, "acquire", result, "all", time.Since(start))
		}
	}()

	client, err := g.clientSets.ClientFor(g.cluster)
	if err != nil {
		klog.Errorf("Get limiter server client for cluster %s error: %v", g.cluster, err)
		result = requestReasonNoReadyClients
		interrupted = true
		return
	}

	requestTime := time.Now().UnixNano()
	acquireRequest, limitRequestsMap := g.acquireRequest(requestTime)
	if acquireRequest == nil || len(acquireRequest.Spec.Requests) == 0 {
		result = requestReasonSkipped
		interrupted = true
		return
	}
	acquireRequest.Spec.RequestID = requestTime

	go func() {
		defer func() {
			metrics.RecordRateLimiterRequest(g.cluster, "acquire", result, "all", time.Since(start))
		}()

		ctx, cancel := context.WithTimeout(context.TODO(), LimitRequestTimeout)
		defer cancel()

		acquireResult, err := client.ProxyV1alpha1().RateLimitConditions().Acquire(ctx, g.cluster, acquireRequest, metav1.CreateOptions{})
		if err != nil {
			klog.Errorf("Do acquire request error: %v", err)
			result = requestReasonRateLimiterError
			if os.IsTimeout(err) {
				result = requestReasonTimeout
			}

			acquireResult = &proxyv1alpha1.RateLimitAcquire{}
			for _, r := range acquireRequest.Spec.Requests {
				acquireResult.Status.Results = append(acquireResult.Status.Results, proxyv1alpha1.RateLimitAcquireResult{
					FlowControl: r.FlowControl,
					Accept:      false,
					Error:       err.Error(),
				})
			}
		} else {
			result = requestReasonSuccess
		}

		for _, r := range acquireResult.Status.Results {
			rs := r
			if counter := g.counterMap[r.FlowControl]; counter != nil {
				acceptInfo := &AcquireResult{
					request:     limitRequestsMap[rs.FlowControl],
					result:      &rs,
					requestTime: requestTime,
				}
				counter.send(acceptInfo)
			}

			reason := requestReasonSuccess
			if len(rs.Error) > 0 {
				reason = requestReasonRateLimiterError
			}
			metrics.RecordRateLimiterRequest(g.cluster, "acquire", reason, rs.FlowControl, -1)
		}
	}()
}

func (g *globalCounterManager) acquireRequest(requestTime int64) (*proxyv1alpha1.RateLimitAcquire, map[string]*proxyv1alpha1.RateLimitAcquireRequest) {
	counterHasEvent := map[string]*globalCounter{}
	counterResync := map[string]bool{}
	unixNow := time.Now().Unix()
	g.lock.Lock()
	for name, c := range g.counterMap {
		lastSync := atomic.LoadInt64(&c.lastSyncTime)
		if c.hasEvent() {
			counterHasEvent[name] = c
		} else if unixNow-lastSync > 2 {
			counterHasEvent[name] = c
			counterResync[name] = true
		}
	}
	g.lock.Unlock()

	if len(counterHasEvent) == 0 {
		return nil, nil
	}

	var limitRequest = proxyv1alpha1.RateLimitAcquire{
		ObjectMeta: metav1.ObjectMeta{
			Name: g.cluster,
		},
		Spec: proxyv1alpha1.RateLimitAcquireSpec{
			Instance: g.clientSets.ClientID(),
		},
	}
	limitRequestsMap := map[string]*proxyv1alpha1.RateLimitAcquireRequest{}
	for name, c := range counterHasEvent {
		req := proxyv1alpha1.RateLimitAcquireRequest{
			FlowControl: name,
		}
		switch c.typ {
		case proxyv1alpha1.TokenBucket:
			hits := c.flowControl.ExpectToken()
			if hits <= 0 && !counterResync[name] {
				continue
			}
			c.flowControl.AddAcquiring(hits)
			req.Tokens = hits
		case proxyv1alpha1.MaxRequestsInflight:
			inflight := c.flowControl.ExpectToken()
			req.Tokens = inflight
		default:
			continue
		}
		limitRequest.Spec.Requests = append(limitRequest.Spec.Requests, req)
		limitRequestsMap[name] = &req
	}

	return &limitRequest, limitRequestsMap
}

type globalCounter struct {
	name         string
	typ          proxyv1alpha1.FlowControlSchemaType
	lastSyncTime int64
	stopCh       chan struct{}
	eventCh      chan struct{}
	manager      *globalCounterManager
	flowControl  RemoteFlowControlWrapper

	// debug
	requiredRate util.RateMeter
	acquireRate  util.RateMeter
	reqRate      util.RateMeter
	limitedRate  util.RateMeter
}

type AcquireResult struct {
	request     *proxyv1alpha1.RateLimitAcquireRequest
	result      *proxyv1alpha1.RateLimitAcquireResult
	requestTime int64
}

func (g *globalCounter) Count(delta int32) {
	select {
	case g.eventCh <- struct{}{}:
	default:
	}
	g.manager.mark()
}

func (g *globalCounter) stop() {
	close(g.eventCh)
}

func (g *globalCounter) send(response *AcquireResult) {
	if len(response.result.Error) != 0 && response.result.Error != "RequestIDTooOld" {
		klog.Warningf("[global counter] cluster=%q flowcontrol=%q global count response err: %v",
			g.manager.cluster, g.name, response.result.Error)
	}

	// debug
	{
		g.requiredRate.RecordN(int64(response.request.Tokens))
		g.reqRate.Record()
		if response.result.Accept {
			g.acquireRate.RecordN(int64(response.result.Limit))
		} else {
			g.limitedRate.Record()
		}
	}

	if g.flowControl.SetLimit(response) {
		go func() {
			<-time.NewTimer(time.Millisecond * 200).C
			g.Count(1)
		}()
	}
	atomic.StoreInt64(&g.lastSyncTime, time.Now().Unix())
}

func (g *globalCounter) hasEvent() bool {
	select {
	case _, ok := <-g.eventCh:
		return ok
	default:
		return false
	}
}

// TODO remote this
func (g *globalCounter) resetCheck(stopCh <-chan struct{}) {
	klog.Infof("[global counter] cluster=%q flowcontrol=%q start reset check", g.manager.cluster, g.name)

	atomic.StoreInt64(&g.lastSyncTime, time.Now().Unix())
	ticker := time.NewTicker(MaxIdealDuration)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			lastSync := atomic.LoadInt64(&g.lastSyncTime)
			if time.Now().Unix()-lastSync > 4 {
				klog.Infof("[global counter] cluster=%q flowcontrol=%q reset global count limiter for timeout",
					g.manager.cluster, g.name)

				result := &proxyv1alpha1.RateLimitAcquireResult{
					Error: "timeout",
				}
				g.flowControl.SetLimit(&AcquireResult{
					result: result,
				})
			}
		case <-stopCh:
			return
		}
	}
}

func (g *globalCounter) debugInfo(stopCh <-chan struct{}) {
	ticker := time.NewTicker(time.Second * 1)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if os.Getenv("DEBUG_LIMITER_COUNTER") == "true" {
				klog.Infof("[debug] remote counter [%v/%v/%v]: token [require: %v, acquire: %v], request: [total: %v, limited: %v]",
					g.manager.cluster, g.name, g.manager.clientID,
					g.requiredRate.Delta(), g.acquireRate.Delta(),
					g.reqRate.Delta(), g.limitedRate.Delta(),
				)
			}
		case <-stopCh:
			return
		}
	}
}
