//go:build e2e
// +build e2e

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

package e2e

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/gke-demos/workload-resizer/test/utils"
)

// configMapManifest is the test config the controller will load. Designed so:
//   - n2d-standard-4 is the baseline (perf 1.0)
//   - n4-standard-4 is more powerful (perf 1.25 → CPU shrinks 20%)
//   - tiny-machine is less powerful (perf 0.5 → CPU doubles)
//   - huge-machine forces a clamp at the cpu.min floor
//   - cpu.min = 50m, cpu.max = 16
const configMapManifest = `
apiVersion: v1
kind: ConfigMap
metadata:
  name: workload-resizer-config
  namespace: workload-resizer-system
data:
  config.yaml: |
    baselineInstanceType: n2d-standard-4
    nodeTypes:
      n2d-standard-4: { cpuPerf: 1.0,   memPerf: 1.0 }
      n4-standard-4:  { cpuPerf: 1.25,  memPerf: 1.0 }
      tiny-machine:   { cpuPerf: 0.5,   memPerf: 1.0 }
      huge-machine:   { cpuPerf: 100.0, memPerf: 100.0 }
    bounds:
      cpu:    { min: "50m",  max: "16" }
      memory: { min: "64Mi", max: "32Gi" }
`

func deploymentManifest(name, instanceType string) string {
	return fmt.Sprintf(`
apiVersion: apps/v1
kind: Deployment
metadata:
  name: %s
  namespace: default
  labels:
    app: %s
spec:
  replicas: 1
  selector:
    matchLabels:
      app: %s
  template:
    metadata:
      labels:
        app: %s
    spec:
      nodeSelector:
        type: kwok
        node.kubernetes.io/instance-type: %s
      tolerations:
      - key: kwok.x-k8s.io/node
        operator: Exists
        effect: NoSchedule
      containers:
      - name: app
        image: registry.k8s.io/pause:3.10
        resources:
          requests:
            cpu: "1000m"
            memory: "1Gi"
          limits:
            cpu: "1000m"
            memory: "1Gi"
        resizePolicy:
        - resourceName: cpu
          restartPolicy: NotRequired
        - resourceName: memory
          restartPolicy: NotRequired
`, name, name, name, name, instanceType)
}

func kubectlApply(manifest string) error {
	cmd := exec.Command("kubectl", "apply", "-f", "-")
	cmd.Stdin = strings.NewReader(manifest)
	_, err := utils.Run(cmd)
	return err
}

func deleteDeployment(name string) {
	cmd := exec.Command("kubectl", "delete", "deployment", name, "-n", "default", "--ignore-not-found", "--wait=false")
	_, _ = utils.Run(cmd)
}

func getPodForApp(label string) (*corev1.Pod, error) {
	cmd := exec.Command("kubectl", "get", "pods", "-n", "default", "-l", "app="+label, "-o", "json")
	out, err := utils.Run(cmd)
	if err != nil {
		return nil, err
	}
	var list corev1.PodList
	if err := json.Unmarshal([]byte(out), &list); err != nil {
		return nil, fmt.Errorf("unmarshal pod list: %w", err)
	}
	if len(list.Items) == 0 {
		return nil, fmt.Errorf("no pods found for app=%s", label)
	}
	return &list.Items[0], nil
}

func eventsForPod(podName string) (string, error) {
	cmd := exec.Command("kubectl", "get", "events", "-n", "default",
		"--field-selector", "involvedObject.name="+podName,
		"-o", "jsonpath={range .items[*]}{.reason}|{.message}{\"\\n\"}{end}")
	return utils.Run(cmd)
}

// declareKWOKPodResizeSupport patches the pod's status to add
// containerStatuses[*].resources, which is what the K8s 1.35+ API server
// uses to determine that the node supports in-place resize. KWOK's default
// pod-ready stage doesn't populate this field, so we do it manually here.
//
// The actual values written don't matter for the API server's validation
// (it only checks `resources != nil`), but we mirror the spec for realism.
func declareKWOKPodResizeSupport(podName string) error {
	patch := `{"status":{"containerStatuses":[{"name":"app","resources":{"requests":{"cpu":"1000m","memory":"1Gi"},"limits":{"cpu":"1000m","memory":"1Gi"}}}]}}`
	cmd := exec.Command("kubectl", "patch", "pod", podName, "-n", "default",
		"--subresource=status", "--type=merge", "-p", patch)
	_, err := utils.Run(cmd)
	return err
}

// waitForRunningPodAndDeclareResize waits for the deployment's pod to reach
// Running, then patches its status so the controller's resize will be allowed.
func waitForRunningPodAndDeclareResize(label string) {
	GinkgoHelper()
	Eventually(func(g Gomega) {
		p, err := getPodForApp(label)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(p.Status.Phase).To(Equal(corev1.PodRunning))
		g.Expect(declareKWOKPodResizeSupport(p.Name)).To(Succeed())
	}, 60*time.Second, 1*time.Second).Should(Succeed())
}

var _ = Describe("Resize", Ordered, func() {
	BeforeAll(func() {
		By("applying the workload-resizer ConfigMap")
		Expect(kubectlApply(configMapManifest)).To(Succeed())
		// Give the controller a moment to refresh its in-memory config.
		time.Sleep(35 * time.Second)
	})

	AfterEach(func() {
		if !CurrentSpecReport().Failed() {
			return
		}
		_, _ = fmt.Fprintf(GinkgoWriter, "\n=== DEBUG: pods in default ns ===\n")
		out, _ := utils.Run(exec.Command("kubectl", "get", "pods", "-n", "default", "-o", "wide"))
		_, _ = fmt.Fprintln(GinkgoWriter, out)

		_, _ = fmt.Fprintf(GinkgoWriter, "\n=== DEBUG: nodes ===\n")
		out, _ = utils.Run(exec.Command("kubectl", "get", "nodes",
			"-L", "node.kubernetes.io/instance-type", "-L", "type"))
		_, _ = fmt.Fprintln(GinkgoWriter, out)

		_, _ = fmt.Fprintf(GinkgoWriter, "\n=== DEBUG: controller logs (last 100 lines) ===\n")
		out, _ = utils.Run(exec.Command("kubectl", "logs",
			"-l", "control-plane=controller-manager",
			"-n", namespace, "--tail=100"))
		_, _ = fmt.Fprintln(GinkgoWriter, out)

		_, _ = fmt.Fprintf(GinkgoWriter, "\n=== DEBUG: events in default ns (last 30) ===\n")
		out, _ = utils.Run(exec.Command("kubectl", "get", "events", "-n", "default",
			"--sort-by=.lastTimestamp"))
		_, _ = fmt.Fprintln(GinkgoWriter, out)
	})

	const (
		resizedAnnotation = "workload-resizer.io/applied-instance-type"
		origCPUAnnotation = "workload-resizer.io/original-cpu.app"
		origMemAnnotation = "workload-resizer.io/original-memory.app"
	)

	It("scenario 1: pod on baseline node — no resize, applied annotation written", func() {
		Expect(utils.CreateKWOKNode("kwok-baseline", "n2d-standard-4")).To(Succeed())
		DeferCleanup(func() { utils.DeleteKWOKNode("kwok-baseline") })
		Expect(kubectlApply(deploymentManifest("baseline-app", "n2d-standard-4"))).To(Succeed())
		DeferCleanup(func() { deleteDeployment("baseline-app") })
		waitForRunningPodAndDeclareResize("baseline-app")

		Eventually(func(g Gomega) {
			p, err := getPodForApp("baseline-app")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(p.Annotations[resizedAnnotation]).To(Equal("n2d-standard-4"))
			g.Expect(p.Spec.Containers[0].Resources.Requests.Cpu().Cmp(resource.MustParse("1000m"))).To(Equal(0))
		}, 90*time.Second, 2*time.Second).Should(Succeed())
	})

	It("scenario 2: pod on more powerful node — CPU reduced", func() {
		Expect(utils.CreateKWOKNode("kwok-n4", "n4-standard-4")).To(Succeed())
		DeferCleanup(func() { utils.DeleteKWOKNode("kwok-n4") })
		Expect(kubectlApply(deploymentManifest("n4-app", "n4-standard-4"))).To(Succeed())
		DeferCleanup(func() { deleteDeployment("n4-app") })
		waitForRunningPodAndDeclareResize("n4-app")

		Eventually(func(g Gomega) {
			p, err := getPodForApp("n4-app")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(p.Annotations[resizedAnnotation]).To(Equal("n4-standard-4"))
			origCPU, _ := resource.ParseQuantity(p.Annotations[origCPUAnnotation])
			g.Expect(origCPU.Cmp(resource.MustParse("1000m"))).To(Equal(0))
			origMem, _ := resource.ParseQuantity(p.Annotations[origMemAnnotation])
			g.Expect(origMem.Cmp(resource.MustParse("1Gi"))).To(Equal(0))
			g.Expect(p.Spec.Containers[0].Resources.Requests.Cpu().Cmp(resource.MustParse("800m"))).To(Equal(0))
			// QoS preservation: limits also resized.
			g.Expect(p.Spec.Containers[0].Resources.Limits.Cpu().Cmp(resource.MustParse("800m"))).To(Equal(0))
		}, 90*time.Second, 2*time.Second).Should(Succeed())
	})

	It("scenario 3: pod on less powerful node — CPU increased", func() {
		Expect(utils.CreateKWOKNode("kwok-tiny", "tiny-machine")).To(Succeed())
		DeferCleanup(func() { utils.DeleteKWOKNode("kwok-tiny") })
		Expect(kubectlApply(deploymentManifest("tiny-app", "tiny-machine"))).To(Succeed())
		DeferCleanup(func() { deleteDeployment("tiny-app") })
		waitForRunningPodAndDeclareResize("tiny-app")

		Eventually(func(g Gomega) {
			p, err := getPodForApp("tiny-app")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(p.Annotations[resizedAnnotation]).To(Equal("tiny-machine"))
			// 1000m * 1.0 / 0.5 = 2000m
			g.Expect(p.Spec.Containers[0].Resources.Requests.Cpu().Cmp(resource.MustParse("2"))).To(Equal(0))
		}, 90*time.Second, 2*time.Second).Should(Succeed())
	})

	It("scenario 4: pod on unknown node type — skip + UnknownNodeType event", func() {
		Expect(utils.CreateKWOKNode("kwok-unknown", "totally-unknown-type")).To(Succeed())
		DeferCleanup(func() { utils.DeleteKWOKNode("kwok-unknown") })
		Expect(kubectlApply(deploymentManifest("unknown-app", "totally-unknown-type"))).To(Succeed())
		DeferCleanup(func() { deleteDeployment("unknown-app") })
		// No declareResizeSupport — controller skips before any resize attempt.

		var pod *corev1.Pod
		Eventually(func(g Gomega) {
			var err error
			pod, err = getPodForApp("unknown-app")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(pod.Spec.NodeName).NotTo(BeEmpty())
		}, 60*time.Second, 2*time.Second).Should(Succeed())

		Eventually(func(g Gomega) {
			out, err := eventsForPod(pod.Name)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(out).To(ContainSubstring("UnknownNodeType"))
			g.Expect(out).To(ContainSubstring("totally-unknown-type"))
		}, 90*time.Second, 2*time.Second).Should(Succeed())

		// Pod should not have the applied-instance-type annotation.
		p, err := getPodForApp("unknown-app")
		Expect(err).NotTo(HaveOccurred())
		Expect(p.Annotations[resizedAnnotation]).To(BeEmpty())
		Expect(p.Spec.Containers[0].Resources.Requests.Cpu().Cmp(resource.MustParse("1000m"))).To(Equal(0))
	})

	It("scenario 5: bounds clamping — desired below floor is pulled up to bounds.cpu.min", func() {
		Expect(utils.CreateKWOKNode("kwok-huge", "huge-machine")).To(Succeed())
		DeferCleanup(func() { utils.DeleteKWOKNode("kwok-huge") })
		Expect(kubectlApply(deploymentManifest("huge-app", "huge-machine"))).To(Succeed())
		DeferCleanup(func() { deleteDeployment("huge-app") })
		waitForRunningPodAndDeclareResize("huge-app")

		Eventually(func(g Gomega) {
			p, err := getPodForApp("huge-app")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(p.Annotations[resizedAnnotation]).To(Equal("huge-machine"))
			// 1000m * 1.0 / 100.0 = 10m, clamped up to 50m.
			g.Expect(p.Spec.Containers[0].Resources.Requests.Cpu().Cmp(resource.MustParse("50m"))).To(Equal(0))
		}, 90*time.Second, 2*time.Second).Should(Succeed())
	})

	It("scenario 6: controller restart — no double-resize", func() {
		// Same workload as scenario 2, but after the initial resize completes
		// we restart the controller and assert the pod's resources remain
		// unchanged on the second pass.
		Expect(utils.CreateKWOKNode("kwok-n4-restart", "n4-standard-4")).To(Succeed())
		DeferCleanup(func() { utils.DeleteKWOKNode("kwok-n4-restart") })
		Expect(kubectlApply(deploymentManifest("restart-app", "n4-standard-4"))).To(Succeed())
		DeferCleanup(func() { deleteDeployment("restart-app") })
		waitForRunningPodAndDeclareResize("restart-app")

		// Wait for first resize to land.
		Eventually(func(g Gomega) {
			p, err := getPodForApp("restart-app")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(p.Annotations[resizedAnnotation]).To(Equal("n4-standard-4"))
			g.Expect(p.Spec.Containers[0].Resources.Requests.Cpu().Cmp(resource.MustParse("800m"))).To(Equal(0))
		}, 90*time.Second, 2*time.Second).Should(Succeed())

		By("restarting the controller-manager")
		cmd := exec.Command("kubectl", "rollout", "restart", "deployment",
			"workload-resizer-controller-manager", "-n", namespace)
		_, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred())
		cmd = exec.Command("kubectl", "rollout", "status", "deployment",
			"workload-resizer-controller-manager", "-n", namespace, "--timeout=2m")
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred())

		// After restart, give the controller time to re-reconcile.
		// Assert the pod's CPU is still 800m (NOT 800m * 1.0/1.25 = 640m, which
		// would happen if the controller treated the current spec as the new
		// "original").
		Consistently(func(g Gomega) {
			p, err := getPodForApp("restart-app")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(p.Spec.Containers[0].Resources.Requests.Cpu().Cmp(resource.MustParse("800m"))).To(Equal(0))
			origCPU, _ := resource.ParseQuantity(p.Annotations[origCPUAnnotation])
			g.Expect(origCPU.Cmp(resource.MustParse("1000m"))).To(Equal(0))
		}, 30*time.Second, 2*time.Second).Should(Succeed())
	})
})
