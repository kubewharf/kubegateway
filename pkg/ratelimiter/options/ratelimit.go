package options

import (
	"github.com/spf13/pflag"
	componentbaseconfig "k8s.io/component-base/config"
	"time"
)

type RateLimitOptions struct {
	ShardingCount      int
	LimitStore         string
	Identity           string
	K8sStoreSyncPeriod time.Duration
	componentbaseconfig.LeaderElectionConfiguration

	EnableControlPlane bool
}

func (c *RateLimitOptions) AddFlags(fs *pflag.FlagSet) {
	if c == nil {
		return
	}
	// leaderelectionconfig.BindFlags(&c.LeaderElectionConfiguration, fs)

	fs.StringVar(&c.Identity, "identity", c.Identity, "rate limiter identity for leader election")
	fs.IntVar(&c.ShardingCount, "sharding-count", c.ShardingCount, "Total rate limiter sharding count")

	fs.StringVar(&c.LimitStore, "limit-store", c.LimitStore, "rate limiter store type")
	fs.DurationVar(&c.K8sStoreSyncPeriod, "k8s-store-sync-period", c.K8sStoreSyncPeriod, "sync period for k8s object store")

	fs.BoolVar(&c.EnableControlPlane, "enable-controlplane", c.EnableControlPlane, "start control plane server")
}
