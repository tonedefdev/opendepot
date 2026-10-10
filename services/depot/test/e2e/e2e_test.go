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
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	utils "github.com/tonedefdev/opendepot/pkg/testutils"
)

var _ = Describe("Depot", Ordered, func() {
	const (
		depotNamespace     = "opendepot-system"
		depotCRName        = "e2e-depot"
		moduleCRName       = "terraform-aws-key-pair"
		moduleVersion      = "2.0.0"
		moduleProvider     = "aws"
		providerCRName     = "random"
		providerVersion    = "3.6.0"
		skillCRName        = "gh-actions-debug"
		skillVersion       = "0.9.0"
		skillVersionCRName = "skill-gh-actions-debug-0-9-0"
		skillRepoOwner     = "tonedefdev"
		skillRepoURL       = "https://github.com/tonedefdev/opendepot"
		skillRepoPath      = ".github/skills/gh-actions-debug"
		skillTagPrefix     = "opendepot-"
		agentCRName        = "code-review"
		agentVersion       = "0.9.0"
		agentVersionCRName = "agent-code-review-0-9-0"
		agentRepoOwner     = "tonedefdev"
		agentRepoURL       = "https://github.com/tonedefdev/opendepot"
		agentRepoPath      = ".github/agents"
		agentTagPrefix     = "opendepot-"
		moduleStoragePath  = "/data/modules"
		branchDepotCRName  = "e2e-branch-depot"
		branchSkillCRName  = "branch-skill-e2e"
		branchInvalidCR    = "e2e-branch-invalid"
		branchRepoURL      = "https://github.com/" + utils.BranchFixtureRepo
		branchTriggerKey   = "opendepot.defdev.io/e2e-trigger"
	)

	BeforeAll(func() {
		By("cleaning up any pre-existing Depot, Module, Provider, Skill, Agent, and Version CRs from previous runs")
		func() {
			cmd := exec.Command("kubectl", "delete", "depot", depotCRName,
				"-n", depotNamespace, "--ignore-not-found")
			_, _ = utils.Run(cmd)
			cmd = exec.Command("kubectl", "delete", "module", moduleCRName,
				"-n", depotNamespace, "--ignore-not-found")
			_, _ = utils.Run(cmd)
			cmd = exec.Command("kubectl", "delete", "provider", providerCRName,
				"-n", depotNamespace, "--ignore-not-found")
			_, _ = utils.Run(cmd)
			cmd = exec.Command("kubectl", "delete", "skill", skillCRName,
				"-n", depotNamespace, "--ignore-not-found")
			_, _ = utils.Run(cmd)
			cmd = exec.Command("kubectl", "delete", "agent", agentCRName,
				"-n", depotNamespace, "--ignore-not-found")
			_, _ = utils.Run(cmd)
			cmd = exec.Command("kubectl", "delete", "version", skillVersionCRName, agentVersionCRName,
				"-n", depotNamespace, "--ignore-not-found")
			_, _ = utils.Run(cmd)
			cmd = exec.Command("kubectl", "delete", "depot", branchDepotCRName,
				"-n", depotNamespace, "--ignore-not-found")
			_, _ = utils.Run(cmd)
			cmd = exec.Command("kubectl", "delete", "skill", branchSkillCRName,
				"-n", depotNamespace, "--ignore-not-found")
			_, _ = utils.Run(cmd)
		}()

		By("applying the Depot CR")
		depotYAML := fmt.Sprintf(`
apiVersion: opendepot.defdev.io/v1alpha1
kind: Depot
metadata:
  name: %s
  namespace: %s
spec:
  moduleConfigs:
    - name: %s
      provider: %s
      repoOwner: terraform-aws-modules
      versionConstraints: "= %s"
      fileFormat: zip
      storageConfig:
        fileSystem:
          directoryPath: %s
  providerConfigs:
    - name: %s
      operatingSystems:
        - linux
      architectures:
        - amd64
      versionConstraints: "= %s"
      storageConfig:
        fileSystem:
          directoryPath: %s
  skillConfigs:
    - name: %s
      repoOwner: %s
      repoUrl: %s
      path: %s
      tagPrefix: %s
      versionConstraints: "= %s"
      storageConfig:
        fileSystem:
          directoryPath: %s
  agentConfigs:
    - name: %s
      repoOwner: %s
      repoUrl: %s
      path: %s
      tagPrefix: %s
      versionConstraints: "= %s"
      storageConfig:
        fileSystem:
          directoryPath: %s
`, depotCRName, depotNamespace,
			moduleCRName, moduleProvider, moduleVersion, moduleStoragePath,
			providerCRName, providerVersion, moduleStoragePath,
			skillCRName, skillRepoOwner, skillRepoURL, skillRepoPath, skillTagPrefix, skillVersion, moduleStoragePath,
			agentCRName, agentRepoOwner, agentRepoURL, agentRepoPath, agentTagPrefix, agentVersion, moduleStoragePath)

		depotFile := filepath.Join(GinkgoT().TempDir(), "test-depot.yaml")
		Expect(os.WriteFile(depotFile, []byte(depotYAML), 0600)).To(Succeed())
		cmd := exec.Command("kubectl", "apply", "-f", depotFile)
		_, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to apply test Depot CR")
	})

	AfterAll(func() {
		cmd := exec.Command("kubectl", "delete", "depot", depotCRName,
			"-n", depotNamespace, "--ignore-not-found")
		_, _ = utils.Run(cmd)
		cmd = exec.Command("kubectl", "delete", "module", moduleCRName,
			"-n", depotNamespace, "--ignore-not-found")
		_, _ = utils.Run(cmd)
		cmd = exec.Command("kubectl", "delete", "provider", providerCRName,
			"-n", depotNamespace, "--ignore-not-found")
		_, _ = utils.Run(cmd)
		cmd = exec.Command("kubectl", "delete", "skill", skillCRName,
			"-n", depotNamespace, "--ignore-not-found")
		_, _ = utils.Run(cmd)
		cmd = exec.Command("kubectl", "delete", "agent", agentCRName,
			"-n", depotNamespace, "--ignore-not-found")
		_, _ = utils.Run(cmd)
		cmd = exec.Command("kubectl", "delete", "version", skillVersionCRName, agentVersionCRName,
			"-n", depotNamespace, "--ignore-not-found")
		_, _ = utils.Run(cmd)
		cmd = exec.Command("kubectl", "delete", "depot", branchDepotCRName,
			"-n", depotNamespace, "--ignore-not-found")
		_, _ = utils.Run(cmd)
		cmd = exec.Command("kubectl", "delete", "skill", branchSkillCRName,
			"-n", depotNamespace, "--ignore-not-found")
		_, _ = utils.Run(cmd)
	})

	It("should create a Module CR from the depot moduleConfigs", func() {
		By("waiting for the Module CR to be created")
		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "get", "module", moduleCRName,
				"-n", depotNamespace,
				"--no-headers",
			)
			output, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(output).NotTo(BeEmpty(), "expected Module CR to exist")
		}, 2*time.Minute, 5*time.Second).Should(Succeed())
	})

	It("should set the correct version on the Module CR", func() {
		By("verifying the Module CR has the expected version")
		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "get", "module", moduleCRName,
				"-n", depotNamespace,
				"-o", fmt.Sprintf(`jsonpath={.spec.versions[?(@.version=="%s")].version}`, moduleVersion),
			)
			output, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(output).To(Equal(moduleVersion), "expected version %s in Module spec", moduleVersion)
		}, 2*time.Minute, 5*time.Second).Should(Succeed())
	})

	It("should set the correct repoOwner on the Module CR", func() {
		By("verifying the Module CR has the expected repoOwner")
		cmd := exec.Command("kubectl", "get", "module", moduleCRName,
			"-n", depotNamespace,
			"-o", `jsonpath={.spec.moduleConfig.repoOwner}`,
		)
		output, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred())
		Expect(output).To(Equal("terraform-aws-modules"))
	})

	It("should create a Provider CR from the depot providerConfigs", func() {
		By("waiting for the Provider CR to be created")
		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "get", "provider", providerCRName,
				"-n", depotNamespace,
				"--no-headers",
			)
			output, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(output).NotTo(BeEmpty(), "expected Provider CR to exist")
		}, 2*time.Minute, 5*time.Second).Should(Succeed())
	})

	It("should set the correct version on the Provider CR", func() {
		By("verifying the Provider CR has the expected version")
		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "get", "provider", providerCRName,
				"-n", depotNamespace,
				"-o", fmt.Sprintf(`jsonpath={.spec.versions[?(@.version=="%s")].version}`, providerVersion),
			)
			output, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(output).To(Equal(providerVersion), "expected version %s in Provider spec", providerVersion)
		}, 2*time.Minute, 5*time.Second).Should(Succeed())
	})

	It("should set the correct operatingSystems on the Provider CR", func() {
		By("verifying the Provider CR has the expected operatingSystems")
		cmd := exec.Command("kubectl", "get", "provider", providerCRName,
			"-n", depotNamespace,
			"-o", `jsonpath={.spec.providerConfig.operatingSystems[0]}`,
		)
		output, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred())
		Expect(output).To(Equal("linux"))
	})

	It("should create a Skill CR with the matched version from the depot skillConfigs", func() {
		By("waiting for the Skill CR to list the matched monorepo path and version")
		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "get", "skill", skillCRName,
				"-n", depotNamespace,
				"-o", "jsonpath={.spec.agentSourceConfig.path} {.spec.versions[*].version}",
			)
			output, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(strings.Fields(output)).To(Equal([]string{skillRepoPath, skillVersion}), "unexpected Skill CR spec")
		}, 2*time.Minute, 5*time.Second).Should(Succeed())
	})

	It("should create an Agent CR with the matched version from the depot agentConfigs", func() {
		By("waiting for the Agent CR to list the matched monorepo path and version")
		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "get", "agent", agentCRName,
				"-n", depotNamespace,
				"-o", "jsonpath={.spec.agentSourceConfig.path} {.spec.versions[*].version}",
			)
			output, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(strings.Fields(output)).To(Equal([]string{agentRepoPath, agentVersion}), "unexpected Agent CR spec")
		}, 2*time.Minute, 5*time.Second).Should(Succeed())
	})

	It("should update the Depot status with managed module and provider names", func() {
		By("verifying Depot status.modules contains the module name")
		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "get", "depot", depotCRName,
				"-n", depotNamespace,
				"-o", `jsonpath={.status.modules}`,
			)
			output, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(output).To(ContainSubstring(moduleCRName))
		}, 2*time.Minute, 5*time.Second).Should(Succeed())

		By("verifying Depot status.providers contains the provider name")
		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "get", "depot", depotCRName,
				"-n", depotNamespace,
				"-o", `jsonpath={.status.providers}`,
			)
			output, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(output).To(ContainSubstring(providerCRName))
		}, 2*time.Minute, 5*time.Second).Should(Succeed())
	})

	It("should update an existing Module CR when the Depot is re-reconciled", func() {
		By("patching the Depot to change the version constraint")
		patchYAML := fmt.Sprintf(`{"spec":{"moduleConfigs":[{"name":%q,"provider":%q,"repoOwner":"terraform-aws-modules","versionConstraints":"= %s","fileFormat":"zip","storageConfig":{"fileSystem":{"directoryPath":%q}}}],"providerConfigs":[{"name":%q,"operatingSystems":["linux"],"architectures":["amd64"],"versionConstraints":"= %s","storageConfig":{"fileSystem":{"directoryPath":%q}}}]}}`,
			moduleCRName, moduleProvider, moduleVersion, moduleStoragePath,
			providerCRName, providerVersion, moduleStoragePath)

		cmd := exec.Command("kubectl", "patch", "depot", depotCRName,
			"-n", depotNamespace,
			"--type=merge",
			"-p", patchYAML,
		)
		_, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to patch Depot")

		By("verifying the Module CR still has the correct version")
		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "get", "module", moduleCRName,
				"-n", depotNamespace,
				"-o", fmt.Sprintf(`jsonpath={.spec.versions[?(@.version=="%s")].version}`, moduleVersion),
			)
			output, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(output).To(Equal(moduleVersion))
		}, 2*time.Minute, 5*time.Second).Should(Succeed())
	})

	It("should reject a skill source that sets both ref and versionConstraints at admission", func() {
		Expect(utils.RequireFixtureAtMain(utils.SkillFixturePath)).To(Succeed())

		By("applying a Depot whose skill source sets ref and versionConstraints")
		depotYAML := fmt.Sprintf(`
apiVersion: opendepot.defdev.io/v1alpha1
kind: Depot
metadata:
  name: %s
  namespace: %s
spec:
  skillConfigs:
    - name: %s
      repoOwner: github
      repoUrl: %s
      path: %s
      ref: %s
      versionConstraints: "= 1.0.0"
      storageConfig:
        fileSystem:
          directoryPath: %s
`, branchInvalidCR, depotNamespace, branchInvalidCR, branchRepoURL,
			utils.SkillFixturePath, utils.BranchFixtureRef, moduleStoragePath)

		depotFile := filepath.Join(GinkgoT().TempDir(), "test-branch-invalid-depot.yaml")
		Expect(os.WriteFile(depotFile, []byte(depotYAML), 0600)).To(Succeed())
		cmd := exec.Command("kubectl", "apply", "-f", depotFile)
		_, err := utils.Run(cmd)
		Expect(err).To(HaveOccurred(), "expected admission to reject ref with versionConstraints")
		Expect(err.Error()).To(ContainSubstring("ref and versionConstraints are mutually exclusive"))

		By("verifying the rejected Depot was not created")
		cmd = exec.Command("kubectl", "get", "depot", branchInvalidCR,
			"-n", depotNamespace,
			"--ignore-not-found",
			"-o", "name",
		)
		output, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred())
		Expect(output).To(BeEmpty(), "expected no Depot CR for the rejected spec")
	})

	It("should not create a Skill from a ref source when branch sourcing is disabled", func() {
		Expect(utils.RequireFixtureAtMain(utils.SkillFixturePath)).To(Succeed())

		By("applying a Depot with a branch skill source while branch sourcing is disabled")
		depotYAML := fmt.Sprintf(`
apiVersion: opendepot.defdev.io/v1alpha1
kind: Depot
metadata:
  name: %s
  namespace: %s
spec:
  skillConfigs:
    - name: %s
      repoOwner: github
      repoUrl: %s
      path: %s
      ref: %s
      branchPolicy:
        minVersionInterval: 0s
      storageConfig:
        fileSystem:
          directoryPath: %s
`, branchDepotCRName, depotNamespace, branchSkillCRName, branchRepoURL,
			utils.SkillFixturePath, utils.BranchFixtureRef, moduleStoragePath)

		depotFile := filepath.Join(GinkgoT().TempDir(), "test-branch-depot.yaml")
		Expect(os.WriteFile(depotFile, []byte(depotYAML), 0600)).To(Succeed())
		cmd := exec.Command("kubectl", "apply", "-f", depotFile)
		_, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to apply branch Depot CR")

		By("triggering a Depot reconcile with an annotation change")
		cmd = exec.Command("kubectl", "annotate", "depot", branchDepotCRName,
			"-n", depotNamespace,
			fmt.Sprintf("%s=%d", branchTriggerKey, time.Now().UnixNano()),
			"--overwrite",
		)
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to annotate branch Depot CR")

		By("verifying no Skill CR is created for the branch source")
		Consistently(func(g Gomega) {
			cmd := exec.Command("kubectl", "get", "skill", branchSkillCRName,
				"-n", depotNamespace,
				"--ignore-not-found",
				"-o", "name",
			)
			output, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(output).To(BeEmpty(), "expected no Skill CR while branch sourcing is disabled")
		}, 30*time.Second, 5*time.Second).Should(Succeed())
	})

	Context("with branch sourcing enabled", Ordered, func() {
		// listBranchEntries returns one "commit|state|version" line per entry in the branch Skill spec.
		listBranchEntries := func(g Gomega) []string {
			cmd := exec.Command("kubectl", "get", "skill", branchSkillCRName,
				"-n", depotNamespace,
				"-o", `jsonpath={range .spec.versions[*]}{.commit}{"|"}{.state}{"|"}{.version}{"\n"}{end}`,
			)
			output, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())

			return strings.Fields(output)
		}

		BeforeAll(func() {
			By("enabling branch sourcing on the Helm release")
			chartPath, err := utils.GetChartPath()
			Expect(err).NotTo(HaveOccurred())

			cmd := exec.Command("helm", "upgrade", helmReleaseName, chartPath,
				"--namespace", depotNamespace,
				"--reuse-values",
				"--set", "branchSourcing.enabled=true",
				"--wait",
				"--timeout", "3m",
			)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to enable branch sourcing")
		})

		It("should add one pending entry with a commit and no version on the first sighting", func() {
			Expect(utils.RequireFixtureAtMain(utils.SkillFixturePath)).To(Succeed())

			By("triggering a Depot reconcile with an annotation change")
			cmd := exec.Command("kubectl", "annotate", "depot", branchDepotCRName,
				"-n", depotNamespace,
				fmt.Sprintf("%s=%d", branchTriggerKey, time.Now().UnixNano()),
				"--overwrite",
			)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to annotate branch Depot CR")

			By("waiting for the Skill CR to list exactly one pending entry")
			Eventually(func(g Gomega) {
				entries := listBranchEntries(g)
				g.Expect(entries).To(HaveLen(1), "expected exactly one branch entry")

				fields := strings.Split(entries[0], "|")
				g.Expect(fields).To(HaveLen(3))
				g.Expect(fields[0]).To(MatchRegexp(`^[0-9a-f]{40}$`), "expected a 40-hex commit SHA")
				g.Expect(fields[1]).To(Equal("Pending"))
				g.Expect(fields[2]).To(BeEmpty(), "the Depot must not assign a version")
			}, 2*time.Minute, 5*time.Second).Should(Succeed())
		})

		It("should not add an entry while the newest entry is pending", func() {
			Expect(utils.RequireFixtureAtMain(utils.SkillFixturePath)).To(Succeed())

			By("triggering another Depot reconcile while the entry is pending")
			cmd := exec.Command("kubectl", "annotate", "depot", branchDepotCRName,
				"-n", depotNamespace,
				fmt.Sprintf("%s=%d", branchTriggerKey, time.Now().UnixNano()),
				"--overwrite",
			)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to annotate branch Depot CR")

			By("verifying the Skill CR still lists exactly one entry")
			Consistently(func(g Gomega) {
				g.Expect(listBranchEntries(g)).To(HaveLen(1), "expected no new entry while the newest entry is pending")
			}, 30*time.Second, 5*time.Second).Should(Succeed())
		})
	})
})
