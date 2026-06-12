# Issue Tracker: Hybrid Matching Rules for Config Overrides

The following tracking issues have been updated in the repository to reflect the **Hybrid Configuration Schema** (combining prioritized overrides list and direct mappings for GKE ComputeClass, Pod labels, and Pod annotations).

---

## [Issue #1] Support Hybrid Matcher Config Schema and Validation
* **Status**: Completed (PR Pending)
* **Component**: `internal/config/`
* **Dependency**: None

### Description
Implement the hybrid `Config` struct schema inside `internal/config/config.go` and its associated parser/validation logic inside `Validate()`.

### Acceptance Criteria
- [ ] Add `ComputeClassConfig`, `LabelConfig`, `AnnotationConfig`, `Match`, `Override`, and update `Config` structs in `internal/config/config.go` as defined in `docs/design/ordered-matching-rules.md`.
- [ ] Ensure backward compatibility with existing configurations (no direct maps or overrides defined).
- [ ] Implement robust nested validation helper `validateSubConfig()` checking:
  - [ ] Custom `baselineNodeType` exists either in the rule's `nodeTypes` or global `nodeTypes`.
  - [ ] Custom `nodeTypes` multipliers are greater than `0`.
  - [ ] Custom `bounds` satisfy `min <= max`.
- [ ] Ensure duplicate override names and empty matching criteria are rejected.
- [ ] Ensure empty keys in `podLabels` and `podAnnotations` nested maps are rejected.
- [ ] Add thorough unit tests in `internal/config/config_test.go` covering all valid and invalid configurations.

---

## [Issue #2] Implement Priority Resolution & Reconciler Integration
* **Status**: Proposed
* **Component**: `internal/controller/`
* **Dependency**: Issue #1

### Description
Implement configuration priority resolution within the `PodReconciler` inside `internal/controller/pod_controller.go` and integrate it into `Reconcile()`.

### Acceptance Criteria
- [ ] Implement a configuration resolver helper on `PodReconciler` that resolves the active config for a given `Pod` following this strict precedence sequence:
  1. **`overrides` list**: Apply the first rule whose Match block criteria is fully satisfied by the Pod.
  2. **`podAnnotations` map**: Match on annotations in `pod.Annotations`.
  3. **`podLabels` map**: Match on labels in `pod.Labels`.
  4. **`computeClasses` map**: Match on `pod.Spec.NodeSelector["cloud.google.com/compute-class"]`.
  5. **Global defaults**: Default fallback.
- [ ] Implement direct fallback: if a matched sub-config is missing fields (e.g., no custom `bounds`), fill them in **directly from the top-level global defaults**.
- [ ] Integration: update `Reconcile` to use the resolved config values for ratio calculations and clamping.
- [ ] Add envtest integration tests in `internal/controller/pod_controller_test.go` verifying that:
  - [ ] An override match is resolved and applied with correct precedence.
  - [ ] Direct maps (`computeClasses`, `podLabels`, `podAnnotations`) are resolved correctly.
  - [ ] Missing fields are filled in from the global defaults.
  - [ ] Backward compatibility remains intact (bare pods or non-matching pods fall back to global defaults).
