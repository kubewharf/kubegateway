/*
Copyright 2024 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package monitor

import (
	"time"

	"github.com/kubewharf/kubegateway/pkg/flowcontrols/util"
)

const (
	rateMeterBucketLen    = 3
	rateMeterTickDuration = time.Second * 10
)

type RateMonitor struct {
	rate *util.Meter
}

func NewRateMonitor() *RateMonitor {
	m := &RateMonitor{
		rate: util.NewMeter("request-rate", rateMeterBucketLen, rateMeterTickDuration, 0, 0),
	}
	m.rate.Start()
	return m
}

func (m *RateMonitor) RequestAdd(n int64) {
	m.rate.AddN(n)
}

func (m *RateMonitor) RequestStart() {
	m.rate.StartOne()
}

func (m *RateMonitor) RequestEnd() {
	m.rate.EndOne()
}

func (m *RateMonitor) Rate() float64 {
	return m.rate.Rate()
}

func (m *RateMonitor) Inflight() int32 {
	return m.rate.CurrentInflight()
}
