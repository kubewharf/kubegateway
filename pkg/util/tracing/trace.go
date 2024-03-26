package tracing

import (
	"context"
	"fmt"
	"k8s.io/klog"
	"math/rand"
	"net/http/httptrace"
	"strings"
	"sync"
	"time"
)

const (
	// requestTraceContextKey is the context key for the extra request info.
	requestTraceContextKey int = iota
)

const (
	StepDispatcher          = "dispatcher"
	StepTransport           = "transport"
	StepGotConn             = "got_conn"
	StepWroteRequestHeader  = "wrote_req_header"
	StepReadRequest         = "read_request"
	StepWroteRequest        = "wrote_req"
	StepFirstResponse       = "first_resp_byte"
	StepGotResponse         = "got_resp"
	StepWroteResponseHeader = "wrote_resp_header"
	StepReadResp            = "read_resp"
	StepWroteResp           = "wrote_resp"
)

const (
	MetricStageClientRead    = "client_read"
	MetricStageClientWrite   = "client_write"
	MetricStageUpstreamRead  = "upstream_read"
	MetricStageUpstreamWrite = "upstream_write"
	MetricStageHandlingDelay = "handling_delay"
)

var (
	stagesTransitions = map[string][]Transition{
		MetricStageClientRead: {
			{From: StepWroteRequestHeader, To: StepReadRequest},
		},
		MetricStageUpstreamWrite: {
			{From: StepGotConn, To: StepWroteRequestHeader},
			{From: StepReadRequest, FromAlt: StepWroteRequestHeader, To: StepWroteRequest},
		},
		MetricStageUpstreamRead: {
			{From: StepWroteRequest, To: StepGotResponse},
			{From: StepWroteResponseHeader, To: StepReadResp},
		},
		MetricStageClientWrite: {
			{From: StepGotResponse, To: StepWroteResponseHeader},
			{From: StepReadResp, To: StepWroteResp},
		},
	}
)

// WithRequestTraceInfo returns a copy of parent in which the RequestTraceInfo value is set
func WithRequestTraceInfo(parent context.Context, info *RequestTraceInfo) context.Context {
	ctx := httptrace.WithClientTrace(parent, info.httpTrace)
	ctx = context.WithValue(ctx, requestTraceContextKey, info)
	return ctx
}

// RequestTraceInfoFrom returns the value of the RequestTraceInfo key on the ctx
func RequestTraceInfoFrom(ctx context.Context) (*RequestTraceInfo, bool) {
	info, ok := ctx.Value(requestTraceContextKey).(*RequestTraceInfo)
	return info, ok
}

func Step(ctx context.Context, msg string, options ...StepOption) {
	if trace, ok := RequestTraceInfoFrom(ctx); ok {
		trace.Step(msg, options...)
	}
}

func TraceID(ctx context.Context) int32 {
	if trace, ok := RequestTraceInfoFrom(ctx); ok {
		return trace.ID()
	}
	return -1
}

func New(name string) *RequestTraceInfo {
	t := &RequestTraceInfo{
		name:      name,
		startTime: time.Now(),
		traceId:   rand.Int31(),
	}
	t.WithHttpTrace()

	return t
}

type RequestTraceInfo struct {
	name       string
	traceId    int32
	startTime  time.Time
	endTime    time.Time
	attributes []KeyValue
	steps      []*step
	httpTrace  *httptrace.ClientTrace
	sync.Mutex

	stageLatency map[string]time.Duration
}

func (t *RequestTraceInfo) WithHttpTrace() {
	t.httpTrace = &httptrace.ClientTrace{
		GotConn: func(ci httptrace.GotConnInfo) { t.Step(StepGotConn) },
		//GotFirstResponseByte: func() { t.Step(StepFirstResponse) },
		WroteHeaders: func() { t.Step(StepWroteRequestHeader) },
		WroteRequest: func(e httptrace.WroteRequestInfo) { t.Step(StepWroteRequest) },
	}
}

func (t *RequestTraceInfo) Step(msg string, options ...StepOption) {
	if t.steps == nil {
		// traces almost always have less than 6 steps, do this to avoid more than a single allocation
		t.steps = make([]*step, 0, 6)
	}

	s := &step{stepTime: time.Now(), msg: msg}

	for _, opt := range options {
		opt.applyStep(s)
	}

	t.Lock()
	t.steps = append(t.steps, s)
	t.Unlock()
}

func (t *RequestTraceInfo) WithAttributes(attributes ...KeyValue) {
	t.attributes = append(attributes, t.attributes...)
}

func (t *RequestTraceInfo) IfLong(threshold time.Duration) bool {
	return time.Since(t.startTime) >= threshold
}

func (t *RequestTraceInfo) End() {
	t.endTime = time.Now()
}

func (t *RequestTraceInfo) ID() int32 {
	return t.traceId
}

func (t *RequestTraceInfo) StageLatency() map[string]time.Duration {
	if t.stageLatency != nil {
		return t.stageLatency
	}

	stepTimestamp := map[string]time.Time{}
	for _, st := range t.steps {
		stepTimestamp[st.msg] = st.stepTime
	}

	eliminatedLatency := time.Duration(0)
	stageLatency := map[string]time.Duration{}
	for stage, transitions := range stagesTransitions {
		for _, trans := range transitions {
			start, startExist := stepTimestamp[trans.From]
			if !startExist {
				start, startExist = stepTimestamp[trans.FromAlt]
			}
			end, endExist := stepTimestamp[trans.To]
			if !startExist || !endExist {
				continue
			}
			cost := end.Sub(start)
			if cost < 0 {
				continue
			}
			stageLatency[stage] += cost
			eliminatedLatency += cost
		}
	}
	stageLatency[MetricStageHandlingDelay] = t.endTime.Sub(t.startTime) - eliminatedLatency

	t.stageLatency = stageLatency
	return stageLatency
}

func (t *RequestTraceInfo) Log() {
	traceId := t.traceId
	endTime := t.endTime
	totalTime := endTime.Sub(t.startTime)
	klog.Infof("Trace [%d] [%s] (started: %v) (total time: %v) %s", traceId, t.name, t.startTime.Format(time.RFC3339), totalTime, formatAttributes(t.attributes))

	lastStepTime := t.startTime
	for _, step := range t.steps {
		stepDuration := step.stepTime.Sub(lastStepTime)
		klog.Infof("Trace [%d] [%v] [%v] %s", traceId, step.stepTime.Sub(t.startTime), stepDuration, step.msg)
		lastStepTime = step.stepTime
	}
	stepDuration := endTime.Sub(lastStepTime)
	klog.Infof("Trace [%d] [%v] [%v] end", traceId, endTime.Sub(t.startTime), stepDuration)
}

type step struct {
	stepTime time.Time
	msg      string
}

type StepOption interface {
	applyStep(s *step)
}

var _ StepOption = timestampOption{}

type timestampOption time.Time

func (o timestampOption) applyStep(s *step) {
	s.stepTime = time.Time(o)
}

func WithStepTimestamp(t time.Time) StepOption {
	return timestampOption(t)
}

type KeyValue struct {
	Key   string
	Value string
}

func StringKeyValue(key, val string) KeyValue {
	return KeyValue{Key: key, Value: val}
}

func formatAttributes(attributes []KeyValue) string {
	var kvs []string
	for _, a := range attributes {
		kvs = append(kvs, fmt.Sprintf("%s=%q", a.Key, a.Value))
	}
	return strings.Join(kvs, " ")
}

// Transition describe transition between two phases.
type Transition struct {
	From    string
	FromAlt string // alternative
	To      string
}
