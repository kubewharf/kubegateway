#!/usr/bin/env bash

# Copyright 2014 The Kubernetes Authors.
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

# Controls verbosity of the script output and logging.
VERBOSE="${VERBOSE:-5}"

# Log an error but keep going.  Don't dump the stack or exit.
kube::log::error() {
  timestamp=$(date +"[%m%d %H:%M:%S]")
  echo "!!! ${timestamp} ${1-}" >&2
  shift
  for message; do
    echo "    ${message}" >&2
  done
}

# Print out some info that isn't a top level status line
kube::log::info() {
  local V="${V:-0}"
  if [[ ${VERBOSE} < ${V} ]]; then
    return
  fi

  for message; do
    echo "${message}"
  done
}

# Print a status line.  Formatted to show up in a stream of output.
kube::log::status() {
  local V="${V:-0}"
  if [[ ${VERBOSE} < ${V} ]]; then
    return
  fi

  timestamp=$(date +"[%m%d %H:%M:%S]")
  echo "+++ ${timestamp} ${1}"
  shift
  for message; do
    echo "    ${message}"
  done
}
