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
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pkg/errors"
	"github.com/zoumo/goset"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/util/net"
	"k8s.io/apimachinery/pkg/util/proxy"
	"k8s.io/apiserver/pkg/authorization/authorizer"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/cert"
	"k8s.io/component-base/featuregate"
	"k8s.io/klog"

	proxyv1alpha1 "github.com/kubewharf/kubegateway/pkg/apis/proxy/v1alpha1"
	"github.com/kubewharf/kubegateway/pkg/clusters/features"
	gatewayflowcontrol "github.com/kubewharf/kubegateway/pkg/flowcontrols"
	"github.com/kubewharf/kubegateway/pkg/flowcontrols/flowcontrol"
	"github.com/kubewharf/kubegateway/pkg/ratelimiter/clientsets"
	"github.com/kubewharf/kubegateway/pkg/transport"
)

var (
	ErrNoReadyEndpoints    = errors.New("no ready endpoints")
	ErrNoRouterRuleMatches = errors.New("no router rule matches this request")
)

// EndpointPicker knows
type EndpointPicker interface {
	FlowControl() flowcontrol.FlowControl
	FlowControlName() string
	Pop() (*EndpointInfo, error)
	EnableLog() bool
}

// endpointPickStrategy implement EndpointPicker interface
type endpointPickStrategy struct {
	cluster         *ClusterInfo
	strategy        proxyv1alpha1.Strategy
	flowControl     flowcontrol.FlowControl
	flowControlName string
	upstreams       []string
	enableLog       bool
}

func (s *endpointPickStrategy) Pop() (*EndpointInfo, error) {
	if len(s.upstreams) == 0 {
		return nil, ErrNoReadyEndpoints
	}
	readyEndpoints := []*EndpointInfo{}
	unreadyReason := []string{}
	for _, ep := range s.upstreams {
		info, ok := s.cluster.Endpoints.Load(ep)
		if ok {
			if info.IsReady() {
				readyEndpoints = append(readyEndpoints, info)
			} else {
				unreadyReason = append(unreadyReason, info.UnreadyReason())
			}
		}
	}
	if len(readyEndpoints) == 0 {
		return nil, errors.WithMessage(ErrNoReadyEndpoints, strings.Join(unreadyReason, " "))
	}

	if len(readyEndpoints) == 1 {
		return readyEndpoints[0], nil
	}

	// TODO: apply strategy
	key := fmt.Sprintf("%v", readyEndpoints)
	var i uint64
	lb, _ := s.cluster.loadbalancer.LoadOrStore(key, &i)
	index := atomic.AddUint64(lb.(*uint64), 1)
	index = index % uint64(len(readyEndpoints))
	return readyEndpoints[index], nil
}

func (s *endpointPickStrategy) EnableLog() bool {
	return s.enableLog
}

func (s *endpointPickStrategy) FlowControl() flowcontrol.FlowControl {
	return s.flowControl
}

func (s *endpointPickStrategy) FlowControlName() string {
	return s.flowControlName
}

// ClusterInfo is a wrapper to a UpstreamCluster with additional information
type ClusterInfo struct {
	// server Cluster
	Cluster string

	// serverNames are used to route requests with different hostnames
	serverNames sync.Map

	// global rate limiter type
	globalRateLimiter string

	// Endpoints hold all endpoint info,
	Endpoints *EndpointInfoMap

	// Context will be canceled after cluster stopped or deleted
	ctx    context.Context
	cancel context.CancelFunc

	flowcontrol  gatewayflowcontrol.UpstreamLimiter
	loadbalancer sync.Map

	// upstream endpoint client rest config, the host must be replaced when using it
	restConfig *rest.Config
	// current synced tls config for secure seving
	currentSecureServingTLSConfig atomic.Value
	// current dispatch policies
	currentDispatchPolicies atomic.Value
	// current logging config
	currentLoggingConfig atomic.Value
	featuregate          featuregate.MutableFeatureGate

	healthCheckIntervalSeconds time.Duration
	endpointHeathCheck         EndpointHealthCheck
	skipSyncEndpoints          bool
}

type secureServingConfig struct {
	secureServing *proxyv1alpha1.SecureServing
	clientCA      *x509.CertPool
	certs         []tls.Certificate
	verifyOptions *x509.VerifyOptions
}

// NewEmptyClusterInfo creates a empty ClusterInfo without UpstreamCluster information such as endpoints
func NewEmptyClusterInfo(clusterName string, config *rest.Config, healthCheck EndpointHealthCheck, rateLimiter string, clientSets clientsets.ClientSets) *ClusterInfo {
	clusterName = strings.ToLower(clusterName)
	ctx, cancel := context.WithCancel(context.Background())

	skipEndpoints := false
	if config == nil && healthCheck == nil {
		klog.Warningf("Skip sync endpoints")
		skipEndpoints = true
	}

	limiter := gatewayflowcontrol.NewUpstreamLimiter(ctx, clusterName, "", clientSets)

	info := &ClusterInfo{
		ctx:                        ctx,
		cancel:                     cancel,
		Cluster:                    clusterName,
		restConfig:                 config,
		Endpoints:                  &EndpointInfoMap{data: sync.Map{}},
		healthCheckIntervalSeconds: 5 * time.Second,
		globalRateLimiter:          rateLimiter,
		flowcontrol:                limiter,
		loadbalancer:               sync.Map{},
		endpointHeathCheck:         healthCheck,
		skipSyncEndpoints:          skipEndpoints,
		featuregate:                features.DefaultMutableFeatureGate.DeepCopy(),
	}
	return info
}

// CreateClusterInfo try every endpoint to find a ready endpoint, and then init rest config
func CreateClusterInfo(cluster *proxyv1alpha1.UpstreamCluster,
	healthCheck EndpointHealthCheck,
	rateLimiter string,
	clientSets clientsets.ClientSets,
) (*ClusterInfo, error) {
	restconfig, err := buildClusterRESTConfig(cluster)
	if err != nil {
		return nil, err
	}

	klog.Infof("create valid rest config for cluster: %v", cluster.Name)
	info := NewEmptyClusterInfo(cluster.Name, restconfig, healthCheck, rateLimiter, clientSets)
	err = info.Sync(cluster)
	if err != nil {
		return nil, err
	}
	return info, nil
}

func (c *ClusterInfo) Context() context.Context {
	return c.ctx
}

func (c *ClusterInfo) LoadTLSConfig() (*tls.Config, bool) {
	cfg, ok := c.loadSecureServingConfig()
	if !ok {
		return nil, false
	}
	if len(cfg.certs) == 0 && cfg.clientCA == nil {
		return nil, false
	}

	return &tls.Config{
		ClientCAs:    cfg.clientCA,
		Certificates: cfg.certs,
	}, true
}

func (c *ClusterInfo) LoadVerifyOptions() (x509.VerifyOptions, bool) {
	empty := x509.VerifyOptions{}
	cfg, ok := c.loadSecureServingConfig()
	if !ok {
		return empty, false
	}
	if cfg.verifyOptions == nil {
		return empty, false
	}
	return *cfg.verifyOptions, true
}

func (c *ClusterInfo) LoadServerNames() []string {
	var serverNames = []string{c.Cluster}
	cfg, ok := c.loadSecureServingConfig()
	if ok {
		for _, serverName := range cfg.secureServing.ServerNames {
			serverNames = append(serverNames, strings.ToLower(serverName))
		}
	}

	return serverNames
}

func (c *ClusterInfo) loadSecureServingConfig() (secureServingConfig, bool) {
	empty := secureServingConfig{
		secureServing: &proxyv1alpha1.SecureServing{},
	}
	uncastObj := c.currentSecureServingTLSConfig.Load()
	if uncastObj == nil {
		return empty, false
	}
	spec, ok := uncastObj.(*secureServingConfig)
	if !ok {
		return empty, false
	}
	if spec == nil {
		return empty, false
	}
	return *spec, true
}

func (c *ClusterInfo) loadDispatchPolicies() []proxyv1alpha1.DispatchPolicy {
	uncastObj := c.currentDispatchPolicies.Load()
	if uncastObj == nil {
		return nil
	}

	polices, ok := uncastObj.([]proxyv1alpha1.DispatchPolicy)
	if !ok {
		return nil
	}
	return polices
}

func (c *ClusterInfo) loadLoggingConfig() proxyv1alpha1.LoggingConfig {
	empty := proxyv1alpha1.LoggingConfig{}
	uncastObj := c.currentLoggingConfig.Load()
	if uncastObj == nil {
		return empty
	}
	cfg, ok := uncastObj.(proxyv1alpha1.LoggingConfig)
	if !ok {
		return empty
	}
	return cfg
}

// Sync will only be triggered by upstream event handler, it is single thread.
// so there is no need to add a lock
// TODO: how to deal with clientConfig changes
func (c *ClusterInfo) Sync(cluster *proxyv1alpha1.UpstreamCluster) error {
	if c.Cluster != strings.ToLower(cluster.Name) {
		klog.V(3).Infof("[cluster info] skip syncing cluster because input cluster name is mismatching, %v != %v", c.Cluster, cluster.Name)
		return nil
	}

	klog.V(5).Infof("[cluster info] syncing cluster info, name=%q", c.Cluster)

	if cluster.Annotations != nil {
		if err := c.syncFeatureGate(cluster.Annotations); err != nil {
			// we should never get here because there is validating admission
			return err
		}
	}

	// sync flow control type
	c.flowcontrol.ResetLimiter(getFlowControlType(c.globalRateLimiter, c.featuregate))

	// update flow control
	c.flowcontrol.Sync(cluster.Spec.FlowControl)

	// update secure serving
	if err := c.syncSecureServingConfigLocked(cluster.Spec.SecureServing); err != nil {
		return err
	}

	// add or update endpoints
	if err := c.syncEndpoints(cluster.Spec.Servers); err != nil {
		return err
	}

	// set dispatch policies
	c.currentDispatchPolicies.Store(cluster.Spec.DispatchPolicies)
	c.currentLoggingConfig.Store(cluster.Spec.Logging)

	return nil
}

func (c *ClusterInfo) syncEndpoints(servers []proxyv1alpha1.UpstreamClusterServer) error {
	if c.skipSyncEndpoints {
		return nil
	}

	// update endpoints
	currentEPs := goset.NewSetFromStrings(c.AllEndpoints())
	wantedEPs := goset.NewSet()

	for _, e := range servers {
		wantedEPs.Add(e.Endpoint) //nolint
	}

	deleted := currentEPs.Diff(wantedEPs)
	added := wantedEPs.Diff(currentEPs)

	if added.Len() > 0 || deleted.Len() > 0 {
		// servers changed, reset loadbalancer
		c.loadbalancer = sync.Map{}
	}

	deleted.Range(func(index int, elem interface{}) bool {
		ep := elem.(string)
		info, loaded := c.Endpoints.LoadAndDelete(ep)
		if !loaded {
			return true
		}
		klog.Infof("[cluster info] endpoint=%q is deleted from cluster %q", info.Endpoint, c.Cluster)
		if info.cancel != nil {
			info.cancel()
		}
		return true
	})

	var syncErr error

	disabled := goset.NewSet()
	for _, server := range servers {
		if server.Disabled != nil && *server.Disabled {
			disabled.Add(server.Endpoint) //nolint
		}
	}
	wantedEPs.Range(func(index int, elem interface{}) bool {
		ep := elem.(string)
		syncErr = c.addOrUpdateEndpoint(ep, disabled.Contains(ep))
		// stop loop if add or update error
		return syncErr == nil
	})

	return syncErr
}

func (c *ClusterInfo) syncSecureServingConfigLocked(newSecureServing proxyv1alpha1.SecureServing) error {
	oldCfg, _ := c.loadSecureServingConfig()
	if apiequality.Semantic.DeepEqual(oldCfg.secureServing, newSecureServing) {
		return nil
	}
	oldSecureServing := *oldCfg.secureServing

	newCfg := secureServingConfig{
		secureServing: &newSecureServing,
		verifyOptions: oldCfg.verifyOptions,
		clientCA:      oldCfg.clientCA,
		certs:         oldCfg.certs,
	}

	if !apiequality.Semantic.DeepEqual(oldSecureServing.ClientCAData, newSecureServing.ClientCAData) {
		// client ca data changed
		if len(newSecureServing.ClientCAData) == 0 {
			// clean verifyOptions
			klog.Infof("[cluster info] cluster=%q cleanup clientCA and verifyOptions", c.Cluster)
			newCfg.verifyOptions = nil
			newCfg.clientCA = nil
		} else {
			// use new client ca if upstream cluster client ca is not empty
			newClientCAPool := x509.NewCertPool()
			newClientCAs, err := cert.ParseCertsPEM(newSecureServing.ClientCAData)
			if err != nil {
				return fmt.Errorf("unable to load client CA file %q: %v", newSecureServing.ClientCAData, err)
			}

			for _, ca := range newClientCAs {
				newClientCAPool.AddCert(ca)
			}

			verifyOptions := &x509.VerifyOptions{
				KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
				Roots:     newClientCAPool,
			}
			klog.Infof("[cluster info] cluster=%q update clientCA and verifyOptions", c.Cluster)
			newCfg.verifyOptions = verifyOptions
			newCfg.clientCA = newClientCAPool
		}
	}

	if !apiequality.Semantic.DeepEqual(oldSecureServing.KeyData, newSecureServing.KeyData) ||
		!apiequality.Semantic.DeepEqual(oldSecureServing.CertData, newSecureServing.CertData) {
		// key or cert changed
		if len(newSecureServing.KeyData) == 0 && len(newSecureServing.CertData) == 0 {
			klog.Infof("[cluster info] cluster=%q cleanup key and cert", c.Cluster)
			newCfg.certs = nil
		} else if len(newSecureServing.KeyData) > 0 && len(newSecureServing.CertData) > 0 {
			cert, err := tls.X509KeyPair(newSecureServing.CertData, newSecureServing.KeyData)
			if err != nil {
				return fmt.Errorf("invalid serving cert keypair: %v", err)
			}
			klog.Infof("[cluster info] cluster=%q update key and cert", c.Cluster)
			newCfg.certs = []tls.Certificate{cert}
		}
	}

	c.currentSecureServingTLSConfig.Store(&newCfg)
	return nil
}

func (c *ClusterInfo) AllEndpoints() []string {
	return c.Endpoints.Names()
}

func (c *ClusterInfo) Stop() {
	// cancel context to stop all request to this cluster
	if c.cancel != nil {
		c.cancel()
	}
}

// MatchAttributes matches a requestAttributes from reqeust and return a flowcontrol and endpointPicker
func (c *ClusterInfo) MatchAttributes(requestAttributes authorizer.Attributes) (EndpointPicker, error) {
	policies := c.loadDispatchPolicies()
	logging := c.loadLoggingConfig()
	policy := MatchPolicies(requestAttributes, policies)
	if policy == nil {
		return nil, ErrNoRouterRuleMatches
	}

	flowControlName := policy.FlowControlSchemaName
	if len(flowControlName) == 0 {
		flowControlName = "system-default"
	}
	result := &endpointPickStrategy{
		cluster:         c,
		strategy:        policy.Strategy,
		flowControl:     c.GetFlowSchema(policy.FlowControlSchemaName),
		flowControlName: flowControlName,
		enableLog:       isLogEnabled(logging.Mode, policy.LogMode),
	}

	if len(policy.UpstreamSubset) != 0 {
		result.upstreams = policy.UpstreamSubset
	} else {
		result.upstreams = c.AllEndpoints()
	}

	return result, nil
}

func (c *ClusterInfo) PickOne() (*EndpointInfo, error) {
	s := &endpointPickStrategy{
		cluster:   c,
		upstreams: c.AllEndpoints(),
	}
	return s.Pop()
}

func (c *ClusterInfo) GetFlowSchema(name string) flowcontrol.FlowControl {
	return c.flowcontrol.GetOrDefault(name)
}

func (c *ClusterInfo) addOrUpdateEndpoint(endpoint string, disabled bool) error {
	info, ok := c.Endpoints.Load(endpoint)
	if ok {
		info.SetDisabled(disabled)
		EnsureGatewayHealthCheck(info, c.healthCheckIntervalSeconds, info.ctx)
		return nil
	}

	http2configCopy := *c.restConfig
	http2configCopy.WrapTransport = transport.NewDynamicImpersonatingRoundTripper
	http2configCopy.Host = endpoint
	ts, err := rest.TransportFor(&http2configCopy)
	if err != nil {
		klog.Errorf("failed to create http2 transport for <cluster:%s,endpoint:%s>, err: %v", c.Cluster, endpoint, err)
		return err
	}

	// since http2 doesn't support websocket, we need to disable http2 when using websocket
	upgradeConfigCopy := http2configCopy
	upgradeConfigCopy.NextProtos = []string{"http/1.1"}
	ts2, err := rest.TransportFor(&upgradeConfigCopy)
	if err != nil {
		klog.Errorf("failed to create http/1.1 transport for <cluster:%s,endpoint:%s>, err: %v", c.Cluster, endpoint, err)
		return err
	}
	urrt, ok := unwrapUpgradeRequestRoundTripper(ts2)
	if !ok {
		klog.Errorf("failed to convert transport to proxy.UpgradeRequestRoundTripper for <cluster:%s,endpoint:%s>", c.Cluster, endpoint)
	}

	client, err := kubernetes.NewForConfig(&http2configCopy)
	if err != nil {
		klog.Errorf("failed to create clientset for <cluster:%s,endpoint:%s>, err: %v", c.Cluster, endpoint, err)
		return err
	}

	// initial endpoint status
	initStatus := endpointStatus{
		Disabled: disabled,
		Healthy:  false,
	}

	ctx, cancel := context.WithCancel(c.Context())
	info = &EndpointInfo{
		ctx:                   ctx,
		cancel:                cancel,
		Cluster:               c.Cluster,
		Endpoint:              endpoint,
		status:                initStatus,
		proxyConfig:           &http2configCopy,
		ProxyTransport:        ts,
		proxyUpgradeConfig:    &upgradeConfigCopy,
		PorxyUpgradeTransport: urrt,
		clientset:             client,
		healthCheckFun:        c.endpointHeathCheck,
	}

	klog.Infof("[cluster info] new endpoint added, cluster=%q, endpoint=%q", c.Cluster, info.Endpoint)
	c.Endpoints.Store(endpoint, info)

	EnsureGatewayHealthCheck(info, c.healthCheckIntervalSeconds, info.ctx)

	return nil
}

func (c *ClusterInfo) FeatureEnabled(key featuregate.Feature) bool {
	return c.featuregate.Enabled(key)
}

func (c *ClusterInfo) syncFeatureGate(annotations map[string]string) error {
	featuregate := annotations[features.FeatureGateAnnotationKey]
	if len(featuregate) == 0 {
		if !features.IsDefault(c.featuregate) {
			// reset featuregate
			c.featuregate = features.DefaultMutableFeatureGate.DeepCopy()
		}
		return nil
	}
	return c.featuregate.Set(featuregate)
}

// upstream policy    enabled
// off      *         false
// *        off       false
// on       on or ""  true
// on or "" on        true
// ""       ""        false
func isLogEnabled(upstream, policy proxyv1alpha1.LogMode) bool {
	if upstream == proxyv1alpha1.LogOff || policy == proxyv1alpha1.LogOff {
		return false
	}
	if upstream == proxyv1alpha1.LogOn || policy == proxyv1alpha1.LogOn {
		return true
	}
	return false
}

func getFlowControlType(global string, featuregate featuregate.MutableFeatureGate) string {
	if global == flowcontrol.RemoteFlowControls && featuregate.Enabled(features.GlobalRateLimiter) {
		return flowcontrol.RemoteFlowControls
	}
	return flowcontrol.LocalFlowControls
}

func unwrapUpgradeRequestRoundTripper(rt http.RoundTripper) (proxy.UpgradeRequestRoundTripper, bool) {
	urrt, ok := rt.(proxy.UpgradeRequestRoundTripper)
	if ok {
		return urrt, ok
	}

	var rtw net.RoundTripperWrapper
	var isWrapper bool
	rtw, isWrapper = rt.(net.RoundTripperWrapper)
	for isWrapper {
		rt = rtw.WrappedRoundTripper()
		urrt, found := rt.(proxy.UpgradeRequestRoundTripper)
		if found {
			return urrt, true
		}
		rtw, isWrapper = rt.(net.RoundTripperWrapper)
	}

	return nil, false
}
