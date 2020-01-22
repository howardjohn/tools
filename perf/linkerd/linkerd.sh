#!/bin/bash

set -eux

tmp=$(mktemp -d)
version=edge-20.1.2
cd $tmp
curl -sLO https://github.com/linkerd/linkerd2/releases/download/$version/linkerd2-cli-$version-linux
linkerd=$tmp/linkerd2-cli-$version-linux
chmod +x $linkerd

$linkerd install | kubectl apply -f -
