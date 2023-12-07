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

set -o errexit
set -o nounset
set -o pipefail

readonly BASE_SOURCE_ROOT="$(cd "$(dirname "${BASH_SOURCE}")/.." && pwd -P)"
source "${BASE_SOURCE_ROOT}"/hack/lib/pki.sh
source "${BASE_SOURCE_ROOT}"/hack/lib/utils.sh

readonly TEST_DATA_PATH="${BASE_SOURCE_ROOT}"/hack/testdata
readonly TEMP_DIR="${BASE_SOURCE_ROOT}"/output/pki
readonly UPSTREAM_CONF_DIR="${TEMP_DIR}"/upstream
readonly GATEWAY_CONF_DIR="${TEMP_DIR}"/gateway
readonly kind_cluster_name="kubegateway"
readonly context_name="kind-${kind_cluster_name}"

ensure_kind_cluster() {
    echo ">> ensure kind cluster ${kind_cluster_name}"
    if kind get clusters | grep "${kind_cluster_name}"; then
        # clean up
        if kubectl --context="${context_name}" get statefulset kubegateway; then
            kubectl --context="${context_name}" delete statefulset kubegateway
        fi
    else
        kind create cluster --name=${kind_cluster_name}
    fi
}

get_upstream_pki_kind() {
    [ -d "${UPSTREAM_CONF_DIR}" ] || mkdir -p "${UPSTREAM_CONF_DIR}"

    echo ">> get upstream pki fron kind, output path ${UPSTREAM_CONF_DIR}"

    local kind_docker=$(docker ps --filter "name=${kind_cluster_name}-control-plane" --format "{{.ID}}")

    docker cp "${kind_docker}":/etc/kubernetes/pki/ca.crt "${UPSTREAM_CONF_DIR}"/ca.crt
    docker cp "${kind_docker}":/etc/kubernetes/pki/ca.key "${UPSTREAM_CONF_DIR}"/ca.key
    docker cp "${kind_docker}":/etc/kubernetes/pki/apiserver.crt "${UPSTREAM_CONF_DIR}"/apiserver.crt
    docker cp "${kind_docker}":/etc/kubernetes/pki/apiserver.key "${UPSTREAM_CONF_DIR}"/apiserver.key
    yq eval '.users.[]|select(.name=="'${context_name}'")|.user.client-certificate-data' ~/.kube/config | base64 \
        --decode >"${UPSTREAM_CONF_DIR}"/client.crt
    yq eval '.users.[]|select(.name=="'${context_name}'")|.user.client-key-data' ~/.kube/config | base64 \
        --decode >"${UPSTREAM_CONF_DIR}"/client-key.crt

}

gen_gateway_pki() {
    [ -d "${GATEWAY_CONF_DIR}" ] || mkdir -p "${GATEWAY_CONF_DIR}"
    # generate ca
    util::pki::gen_ca "${GATEWAY_CONF_DIR}"
    # generate kubegateway control plane server key and cert
    echo ">> gen gateway control plane server cert"
    gen_gateway_server_cert
    # generate kubegateway control plane client key and cert
    echo ">> gen gateway control plane admin client cert"
    gen_gateway_client_cert
    # generate upstream kubegateway user client
    echo ">> gen gateway control plane admin client cert"
    gen_upstream_gateway_client_cert
}

gen_gateway_server_cert() {
    KUBERNETES_HOSTNAMES=kubernetes,kubernetes.default,kubernetes.default.svc,kubernetes.default.svc.cluster,kubernetes.svc.cluster.local,localhost,host.minikube.internal

    cat >"${GATEWAY_CONF_DIR}"/kubegateway-csr.json <<EOF
{
  "CN": "kubegateway",
  "key": {
    "algo": "rsa",
    "size": 2048
  },
  "names": [
    {
      "C": "US",
      "L": "Sunnyvale",
      "O": "KubeWharf",
      "OU": "KubeWharf",
      "ST": "CA"
    }
  ]
}
EOF
    cd "${GATEWAY_CONF_DIR}"
    cfssl gencert \
        -ca="${GATEWAY_CONF_DIR}"/ca.pem \
        -ca-key="${GATEWAY_CONF_DIR}"/ca-key.pem \
        -config="${GATEWAY_CONF_DIR}"/ca-config.json \
        -hostname=127.0.0.1,${KUBERNETES_HOSTNAMES} \
        -profile=kubernetes \
        "${GATEWAY_CONF_DIR}"/kubegateway-csr.json | cfssljson -bare kubegateway
    cd -
}

gen_gateway_client_cert() {
    cat >"${GATEWAY_CONF_DIR}"/admin-csr.json <<EOF
{
  "CN": "admin",
  "key": {
    "algo": "rsa",
    "size": 2048
  },
  "names": [
    {
      "C": "US",
      "L": "Sunnyvale",
      "O": "system:masters",
      "OU": "KubeWharf",
      "ST": "CA"
    }
  ]
}
EOF
    cd "${GATEWAY_CONF_DIR}"
    cfssl gencert \
        -ca="${GATEWAY_CONF_DIR}"/ca.pem \
        -ca-key="${GATEWAY_CONF_DIR}"/ca-key.pem \
        -config="${GATEWAY_CONF_DIR}"/ca-config.json \
        -profile=kubernetes \
        "${GATEWAY_CONF_DIR}"/admin-csr.json | cfssljson -bare admin
    cd -
}

gen_upstream_gateway_client_cert() {
    cat >"${GATEWAY_CONF_DIR}"/upstream-gateway-client.json <<EOF
{
  "CN": "kubegateway-upstream-client",
  "key": {
    "algo": "rsa",
    "size": 2048
  },
  "names": [
    {
      "C": "US",
      "L": "Sunnyvale",
      "O": "KubeWharf",
      "OU": "KubeWharf",
      "ST": "CA"
    }
  ]
}
EOF
    cd "${GATEWAY_CONF_DIR}"
    # use upstream ca generate kubegateway client key and cert
    cfssl gencert \
        -ca="${UPSTREAM_CONF_DIR}"/ca.crt \
        -ca-key="${UPSTREAM_CONF_DIR}"/ca.key \
        -config="${GATEWAY_CONF_DIR}"/ca-config.json \
        -profile=kubernetes \
        "${GATEWAY_CONF_DIR}"/upstream-gateway-client.json | cfssljson -bare kubegateway-upstream-client
    cd -
}

publish_secret_and_context() {
    echo ">> create gateway-control-plane context"
    # create kubegateway control plane context
    kubectl config set-cluster gateway-control-plane \
        --certificate-authority="${GATEWAY_CONF_DIR}"/ca.pem \
        --server=https://127.0.0.1:9443

    kubectl config set-credentials gateway-control-plane-admin \
        --client-certificate="${GATEWAY_CONF_DIR}"/admin.pem \
        --client-key="${GATEWAY_CONF_DIR}"/admin-key.pem

    kubectl config set-context gateway-control-plane \
        --cluster=gateway-control-plane \
        --user=gateway-control-plane-admin

    # create kubegateway proxy context
    echo ">> create gateway-proxy context"
    kubectl config set-cluster gateway-proxy \
        --certificate-authority="${UPSTREAM_CONF_DIR}"/ca.crt \
        --server=https://localhost:8443

    kubectl config set-context gateway-proxy \
        --cluster=gateway-proxy \
        --user="${context_name}"

    # create secret
    echo ">> create gateway-sever-pki secret"
    if kubectl --context "${context_name}" get secret gateway-sever-pki; then
        kubectl --context "${context_name}" delete secret gateway-sever-pki
    fi
    kubectl create secret generic gateway-sever-pki \
        --from-file=ca-key.pem="${GATEWAY_CONF_DIR}"/ca-key.pem \
        --from-file=ca.pem="${GATEWAY_CONF_DIR}"/ca.pem \
        --from-file=kubegateway-key.pem="${GATEWAY_CONF_DIR}"/kubegateway-key.pem \
        --from-file=kubegateway.pem="${GATEWAY_CONF_DIR}"/kubegateway.pem

}

ensure_kind_cluster

# get and generate certs
get_upstream_pki_kind
gen_gateway_pki
publish_secret_and_context

# build binary and container
docker build --build-arg VERSION=local-up -f build/kube-gateway/Dockerfile . -t kube-gateway:local-up
kind load docker-image --name="${kind_cluster_name}" kube-gateway:local-up

kubectl --context "${context_name}" apply -f "${TEST_DATA_PATH}"/local-up.yaml

os=$(kube::util::host_os)

if [[ ${os} == "darwin" ]]; then
    sed -e "s/<client-ca>/$(base64 -i ${UPSTREAM_CONF_DIR}/ca.crt)/g" \
        -e "s/<client-key>/$(base64 -i ${GATEWAY_CONF_DIR}/kubegateway-upstream-client-key.pem)/g" \
        -e "s/<client-cert>/$(base64 -i ${GATEWAY_CONF_DIR}/kubegateway-upstream-client.pem)/g" \
        -e "s/<serving-client-ca>/$(base64 -i ${UPSTREAM_CONF_DIR}/ca.crt)/g" \
        -e "s/<serving-key>/$(base64 -i ${UPSTREAM_CONF_DIR}/apiserver.key)/g" \
        -e "s/<serving-cert>/$(base64 -i ${UPSTREAM_CONF_DIR}/apiserver.crt)/g" \
        "${TEST_DATA_PATH}"/localhost.yaml.tmpl >"${GATEWAY_CONF_DIR}"/localhost.yaml
elif [[ ${os} == "linux" ]]; then
    sed -e "s/<client-ca>/$(base64 -w 0 ${UPSTREAM_CONF_DIR}/ca.crt)/g" \
        -e "s/<client-key>/$(base64 -w 0 ${GATEWAY_CONF_DIR}/kubegateway-upstream-client-key.pem)/g" \
        -e "s/<client-cert>/$(base64 -w 0 ${GATEWAY_CONF_DIR}/kubegateway-upstream-client.pem)/g" \
        -e "s/<serving-client-ca>/$(base64 -w 0 ${UPSTREAM_CONF_DIR}/ca.crt)/g" \
        -e "s/<serving-key>/$(base64 -w 0 ${UPSTREAM_CONF_DIR}/apiserver.key)/g" \
        -e "s/<serving-cert>/$(base64 -w 0 ${UPSTREAM_CONF_DIR}/apiserver.crt)/g" \
        "${TEST_DATA_PATH}"/localhost.yaml.tmpl >"${GATEWAY_CONF_DIR}"/localhost.yaml
else
      kube::log::info "${os} is NOT supported."
fi

while ! kubectl --context "${context_name}" get pod kubegateway-0 | grep "Running" >/dev/null; do
    echo ">> waiting for kubegateway server running, sleep 5s"
    sleep 5
done

{
    sleep 5
    kubectl --context gateway-control-plane apply -f "${GATEWAY_CONF_DIR}"/localhost.yaml
    echo "
Congratulations !!

You can now use gateway-control-plane context to connect gateway control plane:

    kubectl --context gateway-control-plane api-resources

You can now use gateway-proxy context to connect upstream cluster:

    kubectl --context gateway-proxy api-resources

Have a nice day! 👋
"
} &

# wait here
kubectl --context "${context_name}" port-forward svc/kubegateway 9443:9443 8443:8443
