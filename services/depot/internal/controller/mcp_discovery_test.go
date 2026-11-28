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

package controller

import (
	"context"
	"net/http"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
)

var _ = Describe("Depot Controller MCP server discovery", func() {
	const depotName = "mcp-discovery-depot"

	ctx := context.Background()

	depotKey := types.NamespacedName{
		Name:      depotName,
		Namespace: "default",
	}

	var originalTransport http.RoundTripper

	BeforeEach(func() {
		originalTransport = http.DefaultTransport
		Expect(k8sClient.Create(ctx, &opendepotv1alpha1.Depot{
			ObjectMeta: metav1.ObjectMeta{
				Name:      depotName,
				Namespace: "default",
			},
		})).To(Succeed())
	})

	AfterEach(func() {
		http.DefaultTransport = originalTransport

		depot := &opendepotv1alpha1.Depot{}
		Expect(k8sClient.Get(ctx, depotKey, depot)).To(Succeed())
		Expect(k8sClient.Delete(ctx, depot)).To(Succeed())
	})

	It("discovers Git tags for a Source server that satisfy versionConstraints", func() {
		http.DefaultTransport = githubTagsTransport{tags: []string{"v0.9.0", discoveryTagV100, discoveryTagV120}}

		serverName := "source-server"
		setDiscoveryDepotSpec(ctx, depotKey, func(depot *opendepotv1alpha1.Depot) {
			depot.Spec.MCPServerConfigs = []opendepotv1alpha1.MCPServerSourceConfig{{
				Name: &serverName,
				Source: opendepotv1alpha1.MCPServerSource{
					Type: "Source",
					Source: &opendepotv1alpha1.MCPRepoSource{
						RepoOwner: discoveryRepoOwner,
						Path:      "servers/source",
					},
				},
				VersionConstraints: ">= 1.0.0",
			}}
		})

		Expect(reconcileDiscoveryDepot(ctx, depotKey, newDiscoveryReconciler())).To(Succeed())

		server := &opendepotv1alpha1.MCPServer{}
		key := types.NamespacedName{Name: serverName, Namespace: "default"}
		Expect(k8sClient.Get(ctx, key, server)).To(Succeed())
		DeferCleanup(func() {
			_ = k8sClient.Delete(ctx, server)
		})

		names := make([]string, 0, len(server.Spec.Versions))
		for _, v := range server.Spec.Versions {
			names = append(names, v.Name)
		}
		Expect(names).To(ConsistOf(discoveryTagV100, discoveryTagV120))
		Expect(mustGetDepot(ctx, depotKey).Status.MCPServers).To(ConsistOf(serverName))
	})

	It("follows the synced tags of the referenced OCIArtifact for an Image server", func() {
		artifactName := "server-image"
		artifact := &opendepotv1alpha1.OCIArtifact{
			ObjectMeta: metav1.ObjectMeta{
				Name:      artifactName,
				Namespace: "default",
			},
			Spec: opendepotv1alpha1.OCIArtifactSpec{
				OCIArtifactConfig: opendepotv1alpha1.OCIArtifactConfig{
					Name:         &artifactName,
					ArtifactType: discoveryArtifactImage,
					Build: &opendepotv1alpha1.OCIBuildSource{
						RepoOwner: discoveryRepoOwner,
					},
				},
				Versions: []opendepotv1alpha1.OCIArtifactVersion{{
					Name:    discoveryTagV100,
					Version: discoveryTagV100,
				}},
			},
		}
		Expect(k8sClient.Create(ctx, artifact)).To(Succeed())
		DeferCleanup(func() {
			_ = k8sClient.Delete(ctx, artifact)
		})

		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: artifactName, Namespace: "default"}, artifact)).To(Succeed())
		artifact.Status = opendepotv1alpha1.OCIArtifactStatus{
			VersionRefs: map[string]*opendepotv1alpha1.OCIArtifactVersion{
				discoveryTagV100: {Name: discoveryTagV100, Version: discoveryTagV100, Synced: true},
				discoveryTagV110: {Name: discoveryTagV110, Version: discoveryTagV110, Synced: true},
				discoveryTagV200: {Name: discoveryTagV200, Version: discoveryTagV200, Synced: false},
			},
		}
		Expect(k8sClient.Status().Update(ctx, artifact)).To(Succeed())

		serverName := "image-server"
		setDiscoveryDepotSpec(ctx, depotKey, func(depot *opendepotv1alpha1.Depot) {
			depot.Spec.MCPServerConfigs = []opendepotv1alpha1.MCPServerSourceConfig{{
				Name: &serverName,
				Source: opendepotv1alpha1.MCPServerSource{
					Type: mcpSourceTypeImage,
					Image: &opendepotv1alpha1.MCPImageSource{
						OCIArtifactRef: corev1.LocalObjectReference{Name: artifactName},
					},
				},
			}}
		})

		Expect(reconcileDiscoveryDepot(ctx, depotKey, newDiscoveryReconciler())).To(Succeed())

		server := &opendepotv1alpha1.MCPServer{}
		key := types.NamespacedName{Name: serverName, Namespace: "default"}
		Expect(k8sClient.Get(ctx, key, server)).To(Succeed())
		DeferCleanup(func() {
			_ = k8sClient.Delete(ctx, server)
		})

		names := make([]string, 0, len(server.Spec.Versions))
		for _, v := range server.Spec.Versions {
			names = append(names, v.Name)
		}
		Expect(names).To(ConsistOf(discoveryTagV100, discoveryTagV110))
		Expect(mustGetDepot(ctx, depotKey).Status.MCPServers).To(ConsistOf(serverName))
	})

	It("is idempotent when the same MCP server config is reconciled again", func() {
		http.DefaultTransport = githubTagsTransport{tags: []string{discoveryTagV100}}

		serverName := "idempotent-server"
		setDiscoveryDepotSpec(ctx, depotKey, func(depot *opendepotv1alpha1.Depot) {
			depot.Spec.MCPServerConfigs = []opendepotv1alpha1.MCPServerSourceConfig{{
				Name: &serverName,
				Source: opendepotv1alpha1.MCPServerSource{
					Type: "Source",
					Source: &opendepotv1alpha1.MCPRepoSource{
						RepoOwner: discoveryRepoOwner,
						Path:      "servers/idempotent",
					},
				},
			}}
		})

		reconciler := newDiscoveryReconciler()
		Expect(reconcileDiscoveryDepot(ctx, depotKey, reconciler)).To(Succeed())

		server := &opendepotv1alpha1.MCPServer{}
		key := types.NamespacedName{Name: serverName, Namespace: "default"}
		Expect(k8sClient.Get(ctx, key, server)).To(Succeed())
		DeferCleanup(func() {
			_ = k8sClient.Delete(ctx, server)
		})
		firstVersion := server.ResourceVersion

		Expect(reconcileDiscoveryDepot(ctx, depotKey, reconciler)).To(Succeed())
		Expect(k8sClient.Get(ctx, key, server)).To(Succeed())
		Expect(server.ResourceVersion).To(Equal(firstVersion))
	})

	It("denies npm discovery when the registry is not in the npm allow-list", func() {
		serverName := "denied-npm"
		setDiscoveryDepotSpec(ctx, depotKey, func(depot *opendepotv1alpha1.Depot) {
			depot.Spec.MCPServerConfigs = []opendepotv1alpha1.MCPServerSourceConfig{{
				Name: &serverName,
				Source: opendepotv1alpha1.MCPServerSource{
					Type: "Npm",
					Npm: &opendepotv1alpha1.MCPNpmSource{
						Package: "@modelcontextprotocol/server-filesystem",
					},
				},
			}}
		})

		Expect(reconcileDiscoveryDepot(ctx, depotKey, newDiscoveryReconciler())).To(HaveOccurred())

		err := k8sClient.Get(ctx, types.NamespacedName{Name: serverName, Namespace: "default"}, &opendepotv1alpha1.MCPServer{})
		Expect(apierrors.IsNotFound(err)).To(BeTrue())
	})

	It("denies PyPI discovery when the index is not in the PyPI allow-list", func() {
		serverName := "denied-pypi"
		setDiscoveryDepotSpec(ctx, depotKey, func(depot *opendepotv1alpha1.Depot) {
			depot.Spec.MCPServerConfigs = []opendepotv1alpha1.MCPServerSourceConfig{{
				Name: &serverName,
				Source: opendepotv1alpha1.MCPServerSource{
					Type: "PyPI",
					PyPI: &opendepotv1alpha1.MCPPyPISource{
						Package: "mcp-server-fetch",
					},
				},
			}}
		})

		Expect(reconcileDiscoveryDepot(ctx, depotKey, newDiscoveryReconciler())).To(HaveOccurred())

		err := k8sClient.Get(ctx, types.NamespacedName{Name: serverName, Namespace: "default"}, &opendepotv1alpha1.MCPServer{})
		Expect(apierrors.IsNotFound(err)).To(BeTrue())
	})

	It("updates an existing MCPServer when newer tags are discovered", func() {
		http.DefaultTransport = githubTagsTransport{tags: []string{discoveryTagV100}}

		serverName := "updated-server"
		setDiscoveryDepotSpec(ctx, depotKey, func(depot *opendepotv1alpha1.Depot) {
			depot.Spec.MCPServerConfigs = []opendepotv1alpha1.MCPServerSourceConfig{{
				Name: &serverName,
				Source: opendepotv1alpha1.MCPServerSource{
					Type: mcpSourceTypeSource,
					Source: &opendepotv1alpha1.MCPRepoSource{
						RepoOwner: discoveryRepoOwner,
						Path:      "servers/updated",
					},
				},
			}}
		})

		reconciler := newDiscoveryReconciler()
		Expect(reconcileDiscoveryDepot(ctx, depotKey, reconciler)).To(Succeed())

		http.DefaultTransport = githubTagsTransport{tags: []string{discoveryTagV100, discoveryTagV110}}
		Expect(reconcileDiscoveryDepot(ctx, depotKey, reconciler)).To(Succeed())

		server := &opendepotv1alpha1.MCPServer{}
		key := types.NamespacedName{Name: serverName, Namespace: "default"}
		Expect(k8sClient.Get(ctx, key, server)).To(Succeed())
		DeferCleanup(func() {
			_ = k8sClient.Delete(ctx, server)
		})

		names := make([]string, 0, len(server.Spec.Versions))
		for _, v := range server.Spec.Versions {
			names = append(names, v.Name)
		}
		Expect(names).To(ConsistOf(discoveryTagV100, discoveryTagV110))
		Expect(metav1.IsControlledBy(server, mustGetDepot(ctx, depotKey))).To(BeTrue())
	})

	It("fails to discover an Image server whose OCIArtifact does not exist", func() {
		serverName := "missing-image-server"
		setDiscoveryDepotSpec(ctx, depotKey, func(depot *opendepotv1alpha1.Depot) {
			depot.Spec.MCPServerConfigs = []opendepotv1alpha1.MCPServerSourceConfig{{
				Name: &serverName,
				Source: opendepotv1alpha1.MCPServerSource{
					Type: mcpSourceTypeImage,
					Image: &opendepotv1alpha1.MCPImageSource{
						OCIArtifactRef: corev1.LocalObjectReference{Name: "does-not-exist"},
					},
				},
			}}
		})

		Expect(reconcileDiscoveryDepot(ctx, depotKey, newDiscoveryReconciler())).To(HaveOccurred())

		err := k8sClient.Get(ctx, types.NamespacedName{Name: serverName, Namespace: "default"}, &opendepotv1alpha1.MCPServer{})
		Expect(apierrors.IsNotFound(err)).To(BeTrue())
	})
})

func TestMCPServerDiscoveryRejectsInvalidNames(t *testing.T) {
	r := &DepotReconciler{}
	depot := &opendepotv1alpha1.Depot{}

	empty := "  "
	invalid := "Not_A_DNS_Name"
	for _, name := range []*string{nil, &empty, &invalid} {
		if _, err := r.reconcileMCPServerConfig(context.Background(), depot, opendepotv1alpha1.MCPServerSourceConfig{Name: name}); err == nil {
			t.Fatalf("expected an error for name %v", name)
		}
	}
}

func TestMCPServerDiscoveryRequiresMatchingSource(t *testing.T) {
	r := &DepotReconciler{}
	ctx := context.Background()
	name := "server"

	cases := map[string]opendepotv1alpha1.MCPServerSourceConfig{
		"unsupported type": {Name: &name, Source: opendepotv1alpha1.MCPServerSource{Type: "Bogus"}},
		"npm":              {Name: &name, Source: opendepotv1alpha1.MCPServerSource{Type: mcpSourceTypeNpm}},
		"pypi":             {Name: &name, Source: opendepotv1alpha1.MCPServerSource{Type: mcpSourceTypePyPI}},
		"source":           {Name: &name, Source: opendepotv1alpha1.MCPServerSource{Type: mcpSourceTypeSource}},
		"image":            {Name: &name, Source: opendepotv1alpha1.MCPServerSource{Type: mcpSourceTypeImage}},
	}

	for test, config := range cases {
		if _, err := r.discoverMCPServerVersions(ctx, "default", &config); err == nil {
			t.Fatalf("%s: expected an error", test)
		}
	}
}

func TestMCPServerDiscoveryDeniesUnlistedRegistries(t *testing.T) {
	r := &DepotReconciler{}
	ctx := context.Background()
	registry := "registry.example.com"
	index := "pypi.example.com"
	python := "3.12"

	npm := &opendepotv1alpha1.MCPServerSourceConfig{
		Source: opendepotv1alpha1.MCPServerSource{
			Type: mcpSourceTypeNpm,
			Npm:  &opendepotv1alpha1.MCPNpmSource{Package: "server", Registry: &registry},
		},
	}
	if _, err := r.discoverNpmVersions(ctx, npm); err == nil {
		t.Fatal("expected npm registry to be denied")
	}

	pypi := &opendepotv1alpha1.MCPServerSourceConfig{
		Source: opendepotv1alpha1.MCPServerSource{
			Type: mcpSourceTypePyPI,
			PyPI: &opendepotv1alpha1.MCPPyPISource{
				Package:       "server",
				Index:         &index,
				Platforms:     []string{"manylinux_2_28_x86_64"},
				PythonVersion: &python,
			},
		},
	}
	if _, err := r.discoverPyPIVersions(ctx, pypi); err == nil {
		t.Fatal("expected PyPI index to be denied")
	}
}

func TestMCPServerDiscoveryMissingSourceConfigs(t *testing.T) {
	r := &DepotReconciler{}
	ctx := context.Background()
	config := &opendepotv1alpha1.MCPServerSourceConfig{}

	if _, err := r.discoverNpmVersions(ctx, config); err == nil {
		t.Fatal("expected npm source error")
	}

	if _, err := r.discoverPyPIVersions(ctx, config); err == nil {
		t.Fatal("expected pypi source error")
	}

	if _, err := r.discoverMCPSourceVersions(ctx, "default", config); err == nil {
		t.Fatal("expected source error")
	}

	if _, err := r.discoverImageVersions(ctx, "default", config); err == nil {
		t.Fatal("expected image source error")
	}
}

func TestFilterSemverVersions(t *testing.T) {
	versions := []string{"v1.2.0", "not-a-version", "2.0.0", "v0.9.0"}

	all, err := filterSemverVersions(versions, " ")
	if err != nil {
		t.Fatalf("blank constraints: %v", err)
	}

	if len(all) != 3 || all[0] != "v1.2.0" {
		t.Fatalf("blank constraints kept %v", all)
	}

	matched, err := filterSemverVersions(versions, ">= 1.0.0, < 2.0.0")
	if err != nil {
		t.Fatalf("constraints: %v", err)
	}

	if len(matched) != 1 || matched[0] != "v1.2.0" {
		t.Fatalf("constraints kept %v", matched)
	}

	if _, err := filterSemverVersions(versions, "not a constraint"); err == nil {
		t.Fatal("expected an invalid constraint error")
	}
}

func TestWithSchemeDefaultsToHTTPS(t *testing.T) {
	cases := map[string]string{
		"registry.example.com":            "https://registry.example.com",
		"  https://registry.example.com ": "https://registry.example.com",
		"http://localhost:8080":           "http://localhost:8080",
	}

	for in, want := range cases {
		if got := withScheme(in); got != want {
			t.Fatalf("withScheme(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMergeMCPVersionsWithoutLimit(t *testing.T) {
	discovered := []opendepotv1alpha1.MCPServerVersion{{Name: discoveryTagV100, Version: discoveryTagV100}}

	merged := mergeMCPVersions(nil, discovered, nil)
	if len(merged) != 1 || merged[0].Name != discoveryTagV100 {
		t.Fatalf("merged %v", merged)
	}
}
