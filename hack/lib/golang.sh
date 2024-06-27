#!/usr/bin/env bash

# Copyright 2024 The Kubernetes Authors.
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


# Prints the value that needs to be passed to the -ldflags parameter of go build
# in order to set the Kubernetes based on the git tree status.
# IMPORTANT: if you update any of these, also update the lists in
# pkg/version/def.bzl and hack/print-workspace-status.sh.
kube::version::ldflags() {
  local -a ldflags
  function add_ldflag() {
    local key=${1}
    local val=${2}
    # If you update these, also update the list component-base/version/def.bzl.
    ldflags+=(
      "-X 'k8s.io/client-go/pkg/version.${key}=${val}'"
      "-X 'k8s.io/component-base/version.${key}=${val}'"
    )
  }

  add_ldflag "buildDate" "$(date ${SOURCE_DATE_EPOCH:+"--date=@${SOURCE_DATE_EPOCH}"} -u +'%Y-%m-%dT%H:%M:%SZ')"
  if [[ -n ${GIT_COMMIT-} ]]; then
    add_ldflag "gitCommit" "${GIT_COMMIT}"
    add_ldflag "gitTreeState" "${GIT_TREE_STATE:-}"
  fi

  if [[ -n ${GIT_VERSION-} ]]; then
    add_ldflag "gitVersion" "${GIT_VERSION}"
  fi

  if [[ -n ${GIT_MAJOR-} && -n ${GIT_MINOR-} ]]; then
    add_ldflag "gitMajor" "${GIT_MAJOR:-}"
    add_ldflag "gitMinor" "${GIT_MINOR:-}"
  fi

  # The -ldflags parameter takes a single string, so join the output.
  echo "${ldflags[*]-}"
}

# Build binaries targets specified
#
# Input:
#   $@ - targets and go flags.  If no targets are set then all binaries targets
#     are built.
#   MAKE_RULES_GO_BUILD_PLATFORMS - Incoming variable of targets to build for.  If unset
#     then just the host architecture is built.
kube::golang::build_binaries() {
  # Create a sub-shell so that we don't pollute the outer environment
  (
    # Check for `go` binary and set ${GOPATH}.
    # kube::golang::setup_env
    V=2 kube::log::info "Go version: $(go version)"

    local host_platform
    host_platform="$(go env GOHOSTOS)/$(go env GOHOSTARCH)"

    export GOLDFLAGS="${GOLDFLAGS:-} $(kube::version::ldflags)"

    local -a targets=()
    local arg

    for arg; do
      targets+=("${arg}")
    done

    local -a platforms
    IFS=" " read -ra platforms <<< "${MAKE_RULES_GO_BUILD_PLATFORMS:-}"
    if [[ ${#platforms[@]} -eq 0 ]]; then
      platforms=("${host_platform}")
    fi

    for platform in "${platforms[@]}"; do
      kube::log::status "Building go targets for ${platform}:" "${targets[@]}"
      (
        export GOOS=${platform%/*}
        export GOARCH=${platform##*/}

        dir="bin/${platform//\//_}"
        mkdir -p "${dir}"

        # shellcheck disable=SC2164
        cd "${MAKE_RULES_WORKSPACE}"

        for target in "${targets[@]}"; do


            # pre-hook
            if [[ -f "${MAKE_RULES_WORKSPACE}/${target}/pre-build" ]]; then
              kube::log::status "Pre hook for ${target}:" "${target}/pre-build"
              bash "${MAKE_RULES_WORKSPACE}/${target}/pre-build"
            fi

            # build
            build_args=(
              ${GOFLAGS[@]:-}
              -gcflags "${GOGCFLAGS:-}"
              -asmflags "${GOASMFLAGS:-}"
              -ldflags "${GOLDFLAGS:-}"
              -tags "${GOTAGS:-}"
            )
            go_build_args="${build_args[@]}"

            bin=${target##*/}
            kube::log::status "Building go target for ${platform}:" "target: ${target}" "output: ${dir}/${bin}" "args: ${go_build_args}"
            go build "${build_args[@]}" -o "${dir}/${bin}" "./${target}"

            # post-hook
            if [[ -f "${MAKE_RULES_WORKSPACE}/${target}/post-build" ]]; then
              kube::log::status "Post hook for ${target}:" "${target}/post-build"
              bash "${MAKE_RULES_WORKSPACE}/${target}/post-build"
            fi
        done
      )
    done
  )
}
