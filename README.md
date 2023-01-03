# KubeGateway

English | [简体中文](README.zh_CN.md)

## Overview

Kube-gateway is a best practice for managing massive kubernetes clusters within ByteDance.

It is a layer 7 load balancing proxy specifically designed and customized for HTTP2 flow for kube-apiserver.

The goal is to provide flexible and stable flow governance solutions for massive large-scale kubernetes clusters (more than 1,000 nodes).

## Features

In terms of traffic governance:

- It proactively performs request-level load balancing for multiple kube-apiservers;
- It provides kube-apiserver with routing rules customized for flow characteristics. It can distinguish requests through verb, apiGroup, resource, user, userGroup, serviceAccounts, nonResourceURLs and other information, and perform differentiated forwarding. It also has flow governance functions such as limited flow, degradation, and fuse;
- It converges the number of TCP connections on a single kube-apiserver instance by at least an order of magnitude;
- Its configuration, such as routing, takes effect immediately without restarting the service.

In terms of massive cluster proxies：

- It is able to dynamically add and remove proxy support for new clusters;
- It provides different TLS certificates and ClientCA for different clusters;
- It provides allow/disable list, monitoring alarm, fuse and other functions.

## Detailed Doc

- [Design documentation](docs/en/design.md)
- [Manually Setup](docs/en/manually-setup.md)
- [Develop Guide](docs/en/quick_start.md)
- [Performance testing](docs/en/benchmark.md)

## Contributing

Please refer to [Contributing](CONTRIBUTING.md)

## Code of Conduct

Please refer to [Code of Conduct](CODE_OF_CONDUCT.md) for more details.

## Contact Us

Please refer to [Maintainers](MAINTAINERS.md)

## Security

If you find a potential security issue in this project, or think you may have discovered a security issue.

We hope you notify Bytedance Security via our [Security Center](https://security.bytedance.com/src) or [Vulnerability Report Email](sec@bytedance.com).

Please **do not** create a public GitHub issue.

## License

This project follows [Apache-2.0 License](LICENSE).
