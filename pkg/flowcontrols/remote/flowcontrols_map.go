package remote

import (
	"sync"
)

type FlowControlMap struct {
	data sync.Map
}

func NewFlowControlsMap() *FlowControlMap {
	return &FlowControlMap{
		data: sync.Map{},
	}
}

func (f *FlowControlMap) Load(name string) (FlowControlCache, bool) {
	fl, ok := f.data.Load(name)
	if !ok {
		return nil, false
	}
	return fl.(FlowControlCache), true
}

func (f *FlowControlMap) Store(name string, fl FlowControlCache) {
	f.data.Store(name, fl)
}

func (f *FlowControlMap) Delete(name string) {
	fl, ok := f.data.Load(name)
	if ok {
		fl.(FlowControlCache).Stop()
	}
	f.data.Delete(name)
}

func (f *FlowControlMap) Len() int {
	length := 0
	f.data.Range(func(key, value interface{}) bool {
		length++
		return true
	})
	return length
}

func (f *FlowControlMap) Snapshot() map[string]FlowControlCache {
	results := map[string]FlowControlCache{}
	f.data.Range(func(key, value interface{}) bool {
		results[key.(string)] = value.(FlowControlCache)
		return true
	})
	return results
}
