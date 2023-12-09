package ratelimiter

import (
	apiserver "k8s.io/apiserver/pkg/server"
	"k8s.io/client-go/informers"
	"net/http"

	clientset "k8s.io/client-go/kubernetes"

	"github.com/kubewharf/kubegateway/pkg/ratelimiter/limiter"
)

type Server struct {
	Name string

	SecureServing   *apiserver.SecureServingInfo
	InsecureServing *apiserver.DeprecatedInsecureServingInfo
	Client          clientset.Interface
	InformerFactory informers.SharedInformerFactory
	RateLimiter     limiter.RateLimiter

	SecureHandler   http.Handler
	InsecureHandler http.Handler
}

func (s *Server) PrepareRun() PreparedServer {
	return s
}

// Run spawns the secure http server. It only returns if stopCh is closed
// or the secure port cannot be listened on initially.
func (s *Server) Run(stopCh <-chan struct{}) (err error) {
	internalStopCh := make(chan struct{})
	go func() {
		<-stopCh
		// TODO graceful shutdown

		close(internalStopCh)
	}()

	var stoppedCh <-chan struct{}
	if s.SecureServing != nil {
		if stoppedCh, err = s.SecureServing.Serve(s.SecureHandler, 0, internalStopCh); err != nil {
			return err
		}
	}

	if s.InsecureServing != nil {
		if err := s.InsecureServing.Serve(s.InsecureHandler, 0, internalStopCh); err != nil {
			return err
		}
	}

	go s.RateLimiter.Run(internalStopCh)

	<-internalStopCh
	if stoppedCh != nil {
		<-stoppedCh
	}

	return nil
}

type PreparedServer interface {
	Run(stopCh <-chan struct{}) error
}
