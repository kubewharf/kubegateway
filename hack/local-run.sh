#!/bin/bash

# Copyright 2022 ByteDance and its affiliates.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

output/kube-gateway \
    --etcd-servers=http://localhost:2379 \
    --secure-port=9443 \
    --etcd-prefix=/registry/KubeGateway \
    --proxy-secure-ports=6443 \
    --authorization-mode AlwaysAllow \
    --v=5 \
    --log-dir=output \
    --logtostderr=false \
    --enable-proxy-access-log=true \
    --enable-reuse-port \
    --loopback-client-token="kube-gateway-privileged"
