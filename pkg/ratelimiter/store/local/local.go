package local

import (
	"github.com/kubewharf/kubegateway/pkg/ratelimiter/store/flowcontrol"
	"sync"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"

	proxyv1alpha1 "github.com/kubewharf/kubegateway/pkg/apis/proxy/v1alpha1"
	_interface "github.com/kubewharf/kubegateway/pkg/ratelimiter/store/interface"
)

func NewLocalStore() _interface.LimitStore {
	return &localStore{}
}

type localStore struct {
	clusters sync.Map
}

func (s *localStore) Get(cluster, name string) (*proxyv1alpha1.RateLimitCondition, error) {
	clusterStore, ok := s.clusters.Load(cluster)
	if !ok {
		return nil, errors.NewNotFound(proxyv1alpha1.Resource("ratelimitcondition"), name)
	}
	condition, ok := clusterStore.(*upstreamCondition).loadCondition(name)
	if !ok {
		return nil, errors.NewNotFound(proxyv1alpha1.Resource("ratelimitcondition"), name)
	}

	return condition, nil
}

func (s *localStore) Add(cluster string, condition *proxyv1alpha1.RateLimitCondition) error {
	clusterStore, ok := s.clusters.Load(cluster)
	if !ok {
		clusterStore = newUpstreamCondition(cluster)
		s.clusters.Store(cluster, clusterStore)
	}

	cd := clusterStore.(*upstreamCondition)
	if _, ok := cd.loadCondition(condition.Name); ok {
		return errors.NewAlreadyExists(proxyv1alpha1.Resource("ratelimitcondition"), condition.Name)
	}

	cd.conditions.Store(condition.Name, condition)
	return nil
}

func (s *localStore) Save(cluster string, condition *proxyv1alpha1.RateLimitCondition) error {
	clusterStore, ok := s.clusters.Load(cluster)
	if !ok {
		clusterStore = newUpstreamCondition(cluster)
		s.clusters.Store(cluster, clusterStore)
	}

	clusterStore.(*upstreamCondition).storeCondition(condition)
	return nil
}

func (s *localStore) Delete(cluster, name string) error {
	clusterStore, ok := s.clusters.Load(cluster)
	if !ok {
		return nil
	}

	clusterStore.(*upstreamCondition).deleteCondition(name)
	return nil
}

func (s *localStore) DeleteUpstream(cluster string) error {
	s.clusters.Delete(cluster)
	return nil
}

func (s *localStore) ListUpstream(cluster string) []*proxyv1alpha1.RateLimitCondition {
	clusterStore, ok := s.clusters.Load(cluster)
	if !ok {
		return nil
	}

	var results []*proxyv1alpha1.RateLimitCondition
	clusterStore.(*upstreamCondition).conditions.Range(func(key, value interface{}) bool {
		results = append(results, value.(*proxyv1alpha1.RateLimitCondition))
		return true
	})
	return results
}

func (s *localStore) List(selector labels.Selector) []*proxyv1alpha1.RateLimitCondition {
	var results []*proxyv1alpha1.RateLimitCondition
	s.clusters.Range(func(key, value interface{}) bool {
		value.(*upstreamCondition).conditions.Range(func(key, value interface{}) bool {
			item := value.(*proxyv1alpha1.RateLimitCondition)
			if selector.Matches(labels.Set(item.Labels)) {
				results = append(results, item)
			}
			return true
		})
		return true
	})
	return results
}

func (s *localStore) GetFlowControl(cluster, name string) (flowcontrol.GlobalFlowControl, error) {
	clusterStore, ok := s.clusters.Load(cluster)
	if !ok {
		return nil, errors.NewNotFound(proxyv1alpha1.Resource("flowControl"), name)
	}
	fc, ok := clusterStore.(*upstreamCondition).loadFlowControls(name)
	if !ok {
		return nil, errors.NewNotFound(proxyv1alpha1.Resource("flowControl"), name)
	}

	return fc, nil
}

func (s *localStore) SyncFlowControl(cluster string, fc proxyv1alpha1.FlowControl) {
	clusterStore, ok := s.clusters.Load(cluster)
	if !ok {
		clusterStore = newUpstreamCondition(cluster)
		s.clusters.Store(cluster, clusterStore)
	}

	clusterStore.(*upstreamCondition).syncLocalFlowControls(fc)
}

func (s *localStore) DeleteInstanceState(instance string) {
	s.clusters.Range(func(key, value interface{}) bool {
		value.(*upstreamCondition).flowControls.Range(func(key, value interface{}) bool {
			fc := value.(flowcontrol.GlobalFlowControl)
			fc.SetState(instance, -1, -1)
			return true
		})
		return true
	})
}

func (s *localStore) Load() error {
	return nil
}

func (s *localStore) Flush() error {
	return nil
}

func (s *localStore) Stop() error {
	return nil
}
