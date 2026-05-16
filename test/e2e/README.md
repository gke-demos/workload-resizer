# e2e test suite

End-to-end tests for the `workload-resizer` controller. Run with:

```bash
make test-e2e
```

This spins up a Kind cluster, installs KWOK, deploys the controller, runs the
suite, then tears it all down. ~3 minutes on a warm machine.

For faster iteration:

```bash
KIND_CLUSTER=workload-resizer-test-e2e make setup-test-e2e   # one-time
go test -tags=e2e ./test/e2e/ -v -ginkgo.v -ginkgo.focus="scenario 2"
```

## Prerequisites

- `kind` ≥ 0.20 — older versions can't run `kindest/node:v1.35.0`. Override
  the image with `KIND_NODE_IMAGE=...` if needed.
- `docker`, `kubectl`, `go`.

## Running against an existing cluster (not Kind)

Set `USE_EXISTING_CLUSTER=true` and `MANAGER_IMAGE=...` (a registry-qualified
ref the cluster can pull):

```bash
docker push <registry>/workload-resizer:<tag>
MANAGER_IMAGE=<registry>/workload-resizer:<tag> USE_EXISTING_CLUSTER=true make test-e2e
```

This skips Kind cluster create/delete and the `kind load docker-image` step.
You are responsible for building and pushing the image yourself; the suite
still runs `make deploy` (with `IMG=$MANAGER_IMAGE`) to install the controller
into `workload-resizer-system`.

Caveats when targeting a shared cluster:
- KWOK will install fake nodes alongside real ones. Set `KWOK_INSTALL_SKIP=true`
  to skip — though the scenarios that pin to specific instance types
  (`n4-standard-4`, `tiny-machine`, etc.) won't run meaningfully without
  matching node labels.
- A crashed run leaves more behind than `kind delete cluster` would. See
  the cleanup section below.
- The controller has cluster-wide RBAC; needs cluster-admin to deploy.

## Known gotchas

**KWOK doesn't populate `pod.status.containerStatuses[*].resources`**, but
K8s 1.35+ uses that field to gate the `/resize` subresource (the kubelet sets
it to advertise `InPlacePodVerticalScaling` support). KWOK's default
`pod-ready` Stage builds containerStatuses without it.

Workaround: `declareKWOKPodResizeSupport` in `resize_test.go` patches the pod
status manually via `kubectl patch --subresource=status` after the pod reaches
Running. If a future KWOK release populates this field natively, that helper
can be deleted.

The same validation triggers in real heterogeneous clusters during version
skew (old kubelet, new control plane). The controller handles it as a
soft-skip with a `ResizeUnsupported` event — see
`isResizeUnsupportedError` in `internal/controller/pod_controller.go`.

## Cleanup after a crashed run

Deploy/undeploy happens in `BeforeSuite`/`AfterSuite` (not per-Describe), so a
run killed mid-suite leaves the controller and Kind cluster up. Common
leftovers:

```bash
kubectl delete clusterrolebinding workload-resizer-metrics-binding --ignore-not-found
make undeploy ignore-not-found=true
make uninstall ignore-not-found=true
kubectl delete ns workload-resizer-system --ignore-not-found
kind delete cluster --name workload-resizer-test-e2e
```
