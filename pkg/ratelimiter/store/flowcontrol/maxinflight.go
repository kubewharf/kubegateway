package flowcontrol

import (
	"fmt"
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

func (f *globalMaxInflight) SetState(instance string, requestId int64, current int32) (int32, error) {
	f.lock.RLock()
	state, ok := f.instanceStates[instance]
	f.lock.RUnlock()

	if current < 0 {
		if ok {
			f.lock.Lock()
			delete(f.instanceStates, instance)
			f.lock.Unlock()
			current = 0
		}
		return -1, nil
	} else if !ok || state == nil {
		f.lock.Lock()
		state = &instanceState{}
		f.instanceStates[instance] = state
		f.lock.Unlock()
	}

	if requestId > 0 {
		f.lock.RLock()
		oldId := atomic.LoadInt64(&state.requestId)
		if requestId <= oldId {
			f.lock.RUnlock()
			return -1, RequestIDTooOld
		}
		atomic.StoreInt64(&state.requestId, requestId)
		f.lock.RUnlock()
	}

	old := atomic.SwapInt32(&state.count, current)
	delta := current - old
	overflowed := f.add(delta)

	if overflowed > 0 {
		atomic.AddInt32(&state.count, -delta)
		f.add(-delta)
		return old, nil
	}
	if overflowed == 0 && current > 0 {
		return current, nil
	}
	return -1, nil
}

func (f *globalMaxInflight) overflow() int32 {
	max := atomic.LoadInt32(&f.max)
	count := atomic.LoadInt32(&f.count)
	return count - max
}
