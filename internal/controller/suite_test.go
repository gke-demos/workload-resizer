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
	"fmt"
	"path/filepath"
	goruntime "runtime"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/gke-demos/workload-resizer/internal/config"
	"github.com/gke-demos/workload-resizer/internal/controller"
)

var (
	cfg       *rest.Config
	k8sClient client.Client
	testEnv   *envtest.Environment
	cancelMgr context.CancelFunc
	recorder  *record.FakeRecorder
	store     *config.Store
)

func TestController(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Controller Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	By("bootstrapping test environment")
	_, currentFile, _, _ := goruntime.Caller(0)
	binDir := filepath.Join(filepath.Dir(currentFile), "..", "..", "bin", "k8s",
		fmt.Sprintf("1.35.0-%s-%s", goruntime.GOOS, goruntime.GOARCH))
	testEnv = &envtest.Environment{
		BinaryAssetsDirectory: binDir,
	}

	var err error
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	Expect(corev1.AddToScheme(scheme.Scheme)).To(Succeed())
	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:                 scheme.Scheme,
		Metrics:                metricsserver.Options{BindAddress: "0"},
		HealthProbeBindAddress: "0",
	})
	Expect(err).NotTo(HaveOccurred())

	store = config.NewStore()
	store.Set(testConfig())

	recorder = record.NewFakeRecorder(256)
	Expect((&controller.PodReconciler{
		Client:    mgr.GetClient(),
		APIReader: mgr.GetAPIReader(),
		Recorder:  recorder,
		Config:    store,
	}).SetupWithManager(mgr)).To(Succeed())

	var ctx context.Context
	ctx, cancelMgr = context.WithCancel(context.Background())
	go func() {
		defer GinkgoRecover()
		Expect(mgr.Start(ctx)).To(Succeed())
	}()

	// Give the cache a moment to sync.
	Eventually(func() error {
		var pods corev1.PodList
		return mgr.GetClient().List(ctx, &pods)
	}, 10*time.Second, 250*time.Millisecond).Should(Succeed())
})

var _ = AfterSuite(func() {
	if cancelMgr != nil {
		cancelMgr()
	}
	By("tearing down test environment")
	Expect(testEnv.Stop()).To(Succeed())
})

// helpers ---------------------------------------------------------------------

func testConfig() *config.Config {
	return &config.Config{
		BaselineNodeType: "n2d",
		NodeTypes: map[string]config.NodeProfile{
			"n2d":  {CPUPerf: 1.0, MemPerf: 1.0},
			"n4":   {CPUPerf: 1.25, MemPerf: 1.0},
			"tiny": {CPUPerf: 0.5, MemPerf: 1.0},
			"huge": {CPUPerf: 100.0, MemPerf: 100.0}, // forces clamp at min
		},
		Bounds: config.Bounds{
			CPU:    config.Bound{Min: resource.MustParse("50m"), Max: resource.MustParse("16")},
			Memory: config.Bound{Min: resource.MustParse("64Mi"), Max: resource.MustParse("32Gi")},
		},
	}
}

// makeNode builds a Node carrying the controller's default
// node-type label (cloud.google.com/machine-family). nodeType is the
// label *value* — pick something that appears in testConfig().NodeTypes
// (or doesn't, to exercise the UnknownNodeType path).
func makeNode(name, nodeType string) *corev1.Node {
	labels := map[string]string{}
	if nodeType != "" {
		labels[controller.DefaultNodeTypeLabel] = nodeType
	}
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels},
		Status: corev1.NodeStatus{
			Capacity: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("64"),
				corev1.ResourceMemory: resource.MustParse("128Gi"),
				corev1.ResourcePods:   resource.MustParse("110"),
			},
			Allocatable: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("64"),
				corev1.ResourceMemory: resource.MustParse("128Gi"),
				corev1.ResourcePods:   resource.MustParse("110"),
			},
			Conditions: []corev1.NodeCondition{
				{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
			},
		},
	}
}

func makeOwnedPod(name, ns, nodeName, cpu, mem string) *corev1.Pod {
	tru := true
	notRequired := corev1.NotRequired
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "apps/v1",
				Kind:       "ReplicaSet",
				Name:       "fake-rs",
				UID:        "00000000-0000-0000-0000-000000000001",
				Controller: &tru,
			}},
		},
		Spec: corev1.PodSpec{
			NodeName: nodeName,
			Containers: []corev1.Container{{
				Name:  "app",
				Image: "registry.k8s.io/pause:3.10",
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse(cpu),
						corev1.ResourceMemory: resource.MustParse(mem),
					},
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse(cpu),
						corev1.ResourceMemory: resource.MustParse(mem),
					},
				},
				ResizePolicy: []corev1.ContainerResizePolicy{
					{ResourceName: corev1.ResourceCPU, RestartPolicy: notRequired},
					{ResourceName: corev1.ResourceMemory, RestartPolicy: notRequired},
				},
			}},
		},
	}
}

func mustCreate(ctx context.Context, obj client.Object) {
	GinkgoHelper()
	Expect(k8sClient.Create(ctx, obj)).To(Succeed())
}

func getPod(ctx context.Context, name, ns string) *corev1.Pod {
	GinkgoHelper()
	var p corev1.Pod
	Expect(k8sClient.Get(ctx, client.ObjectKey{Name: name, Namespace: ns}, &p)).To(Succeed())
	return &p
}

func cleanup(ctx context.Context, objs ...client.Object) {
	for _, o := range objs {
		_ = k8sClient.Delete(ctx, o)
	}
}

// drainEvents pulls all pending events out of the recorder so the next test
// starts fresh. The fake recorder is a buffered channel.
func drainEvents() {
	for {
		select {
		case <-recorder.Events:
		default:
			return
		}
	}
}

func collectEvents(d time.Duration) []string {
	deadline := time.After(d)
	var out []string
	for {
		select {
		case e := <-recorder.Events:
			out = append(out, e)
		case <-deadline:
			return out
		}
	}
}
