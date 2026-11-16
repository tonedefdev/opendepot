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
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	utils "github.com/tonedefdev/opendepot/pkg/testutils"
)

// Agent description e2e tests verify that the catalog exposes the description of the latest
// installable Version, and that a Skill outside the public catalog does not leak its description.
// Anonymous auth grants browse-all, so the server runs without it to exercise public-only visibility.
var _ = Describe("Agent description catalog", Ordered, func() {
	const (
		descriptionServerPort = 19190

		descriptionPublicNS  = "description-public-e2e"
		descriptionPrivateNS = "description-private-e2e"
		descriptionPublicSk  = "described-skill-e2e"
		descriptionPrivateSk = "private-described-e2e"

		descriptionVersion = "1.0.0"
		descriptionText    = "Reviews pull requests for security issues.\nSecond line of detail."
		descriptionPrivate = "Private skill description that must not be listed to unauthenticated callers."
	)

	var (
		chartPath  string
		serverRepo string
		serverTag  string
		pfCancel   context.CancelFunc
	)

	// descriptionCatalogItem mirrors the catalog fields this suite asserts on.
	type descriptionCatalogItem struct {
		Kind        string `json:"kind"`
		Namespace   string `json:"namespace"`
		Name        string `json:"name"`
		Description string `json:"description,omitempty"`
	}

	type descriptionCatalogEnvelope struct {
		Items []descriptionCatalogItem `json:"items"`
	}

	// descriptionVersionResource returns the Version resource name for a Skill, matching the server's naming.
	descriptionVersionResource := func(name, version string) string {
		return fmt.Sprintf("skill-%s-%s", name, strings.ReplaceAll(version, ".", "-"))
	}

	// descriptionGet performs an unauthenticated GET against the server and returns the status code and body.
	descriptionGet := func(path string) (int, []byte) {
		resp, err := http.Get(fmt.Sprintf("http://localhost:%d%s", descriptionServerPort, path))
		ExpectWithOffset(1, err).NotTo(HaveOccurred())
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		ExpectWithOffset(1, err).NotTo(HaveOccurred())

		return resp.StatusCode, body
	}

	// descriptionCatalog fetches the catalog with the given query and decodes its items.
	descriptionCatalog := func(query string) []descriptionCatalogItem {
		status, body := descriptionGet("/opendepot/agents/v1/catalog" + query)
		ExpectWithOffset(1, status).To(Equal(http.StatusOK), "catalog request failed: %s", body)

		var envelope descriptionCatalogEnvelope
		ExpectWithOffset(1, json.Unmarshal(body, &envelope)).To(Succeed())

		return envelope.Items
	}

	// findDescriptionItem returns the catalog item matching the namespace and name, or nil.
	findDescriptionItem := func(items []descriptionCatalogItem, ns, name string) *descriptionCatalogItem {
		for i := range items {
			if items[i].Namespace == ns && items[i].Name == name {
				return &items[i]
			}
		}

		return nil
	}

	// startDescriptionPortForward starts a self-restarting port-forward to the server Service and
	// blocks until the discovery endpoint answers.
	startDescriptionPortForward := func() context.CancelFunc {
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
					fmt.Sprintf("%d:80", descriptionServerPort),
				)
				cmd.Stdout = GinkgoWriter
				cmd.Stderr = GinkgoWriter
				_ = cmd.Run()
				time.Sleep(200 * time.Millisecond)
			}
		}()

		Eventually(func() error {
			resp, err := http.Get(fmt.Sprintf("http://localhost:%d/.well-known/terraform.json", descriptionServerPort))
			if err != nil {
				return err
			}
			defer resp.Body.Close()

			return nil
		}, 60*time.Second, 1*time.Second).Should(Succeed(), "timed out waiting for server port-forward")

		return cancel
	}

	BeforeAll(func() {
		var err error
		chartPath, err = utils.GetChartPath()
		Expect(err).NotTo(HaveOccurred(), "Failed to resolve chart path")

		serverRepo, serverTag = utils.SplitImageRef(serverImage)

		By("deploying server in non-OIDC mode, where the catalog is public-only")
		args := []string{
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
			"--set", "server.useBearerToken=false",
			"--set", "valkey.dataStorage.enabled=false",
			"--wait",
			"--timeout", "2m",
		}
		_, err = utils.Run(exec.Command("helm", args...))
		Expect(err).NotTo(HaveOccurred(), "Failed to helm upgrade server")

		By("creating a public Skill with a description and a private Skill outside the public catalog")
		applyDescriptionManifest(fmt.Sprintf(`apiVersion: v1
kind: Namespace
metadata:
  name: %[1]s
  labels:
    opendepot.defdev.io/public: "true"
---
apiVersion: v1
kind: Namespace
metadata:
  name: %[2]s
---
apiVersion: opendepot.defdev.io/v1alpha1
kind: Skill
metadata:
  name: %[3]s
  namespace: %[1]s
  labels:
    opendepot.defdev.io/public: "true"
spec:
  agentSourceConfig:
    repoOwner: example
    repoUrl: https://github.com/example/%[3]s
    path: skills/%[3]s
  versions: []
---
apiVersion: opendepot.defdev.io/v1alpha1
kind: Skill
metadata:
  name: %[4]s
  namespace: %[2]s
spec:
  agentSourceConfig:
    repoOwner: example
    repoUrl: https://github.com/example/%[4]s
    path: skills/%[4]s
  versions: []
`, descriptionPublicNS, descriptionPrivateNS, descriptionPublicSk, descriptionPrivateSk))

		patchDescriptionStatus("skills", descriptionPublicSk, descriptionPublicNS,
			`{"status":{"synced":true,"syncStatus":"Synced"}}`)
		patchDescriptionStatus("skills", descriptionPrivateSk, descriptionPrivateNS,
			`{"status":{"synced":true,"syncStatus":"Synced"}}`)

		applyDescriptionManifest(fmt.Sprintf(`apiVersion: opendepot.defdev.io/v1alpha1
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
  namespace: %[6]s
spec:
  type: Skill
  version: %[3]q
  fileName: %[7]s-%[3]s.zip
  agentSourceRef:
    name: %[7]s
`, descriptionVersionResource(descriptionPublicSk, descriptionVersion), descriptionPublicNS, descriptionVersion, descriptionPublicSk,
			descriptionVersionResource(descriptionPrivateSk, descriptionVersion), descriptionPrivateNS, descriptionPrivateSk))

		patchDescriptionStatus("versions", descriptionVersionResource(descriptionPublicSk, descriptionVersion), descriptionPublicNS,
			fmt.Sprintf(`{"status":{"synced":true,"syncStatus":"Synced","fileName":"described.zip","checksum":"abc123","shaSums":"abc123  described.zip","shaSumsSignature":"c2ln","agentMetadata":{"name":%q,"description":%q}}}`,
				descriptionPublicSk, descriptionText))
		patchDescriptionStatus("versions", descriptionVersionResource(descriptionPrivateSk, descriptionVersion), descriptionPrivateNS,
			fmt.Sprintf(`{"status":{"synced":true,"syncStatus":"Synced","fileName":"private.zip","checksum":"def456","shaSums":"def456  private.zip","shaSumsSignature":"c2ln","agentMetadata":{"name":%q,"description":%q}}}`,
				descriptionPrivateSk, descriptionPrivate))

		By("port-forwarding to the server")
		pfCancel = startDescriptionPortForward()
	})

	AfterAll(func() {
		if pfCancel != nil {
			pfCancel()
		}
	})

	It("returns the latest installable Version description for a public Skill in the catalog", func() {
		items := descriptionCatalog("?namespace=" + descriptionPublicNS)

		item := findDescriptionItem(items, descriptionPublicNS, descriptionPublicSk)
		Expect(item).NotTo(BeNil(), "public Skill missing from the public catalog")
		Expect(item.Kind).To(Equal("skill"))
		Expect(item.Description).To(Equal(descriptionText), "catalog must carry the full multi-line description")
	})

	It("does not list a Skill outside the public catalog or expose its description", func() {
		items := descriptionCatalog("")

		Expect(findDescriptionItem(items, descriptionPrivateNS, descriptionPrivateSk)).To(BeNil(),
			"catalog must not list a private Skill")

		status, body := descriptionGet("/opendepot/agents/v1/catalog")
		Expect(status).To(Equal(http.StatusOK))
		Expect(string(body)).NotTo(ContainSubstring(descriptionPrivate),
			"private Skill description must not appear in the catalog")
	})
})

// applyDescriptionManifest applies a manifest with kubectl.
func applyDescriptionManifest(manifest string) {
	cmd := exec.Command("kubectl", "apply", "-f", "-")
	cmd.Stdin = strings.NewReader(manifest)
	_, err := utils.Run(cmd)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "failed to apply description fixtures")
}

// patchDescriptionStatus merges a status patch into the named resource.
func patchDescriptionStatus(resource, name, ns, patch string) {
	cmd := exec.Command("kubectl", "patch", resource, name, "-n", ns,
		"--subresource=status", "--type=merge", "-p", patch)
	_, err := utils.Run(cmd)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "failed to patch status of %s/%s", resource, name)
}
