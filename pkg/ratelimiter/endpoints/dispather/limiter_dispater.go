package dispather

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/emicklei/go-restful"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/klog"

	"github.com/kubewharf/kubegateway/pkg/apis/proxy/v1alpha1"
	"github.com/kubewharf/kubegateway/pkg/ratelimiter/endpoints/filters"
	"github.com/kubewharf/kubegateway/pkg/ratelimiter/limiter"
	"github.com/kubewharf/kubegateway/pkg/ratelimiter/scheme"
	"github.com/kubewharf/kubegateway/pkg/ratelimiter/util"
)

func WithLimiterDispatcher(handler http.Handler, rateLimiter limiter.RateLimiter) http.Handler {
	limiterServer := NewLimiterDispatcher(handler, rateLimiter)

	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		limiterServer.Dispatch(w, req)
	})
}

func NewLimiterDispatcher(delegate http.Handler, rateLimiter limiter.RateLimiter) *LimiterDispatcher {
	rl := &LimiterDispatcher{
		delegateHandler: delegate,
		rateLimiter:     rateLimiter,
	}
	rl.initDispatcher()
	return rl
}

type LimiterDispatcher struct {
	delegateHandler    http.Handler
	goRestfulContainer *restful.Container
	rateLimiter        limiter.RateLimiter
}

func (s *LimiterDispatcher) Dispatch(w http.ResponseWriter, req *http.Request) {
	for _, ws := range s.goRestfulContainer.RegisteredWebServices() {
		if strings.HasPrefix(req.URL.Path, ws.RootPath()) {
			s.goRestfulContainer.Dispatch(w, req)
			return
		}
	}
	s.delegateHandler.ServeHTTP(w, req)
}

func (s *LimiterDispatcher) initDispatcher() {
	ws := new(restful.WebService)
	ws.Path("/apis/proxy.kubegateway.io/v1alpha1")
	ws.Doc("rate limiter server api")
	ws.Route(
		ws.GET("/ratelimitconditions/{condition}").To(s.getRateLimitCondition).
			Doc("get flow control status").
			Operation("flow control status"))
	ws.Route(
		ws.PUT("/ratelimitconditions/{condition}/status").To(func(request *restful.Request, response *restful.Response) {
			s.reportRateLimitConditionStatus(request, response)
		}).
			Doc("report flowcontrol status").
			Operation("report"))
	ws.Route(
		ws.POST("/ratelimitconditions/{condition}/acquire").To(func(request *restful.Request, response *restful.Response) {
			s.doAcquire(request, response)
		}).
			Doc("do limit for global sync counting").
			Operation("report"))

	ws.Route(
		ws.GET("/ratelimit/endpoints").To(func(request *restful.Request, response *restful.Response) {
			withNonResourceInfo(request, "endpoints")
			s.serverInfo(request, response)
		}).
			Doc("get server status").
			Operation("server status"))

	ws.Route(
		ws.POST("/ratelimit/heartbeat").To(func(request *restful.Request, response *restful.Response) {
			withNonResourceInfo(request, "heartbeat")
			s.heartbeat(request, response)
		}).
			Doc("client do heartbeat").
			Operation("do heartbeat"))

	container := restful.NewContainer()
	container.Add(ws)
	s.goRestfulContainer = container
}

func withNonResourceInfo(req *restful.Request, nonResource string) {
	requestInfo, ok := request.RequestInfoFrom(req.Request.Context())
	if ok {
		requestInfo.IsResourceRequest = false
		requestInfo.Resource = ""
		requestInfo.Subresource = nonResource
		requestInfo.Name = ""
	}
}

func (s *LimiterDispatcher) getRateLimitCondition(request *restful.Request, response *restful.Response) {
	condition := request.PathParameter("condition")
	status, err := s.rateLimiter.GetRateLimitCondition("", condition)
	if err != nil {
		responseError(response, http.StatusInternalServerError, fmt.Errorf("get flow control status error: %v", err))
		return
	}

	err = filters.SetExtraRequestInfo(request.Request.Context(), status.Spec.UpstreamCluster, status.Spec.Instance)
	if err != nil {
		klog.Errorf("Set extra request info error: %v", err)
	}

	err = writeJSONResponse(response, status)
	if err != nil {
		responseError(response, http.StatusInternalServerError, fmt.Errorf("write response error: %v", err))
		return
	}
}

// report upstream status for a client instance
func (s *LimiterDispatcher) reportRateLimitConditionStatus(request *restful.Request, response *restful.Response) {
	conditionName := request.PathParameter("condition")

	var rateLimitCondition v1alpha1.RateLimitCondition
	err := request.ReadEntity(&rateLimitCondition)
	if err != nil {
		responseError(response, http.StatusBadRequest, fmt.Errorf("read requst body error: %v", err))
		return
	}

	domain := rateLimitCondition.Spec.UpstreamCluster

	err = filters.SetExtraRequestInfo(request.Request.Context(), domain, rateLimitCondition.Spec.Instance)
	if err != nil {
		klog.Errorf("Set extra request info error: %v", err)
	}

	if util.GenerateRateLimitConditionName(domain, rateLimitCondition.Spec.Instance) != conditionName {
		responseError(response, http.StatusBadRequest, fmt.Errorf("ratelimit condition name must be format: {domain}.{instance}"))
		return
	}

	status, err := s.rateLimiter.UpdateRateLimitConditionStatus(domain, &rateLimitCondition)
	if err != nil {
		responseError(response, http.StatusInternalServerError, fmt.Errorf("report status error: %v", err))
		return
	}

	err = writeJSONResponse(response, status)
	if err != nil {
		responseError(response, http.StatusInternalServerError, fmt.Errorf("write response error: %v", err))
		return
	}
}

// do limit for global sync counting
func (s *LimiterDispatcher) doAcquire(request *restful.Request, response *restful.Response) {
	var acquireRequest v1alpha1.RateLimitAcquire
	err := request.ReadEntity(&acquireRequest)
	if err != nil {
		responseError(response, http.StatusBadRequest, fmt.Errorf("invalid ratelimt request: %v", err))
		return
	}
	domain := acquireRequest.Name

	err = filters.SetExtraRequestInfo(request.Request.Context(), domain, acquireRequest.Spec.Instance)
	if err != nil {
		klog.Errorf("Set extra request info error: %v", err)
	}

	if len(acquireRequest.Spec.Instance) == 0 {
		responseError(response, http.StatusBadRequest, fmt.Errorf("instance can not be empyt"))
		return
	}

	result, err := s.rateLimiter.DoAcquire(domain, &acquireRequest)
	if err != nil {
		code := http.StatusInternalServerError
		switch t := err.(type) {
		case apierrors.APIStatus:
			code = int(t.Status().Code)
		}

		responseError(response, code, err)
		return
	}

	err = response.WriteAsJson(result)
	if err != nil {
		responseError(response, http.StatusInternalServerError, fmt.Errorf("write response error: %v", err))
		return
	}
}

func (s *LimiterDispatcher) serverInfo(request *restful.Request, response *restful.Response) {
	//domain := request.PathParameter("domain")
	info, err := s.rateLimiter.ServerInfo()
	if err != nil {
		responseError(response, http.StatusInternalServerError, fmt.Errorf("get server status error: %v", err))
		return
	}

	err = response.WriteAsJson(info)
	if err != nil {
		responseError(response, http.StatusInternalServerError, fmt.Errorf("write response error: %v", err))
		return
	}
}

func (s *LimiterDispatcher) heartbeat(request *restful.Request, response *restful.Response) {
	instance := request.QueryParameter("instance")

	err := filters.SetExtraRequestInfo(request.Request.Context(), "", instance)
	if err != nil {
		klog.Errorf("Set extra request info error: %v", err)
	}

	err = s.rateLimiter.Heartbeat(instance)
	if err != nil {
		responseError(response, http.StatusInternalServerError, fmt.Errorf("get server status error: %v", err))
		return
	}
}

func responseError(response *restful.Response, httpStatus int, err error) {
	klog.Errorf("response error: %v", err)
	_ = response.WriteError(httpStatus, err)
}

// Derived from go-restful writeJSON.
func writeJSONResponse(response *restful.Response, value runtime.Object) error {
	data, err := runtime.Encode(scheme.Codecs, value)
	if err != nil {
		return err
	}

	if data == nil {
		response.WriteHeader(http.StatusOK)
		// do not write a nil representation
		return nil
	}
	response.Header().Set(restful.HEADER_ContentType, restful.MIME_JSON)
	response.WriteHeader(http.StatusOK)
	if _, err := response.Write(data); err != nil {
		klog.Errorf("Error writing response: %v", err)
	}
	return nil
}
