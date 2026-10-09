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
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	utils "github.com/tonedefdev/opendepot/pkg/testutils"
)

var (
	// agentImage is the agent controller image to deploy for e2e tests.
	agentImage = "agent-controller:e2e-test"

	// versionImage is the version controller image to deploy for e2e tests.
	versionImage = "version-controller:e2e-test"

	// serverImage is the server image to deploy for e2e tests.
	serverImage = "server:e2e-test"

	// jevAPIKey is the TypeSafe Jev API key. When empty, Jev assessment is not exercised.
	jevAPIKey = os.Getenv("OPENDEPOT_JEV_API_KEY")
)

const (
	// namespace where the project is deployed in.
	namespace = "opendepot-system"

	// helmReleaseName is the Helm release that owns the agent, version, and server components.
	helmReleaseName = "opendepot"
	// trivyDBSeedJobName is the one-off Job that populates the Trivy vulnerability DB cache.
	trivyDBSeedJobName = "trivy-db-seed"

	// jevSecretName is the Secret that holds the Jev token referenced by Skill and Agent resources.
	jevSecretName = "opendepot-jev"

	// jevSecretKey is the Secret key that holds the Jev token.
	jevSecretKey = "jevToken"

	// signingSecretName is the Secret that holds the ephemeral GPG key used to sign Skill and Agent versions.
	signingSecretName = "opendepot-gpg"

	// signingSecretKey is the Secret key that holds the base64-encoded armored GPG private key.
	signingSecretKey = "OPENDEPOT_PROVIDER_GPG_PRIVATE_KEY_BASE64"
)

// TestE2E runs the end-to-end test suite for the agent controller.
func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	_, _ = fmt.Fprintf(GinkgoWriter, "Starting opendepot agent e2e test suite\n")
	RunSpecs(t, "e2e suite")
}

var _ = BeforeSuite(func() {
	err := utils.ConfigureKindCluster()
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to configure the Kind kubeconfig")

	repoRoot, err := utils.GetRepoRoot()
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to determine repo root")

	if os.Getenv("SKIP_IMAGE_BUILD") != "true" {
		agentHash, err := utils.ComputeBuildContextHash(repoRoot, []string{
			"services/agent",
			"api",
			"pkg",
			"go.work",
			"go.work.sum",
		})
		ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to compute agent controller build hash")

		if utils.NeedsRebuild(agentImage, agentHash) {
			By("building the agent controller image (context changed or image absent)")
			buildCmd := exec.Command("docker", "build",
				"-t", agentImage,
				"--label", "opendepot.build.hash="+agentHash,
				"-f", "services/agent/Dockerfile",
				".",
			)
			_, err = utils.RunAt(buildCmd, repoRoot)
			ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to build the agent controller image")
		} else {
			By("agent controller image up-to-date, skipping build")
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
				"--build-arg", "INCLUDE_TRIVY=true",
				"-t", versionImage,
				"--label", "opendepot.build.hash="+versionHash,
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

	By("loading the agent controller image on Kind")
	err = utils.LoadImageToKindClusterWithName(agentImage)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to load the agent controller image into Kind")

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

	By("ensuring namespace exists before installing chart")
	cmd = exec.Command("kubectl", "create", "namespace", namespace)
	_, _ = utils.Run(cmd) // ignore error if namespace already exists

	By("generating an ephemeral GPG signing key and storing it in a Secret read from stdin")
	signingKey, err := generateSigningKeyBase64()
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to generate the GPG signing key")

	cmd = exec.Command("kubectl", "create", "secret", "generic", signingSecretName,
		"--namespace", namespace,
		"--from-file", signingSecretKey+"=/dev/stdin",
		"--dry-run=client", "-o", "yaml",
	)
	cmd.Stdin = strings.NewReader(signingKey)
	signingManifest, err := utils.Run(cmd)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to render the signing key Secret")

	cmd = exec.Command("kubectl", "apply", "-f", "-")
	cmd.Stdin = strings.NewReader(signingManifest)
	_, err = utils.Run(cmd)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to apply the signing key Secret")

	if jevAPIKey != "" {
		By("storing the Jev token in a Secret read from stdin")
		cmd = exec.Command("kubectl", "create", "secret", "generic", jevSecretName,
			"--namespace", namespace,
			"--from-file", jevSecretKey+"=/dev/stdin",
			"--dry-run=client", "-o", "yaml",
		)
		cmd.Stdin = strings.NewReader(jevAPIKey)
		manifest, err := utils.Run(cmd)
		ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to render the Jev token Secret")

		cmd = exec.Command("kubectl", "apply", "-f", "-")
		cmd.Stdin = strings.NewReader(manifest)
		_, err = utils.Run(cmd)
		ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to apply the Jev token Secret")
	}

	By("upgrading Helm release to configure the agent controller with local images")
	chartPath, err := utils.GetChartPath()
	ExpectWithOffset(1, err).NotTo(HaveOccurred())

	agentRepo, agentTag := utils.SplitImageRef(agentImage)
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
		"--set", "module.enabled=false",
		"--set", "provider.enabled=false",
		"--set", "scanning.providerScanning=true",
		"--set", "agent.enabled=true",
		"--set", fmt.Sprintf("agent.image.repository=%s", agentRepo),
		"--set", fmt.Sprintf("agent.image.tag=%s", agentTag),
		"--set", "scanning.agentScanning=true",
		"--set", "version.gpg.secretName="+signingSecretName,
		"--set", "server.gpg.secretName="+signingSecretName,
		"--set", fmt.Sprintf("scanning.jev.enabled=%t", jevAPIKey != ""),
		"--set", fmt.Sprintf("version.image.repository=%s", versionRepo),
		"--set", fmt.Sprintf("version.image.tag=%s", versionTag),
		"--set", "server.anonymousAuth=true",
		"--set", fmt.Sprintf("server.image.repository=%s", serverRepo),
		"--set", fmt.Sprintf("server.image.tag=%s", serverTag),
		"--set", "storage.filesystem.enabled=true",
		"--set", "storage.filesystem.hostPath=/data/modules",
		"--set", "scanning.cache.accessMode=ReadWriteOnce",
		"--wait",
		"--timeout", "3m",
	)
	_, err = utils.Run(cmd)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to upgrade Helm release")

	By("seeding the Trivy vulnerability DB that agent scans read in offline mode")
	cmd = exec.Command("kubectl", "delete", "job", trivyDBSeedJobName,
		"--namespace", namespace,
		"--ignore-not-found",
	)
	_, _ = utils.Run(cmd)

	cmd = exec.Command("kubectl", "create", "job", trivyDBSeedJobName,
		"--from=cronjob/trivy-db-updater",
		"--namespace", namespace,
	)
	_, err = utils.Run(cmd)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to create Trivy DB seed job")

	cmd = exec.Command("kubectl", "wait", "--for=condition=complete",
		"job/"+trivyDBSeedJobName,
		"--namespace", namespace,
		"--timeout=20m",
	)
	_, err = utils.Run(cmd)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Trivy DB seed job did not complete")
})

var _ = AfterSuite(func() {
	if !utils.KindClusterConfigured() {
		return
	}

	By("uninstalling Helm release to clean up agent e2e resources")
	cmd := exec.Command("helm", "uninstall", helmReleaseName,
		"--namespace", namespace,
		"--ignore-not-found",
	)
	_, _ = utils.Run(cmd)
})

// generateSigningKeyBase64 creates an unprotected GPG key in a private keyring and returns the
// base64 encoding of its armored private key, the format the version controller expects.
func generateSigningKeyBase64() (string, error) {
	gnupgHome, err := os.MkdirTemp("", "opendepot-gpg-")
	if err != nil {
		return "", fmt.Errorf("create GNUPGHOME: %w", err)
	}
	defer func() { _ = os.RemoveAll(gnupgHome) }()

	const uid = "OpenDepot E2E <e2e@opendepot.invalid>"
	gen := exec.Command("gpg", "--batch", "--homedir", gnupgHome, "--pinentry-mode", "loopback",
		"--passphrase", "", "--quick-gen-key", uid, "rsa3072", "default", "never")
	if out, err := gen.CombinedOutput(); err != nil {
		return "", fmt.Errorf("generate gpg key: %w: %s", err, out)
	}

	export := exec.Command("gpg", "--batch", "--homedir", gnupgHome, "--pinentry-mode", "loopback",
		"--passphrase", "", "--armor", "--export-secret-keys", uid)
	armor, err := export.Output()
	if err != nil {
		return "", fmt.Errorf("export gpg private key: %w", err)
	}

	return base64.StdEncoding.EncodeToString(armor), nil
}
