# Steps
* Setup mesh
* ./setup_large_test.sh 15
* Wait 12hr
* kubectl delete namespace service-graph{01..15}
* kubectl scale deployment client --replicas 0

GKE v1.16.0-gke.20. 12 nodes, 32core

# Linkerd

```
fortio load -qps 0 -t 60s http://svc00-0.service-graph00:8080/
# target 50% 0.00624822
# target 75% 0.00742408
# target 90% 0.0162338
# target 99% 0.0331583
# target 99.9% 0.0720556
Sockets used: 4 (for perfect keepalive, would be 4)
Code 200 : 28150 (100.0 %)
Response Header Sizes : count 28150 avg 118 +/- 0 min 118 max 118 sum 3321700
Response Body/Total Sizes : count 28150 avg 1142 +/- 0 min 1142 max 1142 sum 32147300
All done 28150 calls (plus 4 warmup) 8.526 ms avg, 469.1 qps
```

`irate(container_cpu_usage_seconds_total{namespace="linkerd", container="", pod!~"linkerd-prometheus.*", pod!~"linkerd-grafana.*"}[1m])`
`sum(irate(container_memory_usage_bytes{namespace=~"service-graph..", container="linkerd-proxy"}[1m]))`

Prometheus CPU bounces between 0.5 and 1 CPU.

Startup: https://snapshot.raintank.io/dashboard/snapshot/iX8tlphAUUdXFv7EJB7H7zwITZnGysp1?orgId=2
12hr: https://snapshot.raintank.io/dashboard/snapshot/jY8Eq0sW4urLgPErairimiw6G4jg7Yf4?orgId=2
Shutdown: https://snapshot.raintank.io/dashboard/snapshot/jwnFQ0POyvRynfglVcMS3o7pWwWEHDsy

100m avg of CPU is 20m for control plane

qps p50/p90/p99
-c 2 -qps 100: 99.9 7.3/22/35
-c 2 -qps 1000: 262.7
-c 64 -qps 100: 99.8 57/80/170
-c 64 -qps 1000: 998.9 49/68/172
-c 64 -qps 0: 1175.7

# Istio

Startup: https://snapshot.raintank.io/dashboard/snapshot/Q2d4HQ28mkkx0y3uc8WZpFA3y3L3qR0z?orgId=2
Shutdown: https://snapshot.raintank.io/dashboard/snapshot/yDoM2OqbIzo4lG967wfAMYhmWS4IFCRZ?orgId=2


qps p50/p90/p99
-c 2 -qps 100: 100 5/21/34
-c 2 -qps 1000: 254.7
-c 64 -qps 100: 99.8 22/39/239
-c 64 -qps 1000: 998.9 20/32/98
-c 64 -qps 0: 3300