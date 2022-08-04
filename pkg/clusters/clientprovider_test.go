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
	"errors"
	"testing"
	"time"
)

func createTestNotReadyClusterInfo() *ClusterInfo {
	cfg := newTestUpstreamClusterConfig()
	cfg.Name = "testing.notReadyCluster"
	ret, _ := CreateClusterInfo(cfg, nil)
	return ret
}

func Test_manager_ClientFor(t *testing.T) {
	manager := NewManager()
	// cluster := createTestClusterInfo()

	_, _, err := manager.ClientFor("not-found")
	if err == nil {
		t.Error("ClientFor() want error, got nil")
	}
	if !errors.Is(err, ErrClusterNotFound) {
		t.Errorf("ClientFor() want ErrClusterNotFound, got = %v", err)
	}

	// add not ready cluster
	manager.Add(createTestNotReadyClusterInfo())

	cluster, _, err := manager.ClientFor("testing.notReadyCluster")
	if err == nil {
		t.Error("ClientFor() want error, but got nil")
	}
	if cluster == nil {
		t.Error("ClientFor() want cluster, but got nil")
	}

	// add always ready cluster
	manager.Add(createTestClusterInfo())
	time.Sleep(1 * time.Second)
	cluster, client, err := manager.ClientFor("testing.cluster")
	if err != nil {
		t.Errorf("ClientFor() want no error, got %v", err)
	}
	if cluster == nil {
		t.Error("ClientFor() want cluster, but got nil")
	}
	if client == nil {
		t.Error("ClientFor() want client, but got nil")
	}
}
