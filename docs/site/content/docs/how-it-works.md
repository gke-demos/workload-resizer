---
title: "How it works"
linkTitle: "How it works"
weight: 20
description: "Reconcile flow, annotation contract, the resize subresource, and the design decisions that came out of testing."
---

## The problem

Deployment authors set CPU / memory requests calibrated for a specific machine family. When the cluster has a heterogeneous node pool — say `n2d` alongside the newer, ~25% more powerful `n4` — pods that land on the more powerful nodes over-provision their requests for the actual work being done. The classic fix is the Vertical Pod Autoscaler, but VPA recreates pods and reasons about utilization over time. `workload-resizer` does something narrower: when a pod is scheduled, look at where it landed and patch its requests **right there**, with a known performance-unit ratio, no restart.

## What it watches

The controller watches `Pod` objects and only acts when all of the following are true:

- `spec.nodeName != ""` — the pod has been scheduled.
- It's owned by one of `{ReplicaSet, StatefulSet, DaemonSet, Job}`. Bare pods are skipped (out of scope for v1).
- The `workload-resizer.io/applied-instance-type` annotation isn't present, or doesn't match the current value of the node-type label (default `cloud.google.com/machine-family`; override with `--node-type-label`).

## What it does, step by step

For each container in `spec.containers` (init / sidecar containers are skipped in v1):

1. **Read the originals.** If the pod already has an `original-cpu.<container>` annotation (i.e., the controller has touched it before), use that as the baseline. Otherwise, the current `spec.containers[i].resources.requests` *is* the original — capture it.
2. **Compute the desired value.**

   ```text
   desired = clamp(original × baselinePerf / nodePerf, bounds)
   ```

   `baselinePerf` and `nodePerf` come from the global `ConfigMap`; `bounds` is the absolute min/max floor/ceiling for that resource (so a workload calibrated for a slow node doesn't get resized below `50m` CPU on a 100× faster machine).
3. **Persist the originals annotation first.** Before issuing the resize, write `workload-resizer.io/original-cpu.<container>` and `original-memory.<container>` so reconciliation is idempotent across controller restarts. (If we crash between this step and the resize, the next pass re-derives the same desired value from the annotation.)
4. **Patch via `/resize`.** Issue a strategic merge patch against the `pods/resize` subresource. If the pod was Guaranteed (`requests == limits`), the patch also includes the matching limits — the API server rejects resize patches that would change a pod's QoS class.
5. **Record what we did.** Write `applied-instance-type` and `applied-at` annotations.
6. **Emit an event.** One of `Resized`, `AlreadyAligned`, `UnknownNodeType`, `BoundsClamped`, `ResizeUnsupported`, or `ResizeFailed` — visible in `kubectl describe pod`.

## What the global config looks like

A single ConfigMap (default `workload-resizer-system/workload-resizer-config`):

```yaml
baselineNodeType: n2d
nodeTypes:
  n2d: { cpuPerf: 1.0,  memPerf: 1.0 }
  n4:  { cpuPerf: 1.25, memPerf: 1.0 }
  c3:  { cpuPerf: 1.30, memPerf: 1.0 }
bounds:
  cpu:    { min: "50m",  max: "16" }
  memory: { min: "64Mi", max: "32Gi" }
```

The keys under `nodeTypes:` are the *values* the controller will see on the node-type label — `n2d`, `n4`, `c3` etc. with the default `cloud.google.com/machine-family`; full instance types if you set `--node-type-label=node.kubernetes.io/instance-type` instead. `cpuPerf` and `memPerf` are normalized performance units — the controller computes `baseline / node`, so a `1.25` is "this node is 1.25× as capable as baseline, so it needs `1/1.25 = 0.8` of the request." The controller polls this ConfigMap on `--config-refresh-interval` (30s default).

## Design decisions worth knowing

These came out of envtest and Kind + KWOK testing; they're load-bearing.

### QoS-class preservation

The Kubernetes API server **rejects** a `/resize` patch that would change a pod's QoS class. A Guaranteed pod (where every container has `requests == limits` for both CPU and memory) becomes Burstable the moment you shrink requests without shrinking limits. So the controller mirrors request changes into limits whenever limits were originally set on that container. If you ever change `applyResize` to touch only requests, you'll silently break Guaranteed workloads.

### Node-support gating (K8s 1.35+)

K8s 1.35 GA'd in-place resize, and as part of the version-skew story the API server uses `pod.status.containerStatuses[i].resources != nil` to detect whether the assigned node's kubelet has advertised `InPlacePodVerticalScaling` support. Resize patches against pods on un-advertising nodes are rejected with `"Pod running on node without support for resize"`. The controller treats this as a **soft skip with a `ResizeUnsupported` event, no requeue** — correct for heterogeneous clusters during upgrades. KWOK's default `pod-ready` stage doesn't populate this field, which is why the e2e suite manually patches pod status; see `test/e2e/README.md`.

### Annotation order is the recovery contract

The three-step write order (`original-*` annotation → `/resize` patch → `applied-*` annotation) makes every intermediate state recoverable:

- Crash between step 1 and 2: next reconcile reads `original-*` from the annotation, computes the same `desired`, applies the patch, writes `applied-*`. Same end state.
- Crash between step 2 and 3: next reconcile re-derives `desired` from `original-*`, sees current resources already match, skips the patch, writes `applied-*`.
- Crash after step 3: predicate filters out the pod (annotation matches the node), nothing to do.

If you ever rearrange that order, you'll re-introduce the compounding-resize bug we hit during initial design — where a restarted controller treats an already-resized pod as a fresh baseline and shrinks it again.

### Node lookups bypass the cache

The controller only watches Pods, so the controller-runtime cache has no Node informer. A cached `client.Get(ctx, ..., &node)` returns NotFound for unwatched types until an informer lazily syncs. The controller uses `mgr.GetAPIReader()` for the Node lookup to sidestep this entirely. Slightly slower per reconcile, much more predictable.

## What's out of scope (for v1)

- **HPA / VPA coexistence** — docs warning only. Shrinking CPU requests changes the denominator for HPA's `CPUUtilization` metric and will cascade into scaling decisions; VPA conflicts outright.
- **Init / sidecar containers** — only `spec.containers` are resized.
- **Per-pod baseline override** — the baseline lives in the global ConfigMap. Per-pod annotation override may come post-v1.
- **CRD-based config** — a ConfigMap is enough for v1; a CRD would only earn its complexity if we need finer-grained scoping.

For the full set of design decisions and rationale, see `AGENTS.md` in the repo.
