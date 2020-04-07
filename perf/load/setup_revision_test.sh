#!/bin/bash

# Copyright Istio Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#    http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# This script spins up the standard 20 services per namespace test for as many namespaces
# as desired.
# shellcheck disable=SC2086
WD=$(dirname $0)
WD=$(cd "${WD}"; pwd)
cd "${WD}"

set -ex

# shellcheck disable=SC1091
source common.sh

NUM=${1:?"number of namespaces. 20 x this number"}
START=${2:-"0"}

function start_servicegraphs() {
  local nn=${1:?"number of namespaces"}
  local min=${2:-"0"}

   # shellcheck disable=SC2004
   for ((ii=$min; ii<$nn; ii++)) {
      ns=$(printf 'service-graph%.2d' $ii)
      prefix=$(printf 'svc%.2d-' $ii)
      export INJECTION_LABEL="istio.io/rev=${ns}"
      kubectl label namespace $ns --overwrite istio-injection-
      SKIP_EXTRAS=true "${WD}/../istio-install/setup_istio_operator.sh" -f revision.yaml --set revision="${ns}"
      ${CMD} run_test "${ns}" "${prefix}"
      ${CMD} "${WD}/loadclient/setup_test.sh" "${ns}" "${prefix}"
      sleep 30
  }
}

# Initial install, to get CRDs, etc
"${WD}/../istio-install/setup_istio_operator.sh" -f base.yaml
start_servicegraphs "${NUM}" "${START}"
