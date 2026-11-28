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
})
