/*
Copyright 2026 Google LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/gke-demos/workload-resizer/internal/config"
	"github.com/gke-demos/workload-resizer/internal/resize"
)

const (
	AnnotationPrefix              = "workload-resizer.io/"
	AnnotationOriginalCPUFmt      = AnnotationPrefix + "original-cpu.%s"
	AnnotationOriginalMemoryFmt   = AnnotationPrefix + "original-memory.%s"
	AnnotationAppliedInstanceType = AnnotationPrefix + "applied-instance-type"
	AnnotationAppliedAt           = AnnotationPrefix + "applied-at"
	// AnnotationSkip opts a pod out of resize. Set to "true" on the
	// pod template (workloads inherit from Deployment / StatefulSet /
	// DaemonSet / Job specs). The controller's own pod ships with
	// this annotation so it never tries to resize itself.
	AnnotationSkip = AnnotationPrefix + "skip"

	// DefaultNodeTypeLabel is the node label whose value identifies the
	// "node type" the controller looks up in its config. GKE sets
	// cloud.google.com/machine-family to the machine family (e.g., "n2d",
	// "n4", "c3"), which is the right granularity for performance-unit
	// lookups — perf per core is constant across sizes within a family.
	// Override per cluster via the --node-type-label flag.
	DefaultNodeTypeLabel = "cloud.google.com/machine-family"

	EventReasonResized           = "Resized"
	EventReasonAlreadyAligned    = "AlreadyAligned"
	EventReasonUnknownNodeType   = "UnknownNodeType"
	EventReasonResizeFailed      = "ResizeFailed"
	EventReasonResizeUnsupported = "ResizeUnsupported"
	EventReasonBoundsClamped     = "BoundsClamped"
)

// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;patch
// +kubebuilder:rbac:groups=core,resources=pods/resize,verbs=patch
// +kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;patch

type PodReconciler struct {
	client.Client
	// APIReader bypasses the controller-runtime cache. Used for Node lookups so
	// we don't depend on the cache having an informer for Nodes (the controller
	// only watches Pods).
	APIReader client.Reader
	Recorder  record.EventRecorder
	Config    *config.Store
	// NodeTypeLabel is the node label whose value the controller looks up
	// in cfg.NodeTypes. Empty falls back to DefaultNodeTypeLabel.
	NodeTypeLabel string
}

func (r *PodReconciler) nodeTypeLabel() string {
	if r.NodeTypeLabel != "" {
		return r.NodeTypeLabel
	}
	return DefaultNodeTypeLabel
}

type containerDecision struct {
	name            string
	originalCPU     resource.Quantity
	originalMemory  resource.Quantity
	desiredCPU      resource.Quantity
	desiredMemory   resource.Quantity
	cpuClampedAtMin bool
	cpuClampedAtMax bool
	memClampedAtMin bool
	memClampedAtMax bool

	// hadCPULimit / hadMemoryLimit indicate the original spec had a limit set;
	// if true we mirror the request change into the limit to preserve the pod's
	// QoS class (the API server rejects resize requests that change QoS).
	hadCPULimit    bool
	hadMemoryLimit bool
}

func (r *PodReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var pod corev1.Pod
	if err := r.Get(ctx, req.NamespacedName, &pod); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if pod.Spec.NodeName == "" || !hasControllerOwner(&pod) || isSkipped(&pod) {
		return ctrl.Result{}, nil
	}

	cfg := r.Config.Get()
	if cfg == nil {
		// Config hasn't loaded yet; requeue shortly.
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	var node corev1.Node
	if err := r.APIReader.Get(ctx, types.NamespacedName{Name: pod.Spec.NodeName}, &node); err != nil {
		return ctrl.Result{}, fmt.Errorf("get node %q: %w", pod.Spec.NodeName, err)
	}

	labelKey := r.nodeTypeLabel()
	nodeType := node.Labels[labelKey]
	if nodeType == "" {
		// Node has no node-type label (e.g., bare Kind nodes, or a
		// non-GKE cluster where --node-type-label hasn't been pointed at
		// whatever your provider uses). Nothing to compute against.
		return ctrl.Result{}, nil
	}

	if pod.Annotations[AnnotationAppliedInstanceType] == nodeType {
		// Already aligned for this node type.
		return ctrl.Result{}, nil
	}

	nodeProfile, ok := cfg.NodeTypes[nodeType]
	if !ok {
		r.Recorder.Eventf(&pod, corev1.EventTypeWarning, EventReasonUnknownNodeType,
			"Node type %q not in workload-resizer config; skipping", nodeType)
		return ctrl.Result{}, nil
	}
	baselineProfile := cfg.NodeTypes[cfg.BaselineNodeType]

	decisions := make([]containerDecision, 0, len(pod.Spec.Containers))
	for i := range pod.Spec.Containers {
		c := &pod.Spec.Containers[i]
		origCPU, hasCPU := readOriginalCPU(&pod, c)
		origMem, hasMem := readOriginalMemory(&pod, c)
		if !hasCPU && !hasMem {
			continue
		}
		_, hadCPULimit := c.Resources.Limits[corev1.ResourceCPU]
		_, hadMemLimit := c.Resources.Limits[corev1.ResourceMemory]
		d := containerDecision{
			name:           c.Name,
			originalCPU:    origCPU,
			originalMemory: origMem,
			hadCPULimit:    hadCPULimit,
			hadMemoryLimit: hadMemLimit,
		}
		if hasCPU {
			scaled := resize.ApplyCPURatio(origCPU, baselineProfile.CPUPerf, nodeProfile.CPUPerf)
			d.desiredCPU, d.cpuClampedAtMin, d.cpuClampedAtMax = resize.Clamp(scaled, cfg.Bounds.CPU.Min, cfg.Bounds.CPU.Max)
		}
		if hasMem {
			scaled := resize.ApplyMemoryRatio(origMem, baselineProfile.MemPerf, nodeProfile.MemPerf)
			d.desiredMemory, d.memClampedAtMin, d.memClampedAtMax = resize.Clamp(scaled, cfg.Bounds.Memory.Min, cfg.Bounds.Memory.Max)
		}
		decisions = append(decisions, d)
	}

	if len(decisions) == 0 {
		// No containers had requests we can act on.
		return ctrl.Result{}, nil
	}

	// Step 1: persist originals (idempotent — only writes keys that are missing).
	if err := r.writeOriginalsAnnotation(ctx, &pod, decisions); err != nil {
		return ctrl.Result{}, fmt.Errorf("write originals annotation: %w", err)
	}

	// Step 2: resize via /resize subresource if anything actually changed.
	if changed := needsResize(&pod, decisions); changed {
		if err := r.applyResize(ctx, &pod, decisions); err != nil {
			// "Pod running on node without support for resize" comes from the API
			// server when the node hasn't advertised the InPlacePodVerticalScaling
			// feature (kubelet sets pod.status.containerStatuses[*].resources to
			// signal support). Treat as terminal-skip: emit event, don't requeue.
			// We'll re-attempt the next time the pod gets a status update.
			if isResizeUnsupportedError(err) {
				r.Recorder.Eventf(&pod, corev1.EventTypeWarning, EventReasonResizeUnsupported,
					"Node %q does not currently advertise in-place resize support; skipping", pod.Spec.NodeName)
				return ctrl.Result{}, nil
			}
			r.Recorder.Eventf(&pod, corev1.EventTypeWarning, EventReasonResizeFailed,
				"In-place resize failed: %v", err)
			return ctrl.Result{}, err
		}
		r.Recorder.Eventf(&pod, corev1.EventTypeNormal, EventReasonResized,
			"Resized for node type %q (baseline %q)", nodeType, cfg.BaselineNodeType)
	} else {
		r.Recorder.Eventf(&pod, corev1.EventTypeNormal, EventReasonAlreadyAligned,
			"Pod resources already match desired for node type %q", nodeType)
	}

	for _, d := range decisions {
		if d.cpuClampedAtMin || d.cpuClampedAtMax || d.memClampedAtMin || d.memClampedAtMax {
			r.Recorder.Eventf(&pod, corev1.EventTypeNormal, EventReasonBoundsClamped,
				"Container %q desired values clamped to configured bounds", d.name)
			break
		}
	}

	// Step 3: record what we sized for.
	if err := r.writeAppliedAnnotation(ctx, &pod, nodeType); err != nil {
		return ctrl.Result{}, fmt.Errorf("write applied annotation: %w", err)
	}

	logger.V(1).Info("reconciled", "pod", req.NamespacedName, "nodeType", nodeType)
	return ctrl.Result{}, nil
}

func (r *PodReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("pod-resizer").
		For(&corev1.Pod{}, builder.WithPredicates(podPredicate())).
		Complete(r)
}

func podPredicate() predicate.Predicate {
	interesting := func(p *corev1.Pod) bool {
		return p != nil &&
			p.Spec.NodeName != "" &&
			hasControllerOwner(p) &&
			!isSkipped(p) &&
			p.Annotations[AnnotationAppliedInstanceType] == ""
	}
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			p, _ := e.Object.(*corev1.Pod)
			return interesting(p)
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldP, _ := e.ObjectOld.(*corev1.Pod)
			newP, _ := e.ObjectNew.(*corev1.Pod)
			if interesting(newP) {
				return true
			}
			// Re-check if the applied-instance-type no longer matches reality after a node move
			// (rare, but covers any edge cases where binding changes on the same pod object).
			if oldP != nil && newP != nil && oldP.Spec.NodeName != newP.Spec.NodeName && newP.Spec.NodeName != "" {
				return hasControllerOwner(newP)
			}
			return false
		},
		DeleteFunc:  func(_ event.DeleteEvent) bool { return false },
		GenericFunc: func(_ event.GenericEvent) bool { return false },
	}
}

// isResizeUnsupportedError matches the K8s 1.35+ API server validation that
// rejects /resize requests when pod.status.containerStatuses[*].resources is
// nil — i.e., the kubelet on the assigned node hasn't signalled support for
// the InPlacePodVerticalScaling feature.
func isResizeUnsupportedError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "Pod running on node without support for resize")
}

// isSkipped reports whether the pod carries the opt-out annotation
// (workload-resizer.io/skip=true). Used both in the predicate (avoid
// even enqueuing the pod) and in Reconcile as a defense in depth.
func isSkipped(p *corev1.Pod) bool {
	return p != nil && p.Annotations[AnnotationSkip] == "true"
}

func hasControllerOwner(p *corev1.Pod) bool {
	for _, ref := range p.OwnerReferences {
		if ref.Controller == nil || !*ref.Controller {
			continue
		}
		switch ref.Kind {
		case "ReplicaSet", "StatefulSet", "DaemonSet", "Job":
			return true
		}
	}
	return false
}

// readOriginalCPU returns the original CPU request — either from the persisted
// annotation (if we've resized this pod before) or from the pod spec.
func readOriginalCPU(pod *corev1.Pod, c *corev1.Container) (resource.Quantity, bool) {
	if v, ok := pod.Annotations[fmt.Sprintf(AnnotationOriginalCPUFmt, c.Name)]; ok {
		q, err := resource.ParseQuantity(v)
		if err == nil {
			return q, true
		}
	}
	q, ok := c.Resources.Requests[corev1.ResourceCPU]
	if !ok {
		return resource.Quantity{}, false
	}
	return q, true
}

func readOriginalMemory(pod *corev1.Pod, c *corev1.Container) (resource.Quantity, bool) {
	if v, ok := pod.Annotations[fmt.Sprintf(AnnotationOriginalMemoryFmt, c.Name)]; ok {
		q, err := resource.ParseQuantity(v)
		if err == nil {
			return q, true
		}
	}
	q, ok := c.Resources.Requests[corev1.ResourceMemory]
	if !ok {
		return resource.Quantity{}, false
	}
	return q, true
}

func needsResize(pod *corev1.Pod, decisions []containerDecision) bool {
	for _, d := range decisions {
		c := findContainer(pod, d.name)
		if c == nil {
			continue
		}
		cur := c.Resources.Requests
		if !d.desiredCPU.IsZero() && cur.Cpu().Cmp(d.desiredCPU) != 0 {
			return true
		}
		if !d.desiredMemory.IsZero() && cur.Memory().Cmp(d.desiredMemory) != 0 {
			return true
		}
	}
	return false
}

func findContainer(pod *corev1.Pod, name string) *corev1.Container {
	for i := range pod.Spec.Containers {
		if pod.Spec.Containers[i].Name == name {
			return &pod.Spec.Containers[i]
		}
	}
	return nil
}

func (r *PodReconciler) writeOriginalsAnnotation(ctx context.Context, pod *corev1.Pod, decisions []containerDecision) error {
	patch := map[string]any{}
	annos := map[string]string{}
	for _, d := range decisions {
		cpuKey := fmt.Sprintf(AnnotationOriginalCPUFmt, d.name)
		memKey := fmt.Sprintf(AnnotationOriginalMemoryFmt, d.name)
		if _, ok := pod.Annotations[cpuKey]; !ok && !d.originalCPU.IsZero() {
			annos[cpuKey] = d.originalCPU.String()
		}
		if _, ok := pod.Annotations[memKey]; !ok && !d.originalMemory.IsZero() {
			annos[memKey] = d.originalMemory.String()
		}
	}
	if len(annos) == 0 {
		return nil
	}
	patch["metadata"] = map[string]any{"annotations": annos}
	body, err := json.Marshal(patch)
	if err != nil {
		return err
	}
	return r.Patch(ctx, pod, client.RawPatch(types.MergePatchType, body))
}

func (r *PodReconciler) writeAppliedAnnotation(ctx context.Context, pod *corev1.Pod, nodeType string) error {
	patch := map[string]any{
		"metadata": map[string]any{
			"annotations": map[string]string{
				AnnotationAppliedInstanceType: nodeType,
				AnnotationAppliedAt:           time.Now().UTC().Format(time.RFC3339),
			},
		},
	}
	body, err := json.Marshal(patch)
	if err != nil {
		return err
	}
	return r.Patch(ctx, pod, client.RawPatch(types.MergePatchType, body))
}

func (r *PodReconciler) applyResize(ctx context.Context, pod *corev1.Pod, decisions []containerDecision) error {
	containers := make([]map[string]any, 0, len(decisions))
	for _, d := range decisions {
		req := map[string]string{}
		lim := map[string]string{}
		if !d.desiredCPU.IsZero() {
			req["cpu"] = d.desiredCPU.String()
			// Mirror to limit if one was originally set, to preserve the pod's
			// QoS class — the API server rejects resize requests that would
			// change QoS (e.g., Guaranteed → Burstable).
			if d.hadCPULimit {
				lim["cpu"] = d.desiredCPU.String()
			}
		}
		if !d.desiredMemory.IsZero() {
			req["memory"] = d.desiredMemory.String()
			if d.hadMemoryLimit {
				lim["memory"] = d.desiredMemory.String()
			}
		}
		if len(req) == 0 && len(lim) == 0 {
			continue
		}
		resources := map[string]any{}
		if len(req) > 0 {
			resources["requests"] = req
		}
		if len(lim) > 0 {
			resources["limits"] = lim
		}
		containers = append(containers, map[string]any{
			"name":      d.name,
			"resources": resources,
		})
	}
	patch := map[string]any{"spec": map[string]any{"containers": containers}}
	body, err := json.Marshal(patch)
	if err != nil {
		return err
	}
	return r.SubResource("resize").Patch(ctx, pod, client.RawPatch(types.StrategicMergePatchType, body))
}
