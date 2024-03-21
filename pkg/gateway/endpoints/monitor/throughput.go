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
	throughputMeterBucketLen = 3
	throughputTickDuration   = time.Second * 10
)

type ThroughputMonitor struct {
	request  *util.Meter
	response *util.Meter
}

func NewThroughputMonitor() *ThroughputMonitor {
	m := &ThroughputMonitor{
		request:  util.NewMeter("request", throughputMeterBucketLen, throughputTickDuration, 0, 0),
		response: util.NewMeter("response", throughputMeterBucketLen, throughputTickDuration, 0, 0),
	}
	m.request.Start()
	m.response.Start()
	return m
}

func (m *ThroughputMonitor) RecordRequest(n int64) {
	m.request.AddN(n)
}

func (m *ThroughputMonitor) RecordResponse(n int64) {
	m.response.AddN(n)
}

func (m *ThroughputMonitor) RequestSize() int64 {
	return m.request.Count()
}

func (m *ThroughputMonitor) ResponseSize() int64 {
	return m.response.Count()
}

func (m *ThroughputMonitor) Throughput() float64 {
	return m.request.Rate() + m.response.Rate()
}
