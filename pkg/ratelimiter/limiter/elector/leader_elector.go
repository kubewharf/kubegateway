package elector

import (
	"context"
	"fmt"
	proxyv1alpha1 "github.com/kubewharf/kubegateway/pkg/apis/proxy/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"sync"
	"time"

	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	componentbaseconfig "k8s.io/component-base/config"
	"k8s.io/klog"
)

type LeaderElector interface {
	Run(ctx context.Context)
	IsLeader(shardId int) bool
	GetLeaders() map[int]proxyv1alpha1.EndpointInfo
	SetCallbacks(LeaderCallbacks)
}

type LeaderCallbacks struct {
	// OnStartedLeading is called when a LeaderElector client starts leading
	OnStartedLeading func(shardId int)
	// OnStoppedLeading is called when a LeaderElector client stops leading
	OnStoppedLeading func(shardId int)
}

func NewLeaderElector(config componentbaseconfig.LeaderElectionConfiguration, client clientset.Interface, identity string, ShardingCount int) (LeaderElector, error) {
	l := &leaderElector{
		identity:      identity,
		ShardingCount: ShardingCount,
		configs:       map[int]*leaderelection.LeaderElectionConfig{},
		leaderInfo:    map[int]proxyv1alpha1.EndpointInfo{},
	}

	for i := 0; i < ShardingCount; i++ {
		shardId := i
		electionConfig, err := makeLeaderElectionConfig(config, client, shardId, identity)
		if err != nil {
			return nil, err
		}
		l.configs[i] = electionConfig
	}

	return l, nil
}

type leaderElector struct {
	sync.RWMutex
	ShardingCount int
	configs       map[int]*leaderelection.LeaderElectionConfig
	leaderInfo    map[int]proxyv1alpha1.EndpointInfo
	identity      string

	callbacks LeaderCallbacks
}

func (l *leaderElector) Run(ctx context.Context) {
	for i := 0; i < l.ShardingCount; i++ {
		shardId := i
		electionConfig := l.configs[i]
		electionConfig.Callbacks = leaderelection.LeaderCallbacks{
			OnStartedLeading: func(ctx context.Context) {
				l.startLeading(shardId)
			},
			OnStoppedLeading: func() {
				l.stopLeading(shardId)
			},
			OnNewLeader: func(identity string) {
				l.setLeader(shardId, identity)
			},
		}
		elector, err := leaderelection.NewLeaderElector(*electionConfig)
		if err != nil {
			klog.Fatalf("couldn't create leader elector: %v", err)
		}
		go wait.Until(func() {
			elector.Run(ctx)
		}, time.Second, ctx.Done())
	}
}

func (l *leaderElector) GetLeaders() map[int]proxyv1alpha1.EndpointInfo {
	l.RLock()
	defer l.RUnlock()
	leaderInfo := map[int]proxyv1alpha1.EndpointInfo{}
	for id, leader := range l.leaderInfo {
		leaderInfo[id] = leader
	}
	return leaderInfo
}

func (l *leaderElector) IsLeader(shardId int) bool {
	l.RLock()
	defer l.RUnlock()
	return l.leaderInfo[shardId].Leader == l.identity
}

func (l *leaderElector) SetCallbacks(callbacks LeaderCallbacks) {
	l.callbacks = callbacks
}

func (l *leaderElector) startLeading(shardId int) {
	klog.Infof("Start leading %v", shardId)
	l.setLeader(shardId, l.identity)
	if l.callbacks.OnStartedLeading != nil {
		l.callbacks.OnStartedLeading(shardId)
	}
}

func (l *leaderElector) stopLeading(shardId int) {
	klog.Infof("Stop leading %v", shardId)
	l.Lock()
	if l.leaderInfo[shardId].Leader == l.identity {
		delete(l.leaderInfo, shardId)
	}
	l.Unlock()

	if l.callbacks.OnStoppedLeading != nil {
		l.callbacks.OnStoppedLeading(shardId)
	}
}

func (l *leaderElector) setLeader(shardId int, identity string) {
	klog.Infof("Shard %v leader: %v", shardId, identity)
	l.Lock()
	defer l.Unlock()
	if _, ok := l.leaderInfo[shardId]; !ok {
		l.leaderInfo[shardId] = proxyv1alpha1.EndpointInfo{ShardID: int32(shardId)}
	}
	info := l.leaderInfo[shardId]
	info.Leader = identity
	info.LastChange = metav1.Now()
	l.leaderInfo[shardId] = info
}

// makeLeaderElectionConfig builds a leader election configuration. It will
// create a new resource lock associated with the configuration.
func makeLeaderElectionConfig(config componentbaseconfig.LeaderElectionConfiguration, client clientset.Interface, shardID int, identity string) (*leaderelection.LeaderElectionConfig, error) {
	rl, err := resourcelock.New(config.ResourceLock,
		config.ResourceNamespace,
		fmt.Sprintf("%v-%v", config.ResourceName, shardID),
		client.CoreV1(),
		client.CoordinationV1(),
		resourcelock.ResourceLockConfig{
			Identity: identity,
		})
	if err != nil {
		return nil, fmt.Errorf("couldn't create resource lock: %v", err)
	}

	return &leaderelection.LeaderElectionConfig{
		Lock:          rl,
		LeaseDuration: config.LeaseDuration.Duration,
		RenewDeadline: config.RenewDeadline.Duration,
		RetryPeriod:   config.RetryPeriod.Duration,
		WatchDog:      leaderelection.NewLeaderHealthzAdaptor(time.Second * 20),
		Name:          fmt.Sprintf("kube-gateway-rate-limiter-%v", shardID),
	}, nil
}
