#!/bin/bash

set -e

function set_connection_duration() {
  # TERMINATION_DRAIN_DURATION_SECONDS is not easily accessible currently as it must be manually added to the sidecar injector configmap.
  # If this is set to custom value, we should require the duration value to be set to match it. Otherwise, we default to 4s.

  termination=$(kubectl -n istio-system get configmap istio-sidecar-injector -o jsonpath='{.data.config}')

  if $(echo ${termination} | grep -q TERMINATION_DRAIN_DURATION_SECONDS); then
    connectionDuration=${DURATION:?"If a custom TERMINATION_DRAIN_DURATION_SECONDS is set, then DURATION must be provided. This should be set to a value slight (1s) below the termination duration."}
  else
    connectionDuration=4 # Default drain duration is 5 seconds
  fi;
}

function install_all_config() {
  local DIRNAME="${1:?"output dir"}"
  local OUTFILE="${DIRNAME}/all_config.yaml"

  kubectl create ns graceful-shutdown || true

  kubectl label namespace graceful-shutdown istio-injection=enabled --overwrite || true

  set_connection_duration

  helm -n graceful-shutdown template . --set connectionDuration=${connectionDuration:?} > "${OUTFILE}"

  if [[ -z "${DRY_RUN}" ]]; then
      kubectl -n graceful-shutdown apply -f "${OUTFILE}"
  fi
}

WD=$(dirname $0)
WD=$(cd $WD; pwd)
mkdir -p "${WD}/tmp"

install_all_config "${WD}/tmp" $*
