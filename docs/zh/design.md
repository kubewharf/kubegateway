# 设计文档

## 名词解释

| 名词               | 含义                                                         |
| ------------------ | ------------------------------------------------------------ |
| Upstream APIServer | 被 KubeGateway 代理的 APIServer 都被称为 Upstream APIServer  |
| Upstream Cluster   | 相同的几个 APIServer 组成的 Group 被称为 Upstream Cluster    |
| Endpoint           | 一个 Upstream APIServer 的地址，格式为 schema://host:port，比如 https://192.168.0.1:6443 |
| impersonate        | Kubernetes APIServer 提供的一种特殊机制，允许让某个用户模拟成另外一个用户来进行请求操作 |

## 架构设计

在架构上，分为两个层面

- 控制面：完整的 kube-apiserver 逻辑链路，对外提供代理集群和路由的 CRUD 操作

- 代理：代理通往 upstream cluster 的流量，提供部分 authN/Z 的功能，提供灵活的路由匹配规则，提供灵活的限流规则

### 控制面架构设计

![img](../image/design_control_plane.png)

KubeGateway 的控制面等同于一个完整的 kube-apiserver

1. 拥有健全的 Authentication 和 Authorization
2. 提供了对 proxy rules 等控制面资源的 CRUD 的操作
3. 支持 list/watch 的操作，即可以使用 client-go 直接进行配置变更，不需要额外的 SDK

### 代理层架构设计

![img](../image/design_proxy.png)

在代理层

- 请求经过认证模块后，判断出请求的用户信息，如果必要的话，还会做授权验证
- 然后经过路由匹配模块，来判断请求应该转发到那些 upstream kube-apiserver
- 请求经过流量治理模块
  - 判断是否对它进行限流
  - 根据轮询调度（RoundRobin）的负载均衡算法选择一个合适的 kube-apiserver 作为转发对象
- 将请求通过预先创建好的 HTTP2 的链接转发到后端的 kube-apiserver

## 控制面详细设计

###  API

```Go
// UpstreamClusterSpec defines the desired state of UpstreamCluster
type UpstreamClusterSpec struct {
    // Servers contains a group of upstream api servers
    Servers []UpstreamClusterServer `json:"servers,omitempty" protobuf:"bytes,1,rep,name=servers"`

    // Client connection config for upstream api servers
    ClientConfig ClientConfig `json:"clientConfig,omitempty" protobuf:"bytes,2,opt,name=clientConfig"`

    // Secures serving config for upstream cluster
    SecureServing SecureServing `json:"secureServing,omitempty" protobuf:"bytes,3,opt,name=secureServing"`

    // Client flow control settings, e.g. qps and burst
    FlowControl FlowControl `json:"flowControl,omitempty" protobuf:"bytes,4,opt,name=flowControl"`

    // DispatchPolicies describes how to dispatch requests to upsteams.
    // Only one dispatch policy will be matched
    // The router will follow the order of DispatchPolicies, that means
    // the previous policy has higher priority
    DispatchPolicies []DispatchPolicy `json:"dispatchPolicies,omitempty" protobuf:"bytes,5,rep,name=dispatchPolicies"`
}
```

我们设计了一个 UpstreamCluster 的资源用来表示一个需要被 KubeGateway 代理的后端集群，它包含了

- 后端集群列表
- KubeGateway 访问后端集群的客户端信息
- Server 证书和 CA 信息 用于提供动态的 SNI
- 流量控制信息
- 路由

API 更多的内容请查看链接：TODO 添加链接

### ClientConfig Permissions

在配置 upstream cluster 的 clientConfig 的时候，需要给 kube-gateway 足够的权限来访问 upstream cluster，使之能正常工作，kube-gateway 访问 upstream 主要有三种行为

1. 健康检查
2. Authentication Request (TokenReview) and Authorization Request (SubjectReview)
3. 给 upstream 发请求，impersonate 成另外一个用户

所以它需要的最小权限如下

```YAML
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: kube-gateway
rules:
- apiGroups: [""]
  resources: ["users", "groups", "serviceaccounts"]
  verbs: ["impersonate"]
# Can set "Impersonate-Extra-scopes" header and the "Impersonate-Uid" header.
- apiGroups: ["authentication.k8s.io"]
  resources: ["userextras/scopes", "uids"]
  verbs: ["impersonate"]
- apiGroups: ["authentication.k8s.io"]
  resources: ["tokenreviews", subjectaccessreviews]
  verbs: ["create"]
- nonResourceURLs: ["/healthz*", "/readyz*", "/livez*"]
  verbs: ["get"]
```

然后和 kube-gateway 的 user 绑定

```YAML
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: kube-gateway
subjects:
- kind: User
  name: kube-gateway # This should be your client config user name
  apiGroup: rbac.authorization.k8s.io
roleRef:
  kind: ClusterRole
  name: kube-gateway
  apiGroup: rbac.authorization.k8s.io
```

### FlowControl

目前提供三种流量控制的方法

- Exempt: 表示不限制
- MaxRequestsInflight: 表示限制最大并发数，这个最大并发数跟 qps 不同，它表示同时可以有多少个请求在等待被处理
- TokenBucket: 通过令牌捅来限制请求数量，允许 burst

## 代理层的详细设计

### 路由

在 UpstreamCluster API 中每一个 DispatchPolicy 代表一个路由规则，多个路由规则的生效优先级由列表的先后顺序决定

#### DispatchPolicy API 定义

```Go
type DispatchPolicy struct {
    // Specifies a load balancing method for a server group
    Strategy Strategy `json:"strategy,omitempty" protobuf:"bytes,1,opt,name=strategy,casttype=Strategy"`
    // UpsteamSubset indacates to the list of upstream endpoints. An empty set
    // means use all upstreams
    // +optional
    UpsteamSubset []string `json:"upsteamSubset,omitempty" protobuf:"bytes,2,rep,name=upsteamSubset"`
    // Rules holds all the DispatchPolicyRules for this policy
    // Gateway matches rules according to the list order, the previous rules
    // take higher precedence
    // +optional
    Rules []DispatchPolicyRule `json:"rules,omitempty" protobuf:"bytes,3,rep,name=rules"`
    // FlowControlSchemaName indicates to which flow control schema in spec.FlowControl will
    // take effect on this prolicy
    // If not set, there is no limit
    FlowControlSchemaName string `json:"flowControlSchemaName,omitempty" protobuf:"bytes,4,opt,name=flowControlSchemaName"`
}
```

- Rules 为路由规则，采用了与 rbac 的 PolicyRule 类似的语法
- Strategy 表示这个 Policy 命中后，应该用什么策略来选择其中一台 Upstream，目前只提供 RoundRobin
- UpstreamSubset 如果为空，则命中这个 Policy 之后，将从所有的 Servers 中选取 Endpoint，否则从这个 Subset 中选取
- FlowControlSchemaName 表示这个 policy 需要遵循哪个 flowControlSchema 的规则
路由匹配规则 API 如下，每个字段之间是『与 &&』的关系，而多个 PolicyRule 是 『或 ||』的关系

```Go
// DispatchPolicyRule holds information that describes a policy rule
type DispatchPolicyRule struct {
     // Verbs is a list of Verbs that apply to ALL the ResourceKinds and AttributeRestrictions contained in this rule.
    // - "*" represents all Verbs.
    // - An empty set mains that nothing is allowed.
    // - use '-' prefix to invert verbs matching, e.g. "-get" means match all verbs except "get"
    Verbs []string `json:"verbs,omitempty" protobuf:"bytes,1,rep,name=verbs"`

    // APIGroups is the name of the APIGroup that contains the resources.  If multiple API groups are specified, any action requested against one of
    // the enumerated resources in any API group will be allowed.
    // - "*" represents all APIGroups.
    // - An empty set mains that nothing is allowed.
    // - use '-' prefix to invert apiGroups matching, e.g. "-apps" means match all apiGroups except "apps"
    APIGroups []string `json:"apiGroups,omitempty" protobuf:"bytes,2,rep,name=apiGroups"`

    // Resources is a list of resources this rule applies to.
    // - "*" represents all Resources.
    // - An empty set mains that nothing is allowed.
    // - use "{resource}/{subresource}" to match one resource's subresource
    // - use "*/{subresource}" to match all resources' subresource, but "{resource}/*" is not allowed.
    // - use '-' prefix to invert resources matching, e.g. "-deployments" means match all resources except "deployments".
    Resources []string `json:"resources,omitempty" protobuf:"bytes,3,rep,name=resources"`

    // ResourceNames is an optional white list of names that the rule applies to.
    // - "*" represents all ResourceNames.
    // - An empty set means that everything is allowed.
    // - use '-' prefix to invert resourceNames matching, e.g. "-nginx" means match all resourceNames except "nginx"
    // +optional
    ResourceNames []string `json:"resourceNames,omitempty" protobuf:"bytes,4,rep,name=resourceNames"`

    // Users is a list of users this rule applies to.
    // - "*" represents all Users.
    // - if ServiceAccounts is empty, an empty set means that everything is allowed, otherwise it means nothing is allowed.
    // use '-' prefix to invert users matching, e.g. "-admin" means match all users except "admin"
    // +optional
    Users []string `json:"users,omitempty" protobuf:"bytes,5,rep,name=users"`

    // ServiceAccounts is a list of serviceAccont users this rule applies to. It can be covered if Users matches all.
    // - ServiceAccounts can not use invert matching
    // - if Users is empty, an empty set mains that nothing is allowed, otherwise it means nothing is allowed.
    // - serviceAccount name and namespace must be set
    // +optional
    ServiceAccounts []ServiceAccountRef `json:"serviceAccounts,omitempty" protobuf:"bytes,6,rep,name=serviceAccounts"`

    // UserGroups is a list of users groups this rule applies to. UserGroupAll represents all user groups.
    // - "*" represents all UserGroups.
    // - An empty set means that everything is allowed.
    // - use '-' prefix to invert userGroups matching, e.g. "-system:controllers" means match all userGroups except "system:controllers"
    // +optional
    UserGroups []string `json:"userGroups,omitempty" protobuf:"bytes,7,rep,name=userGroups"`

    // NonResourceURLs is a set of partial urls that a user should have access to.  *s are allowed, but only as the full, final step in the path
    // Since non-resource URLs are not namespaced, this field is only applicable for ClusterRoles referenced from a ClusterRoleBinding.
    // Rules can either apply to API resources (such as "pods"or "secrets") or non-resource URL paths (such as "/api"),  but not both.
    // - "*" represents all NonResourceURLs.
    // - An empty set mains that nothing is allowed.
    // - NonResourceURLs can not use invert matching
    NonResourceURLs []string `json:"nonResourceURLs,omitempty" protobuf:"bytes,8,rep,name=nonResourceURLs"`
}
```

#### Rule 的基本规则

每个字段的基本规则如下

| 字段             | 资源请求是否必填 | 非资源请求是否必填 | 是否支持反选 | 是否支持"*"全选 | 其他规定                                                     |
| ---------------- | ---------------- | ------------------ | ------------ | --------------- | ------------------------------------------------------------ |
| verbs            | 是               | 是                 | 是           | 是              | [HTTP method 与 verbs 的对应关系](https://kubernetes.io/docs/reference/access-authn-authz/authorization/#determine-the-request-verb) |
| apiGroups        | 是               | 否                 | 是           | 是              |                                                              |
| resources        | 是               | 否                 | 是           | 是              | 使用 `{resource}/{subresource}` 来表示某个资源的子资源使用 `*/{subresource}` 来表示所有资源的某种子资源不允许使用 `{resource}/*` 来匹配某个资源的所有子资源 |
| users            | 否               | 否                 | 是           | 是              | 当 ServiceAccounts 为空时，users 为空表示匹配所有的 users，否则表示不匹配所有 users |
| userGroups       | 否               | 否                 | 是           | 是              |                                                              |
| serviceAcccounts | 否               | 否                 | 否           | 否              | 当 Users 为空时，ServiceAccounts 为空表示匹配所有 serviceAccounts ，否则表示不匹配 serviceAccountsserviceAccouts 是特殊的 user+group，也可以直接在 users 中表示 serviceAccounts 的 user name，或者在 userGroups 中匹配一组 serviceAcccounts |
| nonResourceURLs  | 否               | 是                 | 否           | 是              | 支持 `{path}/*` 做前缀匹配                                   |

#### 匹配所有

通过通配符 "*" 来匹配所有，当存在匹配所有的通配符时，其他任何单独匹配都会被忽略

比如下面可以匹配对 pods 的所有操作

```YAML
...
spec:
  dispatchPolicies:
  - rules:
    - resources: ["pods"]
      apiGroups: ["*"]
      verbs: ["*"]
```

需要注意的是在 NonResourceURLs 中 "*" 可以用来匹配 suffix

```YAML
...
spec:
  dispatchPolicies:
  - rules:
    - nonResourceURLs: ["/healthz", "/healthz/*"] # '*' in a nonResourceURL is a suffix glob match
      verbs: ["get", "post"]
```

#### 反选

除了 nonResourceURLs 和 ServiceAccounts 之外，其他的字段都可以在字符串前面加入 "-" 来表示反选，注意一旦使用了反选，则里面所有的元素都应该是反选的，即正选和反选不能同时存在。

比如下面的这个规则用来匹配所有非 pods 和 deployments 的请求

```YAML
...
spec:
  dispatchPolicies:
  - rules:
    - resources: ["-pods", "-deployments"]
      apiGroups: ["*"]
      verbs: ["*"]
```

下面这是一个错误的例子，在实际流程中 "-pods" 会被忽略，这条规则会变为匹配所有 deployments 的请求

```YAML
...
spec:
  dispatchPolicies:
  - rules:
    - resources: ["-pods", "deployments"]
      apiGroups: ["*"]
      verbs: ["*"]
```

### APIServer 链接收敛

在 user impersonation 技术的加持下，kube-gateway 访问 kube-apiserver 使用了固定的 HTTP2 客户端，kube-gateway 的代理转发请求也会通过这个客户端发送并且不会丢失用户信息，从而使得它天然地能够使用 HTTP2 多路复用的能力，即在同一个 TCP 上发送多个请求。

所以当集群规模变大的时候，如达到万级 node，每个 kube-apiserver 原本需要维护的 TCP 连接数由几千降低到几十

![img](../image/design_http2.png)
