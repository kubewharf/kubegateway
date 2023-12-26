package remote

import (
	"context"
	"math"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog"

	proxyv1alpha1 "github.com/kubewharf/kubegateway/pkg/apis/proxy/v1alpha1"
	"github.com/kubewharf/kubegateway/pkg/flowcontrols/flowcontrol"
	"github.com/kubewharf/kubegateway/pkg/gateway/metrics"
	"github.com/kubewharf/kubegateway/pkg/ratelimiter/clientsets"
	"github.com/kubewharf/kubegateway/pkg/ratelimiter/util"
)

const (
	LimiterReconcilePeriod = time.Second * 2
	waitReadyPeriod        = time.Millisecond * 500
)

var (
	requestReasonSuccess                  = "success"
	requestReasonNoReadyClients           = "no_ready_clients"
	requestReasonRateLimiterError         = "rate_limiter_error"
	requestReasonTimeout                  = "timeout"
	requestReasonSkipped                  = "skipped"
	requestReasonFlowControlNotFound      = "flowcontrol_not_found"
	requestReasonGlobalFlowControlDisable = "global_flowcontrol_disable"
)

type Reconcile interface {
	EnsureReconcile(rateLimiter string)
}

func NewReconcile(ctx context.Context, cluster string, clientSets clientsets.ClientSets, flowControls *FlowControlMap) Reconcile {
	var clientID string
	if clientSets != nil {
		clientID = clientSets.ClientID()
	}

	return &reconcile{
		ctx:          ctx,
		cluster:      cluster,
		clientSets:   clientSets,
		flowControls: flowControls,
		clientID:     clientID,
	}
}

type reconcile struct {
	ctx          context.Context
	cancel       context.CancelFunc
	lock         sync.Mutex
	cluster      string
	flowControls *FlowControlMap
	clientSets   clientsets.ClientSets
	clientID     string
}

func (r *reconcile) EnsureReconcile(rateLimiter string) {
	if rateLimiter == flowcontrol.RemoteFlowControls && r.cancel == nil {
		if r.clientSets == nil {
			klog.Errorf("Failed to start remote limiter reconcile because client set is nil")
			return
		}
		r.lock.Lock()
		if r.cancel == nil {
			newCtx, cancel := context.WithCancel(r.ctx)
			r.cancel = cancel
			go func() {
				r.waitForReady(newCtx.Done())
				wait.Until(r.reconcile, LimiterReconcilePeriod, newCtx.Done())
				klog.Infof("Stop reconcile cluster %s from limit server", r.cluster)
			}()
			klog.Infof("Start reconcile cluster %s from limit server", r.cluster)
		}
		r.lock.Unlock()
	}

	if rateLimiter == flowcontrol.LocalFlowControls && r.cancel != nil {
		r.lock.Lock()
		if r.cancel != nil {
			cancel := r.cancel
			r.cancel = nil
			defer cancel()
			klog.Infof("Stop reconcile cluster %s from limit server", r.cluster)
		}
		r.lock.Unlock()

	}

}

func (r *reconcile) waitForReady(stopCh <-chan struct{}) bool {
	err := wait.PollImmediateUntil(waitReadyPeriod,
		func() (bool, error) {
			if !r.clientSets.IsReady(r.cluster) {
				return false, nil
			}
			klog.V(2).Infof("ClientSet for cluster %v is ready", r.cluster)
			shard, err := r.clientSets.ShardIDFor(r.cluster)
			if err != nil {
				klog.Errorf("Get shard for cluster %v failed: %v", r.cluster, err)
				return false, nil
			}
			klog.Infof("Cluster %v chose server shard %v", r.cluster, shard)
			return true, nil
		},
		stopCh)
	if err != nil {
		klog.Errorf("stop wait clientSets ready: %v", err)
		return false
	}
	return true
}

func (r *reconcile) reconcile() {
	r.updateGlobalCuntFlowControls()

	start := time.Now()
	result := "unknown"
	defer func() {
		metrics.RecordRateLimiterRequest(r.cluster, "allocate", result, "all", time.Since(start))
	}()

	client, err := r.clientSets.ClientFor(r.cluster)
	if err != nil {
		klog.Errorf("Get limiter server client for cluster %s error: %v", r.cluster, err)
		result = requestReasonNoReadyClients
		return
	}

	condition := r.buildLimitConditions()

	ret, err := client.ProxyV1alpha1().RateLimitConditions().UpdateStatus(context.Background(), condition, metav1.UpdateOptions{})
	if err != nil {
		klog.Errorf("Update limit condition %s error: %v", condition.Name, err)
		result = requestReasonRateLimiterError
		return
	}
	result = requestReasonSuccess

	klog.V(3).Infof("Condition %s reconcile, status: %+v, spec: %+v", condition.Name, condition.Status.LimitItemStatuses, ret.Spec.LimitItemConfigurations)

	r.updateFlowControls(ret)
}

func (r *reconcile) updateGlobalCuntFlowControls() {
	flowControls := r.flowControls.Snapshot()
	for name, fcCache := range flowControls {
		localConfig := fcCache.LocalFlowControl().Config()
		if localConfig.Strategy != proxyv1alpha1.GlobalCountLimit {
			continue
		}

		itemConfig := proxyv1alpha1.RateLimitItemConfiguration{
			Name:     name,
			Strategy: fcCache.Strategy(),
			LimitItemDetail: proxyv1alpha1.LimitItemDetail{
				MaxRequestsInflight: localConfig.GlobalMaxRequestsInflight,
				TokenBucket:         localConfig.GlobalTokenBucket,
			},
		}
		if fcCache.FlowControl() == nil {
			fcCache.EnableRemoteFlowControl()
		}
		remoteFlowControl := fcCache.FlowControl()
		remoteFlowControl.Sync(itemConfig)
	}
}

func (r *reconcile) buildLimitConditions() *proxyv1alpha1.RateLimitCondition {
	instance := r.clientSets.ClientID()
	conditionName := util.GenerateRateLimitConditionName(r.cluster, instance)

	var newFlowControlConfigs []proxyv1alpha1.RateLimitItemConfiguration
	var newLimitItemStatues []proxyv1alpha1.RateLimitItemStatus

	flowControls := r.flowControls.Snapshot()
	for name, flowControlCache := range flowControls {
		localConfig := flowControlCache.LocalFlowControl().Config()
		if localConfig.Strategy != proxyv1alpha1.GlobalAllocateLimit {
			continue
		}
		if !EnableGlobalFlowControl(localConfig) {
			continue
		}

		itemConfig := getRateLimitItemConfiguration(name, flowControlCache)
		newFlowControlConfigs = append(newFlowControlConfigs, itemConfig)

		status := getRateLimitItemStatus(name, flowControlCache)
		newLimitItemStatues = append(newLimitItemStatues, status)
	}

	condition := &proxyv1alpha1.RateLimitCondition{
		ObjectMeta: metav1.ObjectMeta{
			Name: conditionName,
		},
		Spec: proxyv1alpha1.RateLimitSpec{
			UpstreamCluster:         r.cluster,
			Instance:                instance,
			LimitItemConfigurations: newFlowControlConfigs,
		},
		Status: proxyv1alpha1.RateLimitStatus{
			LimitItemStatuses: newLimitItemStatues,
		},
	}
	return condition
}

func (r *reconcile) updateFlowControls(condition *proxyv1alpha1.RateLimitCondition) {
	for _, config := range condition.Spec.LimitItemConfigurations {
		result := func() string {
			fcCache, ok := r.flowControls.Load(config.Name)
			if !ok {
				klog.Errorf("flow control %s not found in local cache, retry next sync", config.Name)
				return requestReasonFlowControlNotFound
			}

			localConfig := fcCache.LocalFlowControl().Config()
			if !EnableGlobalFlowControl(localConfig) {
				return requestReasonGlobalFlowControlDisable
			}

			if fcCache.FlowControl() == nil {
				fcCache.EnableRemoteFlowControl()
			}
			remoteFlowControl := fcCache.FlowControl()
			remoteFlowControl.Sync(config)
			return requestReasonSuccess
		}()
		metrics.RecordRateLimiterRequest(r.cluster, "allocate", result, config.Name, -1)
	}
}

func getRateLimitItemConfiguration(name string, flowControlCache FlowControlCache) proxyv1alpha1.RateLimitItemConfiguration {
	itemConfig := proxyv1alpha1.RateLimitItemConfiguration{
		Name:     name,
		Strategy: flowControlCache.Strategy(),
	}

	rmfc := flowControlCache.FlowControl()

	if rmfc == nil {
		itemConfig.LimitItemDetail = proxyv1alpha1.LimitItemDetail{}
	} else {
		itemConfig.LimitItemDetail = proxyv1alpha1.LimitItemDetail{
			MaxRequestsInflight: rmfc.Config().MaxRequestsInflight,
			TokenBucket:         rmfc.Config().TokenBucket,
		}
	}
	return itemConfig
}

func getRateLimitItemStatus(name string, flowControlCache FlowControlCache) proxyv1alpha1.RateLimitItemStatus {
	status := proxyv1alpha1.RateLimitItemStatus{
		Name:            name,
		LimitItemDetail: proxyv1alpha1.LimitItemDetail{},
		RequestLevel:    0,
	}

	flowControlSchemaType := flowControlCache.LocalFlowControl().Type()
	if rmfc := flowControlCache.FlowControl(); rmfc != nil {
		flowControlSchemaType = rmfc.Type()
	}

	switch flowControlSchemaType {
	case proxyv1alpha1.MaxRequestsInflight:
		inflight := flowControlCache.Inflight()
		status.LimitItemDetail.MaxRequestsInflight = &proxyv1alpha1.MaxRequestsInflightFlowControlSchema{
			Max: inflight,
		}
		if remoteFlowControl := flowControlCache.FlowControl(); remoteFlowControl != nil {
			status.RequestLevel = int32(float64(inflight) / float64(flowControlCache.FlowControl().Config().MaxRequestsInflight.Max) * 100)
		}
	case proxyv1alpha1.TokenBucket:
		rate := flowControlCache.Rate()
		status.LimitItemDetail.TokenBucket = &proxyv1alpha1.TokenBucketFlowControlSchema{
			QPS:   int32(math.Round(rate)),
			Burst: int32(math.Round(rate)),
		}
		// TODO
		if remoteFlowControl := flowControlCache.FlowControl(); remoteFlowControl != nil {
			status.RequestLevel = int32(100 * rate / float64(flowControlCache.FlowControl().Config().TokenBucket.QPS))
		}
	}

	return status
}
