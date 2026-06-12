---
title: "Configuration reference"
linkTitle: "Configuration"
weight: 30
description: "Detailed reference for the workload-resizer config.yaml structure, including GKE ComputeClass, label/annotation matching, and overrides."
---

The `workload-resizer` configuration is specified inside a single namespaced `ConfigMap` (typically `workload-resizer-system/workload-resizer-config`) under the `config.yaml` key.

This reference describes the full configuration schema, matching precedence order, and validation rules supported by the controller.

---

## Configuration schema

The configuration consists of three core global default settings, followed by optional direct-mapping blocks and an ordered list of prioritized override rules.

```yaml
# 1. Global Defaults (Backwards Compatible)
baselineNodeType: <string>
nodeTypes:
  <node-type-name>: { cpuPerf: <float>, memPerf: <float> }
bounds:
  cpu: { min: <quantity>, max: <quantity> }
  memory: { min: <quantity>, max: <quantity> }

# 2. Direct GKE Compute Class Mappings (Optional)
computeClasses:
  <compute-class-name>:
    baselineNodeType: <string>      # Optional override
    nodeTypes:                      # Optional override/extension
      <node-type-name>: { cpuPerf: <float>, memPerf: <float> }
    bounds:                         # Optional override
      cpu: { min: <quantity>, max: <quantity> }
      memory: { min: <quantity>, max: <quantity> }

# 3. Direct Pod Label Mappings (Optional)
podLabels:
  <label-key>:
    <label-value>:
      baselineNodeType: <string>
      nodeTypes: ...
      bounds: ...

# 4. Direct Pod Annotation Mappings (Optional)
podAnnotations:
  <annotation-key>:
    <annotation-value>:
      baselineNodeType: <string>
      nodeTypes: ...
      bounds: ...

# 5. Prioritized Overrides List (Optional)
overrides:
  - name: <string>
    match:
      computeClass: <string>        # Optional
      podLabel:                     # Optional (AND logic)
        <label-key>: <label-value>
      podAnnotation:                # Optional (AND logic)
        <anno-key>: <anno-value>
    baselineNodeType: <string>
    nodeTypes: ...
    bounds: ...
```

---

## Precedence and evaluation order

When a Pod is reconciled, the controller searches for the first matching configuration block and immediately applies it. Any missing fields in the matched sub-configuration are **merged directly from the top-level global defaults** (first-match-wins with global fallback).

The exact precedence sequence is:

1. **`overrides` list**: Checked in the order defined. The first rule where **all** specified `match` criteria are satisfied by the Pod is selected.
2. **`podAnnotations`**: Checks if the Pod carries any of the keys defined in `podAnnotations` with the specified value.
3. **`podLabels`**: Checks if the Pod carries any of the keys defined in `podLabels` with the specified value.
4. **`computeClasses`**: Checks if the Pod is scheduled on a node matching the `cloud.google.com/compute-class` selector from its `spec.nodeSelector`.
5. **Global Defaults**: If no custom rules or direct maps match, the top-level global settings are applied.

---

## Detailed field reference

### Global configuration

- **`baselineNodeType`** (string, required): The node type that the workloads were originally calibrated against. Must exist in `nodeTypes` (either global or defined within the active sub-config).
- **`nodeTypes`** (map[string]NodeProfile, required): Association of node type names to their performance profiles.
  - **`cpuPerf`** (float, required): Normalized CPU performance capacity (must be `> 0`).
  - **`memPerf`** (float, required): Normalized Memory performance capacity (must be `> 0`).
- **`bounds`** (Bounds, required): Absolute boundaries within which rescaled resources will be clamped.
  - **`cpu`** / **`memory`**:
    - **`min`** (Quantity, required): Minimum allocation floor (e.g., `50m`, `64Mi`).
    - **`max`** (Quantity, required): Maximum allocation ceiling (e.g., `16`, `32Gi`).

### GKE Compute Classes (`computeClasses`)

Allows direct configuration mapping for workloads run on specific GKE Compute Classes (specified via `cloud.google.com/compute-class` in the Pod's node selector).
- **`<compute-class-name>`** (string key): The name of the GKE Compute Class (e.g., `Performance`, `Scale-Out`, or any custom class).
  - Contains optional overrides for `baselineNodeType`, `nodeTypes`, and `bounds`.

### Pod Labels (`podLabels`)

Allows mapping specific Pod labels directly to sizing bounds and ratios.
- **`<label-key>`** (string key): The label key to check on the Pod (e.g., `env`, `tier`).
  - **`<label-value>`** (string key): The exact label value to match (e.g., `production`, `sandbox`).
    - Contains optional overrides for `baselineNodeType`, `nodeTypes`, and `bounds`.

### Pod Annotations (`podAnnotations`)

Allows mapping specific Pod annotations directly to sizing bounds and ratios.
- **`<annotation-key>`** (string key): The annotation key to check on the Pod (e.g., `workload-resizer.io/tier`).
  - **`<annotation-value>`** (string key): The exact annotation value to match (e.g., `critical-db`, `standard`).
    - Contains optional overrides for `baselineNodeType`, `nodeTypes`, and `bounds`.

### Overrides (`overrides`)

An ordered list of custom rules for complex multi-criteria matching.
- **`name`** (string, required): Unique identifier for the override rule.
- **`match`** (Match, required): Logic block containing criteria that must be satisfied. All specified fields inside `match` use **AND** logic:
  - **`computeClass`** (string, optional): Matches GKE Compute Class value in the Pod's node selector.
  - **`podLabel`** (map[string]string, optional): Sub-map of label key-value pairs that the Pod must carry.
  - **`podAnnotation`** (map[string]string, optional): Sub-map of annotation key-value pairs that the Pod must carry.
- **`baselineNodeType`** (string, optional)
- **`nodeTypes`** (map[string]NodeProfile, optional)
- **`bounds`** (Bounds, optional)

---

## Full configuration example

Below is a complete, production-grade example of a `config.yaml` file demonstrating global defaults, direct compute class overrides, label mappings, and multi-criteria priority overrides:

```yaml
# 1. Global defaults (applied if no other rule matches)
baselineNodeType: n2d
nodeTypes:
  n2d: { cpuPerf: 1.0,  memPerf: 1.0 }
  n4:  { cpuPerf: 1.25, memPerf: 1.0 }
  c3:  { cpuPerf: 1.30, memPerf: 1.0 }
bounds:
  cpu: { min: "50m", max: "16" }
  memory: { min: "64Mi", max: "32Gi" }

# 2. Direct GKE Compute Class Mapping
computeClasses:
  Performance:
    baselineNodeType: c3
    bounds:
      cpu: { min: "100m", max: "32" }

# 3. Direct Pod Label Mapping
podLabels:
  env:
    production:
      bounds:
        cpu: { min: "500m", max: "32" }
    sandbox:
      bounds:
        cpu: { min: "10m", max: "100m" }

# 4. Direct Pod Annotation Mapping
podAnnotations:
  workload-resizer.io/tier:
    critical:
      bounds:
        cpu: { min: "1000m", max: "64" }

# 5. Prioritized Overrides (for complex multi-criteria rules)
overrides:
  - name: "critical-production-workloads"
    match:
      computeClass: "Performance"
      podLabel:
        priority: "critical"
        env: "production"
    bounds:
      cpu: { min: "2000m", max: "64" }
```

---

## Validation rules

To ensure the controller never scales Pods with invalid ratios, the ConfigMap is rejected at start and refresh time unless the following rules are met:

1. **Global constraints**:
   - `baselineNodeType` must be present and must exist as a key under global `nodeTypes`.
   - Global `bounds` must satisfy `min <= max` for both CPU and memory.
2. **Sub-configuration constraints** (inside `overrides`, `computeClasses`, `podLabels`, and `podAnnotations`):
   - Any custom `baselineNodeType` defined within a rule must exist either in that rule's custom `nodeTypes` or in the global `nodeTypes` list.
   - Any custom `nodeTypes` performance multipliers (`cpuPerf`, `memPerf`) must be strictly greater than `0`.
   - Any custom `bounds` must satisfy `min <= max`.
3. **Override constraints**:
   - Each rule under `overrides` must have a unique `name` attribute.
   - Each rule's `match` block must specify at least one criteria (`computeClass`, `podLabel`, or `podAnnotation`).
4. **Map structure constraints**:
   - Empty string keys are prohibited at all levels of `podLabels` and `podAnnotations`.
