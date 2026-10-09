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
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	utils "github.com/tonedefdev/opendepot/pkg/testutils"
)

// agentStoragePath is the filesystem directory shared with the version controller, matching the
// storage.filesystem mount that the e2e suite installs.
const agentStoragePath = "/data/modules"

// dumpSyncDiagnostics prints the Version resources and the controller logs that explain why a
// Version did not sync. It runs after a failed spec, before the suite tears down the cluster.
func dumpSyncDiagnostics() {
	for _, args := range [][]string{
		{"get", "versions", "-n", namespace, "-o", "wide"},
		{"get", "versions", "-n", namespace, "-o", "yaml"},
		{"logs", "-n", namespace, "deploy/version-controller", "-c", "version-controller", "--tail=200"},
		{"logs", "-n", namespace, "deploy/agent-controller", "--tail=200"},
	} {
		out, _ := utils.Run(exec.Command("kubectl", args...))
		fmt.Fprintf(GinkgoWriter, "--- kubectl %s ---\n%s\n", strings.Join(args, " "), out)
	}
}

var _ = Describe("Skill", Ordered, func() {
	const (
		serverPortForwardPort = "18081"
		skillName             = "gh-actions-debug"
		skillVersion          = "0.9.0"
		skillVersionCRName    = "skill-gh-actions-debug-0-9-0"
		skillPath             = ".github/skills/gh-actions-debug"
		skillConstraint       = "~> 0.9.0"
		agentsBase            = "http://127.0.0.1:" + serverPortForwardPort + "/opendepot/agents/v1/" + namespace + "/skills/" + skillName
	)

	var pfCancel context.CancelFunc

	BeforeAll(func() {
		By("applying the Skill CR sourced from the public opendepot repository")
		jevBlock := ""
		if jevAPIKey != "" {
			jevBlock = fmt.Sprintf(`
    jevSecretRef:
      name: %s
      key: %s
    jevPolicy: {}`, jevSecretName, jevSecretKey)
		}

		skillYAML := fmt.Sprintf(`
apiVersion: opendepot.defdev.io/v1alpha1
kind: Skill
metadata:
  name: %s
  namespace: %s
spec:
  agentSourceConfig:
    name: %s
    repoOwner: tonedefdev
    repoUrl: https://github.com/tonedefdev/opendepot
    path: %s
    versionConstraints: "%s"
    storageConfig:
      fileSystem:
        directoryPath: %s%s
  versions:
    - version: "%s"
`, skillName, namespace, skillName, skillPath, skillConstraint, agentStoragePath, jevBlock, skillVersion)

		skillFile := GinkgoT().TempDir() + "/skill.yaml"
		Expect(os.WriteFile(skillFile, []byte(skillYAML), 0o600)).To(Succeed())

		cmd := exec.Command("kubectl", "apply", "-f", skillFile)
		_, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to apply the Skill CR")

		By("starting port-forward to the opendepot server")
		pfCtx, cancel := context.WithCancel(context.Background())
		pfCancel = cancel
		pfCmd := exec.CommandContext(pfCtx, "kubectl", "port-forward",
			"svc/server",
			fmt.Sprintf("%s:80", serverPortForwardPort),
			"-n", namespace,
		)
		Expect(pfCmd.Start()).To(Succeed(), "Failed to start port-forward")

		Eventually(func() error {
			resp, err := http.Get(agentsBase + "/versions")
			if err != nil {
				return err
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("unexpected status %d", resp.StatusCode)
			}
			return nil
		}, 60*time.Second, 2*time.Second).Should(Succeed(), "server did not become reachable through port-forward")
	})

	AfterEach(func() {
		if CurrentSpecReport().Failed() {
			dumpSyncDiagnostics()
		}
	})

	AfterAll(func() {
		if pfCancel != nil {
			pfCancel()
		}

		By("deleting the Skill CR")
		cmd := exec.Command("kubectl", "delete", "skill", skillName,
			"-n", namespace,
			"--ignore-not-found",
		)
		_, _ = utils.Run(cmd)
	})

	It("syncs the Skill from GitHub", func() {
		Eventually(func() string {
			cmd := exec.Command("kubectl", "get", "skill", skillName,
				"-n", namespace,
				"-o", "jsonpath={.status.synced}",
			)
			out, _ := utils.Run(cmd)
			return strings.TrimSpace(out)
		}, 5*time.Minute, 5*time.Second).Should(Equal("true"), "Skill did not sync")
	})

	It("creates a Version with a Trivy source scan", func() {
		Eventually(func() string {
			cmd := exec.Command("kubectl", "get", "version", skillVersionCRName,
				"-n", namespace,
				"-o", "jsonpath={.status.synced} {.status.syncStatus}",
			)
			out, _ := utils.Run(cmd)
			return strings.TrimSpace(out)
		}, 5*time.Minute, 5*time.Second).Should(HavePrefix("true"), "Skill Version did not sync")

		cmd := exec.Command("kubectl", "get", "version", skillVersionCRName,
			"-n", namespace,
			"-o", "jsonpath={.status.sourceScan.scannedAt}",
		)
		out, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred())
		Expect(strings.TrimSpace(out)).NotTo(BeEmpty(), "Version has no sourceScan.scannedAt")
	})

	It("records a Jev assessment when a Jev token is configured", func() {
		if jevAPIKey == "" {
			Fail("OPENDEPOT_JEV_API_KEY must be set; the Jev assessment e2e test is required")
		}

		Eventually(func() string {
			cmd := exec.Command("kubectl", "get", "version", skillVersionCRName,
				"-n", namespace,
				"-o", "jsonpath={.status.jevAssessment.evaluatedAt}",
			)
			out, _ := utils.Run(cmd)
			return strings.TrimSpace(out)
		}, 5*time.Minute, 5*time.Second).ShouldNot(BeEmpty(), "Version has no jevAssessment.evaluatedAt")

		cmd := exec.Command("kubectl", "get", "version", skillVersionCRName,
			"-n", namespace,
			"-o", "jsonpath={.status.jevAssessment.model}",
		)
		out, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred())
		Expect(strings.TrimSpace(out)).NotTo(BeEmpty(), "jevAssessment has no model")

		riskLevelCmd := exec.Command("kubectl", "get", "version", skillVersionCRName,
			"-n", namespace,
			"-o", "jsonpath={.status.jevAssessment.riskLevel}",
		)
		riskLevelOut, riskLevelErr := utils.Run(riskLevelCmd)
		Expect(riskLevelErr).NotTo(HaveOccurred())
		riskLevel := strings.TrimSpace(riskLevelOut)
		Expect(riskLevel).NotTo(BeEmpty(), "jevAssessment has no riskLevel")
		jevErrorCmd := exec.Command("kubectl", "get", "version", skillVersionCRName,
			"-n", namespace,
			"-o", "jsonpath={.status.jevAssessment.error}",
		)
		jevErrorOut, jevErrorErr := utils.Run(jevErrorCmd)
		Expect(jevErrorErr).NotTo(HaveOccurred())
		jevError := strings.TrimSpace(jevErrorOut)
		Expect(jevError).To(BeEmpty(), "jevAssessment recorded an error")
	})

	It("serves the version list over agents.v1", func() {
		resp, err := http.Get(agentsBase + "/versions")
		Expect(err).NotTo(HaveOccurred())
		defer func() { _ = resp.Body.Close() }()
		Expect(resp.StatusCode).To(Equal(http.StatusOK))

		body, err := io.ReadAll(resp.Body)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(body)).To(ContainSubstring(skillVersion))
	})

	It("serves the download metadata, checksums, signature, and archive", func() {
		resp, err := http.Get(agentsBase + "/" + skillVersion + "/download")
		Expect(err).NotTo(HaveOccurred())
		_ = resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusOK), "download metadata request failed")

		resp, err = http.Get(agentsBase + "/" + skillVersion + "/SHA256SUMS")
		Expect(err).NotTo(HaveOccurred())
		sums, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusOK), "SHA256SUMS request failed")
		Expect(string(sums)).To(MatchRegexp(`(?m)^[0-9a-f]{64}\s+\S+`), "SHA256SUMS is not in sha256sum format")

		resp, err = http.Get(agentsBase + "/" + skillVersion + "/SHA256SUMS.sig")
		Expect(err).NotTo(HaveOccurred())
		sig, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusOK), "SHA256SUMS.sig request failed")
		Expect(sig).NotTo(BeEmpty(), "SHA256SUMS.sig is empty")

		resp, err = http.Get(agentsBase + "/" + skillVersion + "/archive")
		Expect(err).NotTo(HaveOccurred())
		archive, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusOK), "archive request failed")
		Expect(archive).NotTo(BeEmpty(), "archive is empty")
	})
})

var _ = Describe("Agent", Ordered, func() {
	const (
		serverPortForwardPort = "18082"
		agentName             = "code-review"
		agentVersion          = "0.9.0"
		agentVersionCRName    = "agent-code-review-0-9-0"
		agentPath             = ".github/agents"
		agentConstraint       = "~> 0.9.0"
		agentsBase            = "http://127.0.0.1:" + serverPortForwardPort + "/opendepot/agents/v1/" + namespace + "/agents/" + agentName
	)

	var pfCancel context.CancelFunc

	BeforeAll(func() {
		By("applying the Agent CR sourced from the public opendepot repository")
		jevBlock := ""
		if jevAPIKey != "" {
			jevBlock = fmt.Sprintf(`
    jevSecretRef:
      name: %s
      key: %s
    jevPolicy: {}`, jevSecretName, jevSecretKey)
		}

		agentYAML := fmt.Sprintf(`
apiVersion: opendepot.defdev.io/v1alpha1
kind: Agent
metadata:
  name: %s
  namespace: %s
spec:
  agentSourceConfig:
    name: %s
    repoOwner: tonedefdev
    repoUrl: https://github.com/tonedefdev/opendepot
    path: %s
    versionConstraints: "%s"
    storageConfig:
      fileSystem:
        directoryPath: %s%s
  versions:
    - version: "%s"
`, agentName, namespace, agentName, agentPath, agentConstraint, agentStoragePath, jevBlock, agentVersion)

		agentFile := GinkgoT().TempDir() + "/agent.yaml"
		Expect(os.WriteFile(agentFile, []byte(agentYAML), 0o600)).To(Succeed())

		cmd := exec.Command("kubectl", "apply", "-f", agentFile)
		_, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to apply the Agent CR")

		By("starting port-forward to the opendepot server")
		pfCtx, cancel := context.WithCancel(context.Background())
		pfCancel = cancel
		pfCmd := exec.CommandContext(pfCtx, "kubectl", "port-forward",
			"svc/server",
			fmt.Sprintf("%s:80", serverPortForwardPort),
			"-n", namespace,
		)
		Expect(pfCmd.Start()).To(Succeed(), "Failed to start port-forward")

		Eventually(func() error {
			resp, err := http.Get(agentsBase + "/versions")
			if err != nil {
				return err
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("unexpected status %d", resp.StatusCode)
			}
			return nil
		}, 60*time.Second, 2*time.Second).Should(Succeed(), "server did not become reachable through port-forward")
	})

	AfterEach(func() {
		if CurrentSpecReport().Failed() {
			dumpSyncDiagnostics()
		}
	})

	AfterAll(func() {
		if pfCancel != nil {
			pfCancel()
		}

		By("deleting the Agent CR")
		cmd := exec.Command("kubectl", "delete", "agent", agentName,
			"-n", namespace,
			"--ignore-not-found",
		)
		_, _ = utils.Run(cmd)
	})

	It("syncs the Agent from GitHub", func() {
		Eventually(func() string {
			cmd := exec.Command("kubectl", "get", "agent", agentName,
				"-n", namespace,
				"-o", "jsonpath={.status.synced}",
			)
			out, _ := utils.Run(cmd)
			return strings.TrimSpace(out)
		}, 5*time.Minute, 5*time.Second).Should(Equal("true"), "Agent did not sync")
	})

	It("creates a Version with a Trivy source scan", func() {
		Eventually(func() string {
			cmd := exec.Command("kubectl", "get", "version", agentVersionCRName,
				"-n", namespace,
				"-o", "jsonpath={.status.synced} {.status.syncStatus}",
			)
			out, _ := utils.Run(cmd)
			return strings.TrimSpace(out)
		}, 5*time.Minute, 5*time.Second).Should(HavePrefix("true"), "Agent Version did not sync")

		cmd := exec.Command("kubectl", "get", "version", agentVersionCRName,
			"-n", namespace,
			"-o", "jsonpath={.status.sourceScan.scannedAt}",
		)
		out, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred())
		Expect(strings.TrimSpace(out)).NotTo(BeEmpty(), "Version has no sourceScan.scannedAt")
	})

	It("records a Jev assessment when a Jev token is configured", func() {
		if jevAPIKey == "" {
			Fail("OPENDEPOT_JEV_API_KEY must be set; the Jev assessment e2e test is required")
		}

		Eventually(func() string {
			cmd := exec.Command("kubectl", "get", "version", agentVersionCRName,
				"-n", namespace,
				"-o", "jsonpath={.status.jevAssessment.evaluatedAt}",
			)
			out, _ := utils.Run(cmd)
			return strings.TrimSpace(out)
		}, 5*time.Minute, 5*time.Second).ShouldNot(BeEmpty(), "Version has no jevAssessment.evaluatedAt")

		cmd := exec.Command("kubectl", "get", "version", agentVersionCRName,
			"-n", namespace,
			"-o", "jsonpath={.status.jevAssessment.model}",
		)
		out, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred())
		Expect(strings.TrimSpace(out)).NotTo(BeEmpty(), "jevAssessment has no model")

		riskLevelCmd := exec.Command("kubectl", "get", "version", agentVersionCRName,
			"-n", namespace,
			"-o", "jsonpath={.status.jevAssessment.riskLevel}",
		)
		riskLevelOut, riskLevelErr := utils.Run(riskLevelCmd)
		Expect(riskLevelErr).NotTo(HaveOccurred())
		riskLevel := strings.TrimSpace(riskLevelOut)
		Expect(riskLevel).NotTo(BeEmpty(), "jevAssessment has no riskLevel")
		jevErrorCmd := exec.Command("kubectl", "get", "version", agentVersionCRName,
			"-n", namespace,
			"-o", "jsonpath={.status.jevAssessment.error}",
		)
		jevErrorOut, jevErrorErr := utils.Run(jevErrorCmd)
		Expect(jevErrorErr).NotTo(HaveOccurred())
		jevError := strings.TrimSpace(jevErrorOut)
		Expect(jevError).To(BeEmpty(), "jevAssessment recorded an error")
	})

	It("serves the version list over agents.v1", func() {
		resp, err := http.Get(agentsBase + "/versions")
		Expect(err).NotTo(HaveOccurred())
		defer func() { _ = resp.Body.Close() }()
		Expect(resp.StatusCode).To(Equal(http.StatusOK))

		body, err := io.ReadAll(resp.Body)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(body)).To(ContainSubstring(agentVersion))
	})

	It("serves the download metadata, checksums, signature, and archive", func() {
		resp, err := http.Get(agentsBase + "/" + agentVersion + "/download")
		Expect(err).NotTo(HaveOccurred())
		_ = resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusOK), "download metadata request failed")

		resp, err = http.Get(agentsBase + "/" + agentVersion + "/SHA256SUMS")
		Expect(err).NotTo(HaveOccurred())
		sums, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusOK), "SHA256SUMS request failed")
		Expect(string(sums)).To(MatchRegexp(`(?m)^[0-9a-f]{64}\s+\S+`), "SHA256SUMS is not in sha256sum format")

		resp, err = http.Get(agentsBase + "/" + agentVersion + "/SHA256SUMS.sig")
		Expect(err).NotTo(HaveOccurred())
		sig, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusOK), "SHA256SUMS.sig request failed")
		Expect(sig).NotTo(BeEmpty(), "SHA256SUMS.sig is empty")

		resp, err = http.Get(agentsBase + "/" + agentVersion + "/archive")
		Expect(err).NotTo(HaveOccurred())
		archive, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusOK), "archive request failed")
		Expect(archive).NotTo(BeEmpty(), "archive is empty")
	})
})
