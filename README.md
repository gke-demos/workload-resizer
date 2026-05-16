# workload-resizer

A Kubernetes controller for GKE that uses the in-place pod resize subresource (`pods/resize`, GA in K8s 1.35) to dynamically adjust pod resource requests when pods land on node types whose performance characteristics differ from the type the workload was originally calibrated for.

When a Deployment is sized for one machine type (say `n2d-standard-4`) but the scheduler places its pods on a more powerful one (`n4-standard-4`, ~25% more CPU per core), the original requests over-provision capacity. `workload-resizer` watches scheduled pods, reads the assigned node's `node.kubernetes.io/instance-type` label, and patches the pod's requests in place using a configurable performance-unit matrix — without restarting the container.

## Status

Pre-v1. The v1 design and scope is settled (see [AGENTS.md](./AGENTS.md)); the suite covers unit, envtest, and Kind + KWOK e2e layers.

## Quick start

```bash
# 1. Install the controller (RBAC, Deployment, namespace).
kubectl apply -f https://github.com/gke-demos/workload-resizer/releases/latest/download/install.yaml

# 2. Install the config ConfigMap — edit nodeTypes for your cluster first!
#    The shipped sample lists GKE node types; without this step the
#    controller starts cleanly but is a silent no-op.
curl -fsSLO https://github.com/gke-demos/workload-resizer/releases/latest/download/config.yaml
$EDITOR config.yaml
kubectl apply -f config.yaml

# 3. (Optional) Apply a sample workload to see the resize happen.
kubectl apply -f https://raw.githubusercontent.com/gke-demos/workload-resizer/main/config/samples/deployment.yaml

# Watch the pod get resized:
kubectl get pod -l app=sample-workload -w \
  -o custom-columns=NAME:.metadata.name,\
NODE:.spec.nodeName,\
CPU:.spec.containers[0].resources.requests.cpu,\
APPLIED:.metadata.annotations.workload-resizer\\.io/applied-instance-type
```

See the [install page](https://gke-demos.github.io/workload-resizer/docs/install/) for prerequisites, how to inventory your cluster's instance types, picking performance units, and upgrade/uninstall.

## How it works

For each pod that lands on a node, the controller:

1. Reads `node.kubernetes.io/instance-type` from the assigned node.
2. Looks up the per-type performance unit in the global config (a ConfigMap).
3. Computes `desired = clamp(original × baselinePerf / nodePerf, bounds)` per container.
4. Patches the pod via `/resize`, mirroring the change into limits when present to preserve QoS class.
5. Records the original requests and the applied instance type as annotations on the pod, for idempotent reconciliation across controller restarts.

Full architecture and the design rationale are in [AGENTS.md](./AGENTS.md). The published docs site lives at <https://gke-demos.github.io/workload-resizer/>.

## Development

```bash
dev/tools/ci             # run every CI check locally (same as GitHub Actions)
dev/tools/fix-go-format  # auto-fix formatting
make test                # envtest suite only (fast)
make test-e2e            # full Kind + KWOK e2e (~3 min)
```

See [`dev/README.md`](./dev/README.md) for the dev tooling layout and [CONTRIBUTING.md](./CONTRIBUTING.md) for the contribution workflow (DCO sign-off required).

## License

[Apache License 2.0](./LICENSE).
