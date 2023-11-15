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
	"net/http"
	"sync"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog"

	"github.com/kubewharf/kubegateway/pkg/gateway/metrics"
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
	// http1 proxy round tripper for websockt
	PorxyUpgradeTransport http.RoundTripper

	clientset kubernetes.Interface

	status endpointStatus

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
