// Copyright 2022 ByteDance and its affiliates.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package clusters

import (
	"strings"
	"sync"

	"github.com/pkg/errors"
	"k8s.io/klog"
)

var (
	ErrClusterNotFound = errors.New("cluster not found")
)

type EndpointHealthCheck func(*EndpointInfo) (done bool)

type Manager interface {
	Add(*ClusterInfo)
	AddWithKey(string, *ClusterInfo)
	Get(name string) (*ClusterInfo, bool)
	Delete(name string)
	DeleteWithStop(name string)
	DeleteAll()

	ClientProvider
}

var _ Manager = &manager{}

type manager struct {
	clusters sync.Map
}

func NewManager() Manager {
	return &manager{
		clusters: sync.Map{},
	}
}

func (m *manager) Get(name string) (*ClusterInfo, bool) {
	name = strings.ToLower(name)
	v, ok := m.clusters.Load(name)
	if !ok {
		return nil, false
	}
	return v.(*ClusterInfo), true
}

func (m *manager) Add(cluster *ClusterInfo) {
	m.AddWithKey(cluster.Cluster, cluster)
}

func (m *manager) AddWithKey(key string, cluster *ClusterInfo) {
	if cluster == nil {
		return
	}
	key = strings.ToLower(key)
	klog.V(1).Infof("[cluster manager] new cluster info is added, cluster=%q [%q]", cluster.Cluster, key)
	m.clusters.Store(key, cluster)
}

func (m *manager) Delete(name string) {
	m.doDelete(name, false)
}

func (m *manager) DeleteWithStop(name string) {
	m.doDelete(name, true)
}

func (m *manager) doDelete(name string, stop bool) {
	name = strings.ToLower(name)
	v, ok := m.clusters.LoadAndDelete(name)
	if !ok {
		return
	}
	// close all requests to this cluster
	cluster := v.(*ClusterInfo)
	if stop {
		cluster.Stop()
	}

	klog.V(1).Infof("[cluster manager] cluster info is deleted, cluster=%q [%q]", cluster.Cluster, name)
}

func (m *manager) DeleteAll() {
	klog.V(1).Infof("[cluster manager] delete all cluster info")
	m.clusters.Range(func(key, value interface{}) bool {
		cluster := value.(*ClusterInfo)
		cluster.Stop()
		return true
	})
	m.clusters = sync.Map{}
}
