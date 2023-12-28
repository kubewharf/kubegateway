# 快速开始

## 编译

### 前置条件

需要 go1.16 以上的版本

### 如何编译

在项目中直接运行 

```Plain
make all
```

编辑结果会存在于 

```Shell
bin/${GOOS}_${GOARCH}/kube-gateway
```

## 运行

### 前置条件

KubeGateway 的启动依赖于 etcd，etcd 的启动部署请参考 https://etcd.io/docs/v3.5/quickstart/

### 启动

```Plain
kube-gateway \
    --etcd-servers=http://localhost:2379 \
    --etcd-prefix=/registry/KubeGateway \
    --secure-port=9443 \
    --proxy-secure-ports=6443 \
    --authorization-mode AlwaysAllow \
    --enable-proxy-access-log=true \
    --v=5
```

上面的启动命令下

- kube-gateway 会自签出私钥，证书和 CA 证书
- 控制面会监听在 9443 端口，用于接收配置变更的请求
- 代理会监听在 6443 端口

KubeGateway 启动之后，还不能代理任何的流量，我们需要给控制面添加上游集群的配置，从而让代理生效

### 添加上游集群

首先创建 upstream cluster 的 yaml 文件

- 其中 name `cluster-a.kubegateway.io` 为 upstream cluster 的域名, upstream cluster 的证书中需要签入这个域名

- secureServing 中的内容不是必须的
  - 如果不提供 clientCAData，则会使用 KubeGateway 的 clientCA 用来验证客户端证书
  - 如果不提供 keyData 和 certData，则会使用 KubeGateway 的 key 的 cert 对外服务
  
- 其中 clientConfig 的配置中需要保证 kubegateway 的客户端有足够的权限，详情参考[设计文档](design.md)

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

然后通过 kubectl 把它应用到 KubeGateway 的控制面去

```YAML
kubectl --kubeconfig <path-to-kube-config> apply -f cluster-a.kubegateway.io.yaml
```

### 访问

接着就能通过 KubeGateway 的代理端口访问到对应的集群啦

```Shell
curl -k https://cluster-a.kubegateway.io:6443/api --resolve cluster-a.kubegateway.io:6443:127.0.0.1
```

## 高级配置

### Reuse Port

KubeGateway 支持通过 reuser port 的模式启动，这个模式下，可以在一个节点中启动多个 KubeGateway 的服务提高单节点的可靠性，通过添加启动参数 `--enable-reuse-port` 来实现。

需要注意的是，在 reuse port 的模式下，多个 KubeGateway 的 loopback client 会互相访问，所以需要额外设置一个参数 `--loopback-client-token="put-your-token-here"` 保证它们能互相认证。

### 流量控制

我们有三种流量控制方法，它们可以配置在 UpstreamCluster 的 `spec.flowControl.flowControlSchemas` 中，然后在 `spec.dispatchPolicies.flowControlSchemaName` 中被引用。

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

#### 完整的引用

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

### 路由

详见设计文档中的路由章节：TODO 链接

## 配置举例

### 读写分离

- 将所有 get, list, watch 请求分流到 ip-a
- 将其他请求分流到 ip-b

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

### 限制 list pod 的请求

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

### 限制 user-a 的请求速率

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
