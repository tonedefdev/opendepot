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
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/crypto/bcrypt"

	utils "github.com/tonedefdev/opendepot/pkg/testutils"
)

// agentCatalogEnvelope mirrors the catalog response for assertions.
type agentCatalogEnvelope struct {
	Items      []agentCatalogEntry `json:"items"`
	TotalCount int                 `json:"totalCount"`
	Page       int                 `json:"page"`
	PageSize   int                 `json:"pageSize"`
}

// agentCatalogEntry mirrors a catalog item for assertions.
type agentCatalogEntry struct {
	Kind                 string               `json:"kind"`
	Namespace            string               `json:"namespace"`
	Name                 string               `json:"name"`
	Status               string               `json:"status"`
	LatestVersion        string               `json:"latestVersion,omitempty"`
	UnavailableCandidate *agentCandidateEntry `json:"unavailableCandidate,omitempty"`
}

// agentVersionsEnvelope mirrors the versions response for assertions.
type agentVersionsEnvelope struct {
	Versions              []agentVersionEntry   `json:"versions"`
	UnavailableCandidates []agentCandidateEntry `json:"unavailableCandidates,omitempty"`
}

// agentVersionEntry mirrors a listed version for assertions.
type agentVersionEntry struct {
	Version string `json:"version"`
	Blocked bool   `json:"blocked"`
}

// agentCandidateEntry mirrors an unavailable candidate for assertions.
type agentCandidateEntry struct {
	Version string   `json:"version,omitempty"`
	Commit  string   `json:"commit,omitempty"`
	Status  string   `json:"status"`
	Reasons []string `json:"reasons,omitempty"`
}

// Agent Registry e2e tests verify the Skill endpoints under /opendepot/agents/v1: unavailable
// candidates in the versions response, refusal to download blocked and rejected candidates,
// the paginated and namespace-filtered catalog, and the GroupBinding authorization model.
var _ = Describe("Agent Registry", Ordered, func() {
	const (
		agentServerPort     = 19180
		agentOIDCServerPort = 19181
		agentDexLocalPort   = 15570

		agentPublicNS   = "agent-public-e2e"
		agentPrivateNS  = "agent-private-e2e"
		agentAlphaSkill = "alpha-skill-e2e"
		agentBetaSkill  = "beta-skill-e2e"
		agentGammaSkill = "gamma-skill-e2e"

		agentInstallable = "1.0.0"
		agentBlocked     = "2.0.0"
		agentBranchRef   = "branch-rejected"
		agentCommit      = "3f2a9c1d8e7b6a5f4e3d2c1b0a9f8e7d6c5b4a39"

		agentTestUserEmail    = "agent-gb-e2e@example.com"
		agentTestUserPassword = "agentgbpassword"
		agentTestUserID       = "agent-gb-user-oidc-e2e"
		agentTestClientSecret = "agent-client-secret-e2e"
		agentGroupBindingName = "agent-e2e-groupbinding"
	)

	var (
		chartPath  string
		serverRepo string
		serverTag  string
	)

	// deployAgentServer runs helm upgrade with the base configuration used by the other e2e
	// suites, plus the auth-mode flags supplied by the caller.
	deployAgentServer := func(extraArgs ...string) {
		baseArgs := []string{
			"upgrade", helmReleaseName, chartPath,
			"--install",
			"--create-namespace",
			"--namespace", namespace,
			"--skip-crds",
			"--set", "monitoring.enabled=false",
			"--set", "monitoring.bundled.enabled=false",
			"--set", "global.image.tag=",
			"--set", "depot.enabled=false",
			"--set", "module.enabled=false",
			"--set", "provider.enabled=false",
			"--set", "version.enabled=false",
			"--set", "scanning.enabled=false",
			"--set", "scanning.providerScanning=false",
			"--set", "server.enabled=true",
			"--set", fmt.Sprintf("server.image.repository=%s", serverRepo),
			"--set", fmt.Sprintf("server.image.tag=%s", serverTag),
			"--set", "valkey.dataStorage.enabled=false",
			"--wait",
			"--timeout", "2m",
		}
		args := append(baseArgs, extraArgs...)
		cmd := exec.Command("helm", args...)
		_, err := utils.Run(cmd)
		ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to helm upgrade server")
	}

	// startAgentPortForward starts a self-restarting port-forward to the server Service and
	// blocks until the discovery endpoint answers.
	startAgentPortForward := func(localPort int) context.CancelFunc {
		ctx, cancel := context.WithCancel(context.Background())

		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				default:
				}

				cmd := exec.CommandContext(ctx, "kubectl", "port-forward",
					"-n", namespace,
					"svc/server",
					fmt.Sprintf("%d:80", localPort),
				)
				cmd.Stdout = GinkgoWriter
				cmd.Stderr = GinkgoWriter
				_ = cmd.Run()
				time.Sleep(200 * time.Millisecond)
			}
		}()

		Eventually(func() error {
			resp, err := http.Get(fmt.Sprintf("http://localhost:%d/.well-known/terraform.json", localPort))
			if err != nil {
				return err
			}
			defer resp.Body.Close()

			return nil
		}, 60*time.Second, 1*time.Second).Should(Succeed(), "timed out waiting for server port-forward")

		return cancel
	}

	// startAgentDexPortForward starts a self-restarting port-forward to Dex and waits for discovery.
	startAgentDexPortForward := func(localPort int) context.CancelFunc {
		ctx, cancel := context.WithCancel(context.Background())

		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				default:
				}

				cmd := exec.CommandContext(ctx, "kubectl", "port-forward",
					"-n", namespace,
					"svc/opendepot-dex",
					fmt.Sprintf("%d:5556", localPort),
				)
				cmd.Stdout = GinkgoWriter
				cmd.Stderr = GinkgoWriter
				_ = cmd.Run()
				time.Sleep(200 * time.Millisecond)
			}
		}()

		Eventually(func() error {
			resp, err := http.Get(fmt.Sprintf("http://localhost:%d/dex/.well-known/openid-configuration", localPort))
			if err != nil {
				return err
			}
			defer resp.Body.Close()

			return nil
		}, 60*time.Second, 1*time.Second).Should(Succeed(), "timed out waiting for Dex port-forward")

		return cancel
	}

	// agentRequest performs a GET against the server, optionally with a bearer token, and returns
	// the status code and body.
	agentRequest := func(localPort int, path, token string) (int, []byte) {
		req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://localhost:%d%s", localPort, path), nil)
		ExpectWithOffset(1, err).NotTo(HaveOccurred())

		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}

		resp, err := http.DefaultClient.Do(req)
		ExpectWithOffset(1, err).NotTo(HaveOccurred())
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		ExpectWithOffset(1, err).NotTo(HaveOccurred())

		return resp.StatusCode, body
	}

	// agentVersionsPath returns the versions endpoint path for a Skill.
	agentVersionsPath := func(ns, name string) string {
		return fmt.Sprintf("/opendepot/agents/v1/%s/skills/%s/versions", ns, name)
	}

	// agentDownloadPath returns the download endpoint path for a Skill version.
	agentDownloadPath := func(ns, name, version string) string {
		return fmt.Sprintf("/opendepot/agents/v1/%s/skills/%s/%s/download", ns, name, version)
	}

	// agentArchivePath returns the archive endpoint path for a Skill version.
	agentArchivePath := func(ns, name, version string) string {
		return fmt.Sprintf("/opendepot/agents/v1/%s/skills/%s/%s/archive", ns, name, version)
	}

	// agentVersionResource returns the Version resource name for a Skill, matching the server's naming.
	agentVersionResource := func(name, version string) string {
		return fmt.Sprintf("skill-%s-%s", name, strings.ReplaceAll(version, ".", "-"))
	}

	// applyAgentManifest applies a manifest with kubectl.
	applyAgentManifest := func(manifest string) {
		cmd := exec.Command("kubectl", "apply", "-f", "-")
		cmd.Stdin = strings.NewReader(manifest)
		_, err := utils.Run(cmd)
		ExpectWithOffset(1, err).NotTo(HaveOccurred(), "failed to apply agent fixtures")
	}

	// patchAgentStatus merges a status patch into the named resource.
	patchAgentStatus := func(resource, name, ns, patch string) {
		cmd := exec.Command("kubectl", "patch", resource, name, "-n", ns,
			"--subresource=status", "--type=merge", "-p", patch)
		_, err := utils.Run(cmd)
		ExpectWithOffset(1, err).NotTo(HaveOccurred(), "failed to patch status of %s/%s", resource, name)
	}

	// createAgentFixtures creates the namespaces and Skill and Version fixtures used by every case.
	// Public namespace and Skills carry the public label; gamma lives in the private namespace.
	// It is idempotent, so the OIDC context reuses the same fixtures.
	createAgentFixtures := func() {
		applyAgentManifest(fmt.Sprintf(`apiVersion: v1
kind: Namespace
metadata:
  name: %s
  labels:
    opendepot.defdev.io/public: "true"
---
apiVersion: v1
kind: Namespace
metadata:
  name: %s
`, agentPublicNS, agentPrivateNS))

		applyAgentManifest(fmt.Sprintf(`apiVersion: opendepot.defdev.io/v1alpha1
kind: Skill
metadata:
  name: %[1]s
  namespace: %[3]s
  labels:
    opendepot.defdev.io/public: "true"
spec:
  agentSourceConfig:
    repoOwner: example
    repoUrl: https://github.com/example/%[1]s
    path: skills/%[1]s
  versions: []
---
apiVersion: opendepot.defdev.io/v1alpha1
kind: Skill
metadata:
  name: %[2]s
  namespace: %[3]s
  labels:
    opendepot.defdev.io/public: "true"
spec:
  agentSourceConfig:
    repoOwner: example
    repoUrl: https://github.com/example/%[2]s
    path: skills/%[2]s
  versions: []
---
apiVersion: opendepot.defdev.io/v1alpha1
kind: Skill
metadata:
  name: %[4]s
  namespace: %[5]s
spec:
  agentSourceConfig:
    repoOwner: example
    repoUrl: https://github.com/example/%[4]s
    path: skills/%[4]s
  versions: []
`, agentAlphaSkill, agentBetaSkill, agentPublicNS, agentGammaSkill, agentPrivateNS))

		for _, skill := range []struct{ name, ns string }{
			{agentAlphaSkill, agentPublicNS},
			{agentBetaSkill, agentPublicNS},
			{agentGammaSkill, agentPrivateNS},
		} {
			patchAgentStatus("skills", skill.name, skill.ns, `{"status":{"synced":true,"syncStatus":"Synced"}}`)
		}

		// The controller records versioned Skill entries in versionRefs. The versions endpoint lists from them.
		patchAgentStatus("skills", agentAlphaSkill, agentPublicNS, fmt.Sprintf(`{"status":{"versionRefs":{"%[1]s":{"version":"%[1]s","name":"%[2]s","synced":true,"fileName":"alpha.zip"},"%[3]s":{"version":"%[3]s","name":"%[4]s","synced":false}}}}`,
			agentInstallable, agentVersionResource(agentAlphaSkill, agentInstallable),
			agentBlocked, agentVersionResource(agentAlphaSkill, agentBlocked)))

		// Alpha: an installable 1.0.0, a blocked 2.0.0, and an unversioned rejected branch candidate.
		applyAgentManifest(fmt.Sprintf(`apiVersion: opendepot.defdev.io/v1alpha1
kind: Version
metadata:
  name: %[1]s
  namespace: %[2]s
spec:
  type: Skill
  version: %[3]q
  fileName: %[4]s-%[3]s.zip
  agentSourceRef:
    name: %[4]s
---
apiVersion: opendepot.defdev.io/v1alpha1
kind: Version
metadata:
  name: %[5]s
  namespace: %[2]s
spec:
  type: Skill
  version: %[6]q
  fileName: %[4]s-%[6]s.zip
  agentSourceRef:
    name: %[4]s
---
apiVersion: opendepot.defdev.io/v1alpha1
kind: Version
metadata:
  name: skill-%[4]s-%[7]s
  namespace: %[2]s
spec:
  type: Skill
  version: ""
  sourceCommit: %[8]s
  agentSourceRef:
    name: %[4]s
    ref: %[7]s
`, agentVersionResource(agentAlphaSkill, agentInstallable), agentPublicNS, agentInstallable, agentAlphaSkill,
			agentVersionResource(agentAlphaSkill, agentBlocked), agentBlocked, agentBranchRef, agentCommit))

		patchAgentStatus("versions", agentVersionResource(agentAlphaSkill, agentInstallable), agentPublicNS,
			`{"status":{"synced":true,"syncStatus":"Synced","fileName":"alpha.zip","checksum":"abc123","shaSums":"abc123  alpha.zip","shaSumsSignature":"c2ln"}}`)
		patchAgentStatus("versions", agentVersionResource(agentAlphaSkill, agentBlocked), agentPublicNS,
			`{"status":{"synced":false,"syncStatus":"Synced","jevAssessment":{"evaluatedAt":"2026-01-01T00:00:00Z","blocked":true,"riskLevel":"High","riskScore":0.92,"needsReview":false,"blockReasons":["injection risk above threshold"]}}}`)
		patchAgentStatus("versions", "skill-"+agentAlphaSkill+"-"+agentBranchRef, agentPublicNS,
			`{"status":{"synced":false,"syncStatus":"Scan policy violation: source scan failed","branchPhase":"Rejected"}}`)

		// Beta: a single installable 1.0.0, used for pagination.
		applyAgentManifest(fmt.Sprintf(`apiVersion: opendepot.defdev.io/v1alpha1
kind: Version
metadata:
  name: %[1]s
  namespace: %[2]s
spec:
  type: Skill
  version: %[3]q
  fileName: %[4]s-%[3]s.zip
  agentSourceRef:
    name: %[4]s
`, agentVersionResource(agentBetaSkill, agentInstallable), agentPublicNS, agentInstallable, agentBetaSkill))
		patchAgentStatus("versions", agentVersionResource(agentBetaSkill, agentInstallable), agentPublicNS,
			`{"status":{"synced":true,"syncStatus":"Synced","fileName":"beta.zip","checksum":"def456","shaSums":"def456  beta.zip","shaSumsSignature":"c2ln"}}`)

		// Gamma: a private installable 1.0.0 in the private namespace.
		applyAgentManifest(fmt.Sprintf(`apiVersion: opendepot.defdev.io/v1alpha1
kind: Version
metadata:
  name: %[1]s
  namespace: %[2]s
spec:
  type: Skill
  version: %[3]q
  fileName: %[4]s-%[3]s.zip
  agentSourceRef:
    name: %[4]s
`, agentVersionResource(agentGammaSkill, agentInstallable), agentPrivateNS, agentInstallable, agentGammaSkill))
		patchAgentStatus("versions", agentVersionResource(agentGammaSkill, agentInstallable), agentPrivateNS,
			`{"status":{"synced":true,"syncStatus":"Synced","fileName":"gamma.zip","checksum":"ghi789","shaSums":"ghi789  gamma.zip","shaSumsSignature":"c2ln"}}`)

		// Gamma also carries a blocked 2.0.0 so an authorized caller can be checked against a refused artifact.
		applyAgentManifest(fmt.Sprintf(`apiVersion: opendepot.defdev.io/v1alpha1
kind: Version
metadata:
  name: %[1]s
  namespace: %[2]s
spec:
  type: Skill
  version: %[3]q
  fileName: %[4]s-%[3]s.zip
  agentSourceRef:
    name: %[4]s
`, agentVersionResource(agentGammaSkill, agentBlocked), agentPrivateNS, agentBlocked, agentGammaSkill))
		patchAgentStatus("versions", agentVersionResource(agentGammaSkill, agentBlocked), agentPrivateNS,
			`{"status":{"synced":false,"syncStatus":"Synced","jevAssessment":{"evaluatedAt":"2026-01-01T00:00:00Z","blocked":true,"riskLevel":"High","riskScore":0.92,"needsReview":false,"blockReasons":["injection risk above threshold"]}}}`)
	}

	// decodeInto unmarshals a response body into v and fails the spec on error.
	decodeInto := func(body []byte, v any) {
		ExpectWithOffset(1, json.Unmarshal(body, v)).To(Succeed(), string(body))
	}

	BeforeAll(func() {
		var err error
		chartPath, err = utils.GetChartPath()
		Expect(err).NotTo(HaveOccurred())
		serverRepo, serverTag = utils.SplitImageRef(serverImage)
	})

	AfterAll(func() {
		for _, ns := range []string{agentPublicNS, agentPrivateNS} {
			cmd := exec.Command("kubectl", "delete", "namespace", ns, "--ignore-not-found", "--wait=false")
			_, _ = utils.Run(cmd)
		}
	})

	Context("anonymous auth mode", Ordered, func() {
		var pfCancel context.CancelFunc

		BeforeAll(func() {
			By("deploying server with --anonymous-auth=true")
			deployAgentServer(
				"--set", "server.anonymousAuth=true",
				"--set", "server.useBearerToken=false",
			)
			pfCancel = startAgentPortForward(agentServerPort)

			By("creating fixtures: public Skills alpha and beta, private Skill gamma")
			createAgentFixtures()
		})

		AfterAll(func() {
			if pfCancel != nil {
				pfCancel()
			}
		})

		It("lists a blocked newer version in unavailableCandidates while keeping the installable older version in versions", func() {
			var versions agentVersionsEnvelope

			Eventually(func() int {
				status, body := agentRequest(agentServerPort, agentVersionsPath(agentPublicNS, agentAlphaSkill), "")
				if status == http.StatusOK {
					decodeInto(body, &versions)
				}

				return status
			}, 60*time.Second, 2*time.Second).Should(Equal(http.StatusOK))

			installable := false
			for _, v := range versions.Versions {
				if v.Version == agentInstallable {
					installable = true
					Expect(v.Blocked).To(BeFalse())
				}
			}
			Expect(installable).To(BeTrue(), "installable version missing from versions: %+v", versions)

			var blocked, branch *agentCandidateEntry
			for i := range versions.UnavailableCandidates {
				c := versions.UnavailableCandidates[i]
				switch {
				case c.Version == agentBlocked:
					blocked = &c
				case c.Commit == agentCommit:
					branch = &c
				}
			}

			Expect(blocked).NotTo(BeNil(), "blocked version missing from unavailableCandidates")
			Expect(blocked.Status).To(Equal("blocked"))
			Expect(blocked.Reasons).To(ContainElement("injection risk above threshold"))

			Expect(branch).NotTo(BeNil(), "unversioned rejected branch missing from unavailableCandidates")
			Expect(branch.Status).To(Equal("rejected"))
		})

		It("refuses to download or archive a blocked version", func() {
			for _, path := range []string{
				agentDownloadPath(agentPublicNS, agentAlphaSkill, agentBlocked),
				agentArchivePath(agentPublicNS, agentAlphaSkill, agentBlocked),
			} {
				status, _ := agentRequest(agentServerPort, path, "")
				Expect(status).To(Equal(http.StatusNotFound), path)
			}
		})

		It("refuses to download a rejected unversioned branch candidate", func() {
			status, _ := agentRequest(agentServerPort, agentDownloadPath(agentPublicNS, agentAlphaSkill, agentBranchRef), "")
			Expect(status).To(Equal(http.StatusNotFound))
		})

		It("reports the newest unavailable candidate status in the catalog while keeping the installable latest version", func() {
			var catalog agentCatalogEnvelope

			status, body := agentRequest(agentServerPort, "/opendepot/agents/v1/catalog?namespace="+agentPublicNS, "")
			Expect(status).To(Equal(http.StatusOK), string(body))
			decodeInto(body, &catalog)

			Expect(catalog.TotalCount).To(Equal(2))
			byName := map[string]agentCatalogEntry{}
			for _, item := range catalog.Items {
				byName[item.Name] = item
			}

			alpha, ok := byName[agentAlphaSkill]
			Expect(ok).To(BeTrue())
			Expect(alpha.LatestVersion).To(Equal(agentInstallable))
			Expect(alpha.Status).To(BeElementOf("blocked", "rejected"))
			Expect(alpha.UnavailableCandidate).NotTo(BeNil())

			beta, ok := byName[agentBetaSkill]
			Expect(ok).To(BeTrue())
			Expect(beta.Status).To(Equal("available"))
			Expect(beta.LatestVersion).To(Equal(agentInstallable))
		})

		It("pages through the catalog with page_size and stable ordering", func() {
			seen := map[string]bool{}
			for page := 1; page <= 2; page++ {
				var catalog agentCatalogEnvelope
				path := fmt.Sprintf("/opendepot/agents/v1/catalog?namespace=%s&page_size=1&page=%d", agentPublicNS, page)

				status, body := agentRequest(agentServerPort, path, "")
				Expect(status).To(Equal(http.StatusOK), string(body))
				decodeInto(body, &catalog)

				Expect(catalog.PageSize).To(Equal(1))
				Expect(catalog.Page).To(Equal(page))
				Expect(catalog.TotalCount).To(Equal(2))
				Expect(catalog.Items).To(HaveLen(1))
				seen[catalog.Items[0].Name] = true
			}

			Expect(seen).To(HaveKey(agentAlphaSkill))
			Expect(seen).To(HaveKey(agentBetaSkill))
		})

		It("filters the catalog by namespace using the same visibility as the UI browse API", func() {
			var privateCatalog agentCatalogEnvelope

			// Configured anonymous access has all-access visibility, matching the UI browse API.
			status, body := agentRequest(agentServerPort, "/opendepot/agents/v1/catalog?namespace="+agentPrivateNS, "")
			Expect(status).To(Equal(http.StatusOK), string(body))
			decodeInto(body, &privateCatalog)
			Expect(privateCatalog.TotalCount).To(Equal(1))
			Expect(privateCatalog.Items).To(HaveLen(1))
			Expect(privateCatalog.Items[0].Name).To(Equal(agentGammaSkill))
			Expect(privateCatalog.Items[0].Namespace).To(Equal(agentPrivateNS))

			status, body = agentRequest(agentServerPort, "/opendepot/agents/v1/catalog?namespace="+agentPublicNS, "")
			Expect(status).To(Equal(http.StatusOK), string(body))

			var publicCatalog agentCatalogEnvelope
			decodeInto(body, &publicCatalog)
			for _, item := range publicCatalog.Items {
				Expect(item.Namespace).To(Equal(agentPublicNS))
			}

			status, body = agentRequest(agentServerPort, "/opendepot/agents/v1/catalog", "")
			Expect(status).To(Equal(http.StatusOK), string(body))

			var all agentCatalogEnvelope
			decodeInto(body, &all)
			Expect(all.TotalCount).To(BeNumerically(">=", 3))
		})

		It("rejects an oversized page_size with 400", func() {
			status, _ := agentRequest(agentServerPort, "/opendepot/agents/v1/catalog?page_size=500", "")
			Expect(status).To(Equal(http.StatusBadRequest))
		})
	})

	Context("OIDC auth mode with GroupBinding", Ordered, func() {
		var (
			pfServerCancel context.CancelFunc
			pfDexCancel    context.CancelFunc
			oidcJWT        string
		)

		BeforeAll(func() {
			By("uninstalling the previous release to avoid Dex server-side apply conflicts")
			uninstallCmd := exec.Command("helm", "uninstall", helmReleaseName,
				"--namespace", namespace, "--ignore-not-found")
			_, _ = utils.Run(uninstallCmd)

			By("generating a bcrypt hash for the test password")
			hashBytes, err := bcrypt.GenerateFromPassword([]byte(agentTestUserPassword), 10)
			Expect(err).NotTo(HaveOccurred())
			ensureOIDCClientSecret(agentTestClientSecret)

			valuesFile := filepath.Join(GinkgoT().TempDir(), "agent-e2e-values.yaml")
			dexValues := fmt.Sprintf(`dex:
  enabled: true
  config:
    issuer: http://opendepot-dex.opendepot-system.svc.cluster.local:5556/dex
    storage:
      type: memory
    enablePasswordDB: true
    oauth2:
      responseTypes:
        - code
      grantTypes:
        - authorization_code
        - password
      skipApprovalScreen: true
      passwordConnector: local
    staticPasswords:
      - email: %q
        hash: %q
        username: agentgbuser
        userID: %q
    staticClients:
      - id: opendepot
        name: OpenDepot
        secretEnv: OPENDEPOT_DEX_CLIENT_SECRET
        redirectURIs:
          - http://localhost:10000
    connectors: []
server:
  oidc:
    enabled: true
    clientSecretName: opendepot-dex-client-secret
    dexProxy:
      enabled: false
    groupsClaim: email
`, agentTestUserEmail, string(hashBytes), agentTestUserID)
			Expect(os.WriteFile(valuesFile, []byte(dexValues), 0600)).To(Succeed())

			By("deploying server with OIDC auth mode and Dex enabled")
			deployAgentServer(
				"--set", "server.anonymousAuth=false",
				"--set", "server.useBearerToken=false",
				"-f", valuesFile,
				"--timeout", "10m",
			)
			pfServerCancel = startAgentPortForward(agentOIDCServerPort)
			pfDexCancel = startAgentDexPortForward(agentDexLocalPort)

			By("ensuring the fixtures exist")
			createAgentFixtures()

			By("acquiring a Dex JWT via the password grant")
			Eventually(func() error {
				token, err := agentAcquireDexToken(agentDexLocalPort, agentTestClientSecret, agentTestUserEmail, agentTestUserPassword)
				if err != nil {
					return err
				}
				oidcJWT = token

				return nil
			}, 30*time.Second, 2*time.Second).Should(Succeed(), "failed to acquire Dex JWT")
		})

		AfterAll(func() {
			if pfServerCancel != nil {
				pfServerCancel()
			}
			if pfDexCancel != nil {
				pfDexCancel()
			}
			cmd := exec.Command("kubectl", "delete", "groupbinding", agentGroupBindingName, "-n", namespace, "--ignore-not-found")
			_, _ = utils.Run(cmd)
		})

		It("denies the versions endpoint without a token", func() {
			status, _ := agentRequest(agentOIDCServerPort, agentVersionsPath(agentPrivateNS, agentGammaSkill), "")
			Expect(status).To(Equal(http.StatusUnauthorized))
		})

		It("denies the catalog without a token", func() {
			status, _ := agentRequest(agentOIDCServerPort, "/opendepot/agents/v1/catalog?namespace="+agentPrivateNS, "")
			Expect(status).To(Equal(http.StatusUnauthorized))
		})

		It("denies a valid JWT that matches no GroupBinding", func() {
			status, _ := agentRequest(agentOIDCServerPort, agentVersionsPath(agentPrivateNS, agentGammaSkill), oidcJWT)
			Expect(status).To(Equal(http.StatusForbidden))
		})

		It("allows a matching GroupBinding and denies a Skill outside its resources", func() {
			manifest := fmt.Sprintf(`apiVersion: opendepot.defdev.io/v1alpha1
kind: GroupBinding
metadata:
  name: %s
  namespace: %s
spec:
  expression: %q
  skillResources:
    - %q
`, agentGroupBindingName, namespace, fmt.Sprintf("%q in groups", agentTestUserEmail), agentGammaSkill)
			applyAgentManifest(manifest)

			Eventually(func() int {
				status, _ := agentRequest(agentOIDCServerPort, agentVersionsPath(agentPrivateNS, agentGammaSkill), oidcJWT)

				return status
			}, 60*time.Second, 2*time.Second).Should(Equal(http.StatusOK))

			status, _ := agentRequest(agentOIDCServerPort, agentVersionsPath(agentPublicNS, agentAlphaSkill), oidcJWT)
			Expect(status).To(Equal(http.StatusForbidden))
		})

		It("keeps refusing a blocked version for an authorized caller", func() {
			status, _ := agentRequest(agentOIDCServerPort, agentDownloadPath(agentPrivateNS, agentGammaSkill, agentBlocked), oidcJWT)
			Expect(status).To(Equal(http.StatusNotFound))
		})

		It("returns the catalog to an authorized caller with the private Skill visible", func() {
			var catalog agentCatalogEnvelope

			Eventually(func() int {
				status, body := agentRequest(agentOIDCServerPort, "/opendepot/agents/v1/catalog?namespace="+agentPrivateNS, oidcJWT)
				if status == http.StatusOK {
					decodeInto(body, &catalog)
				}

				return status
			}, 60*time.Second, 2*time.Second).Should(Equal(http.StatusOK))

			Expect(catalog.Items).To(HaveLen(1))
			Expect(catalog.Items[0].Name).To(Equal(agentGammaSkill))
			Expect(catalog.Items[0].LatestVersion).To(Equal(agentInstallable))
		})
	})
})

// agentAcquireDexToken performs a password grant against Dex and returns the ID token.
func agentAcquireDexToken(localPort int, clientSecret, username, password string) (string, error) {
	form := url.Values{
		"grant_type":    {"password"},
		"username":      {username},
		"password":      {password},
		"scope":         {"openid email"},
		"client_id":     {"opendepot"},
		"client_secret": {clientSecret},
	}

	resp, err := http.PostForm(fmt.Sprintf("http://localhost:%d/dex/token", localPort), form)
	if err != nil {
		return "", fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)

		return "", fmt.Errorf("token endpoint returned %d: %s", resp.StatusCode, body)
	}

	var tokenResp struct {
		IDToken string `json:"id_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", fmt.Errorf("failed to decode token response: %w", err)
	}

	if tokenResp.IDToken == "" {
		return "", fmt.Errorf("token response contained empty id_token")
	}

	return tokenResp.IDToken, nil
}
