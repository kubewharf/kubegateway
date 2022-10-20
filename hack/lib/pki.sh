#!/bin/bash

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

util::pki::gen_ca() {
    local dir=$1
    if [[ -z "${dir}" ]]; then
        echo "you must provide a dir for ca generation"
        exit 1
    fi

    [[ -d ${dir} ]] || mkdir -p "${dir}"

    cat >"${dir}"/ca-config.json <<EOF
{
  "signing": {
    "default": {
      "expiry": "8760h"
    },
    "profiles": {
      "kubernetes": {
        "usages": ["signing", "key encipherment", "server auth", "client auth"],
        "expiry": "8760h"
      }
    }
  }
}
EOF

    cat >"${dir}"/ca-csr.json <<EOF
{
  "CN": "Kubernetes",
  "key": {
    "algo": "rsa",
    "size": 2048
  },
  "names": [
    {
      "C": "US",
      "L": "Sunnyvale",
      "O": "KubeWharf",
      "OU": "CA",
      "ST": "CA"
    }
  ]
}
EOF
    cd "${dir}" || exit 1
    cfssl gencert -initca "${dir}"/ca-csr.json | cfssljson -bare ca
    cd - || exit 1
}
