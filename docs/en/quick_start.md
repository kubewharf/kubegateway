# Quick Start

## Compilation

### Precondition

Go1.16 or higher is required.

### How to compile

Run directly on the project:

```Plain
make all
```

The compiled result will exist in:

```Shell
bin/${GOOS}_${GOARCH}/kube-gateway
```

## Running

### Precondition

The startup of KubeGateway depends on etcd, for the startup and deployment of etcd, please refer to: https://etcd.io/docs/v3.5/quickstart/ .

### Startup

```Plain
kube-gateway \
    --etcd-servers=http://localhost:2379 \
    --etcd-prefix=/registry/KubeGateway \
    --secure-port=9443 \
    --proxy-secure-ports=6443 \
    --authorization-mode AlwaysAllow \
    --enable-proxy-access-log=true 、
    --v=5
```

With the above startup command:

- Kube-gateway will self-check out private keys, certificates and CA certificates;
- The control plane listens on port `9443` to receive requests for configuration changes;
- The proxy listens on port `6443`.

After KubeGateway starts, it cannot proxy any traffic yet. We need to add the upstream cluster to the control plane to make the proxy take effect.

### Adding Upstream Cluster

First, create the yaml file for the upstream cluster:

-  The "name" `cluster-a.kubegateway.io` is the domain name of the upstream cluster, it should be signed into the certificate of upstream cluster;
- secureServing is not required
  - If clientCAData is not provided, KubeGateway's clientCA is used to validate the client certificate;
  - If keyData and certData are not provided, the key and cert of the KubeGateway will be used for external services.
- In the clientConfig configuration, you need to ensure that the client of kubegateway has sufficient permissions, see the [design document](design.md) for details.

```YAML
apiVersion: proxy.kubegateway.io/v1alpha1
kind: UpstreamCluster
metadata:
  name: cluster-a.kubegateway.io
spec:
  servers:
  - endpoint: https://<ip>:<port>
  secureServing:
    keyData: <base64 encode key PEM data>
    certData: <base64 encode cert PEM data>
    clientCAData: <base64 encode ca PEM data>
  clientConfig:
    keyData: <base64 encode key PEM data>
    certData: <base64 encode cert PEM data>
    caData: <base64 encode ca PEM data>
  dispatchPolicies:
  - rules:
    - verbs: ["*"]
      apiGroups: ["*"]
      resources: ["*"]
  - rules:
    - verbs: ["*"]
      nonResourceURLs: ["*"]
```

Then apply it to the control plane of KubeGateway through kubectl.

```YAML
kubectl --kubeconfig <path-to-kube-config> apply -f cluster-a.kubegateway.io.yaml
```

### Accessing

Then you can access the corresponding cluster through the KubeGateway proxy port.

```Shell
curl -k https://cluster-a.kubegateway.io:6443/api --resolve cluster-a.kubegateway.io:6443:127.0.0.1
```

## Advanced configuration

### Reuse Port


KubeGateway supports starting through the reuse port mode. In this mode, multiple KubeGateway services can be started in one node to improve the reliability of a single node. This is achieved by adding the startup parameter `--enable-reuse-port`.

It should be noted that in the reuse port mode, multiple KubeGateway loopback clients will access each other, so you need to set an additional parameter `--loopback-client-token="put-your-token-here"` to ensure that they can authenticate each other.

### FlowControl

We have three flow control methods that can be configured in UpstreamCluster's field`spec.flowControl.flowControlSchemas`, and then referenced in field`spec.dispatchPolicies.flowControlSchemaName`.

#### Exempt

```YAML
spec:
  flowControl:
    flowControlSchemas:
    - name: "exempt"
      exempt: {}
```

#### TokenBucket

```YAML
spec:
  flowControl:
    flowControlSchemas:
    - name: "tokenbucket"
      tokenBucket:
        qps: 100
        burst: 200
```

#### MaxRequestsInflight

```YAML
spec:
  flowControl:
    flowControlSchemas:
    - name: "limited"
      maxRequestsInflight:
        max: 1
```

#### Full Quote

```YAML
apiVersion: proxy.kubegateway.io/v1alpha1
kind: UpstreamCluster
metadata:
  name: <your-k8s-cluster-domain>
spec:
  flowControl:
    - name: "exempt"
      exempt: {}
  dispatchPolicies:
  - rules:
    - verbs: ["*"]
      apiGroups: ["*"]
      resources: ["*"]
    flowControlSchemaName: exempt
  - rules:
    - verbs: ["*"]
      nonResourceURLs: ["*"]
```

### Routing

See the Routing section in the design document for details: TODO 链接

## Configuration Examples

### Read-Write Separation

- Shunt all get, list, watch requests to ip-a;
- Shunt other requests to ip-b.

```YAML
apiVersion: proxy.kubegateway.io/v1alpha1
kind: UpstreamCluster
metadata:
  name: cluster-a.kubegateway.io
spec:
  ...
  servers:
  - endpoint: https://ip-a:6443
  - endpoint: https://ip-b:6443
  dispatchPolicies:
  - rules:
    - verbs: ["list", "get", "watch"]
      apiGroups: ["*"]
      resources: ["*"]
    upsteamSubset:
    - https://ip-a:6443
  - rules:
    - verbs: ["*"]
      apiGroups: ["*"]
      resources: ["*"]
    upsteamSubset:
    - https://ip-b:6443
```

### Restricting requests for list pods

```YAML
apiVersion: proxy.kubegateway.io/v1alpha1
kind: UpstreamCluster
metadata:
  name: cluster-a.kubegateway.io
spec:
  ...
  flowControl:
    flowControlSchemas:
    - name: "list-pod"
      tokenBucket:
        qps: 10
        burst: 20
  dispatchPolicies:
  - rules:
    - verbs: ["list"]
      apiGroups: ["*"]
      resources: ["pods"]
    flowControlSchemaName: list-pod
```

### Limiting the request rate of user-a

```YAML
apiVersion: proxy.kubegateway.io/v1alpha1
kind: UpstreamCluster
metadata:
  name: cluster-a.kubegateway.io
spec:
  ...
  flowControl:
    flowControlSchemas:
    - name: "limit-user-a"
      tokenBucket:
        qps: 10
        burst: 20
  dispatchPolicies:
  - rules:
    - verbs: ["*"]
      apiGroups: ["*"]
      resources: ["*"]
      users: ["user-a"]
    flowControlSchemaName: "limit-user-a"
```
