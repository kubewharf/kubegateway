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

package controller

import (
	"k8s.io/apimachinery/pkg/api/errors"
	"sync"
	"time"

	"k8s.io/client-go/tools/cache"
	"k8s.io/klog"

	proxyv1alpha1 "github.com/kubewharf/kubegateway/pkg/apis/proxy/v1alpha1"
	gatewayinformers "github.com/kubewharf/kubegateway/pkg/client/informers"
	gatewayclientset "github.com/kubewharf/kubegateway/pkg/client/kubernetes"
	scheme "github.com/kubewharf/kubegateway/pkg/client/kubernetes/scheme"
	proxylisters "github.com/kubewharf/kubegateway/pkg/client/listers/proxy/v1alpha1"
	"github.com/kubewharf/kubegateway/pkg/syncqueue"
)

type UpstreamController interface {
	Run(stopCh <-chan struct{})
	UpstreamClusterLister() proxylisters.UpstreamClusterLister
	Get(cluster string) (*proxyv1alpha1.UpstreamCluster, bool)
}

type UpstreamClusterHandler func(cluster *proxyv1alpha1.UpstreamCluster) error

type upstreamController struct {
	queue                  *syncqueue.SyncQueue
	lister                 proxylisters.UpstreamClusterLister
	gatewayInformerFactory gatewayinformers.SharedInformerFactory
	synced                 cache.InformerSynced
	handlers               []UpstreamClusterHandler
	clusters               sync.Map
}

func NewUpstreamController(gatewayClient gatewayclientset.Interface, handlers ...UpstreamClusterHandler) UpstreamController {
	gatewayInformerFactory := gatewayinformers.NewSharedInformerFactory(gatewayClient, 0)
	upstreamClusterInformer := gatewayInformerFactory.Proxy().V1alpha1().UpstreamClusters()
	m := &upstreamController{
		handlers:               handlers,
		lister:                 upstreamClusterInformer.Lister(),
		synced:                 upstreamClusterInformer.Informer().HasSynced,
		gatewayInformerFactory: gatewayInformerFactory,
	}
	m.queue = syncqueue.NewPassthroughSyncQueue(proxyv1alpha1.SchemeGroupVersion.WithKind("UpstreamCluster"), m.syncUpstreamCluster)

	upstreamClusterInformer.Informer().AddEventHandler(m.queue.ResourceEventHandler(scheme.Scheme))
	return m
}

func (c *upstreamController) Run(stopCh <-chan struct{}) {
	klog.Info("starting upstream cluster controller")

	go c.gatewayInformerFactory.Start(stopCh)

	if !cache.WaitForCacheSync(stopCh, c.synced) {
		klog.Fatal("failed to wait for upstream cluster synced")
	}

	c.queue.Run(1)
	defer func() {
		c.queue.ShutDown()
	}()
	<-stopCh

	klog.Info("flowControl controller exited")
}

func (c *upstreamController) syncUpstreamCluster(obj interface{}) (syncqueue.Result, error) {
	cluster, ok := obj.(*proxyv1alpha1.UpstreamCluster)
	if !ok {
		return syncqueue.Result{}, nil
	}

	_, err := c.lister.Get(cluster.Name)
	if errors.IsNotFound(err) {
		c.clusters.Delete(cluster.Name)
	}

	for _, handler := range c.handlers {
		err := handler(cluster)
		if err != nil {
			klog.Errorf("failed to handle cluster: %v, err: %v", cluster.Name, err)
			return syncqueue.Result{RequeueAfter: 5 * time.Second, MaxRequeueTimes: 3}, nil
		}
	}
	return syncqueue.Result{}, nil
}

func (c *upstreamController) UpstreamClusterLister() proxylisters.UpstreamClusterLister {
	return c.lister
}

func (c *upstreamController) Get(cluster string) (*proxyv1alpha1.UpstreamCluster, bool) {
	upstream, ok := c.clusters.Load(cluster)
	if !ok {
		return nil, false
	}
	return upstream.(*proxyv1alpha1.UpstreamCluster), true
}
