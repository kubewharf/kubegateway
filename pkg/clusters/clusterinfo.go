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
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pkg/errors"
	"github.com/zoumo/goset"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apiserver/pkg/authorization/authorizer"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/cert"
	"k8s.io/component-base/featuregate"
	"k8s.io/klog"

	proxyv1alpha1 "github.com/kubewharf/kubegateway/pkg/apis/proxy/v1alpha1"
	"github.com/kubewharf/kubegateway/pkg/clusters/features"
	gatewayflowcontrol "github.com/kubewharf/kubegateway/pkg/flowcontrol"
	"github.com/kubewharf/kubegateway/pkg/transport"
)

var (
	ErrNoReadyEndpoints    = errors.New("no ready endpoints")
	ErrNoRouterRuleMatches = errors.New("no router rule matches this request")
)

// EndpointPicker knows
type EndpointPicker interface {
	FlowControl() gatewayflowcontrol.FlowControl
	Pop() (*EndpointInfo, error)
	EnableLog() bool
}

// endpointPickStrategy implement EndpointPicker interface
type endpointPickStrategy struct {
	cluster     *ClusterInfo
	strategy    proxyv1alpha1.Strategy
	flowControl gatewayflowcontrol.FlowControl
	upstreams   []string
	enableLog   bool
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

func (s *endpointPickStrategy) FlowControl() gatewayflowcontrol.FlowControl {
	return s.flowControl
}

// ClusterInfo is a wrapper to a UpstreamCluster with additional information
type ClusterInfo struct {
	// server Cluster
	Cluster string

	// Endpoints hold all endpoint info,
	Endpoints *EndpointInfoMap

	// Context will be canceled after cluster stopped or deleted
	ctx    context.Context
	cancel context.CancelFunc

	defaultFlowControl gatewayflowcontrol.FlowControl
	flowcontrol        *gatewayflowcontrol.FlowControls
	loadbalancer       sync.Map

	// upstream endpoint client rest config, the host must be replaced when using it
	restConfig *rest.Config
	// current synced flow controler spec
	currentFlowControlSpec atomic.Value
	// current synced tls config for secure seving
	currentSecureServingTLSConfig atomic.Value
	// current dispatch policies
	currentDispatchPolicies atomic.Value
	// current logging config
	currentLoggingConfig atomic.Value
	featuregate          featuregate.MutableFeatureGate

	healthCheckIntervalSeconds time.Duration
	endpointHeathCheck         EndpointHealthCheck
}

type secureServingConfig struct {
	secureServing *proxyv1alpha1.SecureServing
	clientCA      *x509.CertPool
	certs         []tls.Certificate
	verifyOptions *x509.VerifyOptions
}

// NewEmptyClusterInfo creates a empty ClusterInfo without UpstreamCluster information such as endpoints
func NewEmptyClusterInfo(clusterName string, config *rest.Config, healthCheck EndpointHealthCheck) *ClusterInfo {
	clusterName = strings.ToLower(clusterName)
	ctx, cancel := context.WithCancel(context.Background())
	info := &ClusterInfo{
		ctx:                        ctx,
		cancel:                     cancel,
		Cluster:                    clusterName,
		restConfig:                 config,
		Endpoints:                  &EndpointInfoMap{data: sync.Map{}},
		healthCheckIntervalSeconds: 5 * time.Second,
		defaultFlowControl:         gatewayflowcontrol.DefaultFlowControl,
		flowcontrol:                gatewayflowcontrol.NewFlowControls(),
		loadbalancer:               sync.Map{},
		endpointHeathCheck:         healthCheck,
		featuregate:                features.DefaultMutableFeatureGate.DeepCopy(),
	}
	return info
}

// CreateClusterInfo try every endpoint to find a ready endpoint, and then init rest config
func CreateClusterInfo(cluster *proxyv1alpha1.UpstreamCluster, healthCheck EndpointHealthCheck) (*ClusterInfo, error) {
	restconfig, err := buildClusterRESTConfig(cluster)
	if err != nil {
		return nil, err
	}

	klog.Infof("create valid rest config for cluster: %v", cluster.Name)
	info := NewEmptyClusterInfo(cluster.Name, restconfig, healthCheck)
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

func (c *ClusterInfo) loadFlowControlSpec() (proxyv1alpha1.FlowControl, bool) {
	empty := proxyv1alpha1.FlowControl{}
	uncastObj := c.currentFlowControlSpec.Load()
	if uncastObj == nil {
		return empty, false
	}
	spec, ok := uncastObj.(*proxyv1alpha1.FlowControl)
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

	// update flow control
	c.syncFlowControlLocked(cluster.Spec.FlowControl)

	// update secure serving
	if err := c.syncSecureServingConfigLocked(cluster.Spec.SecureServing); err != nil {
		return err
	}

	// add or update endpoints
	if err := c.syncEndpoints(cluster.Spec.Servers); err != nil {
		return err
	}

	if err := c.syncFeatureGate(cluster.Annotations); err != nil {
		// we should never get here because there is validating admission
		return err
	}

	// set dispatch policies
	c.currentDispatchPolicies.Store(cluster.Spec.DispatchPolicies)
	c.currentLoggingConfig.Store(cluster.Spec.Logging)

	return nil
}

func (c *ClusterInfo) syncEndpoints(servers []proxyv1alpha1.UpstreamClusterServer) error {
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

func (c *ClusterInfo) syncFlowControlLocked(newObj proxyv1alpha1.FlowControl) {
	oldObj, _ := c.loadFlowControlSpec()
	if apiequality.Semantic.DeepEqual(oldObj, newObj) {
		return
	}

	defer func() {
		c.currentFlowControlSpec.Store(&newObj)
	}()

	oldMap := map[string]proxyv1alpha1.FlowControlSchema{}

	oldset := goset.NewSet()
	newset := goset.NewSet()

	for i, schema := range oldObj.Schemas {
		oldset.Add(schema.Name) //nolint
		oldMap[schema.Name] = oldObj.Schemas[i]
	}

	for _, newSchema := range newObj.Schemas {
		newset.Add(newSchema.Name) //nolint
		oldSchema := oldMap[newSchema.Name]
		oldType := gatewayflowcontrol.GuessFlowControlSchemaType(oldSchema)
		newType := gatewayflowcontrol.GuessFlowControlSchemaType(newSchema)
		fc, ok := c.flowcontrol.Load(newSchema.Name)
		if !ok || oldType != newType {
			// flow control is not created or type changed
			newFC := gatewayflowcontrol.NewFlowControl(newSchema)
			c.flowcontrol.Store(newSchema.Name, newFC)
			klog.Infof("[cluster info] cluster=%q ensure flowcontrol schema %v", c.Cluster, newFC.String())
			continue
		}
		if ok {
			switch newType {
			case proxyv1alpha1.MaxRequestsInflight:
				if fc.Resize(uint32(newSchema.MaxRequestsInflight.Max), 0) {
					klog.Infof("[cluster info] cluster=%q resize flowcontrol schema=%q", c.Cluster, fc.String())
				}
			case proxyv1alpha1.TokenBucket:
				if fc.Resize(uint32(newSchema.TokenBucket.QPS), uint32(newSchema.TokenBucket.Burst)) {
					klog.Infof("[cluster info] cluster=%q resize flowcontrol schema=%q", c.Cluster, fc.String())
				}
			}
		}
	}

	deleted := oldset.Diff(newset)

	deleted.Range(func(_ int, elem interface{}) bool {
		name := elem.(string)
		klog.Infof("[cluster info] cluster=%q delete flowcontrol schema=%q", c.Cluster, name)
		c.flowcontrol.Delete(name)
		return true
	})
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

	result := &endpointPickStrategy{
		cluster:     c,
		strategy:    policy.Strategy,
		flowControl: c.getFlowSchema(policy.FlowControlSchemaName),
		enableLog:   isLogEnabled(logging.Mode, policy.LogMode),
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

func (c *ClusterInfo) getFlowSchema(name string) gatewayflowcontrol.FlowControl {
	if len(name) == 0 {
		return c.defaultFlowControl
	}
	load, ok := c.flowcontrol.Load(name)
	if !ok {
		return c.defaultFlowControl
	}
	return load
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
		PorxyUpgradeTransport: ts2,
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
