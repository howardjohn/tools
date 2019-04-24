# CockroachDB

This test installs an instance of CockroachDB using the [stable/cockroachdb](https://github.com/helm/charts/tree/master/stable/cockroachdb) Helm chart.

The install is generated using `helm template stable/cockroachdb --name cockroachdb --namespace istio-stability-cockroachdb`

Changes needed to work with Istio:
* Change cockroachdb-cockroachdb-public to not conflict with cockroachdb-cockroachdb ports.
* Add ServiceEntry in `istio.yaml`
