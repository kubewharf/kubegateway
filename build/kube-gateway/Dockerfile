# Copyright 2022 ByteDance and its affiliates.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

FROM --platform=${BUILDPLATFORM} golang:1.17 as builder
ADD . /build
ARG TARGETOS TARGETARCH VERSION
WORKDIR /build/
RUN VERSION=${VERSION} make build WHAT=cmd/kube-gateway GO_BUILD_PLATFORMS=${TARGETOS}/${TARGETARCH}

FROM --platform=${TARGETPLATFORM} debian:stable
ARG TARGETOS TARGETARCH
COPY --from=builder /build/bin/${TARGETOS}_${TARGETARCH}/kube-gateway /usr/local/bin/kube-gateway
ENTRYPOINT ["/usr/local/bin/kube-gateway"]
