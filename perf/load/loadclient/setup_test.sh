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

# shellcheck disable=SC2086
WD=$(dirname $0)
WD=$(cd "${WD}"; pwd)
cd "${WD}"

set -ex

NAMESPACE=${1:?"namespace"}
NAMEPREFIX=${2:?"prefix name for service. typically svc-"}


# Additional customization option for load client, e.g. "--set qps=200"
# LOADCLIENT_EXTRA_HELM_FLAGS=${LOADCLIENT_EXTRA_HELM_FLAGS:-""}


function run_test() {
  YAML=$(mktemp).yml
  # shellcheck disable=SC2086
  helm -n ${NAMESPACE} template \
	  --set serviceHost="${SERVICEHOST}" \
    --set Namespace="${NAMESPACE}" \
    --set ingress="${NAMEPREFIX}0.${NAMESPACE}:8080" \
    --set domain="${DNS_DOMAIN}" \
    --set https="false" \
    ${LOADCLIENT_EXTRA_HELM_FLAGS} \
          . > "${YAML}"
  echo "Wrote ${YAML}"

  if [[ -z "${DELETE}" ]];then
    kubectl create ns "${NAMESPACE}" || true
    kubectl label namespace "${NAMESPACE}" istio-injection=enabled --overwrite
    kubectl label namespace "${NAMESPACE}" istio-env=istio-control --overwrite
    sleep 5
    kubectl -n "${NAMESPACE}" apply -f "${YAML}"
  else
    kubectl -n "${NAMESPACE}" delete -f "${YAML}"
  fi
}

run_test
