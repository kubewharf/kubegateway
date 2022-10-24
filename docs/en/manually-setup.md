# Manually Setup

For demo purpose, this tutorial introduces how to setup KubeGateway on local Kubernetes by `kind`, and we will use the kind cluster as upstream cluster.

## Prerequisites

Please install the following tools:
- [go](https://go.dev/doc/install)
- [kind](https://kind.sigs.k8s.io/docs/user/quick-start/#installation)
- [kubectl](https://kubernetes.io/docs/tasks/tools/)
- [yq](https://github.com/mikefarah/yq#install)
- [cfssl](https://github.com/cloudflare/cfssl#installation)

## Quick Start

Build the environment locally by running the script

```
bash hack/local-up.sh
```

The script performs the following tasks:
1. create a local kubernetes cluster named `kubegateway` by `kind`
2. generating
   1. generate tls key and cert for kube-gateway control plane server, and create a associated secret
   2. generate tls key and cert for kube-gateway to connect to upstream kube-apiserver
   3. generate tls key and cert for client to connect to kube-gateway control plane
3. configure contexts of kubectl
   1. create a new context named `gateway-control-plane`
   2. create a new context named `gateway-proxy`
4. compile and build image
5. run kube-gateway on local kubernetes cluster and create a new UpstreamCluster on it

![arch](../image/local-up.jpg)

After script running completed, then
- you can comunicate with kube-gateway control plane by using `kubectl --context gateway-control-plane`
- you can comunicate with kube-apiserver through kube-gateway proxy by using `kubectl --context gateway-proxy`
- you can comunicate with kube-apiserver directly by using `kubectl --context kind-kubegateway`


### Extra Notes
1. make sure local port 8443 is not occupied
2. please specify the domain name to `localhost` when connecting to kube-apiserver through kube-gateway proxy 
3. for demo purpose, default user has super privilege. you can generate new user tls key and cert by using `ca.key` and `ca.crt` under `output/upstream`
