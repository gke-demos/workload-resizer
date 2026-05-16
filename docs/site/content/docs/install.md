---
title: "Install"
linkTitle: "Install"
weight: 10
description: "Apply the controller + ConfigMap, inventory your cluster's machine families, and verify with a sample workload."
---

`workload-resizer` ships as two manifests on each GitHub Release:

- **`install.yaml`** — controller Deployment, RBAC, and the
  `workload-resizer-system` namespace, with the image pinned to the
  release tag.
- **`config.yaml`** — a sample `ConfigMap` with the schema and example
  GKE node-type performance units.

Both are needed. **Apply `config.yaml` first** — it carries the
`workload-resizer-system` namespace and the required ConfigMap, so
the controller pod (created by `install.yaml`) finds what it needs
already in place. Reverse the order and the controller pod fails
fast on startup (manager exits with `initial config load: configmaps
... not found`, kubelet drops it into `CrashLoopBackOff`); it'll
recover once the ConfigMap is applied, but only after the kubelet's
restart backoff. Once the ConfigMap exists at runtime and is later
deleted, the controller keeps using the last-known config and logs
the refresh failure every 30s (`--config-refresh-interval`).

## Prerequisites

- Kubernetes **1.35+** on the API server *and* the nodes. The
  in-place pod resize subresource (`pods/resize`) is GA in 1.35; on
  older nodes the controller's `/resize` patches are rejected with a
  soft-skip `ResizeUnsupported` event.
- Nodes labeled with `cloud.google.com/machine-family` (the controller's
  default `--node-type-label`; GKE sets it for you). If you're not on
  GKE, point `--node-type-label` at whatever label your provider uses
  to identify machine families.
- A user with cluster-admin (the controller's RBAC is cluster-scoped:
  `pods`, `pods/resize`, `nodes`, `events`).

## Install

Pick a release tag from the [releases page](https://github.com/gke-demos/workload-resizer/releases),
then apply both manifests in order. The examples below use the
`/latest/` redirect, which always points at the latest release;
substitute a pinned `v0.x.y` tag for production.

```bash
# 1. Namespace + ConfigMap. Edit nodeTypes for your cluster first.
#    Applying this BEFORE install.yaml means the controller pod (from
#    step 2) finds the ConfigMap already in place and starts cleanly.
curl -fsSLO https://github.com/gke-demos/workload-resizer/releases/latest/download/config.yaml
$EDITOR config.yaml
kubectl apply -f config.yaml

# 2. Controller (RBAC, Deployment). Idempotent across upgrades.
kubectl apply -f https://github.com/gke-demos/workload-resizer/releases/latest/download/install.yaml
```

Confirm the controller is running:

```bash
kubectl -n workload-resizer-system rollout status deployment/workload-resizer-controller-manager
kubectl -n workload-resizer-system logs deployment/workload-resizer-controller-manager
```

## Inventorying your cluster's node families

The shipped `config.yaml` lists GKE machine families (`n2d`, `n4`,
`c3`) under `nodeTypes:`. Yours may differ. Get the actual set:

```bash
kubectl get nodes \
  -L cloud.google.com/machine-family \
  -L node.kubernetes.io/instance-type
```

The `MACHINE-FAMILY` column is what the controller keys off by
default. Every distinct value that appears there should also appear
as a key under `nodeTypes:` in your ConfigMap. Any value the
controller sees on a pod's node that *isn't* in the config will be
skipped with an `UnknownNodeType` event on the pod — visible via
`kubectl describe pod`.

If you'd rather match on the full instance type (e.g., `n4-standard-4`
vs `n4-standard-8` as distinct entries), set
`--node-type-label=node.kubernetes.io/instance-type` and key
`nodeTypes:` by instance type. Family-level is the recommended
default because perf per core is constant across sizes within a
family.

## Picking performance units

The math is `desired = original × baselinePerf / nodePerf`. So:

- **Pick a baseline.** Whichever machine family your existing
  Deployments' requests are calibrated against. Set `baselineNodeType`
  to that family (e.g., `n2d`) and give its entry `cpuPerf: 1.0`.
- **Express other types relative to the baseline.** If a newer node
  is 25% more powerful per core, give it `cpuPerf: 1.25` — the
  controller will compute `original × 1.0 / 1.25 = 0.8 × original`
  and shrink the pod's CPU request accordingly.

For GCP machine families, Google's [machine type performance
documentation](https://cloud.google.com/compute/docs/general-purpose-machines)
is a reasonable starting point; benchmark your own workloads when you
care about precision.

## Verifying with a sample workload

The repo ships a `sample-workload` Deployment at
[`config/samples/deployment.yaml`](https://github.com/gke-demos/workload-resizer/blob/main/config/samples/deployment.yaml).
Apply it, then watch what the controller writes:

```bash
kubectl apply -f https://raw.githubusercontent.com/gke-demos/workload-resizer/main/config/samples/deployment.yaml

kubectl get pod -l app=sample-workload \
  -o custom-columns=NAME:.metadata.name,\
NODE:.spec.nodeName,\
CPU:.spec.containers[0].resources.requests.cpu,\
APPLIED:.metadata.annotations.workload-resizer\\.io/applied-instance-type
```

Once the controller has reconciled, `APPLIED` shows the instance type
the resize was computed against, and the `CPU` column reflects the
post-resize value if the node type differed from the baseline.

## Upgrading

```bash
# Pull the new install.yaml; this preserves your customized ConfigMap.
kubectl apply -f https://github.com/gke-demos/workload-resizer/releases/latest/download/install.yaml
```

`install.yaml` deliberately does **not** include the ConfigMap, so a
re-apply on upgrade never overwrites your customized config. If the
ConfigMap schema changes in a future release, the release notes will
say so explicitly.

## Uninstall

```bash
kubectl delete -f https://github.com/gke-demos/workload-resizer/releases/latest/download/install.yaml
```

Pod annotations the controller wrote
(`workload-resizer.io/original-cpu.*`, `applied-instance-type`,
`applied-at`) are left on existing pods. The current resource requests
on those pods stay at whatever the controller last set; replacement
pods (from the Deployment's PodTemplate) start fresh from the original
template requests.
