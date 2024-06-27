#!/usr/bin/env bash

# Copyright 2024 ByteDance and its affiliates.
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

export MAKE_RULES_WORKSPACE=$(dirname "${BASH_SOURCE[0]}")/../..
export MAKE_RULES_GO_BUILD_PLATFORMS=${GO_BUILD_PLATFORMS-""}

VERBOSE="${VERBOSE:-1}"
if [[ -n ${VERSION-} ]]; then
  GIT_VERSION="${VERSION}"
fi


source "${MAKE_RULES_WORKSPACE}/hack/lib/golang.sh"
source "${MAKE_RULES_WORKSPACE}/hack/lib/logging.sh"

bash "${MAKE_RULES_WORKSPACE}/hack/hooks/pre-build"

kube::golang::build_binaries "$@"

bash "${MAKE_RULES_WORKSPACE}/hack/hooks/post-build"