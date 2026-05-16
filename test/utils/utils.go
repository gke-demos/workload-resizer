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

package utils

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"

	. "github.com/onsi/ginkgo/v2" // nolint:revive,staticcheck
)

const (
	certmanagerVersion = "v1.20.2"
	certmanagerURLTmpl = "https://github.com/cert-manager/cert-manager/releases/download/%s/cert-manager.yaml"

	kwokVersion        = "v0.7.0"
	kwokReleaseURLTmpl = "https://github.com/kubernetes-sigs/kwok/releases/download/%s/%s"

	defaultKindBinary  = "kind"
	defaultKindCluster = "kind"
)

func warnError(err error) {
	_, _ = fmt.Fprintf(GinkgoWriter, "warning: %v\n", err)
}

// Run executes the provided command within this context
func Run(cmd *exec.Cmd) (string, error) {
	dir, _ := GetProjectDir()
	cmd.Dir = dir

	if err := os.Chdir(cmd.Dir); err != nil {
		_, _ = fmt.Fprintf(GinkgoWriter, "chdir dir: %q\n", err)
	}

	cmd.Env = append(os.Environ(), "GO111MODULE=on")
	command := strings.Join(cmd.Args, " ")
	_, _ = fmt.Fprintf(GinkgoWriter, "running: %q\n", command)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("%q failed with error %q: %w", command, string(output), err)
	}

	return string(output), nil
}

// UninstallCertManager uninstalls the cert manager
func UninstallCertManager() {
	url := fmt.Sprintf(certmanagerURLTmpl, certmanagerVersion)
	cmd := exec.Command("kubectl", "delete", "-f", url)
	if _, err := Run(cmd); err != nil {
		warnError(err)
	}

	// Delete leftover leases in kube-system (not cleaned by default)
	kubeSystemLeases := []string{
		"cert-manager-cainjector-leader-election",
		"cert-manager-controller",
	}
	for _, lease := range kubeSystemLeases {
		cmd = exec.Command("kubectl", "delete", "lease", lease,
			"-n", "kube-system", "--ignore-not-found", "--force", "--grace-period=0")
		if _, err := Run(cmd); err != nil {
			warnError(err)
		}
	}
}

// InstallCertManager installs the cert manager bundle.
func InstallCertManager() error {
	url := fmt.Sprintf(certmanagerURLTmpl, certmanagerVersion)
	cmd := exec.Command("kubectl", "apply", "-f", url)
	if _, err := Run(cmd); err != nil {
		return err
	}
	// Wait for cert-manager-webhook to be ready, which can take time if cert-manager
	// was re-installed after uninstalling on a cluster.
	cmd = exec.Command("kubectl", "wait", "deployment.apps/cert-manager-webhook",
		"--for", "condition=Available",
		"--namespace", "cert-manager",
		"--timeout", "5m",
	)

	_, err := Run(cmd)
	return err
}

// IsCertManagerCRDsInstalled checks if any Cert Manager CRDs are installed
// by verifying the existence of key CRDs related to Cert Manager.
func IsCertManagerCRDsInstalled() bool {
	// List of common Cert Manager CRDs
	certManagerCRDs := []string{
		"certificates.cert-manager.io",
		"issuers.cert-manager.io",
		"clusterissuers.cert-manager.io",
		"certificaterequests.cert-manager.io",
		"orders.acme.cert-manager.io",
		"challenges.acme.cert-manager.io",
	}

	// Execute the kubectl command to get all CRDs
	cmd := exec.Command("kubectl", "get", "crds")
	output, err := Run(cmd)
	if err != nil {
		return false
	}

	// Check if any of the Cert Manager CRDs are present
	crdList := GetNonEmptyLines(output)
	for _, crd := range certManagerCRDs {
		for _, line := range crdList {
			if strings.Contains(line, crd) {
				return true
			}
		}
	}

	return false
}

// InstallKWOK installs the KWOK controller and stage-fast policies into the
// current cluster. KWOK simulates a kubelet for fake nodes, which lets us
// stand up many "instance types" cheaply.
func InstallKWOK() error {
	manifests := []string{"kwok.yaml", "stage-fast.yaml"}
	for _, m := range manifests {
		url := fmt.Sprintf(kwokReleaseURLTmpl, kwokVersion, m)
		cmd := exec.Command("kubectl", "apply", "-f", url)
		if _, err := Run(cmd); err != nil {
			return fmt.Errorf("apply %s: %w", m, err)
		}
	}
	cmd := exec.Command("kubectl", "wait", "deployment.apps/kwok-controller",
		"--for", "condition=Available",
		"--namespace", "kube-system",
		"--timeout", "2m",
	)
	if _, err := Run(cmd); err != nil {
		return fmt.Errorf("wait for kwok-controller: %w", err)
	}
	return nil
}

// UninstallKWOK removes the KWOK controller and stage policies.
func UninstallKWOK() {
	manifests := []string{"stage-fast.yaml", "kwok.yaml"}
	for _, m := range manifests {
		url := fmt.Sprintf(kwokReleaseURLTmpl, kwokVersion, m)
		cmd := exec.Command("kubectl", "delete", "-f", url, "--ignore-not-found")
		if _, err := Run(cmd); err != nil {
			warnError(err)
		}
	}
}

// IsKWOKInstalled reports whether the kwok-controller deployment exists in kube-system.
func IsKWOKInstalled() bool {
	cmd := exec.Command("kubectl", "get", "deployment", "kwok-controller",
		"-n", "kube-system", "--ignore-not-found")
	out, err := Run(cmd)
	if err != nil {
		return false
	}
	return strings.Contains(out, "kwok-controller")
}

// CreateKWOKNode creates a fake Node managed by KWOK with the given
// machine family as the value of cloud.google.com/machine-family
// (the controller's default --node-type-label). It also sets
// node.kubernetes.io/instance-type to "<family>-fake" so the node
// looks GKE-shaped to anything else inspecting it.
func CreateKWOKNode(name, family string) error {
	manifest := fmt.Sprintf(`
apiVersion: v1
kind: Node
metadata:
  name: %s
  annotations:
    node.alpha.kubernetes.io/ttl: "0"
    kwok.x-k8s.io/node: fake
  labels:
    type: kwok
    cloud.google.com/machine-family: %s
    node.kubernetes.io/instance-type: %s-fake
spec:
  taints:
  - effect: NoSchedule
    key: kwok.x-k8s.io/node
    value: fake
status:
  capacity:
    cpu: "64"
    memory: "128Gi"
    pods: "110"
  allocatable:
    cpu: "64"
    memory: "128Gi"
    pods: "110"
  conditions:
  - status: "True"
    type: Ready
    reason: KubeletReady
  nodeInfo:
    architecture: amd64
    operatingSystem: linux
    kubeletVersion: fake
`, name, family, family)
	cmd := exec.Command("kubectl", "apply", "-f", "-")
	cmd.Stdin = strings.NewReader(manifest)
	if _, err := Run(cmd); err != nil {
		return fmt.Errorf("apply kwok node %s: %w", name, err)
	}
	return nil
}

// DeleteKWOKNode removes a KWOK fake node by name.
func DeleteKWOKNode(name string) {
	cmd := exec.Command("kubectl", "delete", "node", name, "--ignore-not-found", "--wait=false")
	if _, err := Run(cmd); err != nil {
		warnError(err)
	}
}

// LoadImageToKindClusterWithName loads a local docker image to the kind cluster
func LoadImageToKindClusterWithName(name string) error {
	cluster := defaultKindCluster
	if v, ok := os.LookupEnv("KIND_CLUSTER"); ok {
		cluster = v
	}
	kindOptions := []string{"load", "docker-image", name, "--name", cluster}
	kindBinary := defaultKindBinary
	if v, ok := os.LookupEnv("KIND"); ok {
		kindBinary = v
	}
	cmd := exec.Command(kindBinary, kindOptions...)
	_, err := Run(cmd)
	return err
}

// GetNonEmptyLines converts given command output string into individual objects
// according to line breakers, and ignores the empty elements in it.
func GetNonEmptyLines(output string) []string {
	var res []string
	elements := strings.SplitSeq(output, "\n")
	for element := range elements {
		if element != "" {
			res = append(res, element)
		}
	}

	return res
}

// GetProjectDir will return the directory where the project is
func GetProjectDir() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return wd, fmt.Errorf("failed to get current working directory: %w", err)
	}
	wd = strings.ReplaceAll(wd, "/test/e2e", "")
	return wd, nil
}

// UncommentCode searches for target in the file and remove the comment prefix
// of the target content. The target content may span multiple lines.
func UncommentCode(filename, target, prefix string) error {
	// false positive
	// nolint:gosec
	content, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("failed to read file %q: %w", filename, err)
	}
	strContent := string(content)

	idx := strings.Index(strContent, target)
	if idx < 0 {
		return fmt.Errorf("unable to find the code %q to be uncommented", target)
	}

	out := new(bytes.Buffer)
	_, err = out.Write(content[:idx])
	if err != nil {
		return fmt.Errorf("failed to write to output: %w", err)
	}

	scanner := bufio.NewScanner(bytes.NewBufferString(target))
	if !scanner.Scan() {
		return nil
	}
	for {
		if _, err = out.WriteString(strings.TrimPrefix(scanner.Text(), prefix)); err != nil {
			return fmt.Errorf("failed to write to output: %w", err)
		}
		// Avoid writing a newline in case the previous line was the last in target.
		if !scanner.Scan() {
			break
		}
		if _, err = out.WriteString("\n"); err != nil {
			return fmt.Errorf("failed to write to output: %w", err)
		}
	}

	if _, err = out.Write(content[idx+len(target):]); err != nil {
		return fmt.Errorf("failed to write to output: %w", err)
	}

	// false positive
	// nolint:gosec
	if err = os.WriteFile(filename, out.Bytes(), 0644); err != nil {
		return fmt.Errorf("failed to write file %q: %w", filename, err)
	}

	return nil
}
