---
title: workload-resizer
---

{{< blocks/cover title="workload-resizer" image_anchor="top" height="med" >}}

<p class="lead mt-5">
A Kubernetes controller for GKE that uses the in-place pod resize subresource (<code>pods/resize</code>, GA in K8s 1.35) to adjust pod resource requests when pods land on node types whose performance characteristics differ from the type the workload was originally calibrated for.
</p>

<a class="btn btn-lg btn-primary me-3 mb-4" href="docs/install/">Get started <i class="fa-solid fa-arrow-right ms-2"></i></a>
<a class="btn btn-lg btn-secondary me-3 mb-4" href="https://github.com/gke-demos/workload-resizer">Source on GitHub <i class="fa-brands fa-github ms-2"></i></a>

{{< /blocks/cover >}}

{{% blocks/lead color="primary" %}}

When a Deployment is sized for one machine family (say `n2d`) but the scheduler places its pods on a more powerful one (`n4`, ~25% more CPU per core), the original requests over-provision capacity. `workload-resizer` watches scheduled pods, reads the assigned node's `cloud.google.com/machine-family` label, and patches the pod's requests in place using a configurable performance-unit matrix — **without restarting the container**.

{{% /blocks/lead %}}

{{% blocks/section color="dark" type="row" %}}

{{% blocks/feature icon="fa-solid fa-bolt" title="No-restart resize" url="docs/how-it-works/" %}}
Adjustments land on running pods via the in-place resize subresource (GA in K8s 1.35). Containers keep their identity, sockets, in-memory state; the kubelet adjusts cgroup limits live.
{{% /blocks/feature %}}

{{% blocks/feature icon="fa-solid fa-network-wired" title="Heterogeneous-pool aware" url="docs/how-it-works/" %}}
One Deployment, many machine families. The controller normalizes effective capacity at scheduling time using per-family performance units — calibrate once, run anywhere your cluster has nodes for.
{{% /blocks/feature %}}

{{% blocks/feature icon="fa-solid fa-shield-halved" title="QoS-class preserving" url="docs/how-it-works/" %}}
When a pod is Guaranteed (<code>requests == limits</code>), the resize mirrors the request change into the limit so the API server doesn't reject the patch. Original template requests are captured as annotations before the first resize, so controller restarts don't compound or undo changes.
{{% /blocks/feature %}}

{{% /blocks/section %}}

{{% blocks/section color="white" %}}

<div class="td-content col-12 col-lg-8 mx-auto">

## Install

ConfigMap first, then the controller. Reversed order works too but
the controller pod crash-loops briefly until the ConfigMap lands —
applying in this order avoids that. For non-GKE clusters, edit
`config.yaml` first to match your nodes' `machine-family` label
values.

```bash
URL=https://github.com/gke-demos/workload-resizer/releases/latest/download
kubectl apply -f $URL/config.yaml
kubectl apply -f $URL/install.yaml
```

The [Install guide](docs/install/) covers prerequisites, how to
inventory your cluster's machine families, picking performance
units, and verifying with a sample workload.

</div>

{{% /blocks/section %}}
