package controller

import (
	"context"
	"errors"
	"strings"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
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

var _ = Describe("MCPServer Controller reconcile coverage", func() {
	const (
		resourceName = "cov-mcp-server"
		artifactName = "cov-mcp-image"
	)

	ctx := context.Background()

	key := types.NamespacedName{
		Name:      resourceName,
		Namespace: "default",
	}

	newMCPServer := func(source opendepotv1alpha1.MCPServerSource, versions []opendepotv1alpha1.MCPServerVersion, limit *int) *opendepotv1alpha1.MCPServer {
		return &opendepotv1alpha1.MCPServer{
			ObjectMeta: metav1.ObjectMeta{
				Name:      resourceName,
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

	npmSource := func(pkg string) opendepotv1alpha1.MCPServerSource {
		return opendepotv1alpha1.MCPServerSource{
			Type: "Npm",
			Npm:  &opendepotv1alpha1.MCPNpmSource{Package: pkg},
		}
	}

	imageSource := func() opendepotv1alpha1.MCPServerSource {
		return opendepotv1alpha1.MCPServerSource{
			Type:  "Image",
			Image: &opendepotv1alpha1.MCPImageSource{OCIArtifactRef: corev1.LocalObjectReference{Name: artifactName}},
		}
	}

	reconcileWith := func(c client.Client) (reconcile.Result, error) {
		controllerReconciler := &MCPServerReconciler{
			Client: c,
			Scheme: k8sClient.Scheme(),
		}

		return controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key})
	}

	getMCPServer := func() *opendepotv1alpha1.MCPServer {
		mcpServer := &opendepotv1alpha1.MCPServer{}
		Expect(k8sClient.Get(ctx, key, mcpServer)).To(Succeed())
		return mcpServer
	}

	getVersion := func(version string) *opendepotv1alpha1.Version {
		versionObject := &opendepotv1alpha1.Version{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: mcpVersionName(resourceName, version), Namespace: "default"}, versionObject)).To(Succeed())
		return versionObject
	}

	AfterEach(func() {
		Expect(k8sClient.DeleteAllOf(ctx, &opendepotv1alpha1.Version{}, client.InNamespace("default"),
			client.MatchingLabels{mcpServerLabelKey: resourceName})).To(Succeed())

		mcpServer := &opendepotv1alpha1.MCPServer{}
		if err := k8sClient.Get(ctx, key, mcpServer); err == nil {
			Expect(k8sClient.Delete(ctx, mcpServer)).To(Succeed())
		}

		artifact := &opendepotv1alpha1.OCIArtifact{}
		if err := k8sClient.Get(ctx, client.ObjectKey{Name: artifactName, Namespace: "default"}, artifact); err == nil {
			Expect(k8sClient.Delete(ctx, artifact)).To(Succeed())
		}
	})

	It("should return no error when the MCPServer does not exist", func() {
		result, err := reconcileWith(k8sClient)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(BeZero())
	})

	It("should return the error when reading the MCPServer fails", func() {
		failing := covInterceptedClient(interceptor.Funcs{
			Get: func(_ context.Context, _ client.WithWatch, _ client.ObjectKey, _ client.Object, _ ...client.GetOption) error {
				return errors.New("mcpserver read failed")
			},
		})

		_, err := reconcileWith(failing)
		Expect(err).To(MatchError("mcpserver read failed"))
	})

	It("should return the error when the Image OCIArtifact cannot be read", func() {
		Expect(k8sClient.Create(ctx, newMCPServer(imageSource(), []opendepotv1alpha1.MCPServerVersion{{Version: "1.0.0"}}, nil))).To(Succeed())

		failing := covInterceptedClient(interceptor.Funcs{
			Get: func(ctx context.Context, c client.WithWatch, objKey client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if _, ok := obj.(*opendepotv1alpha1.OCIArtifact); ok {
					return errors.New("oci artifact read failed")
				}

				return c.Get(ctx, objKey, obj, opts...)
			},
		})

		_, err := reconcileWith(failing)
		Expect(err).To(MatchError(ContainSubstring("oci artifact read failed")))
		versionErr := k8sClient.Get(ctx, client.ObjectKey{Name: mcpVersionName(resourceName, "1.0.0"), Namespace: "default"},
			&opendepotv1alpha1.Version{})
		Expect(k8serr.IsNotFound(versionErr)).To(BeTrue())
	})

	It("should return the error when the invalid Image source status cannot be written", func() {
		Expect(k8sClient.Create(ctx, newMCPServer(imageSource(), []opendepotv1alpha1.MCPServerVersion{{Version: "1.0.0"}}, nil))).To(Succeed())

		failing := covInterceptedClient(interceptor.Funcs{
			SubResourceUpdate: func(_ context.Context, _ client.Client, _ string, _ client.Object, _ ...client.SubResourceUpdateOption) error {
				return errors.New("status update failed")
			},
		})

		_, err := reconcileWith(failing)
		Expect(err).To(MatchError("status update failed"))
	})

	It("should return the error when the trimmed spec.versions cannot be written", func() {
		limit := 1
		Expect(k8sClient.Create(ctx, newMCPServer(npmSource("example-mcp-server"),
			[]opendepotv1alpha1.MCPServerVersion{{Version: "1.0.0"}, {Version: "2.0.0"}}, &limit))).To(Succeed())

		failing := covInterceptedClient(interceptor.Funcs{
			Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
				if _, ok := obj.(*opendepotv1alpha1.MCPServer); ok {
					return errors.New("spec update failed")
				}

				return c.Update(ctx, obj, opts...)
			},
		})

		_, err := reconcileWith(failing)
		Expect(err).To(MatchError("spec update failed"))
	})

	It("should return the error when the MCPServer cannot be re-read before trimming", func() {
		limit := 1
		Expect(k8sClient.Create(ctx, newMCPServer(npmSource("example-mcp-server"),
			[]opendepotv1alpha1.MCPServerVersion{{Version: "1.0.0"}, {Version: "2.0.0"}}, &limit))).To(Succeed())

		reads := 0
		failing := covInterceptedClient(interceptor.Funcs{
			Get: func(ctx context.Context, c client.WithWatch, objKey client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if _, ok := obj.(*opendepotv1alpha1.MCPServer); ok {
					reads++
					if reads > 1 {
						return errors.New("mcpserver reread failed")
					}
				}

				return c.Get(ctx, objKey, obj, opts...)
			},
		})

		_, err := reconcileWith(failing)
		Expect(err).To(MatchError("mcpserver reread failed"))
	})

	It("should return the error when an existing Version cannot be read", func() {
		Expect(k8sClient.Create(ctx, newMCPServer(npmSource("example-mcp-server"),
			[]opendepotv1alpha1.MCPServerVersion{{Version: "1.0.0"}}, nil))).To(Succeed())

		failing := covInterceptedClient(interceptor.Funcs{
			Get: func(ctx context.Context, c client.WithWatch, objKey client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if _, ok := obj.(*opendepotv1alpha1.Version); ok {
					return errors.New("version read failed")
				}

				return c.Get(ctx, objKey, obj, opts...)
			},
		})

		_, err := reconcileWith(failing)
		Expect(err).To(MatchError(ContainSubstring("version read failed")))
	})

	It("should return the error when a Version cannot be created", func() {
		Expect(k8sClient.Create(ctx, newMCPServer(npmSource("example-mcp-server"),
			[]opendepotv1alpha1.MCPServerVersion{{Version: "1.0.0"}}, nil))).To(Succeed())

		failing := covInterceptedClient(interceptor.Funcs{
			Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
				if _, ok := obj.(*opendepotv1alpha1.Version); ok {
					return errors.New("version create failed")
				}

				return c.Create(ctx, obj, opts...)
			},
		})

		_, err := reconcileWith(failing)
		Expect(err).To(MatchError(ContainSubstring("version create failed")))
	})

	It("should return the error when the owner reference cannot be set on a new Version", func() {
		owner := newMCPServer(npmSource("example-mcp-server"), nil, nil)
		config := mcpSourceRef(owner)

		_, _, err := reconcileMCPVersions(ctx, k8sClient, runtime.NewScheme(), logr.Discard(), owner, config,
			[]opendepotv1alpha1.MCPServerVersion{{Version: "1.0.0"}}, false)
		Expect(err).To(HaveOccurred())
	})

	It("should return the error when an owned Version cannot be patched for ForceSync", func() {
		Expect(k8sClient.Create(ctx, newMCPServer(npmSource("example-mcp-server"),
			[]opendepotv1alpha1.MCPServerVersion{{Version: "1.0.0"}}, nil))).To(Succeed())
		_, err := reconcileWith(k8sClient)
		Expect(err).NotTo(HaveOccurred())

		updated := getMCPServer()
		updated.Spec.ForceSync = true
		Expect(k8sClient.Update(ctx, updated)).To(Succeed())

		failing := covInterceptedClient(interceptor.Funcs{
			Patch: func(_ context.Context, _ client.WithWatch, _ client.Object, _ client.Patch, _ ...client.PatchOption) error {
				return errors.New("version patch failed")
			},
		})

		_, err = reconcileWith(failing)
		Expect(err).To(MatchError(ContainSubstring("version patch failed")))
	})

	It("should return the error when the ForceSync reset cannot be written", func() {
		mcpServer := newMCPServer(npmSource("example-mcp-server"), []opendepotv1alpha1.MCPServerVersion{{Version: "1.0.0"}}, nil)
		mcpServer.Spec.ForceSync = true
		Expect(k8sClient.Create(ctx, mcpServer)).To(Succeed())

		failing := covInterceptedClient(interceptor.Funcs{
			Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
				if _, ok := obj.(*opendepotv1alpha1.MCPServer); ok {
					return errors.New("forcesync update failed")
				}

				return c.Update(ctx, obj, opts...)
			},
		})

		_, err := reconcileWith(failing)
		Expect(err).To(MatchError("forcesync update failed"))
	})

	It("should return the error when the MCPServer status cannot be written", func() {
		Expect(k8sClient.Create(ctx, newMCPServer(npmSource("example-mcp-server"),
			[]opendepotv1alpha1.MCPServerVersion{{Version: "1.0.0"}}, nil))).To(Succeed())

		failing := covInterceptedClient(interceptor.Funcs{
			SubResourceUpdate: func(_ context.Context, _ client.Client, _ string, _ client.Object, _ ...client.SubResourceUpdateOption) error {
				return errors.New("mcp status update failed")
			},
		})

		_, err := reconcileWith(failing)
		Expect(err).To(MatchError("mcp status update failed"))
	})

	It("should return the error when owned Versions cannot be listed for removal", func() {
		Expect(k8sClient.Create(ctx, newMCPServer(npmSource("example-mcp-server"),
			[]opendepotv1alpha1.MCPServerVersion{{Version: "1.0.0"}}, nil))).To(Succeed())

		failing := covInterceptedClient(interceptor.Funcs{
			List: func(_ context.Context, _ client.WithWatch, _ client.ObjectList, _ ...client.ListOption) error {
				return errors.New("version list failed")
			},
		})

		_, err := reconcileWith(failing)
		Expect(err).To(MatchError("version list failed"))
	})

	It("should return the error when an undeclared owned Version cannot be deleted", func() {
		Expect(k8sClient.Create(ctx, newMCPServer(npmSource("example-mcp-server"),
			[]opendepotv1alpha1.MCPServerVersion{{Version: "1.0.0"}, {Version: "2.0.0"}}, nil))).To(Succeed())
		_, err := reconcileWith(k8sClient)
		Expect(err).NotTo(HaveOccurred())

		updated := getMCPServer()
		updated.Spec.Versions = []opendepotv1alpha1.MCPServerVersion{{Version: "2.0.0"}}
		Expect(k8sClient.Update(ctx, updated)).To(Succeed())

		failing := covInterceptedClient(interceptor.Funcs{
			Delete: func(_ context.Context, _ client.WithWatch, _ client.Object, _ ...client.DeleteOption) error {
				return errors.New("version delete failed")
			},
		})

		_, err = reconcileWith(failing)
		Expect(err).To(MatchError("version delete failed"))
	})

	It("should report synced Versions in the status once they have synced", func() {
		Expect(k8sClient.Create(ctx, newMCPServer(npmSource("example-mcp-server"),
			[]opendepotv1alpha1.MCPServerVersion{{Version: "1.0.0"}}, nil))).To(Succeed())
		_, err := reconcileWith(k8sClient)
		Expect(err).NotTo(HaveOccurred())

		version := getVersion("1.0.0")
		version.Status.Synced = true
		Expect(k8sClient.Status().Update(ctx, version)).To(Succeed())

		_, err = reconcileWith(k8sClient)
		Expect(err).NotTo(HaveOccurred())

		updated := getMCPServer()
		Expect(updated.Status.Synced).To(BeTrue())
		Expect(updated.Status.SyncStatus).To(Equal("Successfully synced MCPServer"))
		Expect(updated.Status.VersionRefs["1.0.0"].Synced).To(BeTrue())
	})

	It("should report an Image source without an ociArtifactRef as invalid", func() {
		r := &MCPServerReconciler{Client: k8sClient}

		msg, err := r.validateMCPImageSource(ctx, newMCPServer(opendepotv1alpha1.MCPServerSource{Type: "Image"}, nil, nil))
		Expect(err).NotTo(HaveOccurred())
		Expect(msg).To(Equal("Image source requires an ociArtifactRef"))
	})

	It("should return the error when a status read fails during the status update", func() {
		failing := covInterceptedClient(interceptor.Funcs{
			Get: func(_ context.Context, _ client.WithWatch, _ client.ObjectKey, _ client.Object, _ ...client.GetOption) error {
				return errors.New("mcpserver status read failed")
			},
		})

		r := &MCPServerReconciler{Client: failing}
		err := r.updateMCPServerStatus(ctx, key, opendepotv1alpha1.MCPServerStatus{})
		Expect(err).To(MatchError("mcpserver status read failed"))
	})

	It("should return the error when an in-sync check cannot re-read the Version", func() {
		Expect(k8sClient.Create(ctx, newMCPServer(npmSource("example-mcp-server"),
			[]opendepotv1alpha1.MCPServerVersion{{Version: "1.0.0"}}, nil))).To(Succeed())
		_, err := reconcileWith(k8sClient)
		Expect(err).NotTo(HaveOccurred())

		owner := getMCPServer()
		version := getVersion("1.0.0")
		failing := covInterceptedClient(interceptor.Funcs{
			Get: func(_ context.Context, _ client.WithWatch, _ client.ObjectKey, obj client.Object, _ ...client.GetOption) error {
				if _, ok := obj.(*opendepotv1alpha1.Version); ok {
					return errors.New("version reread failed")
				}

				return nil
			},
		})

		err = syncMCPVersion(ctx, failing, logr.Discard(), owner, version, mcpSourceRef(owner), true)
		Expect(err).To(MatchError("version reread failed"))
	})

	It("should report a collision when the Version is no longer owned after re-reading", func() {
		Expect(k8sClient.Create(ctx, newMCPServer(npmSource("example-mcp-server"),
			[]opendepotv1alpha1.MCPServerVersion{{Version: "1.0.0"}}, nil))).To(Succeed())
		_, err := reconcileWith(k8sClient)
		Expect(err).NotTo(HaveOccurred())

		owner := getMCPServer()
		version := getVersion("1.0.0")
		failing := covInterceptedClient(interceptor.Funcs{
			Get: func(_ context.Context, _ client.WithWatch, _ client.ObjectKey, obj client.Object, _ ...client.GetOption) error {
				if v, ok := obj.(*opendepotv1alpha1.Version); ok {
					v.Labels = map[string]string{mcpServerLabelKey: "other-mcp-server", mcpNamespaceLabelKey: "default"}
				}

				return nil
			},
		})

		err = syncMCPVersion(ctx, failing, logr.Discard(), owner, version, mcpSourceRef(owner), true)
		Expect(errors.Is(err, errMCPVersionCollision)).To(BeTrue())
	})

	It("should skip the patch when the Version is already in sync after re-reading", func() {
		Expect(k8sClient.Create(ctx, newMCPServer(npmSource("example-mcp-server"),
			[]opendepotv1alpha1.MCPServerVersion{{Version: "1.0.0"}}, nil))).To(Succeed())
		_, err := reconcileWith(k8sClient)
		Expect(err).NotTo(HaveOccurred())

		owner := getMCPServer()
		version := getVersion("1.0.0")
		failing := covInterceptedClient(interceptor.Funcs{
			Get: func(_ context.Context, _ client.WithWatch, _ client.ObjectKey, obj client.Object, _ ...client.GetOption) error {
				if v, ok := obj.(*opendepotv1alpha1.Version); ok {
					v.Spec.ForceSync = true
				}

				return nil
			},
			Patch: func(_ context.Context, _ client.WithWatch, _ client.Object, _ client.Patch, _ ...client.PatchOption) error {
				return errors.New("unexpected patch")
			},
		})

		err = syncMCPVersion(ctx, failing, logr.Discard(), owner, version, mcpSourceRef(owner), true)
		Expect(err).NotTo(HaveOccurred())
	})

	It("should build a bounded Version name when the config name is long", func() {
		longName := strings.Repeat("a", 80)

		name := mcpVersionName(longName, "1.0.0")
		Expect(len(name)).To(BeNumerically("<=", mcpVersionNameMaxLength))
		Expect(name).To(HavePrefix("mcp-aaaa"))
		Expect(name).NotTo(HaveSuffix("-"))
		Expect(mcpVersionName(longName, "1.0.0")).To(Equal(name))
	})

	It("should describe the sync progress of the kept versions", func() {
		Expect(mcpSyncStatus(0, 0)).To(Equal("No versions declared"))
		Expect(mcpSyncStatus(2, 2)).To(Equal("Successfully synced MCPServer"))
		Expect(mcpSyncStatus(1, 3)).To(Equal("Syncing 1 of 3 versions"))
	})

	It("should register the reconciler with a manager", func() {
		mgr, err := ctrl.NewManager(cfg, ctrl.Options{
			Scheme:  k8sClient.Scheme(),
			Metrics: metricsserver.Options{BindAddress: "0"},
		})
		Expect(err).NotTo(HaveOccurred())

		controllerReconciler := &MCPServerReconciler{
			Client: mgr.GetClient(),
			Scheme: mgr.GetScheme(),
		}
		Expect(controllerReconciler.SetupWithManager(mgr)).To(Succeed())
	})
})
