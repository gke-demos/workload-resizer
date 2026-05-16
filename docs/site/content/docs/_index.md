---
title: Documentation
toc: false
sidebar:
  open: true
---

`workload-resizer` is a Kubernetes controller for GKE that uses the in-place pod resize subresource to dynamically adjust pod resource requests based on the instance type of the node a pod lands on. Requests calibrated for one machine type are recomputed against per-type performance units when the pod lands on a different one.

{{< cards >}}
  {{< card link="install/" title="Install" subtitle="Apply the controller + ConfigMap, inventory your nodes' instance types, and verify with a sample workload." icon="cloud-upload" >}}
  {{< card link="how-it-works/" title="How it works" subtitle="Reconcile flow, annotation contract, the resize subresource, and the design decisions that came out of testing." icon="cog" >}}
{{< /cards >}}

## Quick links

- **Source**: [github.com/gke-demos/workload-resizer](https://github.com/gke-demos/workload-resizer)
- **Releases**: [latest tags](https://github.com/gke-demos/workload-resizer/releases)
- **Issue tracker**: [report a bug or request a feature](https://github.com/gke-demos/workload-resizer/issues)
- **Container image**: `ghcr.io/gke-demos/workload-resizer`
