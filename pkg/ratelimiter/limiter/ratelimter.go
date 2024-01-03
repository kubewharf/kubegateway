package limiter

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog"

	proxyv1alpha1 "github.com/kubewharf/kubegateway/pkg/apis/proxy/v1alpha1"
	gatewayclientset "github.com/kubewharf/kubegateway/pkg/client/kubernetes"
	"github.com/kubewharf/kubegateway/pkg/flowcontrols/flowcontrol"
	"github.com/kubewharf/kubegateway/pkg/ratelimiter/limiter/controller"
	"github.com/kubewharf/kubegateway/pkg/ratelimiter/limiter/elector"
	"github.com/kubewharf/kubegateway/pkg/ratelimiter/metrics"
	"github.com/kubewharf/kubegateway/pkg/ratelimiter/options"
	"github.com/kubewharf/kubegateway/pkg/ratelimiter/store"
	"github.com/kubewharf/kubegateway/pkg/ratelimiter/store/interface"
	"github.com/kubewharf/kubegateway/pkg/ratelimiter/util"
)

const (
	ClientHeartBeatTimeout = time.Second * 3
	syncPeriod             = time.Second
	cleanupPeriod          = time.Second * 30

	RateLimitConditionInstanceLabel = "proxy.kubegateway.io/ratelimitcondition.instance"
)

func NewRateLimiter(gatewayClient gatewayclientset.Interface, client kubernetes.Interface, limitOptions options.RateLimitOptions) (RateLimiter, error) {
	leaderElector, err := elector.NewLeaderElector(limitOptions.LeaderElectionConfiguration, client, limitOptions.Identity, limitOptions.ShardingCount)
	if err != nil {
		return nil, err
	}

	limiter := &rateLimiter{
		runId:         rand.String(8),
		identity:      limitOptions.Identity,
		shardCount:    limitOptions.ShardingCount,
		limitOptions:  limitOptions,
		gatewayClient: gatewayClient,
		leaderElector: leaderElector,
		clientCache:   NewClientCache(),
		limitStoreMap: map[int]_interface.LimitStore{},
		upstreamLock:  map[string]*sync.Mutex{},
	}

	controller := controller.NewUpstreamController(gatewayClient, limiter.UpstreamConditionHandler)
	limiter.upstreamController = controller

	leaderElector.SetCallbacks(elector.LeaderCallbacks{
		OnStartedLeading: limiter.startLeading,
		OnStoppedLeading: limiter.stopLeading,
	})

	return limiter, nil
}

type RateLimiter interface {
	Run(stopCh <-chan struct{})
	DoAcquire(upstream string, acquire *proxyv1alpha1.RateLimitAcquire) (*proxyv1alpha1.RateLimitAcquire, error)
	UpdateRateLimitConditionStatus(upstream string, condition *proxyv1alpha1.RateLimitCondition) (*proxyv1alpha1.RateLimitCondition, error)
	GetUpstreamStatus(upstream string) (*proxyv1alpha1.RateLimitCondition, error)
	GetRateLimitCondition(upstream, conditionName string) (*proxyv1alpha1.RateLimitCondition, error)
	Heartbeat(instance string) error
	GetLeaders() map[int]proxyv1alpha1.EndpointInfo
	ServerInfo() (*proxyv1alpha1.RateLimitServerInfo, error)
}

type rateLimiter struct {
	runId      string
	identity   string
	shardCount int

	limitOptions  options.RateLimitOptions
	gatewayClient gatewayclientset.Interface

	leaderElector  elector.LeaderElector
	clientCache    *ClientCache
	limitStoreLock sync.RWMutex
	limitStoreMap  map[int]_interface.LimitStore

	upstreamController controller.UpstreamController
	upstreamLock       map[string]*sync.Mutex
}

func (r *rateLimiter) Run(stopCh <-chan struct{}) {
	klog.Info("starting rate limiter")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// start upstream controller
	go r.upstreamController.Run(stopCh)

	// start periodic sync
	go wait.Until(r.sync, syncPeriod, stopCh)
	go wait.Until(r.cleanupUnknownCondition, cleanupPeriod, stopCh)

	r.leaderElector.Run(ctx)

	<-stopCh

	klog.Info("rate limiter exited")
}

func (r *rateLimiter) UpdateRateLimitConditionStatus(upstream string, condition *proxyv1alpha1.RateLimitCondition) (*proxyv1alpha1.RateLimitCondition, error) {
	shardId := util.GetShardID(upstream, r.shardCount)
	if !r.leaderElector.IsLeader(shardId) {
		return condition, fmt.Errorf("upstream %s, shard %v, leader is %v", upstream, shardId, r.leaderElector.GetLeaders()[shardId].Leader)
	}

	klog.V(4).Infof("Update condition %s status: %+v", condition.Name, condition.Status)

	limitStore := r.getLimitStoreForShard(shardId)
	if limitStore == nil {
		return nil, fmt.Errorf("limit store for upstream %s upstream shard %v not found", upstream, shardId)
	}

	upstreamCondition, err := limitStore.Get(condition.Spec.UpstreamCluster, upstreamStateConditionName(condition.Spec.UpstreamCluster))
	if err != nil {
		return nil, err
	}

	oldCondition, err := limitStore.Get(condition.Spec.UpstreamCluster, condition.Name)
	if errors.IsNotFound(err) {
		oldCondition = &proxyv1alpha1.RateLimitCondition{
			TypeMeta:   upstreamCondition.TypeMeta,
			ObjectMeta: metav1.ObjectMeta{Name: condition.Name},
			Spec: proxyv1alpha1.RateLimitSpec{
				UpstreamCluster:         upstreamCondition.Spec.UpstreamCluster,
				LimitItemConfigurations: condition.Spec.DeepCopy().LimitItemConfigurations,
			},
		}
	} else if err != nil {
		return nil, err
	}

	if condition.Labels == nil {
		condition.Labels = map[string]string{}
	}
	condition.Labels["proxy.kubegateway.io/ratelimitcondition.instance"] = oldCondition.Spec.Instance

	mutex := r.upstreamLock[condition.Spec.UpstreamCluster]
	mutex.Lock()
	defer mutex.Unlock()

	clients, err := r.clientCache.AllClients()
	if err != nil {
		return nil, err
	}

	flowControlStatusMap := util.FlowControlStatusToMap(condition.Status.LimitItemStatuses)
	upstreamStatusMap := util.FlowControlStatusToMap(upstreamCondition.Status.LimitItemStatuses)
	upstreamFlowControlMap := util.FlowControlConfigToMap(upstreamCondition.Spec.LimitItemConfigurations)

	var newFlowControls []proxyv1alpha1.RateLimitItemConfiguration
	for _, flowControlConfig := range condition.Spec.LimitItemConfigurations {
		flowControlStatus := flowControlStatusMap[flowControlConfig.Name]
		upstreamUsed := upstreamStatusMap[flowControlConfig.Name]
		upstreamTotal, ok := upstreamFlowControlMap[flowControlConfig.Name]
		if !ok {
			klog.V(3).Infof("[condition] name=%s skip not found flowcontrol %s", condition.Name, flowControlConfig.Name)
			continue
		}

		itemType := flowcontrol.GetFlowControlTypeFromLimitItem(flowControlConfig.LimitItemDetail)
		upstreamItemType := flowcontrol.GetFlowControlTypeFromLimitItem(upstreamTotal.LimitItemDetail)
		if itemType != proxyv1alpha1.Unknown && itemType != upstreamItemType {
			return nil, fmt.Errorf("upstream flow control item type %s not equal to instance item type %s", upstreamItemType, itemType)
		}

		newConfig := calculateNextQuota(upstreamTotal, upstreamUsed, flowControlConfig, flowControlStatus, len(clients), condition)
		//klog.V(4).Infof("[condition] name=%q next quota of condition: %+v", condition.Name, newConfig)

		var allocatedLimit int32
		switch itemType {
		case proxyv1alpha1.MaxRequestsInflight:
			allocatedLimit = newConfig.MaxRequestsInflight.Max
		case proxyv1alpha1.TokenBucket:
			allocatedLimit = newConfig.TokenBucket.QPS
		}

		metrics.MonitorAllocateRequest(upstream, condition.Spec.Instance, flowControlConfig.Name, string(itemType), string(flowControlConfig.Strategy), "allocate", allocatedLimit)

		newFlowControls = append(newFlowControls, newConfig)
	}
	condition.Spec.LimitItemConfigurations = newFlowControls

	err = limitStore.Save(condition.Spec.UpstreamCluster, condition)
	if err != nil {
		return nil, err
	}

	upstreamCondition = r.calculateUpstreamCondition(limitStore, upstreamCondition)
	err = limitStore.Save(condition.Spec.UpstreamCluster, upstreamCondition)
	if err != nil {
		return nil, fmt.Errorf("save upstream condition error: %v", err)
	}

	return condition, nil
}

func (r *rateLimiter) DoAcquire(upstream string, acquireRequest *proxyv1alpha1.RateLimitAcquire) (*proxyv1alpha1.RateLimitAcquire, error) {
	shardId := util.GetShardID(upstream, r.shardCount)
	if !r.leaderElector.IsLeader(shardId) {
		return nil, fmt.Errorf("upstream %s, shard %v, leader is %v", upstream, shardId, r.leaderElector.GetLeaders()[shardId].Leader)
	}

	limitStore := r.getLimitStoreForShard(shardId)
	if limitStore == nil {
		return nil, fmt.Errorf("limit store for upstream %s upstream shard %v not found", upstream, shardId)
	}

	var resultLogs []string
	result := &acquireRequest.Status
	for _, limit := range acquireRequest.Spec.Requests {
		rs, err := func() (proxyv1alpha1.RateLimitAcquireResult, error) {
			rs := proxyv1alpha1.RateLimitAcquireResult{
				FlowControl: limit.FlowControl,
			}

			flowControl, err := limitStore.GetFlowControl(upstream, limit.FlowControl)
			if err != nil {
				return rs, err
			}
			if limit.Tokens < 0 {
				return rs, fmt.Errorf("tokens cannot be negative")
			}

			switch flowControl.Type() {
			case proxyv1alpha1.TokenBucket:
				token := limit.Tokens
				for i := 0; i < 4; i++ {
					rs.Accept = flowControl.TryAcquireN(acquireRequest.Spec.Instance, token)
					if rs.Accept {
						rs.Limit = token
						break
					}
					token = token / 2
					if token <= 0 {
						break
					}
				}
				metrics.MonitorAcquiredTokenCounter(upstream, acquireRequest.Spec.Instance, limit.FlowControl, string(flowControl.Type()), string(proxyv1alpha1.GlobalCountLimit), "acquire", rs.Limit)
			case proxyv1alpha1.MaxRequestsInflight:
				threshold := flowControl.SetState(acquireRequest.Spec.Instance, limit.Tokens)
				if threshold < 0 {
					rs.Accept = true
					rs.Limit = limit.Tokens
				} else {
					rs.Accept = false
					rs.Limit = threshold
				}
				metrics.MonitorAllocateRequest(upstream, acquireRequest.Spec.Instance, limit.FlowControl, string(flowControl.Type()), string(proxyv1alpha1.GlobalCountLimit), "acquire", rs.Limit)
			}
			return rs, nil
		}()

		var rslog string
		if err != nil {
			rs.Accept = false
			rs.Error = err.Error()
			rslog = fmt.Sprintf("[name=%q token=%v result=%v error=%v]", limit.FlowControl, limit.Tokens, rs.Accept, rs.Error)
		} else {
			rslog = fmt.Sprintf("[name=%q token=%v result=%v (%v)]", limit.FlowControl, limit.Tokens, rs.Accept, rs.Limit)
		}
		resultLogs = append(resultLogs, rslog)

		result.Results = append(result.Results, rs)
	}

	klog.V(2).Infof("[acquire] upstream=%q instance=%q: %v", upstream, acquireRequest.Spec.Instance, strings.Join(resultLogs, ","))

	return acquireRequest, nil
}

func (r *rateLimiter) GetUpstreamStatus(upstream string) (*proxyv1alpha1.RateLimitCondition, error) {
	limitStore := r.getLimitStore(upstream)
	if limitStore == nil {
		return nil, fmt.Errorf("limit store for upstream %s shard %v not found", upstream, util.GetShardID(upstream, r.shardCount))
	}

	upstreamCondition, err := limitStore.Get(upstream, upstreamStateConditionName(upstream))
	if err != nil {
		return nil, err
	}
	return upstreamCondition, err
}

func (r *rateLimiter) GetRateLimitCondition(upstream, condition string) (*proxyv1alpha1.RateLimitCondition, error) {
	if len(upstream) == 0 {
		upstreams, err := r.upstreamController.UpstreamClusterLister().List(labels.Everything())
		if err != nil {
			return nil, fmt.Errorf("list upstream clusters error: %v", err)
		}
		for _, upstreamCluster := range upstreams {
			if strings.HasPrefix(condition, upstreamCluster.Name) {
				upstream = upstreamCluster.Name
			}
		}
	}

	limitStore := r.getLimitStore(upstream)
	if limitStore == nil {
		return nil, fmt.Errorf("limit store of shard %v not found", util.GetShardID(upstream, r.shardCount))
	}

	rateLimitCondition, err := limitStore.Get(upstream, condition)
	if err != nil {
		return nil, err
	}
	return rateLimitCondition, err
}

func (r *rateLimiter) UpstreamConditionHandler(cluster *proxyv1alpha1.UpstreamCluster) error {
	// TODO skip upstream with no GlobalRateLimiter feature-gates

	shardId := util.GetShardID(cluster.Name, r.shardCount)
	if !r.leaderElector.IsLeader(shardId) {
		return nil
	}

	limitStore := r.getLimitStoreForShard(shardId)
	if limitStore == nil {
		return fmt.Errorf("limit store for upstream %s shard %v not found", cluster.Name, shardId)
	}

	klog.V(1).Infof("Handle upstream cluster %v, shard %v", cluster.Name, shardId)

	_, err := r.upstreamController.UpstreamClusterLister().Get(cluster.Name)
	if errors.IsNotFound(err) {
		// clean cluster
		return limitStore.DeleteUpstream(cluster.Name)
	}
	if err != nil {
		return err
	}

	if _, ok := r.upstreamLock[cluster.Name]; !ok {
		r.upstreamLock[cluster.Name] = &sync.Mutex{}
	}

	upstreamCondition, err := limitStore.Get(cluster.Name, upstreamStateConditionName(cluster.Name))
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	upstreamCondition = updateUpstreamStateCondition(upstreamCondition, cluster)

	err = limitStore.Save(cluster.Name, upstreamCondition)
	if err != nil {
		return err
	}
	limitStore.SyncFlowControl(cluster.Name, cluster.Spec.FlowControl)

	return nil
}

func (r *rateLimiter) Heartbeat(instance string) error {
	r.clientCache.Heartbeat(instance)
	return nil
}

func (r *rateLimiter) GetLeaders() map[int]proxyv1alpha1.EndpointInfo {
	return r.leaderElector.GetLeaders()
}

func (r *rateLimiter) ServerInfo() (*proxyv1alpha1.RateLimitServerInfo, error) {
	serverInfo := proxyv1alpha1.RateLimitServerInfo{
		Server: r.identity,
		ID:     r.runId,
	}
	leaderMap := r.GetLeaders()

	var managedShards []int32
	var endpoints []proxyv1alpha1.EndpointInfo
	for shard, leaderInfo := range leaderMap {
		endpoints = append(endpoints, leaderInfo)
		if leaderInfo.Leader == r.identity {
			managedShards = append(managedShards, int32(shard))
		}
	}

	sort.Slice(managedShards, func(i, j int) bool {
		return managedShards[i] < managedShards[j]
	})
	sort.Slice(endpoints, func(i, j int) bool {
		return endpoints[i].ShardID < endpoints[j].ShardID
	})

	serverInfo.ShardCount = int32(r.shardCount)
	serverInfo.Endpoints = endpoints
	serverInfo.ManagedShards = managedShards

	return &serverInfo, nil
}

func (r *rateLimiter) getLimitStore(upstream string) _interface.LimitStore {
	shardId := util.GetShardID(upstream, r.shardCount)
	return r.getLimitStoreForShard(shardId)
}

func (r *rateLimiter) getLimitStoreForShard(shardId int) _interface.LimitStore {
	r.limitStoreLock.RLock()
	defer r.limitStoreLock.RUnlock()
	return r.limitStoreMap[shardId]
}

func (r *rateLimiter) startLeading(shardId int) {
	r.limitStoreLock.RLock()
	_, ok := r.limitStoreMap[shardId]
	r.limitStoreLock.RUnlock()
	if ok {
		klog.Errorf("Start leading failed, limit store for shard %v already exist", shardId)
		return
	}

	var limitStore _interface.LimitStore
	r.limitStoreLock.Lock()
	_, ok = r.limitStoreMap[shardId]
	if !ok {
		limitStore = store.NewLimitStore(r.gatewayClient, r.limitOptions, shardId, r.shardCount)
		if limitStore == nil {
			klog.Errorf("Start leading shard %v failed, limit store type %v not found", shardId, r.limitOptions.LimitStore)
		} else {
			klog.Infof("Start leading shard %v", shardId)
			r.limitStoreMap[shardId] = limitStore
		}
	}
	r.limitStoreLock.Unlock()

	if limitStore != nil {
		err := func() error {
			err := limitStore.Load()
			if err != nil {
				return err
			}
			return r.syncUpstreamClustersForShard(shardId)
		}()
		if err != nil {
			klog.Errorf("Load limit store for shard %v error: %v", shardId, err)

			r.limitStoreLock.Lock()
			if r.limitStoreMap[shardId] == limitStore {
				delete(r.limitStoreMap, shardId)
			}
			r.limitStoreLock.Unlock()
		}
	}
}

func (r *rateLimiter) syncUpstreamClustersForShard(shardId int) error {
	upstreams, err := r.upstreamController.UpstreamClusterLister().List(labels.Everything())
	if err != nil {
		return fmt.Errorf("list upstream clusters error: %v", err)
	}
	for _, upstream := range upstreams {
		if util.GetShardID(upstream.Name, r.shardCount) == shardId {
			err = r.UpstreamConditionHandler(upstream)
			if err != nil {
				return fmt.Errorf("list upstream clusters error: %v", err)
			}
		}
	}
	return nil
}

func (r *rateLimiter) stopLeading(shardId int) {
	r.limitStoreLock.Lock()
	limitStore, ok := r.limitStoreMap[shardId]
	if !ok {
		klog.Errorf("Load limit store for shard %v not found", shardId)
	}
	delete(r.limitStoreMap, shardId)
	r.limitStoreLock.Unlock()

	if limitStore != nil {
		err := limitStore.Stop()
		if err != nil {
			// TODO retry
			klog.Errorf("Stop and flush limit store for shard %v error: %v", shardId, err)
		}
	}
}

func (r *rateLimiter) sync() {
	r.cleanupTimeoutClient()
	r.leaderCheck()
}

func (r *rateLimiter) cleanupTimeoutClient() {
	clients, err := r.clientCache.AllClients()
	if err != nil {
		klog.Errorf("Get all clients error: %v", err)
		return
	}

	// clean up timeout clients
	for c, lastHeartbeat := range clients {
		if time.Now().After(lastHeartbeat.Add(ClientHeartBeatTimeout)) {
			r.clientCache.Delete(c)
			instance := c
			go func() {
				for _, limitStore := range r.limitStoreMap {
					conditions := limitStore.List(labels.Set{RateLimitConditionInstanceLabel: instance}.AsSelector())
					for _, condition := range conditions {
						r.deleteCondition(limitStore, condition, true)
					}
					r.deleteGlobalFlowControl(limitStore, instance)
				}
			}()
		}
	}
}

func (r *rateLimiter) cleanupUnknownCondition() {
	clients, err := r.clientCache.AllClients()
	if err != nil {
		klog.Errorf("Get all clients error: %v", err)
		return
	}

	expectClients := map[string]bool{}
	for c, _ := range clients {
		expectClients[c] = true
	}

	upstreams, err := r.upstreamController.UpstreamClusterLister().List(labels.Everything())
	if err != nil {
		klog.Errorf("list upstream clusters error: %v", err)
		return
	}
	expectUpstreams := map[string]bool{}
	for _, u := range upstreams {
		expectUpstreams[u.Name] = true
	}

	// clean up when client not found
	clientsToDelete := map[string]bool{}
	upstreamsToDelete := map[string]bool{}
	for _, limitStore := range r.limitStoreMap {
		conditions := limitStore.List(labels.Everything())
		for _, condition := range conditions {
			if expectClients[condition.Spec.Instance] {
				continue
			}
			r.deleteCondition(limitStore, condition, false)
			clientsToDelete[condition.Spec.Instance] = true

			if !expectUpstreams[condition.Spec.UpstreamCluster] {
				upstreamsToDelete[condition.Spec.UpstreamCluster] = true
			}
		}
	}

	for instance, _ := range clientsToDelete {
		for _, limitStore := range r.limitStoreMap {
			r.deleteGlobalFlowControl(limitStore, instance)
		}
	}

	for upstream, _ := range upstreamsToDelete {
		for _, limitStore := range r.limitStoreMap {
			err := limitStore.DeleteUpstream(upstream)
			if err != nil {
				klog.Errorf("Delete upstream %v condition err: %v", upstream, err)
			}
		}
	}
}

func (r *rateLimiter) deleteCondition(limitStore _interface.LimitStore, condition *proxyv1alpha1.RateLimitCondition, logForSkip bool) {
	upstream := condition.Spec.UpstreamCluster
	shardId := util.GetShardID(upstream, r.shardCount)
	if !r.leaderElector.IsLeader(shardId) || len(condition.Spec.Instance) == 0 {
		if logForSkip {
			klog.Errorf("Skip delete condition %s, leader of (upstream %s, shard %v) is %v", condition.Name, upstream, shardId, r.leaderElector.GetLeaders()[shardId])
		}
		return
	}

	klog.Infof("Clean up condition %v for unknown client %v", condition.Name, condition.Spec.Instance)
	err := limitStore.Delete(upstream, condition.Name)
	if err != nil {
		klog.Errorf("Delete error: %v", err)
	}
}

func (r *rateLimiter) deleteGlobalFlowControl(limitStore _interface.LimitStore, instance string) {
	limitStore.DeleteInstanceState(instance)
}

func (r *rateLimiter) leaderCheck() {
	leaders := r.leaderElector.GetLeaders()
	for shard, leader := range leaders {
		leaderState := 0
		if leader.Leader == r.identity {
			r.limitStoreLock.Lock()
			_, ok := r.limitStoreMap[shard]
			r.limitStoreLock.Unlock()
			if !ok {
				r.startLeading(shard)
			}
			leaderState = 1
		}
		metrics.MonitorLeaderElection(shard, leader.Leader, leaderState)
	}

	var leaderToStop []int
	r.limitStoreLock.Lock()
	for shard, _ := range r.limitStoreMap {
		isLeader := false
		for s, leader := range leaders {
			if s == shard && leader.Leader == r.identity {
				isLeader = true
				break
			}
		}
		if !isLeader {
			leaderToStop = append(leaderToStop, shard)
		}
	}
	r.limitStoreLock.Unlock()

	for _, shard := range leaderToStop {
		r.stopLeading(shard)
	}
}

func (r *rateLimiter) calculateUpstreamCondition(limitStore _interface.LimitStore, upstreamCondition *proxyv1alpha1.RateLimitCondition) *proxyv1alpha1.RateLimitCondition {
	conditions := limitStore.ListUpstream(upstreamCondition.Spec.UpstreamCluster)
	upstreamStateName := upstreamStateConditionName(upstreamCondition.Spec.UpstreamCluster)
	upstreamFlowControlMap := util.FlowControlConfigToMap(upstreamCondition.Spec.LimitItemConfigurations)

	upstreamStatusMap := map[string]*proxyv1alpha1.RateLimitItemStatus{}
	requestLevelMap := map[string]float64{}

	for _, condition := range conditions {
		if condition.Name == upstreamStateName {
			continue
		}

		for _, item := range condition.Spec.LimitItemConfigurations {
			total, ok := upstreamStatusMap[item.Name]
			if !ok {
				total = &proxyv1alpha1.RateLimitItemStatus{
					Name:            item.Name,
					LimitItemDetail: proxyv1alpha1.LimitItemDetail{},
				}
				upstreamStatusMap[item.Name] = total
			}

			switch {
			case item.MaxRequestsInflight != nil:
				if total.MaxRequestsInflight == nil {
					total.MaxRequestsInflight = &proxyv1alpha1.MaxRequestsInflightFlowControlSchema{}
				}
				total.MaxRequestsInflight.Max += item.MaxRequestsInflight.Max
			case item.TokenBucket != nil:
				if total.TokenBucket == nil {
					total.TokenBucket = &proxyv1alpha1.TokenBucketFlowControlSchema{}
				}
				total.TokenBucket.QPS += item.TokenBucket.QPS
				total.TokenBucket.Burst += item.TokenBucket.Burst
			}
		}

		for _, status := range condition.Status.LimitItemStatuses {
			level, _ := requestLevelMap[status.Name]

			flowControlConfig := upstreamFlowControlMap[status.Name]
			switch {
			case status.MaxRequestsInflight != nil:
				level += float64(status.MaxRequestsInflight.Max) / float64(flowControlConfig.MaxRequestsInflight.Max)
			case status.TokenBucket != nil:
				level += float64(status.TokenBucket.QPS) / float64(flowControlConfig.TokenBucket.QPS)
			}
			requestLevelMap[status.Name] = level
		}

	}

	var newFlowControlStatus []proxyv1alpha1.RateLimitItemStatus
	for _, status := range upstreamStatusMap {
		status.RequestLevel = int32(requestLevelMap[status.Name] * 100)
		newFlowControlStatus = append(newFlowControlStatus, *status)
	}
	upstreamCondition.Status.LimitItemStatuses = newFlowControlStatus
	return upstreamCondition
}

func updateUpstreamStateCondition(upstreamCondition *proxyv1alpha1.RateLimitCondition, cluster *proxyv1alpha1.UpstreamCluster) *proxyv1alpha1.RateLimitCondition {
	if upstreamCondition == nil {
		upstreamCondition = &proxyv1alpha1.RateLimitCondition{
			ObjectMeta: metav1.ObjectMeta{
				Name: upstreamStateConditionName(cluster.Name),
			},
			Spec: proxyv1alpha1.RateLimitSpec{
				UpstreamCluster: cluster.Name,
			},
			Status: proxyv1alpha1.RateLimitStatus{},
		}
	}
	flowControlStatusToMap := util.FlowControlStatusToMap(upstreamCondition.Status.LimitItemStatuses)

	var newFlowControls []proxyv1alpha1.RateLimitItemConfiguration
	var newFlowControlStatus []proxyv1alpha1.RateLimitItemStatus
	for _, flowControlSchema := range cluster.Spec.FlowControl.Schemas {
		flowControlName := flowControlSchema.Name
		// TODO skip non global flowcontrol
		limitItemDetail := toFlowControlLimit(flowControlSchema)
		newFlowControls = append(newFlowControls, proxyv1alpha1.RateLimitItemConfiguration{
			Name:            flowControlName,
			LimitItemDetail: limitItemDetail,
		})

		flowControlStatus, ok := flowControlStatusToMap[flowControlName]
		if !ok {
			flowControlStatus = proxyv1alpha1.RateLimitItemStatus{
				Name:            flowControlName,
				LimitItemDetail: proxyv1alpha1.LimitItemDetail{},
				RequestLevel:    0,
			}
		}
		if limitItemDetail.TokenBucket != nil && flowControlStatus.TokenBucket == nil {
			flowControlStatus.TokenBucket = &proxyv1alpha1.TokenBucketFlowControlSchema{}
		}
		if limitItemDetail.MaxRequestsInflight != nil && flowControlStatus.MaxRequestsInflight == nil {
			flowControlStatus.MaxRequestsInflight = &proxyv1alpha1.MaxRequestsInflightFlowControlSchema{}
		}

		newFlowControlStatus = append(newFlowControlStatus, flowControlStatus)
	}

	upstreamCondition.Spec.LimitItemConfigurations = newFlowControls
	upstreamCondition.Status.LimitItemStatuses = newFlowControlStatus

	return upstreamCondition
}

func toFlowControlLimit(schema proxyv1alpha1.FlowControlSchema) proxyv1alpha1.LimitItemDetail {
	limit := proxyv1alpha1.LimitItemDetail{}
	switch {
	case schema.GlobalMaxRequestsInflight != nil:
		limit.MaxRequestsInflight = &proxyv1alpha1.MaxRequestsInflightFlowControlSchema{
			Max: schema.GlobalMaxRequestsInflight.Max,
		}
	case schema.GlobalTokenBucket != nil:
		limit.TokenBucket = &proxyv1alpha1.TokenBucketFlowControlSchema{
			QPS:   schema.GlobalTokenBucket.QPS,
			Burst: schema.GlobalTokenBucket.Burst,
		}
	}

	return limit
}

func upstreamStateConditionName(cluster string) string {
	return fmt.Sprintf("%s.state", cluster)
}
