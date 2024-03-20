package filters

import (
	"github.com/kubewharf/kubegateway/pkg/ratelimiter/metrics"
	apiservermetrics "k8s.io/apiserver/pkg/endpoints/metrics"
	"k8s.io/apiserver/pkg/endpoints/request"
	"net/http"
	"time"
)

// WithRequestMetric monitor request metric
func WithRequestMetric(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		now := time.Now()
		delegate := &apiservermetrics.ResponseWriterDelegator{ResponseWriter: w}

		handler.ServeHTTP(delegate, req)

		requestInfo, ok := request.RequestInfoFrom(req.Context())
		if !ok {
			return
		}
		extraRequestInfo, ok := ExtraReqeustInfoFrom(req.Context())

		MonitorAfterRequest(req, requestInfo, extraRequestInfo, delegate, time.Since(now))
	})
}

func MonitorAfterRequest(
	req *http.Request,
	requestInfo *request.RequestInfo,
	extraRequestInfo *ExtraRequestInfo,
	rw *apiservermetrics.ResponseWriterDelegator,
	elapsed time.Duration,
) {

	var serverName, clientID string
	if extraRequestInfo != nil {
		serverName = extraRequestInfo.UpstreamCluster
		clientID = extraRequestInfo.Instance
	}
	if len(serverName) == 0 {
		serverName = "empty"
	}
	if len(clientID) == 0 {
		clientID = "empty"
	}

	metrics.MonitorLimiterRequest(req,
		serverName,
		clientID,
		requestInfo,
		rw.Status(),
		rw.ContentLength(),
		elapsed,
	)

	// TODO log for error or long latency
}
