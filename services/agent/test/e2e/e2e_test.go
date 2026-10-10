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
	"encoding/base64"
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

// assertDefinitionRef checks that a Skill or Agent Version references a definition ConfigMap whose
// decoded entry file starts with frontmatter, and that no stale README ConfigMap exists for it.
func assertDefinitionRef(versionCRName, wantFileName string) {
	var ref string
	Eventually(func() string {
		cmd := exec.Command("kubectl", "get", "version", versionCRName,
			"-n", namespace,
			"-o", "jsonpath={.status.definitionConfigMapRef.name}",
		)
		out, _ := utils.Run(cmd)
		ref = strings.TrimSpace(out)
		return ref
	}, 5*time.Minute, 5*time.Second).ShouldNot(BeEmpty(), "Version has no definitionConfigMapRef.name")

	fileCmd := exec.Command("kubectl", "get", "version", versionCRName,
		"-n", namespace,
		"-o", "jsonpath={.status.definitionConfigMapRef.fileName}",
	)
	fileOut, err := utils.Run(fileCmd)
	Expect(err).NotTo(HaveOccurred())
	Expect(strings.TrimSpace(fileOut)).To(Equal(wantFileName), "definitionConfigMapRef.fileName is wrong")

	keyCmd := exec.Command("kubectl", "get", "version", versionCRName,
		"-n", namespace,
		"-o", "jsonpath={.status.definitionConfigMapRef.key}",
	)
	keyOut, err := utils.Run(keyCmd)
	Expect(err).NotTo(HaveOccurred())
	key := strings.TrimSpace(keyOut)
	Expect(key).NotTo(BeEmpty(), "definitionConfigMapRef.key is empty")

	dataCmd := exec.Command("kubectl", "get", "configmap", ref,
		"-n", namespace,
		"-o", fmt.Sprintf("jsonpath={.data.%s}", key),
	)
	dataOut, err := utils.Run(dataCmd)
	Expect(err).NotTo(HaveOccurred(), "definition ConfigMap does not exist")
	content, err := base64.StdEncoding.DecodeString(strings.TrimSpace(dataOut))
	Expect(err).NotTo(HaveOccurred(), "definition ConfigMap data is not base64")
	Expect(string(content)).To(HavePrefix("---"), "definition does not start with frontmatter")

	readmeCmd := exec.Command("kubectl", "get", "configmap", versionCRName+"-readme",
		"-n", namespace,
		"--ignore-not-found",
		"-o", "name",
	)
	readmeOut, err := utils.Run(readmeCmd)
	Expect(err).NotTo(HaveOccurred())
	Expect(strings.TrimSpace(readmeOut)).To(BeEmpty(), "Version has a stale README ConfigMap")
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
    githubClientConfig:
      useAuthenticatedClient: true
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

	It("references a definition ConfigMap and no README ConfigMap", func() {
		assertDefinitionRef(skillVersionCRName, "SKILL.md")
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
    githubClientConfig:
      useAuthenticatedClient: true
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

	It("references a definition ConfigMap and no README ConfigMap", func() {
		assertDefinitionRef(agentVersionCRName, "code-review.agent.md")
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

var _ = Describe("Branch Skill", Ordered, func() {
	const (
		branchSkillName     = "acquire-codebase-knowledge"
		branchSkillVersion  = "1.0.0"
		branchVersionCRName = "skill-" + branchSkillName + "-" + utils.SkillFixtureSHA
	)

	BeforeAll(func() {
		By("enabling branch sourcing on the version controller")
		chartPath, err := utils.GetChartPath()
		Expect(err).NotTo(HaveOccurred())

		cmd := exec.Command("helm", "upgrade", helmReleaseName, chartPath,
			"--namespace", namespace,
			"--reuse-values",
			"--set", "branchSourcing.enabled=true",
			"--wait",
			"--timeout", "3m",
		)
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to enable branch sourcing")

		By("applying a Skill with one pending branch entry pinned to the fixture commit")
		skillYAML := fmt.Sprintf(`
apiVersion: opendepot.defdev.io/v1alpha1
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
    repoUrl: https://github.com/%s
    ref: %s
    path: %s
    storageConfig:
      fileSystem:
        directoryPath: %s
  versions:
    - commit: %s
      state: Pending
`, branchSkillName, namespace, branchSkillName, utils.BranchFixtureRepo, utils.BranchFixtureRef,
			utils.SkillFixturePath, agentStoragePath, utils.SkillFixtureSHA)

		skillFile := GinkgoT().TempDir() + "/branch-skill.yaml"
		Expect(os.WriteFile(skillFile, []byte(skillYAML), 0o600)).To(Succeed())

		cmd = exec.Command("kubectl", "apply", "-f", skillFile)
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to apply the branch Skill CR")
	})

	AfterEach(func() {
		if CurrentSpecReport().Failed() {
			dumpSyncDiagnostics()
		}
	})

	AfterAll(func() {
		By("deleting the branch Skill CR")
		cmd := exec.Command("kubectl", "delete", "skill", branchSkillName,
			"-n", namespace,
			"--ignore-not-found",
		)
		_, _ = utils.Run(cmd)

		By("disabling branch sourcing on the version controller")
		chartPath, err := utils.GetChartPath()
		Expect(err).NotTo(HaveOccurred())

		cmd = exec.Command("helm", "upgrade", helmReleaseName, chartPath,
			"--namespace", namespace,
			"--reuse-values",
			"--set", "branchSourcing.enabled=false",
			"--wait",
			"--timeout", "3m",
		)
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to disable branch sourcing")
	})

	It("names the branch Version after its commit with an empty spec.version", func() {
		Expect(utils.RequireFixtureAtMain(utils.SkillFixturePath)).To(BeNil(), "fixture removed upstream")

		By("waiting for the Version named after the pinned commit")
		Eventually(func() error {
			_, err := utils.Run(exec.Command("kubectl", "get", "version", branchVersionCRName, "-n", namespace))
			return err
		}, 2*time.Minute, 2*time.Second).Should(Succeed(), "branch Version was not created")

		By("checking the commit label, spec.sourceCommit, and the empty spec.version")
		cmd := exec.Command("kubectl", "get", "version", branchVersionCRName,
			"-n", namespace,
			"-o", "jsonpath={.metadata.labels.opendepot\\.defdev\\.io/commit} {.spec.sourceCommit} {.spec.version}",
		)
		out, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred())
		Expect(strings.TrimSpace(out)).To(Equal(utils.SkillFixtureSHA+" "+utils.SkillFixtureSHA),
			"branch Version must carry the commit label and sourceCommit with an empty spec.version")
	})

	It("copies the assigned version and state back to the entry and version refs", func() {
		Expect(utils.RequireFixtureAtMain(utils.SkillFixturePath)).To(BeNil(), "fixture removed upstream")

		By("assigning 1.0.0 to the branch Version through spec.version")
		cmd := exec.Command("kubectl", "patch", "version", branchVersionCRName,
			"-n", namespace,
			"--type=merge",
			"-p", `{"spec":{"version":"`+branchSkillVersion+`"}}`,
		)
		_, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to patch the Version spec.version")

		By("marking the branch Version Assigned through its status")
		cmd = exec.Command("kubectl", "patch", "version", branchVersionCRName,
			"-n", namespace,
			"--subresource=status",
			"--type=merge",
			"-p", `{"status":{"branchPhase":"Assigned","synced":true,"syncStatus":"Successfully synced version"}}`,
		)
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to patch the Version status.branchPhase")

		By("checking the Skill entry copies the version and the Assigned state")
		Eventually(func() string {
			cmd := exec.Command("kubectl", "get", "skill", branchSkillName,
				"-n", namespace,
				"-o", "jsonpath={.spec.versions[0].version} {.spec.versions[0].state}",
			)
			out, _ := utils.Run(cmd)
			return strings.TrimSpace(out)
		}, 2*time.Minute, 2*time.Second).Should(Equal(branchSkillVersion+" Assigned"), "entry was not copied back")

		By("checking the Skill version refs point at the SHA-named Version")
		Eventually(func() string {
			cmd := exec.Command("kubectl", "get", "skill", branchSkillName,
				"-n", namespace,
				"-o", "jsonpath={.status.versionRefs.1\\.0\\.0.name}",
			)
			out, _ := utils.Run(cmd)
			return strings.TrimSpace(out)
		}, 2*time.Minute, 2*time.Second).Should(Equal(branchVersionCRName), "versionRefs was not updated")
	})

	It("rejects a second change to the branch Version spec.version", func() {
		Expect(utils.RequireFixtureAtMain(utils.SkillFixturePath)).To(BeNil(), "fixture removed upstream")

		By("patching spec.version to a different version")
		cmd := exec.Command("kubectl", "patch", "version", branchVersionCRName,
			"-n", namespace,
			"--type=merge",
			"-p", `{"spec":{"version":"1.0.1"}}`,
		)
		_, err := utils.Run(cmd)
		Expect(err).To(HaveOccurred(), "second spec.version patch was accepted")
		Expect(err.Error()).To(ContainSubstring("version is write-once for branch Versions"))

		By("checking spec.version is still 1.0.0")
		cmd = exec.Command("kubectl", "get", "version", branchVersionCRName,
			"-n", namespace,
			"-o", "jsonpath={.spec.version}",
		)
		out, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred())
		Expect(strings.TrimSpace(out)).To(Equal(branchSkillVersion))
	})
})
