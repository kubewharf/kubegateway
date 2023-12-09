package k8s

import (
	"context"
	"fmt"
	"github.com/kubewharf/kubegateway/pkg/ratelimiter/store/flowcontrol"
	"github.com/kubewharf/kubegateway/pkg/ratelimiter/util"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog"

	proxyv1alpha1 "github.com/kubewharf/kubegateway/pkg/apis/proxy/v1alpha1"
	gatewayclientset "github.com/kubewharf/kubegateway/pkg/client/kubernetes"
	"github.com/kubewharf/kubegateway/pkg/ratelimiter/store/interface"
	"github.com/kubewharf/kubegateway/pkg/ratelimiter/store/local"
)

func NewK8sCacheStore(gatewayClient gatewayclientset.Interface, syncPeriod time.Duration, shard, shardCount int) _interface.LimitStore {
	localStore := local.NewLocalStore()

	store := &objectStore{
		shard:         shard,
		shardCount:    shardCount,
		stopCh:        make(chan struct{}),
		syncPeriod:    syncPeriod,
		localStore:    localStore,
		gatewayClient: gatewayClient,
	}

	if syncPeriod > 0 {
		go wait.Until(store.sync, syncPeriod, store.stopCh)
	}
	return store
}

type objectStore struct {
	sync.Mutex
	shard         int
	shardCount    int
	stopCh        chan struct{}
	syncPeriod    time.Duration
	localStore    _interface.LimitStore
	gatewayClient gatewayclientset.Interface
}

func (s *objectStore) Get(cluster, name string) (*proxyv1alpha1.RateLimitCondition, error) {
	return s.localStore.Get(cluster, name)
}

func (s *objectStore) Save(cluster string, condition *proxyv1alpha1.RateLimitCondition) error {
	shard := util.GetShardID(condition.Spec.UpstreamCluster, s.shardCount)
	if shard != s.shard {
		return fmt.Errorf("condition %s should be managed by shard %v, not %v", condition.Name, shard, s.shard)
	}

	klog.V(5).Infof("Save upstream %s condition %s to store %v", cluster, condition.Name, s.shard)
	if s.syncPeriod == 0 {
		var err error
		condition, err = s.createOrUpdate(condition)
		if err != nil {
			return err
		}
	}
	return s.localStore.Save(cluster, condition)
}

func (s *objectStore) Delete(cluster, name string) error {
	err := retry.RetryOnConflict(retry.DefaultRetry, func() (err error) {
		err = s.gatewayClient.ProxyV1alpha1().RateLimitConditions().Delete(context.Background(), name, v1.DeleteOptions{})
		if err == nil || errors.IsNotFound(err) {
			return nil
		}
		return err
	})
	if err != nil {
		return err
	}
	return s.localStore.Delete(cluster, name)
}

func (s *objectStore) DeleteUpstream(cluster string) error {
	itemsToDelete := s.localStore.ListUpstream(cluster)
	for _, item := range itemsToDelete {
		err := retry.RetryOnConflict(retry.DefaultRetry, func() (err error) {
			err = s.gatewayClient.ProxyV1alpha1().RateLimitConditions().Delete(context.Background(), item.Name, v1.DeleteOptions{})
			if err == nil || errors.IsNotFound(err) {
				return nil
			}
			return err
		})
		if err != nil {
			return err
		}
	}
	return s.localStore.DeleteUpstream(cluster)
}

func (s *objectStore) ListUpstream(cluster string) []*proxyv1alpha1.RateLimitCondition {
	return s.localStore.ListUpstream(cluster)
}

func (s *objectStore) List(selector labels.Selector) []*proxyv1alpha1.RateLimitCondition {
	return s.localStore.List(selector)
}

func (s *objectStore) Load() error {
	items, err := s.gatewayClient.ProxyV1alpha1().RateLimitConditions().List(context.Background(), v1.ListOptions{
		ResourceVersion: "0",
	})
	if err != nil {
		return err
	}

	for _, item := range items.Items {
		newItem := item.DeepCopy()
		if util.GetShardID(newItem.Spec.UpstreamCluster, s.shardCount) != s.shard {
			continue
		}

		klog.V(2).Infof("Load condition %v for shard %v", newItem.Name, s.shard)
		_ = s.localStore.Save(item.Spec.UpstreamCluster, newItem)
	}
	return nil
}

func (s *objectStore) GetFlowControl(cluster, name string) (flowcontrol.GlobalFlowControl, error) {
	return s.localStore.GetFlowControl(cluster, name)
}

func (s *objectStore) SyncFlowControl(cluster string, fc proxyv1alpha1.FlowControl) {
	s.localStore.SyncFlowControl(cluster, fc)
}

func (s *objectStore) DeleteInstanceState(instance string) {
	s.localStore.DeleteInstanceState(instance)
}

func (s *objectStore) Flush() error {
	return s.doSyncLocked()
}

func (s *objectStore) Stop() error {
	err := s.doSyncLocked()
	if err != nil {
		return err
	}
	close(s.stopCh)
	return s.localStore.Stop()
}

func (s *objectStore) createOrUpdate(condition *proxyv1alpha1.RateLimitCondition) (*proxyv1alpha1.RateLimitCondition, error) {
	item := condition.DeepCopy()
	item.ResourceVersion = ""

	err := wait.ExponentialBackoff(retry.DefaultRetry, func() (done bool, err error) {
		_, err = s.gatewayClient.ProxyV1alpha1().RateLimitConditions().Update(context.Background(), item, v1.UpdateOptions{})
		switch {
		case errors.IsNotFound(err):
			item, err = s.gatewayClient.ProxyV1alpha1().RateLimitConditions().Create(context.Background(), item, v1.CreateOptions{})
			return err == nil, nil
		case errors.IsConflict(err):
			latest, err := s.gatewayClient.ProxyV1alpha1().RateLimitConditions().Get(context.Background(), condition.Name, v1.GetOptions{ResourceVersion: "0"})
			if err == nil {
				item = latest
				item.Spec = condition.Spec
				item.Status = condition.Status
			}
			return false, nil
		}

		return true, nil
	})

	return item, err
}

func (s *objectStore) sync() {
	err := s.doSyncLocked()
	if err != nil {
		klog.Errorf("Sync store error: %v", err)
	}
}

func (s *objectStore) doSyncLocked() error {
	klog.V(2).Infof("Sync local data to k8s for shard %v", s.shard)

	s.Lock()
	defer s.Unlock()
	items := s.localStore.List(labels.Everything())
	for _, item := range items {
		shard := util.GetShardID(item.Spec.UpstreamCluster, s.shardCount)
		if shard != s.shard {
			continue
		}

		_, err := s.createOrUpdate(item)
		if err != nil {
			return fmt.Errorf("sync %v error: %v", item.Name, err)
		}
	}
	return nil
}
