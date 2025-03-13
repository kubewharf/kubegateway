// Copyright 2022 ByteDance and its affiliates.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package clusters

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	utilnet "k8s.io/apimachinery/pkg/util/net"
	"k8s.io/apimachinery/pkg/util/proxy"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	kts "k8s.io/client-go/transport"
	"k8s.io/klog"

	"github.com/kubewharf/kubegateway/pkg/gateway/metrics"
	"github.com/kubewharf/kubegateway/pkg/transport"
)

type endpointStatus struct {
	Healthy  bool
	Reason   string
	Message  string
	Disabled bool
	mux      sync.RWMutex
}

func (s *endpointStatus) IsReady() bool {
	s.mux.RLock()
	defer s.mux.RUnlock()
	return !s.Disabled && s.Healthy
}

func (s *endpointStatus) SetDisabled(disabled bool) {
	s.mux.Lock()
	defer s.mux.Unlock()
	s.Disabled = disabled
}

func (s *endpointStatus) SetStatus(healthy bool, reason, message string) {
	s.mux.Lock()
	defer s.mux.Unlock()
	s.Healthy = healthy
	s.Reason = reason
	s.Message = message
}

type EndpointInfo struct {
	ctx    context.Context
	cancel context.CancelFunc

	Cluster  string
	Endpoint string

	proxyConfig        *rest.Config
	proxyUpgradeConfig *rest.Config
	// http2 proxy round tripper
	ProxyTransport http.RoundTripper
	// http1 proxy round tripper for websocket
	PorxyUpgradeTransport proxy.UpgradeRequestRoundTripper

	clientset    kubernetes.Interface
	cancelableTs *transport.CancelableTransport

	status *endpointStatus

	healthCheckFun    EndpointHealthCheck
	healthCheckCh     chan struct{}
	cancelHealthCheck context.CancelFunc
	sync.Mutex
}

func (e *EndpointInfo) Context() context.Context {
	return e.ctx
}

func (e *EndpointInfo) Clientset() kubernetes.Interface {
	return e.clientset
}

func (e *EndpointInfo) createTransport() (*transport.CancelableTransport, http.RoundTripper, *kubernetes.Clientset, error) {
	ts, err := newTransport(e.proxyConfig)
	if err != nil {
		klog.Errorf("failed to create http2 transport for <cluster:%s,endpoint:%s>, err: %v", e.Cluster, e.Endpoint, err)
		return nil, nil, nil, err
	}
	cancelableTs := transport.NewCancelableTransport(ts)
	ts = cancelableTs

	proxyTs, err := rest.HTTPWrappersForConfig(e.proxyConfig, ts)
	if err != nil {
		klog.Errorf("failed to wrap http2 transport for <cluster:%s,endpoint:%s>, err: %v", e.Cluster, e.Endpoint, err)
		return nil, nil, nil, err
	}

	clientsetConfig := *e.proxyConfig
	clientsetConfig.Transport = ts // let client set use the same transport as proxy
	clientsetConfig.TLSClientConfig = rest.TLSClientConfig{}
	client, err := kubernetes.NewForConfig(&clientsetConfig)
	if err != nil {
		klog.Errorf("failed to create clientset for <cluster:%s,endpoint:%s>, err: %v", e.Cluster, e.Endpoint, err)
		return nil, nil, nil, err
	}

	return cancelableTs, proxyTs, client, nil
}

func (e *EndpointInfo) ResetTransport() error {
	cancelableTs, ts, client, err := e.createTransport()
	if err != nil {
		return err
	}
	klog.Infof("set new transport %p for cluster %s endpoint: %s", cancelableTs, e.Cluster, e.Endpoint)
	e.ProxyTransport = ts
	e.clientset = client
	cancelTs := e.cancelableTs
	e.cancelableTs = cancelableTs
	if cancelTs != nil {
		klog.Infof("close transport %p for cluster %s endpoint: %s", cancelTs, e.Cluster, e.Endpoint)
		cancelTs.Close()
	}
	return nil
}

func newTransport(cfg *rest.Config) (http.RoundTripper, error) {
	config, err := cfg.TransportConfig()
	if err != nil {
		return nil, err
	}
	tlsConfig, err := kts.TLSConfigFor(config)
	if err != nil {
		return nil, err
	}
	// The options didn't require a custom TLS config
	if tlsConfig == nil && config.Dial == nil {
		return http.DefaultTransport, nil
	}
	dial := config.Dial
	if dial == nil {
		dial = (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext
	}
	return utilnet.SetTransportDefaults(&http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		TLSHandshakeTimeout: 10 * time.Second,
		TLSClientConfig:     tlsConfig,
		MaxIdleConnsPerHost: 25,
		DialContext:         dial,
		DisableCompression:  config.DisableCompression,
	}), nil
}

func (e *EndpointInfo) SetDisabled(disabled bool) {
	if e.status.Disabled != disabled {
		e.status.Disabled = disabled
		e.recordStatusChange()
	}
}

func (e *EndpointInfo) IstDisabled() bool {
	return e.status.Disabled
}

func (e *EndpointInfo) UpdateStatus(healthy bool, reason, message string) {
	if !healthy {
		metrics.RecordUnhealthyUpstream(e.Cluster, e.Endpoint, reason)
	}
	if e.status.Healthy != healthy {
		// healthy changed
		e.status.SetStatus(healthy, reason, message)
		e.recordStatusChange()
	}
}

func (e *EndpointInfo) TriggerHealthCheck() {
	if e.healthCheckCh == nil {
		e.healthCheckCh = make(chan struct{}, 1)
	}

	select {
	case e.healthCheckCh <- struct{}{}:
	default:
	}
}

func EnsureGatewayHealthCheck(e *EndpointInfo, interval time.Duration, ctx context.Context) {
	if e.healthCheckFun == nil {
		return
	}

	if e.IstDisabled() && e.cancelHealthCheck != nil {
		e.Lock()
		cancel := e.cancelHealthCheck
		e.cancelHealthCheck = nil
		e.Unlock()
		cancel()
	}

	if !e.IstDisabled() && e.cancelHealthCheck == nil {
		e.Lock()
		if e.cancelHealthCheck == nil {
			newCtx, cancel := context.WithCancel(ctx)
			e.cancelHealthCheck = cancel
			startGatewayHealthCheck(e, interval, newCtx)
		}
		e.Unlock()
	}
}

func startGatewayHealthCheck(e *EndpointInfo, interval time.Duration, ctx context.Context) {
	if e.healthCheckCh == nil {
		e.healthCheckCh = make(chan struct{}, 1)
	}

	go func() {
		tick := time.NewTicker(interval)
		defer tick.Stop()

		// trigger health check immediately
		e.healthCheckCh <- struct{}{}
		for {
			select {
			case <-tick.C:
				e.healthCheckCh <- struct{}{}
			case <-ctx.Done():
				return
			}
		}
	}()

	go func() {
		klog.V(2).Infof("[endpoint info] start health checking for cluster=%q, endpoint=%q", e.Cluster, e.Endpoint)
		defer klog.V(2).Infof("[endpoint info] stop health checking for cluster=%q, endpoint=%q", e.Cluster, e.Endpoint)

		for {
			select {
			case <-e.healthCheckCh:
				e.healthCheckFun(e)
			case <-ctx.Done():
				return
			}
		}
	}()
}

func (e *EndpointInfo) recordStatusChange() {
	klog.V(1).Infof(
		"[endpoint info] endpoint status changed, cluster=%q, endpoint=%q, disabled=%v, healthy=%v, reason=%q, message=%q",
		e.Cluster, e.Endpoint, e.status.Disabled, e.status.Healthy, e.status.Reason, e.status.Message,
	)
}

func (e *EndpointInfo) IsReady() bool {
	return e.status.IsReady()
}

func (e *EndpointInfo) UnreadyReason() string {
	message := ""
	if e.status.Disabled {
		message = fmt.Sprintf("endpoint=%q is disabled.", e.Endpoint)
	} else if !e.status.Healthy {
		message = fmt.Sprintf("endpoint=%q is unhealthy, reason=%q, message=%q.", e.Endpoint, e.status.Reason, e.status.Message)
	}
	return message
}

type EndpointInfoMap struct {
	data sync.Map
}

func (m *EndpointInfoMap) Load(name string) (*EndpointInfo, bool) {
	v, loaded := m.data.Load(name)
	if !loaded {
		return nil, false
	}
	return v.(*EndpointInfo), true
}

func (m *EndpointInfoMap) LoadAndDelete(name string) (*EndpointInfo, bool) {
	v, loaded := m.data.LoadAndDelete(name)
	if !loaded {
		return nil, false
	}
	return v.(*EndpointInfo), true
}

func (m *EndpointInfoMap) Store(name string, ep *EndpointInfo) {
	m.data.Store(name, ep)
}

func (m *EndpointInfoMap) LoadOrStore(name string, ep *EndpointInfo) (*EndpointInfo, bool) {
	v, loaded := m.data.LoadOrStore(name, ep)
	return v.(*EndpointInfo), loaded
}

func (m *EndpointInfoMap) Range(rangeFn func(name string, info *EndpointInfo) bool) {
	m.data.Range(func(key, value interface{}) bool {
		return rangeFn(key.(string), value.(*EndpointInfo))
	})
}

func (m *EndpointInfoMap) Names() []string {
	names := []string{}
	m.Range(func(name string, info *EndpointInfo) bool {
		names = append(names, name)
		return true
	})
	return names
}
