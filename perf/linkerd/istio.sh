#!/bin/bash

set -eux

tmp=$(mktemp -d)
sha=9727308b3dadbfc8151cf70a045d1c7c52ab222b
cd $tmp
curl -sL https://storage.googleapis.com/istio-build/dev/1.5-alpha.$sha/istio-1.5-alpha.$sha-linux.tar.gz | tar xz
istioctl=$tmp/istio-1.5-alpha.$sha/bin/istioctl

cat <<EOF > $tmp/iop.yaml
apiVersion: operator.istio.io/v1alpha1
kind: IstioOperator
spec:
  components:
    telemetry:
      enabled: false
    citadel:
      enabled: false
    ingressGateways:
    - enabled: false
    galley:
      enabled: false

  addonComponents:
    grafana:
      enabled: true

  values:
    telemetry:
      v2:
        enabled: true
      v1:
        enabled: false
EOF

${istioctl} manifest apply -y --wait -f $tmp/iop.yaml
