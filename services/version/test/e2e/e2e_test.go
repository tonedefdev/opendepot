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
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

		AfterAll(func() {
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
      useAuthenticatedClient: false
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

			// Use the base image ref — the Helm ternary appends "-scanning" automatically
			// when scanning.enabled=true, so version-controller:e2e-test becomes
			// version-controller:e2e-test-scanning.
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
      useAuthenticatedClient: false
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

			By("applying the null Provider CR with two versions")
			providerYAML := fmt.Sprintf(`apiVersion: opendepot.defdev.io/v1alpha1
kind: Provider
metadata:
  name: "%s"
  namespace: %s
spec:
  providerConfig:
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
      useAuthenticatedClient: false
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
})
