package flowcontrol

import (
	"context"
	"github.com/kubewharf/kubegateway/pkg/apis/proxy/v1alpha1"
	"golang.org/x/time/rate"
	"testing"
)

func Test_globalTokenBucket_TryAcquireN(t *testing.T) {
	type fields struct {
		limitQPS   int
		clientQPS  int
		timeWindow int
	}

	tests := []struct {
		name   string
		fields fields
		want   bool
	}{
		// TODO: Add test cases.
		{
			name: "test1",
			fields: fields{
				limitQPS:   200,
				clientQPS:  200,
				timeWindow: 5,
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newTokenBucketFlowControl(tt.name, v1alpha1.TokenBucket, int32(tt.fields.limitQPS), int32(tt.fields.limitQPS))
			limiter := rate.NewLimiter(rate.Limit(tt.fields.clientQPS), tt.fields.clientQPS)

			timeWindow := tt.fields.timeWindow
			count := 0
			for i := 0; i < tt.fields.clientQPS*timeWindow; i++ {
				limiter.Wait(context.TODO())
				acquire := f.TryAcquireN("test", 1)
				if acquire {
					count++
				}
			}
			expect := timeWindow * tt.fields.limitQPS
			if count > expect {
				t.Errorf("acepted count %v, expect: %v", count, expect)
			} else {
				t.Logf("acepted count %v, expect: %v", count, expect)
			}
		})
	}
}
