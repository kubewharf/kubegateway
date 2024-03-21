package util

import (
	"math"
	"sync"
	"sync/atomic"
	"time"

	"k8s.io/apimachinery/pkg/util/clock"
	"k8s.io/klog"
)

type Meter struct {
	rateBucketLen      int
	rateBucketDuration time.Duration

	inflightBucketLen      int
	inflightBucketDuration time.Duration

	name string

	stopCh  chan struct{}
	clock   clock.Clock
	mu      sync.Mutex
	started bool

	uncounted      int64
	counter        int64
	currentIndex   int
	rateAvg        float64
	last           time.Time
	counterBuckets []float64

	inflight        int32
	inflightIndex   int
	inflightAvg     float64
	inflightMax     int32
	inflightBuckets []int32
	inflightChan    chan int32

	debug bool
}

func NewMeter(name string,
	rateBucketLen int,
	rateBucketDuration time.Duration,
	inflightBucketLen int,
	inflightBucketDuration time.Duration,
) *Meter {
	stopCh := make(chan struct{})
	m := &Meter{
		name:                   name,
		stopCh:                 stopCh,
		clock:                  clock.RealClock{},
		last:                   time.Now(),
		mu:                     sync.Mutex{},
		counterBuckets:         make([]float64, rateBucketLen),
		rateBucketLen:          rateBucketLen,
		rateBucketDuration:     rateBucketDuration,
		inflightBuckets:        make([]int32, inflightBucketLen),
		inflightBucketLen:      inflightBucketLen,
		inflightBucketDuration: inflightBucketDuration,
		inflightChan:           make(chan int32, 1),
	}
	return m
}

func (m *Meter) Start() {
	if m.started {
		return
	}

	go m.rateTick()
	if m.inflightBucketDuration > 0 {
		go m.inflightWorker()
	}

	m.started = true
}

func (m *Meter) Stop() {
	close(m.stopCh)
}

func (m *Meter) Rate() float64 {
	return m.rateAvg
}

func (m *Meter) AvgInflight() float64 {
	return m.inflightAvg
}

func (m *Meter) MaxInflight() int32 {
	return m.inflightMax
}

func (m *Meter) CurrentInflight() int32 {
	return atomic.LoadInt32(&m.inflight)
}

func (m *Meter) AddN(n int64) {
	m.add(n)
}

func (m *Meter) Count() int64 {
	return atomic.LoadInt64(&m.counter)
}

func (m *Meter) StartOne() {
	m.addInflight(1)
	m.add(1)
}

func (m *Meter) EndOne() {
	m.addInflight(-1)
}

func (m *Meter) addInflight(add int32) {
	inflight := atomic.AddInt32(&m.inflight, add)

	select {
	case m.inflightChan <- inflight:
	default:
	}
}

func (m *Meter) add(add int64) {
	atomic.AddInt64(&m.uncounted, add)
	atomic.AddInt64(&m.counter, add)
}

func (m *Meter) rateTick() {
	ticker := time.NewTicker(m.rateBucketDuration)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			m.calculateAvgRate()
		case <-m.stopCh:
			return
		}
	}
}

func (m *Meter) calculateAvgRate() {
	latestRate := m.latestRate()

	m.mu.Lock()
	lastRate := m.counterBuckets[m.currentIndex]
	if lastRate == math.NaN() {
		lastRate = 0
	}

	rateAvg := m.rateAvg + (latestRate-lastRate)/float64(len(m.counterBuckets))
	m.rateAvg = rateAvg
	m.counterBuckets[m.currentIndex] = latestRate
	m.currentIndex = (m.currentIndex + 1) % len(m.counterBuckets)
	m.mu.Unlock()

	klog.V(6).Infof("FlowControl %s tick: latestRate %v, rateAvg %v, currentIndex %v, counterBuckets %v",
		m.name, latestRate, m.rateAvg, m.currentIndex, m.counterBuckets)
}

func (m *Meter) latestRate() float64 {
	count := atomic.LoadInt64(&m.uncounted)
	atomic.AddInt64(&m.uncounted, -count)
	m.mu.Lock()
	last := m.last
	now := m.clock.Now()
	timeWindow := float64(now.Sub(last)) / float64(time.Second)
	instantRate := float64(count) / timeWindow
	m.last = now
	m.mu.Unlock()

	klog.V(6).Infof("FlowControl %s latestRate: count %v, timeWindow %v,rate %v",
		m.name, count, timeWindow, instantRate)

	return instantRate
}

func (m *Meter) inflightWorker() {

	timerDuration := m.inflightBucketDuration * 2

	timer := m.clock.NewTimer(timerDuration)
	defer timer.Stop()

	for {
		select {
		case inflight := <-m.inflightChan:
			m.calInflight(inflight)
		case <-timer.C():
			m.calInflight(atomic.LoadInt32(&m.inflight))
		case <-m.stopCh:
			return
		}
		timer.Reset(timerDuration)
	}
}

func (m *Meter) calInflight(inflight int32) {
	m.mu.Lock()

	now := m.clock.Now()
	milli := now.UnixMilli()
	currentIndex := int(milli / int64(m.inflightBucketDuration/time.Millisecond) % int64(m.inflightBucketLen))
	lastIndex := m.inflightIndex

	if currentIndex == lastIndex {
		max := m.inflightBuckets[currentIndex]
		if inflight > max {
			m.inflightBuckets[currentIndex] = inflight
		}
	} else {
		fakeIndex := currentIndex
		if currentIndex < lastIndex {
			fakeIndex += m.inflightBucketLen
		}
		inflightDelta := m.inflightBuckets[lastIndex]

		for i := lastIndex + 1; i < fakeIndex; i++ {
			index := i % m.inflightBucketLen
			inflightDelta -= m.inflightBuckets[index]
			m.inflightBuckets[index] = 0
		}
		inflightDelta -= m.inflightBuckets[currentIndex]
		m.inflightBuckets[currentIndex] = inflight
		m.inflightIndex = currentIndex

		m.inflightAvg = m.inflightAvg + float64(inflightDelta)*m.inflightBucketDuration.Seconds()

		max := int32(0)
		for _, ift := range m.inflightBuckets {
			if ift > max {
				max = ift
			}
		}
		m.inflightMax = max

		if m.debug {
			klog.Infof("[debug] [%v] bucket: %v, delta: %v, max: %v, index: %v, avg: %.2f",
				m.clock.Now().Format(time.RFC3339Nano), m.inflightBuckets, inflightDelta, max, currentIndex, m.inflightAvg)
		}
	}

	m.mu.Unlock()
}
