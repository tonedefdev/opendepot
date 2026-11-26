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
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	utils "github.com/tonedefdev/opendepot/pkg/testutils"
)

var (
	// projectImage is the iac controller image to deploy for e2e tests.
	// Override with the IMG environment variable.
	projectImage = func() string {
		if img := os.Getenv("IMG"); img != "" {
			return img
		}
		return "iac-controller:e2e-test"
	}()

	// versionImage is the version controller image to deploy for e2e tests.
	versionImage = "version-controller:e2e-test"

	// serverImage is the server image to deploy for e2e tests.
	serverImage = "server:e2e-test"

	// gpgHome is the temp directory used as GNUPGHOME for the test key pair.
	gpgHome string
)

// namespace where the project is deployed in.
const namespace = "opendepot-system"

const (
	// helmReleaseName is the existing Helm release that owns iac/version/server.
	helmReleaseName = "opendepot"
	// gpgSecretName is the k8s Secret created to hold the test GPG keys.
	gpgSecretName = "opendepot-provider-gpg-test"
)

// TestE2E runs the end-to-end test suite for the iac controller, which hosts the Module and Provider reconcilers.
func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	_, _ = fmt.Fprintf(GinkgoWriter, "Starting opendepot iac e2e test suite\n")
	RunSpecs(t, "e2e suite")
}

var _ = BeforeSuite(func() {
	err := utils.ConfigureKindCluster()
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to configure the Kind kubeconfig")

	repoRoot, err := utils.GetRepoRoot()
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to determine repo root")

	if os.Getenv("SKIP_IMAGE_BUILD") != "true" {
		iacHash, err := utils.ComputeBuildContextHash(repoRoot, []string{
			"services/iac",
			"api",
			"pkg",
			"go.work",
			"go.work.sum",
		})
		ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to compute iac controller build hash")

		if utils.NeedsRebuild(projectImage, iacHash) {
			By("building the iac controller image (context changed or image absent)")
			buildCmd := exec.Command("docker", "build",
				"-t", projectImage,
				"--label", "opendepot.build.hash="+iacHash,
				"-f", "services/iac/Dockerfile",
				".",
			)
			_, err = utils.RunAt(buildCmd, repoRoot)
			ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to build the iac controller image")
		} else {
			By("iac controller image up-to-date, skipping build")
		}

		versionHash, err := utils.ComputeBuildContextHash(repoRoot, []string{
			"services/version",
			"api",
			"pkg",
			"go.work",
			"go.work.sum",
		})
		ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to compute version controller build hash")

		if utils.NeedsRebuild(versionImage, versionHash) {
			By("building the version controller image (context changed or image absent)")
			versionBuildCmd := exec.Command("docker", "build",
				"--build-arg", "INCLUDE_TOFU=true",
				"-t", versionImage,
				"--label", "opendepot.build.hash="+versionHash,
				"--build-arg", "INCLUDE_TRIVY=true",
				"-f", "services/version/Dockerfile",
				".",
			)
			_, err = utils.RunAt(versionBuildCmd, repoRoot)
			ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to build the version controller image")
		} else {
			By("version controller image up-to-date, skipping build")
		}

		serverHash, err := utils.ComputeBuildContextHash(repoRoot, []string{
			"services/server",
			"api",
			"pkg",
			"go.work",
			"go.work.sum",
		})
		ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to compute server build hash")

		if utils.NeedsRebuild(serverImage, serverHash) {
			By("building the server image (context changed or image absent)")
			serverBuildCmd := exec.Command("docker", "build",
				"--build-arg", "INCLUDE_TOFU=true",
				"-t", serverImage,
				"--label", "opendepot.build.hash="+serverHash,
				"-f", "services/server/Dockerfile",
				".",
			)
			_, err = utils.RunAt(serverBuildCmd, repoRoot)
			ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to build the server image")
		} else {
			By("server image up-to-date, skipping build")
		}
	} else {
		By("SKIP_IMAGE_BUILD=true: skipping image builds, using pre-built images")
	}

	By("loading the iac controller image on Kind")
	err = utils.LoadImageToKindClusterWithName(projectImage)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to load the iac controller image into Kind")

	By("loading the version controller image on Kind")
	err = utils.LoadImageToKindClusterWithName(versionImage)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to load the version controller image into Kind")

	By("loading the server image on Kind")
	err = utils.LoadImageToKindClusterWithName(serverImage)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to load the server image into Kind")

	By("ensuring all chart CRDs are installed")
	allCRDsPath := filepath.Join(repoRoot, "chart", "opendepot", "crds")
	cmd := exec.Command("kubectl", "apply", "--server-side", "--force-conflicts", "-f", allCRDsPath)
	_, err = utils.Run(cmd)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to apply chart CRDs")

	By("generating test GPG key pair")
	gpgHome, err = os.MkdirTemp("", "opendepot-e2e-gpg-*")
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to create temp GPG home")

	keyID, asciiArmor, privateKeyBase64, err := utils.GenerateTestGPGKeyPair(gpgHome)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to generate test GPG key pair")

	By("ensuring namespace exists before creating secrets")
	cmd = exec.Command("kubectl", "create", "namespace", namespace)
	_, _ = utils.Run(cmd) // ignore error if namespace already exists

	By("creating GPG secret in cluster")
	cmd = exec.Command("kubectl", "create", "secret", "generic", gpgSecretName,
		"--namespace", namespace,
		fmt.Sprintf("--from-literal=OPENDEPOT_PROVIDER_GPG_KEY_ID=%s", keyID),
		fmt.Sprintf("--from-literal=OPENDEPOT_PROVIDER_GPG_ASCII_ARMOR=%s", asciiArmor),
		fmt.Sprintf("--from-literal=OPENDEPOT_PROVIDER_GPG_PRIVATE_KEY_BASE64=%s", privateKeyBase64),
	)
	_, err = utils.Run(cmd)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to create GPG secret")

	By("upgrading Helm release to configure the iac controller with local images and GPG signing")
	chartPath, err := utils.GetChartPath()
	ExpectWithOffset(1, err).NotTo(HaveOccurred())

	// Parse the repository and tag from each image reference (format: "repo:tag").
	iacRepo, iacTag := utils.SplitImageRef(projectImage)
	versionRepo, versionTag := utils.SplitImageRef(versionImage)
	serverRepo, serverTag := utils.SplitImageRef(serverImage)

	cmd = exec.Command("helm", "upgrade", helmReleaseName, chartPath,
		"--install",
		"--create-namespace",
		"--namespace", namespace,
		"--skip-crds",
		"--set", "monitoring.enabled=false",
		"--set", "monitoring.bundled.enabled=false",
		"--set", "depot.enabled=false",
		// The Module and Provider reconcilers both run in the single iac-controller deployment.
		"--set", "iac.enabled=true",
		"--set", fmt.Sprintf("iac.image.repository=%s", iacRepo),
		"--set", fmt.Sprintf("iac.image.tag=%s", iacTag),
		"--set", fmt.Sprintf("version.image.repository=%s", versionRepo),
		"--set", fmt.Sprintf("version.image.tag=%s", versionTag),
		"--set", fmt.Sprintf("server.gpg.secretName=%s", gpgSecretName),
		"--set", "server.anonymousAuth=true",
		"--set", fmt.Sprintf("server.image.repository=%s", serverRepo),
		"--set", fmt.Sprintf("server.image.tag=%s", serverTag),
		// Enable filesystem storage with a hostPath volume for Kind (no ReadWriteMany SC available).
		"--set", "storage.filesystem.enabled=true",
		"--set", "storage.filesystem.hostPath=/data/modules",
		"--set", "scanning.cache.accessMode=ReadWriteOnce",
		// Increase version controller memory limit to handle large provider binaries (AWS ~700MB).
		"--set", "version.resources.limits.memory=2Gi",
		"--wait",
		"--timeout", "3m",
	)
	cmd.Args = append(cmd.Args, utils.CoverageHelmArgs()...)
	_, err = utils.Run(cmd)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to upgrade Helm release")
})

var _ = AfterSuite(func() {
	if !utils.KindClusterConfigured() {
		return
	}

	ExpectWithOffset(1, utils.CollectCoverage(namespace)).To(Succeed())

	By("uninstalling Helm release to clean up iac e2e resources")
	cmd := exec.Command("helm", "uninstall", helmReleaseName,
		"--namespace", namespace,
		"--ignore-not-found",
	)
	_, _ = utils.Run(cmd)

	By("deleting GPG secret")
	cmd = exec.Command("kubectl", "delete", "secret", gpgSecretName,
		"--namespace", namespace, "--ignore-not-found",
	)
	_, _ = utils.Run(cmd)

	By("cleaning up temp GPG home")
	if gpgHome != "" {
		os.RemoveAll(gpgHome)
	}
})
