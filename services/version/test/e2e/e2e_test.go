/*
Copyright 2026 Tony Owens.

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
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	utils "github.com/tonedefdev/opendepot/pkg/testutils"
)

// namespace where the project is deployed in.
const namespace = "opendepot-system"

var _ = Describe("Version", Ordered, func() {
	var controllerPodName string

	AfterEach(func() {
		specReport := CurrentSpecReport()
		if specReport.Failed() {
			if controllerPodName != "" {
				By("Fetching version controller pod logs")
				cmd := exec.Command("kubectl", "logs", controllerPodName, "-n", namespace)
				controllerLogs, err := utils.Run(cmd)
				if err == nil {
					_, _ = fmt.Fprintf(GinkgoWriter, "Version controller logs:\n %s", controllerLogs)
				} else {
					_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get version controller logs: %s", err)
				}
			}

			By("Fetching Kubernetes events")
			cmd := exec.Command("kubectl", "get", "events", "-n", namespace, "--sort-by=.lastTimestamp")
			eventsOutput, err := utils.Run(cmd)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Kubernetes events:\n%s", eventsOutput)
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get Kubernetes events: %s", err)
			}
		}
	})

	SetDefaultEventuallyTimeout(2 * time.Minute)
	SetDefaultEventuallyPollingInterval(time.Second)

	Context("Controller", func() {
		It("should run successfully", func() {
			By("validating that the version-controller pod is running")
			verifyControllerUp := func(g Gomega) {
				cmd := exec.Command("kubectl", "get",
					"pods", "-l", "app=version-controller",
					"-o", "go-template={{ range .items }}"+
						"{{ if not .metadata.deletionTimestamp }}"+
						"{{ .metadata.name }}"+
						"{{ \"\\n\" }}{{ end }}{{ end }}",
					"-n", namespace,
				)
				podOutput, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred(), "Failed to retrieve version-controller pod information")
				podNames := utils.GetNonEmptyLines(podOutput)
				g.Expect(podNames).To(HaveLen(1), "expected 1 version-controller pod running")
				controllerPodName = podNames[0]
				g.Expect(controllerPodName).To(ContainSubstring("version-controller"))

				cmd = exec.Command("kubectl", "get",
					"pods", controllerPodName, "-o", "jsonpath={.status.phase}",
					"-n", namespace,
				)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Running"), "Incorrect version-controller pod status")
			}
			Eventually(verifyControllerUp).Should(Succeed())
		})

		It("should be allowed to delete Pods in its namespace", func() {
			cmd := exec.Command("kubectl", "auth", "can-i", "delete", "pods",
				"--as=system:serviceaccount:"+namespace+":version-controller",
				"-n", namespace,
			)
			output, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(strings.TrimSpace(output)).To(Equal("yes"))
		})
	})

	Context("Version CR", Ordered, func() {
		const (
			versionCRName = "version-e2e-dual-config"
			storageDir    = "/data/modules"
			providerName  = "null"
			testFileName  = "null.zip"
			testVersion   = "3.2.3"
		)

		AfterAll(func() {
			By("removing the test Version CR")
			cmd := exec.Command("kubectl", "delete", "version", versionCRName,
				"-n", namespace, "--ignore-not-found")
			_, _ = utils.Run(cmd)
		})

		It("should add the opendepot finalizer to a Version CR", func() {
			By("applying a Provider-type Version CR with both moduleConfigRef and providerConfigRef")
			versionYAML := fmt.Sprintf(`apiVersion: opendepot.defdev.io/v1alpha1
kind: Version
metadata:
  name: %s
  namespace: %s
spec:
  type: Provider
  version: %q
  fileName: %q
  providerConfigRef:
    name: %q
    storageConfig:
      fileSystem:
        directoryPath: %s
  moduleConfigRef:
    storageConfig:
      fileSystem:
        directoryPath: %s
`, versionCRName, namespace, testVersion, testFileName, providerName, storageDir, storageDir)

			versionFile := filepath.Join(GinkgoT().TempDir(), "test-version.yaml")
			Expect(os.WriteFile(versionFile, []byte(versionYAML), 0600)).To(Succeed())

			cmd := exec.Command("kubectl", "apply", "-f", versionFile)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to apply Version CR")

			By("waiting for the opendepot finalizer to be added")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "version", versionCRName,
					"-n", namespace,
					"-o", "jsonpath={.metadata.finalizers}",
				)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(ContainSubstring("opendepot.defdev.io/finalizer"),
					"expected opendepot finalizer to be present on the Version CR")
			}).Should(Succeed())
		})

		It("should not successfully sync a Version CR with conflicting moduleConfigRef and providerConfigRef", func() {
			By("waiting for the dual-config syncStatus message to be written")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "version", versionCRName,
					"-n", namespace,
					"-o", "jsonpath={.status.syncStatus}",
				)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(ContainSubstring("both are defined"),
					"expected dual-config guard syncStatus message to be written")
			}).Should(Succeed())

			By("confirming the Version CR never reaches synced=true")
			Consistently(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "version", versionCRName,
					"-n", namespace,
					"-o", "jsonpath={.status.synced}",
				)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).NotTo(Equal("true"),
					"Version CR with both moduleConfigRef and providerConfigRef must not sync successfully")
			}, 15*time.Second, 3*time.Second).Should(Succeed())
		})

		It("should fully remove the Version CR after deletion", func() {
			By("deleting the Version CR")
			cmd := exec.Command("kubectl", "delete", "version", versionCRName,
				"-n", namespace,
			)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to delete Version CR")

			By("waiting for the Version CR to be fully removed")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "version", versionCRName,
					"-n", namespace,
					"--ignore-not-found",
				)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(BeEmpty(), "Version CR should be fully removed")
			}, 2*time.Minute).Should(Succeed())
		})
	})

	Context("Version Name Validation", Ordered, func() {
		const dotVersionCRName = "invalid.version.name"

		AfterAll(func() {
			By("removing the invalid-name Version CR")
			cmd := exec.Command("kubectl", "delete", "version", dotVersionCRName,
				"-n", namespace, "--ignore-not-found")
			_, _ = utils.Run(cmd)
		})

		It("should reject a Version CR whose name contains '.' characters", func() {
			By("applying a Version CR with '.' in its name")
			versionYAML := fmt.Sprintf(`apiVersion: opendepot.defdev.io/v1alpha1
kind: Version
metadata:
  name: %s
  namespace: %s
spec:
  type: Module
  version: "1.0.0"
  moduleConfigRef:
    storageConfig:
      fileSystem:
        directoryPath: /data/modules
`, dotVersionCRName, namespace)

			versionFile := filepath.Join(GinkgoT().TempDir(), "invalid-name-version.yaml")
			Expect(os.WriteFile(versionFile, []byte(versionYAML), 0600)).To(Succeed())

			cmd := exec.Command("kubectl", "apply", "-f", versionFile)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to apply invalid-name Version CR")

			By("waiting for the name-validation syncStatus message to be written")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "version", dotVersionCRName,
					"-n", namespace,
					"-o", "jsonpath={.status.syncStatus}",
				)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(ContainSubstring("must not contain '.' characters"),
					"expected name-validation syncStatus message to be written")
			}).Should(Succeed())

			By("confirming the Version CR never reaches synced=true")
			Consistently(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "version", dotVersionCRName,
					"-n", namespace,
					"-o", "jsonpath={.status.synced}",
				)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).NotTo(Equal("true"),
					"Version CR with '.' in its name must not sync successfully")
			}, 15*time.Second, 3*time.Second).Should(Succeed())
		})
	})

	Context("Scanning Disabled", Ordered, func() {
		const (
			noScanVersionName = "no-scan-s3-bucket-4-3-0"
			noScanModule      = "terraform-aws-s3-bucket"
			noScanVersion     = "4.3.0"
			noScanRepoOwner   = "terraform-aws-modules"
			noScanRepoURL     = "https://github.com/terraform-aws-modules/terraform-aws-s3-bucket"
			noScanStorageDir  = "/data/modules"
		)
		var portForwardCancel context.CancelFunc

		BeforeAll(func() {
			By("upgrading Helm release to disable scanning")
			chartPath, err := utils.GetChartPath()
			ExpectWithOffset(1, err).NotTo(HaveOccurred())

			helmCmd := exec.Command("helm", "upgrade", helmReleaseName, chartPath,
				"--reuse-values",
				"--namespace", namespace,
				"--set", "scanning.enabled=false",
				"--set", "scanning.providerScanning=false",
				"--wait",
				"--timeout", "3m",
			)
			_, err = utils.Run(helmCmd)
			ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to upgrade Helm release to disable scanning")

			By("starting a port-forward to the server for artifact verification")
			portForwardCtx, cancel := context.WithCancel(context.Background())
			portForwardCancel = cancel
			cmd := exec.CommandContext(portForwardCtx, "kubectl", "port-forward",
				"-n", namespace, "svc/server", "18081:80",
			)
			cmd.Stdout = GinkgoWriter
			cmd.Stderr = GinkgoWriter
			Expect(cmd.Start()).To(Succeed())

			Eventually(func() error {
				resp, err := http.Get("http://localhost:18081/.well-known/terraform.json")
				if err != nil {
					return err
				}
				defer resp.Body.Close()
				return nil
			}, 60*time.Second, time.Second).Should(Succeed())
		})

		AfterAll(func() {
			if portForwardCancel != nil {
				portForwardCancel()
			}

			By("removing the no-scan Version CR")
			cmd := exec.Command("kubectl", "delete", "version", noScanVersionName,
				"-n", namespace, "--ignore-not-found")
			_, _ = utils.Run(cmd)
		})

		It("should sync a module Version CR and produce no scan findings when scanning is disabled", func() {
			By("applying a module Version CR")
			versionYAML := fmt.Sprintf(`apiVersion: opendepot.defdev.io/v1alpha1
kind: Version
metadata:
  name: %s
  namespace: %s
spec:
  type: Module
  version: %q
  moduleConfigRef:
    name: %q
    repoOwner: %q
    repoUrl: %q
    githubClientConfig:
      useAuthenticatedClient: true
    storageConfig:
      fileSystem:
        directoryPath: %s
`, noScanVersionName, namespace, noScanVersion, noScanModule, noScanRepoOwner, noScanRepoURL, noScanStorageDir)

			versionFile := filepath.Join(GinkgoT().TempDir(), "no-scan-version.yaml")
			Expect(os.WriteFile(versionFile, []byte(versionYAML), 0600)).To(Succeed())

			cmd := exec.Command("kubectl", "apply", "-f", versionFile)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to apply no-scan Version CR")

			By("waiting for the Version CR to reach synced=true")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "version", noScanVersionName,
					"-n", namespace,
					"-o", "jsonpath={.status.synced}",
				)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("true"),
					"expected Version CR to reach synced=true")
			}, 5*time.Minute, 10*time.Second).Should(Succeed())

			By("asserting that status.sourceScan is absent when scanning is disabled")
			cmd = exec.Command("kubectl", "get", "version", noScanVersionName,
				"-n", namespace,
				"-o", "jsonpath={.status.sourceScan}",
			)
			sourceScan, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(sourceScan).To(BeEmpty(),
				"expected status.sourceScan to be absent when scanning is disabled")

			By("verifying the uploaded artifact and its checksum in storage")
			cmd = exec.Command("kubectl", "get", "version", noScanVersionName,
				"-n", namespace,
				"-o", "jsonpath={.spec.fileName},{.status.checksum}",
			)
			artifactInfo, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			parts := strings.SplitN(strings.TrimSpace(artifactInfo), ",", 2)
			Expect(parts).To(HaveLen(2), "unexpected artifact metadata: %s", artifactInfo)
			Expect(parts[0]).NotTo(BeEmpty(), "expected spec.fileName to be persisted")
			Expect(parts[1]).NotTo(BeEmpty(), "expected status.checksum to be persisted")

			expectedChecksum, err := base64.StdEncoding.DecodeString(parts[1])
			Expect(err).NotTo(HaveOccurred(), "expected status.checksum to be base64 encoded")
			encodedDirectory := base64.RawURLEncoding.EncodeToString([]byte(noScanStorageDir))
			artifactURL := fmt.Sprintf("http://localhost:18081/opendepot/modules/v1/download/fileSystem/%s/%s/%s",
				encodedDirectory, noScanModule, parts[0])
			artifactURL += "?fileChecksum=" + url.QueryEscape(parts[1])
			resp, err := http.Get(artifactURL)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusOK), "expected uploaded artifact to be downloadable")
			artifactBytes, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			Expect(err).NotTo(HaveOccurred())
			Expect(artifactBytes).NotTo(BeEmpty(), "expected uploaded artifact to contain bytes")
			artifactChecksum := sha256.Sum256(artifactBytes)
			Expect(artifactChecksum[:]).To(Equal(expectedChecksum),
				"expected stored artifact checksum to match status.checksum")
		})
	})

	Context("Module IaC Scan", Ordered, func() {
		const (
			scanVersionName = "terraform-aws-s3-bucket-4-3-0"
			scanModuleName  = "terraform-aws-s3-bucket"
			scanVersion     = "4.3.0"
			scanRepoOwner   = "terraform-aws-modules"
			scanRepoURL     = "https://github.com/terraform-aws-modules/terraform-aws-s3-bucket"
			scanStorageDir  = "/data/modules"
		)

		AfterAll(func() {
			By("removing the Module IaC Scan Version CR")
			cmd := exec.Command("kubectl", "delete", "version", scanVersionName,
				"-n", namespace, "--ignore-not-found")
			_, _ = utils.Run(cmd)
		})

		BeforeAll(func() {
			By("upgrading Helm release to enable scanning")
			chartPath, err := utils.GetChartPath()
			ExpectWithOffset(1, err).NotTo(HaveOccurred())

			baseRepo, baseTag := utils.SplitImageRef(projectImage)
			cmd := exec.Command("helm", "upgrade", helmReleaseName, chartPath,
				"--reuse-values",
				"--namespace", namespace,
				"--set", fmt.Sprintf("version.image.repository=%s", baseRepo),
				"--set", fmt.Sprintf("version.image.tag=%s", baseTag),
				"--set", "scanning.enabled=true",
				"--set", "scanning.cache.accessMode=ReadWriteOnce",
				"--wait",
				"--timeout", "3m",
			)
			_, err = utils.Run(cmd)
			ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to upgrade Helm release to enable scanning")
		})

		It("should produce IaC findings for a module with known misconfigurations", func() {
			By("applying an inline module Version CR for terraform-aws-s3-bucket 4.3.0")
			versionYAML := fmt.Sprintf(`apiVersion: opendepot.defdev.io/v1alpha1
kind: Version
metadata:
  name: %s
  namespace: %s
spec:
  type: Module
  version: %q
  moduleConfigRef:
    name: %q
    repoOwner: %q
    repoUrl: %q
    githubClientConfig:
      useAuthenticatedClient: true
    storageConfig:
      fileSystem:
        directoryPath: %s
`, scanVersionName, namespace, scanVersion, scanModuleName, scanRepoOwner, scanRepoURL, scanStorageDir)

			versionFile := filepath.Join(GinkgoT().TempDir(), "scan-version.yaml")
			Expect(os.WriteFile(versionFile, []byte(versionYAML), 0600)).To(Succeed())

			cmd := exec.Command("kubectl", "apply", "-f", versionFile)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to apply Module IaC Scan Version CR")

			By("waiting for the Version CR to reach synced=true (network download involved)")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "version", scanVersionName,
					"-n", namespace,
					"-o", "jsonpath={.status.synced}",
				)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("true"),
					"expected Version CR to reach synced=true")
			}, 5*time.Minute, 10*time.Second).Should(Succeed())

			By("asserting that status.sourceScan.findings contains at least one security finding")
			cmd = exec.Command("kubectl", "get", "version", scanVersionName,
				"-n", namespace,
				"-o", "jsonpath={.status.sourceScan.findings}",
			)
			findings, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(findings).To(ContainSubstring("vulnerabilityID"),
				"expected sourceScan.findings to contain at least one security finding")
		})
	})

	Context("ScanPolicy Exemptions", Ordered, func() {
		const (
			exemptVersionName = "terraform-aws-s3-bucket-4-2-0"
			exemptModuleName  = "terraform-aws-s3-bucket"
			exemptVersion     = "4.2.0"
			exemptRepoOwner   = "terraform-aws-modules"
			exemptRepoURL     = "https://github.com/terraform-aws-modules/terraform-aws-s3-bucket"
			exemptStorageDir  = "/data/modules"
			blockPolicyName   = "e2e-block-all"
			exemptPolicyName  = "e2e-blanket-exemption"
			lateBlockName     = "e2e-late-block"
		)

		AfterAll(func() {
			By("removing the ScanPolicy Exemptions Version CR and ScanPolicies")
			cmd := exec.Command("kubectl", "delete", "version", exemptVersionName,
				"-n", namespace, "--ignore-not-found")
			_, _ = utils.Run(cmd)

			cmd = exec.Command("kubectl", "delete", "scanpolicy", blockPolicyName, exemptPolicyName, lateBlockName,
				"-n", namespace, "--ignore-not-found")
			_, _ = utils.Run(cmd)
		})

		It("should block a module Version CR when a ScanPolicy lowers the severity threshold", func() {
			By("applying a ScanPolicy that blocks on any finding at LOW or above")
			blockYAML := fmt.Sprintf(`apiVersion: opendepot.defdev.io/v1alpha1
kind: ScanPolicy
metadata:
  name: %s
  namespace: %s
spec:
  priority: 10
  severityThreshold: LOW
`, blockPolicyName, namespace)

			blockFile := filepath.Join(GinkgoT().TempDir(), "block-policy.yaml")
			Expect(os.WriteFile(blockFile, []byte(blockYAML), 0600)).To(Succeed())

			cmd := exec.Command("kubectl", "apply", "-f", blockFile)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to apply blocking ScanPolicy")

			By("applying an inline module Version CR for terraform-aws-s3-bucket 4.2.0")
			versionYAML := fmt.Sprintf(`apiVersion: opendepot.defdev.io/v1alpha1
kind: Version
metadata:
  name: %s
  namespace: %s
spec:
  type: Module
  version: %q
  moduleConfigRef:
    name: %q
    repoOwner: %q
    repoUrl: %q
    githubClientConfig:
      useAuthenticatedClient: true
    storageConfig:
      fileSystem:
        directoryPath: %s
`, exemptVersionName, namespace, exemptVersion, exemptModuleName, exemptRepoOwner, exemptRepoURL, exemptStorageDir)

			versionFile := filepath.Join(GinkgoT().TempDir(), "exempt-version.yaml")
			Expect(os.WriteFile(versionFile, []byte(versionYAML), 0600)).To(Succeed())

			cmd = exec.Command("kubectl", "apply", "-f", versionFile)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to apply ScanPolicy Exemptions Version CR")

			By("waiting for the scan to run and record findings")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "version", exemptVersionName,
					"-n", namespace,
					"-o", "jsonpath={.status.sourceScan.findings}",
				)
				findings, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(findings).To(ContainSubstring("vulnerabilityID"),
					"expected sourceScan.findings to contain at least one security finding")
			}, 5*time.Minute, 10*time.Second).Should(Succeed())

			By("asserting that the blocking ScanPolicy prevents the Version CR from syncing")
			Consistently(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "version", exemptVersionName,
					"-n", namespace,
					"-o", "jsonpath={.status.synced}",
				)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).NotTo(Equal("true"),
					"expected blocking findings to prevent the Version CR from syncing")
			}, 30*time.Second, 5*time.Second).Should(Succeed())
		})

		It("should unblock the Version CR once a higher priority ScanPolicy exempts the findings", func() {
			By("applying a higher priority ScanPolicy that exempts every module finding")
			policyYAML := fmt.Sprintf(`apiVersion: opendepot.defdev.io/v1alpha1
kind: ScanPolicy
metadata:
  name: %s
  namespace: %s
spec:
  priority: 20
  severityThreshold: LOW
  exemptions:
  - reason: End-to-end test blanket exemption
    vulnerabilityIDs:
    - "*"
    scanTypes:
    - module
`, exemptPolicyName, namespace)

			policyFile := filepath.Join(GinkgoT().TempDir(), "scan-policy.yaml")
			Expect(os.WriteFile(policyFile, []byte(policyYAML), 0600)).To(Succeed())

			cmd := exec.Command("kubectl", "apply", "-f", policyFile)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to apply exempting ScanPolicy")

			By("waiting for the Version CR to reach synced=true")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "version", exemptVersionName,
					"-n", namespace,
					"-o", "jsonpath={.status.synced}",
				)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("true"),
					"expected the ScanPolicy exemption to unblock the Version CR")
			}, 5*time.Minute, 10*time.Second).Should(Succeed())

			By("asserting that the findings are annotated as exempted rather than dropped")
			cmd = exec.Command("kubectl", "get", "version", exemptVersionName,
				"-n", namespace,
				"-o", "jsonpath={.status.sourceScan.findings}",
			)
			findings, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(findings).To(ContainSubstring("vulnerabilityID"),
				"expected exempted findings to remain visible for auditing")
			Expect(findings).To(ContainSubstring(`"exempted":true`),
				"expected findings to be marked as exempted")
			Expect(findings).To(ContainSubstring(exemptPolicyName),
				"expected findings to record the ScanPolicy that exempted them")
		})

		It("should populate ScanPolicy status with match counts and shadowing", func() {
			By("waiting for the ScanPolicy controller to observe the winning policy")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "scanpolicy", exemptPolicyName,
					"-n", namespace,
					"-o", "jsonpath={.status.activeExemptions}",
				)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("1"), "expected one active exemption")

				cmd = exec.Command("kubectl", "get", "scanpolicy", exemptPolicyName,
					"-n", namespace,
					"-o", "jsonpath={.status.matchedVersions}",
				)
				matched, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(matched).NotTo(Equal("0"),
					"expected the ScanPolicy to report at least one matched Version")
			}, 2*time.Minute, 5*time.Second).Should(Succeed())

			By("asserting the lower priority ScanPolicy reports itself as superseded")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "scanpolicy", blockPolicyName,
					"-n", namespace,
					"-o", "jsonpath={.status.supersededBy}",
				)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(ContainSubstring(exemptPolicyName),
					"expected the lower priority ScanPolicy to be superseded")
			}, 2*time.Minute, 5*time.Second).Should(Succeed())
		})

		It("should re-evaluate an already-synced Version when a blocking policy is added", func() {
			By("adding a higher priority blocking ScanPolicy after the Version is synced")
			policyYAML := fmt.Sprintf(`apiVersion: opendepot.defdev.io/v1alpha1
kind: ScanPolicy
metadata:
  name: %s
  namespace: %s
spec:
  priority: 30
  severityThreshold: LOW
`, lateBlockName, namespace)
			policyFile := filepath.Join(GinkgoT().TempDir(), "late-block-policy.yaml")
			Expect(os.WriteFile(policyFile, []byte(policyYAML), 0600)).To(Succeed())

			cmd := exec.Command("kubectl", "apply", "-f", policyFile)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to apply late blocking ScanPolicy")

			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "version", exemptVersionName,
					"-n", namespace, "-o", "jsonpath={.status.synced}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).NotTo(Equal("true"),
					"expected a newly written blocking policy to re-evaluate the synced Version")
			}, 2*time.Minute, 5*time.Second).Should(Succeed())

			By("removing the late blocking policy and confirming the exemption remains effective")
			cmd = exec.Command("kubectl", "delete", "scanpolicy", lateBlockName,
				"-n", namespace, "--ignore-not-found")
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "version", exemptVersionName,
					"-n", namespace, "-o", "jsonpath={.status.synced}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("true"))
			}, 2*time.Minute, 5*time.Second).Should(Succeed())
		})
	})

	Context("Provider Source Scan", Ordered, func() {
		const (
			scanProviderName = "null-version-e2e"
			scanVersion1     = "3.2.3"
			scanVersion2     = "3.2.0"
			scanVersionCR1   = "null-version-e2e-3-2-3-linux-amd64"
			scanVersionCR2   = "null-version-e2e-3-2-0-linux-amd64"
			scanStoragePath  = "/data/modules"
			scanSourceRepo   = "https://github.com/hashicorp/terraform-provider-null"
		)

		AfterAll(func() {
			By("removing Provider Source Scan test resources")
			cmd := exec.Command("kubectl", "delete", "version", scanVersionCR1, scanVersionCR2,
				"-n", namespace, "--ignore-not-found")
			_, _ = utils.Run(cmd)
			cmd = exec.Command("kubectl", "delete", "provider", scanProviderName,
				"-n", namespace, "--ignore-not-found")
			_, _ = utils.Run(cmd)

			By("disabling scanning to restore baseline state")
			chartPath, err := utils.GetChartPath()
			if err != nil {
				return
			}
			cmd = exec.Command("helm", "upgrade", helmReleaseName, chartPath,
				"--reuse-values",
				"--namespace", namespace,
				"--set", "scanning.enabled=false",
				"--set", "scanning.providerScanning=false",
				"--wait",
				"--timeout", "3m",
			)
			_, _ = utils.Run(cmd)
		})

		BeforeAll(func() {
			By("deleting any existing trivy cache PVC to avoid immutable field conflicts")
			cmd := exec.Command("kubectl", "delete", "pvc", "opendepot-trivy-cache",
				"-n", namespace, "--ignore-not-found")
			_, _ = utils.Run(cmd)

			By("upgrading Helm release to enable scanning with offline=false")
			chartPath, err := utils.GetChartPath()
			ExpectWithOffset(1, err).NotTo(HaveOccurred())

			baseRepo, baseTag := utils.SplitImageRef(projectImage)
			cmd = exec.Command("helm", "upgrade", helmReleaseName, chartPath,
				"--reuse-values",
				"--namespace", namespace,
				"--set", fmt.Sprintf("version.image.repository=%s", baseRepo),
				"--set", fmt.Sprintf("version.image.tag=%s", baseTag),
				"--set", "scanning.enabled=true",
				"--set", "scanning.providerScanning=true",
				"--set", "scanning.offline=false",
				"--set", "scanning.cache.accessMode=ReadWriteOnce",
				"--wait",
				"--timeout", "3m",
			)
			_, err = utils.Run(cmd)
			ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to upgrade Helm release to enable scanning")

			By("seeding the Trivy vulnerability DB so the provider source scan does not depend on a first-run download")
			Expect(utils.SeedTrivyDB(namespace)).To(Succeed())

			By("applying the null Provider CR with two versions")
			providerYAML := fmt.Sprintf(`apiVersion: opendepot.defdev.io/v1alpha1
kind: Provider
metadata:
  name: "%s"
  namespace: %s
spec:
  providerConfig:
    githubClientConfig:
      useAuthenticatedClient: true
    name: "null"
    sourceRepository: "%s"
    operatingSystems:
      - linux
    architectures:
      - amd64
    storageConfig:
      fileSystem:
        directoryPath: %s
  versions:
    - version: "%s"
    - version: "%s"
`, scanProviderName, namespace, scanSourceRepo, scanStoragePath, scanVersion1, scanVersion2)

			providerFile := filepath.Join(GinkgoT().TempDir(), "scan-provider.yaml")
			Expect(os.WriteFile(providerFile, []byte(providerYAML), 0600)).To(Succeed())
			cmd = exec.Command("kubectl", "apply", "-f", providerFile)
			_, err = utils.Run(cmd)
			ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to apply Provider CR")

			By("creating Version CR 1 manually (provider controller is not deployed in this suite)")
			version1YAML := fmt.Sprintf(`apiVersion: opendepot.defdev.io/v1alpha1
kind: Version
metadata:
  name: "%s"
  namespace: %s
  labels:
    opendepot.defdev.io/provider: "%s"
spec:
  type: Provider
  version: "%s"
  operatingSystem: linux
  architecture: amd64
  providerConfigRef:
    githubClientConfig:
      useAuthenticatedClient: true
    name: "null"
    sourceRepository: "%s"
    storageConfig:
      fileSystem:
        directoryPath: %s
`, scanVersionCR1, namespace, scanProviderName, scanVersion1, scanSourceRepo, scanStoragePath)

			v1File := filepath.Join(GinkgoT().TempDir(), "scan-version1.yaml")
			Expect(os.WriteFile(v1File, []byte(version1YAML), 0600)).To(Succeed())
			cmd = exec.Command("kubectl", "apply", "-f", v1File)
			_, err = utils.Run(cmd)
			ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to apply Version CR 1")

			By("creating Version CR 2 manually")
			version2YAML := fmt.Sprintf(`apiVersion: opendepot.defdev.io/v1alpha1
kind: Version
metadata:
  name: "%s"
  namespace: %s
  labels:
    opendepot.defdev.io/provider: "%s"
spec:
  type: Provider
  version: "%s"
  operatingSystem: linux
  architecture: amd64
  providerConfigRef:
    githubClientConfig:
      useAuthenticatedClient: true
    name: "null"
    sourceRepository: "%s"
    storageConfig:
      fileSystem:
        directoryPath: %s
`, scanVersionCR2, namespace, scanProviderName, scanVersion2, scanSourceRepo, scanStoragePath)

			v2File := filepath.Join(GinkgoT().TempDir(), "scan-version2.yaml")
			Expect(os.WriteFile(v2File, []byte(version2YAML), 0600)).To(Succeed())
			cmd = exec.Command("kubectl", "apply", "-f", v2File)
			_, err = utils.Run(cmd)
			ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to apply Version CR 2")
		})

		It("should populate sourceScan on Version CR 1", func() {
			By("waiting for Version CR 1 to reach synced=true")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "version", scanVersionCR1,
					"-n", namespace,
					"-o", "jsonpath={.status.synced}",
				)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("true"))
			}, 5*time.Minute, 10*time.Second).Should(Succeed())

			By("waiting for sourceScan.scannedAt to be set on Version CR 1")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "version", scanVersionCR1,
					"-n", namespace,
					"-o", "jsonpath={.status.sourceScan.scannedAt}",
				)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).NotTo(BeEmpty(), "expected sourceScan.scannedAt to be set on %s", scanVersionCR1)
			}, 5*time.Minute, 15*time.Second).Should(Succeed())
		})

		It("should populate sourceScan on Version CR 2", func() {
			By("waiting for Version CR 2 to reach synced=true")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "version", scanVersionCR2,
					"-n", namespace,
					"-o", "jsonpath={.status.synced}",
				)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("true"))
			}, 5*time.Minute, 10*time.Second).Should(Succeed())

			By("waiting for sourceScan.scannedAt to be set on Version CR 2")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "version", scanVersionCR2,
					"-n", namespace,
					"-o", "jsonpath={.status.sourceScan.scannedAt}",
				)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).NotTo(BeEmpty(), "expected sourceScan.scannedAt to be set on %s", scanVersionCR2)
			}, 5*time.Minute, 15*time.Second).Should(Succeed())
		})

		It("should report at least one source finding on Version CR 1", func() {
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "version", scanVersionCR1,
					"-n", namespace,
					"-o", "jsonpath={.status.sourceScan.findings[0].vulnerabilityID}",
				)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).NotTo(BeEmpty(), "expected at least one source finding on %s", scanVersionCR1)
			}, 5*time.Minute, 15*time.Second).Should(Succeed())
		})
	})

	Context("Module README", Ordered, func() {
		const (
			readmeVersionName = "readme-s3-bucket-4-3-0"
			readmeModuleName  = "terraform-aws-s3-bucket"
			readmeVersion     = "4.3.0"
			readmeRepoOwner   = "terraform-aws-modules"
			readmeRepoURL     = "https://github.com/terraform-aws-modules/terraform-aws-s3-bucket"
			readmeStorageDir  = "/data/modules"
		)

		var readmeConfigMapName string

		AfterAll(func() {
			By("removing the Module README Version CR")
			cmd := exec.Command("kubectl", "delete", "version", readmeVersionName,
				"-n", namespace, "--ignore-not-found")
			_, _ = utils.Run(cmd)
		})

		It("should sync a module Version CR and populate status.readmeConfigMapRef", func() {
			By("applying an inline module Version CR for terraform-aws-s3-bucket 4.3.0")
			versionYAML := fmt.Sprintf(`apiVersion: opendepot.defdev.io/v1alpha1
kind: Version
metadata:
  name: %s
  namespace: %s
spec:
  type: Module
  version: %q
  moduleConfigRef:
    name: %q
    repoOwner: %q
    repoUrl: %q
    githubClientConfig:
      useAuthenticatedClient: true
    storageConfig:
      fileSystem:
        directoryPath: %s
`, readmeVersionName, namespace, readmeVersion, readmeModuleName, readmeRepoOwner, readmeRepoURL, readmeStorageDir)

			versionFile := filepath.Join(GinkgoT().TempDir(), "readme-version.yaml")
			Expect(os.WriteFile(versionFile, []byte(versionYAML), 0600)).To(Succeed())

			cmd := exec.Command("kubectl", "apply", "-f", versionFile)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to apply Module README Version CR")

			By("waiting for the Version CR to reach synced=true")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "version", readmeVersionName,
					"-n", namespace,
					"-o", "jsonpath={.status.synced}",
				)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("true"),
					"expected Version CR to reach synced=true")
			}, 5*time.Minute, 10*time.Second).Should(Succeed())

			By("waiting for status.readmeConfigMapRef to be populated")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "version", readmeVersionName,
					"-n", namespace,
					"-o", "jsonpath={.status.readmeConfigMapRef.name}",
				)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).NotTo(BeEmpty(), "expected status.readmeConfigMapRef.name to be set")
				readmeConfigMapName = strings.TrimSpace(output)
			}, 5*time.Minute, 10*time.Second).Should(Succeed())

			By("asserting status.readmeConfigMapRef.key is 'README.md'")
			cmd = exec.Command("kubectl", "get", "version", readmeVersionName,
				"-n", namespace,
				"-o", "jsonpath={.status.readmeConfigMapRef.key}",
			)
			key, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(strings.TrimSpace(key)).To(Equal("README.md"))
		})

		It("should create a ConfigMap owned by the Version CR with base64 encoded README content", func() {
			Expect(readmeConfigMapName).NotTo(BeEmpty(), "readmeConfigMapName must have been captured by the previous spec")

			By("asserting the ConfigMap exists with a non-empty, base64 decodable README entry")
			cmd := exec.Command("kubectl", "get", "configmap", readmeConfigMapName,
				"-n", namespace,
				"-o", `jsonpath={.data['README\.md']}`,
			)
			encoded, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(encoded).NotTo(BeEmpty(), "expected ConfigMap to contain a non-empty README.md entry")

			decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
			Expect(err).NotTo(HaveOccurred(), "expected README.md entry to be valid base64")
			Expect(len(decoded)).To(BeNumerically(">", 0), "expected decoded README content to be non-empty")

			By("asserting the ConfigMap is owned by the Version CR for cascading delete")
			cmd = exec.Command("kubectl", "get", "configmap", readmeConfigMapName,
				"-n", namespace,
				"-o", "jsonpath={.metadata.ownerReferences[0].kind}",
			)
			ownerKind, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(strings.TrimSpace(ownerKind)).To(Equal("Version"))

			cmd = exec.Command("kubectl", "get", "configmap", readmeConfigMapName,
				"-n", namespace,
				"-o", "jsonpath={.metadata.ownerReferences[0].name}",
			)
			ownerName, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(strings.TrimSpace(ownerName)).To(Equal(readmeVersionName))
		})

		It("should re-fetch the README and remain synced when forceSync is set", func() {
			By("patching the Version CR to set forceSync=true")
			cmd := exec.Command("kubectl", "patch", "version", readmeVersionName,
				"-n", namespace,
				"--type=merge",
				"-p", `{"spec":{"forceSync":true}}`,
			)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to patch Version CR with forceSync=true")

			By("waiting for the controller to reset forceSync back to false after a successful re-sync")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "version", readmeVersionName,
					"-n", namespace,
					"-o", "jsonpath={.spec.forceSync}",
				)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				// forceSync has omitempty, so a reset-to-false value is omitted from the
				// jsonpath output entirely rather than rendered as the literal string "false".
				g.Expect(strings.TrimSpace(output)).NotTo(Equal("true"),
					"expected forceSync to be reset to false after a successful re-sync")
			}, 5*time.Minute, 10*time.Second).Should(Succeed())

			By("asserting the Version CR remains synced and readmeConfigMapRef is still populated")
			cmd = exec.Command("kubectl", "get", "version", readmeVersionName,
				"-n", namespace,
				"-o", "jsonpath={.status.synced}",
			)
			synced, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(strings.TrimSpace(synced)).To(Equal("true"))

			cmd = exec.Command("kubectl", "get", "version", readmeVersionName,
				"-n", namespace,
				"-o", "jsonpath={.status.readmeConfigMapRef.name}",
			)
			ref, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(strings.TrimSpace(ref)).To(Equal(readmeConfigMapName))
		})
	})

	Context("Assembly Line", Ordered, func() {
		const (
			contractVersionName = "contract-s3-bucket-4-3-0"
			contractModuleName  = "terraform-aws-s3-bucket"
			contractVersion     = "4.3.0"
			contractRepoOwner   = "terraform-aws-modules"
			contractRepoURL     = "https://github.com/terraform-aws-modules/terraform-aws-s3-bucket"
			assemblyStorageDir  = "/data/modules"
			schemaProviderName  = "null-assembly-e2e"
			schemaVersion       = "3.2.3"
			schemaSourceRepo    = "https://github.com/hashicorp/terraform-provider-null"
		)

		// A provider schema is only extracted for the platform the controller itself runs
		// on. The Kind node shares the host's architecture, so the matching Version CR must
		// use runtime.GOARCH and the non-matching one must use the opposite architecture.
		matchingArch := runtime.GOARCH
		nonMatchingArch := "amd64"
		if matchingArch == "amd64" {
			nonMatchingArch = "arm64"
		}

		schemaVersionCR := fmt.Sprintf("null-assembly-e2e-3-2-3-linux-%s", matchingArch)
		otherPlatformVersionCR := fmt.Sprintf("null-assembly-e2e-3-2-3-linux-%s", nonMatchingArch)

		var contractConfigMapName string

		AfterAll(func() {
			By("removing Assembly Line test resources")
			cmd := exec.Command("kubectl", "delete", "version",
				contractVersionName, schemaVersionCR, otherPlatformVersionCR,
				"-n", namespace, "--ignore-not-found")
			_, _ = utils.Run(cmd)

			cmd = exec.Command("kubectl", "delete", "provider", schemaProviderName,
				"-n", namespace, "--ignore-not-found")
			_, _ = utils.Run(cmd)

			By("disabling Assembly Line to restore baseline state")
			chartPath, err := utils.GetChartPath()
			if err != nil {
				return
			}

			cmd = exec.Command("helm", "upgrade", helmReleaseName, chartPath,
				"--reuse-values",
				"--namespace", namespace,
				"--set", "assembly.enabled=false",
				"--wait",
				"--timeout", "3m",
			)
			_, _ = utils.Run(cmd)
		})

		It("should not derive a contract while Assembly Line is disabled", func() {
			By("applying an inline module Version CR while assembly.enabled is false")
			versionYAML := fmt.Sprintf(`apiVersion: opendepot.defdev.io/v1alpha1
kind: Version
metadata:
  name: %s
  namespace: %s
spec:
  type: Module
  version: %q
  moduleConfigRef:
    name: %q
    provider: aws
    repoOwner: %q
    repoUrl: %q
    githubClientConfig:
      useAuthenticatedClient: true
    storageConfig:
      fileSystem:
        directoryPath: %s
`, contractVersionName, namespace, contractVersion, contractModuleName, contractRepoOwner, contractRepoURL, assemblyStorageDir)

			versionFile := filepath.Join(GinkgoT().TempDir(), "contract-version.yaml")
			Expect(os.WriteFile(versionFile, []byte(versionYAML), 0600)).To(Succeed())

			cmd := exec.Command("kubectl", "apply", "-f", versionFile)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to apply Assembly Line Version CR")

			By("waiting for the Version CR to reach synced=true")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "version", contractVersionName,
					"-n", namespace,
					"-o", "jsonpath={.status.synced}",
				)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("true"), "expected Version CR to reach synced=true")
			}, 5*time.Minute, 10*time.Second).Should(Succeed())

			By("asserting status.contractConfigMapRef is unset")
			cmd = exec.Command("kubectl", "get", "version", contractVersionName,
				"-n", namespace,
				"-o", "jsonpath={.status.contractConfigMapRef.name}",
			)
			output, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(strings.TrimSpace(output)).To(BeEmpty(),
				"expected no contract to be derived while assembly.enabled is false")
		})

		It("should backfill a contract for an already synced module once Assembly Line is enabled", func() {
			By("upgrading Helm release to enable Assembly Line")
			chartPath, err := utils.GetChartPath()
			Expect(err).NotTo(HaveOccurred())

			cmd := exec.Command("helm", "upgrade", helmReleaseName, chartPath,
				"--reuse-values",
				"--namespace", namespace,
				"--set", "assembly.enabled=true",
				"--set", "ui.baseUrl=http://localhost",
				"--wait",
				"--timeout", "3m",
			)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to upgrade Helm release to enable Assembly Line")

			By("triggering reconciliation for the already synced Version")
			cmd = exec.Command("kubectl", "patch", "version", contractVersionName,
				"-n", namespace,
				"--type=merge",
				"-p", `{"spec":{"forceSync":true}}`,
			)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to trigger Assembly Line reconciliation")

			By("waiting for status.contractConfigMapRef to be populated")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "version", contractVersionName,
					"-n", namespace,
					"-o", "jsonpath={.status.contractConfigMapRef.name}",
				)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(strings.TrimSpace(output)).NotTo(BeEmpty(),
					"expected status.contractConfigMapRef.name to be set")
				contractConfigMapName = strings.TrimSpace(output)
			}, 5*time.Minute, 10*time.Second).Should(Succeed())

			By("asserting status.contractConfigMapRef.key is 'contract.json'")
			cmd = exec.Command("kubectl", "get", "version", contractVersionName,
				"-n", namespace,
				"-o", "jsonpath={.status.contractConfigMapRef.key}",
			)
			key, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(strings.TrimSpace(key)).To(Equal("contract.json"))

			By("asserting the module grades as partial: it ships local submodules and its provider schema is absent")
			cmd = exec.Command("kubectl", "get", "version", contractVersionName,
				"-n", namespace,
				"-o", "jsonpath={.status.contractConfigMapRef.grade}",
			)
			grade, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(strings.TrimSpace(grade)).To(Equal("partial"))
		})

		It("should store a gzipped contract in a ConfigMap owned by the Version CR", func() {
			Expect(contractConfigMapName).NotTo(BeEmpty(), "contractConfigMapName must have been captured by the previous spec")

			By("reading and decoding the contract payload")
			cmd := exec.Command("kubectl", "get", "configmap", contractConfigMapName,
				"-n", namespace,
				"-o", `jsonpath={.data['contract\.json']}`,
			)
			encoded, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(encoded).NotTo(BeEmpty(), "expected ConfigMap to contain a non-empty contract.json entry")

			contract, err := decodeE2EContract(strings.TrimSpace(encoded))
			Expect(err).NotTo(HaveOccurred(), "expected contract.json entry to be base64 encoded gzipped JSON")

			Expect(contract.SchemaVersion).To(Equal("assembly.module.v1"))
			Expect(contract.Module.Name).To(Equal(contractModuleName))
			Expect(contract.Module.Version).To(Equal(contractVersion))
			Expect(contract.Compatibility.Grade).To(Equal("partial"))
			Expect(len(contract.Variables)).To(BeNumerically(">", 0), "expected the module contract to declare variables")
			Expect(len(contract.Outputs)).To(BeNumerically(">", 0), "expected the module contract to declare outputs")

			By("asserting every variable carries an encoded cty type")
			for _, v := range contract.Variables {
				Expect(v.Name).NotTo(BeEmpty())
				Expect(len(v.Type)).To(BeNumerically(">", 0), "expected variable %q to carry a type", v.Name)
			}

			By("asserting the 'bucket' variable is typed as a string")
			var bucketType string
			for _, v := range contract.Variables {
				if v.Name == "bucket" {
					bucketType = string(v.Type)
					break
				}
			}
			Expect(bucketType).To(Equal(`"string"`))

			By("asserting every output carries a confidence rating")
			for _, o := range contract.Outputs {
				Expect(o.Confidence).To(BeElementOf("exact", "inferred", "unknown"),
					"unexpected confidence %q on output %q", o.Confidence, o.Name)
			}

			By("asserting the ConfigMap is owned by the Version CR for cascading delete")
			cmd = exec.Command("kubectl", "get", "configmap", contractConfigMapName,
				"-n", namespace,
				"-o", "jsonpath={.metadata.ownerReferences[0].kind}",
			)
			ownerKind, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(strings.TrimSpace(ownerKind)).To(Equal("Version"))

			cmd = exec.Command("kubectl", "get", "configmap", contractConfigMapName,
				"-n", namespace,
				"-o", "jsonpath={.metadata.ownerReferences[0].name}",
			)
			ownerName, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(strings.TrimSpace(ownerName)).To(Equal(contractVersionName))
		})

		It("should extract a reduced provider schema for a matching platform", func() {
			By("applying the null Provider CR")
			providerYAML := fmt.Sprintf(`apiVersion: opendepot.defdev.io/v1alpha1
kind: Provider
metadata:
  name: "%s"
  namespace: %s
spec:
  providerConfig:
    githubClientConfig:
      useAuthenticatedClient: true
    name: "null"
    sourceRepository: "%s"
    operatingSystems:
      - linux
    architectures:
      - %s
      - %s
    storageConfig:
      fileSystem:
        directoryPath: %s
  versions:
    - version: "%s"
`, schemaProviderName, namespace, schemaSourceRepo, matchingArch, nonMatchingArch, assemblyStorageDir, schemaVersion)

			providerFile := filepath.Join(GinkgoT().TempDir(), "assembly-provider.yaml")
			Expect(os.WriteFile(providerFile, []byte(providerYAML), 0600)).To(Succeed())

			cmd := exec.Command("kubectl", "apply", "-f", providerFile)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to apply Provider CR")

			By("creating the matching-platform provider Version CR")
			versionYAML := fmt.Sprintf(`apiVersion: opendepot.defdev.io/v1alpha1
kind: Version
metadata:
  name: "%s"
  namespace: %s
  labels:
    opendepot.defdev.io/provider: "%s"
spec:
  type: Provider
  version: "%s"
  operatingSystem: linux
  architecture: %s
  providerConfigRef:
    githubClientConfig:
      useAuthenticatedClient: true
    name: "null"
    namespace: hashicorp
    sourceRepository: "%s"
    storageConfig:
      fileSystem:
        directoryPath: %s
`, schemaVersionCR, namespace, schemaProviderName, schemaVersion, matchingArch, schemaSourceRepo, assemblyStorageDir)

			versionFile := filepath.Join(GinkgoT().TempDir(), "assembly-provider-version.yaml")
			Expect(os.WriteFile(versionFile, []byte(versionYAML), 0600)).To(Succeed())

			cmd = exec.Command("kubectl", "apply", "-f", versionFile)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to apply provider Version CR")

			By("waiting for status.providerSchemaRef to be populated")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "version", schemaVersionCR,
					"-n", namespace,
					"-o", "jsonpath={.status.providerSchemaRef.key}",
				)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(strings.TrimSpace(output)).NotTo(BeEmpty(),
					"expected status.providerSchemaRef.key to be set")
			}, 8*time.Minute, 15*time.Second).Should(Succeed())

			By("asserting the schema reference carries a sha256 digest and a non-zero size")
			cmd = exec.Command("kubectl", "get", "version", schemaVersionCR,
				"-n", namespace,
				"-o", "jsonpath={.status.providerSchemaRef.digest}",
			)
			digest, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(strings.TrimSpace(digest)).To(HaveLen(64), "expected a hex encoded sha256 digest")

			cmd = exec.Command("kubectl", "get", "version", schemaVersionCR,
				"-n", namespace,
				"-o", "jsonpath={.status.providerSchemaRef.sizeBytes}",
			)
			size, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(strings.TrimSpace(size)).NotTo(Equal("0"), "expected a non-zero schema size")
		})

		It("should not extract a schema for a provider Version on another platform", func() {
			By("creating the non-matching-platform provider Version CR")
			versionYAML := fmt.Sprintf(`apiVersion: opendepot.defdev.io/v1alpha1
kind: Version
metadata:
  name: "%s"
  namespace: %s
  labels:
    opendepot.defdev.io/provider: "%s"
spec:
  type: Provider
  version: "%s"
  operatingSystem: linux
  architecture: %s
  providerConfigRef:
    githubClientConfig:
      useAuthenticatedClient: true
    name: "null"
    namespace: hashicorp
    sourceRepository: "%s"
    storageConfig:
      fileSystem:
        directoryPath: %s
`, otherPlatformVersionCR, namespace, schemaProviderName, schemaVersion, nonMatchingArch, schemaSourceRepo, assemblyStorageDir)

			versionFile := filepath.Join(GinkgoT().TempDir(), "assembly-other-platform-version.yaml")
			Expect(os.WriteFile(versionFile, []byte(versionYAML), 0600)).To(Succeed())

			cmd := exec.Command("kubectl", "apply", "-f", versionFile)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to apply non-matching platform Version CR")

			By("waiting for the Version CR to reach synced=true")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "version", otherPlatformVersionCR,
					"-n", namespace,
					"-o", "jsonpath={.status.synced}",
				)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("true"), "expected Version CR to reach synced=true")
			}, 8*time.Minute, 15*time.Second).Should(Succeed())

			By("asserting status.providerSchemaRef remains unset")
			cmd = exec.Command("kubectl", "get", "version", otherPlatformVersionCR,
				"-n", namespace,
				"-o", "jsonpath={.status.providerSchemaRef.key}",
			)
			output, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(strings.TrimSpace(output)).To(BeEmpty(),
				"expected no schema to be extracted for a platform the controller does not run on")
		})
	})

	Context("Branch sourcing", Ordered, func() {
		const (
			branchSkillName     = "acquire-codebase-knowledge"
			branchJevSkillName  = "conventional-commit"
			branchDepotName     = "branch-e2e-depot"
			branchStorageDir    = "/data/modules"
			branchServerPort    = "18082"
			branchJevSecretName = "opendepot-branch-jev"
			branchJevSecretKey  = "jevToken"
			branchPinnedVersion = "1.0.0"
			branchServerBase    = "http://127.0.0.1:" + branchServerPort + "/opendepot/agents/v1/" + namespace + "/skills/" + branchSkillName
		)

		type branchEntry struct {
			commit       string
			discoveredAt time.Time
		}

		type branchSkillEntry struct {
			Commit       string `json:"commit"`
			State        string `json:"state"`
			Version      string `json:"version"`
			DiscoveredAt string `json:"discoveredAt"`
		}

		type branchVersionRef struct {
			Name   string `json:"name"`
			Synced bool   `json:"synced"`
		}

		type branchVersionsResponse struct {
			Versions []struct {
				Version string `json:"version"`
			} `json:"versions"`
		}

		type branchDownloadResponse struct {
			Version string `json:"version"`
			Shasum  string `json:"shasum"`
		}

		var branchPFCancel context.CancelFunc

		helmUpgrade := func(sets ...string) {
			chartPath, err := utils.GetChartPath()
			Expect(err).NotTo(HaveOccurred())

			args := []string{"upgrade", helmReleaseName, chartPath, "--reuse-values", "--namespace", namespace}
			for _, set := range sets {
				args = append(args, "--set", set)
			}

			args = append(args, "--wait", "--timeout", "3m")
			_, err = utils.Run(exec.Command("helm", args...))
			Expect(err).NotTo(HaveOccurred(), "Failed to upgrade Helm release")
		}

		applyManifest := func(manifest string) {
			manifestFile := filepath.Join(GinkgoT().TempDir(), "branch.yaml")
			Expect(os.WriteFile(manifestFile, []byte(manifest), 0600)).To(Succeed())

			_, err := utils.Run(exec.Command("kubectl", "apply", "-f", manifestFile))
			Expect(err).NotTo(HaveOccurred(), "Failed to apply branch sourcing manifest")
		}

		jevSecretRefBlock := func(jev bool) string {
			if !jev {
				return ""
			}

			return fmt.Sprintf(`
    jevSecretRef:
      name: %s
      key: %s
    jevPolicy: {}`, branchJevSecretName, branchJevSecretKey)
		}

		// applyBranchSkill applies a Skill sourced from the fixture branch, seeded with the given Pending entries.
		// The agent controller creates the branch Versions and copies their outcome back to the entries.
		applyBranchSkill := func(name string, jev bool, entries ...branchEntry) {
			fixturePath := utils.SkillFixturePath
			if jev {
				fixturePath = utils.JevSkillFixturePath
			}

			versions := ""
			for _, entry := range entries {
				versions += fmt.Sprintf(`
    - commit: %s
      discoveredAt: %s
      state: Pending`, entry.commit, entry.discoveredAt.UTC().Format(time.RFC3339))
			}

			applyManifest(fmt.Sprintf(`apiVersion: opendepot.defdev.io/v1alpha1
kind: Skill
metadata:
  name: %s
  namespace: %s
spec:
  agentSourceConfig:
    githubClientConfig:
      useAuthenticatedClient: true
    name: %s
    repoOwner: github
    repoUrl: https://github.com/github/awesome-copilot
    path: %s
    ref: %s
    branchPolicy:
      minVersionInterval: 0s
    storageConfig:
      fileSystem:
        directoryPath: %s%s
  versions:%s
`, name, namespace, name, fixturePath, utils.BranchFixtureRef, branchStorageDir, jevSecretRefBlock(jev), versions))
		}

		// branchSourceBlock renders one Depot skillConfigs entry that sources the fixture branch.
		branchSourceBlock := func(name, path string, jev bool) string {
			return fmt.Sprintf(`
  - name: %s
    repoOwner: github
    repoUrl: https://github.com/github/awesome-copilot
    githubClientConfig:
      useAuthenticatedClient: true
    path: %s
    ref: %s
    branchPolicy:
      minVersionInterval: 0s
    storageConfig:
      fileSystem:
        directoryPath: %s%s`, name, path, utils.BranchFixtureRef, branchStorageDir, jevSecretRefBlock(jev))
		}

		// applyBranchDepot applies the Depot that sources the fixture branch. The Jev Skill is managed only when jev is set.
		applyBranchDepot := func(jev bool) {
			skillConfigs := branchSourceBlock(branchSkillName, utils.SkillFixturePath, false)
			if jev {
				skillConfigs += branchSourceBlock(branchJevSkillName, utils.JevSkillFixturePath, true)
			}

			applyManifest(fmt.Sprintf(`apiVersion: opendepot.defdev.io/v1alpha1
kind: Depot
metadata:
  name: %s
  namespace: %s
spec:
  skillConfigs:%s
`, branchDepotName, namespace, skillConfigs))
		}

		// triggerDepot changes an annotation on the Depot so that it reconciles its branch sources.
		triggerDepot := func() {
			_, err := utils.Run(exec.Command("kubectl", "annotate", "depot", branchDepotName,
				"-n", namespace,
				fmt.Sprintf("opendepot.defdev.io/e2e-trigger=%d", time.Now().UnixNano()),
				"--overwrite",
			))
			Expect(err).NotTo(HaveOccurred(), "Failed to annotate the Depot to trigger a reconcile")
		}

		versionField := func(name, jsonpath string) (string, error) {
			output, err := utils.Run(exec.Command("kubectl", "get", "version", name,
				"-n", namespace,
				"-o", "jsonpath="+jsonpath,
			))

			return strings.TrimSpace(output), err
		}

		mustVersionField := func(name, jsonpath string) string {
			value, err := versionField(name, jsonpath)
			Expect(err).NotTo(HaveOccurred())

			return value
		}

		// skillEntries returns the Skill spec entries, which the Depot and agent controller maintain.
		skillEntries := func(skillName string) ([]branchSkillEntry, error) {
			output, err := utils.Run(exec.Command("kubectl", "get", "skill", skillName,
				"-n", namespace,
				"-o", "jsonpath={.spec.versions}",
			))
			if err != nil {
				return nil, err
			}

			var entries []branchSkillEntry
			if strings.TrimSpace(output) == "" {
				return entries, nil
			}

			return entries, json.Unmarshal([]byte(output), &entries)
		}

		// skillVersionRefs returns the Skill status references that the agent controller writes for synced Versions.
		skillVersionRefs := func(skillName string) (map[string]branchVersionRef, error) {
			output, err := utils.Run(exec.Command("kubectl", "get", "skill", skillName,
				"-n", namespace,
				"-o", "jsonpath={.status.versionRefs}",
			))
			if err != nil {
				return nil, err
			}

			refs := map[string]branchVersionRef{}
			if strings.TrimSpace(output) == "" {
				return refs, nil
			}

			return refs, json.Unmarshal([]byte(output), &refs)
		}

		// headCommitOf returns the entry commit that the Depot added for the branch head, which is any commit other than the pinned SHA.
		headCommitOf := func(entries []branchSkillEntry, pinned string) string {
			for _, entry := range entries {
				if entry.Commit != pinned {
					return entry.Commit
				}
			}

			return ""
		}

		branchVersionName := func(skillName, commit string) string {
			return fmt.Sprintf("skill-%s-%s", skillName, commit)
		}

		branchGet := func(path string) (int, []byte, error) {
			resp, err := http.Get(branchServerBase + path)
			if err != nil {
				return 0, nil, err
			}
			defer func() { _ = resp.Body.Close() }()

			body, err := io.ReadAll(resp.Body)
			return resp.StatusCode, body, err
		}

		// parseBranchSemver encodes a major.minor.patch version as a comparable integer.
		parseBranchSemver := func(version string) (int, error) {
			var major, minor, patch int
			_, err := fmt.Sscanf(version, "%d.%d.%d", &major, &minor, &patch)

			return major*1000000 + minor*1000 + patch, err
		}

		BeforeAll(func() {
			repoRoot, err := utils.GetRepoRoot()
			Expect(err).NotTo(HaveOccurred(), "Failed to determine repo root")

			buildAndLoad := func(image, dockerfile string, paths []string) {
				hash, err := utils.ComputeBuildContextHash(repoRoot, paths)
				Expect(err).NotTo(HaveOccurred(), "Failed to compute build hash for "+image)

				if utils.NeedsRebuild(image, hash) {
					buildCmd := exec.Command("docker", "build",
						"-t", image,
						"--label", "opendepot.build.hash="+hash,
						"-f", dockerfile,
						".",
					)
					_, err = utils.RunAt(buildCmd, repoRoot)
					Expect(err).NotTo(HaveOccurred(), "Failed to build "+image)
				}

				Expect(utils.LoadImageToKindClusterWithName(image)).To(Succeed(), "Failed to load "+image+" into Kind")
			}

			By("building and loading the depot and agent controller images so the chart runs local builds")
			sharedPaths := []string{"api", "pkg", "go.work", "go.work.sum"}
			buildAndLoad(branchDepotImage, "services/depot/Dockerfile", append([]string{"services/depot"}, sharedPaths...))
			buildAndLoad(branchAgentImage, "services/agent/Dockerfile", append([]string{"services/agent"}, sharedPaths...))

			By("enabling the Depot, the Agent, and branch sourcing so the real controllers run")
			helmUpgrade(
				"version.gpg.secretName="+signingSecretName,
				"server.gpg.secretName="+signingSecretName,
				"depot.enabled=true",
				"depot.image.repository=depot-controller",
				"depot.image.tag=e2e-test",
				"agent.enabled=true",
				"agent.image.repository=agent-controller",
				"agent.image.tag=e2e-test",
				"branchSourcing.enabled=true",
				"scanning.enabled=true",
				"scanning.providerScanning=true",
				"scanning.offline=true",
			)

			By("seeding the Trivy vulnerability DB that branch Version scans read in offline mode")
			Expect(utils.SeedTrivyDB(namespace)).To(Succeed())
		})

		AfterAll(func() {
			if branchPFCancel != nil {
				branchPFCancel()
			}

			By("removing branch sourcing test resources")
			cmd := exec.Command("kubectl", "delete", "depot", branchDepotName,
				"-n", namespace, "--ignore-not-found")
			_, _ = utils.Run(cmd)

			cmd = exec.Command("kubectl", "delete", "skill", branchSkillName, branchJevSkillName,
				"-n", namespace, "--ignore-not-found")
			_, _ = utils.Run(cmd)

			cmd = exec.Command("kubectl", "delete", "version",
				"-n", namespace,
				"-l", "opendepot.defdev.io/skill in ("+branchSkillName+","+branchJevSkillName+")",
				"--ignore-not-found")
			_, _ = utils.Run(cmd)

			cmd = exec.Command("kubectl", "delete", "secret", branchJevSecretName,
				"-n", namespace, "--ignore-not-found")
			_, _ = utils.Run(cmd)

			By("disabling the Depot, the Agent, branch sourcing, and Jev to restore baseline state")
			chartPath, err := utils.GetChartPath()
			if err != nil {
				return
			}

			cmd = exec.Command("helm", "upgrade", helmReleaseName, chartPath,
				"--reuse-values",
				"--namespace", namespace,
				"--set", "depot.enabled=false",
				"--set", "agent.enabled=false",
				"--set", "branchSourcing.enabled=false",
				"--set", "scanning.jev.enabled=false",
				"--wait",
				"--timeout", "3m",
			)
			_, _ = utils.Run(cmd)
		})

		It("should assign 1.0.0 with source Initial to a pending entry at the pinned SHA", func() {
			err := utils.RequireFixtureAtMain(utils.SkillFixturePath)
			Expect(err).NotTo(HaveOccurred(), "fixture removed upstream: %s", utils.SkillFixturePath)

			By("creating the Skill with a pending entry at the pinned SHA")
			applyBranchSkill(branchSkillName, false, branchEntry{
				commit:       utils.SkillFixtureSHA,
				discoveredAt: time.Now().Add(-time.Hour),
			})

			pinnedName := branchVersionName(branchSkillName, utils.SkillFixtureSHA)

			By("waiting for the agent controller to create the branch Version and the Version controller to assign it")
			Eventually(func(g Gomega) {
				phase, err := versionField(pinnedName, "{.status.branchPhase}")
				g.Expect(err).NotTo(HaveOccurred())

				syncStatus, _ := versionField(pinnedName, "{.status.syncStatus}")
				g.Expect(phase).To(Equal("Assigned"), "branchPhase held with syncStatus %q", syncStatus)
			}, 5*time.Minute, 10*time.Second).Should(Succeed())

			By("asserting spec.version, source, and sourceCommit")
			Expect(mustVersionField(pinnedName, "{.spec.version}")).To(Equal(branchPinnedVersion))
			Expect(mustVersionField(pinnedName, "{.status.bumpAssessment.source}")).To(Equal("Initial"))
			Expect(mustVersionField(pinnedName, "{.status.sourceCommit}")).To(Equal(utils.SkillFixtureSHA))

			By("waiting for the agent controller to copy the assignment to the Skill entry and status")
			Eventually(func(g Gomega) {
				entries, err := skillEntries(branchSkillName)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(entries).To(HaveLen(1))
				g.Expect(entries[0].State).To(Equal("Assigned"))
				g.Expect(entries[0].Version).To(Equal(branchPinnedVersion))

				refs, err := skillVersionRefs(branchSkillName)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(refs).To(HaveKey(branchPinnedVersion))
				g.Expect(refs[branchPinnedVersion].Name).To(Equal(pinnedName))
			}, 2*time.Minute, 5*time.Second).Should(Succeed())
		})

		It("should add a head entry that reaches a terminal phase", func() {
			err := utils.RequireFixtureAtMain(utils.SkillFixturePath)
			Expect(err).NotTo(HaveOccurred(), "fixture removed upstream: %s", utils.SkillFixturePath)

			By("creating a Depot that sources the same branch")
			applyBranchDepot(false)

			By("triggering a Depot reconcile with an annotation change")
			triggerDepot()

			By("waiting for the Depot to add an entry for the head commit")
			var headCommit string
			Eventually(func(g Gomega) {
				entries, err := skillEntries(branchSkillName)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(entries).To(HaveLen(2))

				headCommit = headCommitOf(entries, utils.SkillFixtureSHA)
				g.Expect(headCommit).To(MatchRegexp(`^[0-9a-f]{40}$`))
			}, 3*time.Minute, 5*time.Second).Should(Succeed())

			headName := branchVersionName(branchSkillName, headCommit)

			By("waiting for the head Version to reach a terminal phase")
			var headPhase string
			Eventually(func(g Gomega) {
				phase, err := versionField(headName, "{.status.branchPhase}")
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(phase).To(BeElementOf("Assigned", "Rejected"))
				headPhase = phase
			}, 5*time.Minute, 10*time.Second).Should(Succeed())

			switch headPhase {
			case "Assigned":
				By("asserting the default PATCH bump when Jev is not configured")
				Expect(mustVersionField(headName, "{.spec.version}")).To(Equal("1.0.1"))
				Expect(mustVersionField(headName, "{.status.bumpAssessment.source}")).To(Equal("Default"))
				Expect(mustVersionField(headName, "{.status.bumpAssessment.category}")).To(Equal("Patch"))
			case "Rejected":
				By("asserting a rejected Version is not assigned a semantic version")
				Expect(mustVersionField(headName, "{.spec.version}")).To(BeEmpty())
			}
		})

		It("should keep entries unique with increasing semantic versions", func() {
			err := utils.RequireFixtureAtMain(utils.SkillFixturePath)
			Expect(err).NotTo(HaveOccurred(), "fixture removed upstream: %s", utils.SkillFixturePath)

			By("triggering another Depot reconcile after the head entry is recorded")
			triggerDepot()

			By("asserting no two entries share a commit and assigned semantic versions strictly increase in discoveredAt order")
			Consistently(func(g Gomega) {
				entries, err := skillEntries(branchSkillName)
				g.Expect(err).NotTo(HaveOccurred())

				sort.SliceStable(entries, func(i, j int) bool {
					return entries[i].DiscoveredAt < entries[j].DiscoveredAt
				})

				seen := map[string]bool{}
				previous := 0
				for _, entry := range entries {
					g.Expect(seen).NotTo(HaveKey(entry.Commit), "duplicate entry for commit %s", entry.Commit)
					seen[entry.Commit] = true

					if entry.Version == "" {
						continue
					}

					current, err := parseBranchSemver(entry.Version)
					g.Expect(err).NotTo(HaveOccurred(), "expected a semantic version, got %q", entry.Version)
					g.Expect(current).To(BeNumerically(">", previous), "assigned versions must increase")
					previous = current
				}
			}, 30*time.Second, 5*time.Second).Should(Succeed())
		})

		It("should serve the assigned versions through the server", func() {
			err := utils.RequireFixtureAtMain(utils.SkillFixturePath)
			Expect(err).NotTo(HaveOccurred(), "fixture removed upstream: %s", utils.SkillFixturePath)

			pinnedName := branchVersionName(branchSkillName, utils.SkillFixtureSHA)

			By("reading the synced pinned Version from the Skill status written by the agent controller")
			refs, err := skillVersionRefs(branchSkillName)
			Expect(err).NotTo(HaveOccurred())
			Expect(refs).To(HaveKey(branchPinnedVersion))
			Expect(refs[branchPinnedVersion].Name).To(Equal(pinnedName))
			Expect(refs[branchPinnedVersion].Synced).To(BeTrue())

			By("starting a port-forward to the server for artifact verification")
			pfCtx, cancel := context.WithCancel(context.Background())
			branchPFCancel = cancel
			cmd := exec.CommandContext(pfCtx, "kubectl", "port-forward",
				"-n", namespace, "svc/server", branchServerPort+":80",
			)
			cmd.Stdout = GinkgoWriter
			cmd.Stderr = GinkgoWriter
			Expect(cmd.Start()).To(Succeed())

			By("asserting /versions lists 1.0.0")
			Eventually(func(g Gomega) {
				status, body, err := branchGet("/versions")
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(status).To(Equal(http.StatusOK))

				var listed branchVersionsResponse
				g.Expect(json.Unmarshal(body, &listed)).To(Succeed())

				versions := []string{}
				for _, v := range listed.Versions {
					versions = append(versions, v.Version)
				}
				g.Expect(versions).To(ContainElement(branchPinnedVersion))
			}, 2*time.Minute, 2*time.Second).Should(Succeed())

			By("asserting /1.0.0/download shasum matches the Version status checksum")
			var download branchDownloadResponse
			Eventually(func(g Gomega) {
				status, body, err := branchGet("/" + branchPinnedVersion + "/download")
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(status).To(Equal(http.StatusOK))
				g.Expect(json.Unmarshal(body, &download)).To(Succeed())
			}, 2*time.Minute, 2*time.Second).Should(Succeed())

			Expect(download.Version).To(Equal(branchPinnedVersion))
			checksum, err := base64.StdEncoding.DecodeString(mustVersionField(pinnedName, "{.status.checksum}"))
			Expect(err).NotTo(HaveOccurred())
			Expect(download.Shasum).To(Equal(hex.EncodeToString(checksum)))

			By("asserting SHA256SUMS is served in sha256sum format")
			Eventually(func(g Gomega) {
				status, body, err := branchGet("/" + branchPinnedVersion + "/SHA256SUMS")
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(status).To(Equal(http.StatusOK))
				g.Expect(string(body)).To(MatchRegexp(`(?m)^[0-9a-f]{64}\s+\S+`))
			}, 2*time.Minute, 2*time.Second).Should(Succeed())

			By("asserting SHA256SUMS.sig is served")
			Eventually(func(g Gomega) {
				status, body, err := branchGet("/" + branchPinnedVersion + "/SHA256SUMS.sig")
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(status).To(Equal(http.StatusOK))
				g.Expect(body).NotTo(BeEmpty())
			}, 2*time.Minute, 2*time.Second).Should(Succeed())
		})

		It("should keep the same checksum and version after forceSync on the same SHA", func() {
			err := utils.RequireFixtureAtMain(utils.SkillFixturePath)
			Expect(err).NotTo(HaveOccurred(), "fixture removed upstream: %s", utils.SkillFixturePath)

			pinnedName := branchVersionName(branchSkillName, utils.SkillFixtureSHA)
			checksum := mustVersionField(pinnedName, "{.status.checksum}")
			Expect(checksum).NotTo(BeEmpty())

			By("setting forceSync on the pinned Version")
			_, err = utils.Run(exec.Command("kubectl", "patch", "version", pinnedName,
				"-n", namespace,
				"--type=merge",
				"-p", `{"spec":{"forceSync":true}}`,
			))
			Expect(err).NotTo(HaveOccurred(), "Failed to set forceSync on the pinned Version")

			By("asserting the checksum and version are unchanged")
			Consistently(func(g Gomega) {
				current, err := versionField(pinnedName, "{.status.checksum}")
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(current).To(Equal(checksum))

				version, err := versionField(pinnedName, "{.spec.version}")
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(version).To(Equal(branchPinnedVersion))
			}, 45*time.Second, 5*time.Second).Should(Succeed())

			By("clearing forceSync")
			_, err = utils.Run(exec.Command("kubectl", "patch", "version", pinnedName,
				"-n", namespace,
				"--type=merge",
				"-p", `{"spec":{"forceSync":false}}`,
			))
			Expect(err).NotTo(HaveOccurred())
		})

		It("should classify the head bump with Jev when a Jev key is configured", func() {
			err := utils.RequireFixtureAtMain(utils.JevSkillFixturePath)
			Expect(err).NotTo(HaveOccurred(), "fixture removed upstream: %s", utils.JevSkillFixturePath)

			jevKey, err := utils.RequireJevAPIKey()
			Expect(err).NotTo(HaveOccurred(), "%s must be set to run the Jev variant", utils.JevAPIKeyEnvVar)

			By("storing the Jev key in a Secret read from stdin")
			cmd := exec.Command("kubectl", "create", "secret", "generic", branchJevSecretName,
				"--namespace", namespace,
				"--from-file", branchJevSecretKey+"=/dev/stdin",
				"--dry-run=client", "-o", "yaml",
			)
			cmd.Stdin = strings.NewReader(jevKey)
			manifest, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to render the Jev Secret")

			apply := exec.Command("kubectl", "apply", "-f", "-")
			apply.Stdin = strings.NewReader(manifest)
			_, err = utils.Run(apply)
			Expect(err).NotTo(HaveOccurred(), "Failed to apply the Jev Secret")

			By("enabling Jev classification")
			helmUpgrade("scanning.jev.enabled=true")

			By("creating the Jev Skill with a pending entry at the pinned SHA")
			applyBranchSkill(branchJevSkillName, true, branchEntry{
				commit:       utils.SkillFixtureSHA,
				discoveredAt: time.Now().Add(-time.Hour),
			})

			pinnedName := branchVersionName(branchJevSkillName, utils.SkillFixtureSHA)
			Eventually(func(g Gomega) {
				phase, err := versionField(pinnedName, "{.status.branchPhase}")
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(phase).To(Equal("Assigned"))
			}, 5*time.Minute, 10*time.Second).Should(Succeed())

			By("waiting for the agent controller to record the pinned Version in the Jev Skill status")
			Eventually(func(g Gomega) {
				refs, err := skillVersionRefs(branchJevSkillName)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(refs).To(HaveKey(branchPinnedVersion))
			}, 2*time.Minute, 5*time.Second).Should(Succeed())

			By("adding the Jev Skill to the Depot and triggering a reconcile")
			applyBranchDepot(true)
			triggerDepot()

			var jevHeadCommit string
			Eventually(func(g Gomega) {
				entries, err := skillEntries(branchJevSkillName)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(entries).To(HaveLen(2))

				jevHeadCommit = headCommitOf(entries, utils.SkillFixtureSHA)
				g.Expect(jevHeadCommit).To(MatchRegexp(`^[0-9a-f]{40}$`))
			}, 3*time.Minute, 5*time.Second).Should(Succeed())

			headName := branchVersionName(branchJevSkillName, jevHeadCommit)

			By("waiting for the head Version to reach Assigned, Rejected, or Held")
			var headPhase string
			Eventually(func(g Gomega) {
				phase, err := versionField(headName, "{.status.branchPhase}")
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(phase).To(BeElementOf("Assigned", "Rejected", "Held"))
				headPhase = phase
			}, 5*time.Minute, 10*time.Second).Should(Succeed())

			if headPhase != "Assigned" {
				return
			}

			By("asserting the source, version, and category agree")
			source := mustVersionField(headName, "{.status.bumpAssessment.source}")
			Expect(source).To(BeElementOf("Jev", "Withheld"))

			category := mustVersionField(headName, "{.status.bumpAssessment.category}")
			expectedVersion := map[string]string{"Patch": "1.0.1", "Minor": "1.1.0", "Major": "2.0.0"}
			Expect(expectedVersion).To(HaveKey(category))
			Expect(mustVersionField(headName, "{.spec.version}")).To(Equal(expectedVersion[category]))

			if source != "Jev" {
				return
			}

			By("asserting the Jev model and probabilities are recorded")
			Expect(mustVersionField(headName, "{.status.bumpAssessment.model}")).NotTo(BeEmpty())

			total := 0.0
			for _, field := range []string{"majorProbability", "minorProbability", "patchProbability"} {
				probability, err := strconv.ParseFloat(mustVersionField(headName, "{.status.bumpAssessment."+field+"}"), 64)
				Expect(err).NotTo(HaveOccurred())
				Expect(probability).To(BeNumerically(">=", 0))
				Expect(probability).To(BeNumerically("<=", 1))
				total += probability
			}
			Expect(total).To(BeNumerically("~", 1.0, 0.01))
		})
	})
})

// e2eContract mirrors the subset of the Assembly Line contract the e2e suite asserts on.
type e2eContract struct {
	SchemaVersion string `json:"schemaVersion"`
	Module        struct {
		Namespace string `json:"namespace"`
		Name      string `json:"name"`
		Provider  string `json:"provider"`
		Version   string `json:"version"`
	} `json:"module"`
	Variables []struct {
		Name     string          `json:"name"`
		Type     json.RawMessage `json:"type"`
		Required bool            `json:"required"`
	} `json:"variables"`
	Outputs []struct {
		Name       string          `json:"name"`
		Type       json.RawMessage `json:"type"`
		Confidence string          `json:"confidence"`
	} `json:"outputs"`
	Compatibility struct {
		Grade    string   `json:"grade"`
		Warnings []string `json:"warnings"`
	} `json:"compatibility"`
}

// decodeE2EContract base64-decodes and gunzips a contract ConfigMap payload.
func decodeE2EContract(encoded string) (*e2eContract, error) {
	compressed, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, err
	}

	gr, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return nil, err
	}
	defer gr.Close()

	raw, err := io.ReadAll(gr)
	if err != nil {
		return nil, err
	}

	var contract e2eContract
	if err := json.Unmarshal(raw, &contract); err != nil {
		return nil, err
	}

	return &contract, nil
}
