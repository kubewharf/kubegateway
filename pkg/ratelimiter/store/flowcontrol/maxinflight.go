package flowcontrol

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	proxyv1alpha1 "github.com/kubewharf/kubegateway/pkg/apis/proxy/v1alpha1"
)

// newMaxInflightFlowControl creates a max inflight flow control.
func newMaxInflightFlowControl(name string, typ proxyv1alpha1.FlowControlSchemaType, max int32) GlobalFlowControl {
	return &globalMaxInflight{
		name:           name,
		typ:            typ,
		max:            max,
		instanceStates: map[string]*instanceState{},
	}
}

type globalMaxInflight struct {
	name  string
	typ   proxyv1alpha1.FlowControlSchemaType
	max   int32
	count int32

	lock           sync.RWMutex
	instanceStates map[string]*instanceState
}

type instanceState struct {
	count     int32
	requestId int64
}

func (f *globalMaxInflight) Type() proxyv1alpha1.FlowControlSchemaType {
	return f.typ
}

func (f *globalMaxInflight) String() string {
	return fmt.Sprintf("name=%v,type=%v,size=%v", f.name, f.typ, f.max)
}

func (f *globalMaxInflight) Resize(n int32, burst int32) bool {
	resized := false
	if f.max != n {
		atomic.StoreInt32(&f.max, n)
		resized = true
	}
	return resized
}

func (f *globalMaxInflight) TryAcquireN(instance string, n int32) bool {
	return false
}

func (f *globalMaxInflight) ReleaseN(instance string, n int32) {
}

func (f *globalMaxInflight) add(n int32) int32 {
	count := atomic.AddInt32(&f.count, n)
	max := atomic.LoadInt32(&f.max)
	return count - max
}

func (f *globalMaxInflight) SetState(instance string, requestId int64, current int32) (bool, int32, error) {
	f.lock.RLock()
	state, ok := f.instanceStates[instance]
	f.lock.RUnlock()

	if current < 0 {
		if ok {
			f.lock.Lock()
			delete(f.instanceStates, instance)
			f.add(-state.count)
			f.lock.Unlock()
			current = 0
		}
		return false, -1, nil
	} else if !ok || state == nil {
		f.lock.Lock()
		state, ok = f.instanceStates[instance]
		if !ok || state == nil {
			state = &instanceState{}
			f.instanceStates[instance] = state
		}
		f.lock.Unlock()
	}

	f.lock.RLock()
	defer f.lock.RUnlock()

	if requestId > 0 {
		oldId := atomic.LoadInt64(&state.requestId)
		if requestId <= oldId {
			return false, current, RequestIDTooOld
		}
		atomic.StoreInt64(&state.requestId, requestId)
	}

	old := atomic.SwapInt32(&state.count, current)
	delta := current - old
	overflowed := f.add(delta)

	if overflowed > 0 {
		atomic.AddInt32(&state.count, -delta)
		f.add(-delta)
		return false, old, nil
	}
	if overflowed == 0 && current > 0 {
		return false, current, nil
	}
	return true, current, nil
}

func (f *globalMaxInflight) overflow() int32 {
	max := atomic.LoadInt32(&f.max)
	count := atomic.LoadInt32(&f.count)
	return count - max
}

func (f *globalMaxInflight) DebugInfo() string {
	var msgs []string
	var total int32
	f.lock.RLock()
	for instance, state := range f.instanceStates {
		msgs = append(msgs, fmt.Sprintf("[%s: %v]", instance, state.count))
		total += state.count
	}
	f.lock.RUnlock()
	count := atomic.LoadInt32(&f.count)
	max := atomic.LoadInt32(&f.max)
	info := fmt.Sprintf("name=%s max=%v count=%v total=%v details=%v", f.name, max, count, total, strings.Join(msgs, ","))
	return info
}
