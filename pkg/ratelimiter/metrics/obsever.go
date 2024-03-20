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

type MetricInfo struct {
	Request      *http.Request
	ServerName   string
	ClientID     string
	Verb         string
	Resource     string
	HttpCode     string
	ResponseSize float64
	Latency      float64

	// for limit allocation
	FlowControl     string
	FlowControlType string
	Strategy        string
	Method          string
	Limit           float64

	// for leader election
	Shard       string
	Leader      string
	LeaderState int
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
