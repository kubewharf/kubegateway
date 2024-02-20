package filters

import (
	"errors"
	"fmt"
	"io"
	"net/http"

	utilnet "k8s.io/apimachinery/pkg/util/net"
	"k8s.io/apiserver/pkg/endpoints/handlers/responsewriters"
	apirequest "k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/klog"

	"github.com/kubewharf/kubegateway/pkg/clusters/features"
	"github.com/kubewharf/kubegateway/pkg/gateway/endpoints/request"
	"github.com/kubewharf/kubegateway/pkg/util/tracing"
)

// WithTraceLog is a filter that record trace log.
func WithTraceLog(handler http.Handler, enableTracing bool, longRunningRequestCheck apirequest.LongRunningRequestCheck) http.Handler {
	if !enableTracing {
		return handler
	}

	klog.V(2).Infof("Enable proxy tracing, ShortRequestLogThreshold=%v, ListRequestLogThreshold=%v", request.ShortRequestLogThreshold, request.ListRequestLogThreshold)

	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		ctx := req.Context()

		requestInfo, ok := apirequest.RequestInfoFrom(ctx)
		if !ok {
			// if this happens, the handler chain isn't setup correctly because there is no request info
			responsewriters.InternalError(w, req, errors.New("no RequestInfo found in the context"))
			return
		}

		// Skip tracing long-running requests.
		if longRunningRequestCheck(req, requestInfo) {
			handler.ServeHTTP(w, req)
			return
		}

		extraInfo, ok := request.ExtraReqeustInfoFrom(ctx)
		if !ok {
			responsewriters.InternalError(w, req, fmt.Errorf("failed to get extra request info from context"))
			return
		}

		cluster := extraInfo.UpstreamCluster
		if cluster == nil || !cluster.FeatureEnabled(features.Tracing) {
			handler.ServeHTTP(w, req)
			return
		}

		tr := tracing.New(fmt.Sprintf("Trace for %v %v", req.Method, req.RequestURI))
		ctx = tracing.WithRequestTraceInfo(ctx, tr)

		req = req.WithContext(ctx)

		defer func() {
			tr.End()

			threshold := request.LogThreshold(requestInfo.Verb)
			if req.Header.Get("x-debug-trace-log") == "1" || tr.IfLong(threshold) {
				tr.WithAttributes(traceFields(req, requestInfo)...)
				tr.Log()
			}
		}()

		rd := &traceReader{
			ReadCloser: req.Body,
			trace:      tr,
		}
		req.Body = rd

		handler.ServeHTTP(w, req)
	})
}

func traceFields(req *http.Request, requestInfo *apirequest.RequestInfo) []tracing.KeyValue {
	sourceIPs := utilnet.SourceIPs(req)
	return []tracing.KeyValue{
		tracing.StringKeyValue("verb", requestInfo.Verb),
		tracing.StringKeyValue("resource", requestInfo.Resource),
		tracing.StringKeyValue("name", requestInfo.Name),
		tracing.StringKeyValue("host", req.Host),
		tracing.StringKeyValue("user-agent", req.Header.Get("User-Agent")),
		tracing.StringKeyValue("srcIP", fmt.Sprintf("%v", sourceIPs)),
	}
}

var _ io.ReadCloser = &traceReader{}

type traceReader struct {
	io.ReadCloser
	trace *tracing.RequestTraceInfo
}

// Write implements io.ReadCloser
func (r *traceReader) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	if err == io.EOF {
		r.trace.Step(tracing.StepReadRequest)
	}
	return n, err
}
