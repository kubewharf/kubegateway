package util

import (
	"sync"
	"sync/atomic"
	"time"
)

type RateMeter struct {
	lastCount int64
	lastTime  time.Time
	count     int64
	lock      sync.Mutex
}

func (r *RateMeter) Record() {
	r.RecordN(1)
}

func (r *RateMeter) RecordN(n int64) {
	atomic.AddInt64(&r.count, n)
}

func (r *RateMeter) Delta() int64 {
	r.lock.Lock()
	count := atomic.LoadInt64(&r.count)
	lastCount := atomic.LoadInt64(&r.lastCount)
	now := time.Now()
	//lastTime := r.lastTime

	r.lastCount = count
	r.lastTime = now
	r.lock.Unlock()

	delta := count - lastCount
	//qps = float64(delta) / now.Sub(lastTime).Seconds()
	return delta
}

func (r *RateMeter) Count() int64 {
	return atomic.LoadInt64(&r.count)
}
