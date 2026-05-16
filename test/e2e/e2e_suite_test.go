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
	"fmt"
	"os"
	"os/exec"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/gke-demos/workload-resizer/test/utils"
)

var (
	// managerImage is the manager image to be built/loaded/deployed. Override
	// via MANAGER_IMAGE when running against a real cluster; it must be a
	// registry-qualified ref the cluster can pull.
	managerImage = imageFromEnv("MANAGER_IMAGE", "example.com/workload-resizer:v0.0.1")
	// useExistingCluster, when true, skips Kind-specific bootstrap (image
	// build, image load) and assumes the cluster the current kubectl context
	// points at is ready to receive the controller.
	useExistingCluster = os.Getenv("USE_EXISTING_CLUSTER") == "true"
	// shouldCleanupCertManager tracks whether CertManager was installed by this suite.
	shouldCleanupCertManager = false
	// shouldCleanupKWOK tracks whether KWOK was installed by this suite.
	shouldCleanupKWOK = false
)

func imageFromEnv(envKey, fallback string) string {
	if v := os.Getenv(envKey); v != "" {
		return v
	}
	return fallback
}

// TestE2E runs the e2e test suite to validate the solution in an isolated environment.
// The default setup requires Kind and CertManager.
//
// To enable kubectl kuberc (use custom kubectl configurations), set: KUBECTL_KUBERC=true
// By default, kuberc is disabled to ensure consistent test behavior across different environments.
// To skip CertManager installation, set: CERT_MANAGER_INSTALL_SKIP=true
func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	_, _ = fmt.Fprintf(GinkgoWriter, "Starting workload-resizer e2e test suite\n")
	RunSpecs(t, "e2e suite")
}

var _ = BeforeSuite(func() {
	if useExistingCluster {
		_, _ = fmt.Fprintf(GinkgoWriter,
			"USE_EXISTING_CLUSTER=true — skipping image build/load. Using image %q against current kubectl context.\n",
			managerImage)
	} else {
		By("building the manager image")
		cmd := exec.Command("make", "docker-build", fmt.Sprintf("IMG=%s", managerImage))
		_, err := utils.Run(cmd)
		ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to build the manager image")

		By("loading the manager image on Kind")
		err = utils.LoadImageToKindClusterWithName(managerImage)
		ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to load the manager image into Kind")
	}

	configureKubectlKubeRC()
	setupCertManager()
	setupKWOK()
	deployController()
})

var _ = AfterSuite(func() {
	undeployController()
	teardownKWOK()
	teardownCertManager()
})

// namespaceForController is the namespace into which the controller is deployed.
const namespaceForController = "workload-resizer-system"

// deployController creates the controller namespace and deploys the manager.
// Done once for the whole suite so multiple Describes can share the deployed controller.
func deployController() {
	By("creating manager namespace")
	cmd := exec.Command("kubectl", "create", "ns", namespaceForController)
	if _, err := utils.Run(cmd); err != nil {
		// If it already exists from a previous run, that's fine.
		_, _ = fmt.Fprintf(GinkgoWriter, "namespace create returned: %v (continuing)\n", err)
	}

	By("labeling the namespace to enforce the restricted security policy")
	cmd = exec.Command("kubectl", "label", "--overwrite", "ns", namespaceForController,
		"pod-security.kubernetes.io/enforce=restricted")
	_, err := utils.Run(cmd)
	Expect(err).NotTo(HaveOccurred(), "Failed to label namespace")

	By("installing CRDs")
	cmd = exec.Command("make", "install")
	_, err = utils.Run(cmd)
	Expect(err).NotTo(HaveOccurred(), "Failed to install CRDs")

	By("deploying the controller-manager")
	cmd = exec.Command("make", "deploy", fmt.Sprintf("IMG=%s", managerImage))
	_, err = utils.Run(cmd)
	Expect(err).NotTo(HaveOccurred(), "Failed to deploy the controller-manager")

	By("waiting for the controller-manager to be Available")
	cmd = exec.Command("kubectl", "rollout", "status",
		"deployment/workload-resizer-controller-manager",
		"-n", namespaceForController, "--timeout=2m")
	_, err = utils.Run(cmd)
	Expect(err).NotTo(HaveOccurred(), "controller-manager did not become Available")
}

func undeployController() {
	By("undeploying the controller-manager")
	cmd := exec.Command("make", "undeploy")
	_, _ = utils.Run(cmd)

	By("uninstalling CRDs")
	cmd = exec.Command("make", "uninstall")
	_, _ = utils.Run(cmd)

	By("removing manager namespace")
	cmd = exec.Command("kubectl", "delete", "ns", namespaceForController, "--ignore-not-found")
	_, _ = utils.Run(cmd)
}

// setupKWOK installs the KWOK controller so we can create fake nodes with
// arbitrary instance-type labels. Skipped if already present, controlled
// by KWOK_INSTALL_SKIP.
func setupKWOK() {
	if os.Getenv("KWOK_INSTALL_SKIP") == "true" {
		_, _ = fmt.Fprintf(GinkgoWriter, "Skipping KWOK installation (KWOK_INSTALL_SKIP=true)\n")
		return
	}
	By("checking if KWOK is already installed")
	if utils.IsKWOKInstalled() {
		_, _ = fmt.Fprintf(GinkgoWriter, "KWOK already installed. Skipping installation.\n")
		return
	}
	shouldCleanupKWOK = true
	By("installing KWOK")
	Expect(utils.InstallKWOK()).To(Succeed(), "Failed to install KWOK")
}

func teardownKWOK() {
	if !shouldCleanupKWOK {
		return
	}
	By("uninstalling KWOK")
	utils.UninstallKWOK()
}

// Disable kubectl kuberc by default for test isolation.
// This prevents local kubectl configurations from affecting test behavior.
// To enable kuberc, set: KUBECTL_KUBERC=true
func configureKubectlKubeRC() {
	if os.Getenv("KUBECTL_KUBERC") != "true" {
		By("disabling kubectl kuberc for test isolation")
		err := os.Setenv("KUBECTL_KUBERC", "false")
		ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to disable kubectl kuberc")
		_, _ = fmt.Fprintf(GinkgoWriter,
			"kubectl kuberc disabled for consistent test behavior (override with KUBECTL_KUBERC=true)\n")
	} else {
		_, _ = fmt.Fprintf(GinkgoWriter, "kubectl kuberc enabled (KUBECTL_KUBERC=true)\n")
	}
}

// setupCertManager installs CertManager if needed for webhook tests.
// Skips installation if CERT_MANAGER_INSTALL_SKIP=true or if already present.
func setupCertManager() {
	if os.Getenv("CERT_MANAGER_INSTALL_SKIP") == "true" {
		_, _ = fmt.Fprintf(GinkgoWriter, "Skipping CertManager installation (CERT_MANAGER_INSTALL_SKIP=true)\n")
		return
	}

	By("checking if CertManager is already installed")
	if utils.IsCertManagerCRDsInstalled() {
		_, _ = fmt.Fprintf(GinkgoWriter, "CertManager is already installed. Skipping installation.\n")
		return
	}

	// Mark for cleanup before installation to handle interruptions and partial installs.
	shouldCleanupCertManager = true

	By("installing CertManager")
	Expect(utils.InstallCertManager()).To(Succeed(), "Failed to install CertManager")
}

// teardownCertManager uninstalls CertManager if it was installed by setupCertManager.
// This ensures we only remove what we installed.
func teardownCertManager() {
	if !shouldCleanupCertManager {
		_, _ = fmt.Fprintf(GinkgoWriter, "Skipping CertManager cleanup (not installed by this suite)\n")
		return
	}

	By("uninstalling CertManager")
	utils.UninstallCertManager()
}
