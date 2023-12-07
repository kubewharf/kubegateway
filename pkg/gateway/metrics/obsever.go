package metrics

import (
	"net/http"
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
)

type MetricInfo struct {
	Request           *http.Request
	IsResourceRequest bool
	ServerName        string
	Endpoint          string
	Verb              string
	Resource          string
	HttpCode          string
	Path              string
	Reason            string
	ResponseSize      float64
	Latency           float64
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
