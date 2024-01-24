package util

import (
	"sync"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/util/clock"
)

func Test_meter_inflight(t *testing.T) {
	name := "fake-flowcontrol"

	inflightBucketSecond := 0.1
	inflightBucketLen := 6

	type fields struct {
		mockInflightPerBuckets []int32
		debugLog               bool
	}
	tests := []struct {
		name    string
		fields  fields
		wantAvg float64
		wantMax int32
	}{
		{
			name: "base inflight",
			fields: fields{
				mockInflightPerBuckets: []int32{1, 3, 5, 4, 2, 3},
				debugLog:               false,
			},
			wantAvg: 15 * inflightBucketSecond, // sum(1, 3, 5, 4, 2)*0.2
			wantMax: 5,
		},
		{
			name: "inflight round1",
			fields: fields{
				mockInflightPerBuckets: []int32{1, 3, 5, 4, 2, 1, 6, 3},
				debugLog:               false,
			},
			wantAvg: 18 * inflightBucketSecond, // sum(5, 4, 2, 1, 6)*0.2
			wantMax: 6,
		},
		{
			name: "inflight max1",
			fields: fields{
				mockInflightPerBuckets: []int32{1, 3, 5, 4, 2, 3, 8, 4, 3, 6, 2, 4, 3},
				debugLog:               false,
			},
			wantAvg: 19 * inflightBucketSecond,
			wantMax: 6,
		},
		{
			name: "inflight max2",
			fields: fields{
				mockInflightPerBuckets: []int32{1, 3, 5, 4, 2, 3, 6, 4, 3, 8, 2, 4, 3},
				debugLog:               false,
			},
			wantAvg: 21 * inflightBucketSecond,
			wantMax: 8,
		},
		{
			name: "inflight long",
			fields: fields{
				mockInflightPerBuckets: []int32{1, 3, 5, 4, 2, 3, 7, 4, 3, 8, 2, 4, 3, 9, 4, 6, 3, 8, 6, 3, 5, 6, 7, 5, 3, 1, 9, 4},
				debugLog:               false,
			},
			wantAvg: 25 * inflightBucketSecond,
			wantMax: 9,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClock := clock.NewFakeClock(time.Now())
			m := &Meter{
				name:               name,
				stopCh:             make(chan struct{}),
				clock:              fakeClock,
				last:               time.Now(),
				mu:                 sync.Mutex{},
				counterBuckets:     make([]float64, 3),
				rateBucketLen:      3,
				rateBucketDuration: time.Second,

				inflightBuckets:        make([]int32, inflightBucketLen),
				inflightBucketLen:      inflightBucketLen,
				inflightBucketDuration: time.Millisecond * time.Duration(1000*inflightBucketSecond),
				inflightChan:           make(chan int32, 100),

				debug: tt.fields.debugLog,
			}

			for _, mockInflight := range tt.fields.mockInflightPerBuckets {
				for i := int32(1); i <= mockInflight; i++ {
					m.calInflight(i)
				}

				if tt.fields.debugLog {
					t.Logf("clock: %v", m.clock.Now().Format(time.RFC3339Nano))
				}
				fakeClock.Sleep(m.inflightBucketDuration)
			}

			if delta := m.AvgInflight() - tt.wantAvg; delta > 0.001 || delta < -0.001 {
				t.Errorf("avgInflight() = %v, want %v", m.AvgInflight(), tt.wantAvg)
			}
			if got := m.MaxInflight(); got != tt.wantMax {
				t.Errorf("maxInflight() = %v, want %v", got, tt.wantMax)
			}
		})
	}
}
