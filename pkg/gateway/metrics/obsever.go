package metrics

import (
	"net/http"
	"time"

	"k8s.io/apiserver/pkg/authentication/user"
	"k8s.io/apiserver/pkg/endpoints/request"
)

type MetricObserver interface {
	Observe(metric MetricInfo)
}

type UnionObserver interface {
	MetricObserver
	AddObserver(MetricObserver)
}

var (
	ProxyUpstreamUnhealthyObservers     = newUnionObserver()
	ProxyReceiveRequestCounterObservers = newUnionObserver()
	ProxyRequestCounterObservers        = newUnionObserver()
	ProxyRequestLatenciesObservers      = newUnionObserver()
	ProxyResponseSizesObservers         = newUnionObserver()
	ProxyRequestTerminationsObservers   = newUnionObserver()
	ProxyWatcherRegisteredObservers     = newUnionObserver()
	ProxyWatcherUnregisteredObservers   = newUnionObserver()

	ProxyRateLimiterRequestCounterObservers = newUnionObserver()
	ProxyGlobalFlowControlAcquireObservers  = newUnionObserver()

	ProxyRequestInflightObservers   = newUnionObserver()
	ProxyRequestThroughputObservers = newUnionObserver()

	ProxyHandlingLatencyObservers = newUnionObserver()
)

type MetricInfo struct {
	Request           *http.Request
	RequestInfo       *request.RequestInfo
	User              user.Info
	UserName          string
	IsLongRunning     bool
	IsResourceRequest bool
	ServerName        string
	Endpoint          string
	FlowControl       string
	Verb              string
	Resource          string
	HttpCode          string
	Path              string
	Reason            string
	Stage             string
	RequestSize       int64
	ResponseSize      int64
	Latency           float64
	Rate              float64
	Inflight          float64
	TraceLatencies    map[string]time.Duration

	Method      string
	Result      string
	LimitMethod string
	Type        string
}

func newUnionObserver() UnionObserver {
	return &unionObserver{}
}

type unionObserver struct {
	observers []MetricObserver
}

func (o *unionObserver) Observe(metric MetricInfo) {
	for _, ob := range o.observers {
		ob.Observe(metric)
	}
}

func (o *unionObserver) AddObserver(observer MetricObserver) {
	o.observers = append(o.observers, observer)
}
