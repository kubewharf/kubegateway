package flowcontrols

import (
	"context"
	"fmt"
	"sync"

	"github.com/zoumo/goset"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/klog"
	"sync/atomic"

	proxyv1alpha1 "github.com/kubewharf/kubegateway/pkg/apis/proxy/v1alpha1"
	"github.com/kubewharf/kubegateway/pkg/flowcontrols/flowcontrol"
	"github.com/kubewharf/kubegateway/pkg/flowcontrols/remote"
	"github.com/kubewharf/kubegateway/pkg/ratelimiter/clientsets"
)

func NewUpstreamLimiter(ctx context.Context, cluster, rateLimiter string, clientSets clientsets.ClientSets) UpstreamLimiter {
	if len(rateLimiter) == 0 {
		rateLimiter = flowcontrol.LocalFlowControls
	}

	var clientID string
	if clientSets != nil {
		clientID = clientSets.ClientID()
	}

	ctx, cancel := context.WithCancel(ctx)

	flowControls := remote.NewFlowControlsMap()

	g := remote.NewGlobalCounterProvider(ctx, cluster, clientSets, clientID)
	r := remote.NewReconcile(ctx, cluster, clientSets, flowControls)

	m := &upstreamLimiter{
		ctx:                   ctx,
		cancel:                cancel,
		cluster:               cluster,
		rateLimiter:           rateLimiter,
		clientSets:            clientSets,
		globalCounterProvider: g,
		defaultFlowControl:    flowcontrol.DefaultFlowControl,
		flowControls:          flowControls,
		reconcile:             r,
		switchToLocalReason:   map[string]string{},
		clientID:              clientID,
	}

	return m
}

type UpstreamLimiter interface {
	GetOrDefault(name string) flowcontrol.FlowControl
	Load(name string) (flowcontrol.FlowControl, bool)
	Sync(newObj proxyv1alpha1.FlowControl)
	ResetLimiter(rateLimiter string)
	AllFlowControls() map[string]remote.FlowControlCache
}

type upstreamLimiter struct {
	ctx    context.Context
	cancel context.CancelFunc

	// server cluster
	cluster     string
	rateLimiter string

	clientSets clientsets.ClientSets

	defaultFlowControl flowcontrol.FlowControl

	// current synced flow controler spec
	currentFlowControlSpec atomic.Value

	flowControls *remote.FlowControlMap

	reconcile             remote.Reconcile
	globalCounterProvider remote.GlobalCounterProvider

	switchToLocalReason map[string]string
	reasonLock          sync.Mutex

	clientID string
}

func (f *upstreamLimiter) ResetLimiter(rateLimiter string) {
	if rateLimiter != f.rateLimiter {
		f.rateLimiter = rateLimiter
		f.reconcile.EnsureReconcile(rateLimiter)
	}
}

func (f *upstreamLimiter) GetOrDefault(name string) flowcontrol.FlowControl {
	if len(name) == 0 {
		return f.defaultFlowControl
	}
	load, ok := f.Load(name)
	if !ok {
		return f.defaultFlowControl
	}
	return load
}

func (f *upstreamLimiter) Load(name string) (flowcontrol.FlowControl, bool) {
	fcw, ok := f.flowControls.Load(name)
	if !ok {
		return nil, ok
	}

	reason := ""
	switch f.rateLimiter {
	case flowcontrol.RemoteFlowControls:
		switch {
		case len(fcw.Strategy()) == 0:
			reason = fmt.Sprintf("strategy is empty")
		case fcw.Strategy() == proxyv1alpha1.LocalLimit:
			reason = fmt.Sprintf("strategy is %s", fcw.Strategy())
		case f.clientSets == nil:
			reason = "clientSets is nil"
		case !f.clientSets.IsReady(f.cluster):
			reason = "clientSets is not ready"
		default:
			fc := fcw.FlowControl()
			if fc != nil {
				return fc, true
			}
			reason = "remote flowcontrol is not synced"
		}
	case flowcontrol.LocalFlowControls:
		return fcw.LocalFlowControl(), true
	default:
		reason = fmt.Sprintf("unkonwn rateLimiter type %s", f.rateLimiter)
	}

	if len(reason) > 0 {
		f.reasonLock.Lock()
		if f.switchToLocalReason[name] != reason {
			klog.Errorf("Use local limiter for flow control %v: %q, old reason: %q, remote fc: [%v]", name, reason, f.switchToLocalReason[name], fcw.FlowControl())
			f.switchToLocalReason[name] = reason
		}
		f.reasonLock.Unlock()
	}

	// use local limiter by default
	return fcw.LocalFlowControl(), true
}
func (f *upstreamLimiter) Sync(flowControls proxyv1alpha1.FlowControl) {
	f.syncLocalFlowControls(flowControls)
}

func (f *upstreamLimiter) syncLocalFlowControls(flowControls proxyv1alpha1.FlowControl) {
	oldObj, _ := f.loadFlowControlSpec()
	if apiequality.Semantic.DeepEqual(oldObj, flowControls) {
		return
	}

	defer func() {
		f.currentFlowControlSpec.Store(&flowControls)
	}()

	oldset := goset.NewSet()
	newset := goset.NewSet()

	for _, schema := range oldObj.Schemas {
		oldset.Add(schema.Name) //nolint
	}

	for _, newSchema := range flowControls.Schemas {
		newset.Add(newSchema.Name) //nolint
		fc, ok := f.flowControls.Load(newSchema.Name)
		if !ok {
			// flow control is not created or type changed
			fc = remote.NewFlowControlCache(f.cluster, newSchema.Name, f.clientID, f.globalCounterProvider)
			f.flowControls.Store(newSchema.Name, fc)
		}
		fc.LocalFlowControl().Sync(newSchema)
	}

	deleted := oldset.Diff(newset)

	deleted.Range(func(_ int, elem interface{}) bool {
		name := elem.(string)
		klog.Infof("[cluster info] cluster=%q delete flowcontrol schema=%q", f.cluster, name)
		f.flowControls.Delete(name)
		return true
	})
}

func (f *upstreamLimiter) loadFlowControlSpec() (proxyv1alpha1.FlowControl, bool) {
	empty := proxyv1alpha1.FlowControl{}
	uncastObj := f.currentFlowControlSpec.Load()
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

func (f *upstreamLimiter) AllFlowControls() map[string]remote.FlowControlCache {
	return f.flowControls.Snapshot()
}
