---
title: Documentation
linkTitle: Documentation
weight: 1
menu:
  main:
    weight: 10
---

You're in the `workload-resizer` reference docs. The site root has the marketing pitch; this section is the reference.

## Start here

**New to the project?** → [Install]({{< relref "install.md" >}}) walks through the controller + ConfigMap apply path, the prerequisite K8s/node versions, how to inventory your cluster's machine families, and how to verify with a sample workload.

**Trying to understand the design?** → [How it works]({{< relref "how-it-works.md" >}}) covers the reconcile flow, the annotation contract that makes restarts idempotent, and the load-bearing design decisions (QoS preservation, node-support gating for K8s 1.35+, why we use `mgr.GetAPIReader()` for node lookups).

## Reference index

### Getting things done
- **[Install]({{< relref "install.md" >}})** — the two-step apply, prerequisites, picking performance units, verifying with the sample workload, upgrade / uninstall.

### Concepts and design
- **[How it works]({{< relref "how-it-works.md" >}})** — reconcile flow, the global config schema, the things to know about the resize subresource (QoS preservation, node-support gating, recovery semantics).
- **[Configuration reference]({{< relref "configuration.md" >}})** — detailed reference for GKE ComputeClass matching, label/annotation rules, and prioritized overrides.
