package local

import (
	"sync"

	"github.com/zoumo/goset"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/klog"

	proxyv1alpha1 "github.com/kubewharf/kubegateway/pkg/apis/proxy/v1alpha1"
	flowcontrol2 "github.com/kubewharf/kubegateway/pkg/flowcontrols/flowcontrol"
	"github.com/kubewharf/kubegateway/pkg/ratelimiter/store/flowcontrol"
)

func newUpstreamCondition(upstream string) *upstreamCondition {
	return &upstreamCondition{upstream: upstream}
}

type upstreamCondition struct {
	conditions             sync.Map
	flowControls           sync.Map
	upstream               string
	currentFlowControlSpec proxyv1alpha1.FlowControl
	lock                   sync.Mutex
}

func (c *upstreamCondition) syncLocalFlowControls(flowControls proxyv1alpha1.FlowControl) {
	oldObj := c.currentFlowControlSpec
	if apiequality.Semantic.DeepEqual(oldObj, flowControls) {
		return
	}

	c.lock.Lock()
	defer c.lock.Unlock()

	if apiequality.Semantic.DeepEqual(oldObj, flowControls) {
		return
	}

	defer func() {
		c.currentFlowControlSpec = flowControls
	}()

	oldset := goset.NewSet()
	newset := goset.NewSet()

	for _, schema := range oldObj.Schemas {
		oldset.Add(schema.Name) //nolint
	}

	for _, newSchema := range flowControls.Schemas {
		if newSchema.GlobalTokenBucket == nil && newSchema.GlobalMaxRequestsInflight == nil {
			continue
		}

		newset.Add(newSchema.Name) //nolint
		fc, ok := c.loadFlowControls(newSchema.Name)
		newType := flowcontrol2.GuessFlowControlSchemaType(newSchema)
		if !ok || fc.Type() != newType {
			// flow control is not created or type changed
			fc = flowcontrol.NewGlobalFlowControl(newSchema)
			if fc != nil {
				c.flowControls.Store(newSchema.Name, fc)
			}
		}
		if ok {
			flowcontrol.ResizeGlobalFlowControl(fc, newSchema, c.upstream)
		}
	}

	deleted := oldset.Diff(newset)
	deleted.Range(func(_ int, elem interface{}) bool {
		name := elem.(string)
		klog.Infof("[cluster info] cluster=%q delete flowcontrol schema=%q", c.upstream, name)
		c.flowControls.Delete(name)
		return true
	})
}

func (c *upstreamCondition) loadFlowControls(name string) (flowcontrol.GlobalFlowControl, bool) {
	fc, ok := c.flowControls.Load(name)
	if !ok {
		return nil, ok
	}
	return fc.(flowcontrol.GlobalFlowControl), ok
}

func (c *upstreamCondition) loadCondition(name string) (*proxyv1alpha1.RateLimitCondition, bool) {
	cd, ok := c.conditions.Load(name)
	if !ok {
		return nil, ok
	}
	return cd.(*proxyv1alpha1.RateLimitCondition), ok
}

func (c *upstreamCondition) storeCondition(condition *proxyv1alpha1.RateLimitCondition) {
	c.conditions.Store(condition.Name, condition)
}

func (c *upstreamCondition) deleteCondition(name string) {
	c.conditions.Delete(name)
}
