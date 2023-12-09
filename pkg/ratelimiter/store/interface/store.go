package _interface

import (
	"github.com/kubewharf/kubegateway/pkg/apis/proxy/v1alpha1"
	"github.com/kubewharf/kubegateway/pkg/ratelimiter/store/flowcontrol"
	"k8s.io/apimachinery/pkg/labels"
)

type LimitStore interface {
	Get(cluster, name string) (*v1alpha1.RateLimitCondition, error)
	Save(cluster string, condition *v1alpha1.RateLimitCondition) error
	Delete(cluster, name string) error
	DeleteUpstream(cluster string) error
	ListUpstream(cluster string) []*v1alpha1.RateLimitCondition
	List(selector labels.Selector) []*v1alpha1.RateLimitCondition
	GetFlowControl(cluster, name string) (flowcontrol.GlobalFlowControl, error)
	SyncFlowControl(cluster string, fc v1alpha1.FlowControl)
	DeleteInstanceState(instance string)
	Load() error
	Flush() error
	Stop() error
}

type ClusterCondition interface {
	Get(name string) (*v1alpha1.RateLimitCondition, error)
	Save(condition *v1alpha1.RateLimitCondition) error
	Delete(string) error
	List(selector labels.Selector) []*v1alpha1.RateLimitCondition
	GetFlowControl(name string) flowcontrol.GlobalFlowControl
	SyncFlowControl(v1alpha1.FlowControl)
}
