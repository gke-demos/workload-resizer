---
title: workload-resizer
toc: false
---

# workload-resizer

A Kubernetes controller for GKE that uses the in-place pod resize subresource (`pods/resize`, GA in K8s 1.35) to dynamically adjust pod resource requests when pods land on node types whose performance characteristics differ from the type the workload was originally calibrated for.

When a Deployment is sized for one machine type (say `n2d-standard-4`) but the scheduler places its pods on a more powerful one (`n4-standard-4`, ~25% more CPU per core), the original requests over-provision capacity. `workload-resizer` watches scheduled pods, reads the assigned node's `node.kubernetes.io/instance-type` label, and patches the pod's requests in place using a configurable performance-unit matrix — **without restarting the container**.

[How it works →](docs/how-it-works/) &nbsp; [View on GitHub →](https://github.com/gke-demos/workload-resizer)

---

## What it gives you

- **In-place resize, no restart.** Pods keep their identity, sockets, in-memory state. The kubelet adjusts cgroup limits live.
- **Heterogeneous-pool aware.** One Deployment, many machine types — the controller normalizes effective capacity at scheduling time.
- **QoS preserving.** When a pod is Guaranteed (`requests == limits`), the resize mirrors the request change into limits so the QoS class doesn't change. The API server would reject the patch otherwise.
- **Idempotent across restarts.** Original template requests are captured as pod annotations before the first resize, so controller restarts don't compound or undo changes.
- **Conservative defaults.** Skip + event on unknown node types, configurable min/max bounds on resized values, soft-skip when a node hasn't advertised resize support.

## What it doesn't (yet)

- HPA / VPA coexistence defenses — docs warning only.
- Init / sidecar container resizing.
- Per-pod baseline override (only a single cluster-wide baseline today).
- CRD-based config (the v1 config is a ConfigMap; a CRD may come later if pluggability needs justify it).

## Status

Pre-v1. The v1 design and scope is settled; the test suite covers unit, envtest, and Kind + KWOK e2e layers. See the [README](https://github.com/gke-demos/workload-resizer#readme) for build / install instructions and `AGENTS.md` in the repo for architecture detail.
