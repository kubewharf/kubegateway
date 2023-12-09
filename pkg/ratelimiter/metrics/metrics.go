package metrics

import (
	"fmt"
	"net/http"
	"time"

	"k8s.io/apiserver/pkg/endpoints/request"
)

func MonitorLimiterRequest(req *http.Request, serverName, clientID string, requestInfo *request.RequestInfo, httpCode, respSize int, elapsed time.Duration) {
	if requestInfo == nil {
		requestInfo = &request.RequestInfo{Verb: req.Method, Path: req.URL.Path}
	}

	elapsedSeconds := elapsed.Seconds()
	resource := "NonResourceRequest"
	if len(requestInfo.Resource) > 0 || len(requestInfo.Subresource) > 0 {
		resource = requestInfo.Resource
		if len(requestInfo.Subresource) > 0 {
			resource += "/" + requestInfo.Subresource
		}
	}

	verb := requestInfo.Verb

	RateLimiterRequestCounterObservers.Observe(MetricInfo{
		Request:      req,
		ServerName:   serverName,
		ClientID:     clientID,
		Verb:         verb,
		Resource:     resource,
		HttpCode:     fmt.Sprintf("%v", httpCode),
		ResponseSize: float64(respSize),
		Latency:      elapsedSeconds,
	})
	RateLimiterRequestLatenciesObservers.Observe(MetricInfo{
		Request:      req,
		ServerName:   serverName,
		ClientID:     clientID,
		Verb:         verb,
		Resource:     resource,
		HttpCode:     fmt.Sprintf("%v", httpCode),
		ResponseSize: float64(respSize),
		Latency:      elapsedSeconds,
	})

	RateLimiterResponseSizesObservers.Observe(MetricInfo{
		Request:      req,
		ServerName:   serverName,
		ClientID:     clientID,
		Verb:         verb,
		Resource:     resource,
		HttpCode:     fmt.Sprintf("%v", httpCode),
		ResponseSize: float64(respSize),
		Latency:      elapsedSeconds,
	})
}

func MonitorAllocateRequest(serverName, clientID, flowControl, flowControlType, strategy, method string, limit int32) {
	RateLimiterAllocatedLimitsObservers.Observe(MetricInfo{
		ServerName:      serverName,
		ClientID:        clientID,
		FlowControl:     flowControl,
		FlowControlType: flowControlType,
		Strategy:        strategy,
		Method:          method,
		Limit:           float64(limit),
	})
}

func MonitorAcquiredTokenCounter(serverName, clientID, flowControl, flowControlType, strategy, method string, limit int32) {
	RateLimiterAllocatedTokenCounterObservers.Observe(MetricInfo{
		ServerName:      serverName,
		ClientID:        clientID,
		FlowControl:     flowControl,
		FlowControlType: flowControlType,
		Strategy:        strategy,
		Method:          method,
		Limit:           float64(limit),
	})
}

func MonitorLeaderElection(shard int, leader string, leaderState int) {
	RateLimiterLeaderElectionObservers.Observe(MetricInfo{
		Shard:       fmt.Sprintf("%v", shard),
		Leader:      leader,
		LeaderState: leaderState,
	})
}
