package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
)

var _ = Describe("MCPServer Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-mcp-server"
		const otherResourceName = "test-mcp-server-b"
		const ociArtifactName = "test-mcp-image"
		const secondVersion = "2.0.0"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		otherTypeNamespacedName := types.NamespacedName{
			Name:      otherResourceName,
			Namespace: "default",
		}

		newMCPServer := func(name string, source opendepotv1alpha1.MCPServerSource, versions []opendepotv1alpha1.MCPServerVersion, limit *int) *opendepotv1alpha1.MCPServer {
			return &opendepotv1alpha1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: "default",
				},
				Spec: opendepotv1alpha1.MCPServerSpec{
					MCPServerSourceConfig: opendepotv1alpha1.MCPServerSourceConfig{
						Source:              source,
						VersionHistoryLimit: limit,
					},
					Versions: versions,
				},
			}
		}

		newNpmSource := func() opendepotv1alpha1.MCPServerSource {
			return opendepotv1alpha1.MCPServerSource{
				Type: "Npm",
				Npm:  &opendepotv1alpha1.MCPNpmSource{Package: "example-mcp-server"},
			}
		}

		newImageSource := func(ref string) opendepotv1alpha1.MCPServerSource {
			return opendepotv1alpha1.MCPServerSource{
				Type:  "Image",
				Image: &opendepotv1alpha1.MCPImageSource{OCIArtifactRef: corev1.LocalObjectReference{Name: ref}},
			}
		}

		newOCIArtifact := func(artifactType string) *opendepotv1alpha1.OCIArtifact {
			return &opendepotv1alpha1.OCIArtifact{
				ObjectMeta: metav1.ObjectMeta{
					Name:      ociArtifactName,
					Namespace: "default",
				},
				Spec: opendepotv1alpha1.OCIArtifactSpec{
					OCIArtifactConfig: opendepotv1alpha1.OCIArtifactConfig{
						ArtifactType: artifactType,
						Mirror: &opendepotv1alpha1.OCIMirrorSource{
							Registry:   "ghcr.io",
							Repository: "example/mcp-server",
						},
					},
					Versions: []opendepotv1alpha1.OCIArtifactVersion{},
				},
			}
		}

		reconcileMCPServer := func(key types.NamespacedName) reconcile.Result {
			controllerReconciler := &MCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			result, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: key,
			})
			Expect(err).NotTo(HaveOccurred())
			return result
		}

		getMCPServer := func(key types.NamespacedName) *opendepotv1alpha1.MCPServer {
			mcpServer := &opendepotv1alpha1.MCPServer{}
			Expect(k8sClient.Get(ctx, key, mcpServer)).To(Succeed())
			return mcpServer
		}

		mcpVersionKey := func(version string) client.ObjectKey {
			return client.ObjectKey{Name: mcpVersionName(resourceName, version), Namespace: "default"}
		}

		versionExists := func(key client.ObjectKey) bool {
			err := k8sClient.Get(ctx, key, &opendepotv1alpha1.Version{})
			if k8serr.IsNotFound(err) {
				return false
			}

			Expect(err).NotTo(HaveOccurred())
			return true
		}

		deleteIfExists := func(obj client.Object) {
			err := k8sClient.Delete(ctx, obj)
			if err != nil {
				Expect(k8serr.IsNotFound(err)).To(BeTrue())
			}
		}

		AfterEach(func() {
			Expect(k8sClient.DeleteAllOf(ctx, &opendepotv1alpha1.Version{}, client.InNamespace("default"),
				client.MatchingLabels{mcpServerLabelKey: resourceName})).To(Succeed())
			Expect(k8sClient.DeleteAllOf(ctx, &opendepotv1alpha1.Version{}, client.InNamespace("default"),
				client.MatchingLabels{mcpServerLabelKey: otherResourceName})).To(Succeed())

			deleteIfExists(&opendepotv1alpha1.MCPServer{ObjectMeta: metav1.ObjectMeta{Name: resourceName, Namespace: "default"}})
			deleteIfExists(&opendepotv1alpha1.MCPServer{ObjectMeta: metav1.ObjectMeta{Name: otherResourceName, Namespace: "default"}})
			deleteIfExists(&opendepotv1alpha1.OCIArtifact{ObjectMeta: metav1.ObjectMeta{Name: ociArtifactName, Namespace: "default"}})
		})

		It("should create a Version for an Npm source and set the status", func() {
			mcpServer := newMCPServer(resourceName, newNpmSource(),
				[]opendepotv1alpha1.MCPServerVersion{{Version: "1.0.0"}, {Version: "v1.1.0"}}, nil)
			Expect(k8sClient.Create(ctx, mcpServer)).To(Succeed())

			reconcileMCPServer(typeNamespacedName)

			version := &opendepotv1alpha1.Version{}
			Expect(k8sClient.Get(ctx, mcpVersionKey("1.0.0"), version)).To(Succeed())
			Expect(version.Spec.Type).To(Equal(opendepotv1alpha1.OpenDepotMCPServer))
			Expect(version.Spec.Version).To(Equal("1.0.0"))
			Expect(version.Spec.MCPServerSourceRef).NotTo(BeNil())
			Expect(*version.Spec.MCPServerSourceRef.Name).To(Equal(resourceName))
			Expect(version.Spec.MCPServerSourceRef.Source.Npm.Package).To(Equal("example-mcp-server"))
			Expect(version.Labels).To(HaveKeyWithValue(mcpServerLabelKey, resourceName))
			Expect(version.Labels).To(HaveKeyWithValue(mcpNamespaceLabelKey, "default"))
			Expect(version.OwnerReferences).To(HaveLen(1))

			Expect(k8sClient.Get(ctx, mcpVersionKey("v1.1.0"), &opendepotv1alpha1.Version{})).To(Succeed())

			updated := getMCPServer(typeNamespacedName)
			Expect(updated.Status.Synced).To(BeFalse())
			Expect(updated.Status.SyncStatus).To(Equal("Syncing 0 of 2 versions"))
			Expect(updated.Status.LatestVersion).NotTo(BeNil())
			Expect(*updated.Status.LatestVersion).To(Equal("v1.1.0"))
			Expect(updated.Status.VersionRefs).To(HaveLen(2))
			Expect(updated.Status.VersionRefs).To(HaveKey("1.0.0"))
		})

		It("should create a Version for a PyPI source", func() {
			source := opendepotv1alpha1.MCPServerSource{
				Type: "PyPI",
				PyPI: &opendepotv1alpha1.MCPPyPISource{Package: "example-mcp-server"},
			}
			mcpServer := newMCPServer(resourceName, source, []opendepotv1alpha1.MCPServerVersion{{Version: "1.0.0"}}, nil)
			Expect(k8sClient.Create(ctx, mcpServer)).To(Succeed())

			reconcileMCPServer(typeNamespacedName)

			version := &opendepotv1alpha1.Version{}
			Expect(k8sClient.Get(ctx, mcpVersionKey("1.0.0"), version)).To(Succeed())
			Expect(version.Spec.Type).To(Equal(opendepotv1alpha1.OpenDepotMCPServer))
			Expect(version.Spec.MCPServerSourceRef.Source.PyPI.Package).To(Equal("example-mcp-server"))
		})

		It("should create a Version for a Source repository", func() {
			source := opendepotv1alpha1.MCPServerSource{
				Type:   "Source",
				Source: &opendepotv1alpha1.MCPRepoSource{RepoOwner: "tonedefdev", Path: "mcp/test-mcp-server"},
			}
			mcpServer := newMCPServer(resourceName, source, []opendepotv1alpha1.MCPServerVersion{{Version: "1.0.0"}}, nil)
			Expect(k8sClient.Create(ctx, mcpServer)).To(Succeed())

			reconcileMCPServer(typeNamespacedName)

			version := &opendepotv1alpha1.Version{}
			Expect(k8sClient.Get(ctx, mcpVersionKey("1.0.0"), version)).To(Succeed())
			Expect(version.Spec.Type).To(Equal(opendepotv1alpha1.OpenDepotMCPServer))
			Expect(version.Spec.MCPServerSourceRef.Source.Source.Path).To(Equal("mcp/test-mcp-server"))
		})

		It("should create a Version for an Image source that references an Image OCIArtifact", func() {
			Expect(k8sClient.Create(ctx, newOCIArtifact("Image"))).To(Succeed())

			mcpServer := newMCPServer(resourceName, newImageSource(ociArtifactName),
				[]opendepotv1alpha1.MCPServerVersion{{Version: "1.0.0"}}, nil)
			Expect(k8sClient.Create(ctx, mcpServer)).To(Succeed())

			result := reconcileMCPServer(typeNamespacedName)
			Expect(result.RequeueAfter).To(BeZero())

			version := &opendepotv1alpha1.Version{}
			Expect(k8sClient.Get(ctx, mcpVersionKey("1.0.0"), version)).To(Succeed())
			Expect(version.Spec.Type).To(Equal(opendepotv1alpha1.OpenDepotMCPServer))
			Expect(version.Spec.MCPServerSourceRef.Source.Image.OCIArtifactRef.Name).To(Equal(ociArtifactName))

			updated := getMCPServer(typeNamespacedName)
			Expect(updated.Status.SyncStatus).To(Equal("Syncing 0 of 1 versions"))
			Expect(updated.Status.VersionRefs).To(HaveKey("1.0.0"))
		})

		It("should not create Versions and should requeue when the Image OCIArtifact is missing", func() {
			mcpServer := newMCPServer(resourceName, newImageSource(ociArtifactName),
				[]opendepotv1alpha1.MCPServerVersion{{Version: "1.0.0"}}, nil)
			Expect(k8sClient.Create(ctx, mcpServer)).To(Succeed())

			result := reconcileMCPServer(typeNamespacedName)
			Expect(result.RequeueAfter).NotTo(BeZero())
			Expect(versionExists(mcpVersionKey("1.0.0"))).To(BeFalse())

			updated := getMCPServer(typeNamespacedName)
			Expect(updated.Status.Synced).To(BeFalse())
			Expect(updated.Status.SyncStatus).To(Equal("referenced OCIArtifact test-mcp-image not found"))
		})

		It("should not create Versions when the Image OCIArtifact is not of artifactType Image", func() {
			Expect(k8sClient.Create(ctx, newOCIArtifact("Helm"))).To(Succeed())

			mcpServer := newMCPServer(resourceName, newImageSource(ociArtifactName),
				[]opendepotv1alpha1.MCPServerVersion{{Version: "1.0.0"}}, nil)
			Expect(k8sClient.Create(ctx, mcpServer)).To(Succeed())

			result := reconcileMCPServer(typeNamespacedName)
			Expect(result.RequeueAfter).NotTo(BeZero())
			Expect(versionExists(mcpVersionKey("1.0.0"))).To(BeFalse())

			updated := getMCPServer(typeNamespacedName)
			Expect(updated.Status.Synced).To(BeFalse())
			Expect(updated.Status.SyncStatus).To(Equal("referenced OCIArtifact test-mcp-image must have artifactType Image"))
		})

		It("should trim spec.versions to the version history limit", func() {
			limit := 1
			mcpServer := newMCPServer(resourceName, newNpmSource(),
				[]opendepotv1alpha1.MCPServerVersion{{Version: "1.0.0"}, {Version: secondVersion}}, &limit)
			Expect(k8sClient.Create(ctx, mcpServer)).To(Succeed())

			reconcileMCPServer(typeNamespacedName)

			updated := getMCPServer(typeNamespacedName)
			Expect(updated.Spec.Versions).To(HaveLen(1))
			Expect(updated.Spec.Versions[0].Version).To(Equal(secondVersion))
			Expect(versionExists(mcpVersionKey("1.0.0"))).To(BeFalse())
			Expect(versionExists(mcpVersionKey(secondVersion))).To(BeTrue())
			Expect(updated.Status.LatestVersion).NotTo(BeNil())
			Expect(*updated.Status.LatestVersion).To(Equal("v" + secondVersion))
		})

		It("should garbage collect owned Versions that are no longer declared", func() {
			mcpServer := newMCPServer(resourceName, newNpmSource(),
				[]opendepotv1alpha1.MCPServerVersion{{Version: "1.0.0"}, {Version: secondVersion}}, nil)
			Expect(k8sClient.Create(ctx, mcpServer)).To(Succeed())
			reconcileMCPServer(typeNamespacedName)
			Expect(versionExists(mcpVersionKey("1.0.0"))).To(BeTrue())

			updated := getMCPServer(typeNamespacedName)
			updated.Spec.Versions = []opendepotv1alpha1.MCPServerVersion{{Version: secondVersion}}
			Expect(k8sClient.Update(ctx, updated)).To(Succeed())

			reconcileMCPServer(typeNamespacedName)
			Expect(versionExists(mcpVersionKey("1.0.0"))).To(BeFalse())
			Expect(versionExists(mcpVersionKey(secondVersion))).To(BeTrue())

			updated = getMCPServer(typeNamespacedName)
			Expect(updated.Status.VersionRefs).To(HaveLen(1))
			Expect(updated.Status.VersionRefs).To(HaveKey(secondVersion))
		})

		It("should propagate ForceSync to the Versions and reset it on the MCPServer", func() {
			mcpServer := newMCPServer(resourceName, newNpmSource(),
				[]opendepotv1alpha1.MCPServerVersion{{Version: "1.0.0"}}, nil)
			Expect(k8sClient.Create(ctx, mcpServer)).To(Succeed())
			reconcileMCPServer(typeNamespacedName)

			updated := getMCPServer(typeNamespacedName)
			updated.Spec.ForceSync = true
			Expect(k8sClient.Update(ctx, updated)).To(Succeed())

			reconcileMCPServer(typeNamespacedName)

			version := &opendepotv1alpha1.Version{}
			Expect(k8sClient.Get(ctx, mcpVersionKey("1.0.0"), version)).To(Succeed())
			Expect(version.Spec.ForceSync).To(BeTrue())

			updated = getMCPServer(typeNamespacedName)
			Expect(updated.Spec.ForceSync).To(BeFalse())
		})

		It("should not patch a Version owned by another MCPServer and should report a collision", func() {
			mcpServer := newMCPServer(resourceName, newNpmSource(),
				[]opendepotv1alpha1.MCPServerVersion{{Version: "1.0.0"}}, nil)
			Expect(k8sClient.Create(ctx, mcpServer)).To(Succeed())
			reconcileMCPServer(typeNamespacedName)

			other := newMCPServer(otherResourceName, newNpmSource(),
				[]opendepotv1alpha1.MCPServerVersion{{Version: "1.0.0"}}, nil)
			other.Spec.MCPServerSourceConfig.Name = new(string)
			*other.Spec.MCPServerSourceConfig.Name = resourceName
			Expect(k8sClient.Create(ctx, other)).To(Succeed())

			reconcileMCPServer(otherTypeNamespacedName)

			version := &opendepotv1alpha1.Version{}
			Expect(k8sClient.Get(ctx, mcpVersionKey("1.0.0"), version)).To(Succeed())
			Expect(version.OwnerReferences).To(HaveLen(1))
			Expect(version.OwnerReferences[0].Name).To(Equal(resourceName))
			Expect(version.Labels).To(HaveKeyWithValue(mcpServerLabelKey, resourceName))

			updated := getMCPServer(otherTypeNamespacedName)
			Expect(updated.Status.Synced).To(BeFalse())
			Expect(updated.Status.SyncStatus).To(ContainSubstring("Version name collision"))
			Expect(updated.Status.VersionRefs).To(BeEmpty())
		})

		It("should not patch or delete a non-MCP Version that holds a declared name", func() {
			existing := &opendepotv1alpha1.Version{
				ObjectMeta: metav1.ObjectMeta{
					Name:      mcpVersionName(resourceName, "1.0.0"),
					Namespace: "default",
					Labels:    map[string]string{mcpServerLabelKey: resourceName, mcpNamespaceLabelKey: "default"},
				},
				Spec: opendepotv1alpha1.VersionSpec{
					Type:    opendepotv1alpha1.OpenDepotAgent,
					Version: "1.0.0",
				},
			}
			Expect(k8sClient.Create(ctx, existing)).To(Succeed())

			mcpServer := newMCPServer(resourceName, newNpmSource(),
				[]opendepotv1alpha1.MCPServerVersion{{Version: "1.0.0"}}, nil)
			Expect(k8sClient.Create(ctx, mcpServer)).To(Succeed())

			reconcileMCPServer(typeNamespacedName)

			version := &opendepotv1alpha1.Version{}
			Expect(k8sClient.Get(ctx, mcpVersionKey("1.0.0"), version)).To(Succeed())
			Expect(version.Spec.Type).To(Equal(opendepotv1alpha1.OpenDepotAgent))
			Expect(version.Spec.MCPServerSourceRef).To(BeNil())
			Expect(version.OwnerReferences).To(BeEmpty())

			updated := getMCPServer(typeNamespacedName)
			Expect(updated.Status.Synced).To(BeFalse())
			Expect(updated.Status.SyncStatus).To(ContainSubstring("Version name collision"))
		})

		It("should not delete a labeled Version that the MCPServer does not own", func() {
			stale := &opendepotv1alpha1.Version{
				ObjectMeta: metav1.ObjectMeta{
					Name:      mcpVersionName(resourceName, "9.9.9"),
					Namespace: "default",
					Labels:    map[string]string{mcpServerLabelKey: resourceName, mcpNamespaceLabelKey: "default"},
				},
				Spec: opendepotv1alpha1.VersionSpec{
					Type:    opendepotv1alpha1.OpenDepotAgent,
					Version: "9.9.9",
				},
			}
			Expect(k8sClient.Create(ctx, stale)).To(Succeed())

			mcpServer := newMCPServer(resourceName, newNpmSource(),
				[]opendepotv1alpha1.MCPServerVersion{{Version: "1.0.0"}}, nil)
			Expect(k8sClient.Create(ctx, mcpServer)).To(Succeed())

			reconcileMCPServer(typeNamespacedName)

			Expect(versionExists(mcpVersionKey("9.9.9"))).To(BeTrue())
		})
	})
})
