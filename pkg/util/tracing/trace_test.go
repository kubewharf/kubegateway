package tracing

import (
	"testing"
	"time"
)

func BenchmarkStageLatency(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		traceInfo := &RequestTraceInfo{}

		steps := []string{StepDispatcher, StepTransport, StepGotConn, StepWroteRequestHeader, StepReadRequest,
			StepWroteRequest, StepGotResponse, StepWroteResponseHeader, StepReadResp, StepWroteResp}
		stepTIme := time.Now()
		for _, s := range steps {
			traceInfo.steps = append(traceInfo.steps, &step{stepTime: stepTIme, msg: s})
			stepTIme = stepTIme.Add(time.Millisecond)
		}
		_ = traceInfo.StageLatency()
	}
	b.StopTimer()

}
