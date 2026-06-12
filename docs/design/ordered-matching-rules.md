# Design: Ordered Matching & Direct Maps for Config Overrides

This design document outlines the updated **Hybrid Configuration Schema** for the `workload-resizer` controller. This design supports both prioritized general override rules and direct, top-level maps matching on GKE `ComputeClass`, Pod labels, and Pod annotations.

---

## 1. Overview and Motivation

To provide maximum flexibility and ease of use, the controller supports two styles of resource resizing configuration:
1. **Direct Maps**: Extremely simple, top-level maps that associate a GKE Compute Class, a Pod Label key-value, or a Pod Annotation key-value directly to custom ratios/bounds.
2. **Prioritized Overrides (Rules)**: An ordered list of custom rules for complex scenarios (such as combining labels and compute classes) where explicit priority evaluation is required.

---

## 2. Configuration Schema Design

The `config.yaml` schema in the `workload-resizer-config` ConfigMap is structured as follows.

### Go Structures (`internal/config/config.go`)

```go
type NodeProfile struct {
	CPUPerf float64 `json:"cpuPerf"`
	MemPerf float64 `json:"memPerf"`
}

type Bound struct {
	Min resource.Quantity `json:"min"`
	Max resource.Quantity `json:"max"`
}

type Bounds struct {
	CPU    Bound `json:"cpu"`
	Memory Bound `json:"memory"`
}

type Match struct {
	// ComputeClass matches the value of the pod's "cloud.google.com/compute-class" nodeSelector.
	ComputeClass string `json:"computeClass,omitempty"`

	// PodLabel matches if the pod contains all the specified labels with their corresponding values.
	PodLabel map[string]string `json:"podLabel,omitempty"`

	// PodAnnotation matches if the pod contains all the specified annotations with their corresponding values.
	PodAnnotation map[string]string `json:"podAnnotation,omitempty"`
}

type Override struct {
	// Name is a descriptive name for the override rule.
	Name string `json:"name"`

	// Match defines the criteria for the pod to qualify for this override.
	// All specified fields inside the Match block must be satisfied (AND logic).
	Match Match `json:"match"`

	// BaselineNodeType overrides the global baseline node type.
	BaselineNodeType string `json:"baselineNodeType,omitempty"`

	// NodeTypes overrides or extends the performance profiles of specific node types.
	NodeTypes map[string]NodeProfile `json:"nodeTypes,omitempty"`

	// Bounds overrides the global sizing limits.
	Bounds *Bounds `json:"bounds,omitempty"`
}

type ComputeClassConfig struct {
	BaselineNodeType string                 `json:"baselineNodeType,omitempty"`
	NodeTypes        map[string]NodeProfile `json:"nodeTypes,omitempty"`
	Bounds           *Bounds                `json:"bounds,omitempty"`
}

type LabelConfig struct {
	BaselineNodeType string                 `json:"baselineNodeType,omitempty"`
	NodeTypes        map[string]NodeProfile `json:"nodeTypes,omitempty"`
	Bounds           *Bounds                `json:"bounds,omitempty"`
}

type AnnotationConfig struct {
	BaselineNodeType string                 `json:"baselineNodeType,omitempty"`
	NodeTypes        map[string]NodeProfile `json:"nodeTypes,omitempty"`
	Bounds           *Bounds                `json:"bounds,omitempty"`
}

type Config struct {
	BaselineNodeType string                                 `json:"baselineNodeType"`
	NodeTypes        map[string]NodeProfile                 `json:"nodeTypes"`
	Bounds           Bounds                                 `json:"bounds"`
	ComputeClasses   map[string]ComputeClassConfig          `json:"computeClasses,omitempty"`
	PodLabels        map[string]map[string]LabelConfig      `json:"podLabels,omitempty"`
	PodAnnotations   map[string]map[string]AnnotationConfig `json:"podAnnotations,omitempty"`
	Overrides        []Override                             `json:"overrides,omitempty"`
}
```

---

## 3. Sample Configuration

```yaml
# 1. Global defaults (backwards compatible fallbacks)
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

## 4. Evaluation & Precedence Order

When reconciling a Pod, the controller searches for a matching configuration block. It uses a **first match wins** precedence hierarchy. 

Once a matching block is found, the controller merges any specified fields with the **global defaults** as a fallback (missing fields are filled directly from the global defaults). No further maps or overrides are evaluated.

### Precedence Sequence:
1. **`Overrides`**: The controller iterates over the `overrides` list. The first rule whose `Match` criteria is fully satisfied by the Pod (AND logic) is applied.
2. **`PodAnnotations`**: For each annotation key-value pair configured in `podAnnotations`, the controller checks if the Pod carries that annotation. If a match is found, it is applied.
3. **`PodLabels`**: For each label key-value pair configured in `podLabels`, the controller checks if the Pod carries that label. If a match is found, it is applied.
4. **`ComputeClasses`**: Checks if the Pod's `spec.nodeSelector["cloud.google.com/compute-class"]` exists in `computeClasses`. If a match is found, it is applied.
5. **Top-Level Defaults**: If no custom rules or direct maps match, the global `baselineNodeType`, `nodeTypes`, and `bounds` are applied.

---

## 5. Security & Validation Rules

`Config.Validate()` enforces:
1. Global configurations are required and must be valid.
2. Sub-configs (overrides, computeClasses, podLabels, podAnnotations):
   - Custom `baselineNodeType` must exist in either the sub-config's `nodeTypes` or the global `nodeTypes`.
   - Custom `nodeTypes` multipliers must be `> 0`.
   - Custom `bounds` must satisfy `min <= max`.
3. Overrides must have unique names and define at least one match criteria.
4. Nested maps inside `podLabels` and `podAnnotations` cannot contain empty string keys.
