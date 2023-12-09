package store

import (
	gatewayclientset "github.com/kubewharf/kubegateway/pkg/client/kubernetes"
	"github.com/kubewharf/kubegateway/pkg/ratelimiter/options"
	"github.com/kubewharf/kubegateway/pkg/ratelimiter/store/interface"
	"github.com/kubewharf/kubegateway/pkg/ratelimiter/store/k8s"
	"github.com/kubewharf/kubegateway/pkg/ratelimiter/store/local"
)

func NewLimitStore(gatewayClient gatewayclientset.Interface, limitOptions options.RateLimitOptions, shard, shardCount int) _interface.LimitStore {
	switch limitOptions.LimitStore {
	case "local":
		return local.NewLocalStore()
	case "k8s":
		return k8s.NewK8sCacheStore(gatewayClient, limitOptions.K8sStoreSyncPeriod, shard, shardCount)
	}
	return nil
}
