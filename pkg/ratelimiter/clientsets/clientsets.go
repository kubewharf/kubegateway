package clientsets

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog"

	proxyv1alpha1 "github.com/kubewharf/kubegateway/pkg/apis/proxy/v1alpha1"
	gatewayclientset "github.com/kubewharf/kubegateway/pkg/client/kubernetes"
	limitutil "github.com/kubewharf/kubegateway/pkg/ratelimiter/util"
)

var (
	clients ClientSets
	once    sync.Once
)

const (
	SyncPeriod                    = time.Second * 2
	ClientCacheTTL                = time.Minute * 2
	ServerHeartBeatInterval       = time.Second * 1
	ServerHeartBeatTimeout        = time.Second * 5
	ServerHeartBeatRequestTimeout = time.Millisecond * 1000

	ServerInfoUrl = "/apis/proxy.kubegateway.io/v1alpha1/ratelimit/endpoints"
	HeartBeatUrl  = "/apis/proxy.kubegateway.io/v1alpha1/ratelimit/heartbeat"
)

type LookupFunc func(service string) []string

type ClientSets interface {
	GetAllClients() []gatewayclientset.Interface
	ClientFor(cluster string) (gatewayclientset.Interface, error)
	ShardIDFor(cluster string) (int, error)
	IsReady(cluster string) bool
	ClientID() string
}

func GetClientSets(ctx context.Context, service, kubeconfig, runIdPrefix string) ClientSets {
	if len(service) == 0 {
		return nil
	}
	once.Do(func() {
		clients = NewClientSets(ctx, service, runIdPrefix, kubeconfig)
	})
	return clients
}

func NewClientSets(ctx context.Context, service, runIdPrefix, kubeconfig string) ClientSets {
	restConfig, err := newRestConfig(service, kubeconfig)
	if err != nil {
		klog.Fatalf("New rest config error: %v", err)
	}
	clients = newClientSets(ctx, service, runIdPrefix, restConfig)
	return clients
}

func NewClientSetsWithRestConfig(ctx context.Context, service, runIdPrefix string, restConfig *rest.Config) ClientSets {
	return newClientSets(ctx, service, runIdPrefix, restConfig)
}

func newClientSets(ctx context.Context, service, runIdPrefix string, restConfig *rest.Config) ClientSets {
	runId := fmt.Sprintf("%v-%s", os.Getpid(), utilrand.String(5))
	if len(runIdPrefix) > 0 {
		runId = fmt.Sprintf("%s-%s", runIdPrefix, runId)
	}

	c := &clientSets{
		service:    service,
		lookupFunc: ServiceLookup,
		restConfig: restConfig,
		runId:      runId,
		insecure:   len(restConfig.TLSClientConfig.CAData) == 0,
	}

	go wait.Until(c.sync, SyncPeriod, ctx.Done())
	go wait.Until(c.cleanup, ClientCacheTTL, ctx.Done())
	go wait.Until(c.clientHeart, ServerHeartBeatInterval, ctx.Done())

	return c
}

type clientSets struct {
	service         string
	endpoints       []string
	leaderEndpoints sync.Map
	leaderReady     sync.Map
	shardCount      int
	lookupFunc      LookupFunc
	clientsCache    sync.Map
	restConfig      *rest.Config
	runId           string
	insecure        bool
}

type clientCache struct {
	expire time.Time
	client gatewayclientset.Interface
}

type heartbeatStatus struct {
	lastChange time.Time
	lastState  bool
	ready      bool
}

func (c *clientSets) GetAllClients() []gatewayclientset.Interface {
	var clients []gatewayclientset.Interface

	c.leaderEndpoints.Range(func(key, value interface{}) bool {
		if cc, ok := c.clientsCache.Load(value); ok {
			clients = append(clients, cc.(*clientCache).client)
		}
		return true
	})
	return clients
}

func (c *clientSets) ClientFor(cluster string) (gatewayclientset.Interface, error) {
	shard, err := c.ShardIDFor(cluster)
	if err != nil {
		return nil, err
	}
	value, ok := c.leaderEndpoints.Load(shard)
	if !ok {
		return nil, fmt.Errorf("server shard %v has no leader", shard)
	}
	server := value.(string)

	return c.getOrCreateClient(server)
}

func (c *clientSets) ShardIDFor(cluster string) (int, error) {
	if c.shardCount == 0 {
		return -1, fmt.Errorf("shard count not synced")
	}
	shard := limitutil.GetShardID(cluster, c.shardCount)
	return shard, nil
}

func (c *clientSets) IsReady(cluster string) bool {
	shard, err := c.ShardIDFor(cluster)
	if err != nil {
		return false
	}
	hbstatus, ok := c.leaderReady.Load(shard)
	if !ok {
		return false
	}
	status := hbstatus.(*heartbeatStatus)
	return status.ready
}

func (c *clientSets) ClientID() string {
	return c.runId
}

func (c *clientSets) sync() {
	servers := c.lookupFunc(c.service)
	if len(servers) == 0 {
		klog.Errorf("Failed to lookup service: %v", servers)
		return
	}

	server := servers[rand.Intn(len(servers))]

	client, err := c.getOrCreateClient(server)
	if err != nil {
		klog.Errorf("Failed to get client: %v", err)
		return
	}

	serverInfo, err := getServerInfo(client)
	if err != nil {
		klog.Errorf("Failed to get server info: %v", err)
		return
	}

	c.shardCount = int(serverInfo.ShardCount)

	for _, ep := range serverInfo.Endpoints {
		old, ok := c.leaderEndpoints.Load(int(ep.ShardID))
		var oldLeader string
		if ok {
			oldLeader = old.(string)
		}

		if oldLeader != ep.Leader {
			klog.Infof("Server shard %v change to %v", ep.ShardID, ep.Leader)
			c.leaderEndpoints.Store(int(ep.ShardID), ep.Leader)
			c.setLeaderStatus(int(ep.ShardID), ep.Leader, true)
		}

		_, err := c.getOrCreateClient(ep.Leader)
		if err != nil {
			klog.Errorf("Update client: %v", err)
			continue
		}
	}

	c.clientsCache.Range(func(key, value interface{}) bool {
		cc := value.(*clientCache)
		cc.expire = time.Now().Add(ClientCacheTTL)
		return true
	})

}

func (c *clientSets) getOrCreateClient(server string) (gatewayclientset.Interface, error) {
	ccObj, ok := c.clientsCache.Load(server)
	if !ok {
		client, err := c.createClient(server)
		if err != nil {
			return nil, fmt.Errorf("failed to create client %s: %v", server, err)
		} else {
			cc := &clientCache{
				expire: time.Now().Add(ClientCacheTTL),
				client: client,
			}
			c.clientsCache.Store(server, cc)
			return client, nil
		}
	}
	return ccObj.(*clientCache).client, nil
}

func (c *clientSets) cleanup() {
	c.clientsCache.Range(func(key, value interface{}) bool {
		cc := value.(*clientCache)
		if cc.expire.After(time.Now()) {
			klog.Infof("Delete expire client %s", key)
			c.clientsCache.Delete(key)
		}

		return true
	})
}

func (c *clientSets) createClient(server string) (gatewayclientset.Interface, error) {
	http2configCopy := *c.restConfig
	http2configCopy.Host = server

	if c.insecure && strings.HasPrefix(server, "https") {
		http2configCopy.TLSClientConfig.Insecure = true
	}

	if strings.HasPrefix(server, "http://") || !strings.HasPrefix(server, "http") {
		http2configCopy.TLSClientConfig = rest.TLSClientConfig{}
	}

	return gatewayclientset.NewForConfig(&http2configCopy)
}

func getServerInfo(client gatewayclientset.Interface) (*proxyv1alpha1.RateLimitServerInfo, error) {
	result := client.ProxyV1alpha1().RESTClient().Get().AbsPath(ServerInfoUrl).Do(context.TODO())

	respBody, err := result.Raw()

	if err != nil {
		return nil, fmt.Errorf("server response error: %v", err)
	}
	statusCode := 0
	result.StatusCode(&statusCode)
	if statusCode != http.StatusOK {
		klog.Errorf("Failed to get server info, resp code: %v", statusCode)
		return nil, fmt.Errorf("status code is %v", statusCode)
	}

	serverInfo := &proxyv1alpha1.RateLimitServerInfo{}
	err = json.Unmarshal(respBody, &serverInfo)
	if err != nil {
		klog.Errorf("Unmarshal server info err: %v", err)
		return nil, fmt.Errorf("unmarshal response body err: %v, body: %v", err, string(respBody))
	}
	return serverInfo, err
}

func (c *clientSets) clientHeart() {
	leaderClients := map[int]struct {
		server string
		client gatewayclientset.Interface
	}{}

	c.leaderEndpoints.Range(func(shard, server interface{}) bool {
		if cc, ok := c.clientsCache.Load(server); ok {
			leaderClients[shard.(int)] = struct {
				server string
				client gatewayclientset.Interface
			}{server: server.(string), client: cc.(*clientCache).client}
		}
		return true
	})

	var wg sync.WaitGroup
	for serverShard, lc := range leaderClients {
		client := lc.client
		server := lc.server
		shard := serverShard
		if client != nil {
			wg.Add(1)
			go func() {
				defer wg.Done()
				result := client.ProxyV1alpha1().RESTClient().Post().
					AbsPath(HeartBeatUrl).
					Param("instance", c.runId).
					Timeout(ServerHeartBeatRequestTimeout).
					Do(context.TODO())

				ready := false
				respBody, err := result.Raw()
				if err != nil {
					klog.Errorf("Heartbeat to %s error: %v, body: %v", server, err, string(respBody))
				} else {
					statusCode := 0
					result.StatusCode(&statusCode)
					if statusCode == http.StatusOK {
						ready = true
					} else {
						klog.Errorf("Heartbeat to %s failed, code: %v, body: %v", server, statusCode, string(respBody))
					}
				}
				c.setLeaderStatus(shard, server, ready)
			}()
		}
	}
	wg.Wait()
}

func (c *clientSets) setLeaderStatus(shard int, server string, ready bool) {
	hs, ok := c.leaderReady.Load(shard)
	if !ok {
		hs = &heartbeatStatus{}
		c.leaderReady.Store(shard, hs)
	}
	status := hs.(*heartbeatStatus)
	if status.lastState != ready {
		status.lastState = ready
		status.lastChange = time.Now()
	}

	if status.ready != ready {
		if ready {
			status.ready = ready
			recordStatusChange(shard, server, ready)
		} else if time.Now().After(status.lastChange.Add(ServerHeartBeatTimeout)) {
			status.ready = ready
			recordStatusChange(shard, server, ready)
		}
	}
}

func recordStatusChange(shard int, server string, ready bool) {
	klog.V(1).Infof("[limiter server] heartbeat status changed, shard=%v, server=%q, ready=%v",
		shard, server, ready,
	)
}

func newRestConfig(server, kubeconfig string) (*rest.Config, error) {
	restConfig, err := clientcmd.BuildConfigFromFlags(server, kubeconfig)
	if err != nil {
		return nil, err
	}
	restConfig.QPS = 10000
	restConfig.Burst = 10000
	return restConfig, nil
}
