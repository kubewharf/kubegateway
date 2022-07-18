# KubeGateway

[English](README.md) | 中文

## 概述

kube-gateway 是字节跳动内部管理海量 kubernetes 集群的最佳实践。
它是为 kube-apiserver 的 HTTP2 流量专门设计并定制的七层负载均衡代理。
目标是为海量的大规模 kubernetes 集群（千级 node 以上）提供灵活的稳定的流量治理方案。

## 特点

在流量治理方面
-  它主动地为多个 kube-apiserver 进行请求级别的负载均衡
-  它为 kube-apiserver 提供流量特征定制的路由规则，可以通过 verb，apiGroup，resource，user，userGroup，serviceAccounts，nonResourceURLs 等信息来区分请求，进行差异化转发，并具备限流、降级、熔断等流量治理能力
-  它能收敛单个 kube-apiserver 实例上的 TCP 连接数，降低至少一个数量级
-  它的路由等配置实时生效而不需要重启服务
在海量集群代理方面
- 能够动态地添加和删除对新集群的代理支持
- 为不同集群动态提供不同的 TLS 证书以及 ClientCA
- 提供允许/禁用清单、监控报警、熔断等功能

## 详细文档
- [设计文档](docs/zh/design.md)
- [快速开始](docs/zh/quick_start.md)
- [性能测试](docs/zh/benchmark.md)

## 贡献代码

请查看 [Contributing](CONTRIBUTING.md) 

## 行为准则

请查看 [Code of Conduct](CODE_OF_CONDUCT.md) .

## 联系我们
请查看 [Maintainers](MAINTAINERS.md)

## 安全问题

如果您在本项目中发现了一个潜在的安全问题，或者认为您可能已经发现了一个安全问题。
我们要求您通过我们的[安全中心](https://security.bytedance.com/src)或[漏洞报告电子邮件](sec@bytedance.com)通知Bytedance Security。

请**不要**创建一个公开的GitHub问题。

## 开源许可

本项目遵循 [Apache-2.0 License](LICENSE).
