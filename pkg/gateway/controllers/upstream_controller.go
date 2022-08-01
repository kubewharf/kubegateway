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

package controllers

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	requestx509 "k8s.io/apiserver/pkg/authentication/request/x509"
	"k8s.io/apiserver/pkg/server/dynamiccertificates"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog"

	proxyv1alpha1 "github.com/kubewharf/kubegateway/pkg/apis/proxy/v1alpha1"
	proxyinformers "github.com/kubewharf/kubegateway/pkg/client/informers/proxy/v1alpha1"
	scheme "github.com/kubewharf/kubegateway/pkg/client/kubernetes/scheme"
	proxylisters "github.com/kubewharf/kubegateway/pkg/client/listers/proxy/v1alpha1"
	"github.com/kubewharf/kubegateway/pkg/clusters"
	gatewaynet "github.com/kubewharf/kubegateway/pkg/gateway/net"
	"github.com/kubewharf/kubegateway/pkg/syncqueue"
)

var _ dynamiccertificates.DynamicClientConfigProvider = &UpstreamClusterController{}
var _ requestx509.SNIVerifyOptionsProvider = &UpstreamClusterController{}

type UpstreamClusterController struct {
	queue  *syncqueue.SyncQueue
	lister proxylisters.UpstreamClusterLister
	synced cache.InformerSynced

	clusters.Manager
}

func NewUpstreamClusterController(upstreamclusterinformer proxyinformers.UpstreamClusterInformer) *UpstreamClusterController {
	m := &UpstreamClusterController{
		lister:  upstreamclusterinformer.Lister(),
		synced:  upstreamclusterinformer.Informer().HasSynced,
		Manager: clusters.NewManager(),
	}
	m.queue = syncqueue.NewPassthroughSyncQueue(proxyv1alpha1.SchemeGroupVersion.WithKind("UpstreamCluster"), m.syncUpstreamCluster)

	upstreamclusterinformer.Informer().AddEventHandler(m.queue.ResourceEventHandler(scheme.Scheme))
	return m
}

func (m *UpstreamClusterController) Run(stopCh <-chan struct{}) {
	klog.Info("starting upstream cluster controller")
	if !cache.WaitForCacheSync(stopCh, m.synced) {
		panic("failed to wait for upstream cluster synced")
	}

	m.queue.Run(1)
	defer func() {
		m.queue.ShutDown()
		m.DeleteAll()
	}()
	<-stopCh
}

func (m *UpstreamClusterController) syncUpstreamCluster(obj interface{}) (syncqueue.Result, error) {
	cluster, ok := obj.(*proxyv1alpha1.UpstreamCluster)
	if !ok {
		return syncqueue.Result{}, nil
	}

	_, err := m.lister.Get(cluster.Name)
	clusterName := cluster.Name
	if errors.IsNotFound(err) {
		// clean cluster
		m.Delete(clusterName)
		return syncqueue.Result{}, nil
	}
	if err != nil {
		return syncqueue.Result{}, err
	}

	info, ok := m.Get(clusterName)

	if !ok {
		// bootstrap
		clusterInfo, err := clusters.CreateClusterInfo(cluster, GatewayHealthCheck)
		if err != nil {
			klog.Errorf("failed to create cluster: %v, err: %v", cluster.Name, err)
			return syncqueue.Result{RequeueAfter: 5 * time.Second, MaxRequeueTimes: 3}, nil
		}

		m.Add(clusterInfo)
		return syncqueue.Result{}, err
	}

	// sync
	err = info.Sync(cluster)
	if err != nil {
		klog.Errorf("failed to sync cluster: %v, err: %v", cluster.Name, err)
		return syncqueue.Result{RequeueAfter: 5 * time.Second, MaxRequeueTimes: 3}, nil
	}

	return syncqueue.Result{}, nil
}

func (m *UpstreamClusterController) WrapGetConfigForClient(getConfigFunc dynamiccertificates.GetConfigForClientFunc) dynamiccertificates.GetConfigForClientFunc {
	return func(clientHello *tls.ClientHelloInfo) (*tls.Config, error) {
		baseTLSConfig, err := getConfigFunc(clientHello)
		if err != nil {
			return baseTLSConfig, err
		}

		// if the client set SNI information, just use our "normal" SNI flow
		// Get request host name from SNI information or inspect the requested IP
		hostname := clientHello.ServerName
		if len(hostname) == 0 {
			// if the client didn't set SNI, then we need to inspect the requested IP so that we can choose
			// a certificate from our list if we specifically handle that IP.  This can happen when an IP is specifically mapped by name.
			var err error
			hostname, _, err = net.SplitHostPort(clientHello.Conn.LocalAddr().String())
			if err != nil {
				klog.Errorf("faild to get hostname from clientHello's conn: %v", err)
				return baseTLSConfig, nil
			}
		}

		klog.V(5).Infof("get tls config for %q", hostname)

		cluster, ok := m.Get(hostname)
		if !ok {
			return baseTLSConfig, nil
		}

		tlsConfig, ok := cluster.LoadTLSConfig()
		if !ok {
			return baseTLSConfig, nil
		}

		tlsConfigCopy := baseTLSConfig.Clone()

		if tlsConfig.ClientCAs != nil {
			// Populate PeerCertificates in requests, but don't reject connections without certificates
			// This allows certificates to be validated by authenticators, while still allowing other auth types
			tlsConfigCopy.ClientAuth = tls.RequestClientCert
			tlsConfigCopy.ClientCAs = tlsConfig.ClientCAs
		}
		if len(tlsConfig.Certificates) > 0 {
			// provide specific certificates
			tlsConfigCopy.Certificates = tlsConfig.Certificates
			tlsConfigCopy.NameToCertificate = nil //nolint
			tlsConfigCopy.GetCertificate = nil
			tlsConfigCopy.GetConfigForClient = nil
		}
		return tlsConfigCopy, nil
	}
}

func (m *UpstreamClusterController) SNIVerifyOptions(host string) (x509.VerifyOptions, bool) {
	hostname := gatewaynet.HostWithoutPort(host)
	empty := x509.VerifyOptions{}
	cluster, ok := m.Get(hostname)
	if !ok {
		return empty, false
	}
	return cluster.LoadVerifyOptions()
}

// health check endpoint periodically
func GatewayHealthCheck(e *clusters.EndpointInfo) (done bool) {
	done = false

	// TODO: use readyz if all kubernetes master version is greater than v1.16
	result := e.Clientset().CoreV1().RESTClient().
		Get().AbsPath("/healthz").Timeout(5 * time.Second).Do(context.TODO())
	err := result.Error()

	var reason, message string
	statusCode := 0

	if err != nil {
		if os.IsTimeout(err) {
			reason = "Timeout"
			message = err.Error()
		} else {
			switch status := err.(type) {
			case errors.APIStatus:
				reason = string(status.Status().Reason)
				message = status.Status().Message
			default:
				reason = "Failure"
				message = err.Error()
			}
		}
	} else {
		result.StatusCode(&statusCode)
		if statusCode == http.StatusOK {
			e.UpdateStatus(true, "", "")
			return done
		}
		reason = "NotReady"
		message = fmt.Sprintf("request %s/healthz, got response code is %v", e.Endpoint, statusCode)
	}
	klog.Errorf("upstream health check failed, cluster=%q endpoint=%q reason=%q message=%q", e.Cluster, e.Endpoint, reason, message)
	e.UpdateStatus(false, reason, message)
	return done
}
