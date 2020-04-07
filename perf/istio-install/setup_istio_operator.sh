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

set -ex

WD=$(dirname "$0")
WD=$(cd "$WD"; pwd)
DIRNAME="${WD}/tmp"
mkdir -p "${DIRNAME}"
export GO111MODULE=on

# Passing a tag, like latest or 1.4-dev
if [[ -n "${TAG:-}" ]]; then
  VERSION=$(curl -sL https://gcsweb.istio.io/gcs/istio-build/dev/"${TAG}")
  OUT_FILE="istio-${VERSION}"
  RELEASE_URL="https://storage.googleapis.com/istio-build/dev/${VERSION}/istio-${VERSION}-linux-amd64.tar.gz"
# Passing a dev version, like 1.4-alpha.41dee99277dbed4bfb3174dd0448ea941cf117fd
elif [[ -n "${DEV_VERSION:-}" ]]; then
  OUT_FILE="istio-${DEV_VERSION}"
  RELEASE_URL="https://storage.googleapis.com/istio-build/dev/${DEV_VERSION}/istio-${DEV_VERSION}-linux-amd64.tar.gz"
# Passing a version, like 1.4.2
elif [[ -n "${VERSION:-}" ]]; then
  OUT_FILE="istio-${VERSION}"
  RELEASE_URL="https://storage.googleapis.com/istio-prerelease/prerelease/${VERSION}/istio-${VERSION}-linux-amd64.tar.gz"
# Passing a release url, like https://storage.googleapis.com/istio-prerelease/prerelease/1.4.1/istio-1.4.1-linux-amd64.tar.gz
elif [[ -n "${RELEASE_URL:-}" ]]; then
  OUT_FILE=${OUT_FILE:-"$(basename "${RELEASE_URL}" -linux-amd64.tar.gz)"}
# Passing a gcs url, like gs://istio-build/dev/1.4-alpha.41dee99277dbed4bfb3174dd0448ea941cf117fd
elif [[ -n "${GCS_URL:-}" ]]; then
  DOWNLOAD_TYPE=gcs
  RELEASE_URL="${GCS_URL}"
  OUT_FILE=${OUT_FILE:-"$(basename "${RELEASE_URL}" -linux-amd64.tar.gz)"}
fi

if [[ -z "${RELEASE_URL:-}" ]]; then
  echo "Must set on of TAG, VERSION, DEV_VERSION, RELEASE_URL, GCS_URL"
  exit 2
fi

function download_release() {
  outfile="${DIRNAME}/${OUT_FILE}"
  if [[ ! -d "${outfile}" ]]; then
    tmp=$(mktemp -d)
    if [[ "${DOWNLOAD_TYPE:-}" == gcs ]]; then
      gsutil cp "${GCS_URL}" "${tmp}/out.tar.gz"
      tar xvf "${tmp}/out.tar.gz" -C "${DIRNAME}"
    else
      curl -fJLs -o "${tmp}/out.tar.gz" "${RELEASE_URL}"
      tar xvf "${tmp}/out.tar.gz" -C "${DIRNAME}"
    fi
  else
    echo "${outfile} already exists, skipping download"
  fi
}

function install_istioctl() {
  release=${1:?release folder}
  shift
  "${release}/bin/istioctl" manifest apply --skip-confirmation --wait "${@}"
}

function install_gateways() {
  local domain=${DNS_DOMAIN:-howardjohn.qualistio.org}
  helm template --set domain="${domain}" "${WD}/base" | kubectl -n istio-system apply -f -
}

function install_prom_op() {
  "$WD/setup_prometheus.sh" "${DIRNAME}"
}

download_release
install_istioctl "${DIRNAME}/${OUT_FILE}" "${@}"

if [[ -z "${SKIP_EXTRAS:-}" ]]; then
  install_prom_op
  install_gateways
fi
