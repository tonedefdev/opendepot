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
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	utils "github.com/tonedefdev/opendepot/pkg/testutils"
)

// legacyChartTag is the last release tag that ships the module-controller and provider-controller Deployments.
const legacyChartTag = "v0.12.0"

var _ = Describe("Upgrade from the pre-iac chart", Ordered, func() {
	const (
		upgradeNamespace        = "opendepot-system"
		upgradeModuleName       = "upgrade-terraform-aws-key-pair"
		upgradeModuleVersion    = "2.0.0"
		upgradeProviderName     = "upgrade-null"
		upgradeProviderUpstream = "null"
		upgradeProviderVersion  = "3.2.3"
		upgradeStoragePath      = "/data/modules"
	)

	var (
		legacyTempDir  string
		legacyChartDir string
	)

	// installRelease runs helm upgrade for the given chart. It sets only module.* and provider.* keys for the
	// iac image, so the same values work for the pre-iac chart and for the local chart.
	installRelease := func(chartPath string) {
		iacRepo, iacTag := utils.SplitImageRef(projectImage)
		versionRepo, versionTag := utils.SplitImageRef(versionImage)
		serverRepo, serverTag := utils.SplitImageRef(serverImage)

		cmd := exec.Command("helm", "upgrade", helmReleaseName, chartPath,
			"--install",
			"--namespace", upgradeNamespace,
			"--skip-crds",
			"--set", "monitoring.enabled=false",
			"--set", "monitoring.bundled.enabled=false",
			"--set", "depot.enabled=false",
			"--set", fmt.Sprintf("module.image.repository=%s", iacRepo),
			"--set", fmt.Sprintf("module.image.tag=%s", iacTag),
			"--set", fmt.Sprintf("provider.image.repository=%s", iacRepo),
			"--set", fmt.Sprintf("provider.image.tag=%s", iacTag),
			"--set", fmt.Sprintf("version.image.repository=%s", versionRepo),
			"--set", fmt.Sprintf("version.image.tag=%s", versionTag),
			"--set", fmt.Sprintf("server.gpg.secretName=%s", gpgSecretName),
			"--set", "server.anonymousAuth=true",
			"--set", fmt.Sprintf("server.image.repository=%s", serverRepo),
			"--set", fmt.Sprintf("server.image.tag=%s", serverTag),
			"--set", "storage.filesystem.enabled=true",
			"--set", "storage.filesystem.hostPath=/data/modules",
			"--set", "scanning.cache.accessMode=ReadWriteOnce",
			"--set", "version.resources.limits.memory=2Gi",
			"--wait",
			"--timeout", "3m",
		)
		_, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to run helm upgrade with chart "+chartPath)
	}

	BeforeAll(func() {
		repoRoot, err := utils.GetRepoRoot()
		Expect(err).NotTo(HaveOccurred(), "Failed to determine repo root")

		By("making sure the " + legacyChartTag + " tag is available")
		_, err = utils.RunAt(exec.Command("git", "rev-parse", "--verify", "--quiet", legacyChartTag+"^{commit}"), repoRoot)
		if err != nil {
			_, err = utils.RunAt(exec.Command("git", "fetch", "--depth=1", "origin", "tag", legacyChartTag), repoRoot)
			Expect(err).NotTo(HaveOccurred(), "Failed to fetch tag "+legacyChartTag)
		}

		By("extracting the pre-iac chart from " + legacyChartTag)
		legacyTempDir, err = os.MkdirTemp("", "opendepot-legacy-chart-*")
		Expect(err).NotTo(HaveOccurred(), "Failed to create temp chart directory")

		archivePath := filepath.Join(legacyTempDir, "chart.tar")
		_, err = utils.RunAt(exec.Command("git", "archive", "--format=tar", "--output", archivePath,
			legacyChartTag, "chart/opendepot"), repoRoot)
		Expect(err).NotTo(HaveOccurred(), "Failed to archive the pre-iac chart")

		_, err = utils.Run(exec.Command("tar", "-xf", archivePath, "-C", legacyTempDir))
		Expect(err).NotTo(HaveOccurred(), "Failed to extract the pre-iac chart")
		legacyChartDir = filepath.Join(legacyTempDir, "chart", "opendepot")

		By("installing the pre-iac chart with module and provider controllers")
		installRelease(legacyChartDir)

		By("applying the test Module CR")
		moduleYAML := fmt.Sprintf(`
apiVersion: opendepot.defdev.io/v1alpha1
kind: Module
metadata:
  name: %s
  namespace: %s
spec:
  moduleConfig:
    fileFormat: zip
    githubClientConfig:
      useAuthenticatedClient: false
    provider: aws
    repoOwner: terraform-aws-modules
    repoUrl: https://github.com/terraform-aws-modules/terraform-aws-key-pair
    storageConfig:
      fileSystem:
        directoryPath: %s
  versions:
    - version: "%s"
`, upgradeModuleName, upgradeNamespace, upgradeStoragePath, upgradeModuleVersion)

		moduleFile := filepath.Join(legacyTempDir, "upgrade-module.yaml")
		Expect(os.WriteFile(moduleFile, []byte(moduleYAML), 0600)).To(Succeed())
		_, err = utils.Run(exec.Command("kubectl", "apply", "-f", moduleFile))
		Expect(err).NotTo(HaveOccurred(), "Failed to apply test Module CR")

		By("applying the test Provider CR")
		operatingSystems := "      - linux"
		if runtime.GOOS != "linux" {
			operatingSystems += "\n      - " + runtime.GOOS
		}
		architectures := "      - amd64"
		if runtime.GOARCH != "amd64" {
			architectures += "\n      - " + runtime.GOARCH
		}

		providerYAML := fmt.Sprintf(`
apiVersion: opendepot.defdev.io/v1alpha1
kind: Provider
metadata:
  name: "%s"
  namespace: %s
spec:
  providerConfig:
    name: "%s"
    operatingSystems:
%s
    architectures:
%s
    storageConfig:
      fileSystem:
        directoryPath: %s
  versions:
    - version: "%s"
`, upgradeProviderName, upgradeNamespace, upgradeProviderUpstream, operatingSystems, architectures,
			upgradeStoragePath, upgradeProviderVersion)

		providerFile := filepath.Join(legacyTempDir, "upgrade-provider.yaml")
		Expect(os.WriteFile(providerFile, []byte(providerYAML), 0600)).To(Succeed())
		_, err = utils.Run(exec.Command("kubectl", "apply", "-f", providerFile))
		Expect(err).NotTo(HaveOccurred(), "Failed to apply test Provider CR")
	})

	AfterAll(func() {
		if !utils.KindClusterConfigured() {
			return
		}

		By("deleting the upgrade test Module and Provider CRs")
		_, _ = utils.Run(exec.Command("kubectl", "delete", "module", upgradeModuleName,
			"-n", upgradeNamespace, "--ignore-not-found"))
		_, _ = utils.Run(exec.Command("kubectl", "delete", "provider", upgradeProviderName,
			"-n", upgradeNamespace, "--ignore-not-found"))

		if legacyTempDir != "" {
			_ = os.RemoveAll(legacyTempDir)
		}
	})

	It("should sync the Module and Provider on the pre-iac chart", func() {
		By("waiting for the Module to report synced=true")
		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "get", "module", upgradeModuleName,
				"-n", upgradeNamespace,
				"-o", "jsonpath={.status.synced}",
			)
			output, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(output).To(Equal("true"))
		}, 5*time.Minute, 5*time.Second).Should(Succeed())

		By("waiting for the Provider to report synced=true")
		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "get", "provider", upgradeProviderName,
				"-n", upgradeNamespace,
				"-o", "jsonpath={.status.synced}",
			)
			output, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(output).To(Equal("true"))
		}, 5*time.Minute, 5*time.Second).Should(Succeed())
	})

	It("should upgrade to the local chart with only legacy module and provider values", func() {
		By("upgrading the release to the local chart")
		chartPath, err := utils.GetChartPath()
		Expect(err).NotTo(HaveOccurred(), "Failed to resolve the local chart path")

		installRelease(chartPath)
	})

	It("should roll out the iac-controller Deployment", func() {
		By("waiting for the iac-controller Deployment to be ready")
		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "get", "deployment", "iac-controller",
				"-n", upgradeNamespace,
				"-o", "jsonpath={.status.readyReplicas}",
			)
			output, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(output).To(Equal("1"))
		}, 3*time.Minute, 5*time.Second).Should(Succeed())
	})

	It("should remove the module-controller and provider-controller Deployments", func() {
		for _, name := range []string{"module-controller", "provider-controller"} {
			By("waiting for the " + name + " Deployment to be deleted")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "deployment", name,
					"-n", upgradeNamespace,
					"--ignore-not-found",
					"-o", "name",
				)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(BeEmpty(), "%s still exists", name)
			}, 3*time.Minute, 5*time.Second).Should(Succeed())
		}
	})

	It("should keep the Module and Provider synced after the upgrade", func() {
		By("waiting for the Module to remain synced under the iac-controller")
		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "get", "module", upgradeModuleName,
				"-n", upgradeNamespace,
				"-o", "jsonpath={.status.synced}",
			)
			output, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(output).To(Equal("true"))
		}, 3*time.Minute, 5*time.Second).Should(Succeed())

		By("waiting for the Provider to remain synced under the iac-controller")
		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "get", "provider", upgradeProviderName,
				"-n", upgradeNamespace,
				"-o", "jsonpath={.status.synced}",
			)
			output, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(output).To(Equal("true"))
		}, 3*time.Minute, 5*time.Second).Should(Succeed())
	})
})
