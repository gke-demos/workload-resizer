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

package controller_test

import (
	"context"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/gke-demos/workload-resizer/internal/config"
	"github.com/gke-demos/workload-resizer/internal/controller"
)

var _ = Describe("PodReconciler", func() {
	var (
		ctx context.Context
		ns  string
	)

	BeforeEach(func() {
		ctx = context.Background()
		ns = "default"
		drainEvents()
	})

	It("scenario 1: pod on baseline node — no resize, applied annotation written", func() {
		node := makeNode("node-baseline", "n2d")
		mustCreate(ctx, node)
		DeferCleanup(func() { cleanup(ctx, node) })

		pod := makeOwnedPod("pod-baseline", ns, "node-baseline", "1000m", "1Gi")
		mustCreate(ctx, pod)
		DeferCleanup(func() { cleanup(ctx, pod) })

		Eventually(func(g Gomega) {
			p := getPod(ctx, "pod-baseline", ns)
			g.Expect(p.Annotations[controller.AnnotationAppliedInstanceType]).To(Equal("n2d"))
			g.Expect(p.Spec.Containers[0].Resources.Requests.Cpu().Cmp(resource.MustParse("1000m"))).To(Equal(0))
			g.Expect(p.Spec.Containers[0].Resources.Requests.Memory().Cmp(resource.MustParse("1Gi"))).To(Equal(0))
		}, 15*time.Second, 250*time.Millisecond).Should(Succeed())
	})

	It("scenario 2: pod on more powerful node — CPU reduced", func() {
		node := makeNode("node-n4", "n4")
		mustCreate(ctx, node)
		DeferCleanup(func() { cleanup(ctx, node) })

		pod := makeOwnedPod("pod-n4", ns, "node-n4", "1000m", "1Gi")
		mustCreate(ctx, pod)
		DeferCleanup(func() { cleanup(ctx, pod) })

		Eventually(func(g Gomega) {
			p := getPod(ctx, "pod-n4", ns)
			g.Expect(p.Annotations[controller.AnnotationAppliedInstanceType]).To(Equal("n4"))
			// API server canonicalizes resource quantities, so compare values, not strings.
			origCPU, _ := resource.ParseQuantity(p.Annotations["workload-resizer.io/original-cpu.app"])
			g.Expect(origCPU.Cmp(resource.MustParse("1000m"))).To(Equal(0))
			origMem, _ := resource.ParseQuantity(p.Annotations["workload-resizer.io/original-memory.app"])
			g.Expect(origMem.Cmp(resource.MustParse("1Gi"))).To(Equal(0))
			// 1000m * 1.0 / 1.25 = 800m
			g.Expect(p.Spec.Containers[0].Resources.Requests.Cpu().Cmp(resource.MustParse("800m"))).To(Equal(0))
			// memory unchanged (memPerf 1.0 / 1.0)
			g.Expect(p.Spec.Containers[0].Resources.Requests.Memory().Cmp(resource.MustParse("1Gi"))).To(Equal(0))
			// QoS preservation: limits should also have been resized to 800m.
			g.Expect(p.Spec.Containers[0].Resources.Limits.Cpu().Cmp(resource.MustParse("800m"))).To(Equal(0))
		}, 15*time.Second, 250*time.Millisecond).Should(Succeed())
	})

	It("scenario 3: pod on less powerful node — CPU increased", func() {
		node := makeNode("node-tiny", "tiny")
		mustCreate(ctx, node)
		DeferCleanup(func() { cleanup(ctx, node) })

		pod := makeOwnedPod("pod-tiny", ns, "node-tiny", "1000m", "1Gi")
		mustCreate(ctx, pod)
		DeferCleanup(func() { cleanup(ctx, pod) })

		Eventually(func(g Gomega) {
			p := getPod(ctx, "pod-tiny", ns)
			g.Expect(p.Annotations[controller.AnnotationAppliedInstanceType]).To(Equal("tiny"))
			// 1000m * 1.0 / 0.5 = 2000m
			g.Expect(p.Spec.Containers[0].Resources.Requests.Cpu().Cmp(resource.MustParse("2"))).To(Equal(0))
		}, 15*time.Second, 250*time.Millisecond).Should(Succeed())
	})

	It("scenario 4: pod on unknown node type — skip + UnknownNodeType event", func() {
		node := makeNode("node-mystery", "totally-unknown-type")
		mustCreate(ctx, node)
		DeferCleanup(func() { cleanup(ctx, node) })

		pod := makeOwnedPod("pod-mystery", ns, "node-mystery", "1000m", "1Gi")
		mustCreate(ctx, pod)
		DeferCleanup(func() { cleanup(ctx, pod) })

		Eventually(func(g Gomega) {
			events := collectEvents(500 * time.Millisecond)
			found := false
			for _, e := range events {
				if strings.Contains(e, controller.EventReasonUnknownNodeType) &&
					strings.Contains(e, "totally-unknown-type") {
					found = true
				}
			}
			g.Expect(found).To(BeTrue(), "expected UnknownNodeType event mentioning the node type")
		}, 15*time.Second, 500*time.Millisecond).Should(Succeed())

		// Pod resources should be untouched and applied-instance-type annotation absent.
		p := getPod(ctx, "pod-mystery", ns)
		Expect(p.Spec.Containers[0].Resources.Requests.Cpu().Cmp(resource.MustParse("1000m"))).To(Equal(0))
		Expect(p.Annotations[controller.AnnotationAppliedInstanceType]).To(BeEmpty())
	})

	It("scenario 5: bounds clamping — desired below floor is pulled up to bounds.cpu.min", func() {
		// huge perf is 100x baseline, so 1000m * 1.0 / 100.0 = 10m, which
		// is below the configured cpu.min of 50m. Expect 50m after reconcile.
		node := makeNode("node-huge", "huge")
		mustCreate(ctx, node)
		DeferCleanup(func() { cleanup(ctx, node) })

		pod := makeOwnedPod("pod-huge", ns, "node-huge", "1000m", "1Gi")
		mustCreate(ctx, pod)
		DeferCleanup(func() { cleanup(ctx, pod) })

		Eventually(func(g Gomega) {
			p := getPod(ctx, "pod-huge", ns)
			g.Expect(p.Annotations[controller.AnnotationAppliedInstanceType]).To(Equal("huge"))
			g.Expect(p.Spec.Containers[0].Resources.Requests.Cpu().Cmp(resource.MustParse("50m"))).To(Equal(0))
		}, 15*time.Second, 250*time.Millisecond).Should(Succeed())
	})

	It("scenario 6: controller restart idempotency — annotation present, no double-resize", func() {
		// Simulate the post-restart scenario: pod already has original-* annotations
		// recording its true template values, but spec already reflects a previous
		// resize. The controller must compute desired from the annotation, not from
		// current spec.
		node := makeNode("node-n4-2", "n4")
		mustCreate(ctx, node)
		DeferCleanup(func() { cleanup(ctx, node) })

		// Pod looks like one that was already resized (cpu = 800m) but for which
		// we never finished writing the applied-instance-type annotation — i.e.,
		// the controller crashed between step 2 and step 3.
		pod := makeOwnedPod("pod-restart", ns, "node-n4-2", "800m", "1Gi")
		pod.Annotations = map[string]string{
			"workload-resizer.io/original-cpu.app":    "1000m",
			"workload-resizer.io/original-memory.app": "1Gi",
		}
		mustCreate(ctx, pod)
		DeferCleanup(func() { cleanup(ctx, pod) })

		// Expected: applied-instance-type written; cpu still 800m (no double-resize
		// to 640m, which would be 800m * 1.0 / 1.25).
		Eventually(func(g Gomega) {
			p := getPod(ctx, "pod-restart", ns)
			g.Expect(p.Annotations[controller.AnnotationAppliedInstanceType]).To(Equal("n4"))
			g.Expect(p.Spec.Containers[0].Resources.Requests.Cpu().Cmp(resource.MustParse("800m"))).To(Equal(0))
			// Originals annotation must remain unchanged (still 1000m, not overwritten with 800m).
			g.Expect(p.Annotations["workload-resizer.io/original-cpu.app"]).To(Equal("1000m"))
		}, 15*time.Second, 250*time.Millisecond).Should(Succeed())
	})

	It("scenario 7: direct computeClasses matching", func() {
		cfg := testConfig()
		cfg.ComputeClasses = map[string]config.ComputeClassConfig{
			"Performance": {
				BaselineNodeType: "c3",
				NodeTypes: map[string]config.NodeProfile{
					"c3": {CPUPerf: 2.0, MemPerf: 1.0},
				},
			},
		}
		store.Set(cfg)
		DeferCleanup(func() { store.Set(testConfig()) })

		node := makeNode("node-cc-n4", "n4")
		mustCreate(ctx, node)
		DeferCleanup(func() { cleanup(ctx, node) })

		pod := makeOwnedPod("pod-cc-perf", ns, "node-cc-n4", "1000m", "1Gi")
		pod.Spec.NodeSelector = map[string]string{
			"cloud.google.com/compute-class": "Performance",
		}
		mustCreate(ctx, pod)
		DeferCleanup(func() { cleanup(ctx, pod) })

		Eventually(func(g Gomega) {
			p := getPod(ctx, "pod-cc-perf", ns)
			g.Expect(p.Annotations[controller.AnnotationAppliedInstanceType]).To(Equal("n4"))
			// Desired CPU: 1000m * 2.0 (baseline c3 perf) / 1.25 (node n4 perf) = 1600m
			g.Expect(p.Spec.Containers[0].Resources.Requests.Cpu().Cmp(resource.MustParse("1600m"))).To(Equal(0))
		}, 15*time.Second, 250*time.Millisecond).Should(Succeed())
	})

	It("scenario 8: direct podLabels matching and bounds override", func() {
		cfg := testConfig()
		cfg.PodLabels = map[string]map[string]config.LabelConfig{
			"env": {
				"sandbox": {
					Bounds: &config.Bounds{
						CPU:    config.Bound{Min: resource.MustParse("10m"), Max: resource.MustParse("100m")},
						Memory: config.Bound{Min: resource.MustParse("16Mi"), Max: resource.MustParse("256Mi")},
					},
				},
			},
		}
		store.Set(cfg)
		DeferCleanup(func() { store.Set(testConfig()) })

		node := makeNode("node-labels-n4", "n4")
		mustCreate(ctx, node)
		DeferCleanup(func() { cleanup(ctx, node) })

		pod := makeOwnedPod("pod-label-sandbox", ns, "node-labels-n4", "1000m", "1Gi")
		pod.Labels = map[string]string{
			"env": "sandbox",
		}
		mustCreate(ctx, pod)
		DeferCleanup(func() { cleanup(ctx, pod) })

		Eventually(func(g Gomega) {
			p := getPod(ctx, "pod-label-sandbox", ns)
			g.Expect(p.Annotations[controller.AnnotationAppliedInstanceType]).To(Equal("n4"))
			// 1000m * 1.0 (baseline) / 1.25 (node n4) = 800m.
			// However, bounds.cpu.max for sandbox is 100m.
			// So it should clamp to 100m!
			g.Expect(p.Spec.Containers[0].Resources.Requests.Cpu().Cmp(resource.MustParse("100m"))).To(Equal(0))
		}, 15*time.Second, 250*time.Millisecond).Should(Succeed())
	})

	It("scenario 9: direct podAnnotations matching and fallback to global defaults", func() {
		cfg := testConfig()
		cfg.PodAnnotations = map[string]map[string]config.AnnotationConfig{
			"workload-resizer.io/tier": {
				"critical-db": {
					Bounds: &config.Bounds{
						CPU:    config.Bound{Min: resource.MustParse("1500m"), Max: resource.MustParse("16")},
						Memory: config.Bound{Min: resource.MustParse("1Gi"), Max: resource.MustParse("32Gi")},
					},
				},
			},
		}
		store.Set(cfg)
		DeferCleanup(func() { store.Set(testConfig()) })

		node := makeNode("node-annos-baseline", "n2d")
		mustCreate(ctx, node)
		DeferCleanup(func() { cleanup(ctx, node) })

		// On baseline node (n2d), CPU requested is 1000m.
		// However, annotations-matched bounds.cpu.min is 1500m.
		// It should be pulled up to 1500m!
		pod := makeOwnedPod("pod-anno-db", ns, "node-annos-baseline", "1000m", "1Gi")
		pod.Annotations = map[string]string{
			"workload-resizer.io/tier": "critical-db",
		}
		mustCreate(ctx, pod)
		DeferCleanup(func() { cleanup(ctx, pod) })

		Eventually(func(g Gomega) {
			p := getPod(ctx, "pod-anno-db", ns)
			g.Expect(p.Annotations[controller.AnnotationAppliedInstanceType]).To(Equal("n2d"))
			g.Expect(p.Spec.Containers[0].Resources.Requests.Cpu().Cmp(resource.MustParse("1500m"))).To(Equal(0))
		}, 15*time.Second, 250*time.Millisecond).Should(Succeed())
	})

	It("scenario 10: prioritized overrides precedence", func() {
		cfg := testConfig()
		cfg.ComputeClasses = map[string]config.ComputeClassConfig{
			"Performance": {
				Bounds: &config.Bounds{
					CPU:    config.Bound{Min: resource.MustParse("200m"), Max: resource.MustParse("32")},
					Memory: config.Bound{Min: resource.MustParse("64Mi"), Max: resource.MustParse("32Gi")},
				},
			},
		}
		cfg.Overrides = []config.Override{
			{
				Name: "high-priority-override",
				Match: config.Match{
					ComputeClass: "Performance",
					PodLabel: map[string]string{
						"priority": "critical",
					},
				},
				Bounds: &config.Bounds{
					CPU:    config.Bound{Min: resource.MustParse("1000m"), Max: resource.MustParse("16")},
					Memory: config.Bound{Min: resource.MustParse("1Gi"), Max: resource.MustParse("32Gi")},
				},
			},
		}
		store.Set(cfg)
		DeferCleanup(func() { store.Set(testConfig()) })

		node := makeNode("node-overrides-baseline", "n2d")
		mustCreate(ctx, node)
		DeferCleanup(func() { cleanup(ctx, node) })

		// Pod has nodeSelector for ComputeClass = Performance, and label priority = critical.
		// It matches both ComputeClasses map and Overrides.
		// Since Overrides takes precedence, it should apply Overrides bounds (CPU min 1000m)
		// rather than ComputeClasses bounds (CPU min 200m).
		// Original requested CPU is 100m.
		// Overrides bounds.cpu.min = 1000m -> clamped to 1000m.
		// ComputeClasses bounds.cpu.min = 200m -> if it applied, clamped to 200m.
		pod := makeOwnedPod("pod-override-prec", ns, "node-overrides-baseline", "100m", "2Gi")
		pod.Labels = map[string]string{
			"priority": "critical",
		}
		pod.Spec.NodeSelector = map[string]string{
			"cloud.google.com/compute-class": "Performance",
		}
		mustCreate(ctx, pod)
		DeferCleanup(func() { cleanup(ctx, pod) })

		Eventually(func(g Gomega) {
			p := getPod(ctx, "pod-override-prec", ns)
			g.Expect(p.Annotations[controller.AnnotationAppliedInstanceType]).To(Equal("n2d"))
			g.Expect(p.Spec.Containers[0].Resources.Requests.Cpu().Cmp(resource.MustParse("1000m"))).To(Equal(0))
		}, 15*time.Second, 250*time.Millisecond).Should(Succeed())
	})
})

var _ = Describe("podPredicate edge cases", func() {
	var (
		ctx context.Context
		ns  string
	)

	BeforeEach(func() {
		ctx = context.Background()
		ns = "default"
		drainEvents()
	})

	It("skips bare pods (no controller owner)", func() {
		node := makeNode("node-bare", "n4")
		mustCreate(ctx, node)
		DeferCleanup(func() { cleanup(ctx, node) })

		pod := makeOwnedPod("pod-bare", ns, "node-bare", "1000m", "1Gi")
		pod.OwnerReferences = nil
		mustCreate(ctx, pod)
		DeferCleanup(func() { cleanup(ctx, pod) })

		// Wait long enough that a real reconcile would have happened, then assert nothing changed.
		Consistently(func(g Gomega) {
			p := getPod(ctx, "pod-bare", ns)
			g.Expect(p.Annotations[controller.AnnotationAppliedInstanceType]).To(BeEmpty())
			g.Expect(p.Spec.Containers[0].Resources.Requests.Cpu().Cmp(resource.MustParse("1000m"))).To(Equal(0))
		}, 3*time.Second, 250*time.Millisecond).Should(Succeed())
	})

	It("skips pods carrying the opt-out annotation", func() {
		node := makeNode("node-skip", "n4")
		mustCreate(ctx, node)
		DeferCleanup(func() { cleanup(ctx, node) })

		// n4 is in the test config and is more powerful than baseline, so
		// without the skip annotation this pod would resize from 1000m to
		// 800m. With the annotation it must not be touched.
		pod := makeOwnedPod("pod-skip", ns, "node-skip", "1000m", "1Gi")
		pod.Annotations = map[string]string{controller.AnnotationSkip: "true"}
		mustCreate(ctx, pod)
		DeferCleanup(func() { cleanup(ctx, pod) })

		Consistently(func(g Gomega) {
			p := getPod(ctx, "pod-skip", ns)
			g.Expect(p.Annotations[controller.AnnotationAppliedInstanceType]).To(BeEmpty())
			g.Expect(p.Spec.Containers[0].Resources.Requests.Cpu().Cmp(resource.MustParse("1000m"))).To(Equal(0))
		}, 3*time.Second, 250*time.Millisecond).Should(Succeed())
	})

	It("skips containers without requests", func() {
		node := makeNode("node-noreq", "n4")
		mustCreate(ctx, node)
		DeferCleanup(func() { cleanup(ctx, node) })

		pod := makeOwnedPod("pod-noreq", ns, "node-noreq", "1000m", "1Gi")
		// Wipe out resources entirely.
		pod.Spec.Containers[0].Resources = corev1.ResourceRequirements{}
		mustCreate(ctx, pod)
		DeferCleanup(func() { cleanup(ctx, pod) })

		Consistently(func(g Gomega) {
			p := getPod(ctx, "pod-noreq", ns)
			g.Expect(p.Annotations[controller.AnnotationAppliedInstanceType]).To(BeEmpty())
		}, 3*time.Second, 250*time.Millisecond).Should(Succeed())
	})
})
