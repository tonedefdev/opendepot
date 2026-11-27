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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
)

var _ = Describe("OCIArtifact Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-oci"
		const digest = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
		const testNamespace = "default"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: testNamespace,
		}

		nameFor := func(tag string) string {
			return ociVersionName(resourceName, tag)
		}

		newOCIArtifact := func(config opendepotv1alpha1.OCIArtifactConfig, versions ...string) *opendepotv1alpha1.OCIArtifact {
			entries := make([]opendepotv1alpha1.OCIArtifactVersion, 0, len(versions))
			for _, version := range versions {
				entries = append(entries, opendepotv1alpha1.OCIArtifactVersion{Name: resourceName, Version: version})
			}

			return &opendepotv1alpha1.OCIArtifact{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: testNamespace,
				},
				Spec: opendepotv1alpha1.OCIArtifactSpec{
					OCIArtifactConfig: config,
					Versions:          entries,
				},
			}
		}

		githubConfig := func(artifactType string) opendepotv1alpha1.OCIArtifactConfig {
			return opendepotv1alpha1.OCIArtifactConfig{
				ArtifactType: artifactType,
				Github: &opendepotv1alpha1.OCIGithubSource{
					RepoOwner:    "tonedefdev",
					AssetPattern: "*.tgz",
				},
			}
		}

		mirrorConfig := func(registry string, platforms ...string) opendepotv1alpha1.OCIArtifactConfig {
			return opendepotv1alpha1.OCIArtifactConfig{
				ArtifactType: "Image",
				Mirror: &opendepotv1alpha1.OCIMirrorSource{
					Registry:   registry,
					Repository: "stefanprodan/podinfo",
				},
				Platforms: platforms,
			}
		}

		reconcileOCIArtifact := func() {
			controllerReconciler := &OCIArtifactReconciler{
				Client:            k8sClient,
				Scheme:            k8sClient.Scheme(),
				AllowedRegistries: []string{"ghcr.io"},
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
		}

		getOCIArtifact := func() *opendepotv1alpha1.OCIArtifact {
			ociArtifact := &opendepotv1alpha1.OCIArtifact{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, ociArtifact)).To(Succeed())
			return ociArtifact
		}

		getVersion := func(name string) *opendepotv1alpha1.Version {
			version := &opendepotv1alpha1.Version{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: name, Namespace: testNamespace}, version)).To(Succeed())
			return version
		}

		listVersions := func() []opendepotv1alpha1.Version {
			versionList := &opendepotv1alpha1.VersionList{}
			Expect(k8sClient.List(ctx, versionList,
				client.InNamespace(testNamespace),
				client.MatchingLabels{ociArtifactLabelKey: resourceName},
			)).To(Succeed())

			return versionList.Items
		}

		setVersionSynced := func(name string) {
			version := getVersion(name)
			version.Status.Synced = true
			version.Status.SyncStatus = "Successfully synced"
			version.Status.OCIManifest = &opendepotv1alpha1.OCIManifestStatus{
				Digest:    digest,
				MediaType: "application/vnd.oci.image.index.v1+json",
			}
			Expect(k8sClient.Status().Update(ctx, version)).To(Succeed())
		}

		AfterEach(func() {
			Expect(k8sClient.DeleteAllOf(ctx, &opendepotv1alpha1.Version{}, client.InNamespace(testNamespace),
				client.MatchingLabels{ociArtifactLabelKey: resourceName})).To(Succeed())

			ociArtifact := &opendepotv1alpha1.OCIArtifact{}
			err := k8sClient.Get(ctx, typeNamespacedName, ociArtifact)
			if err == nil {
				Expect(k8sClient.Delete(ctx, ociArtifact)).To(Succeed())
			}
		})

		It("should create a Version for each Helm version", func() {
			ociArtifact := newOCIArtifact(githubConfig("Helm"), "1.0.0", "v1.1.0")
			Expect(k8sClient.Create(ctx, ociArtifact)).To(Succeed())

			reconcileOCIArtifact()

			version := getVersion(nameFor("1.0.0"))
			Expect(version.Spec.Type).To(Equal(opendepotv1alpha1.OpenDepotOCIArtifact))
			Expect(version.Spec.Version).To(Equal("1.0.0"))
			Expect(version.Spec.OCIArtifactConfigRef).NotTo(BeNil())
			Expect(version.Spec.OCIArtifactConfigRef.ArtifactType).To(Equal("Helm"))
			Expect(version.Labels).To(HaveKeyWithValue(ociArtifactLabelKey, resourceName))
			Expect(version.OwnerReferences).To(HaveLen(1))
			Expect(version.OwnerReferences[0].Kind).To(Equal("OCIArtifact"))

			Expect(getVersion(nameFor("v1.1.0")).Spec.Version).To(Equal("v1.1.0"))
		})

		It("should create a Version for each Generic version", func() {
			ociArtifact := newOCIArtifact(githubConfig("Generic"), "2.0.0")
			Expect(k8sClient.Create(ctx, ociArtifact)).To(Succeed())

			reconcileOCIArtifact()

			version := getVersion(nameFor("2.0.0"))
			Expect(version.Spec.Type).To(Equal(opendepotv1alpha1.OpenDepotOCIArtifact))
			Expect(version.Spec.OCIArtifactConfigRef.ArtifactType).To(Equal("Generic"))
		})

		It("should create one Version per Image tag and not one per platform", func() {
			ociArtifact := newOCIArtifact(mirrorConfig("ghcr.io", "linux/amd64", "linux/arm64"), "6.5.0", "6.5.1")
			Expect(k8sClient.Create(ctx, ociArtifact)).To(Succeed())

			reconcileOCIArtifact()

			versions := listVersions()
			Expect(versions).To(HaveLen(2))
			Expect(versions).To(ConsistOf(
				HaveField("Spec.Version", "6.5.0"),
				HaveField("Spec.Version", "6.5.1"),
			))
			Expect(versions[0].Spec.OCIArtifactConfigRef.Platforms).To(ConsistOf("linux/amd64", "linux/arm64"))
		})

		It("should deny a mirror whose registry is not on the allow-list", func() {
			ociArtifact := newOCIArtifact(mirrorConfig("docker.io"), "1.0.0")
			Expect(k8sClient.Create(ctx, ociArtifact)).To(Succeed())

			reconcileOCIArtifact()

			Expect(listVersions()).To(BeEmpty())
			status := getOCIArtifact().Status
			Expect(status.Synced).To(BeFalse())
			Expect(status.SyncStatus).To(ContainSubstring("not on the allow-list"))
		})

		It("should keep the newest semantic versions within versionHistoryLimit", func() {
			config := githubConfig("Generic")
			config.VersionHistoryLimit = new(2)
			ociArtifact := newOCIArtifact(config, "1.0.0", "2.0.0", "1.2.0", "1.1.0")
			Expect(k8sClient.Create(ctx, ociArtifact)).To(Succeed())

			reconcileOCIArtifact()

			ociArtifact = getOCIArtifact()
			Expect(ociArtifact.Spec.Versions).To(HaveLen(2))
			Expect(ociArtifact.Spec.Versions[0].Version).To(Equal("2.0.0"))
			Expect(ociArtifact.Spec.Versions[1].Version).To(Equal("1.2.0"))
			Expect(ociArtifact.Status.LatestVersion).To(HaveValue(Equal("2.0.0")))
			Expect(listVersions()).To(HaveLen(2))
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: nameFor("1.0.0"), Namespace: testNamespace}, &opendepotv1alpha1.Version{})).
				To(MatchError(k8serr.IsNotFound, "version 1.0.0 should be trimmed"))
		})

		It("should keep the most recently discovered tags when tagPattern is set", func() {
			config := mirrorConfig("ghcr.io")
			config.TagPattern = new("^build-")
			config.VersionHistoryLimit = new(2)
			ociArtifact := newOCIArtifact(config, "build-1", "build-3", "build-2")
			Expect(k8sClient.Create(ctx, ociArtifact)).To(Succeed())

			reconcileOCIArtifact()

			ociArtifact = getOCIArtifact()
			Expect(ociArtifact.Spec.Versions).To(HaveLen(2))
			Expect(ociArtifact.Spec.Versions[0].Version).To(Equal("build-3"))
			Expect(ociArtifact.Spec.Versions[1].Version).To(Equal("build-2"))
			Expect(ociArtifact.Status.LatestVersion).To(HaveValue(Equal("build-2")))
			Expect(listVersions()).To(HaveLen(2))
		})

		It("should rank tags that are not semantic versions below semantic versions", func() {
			ociArtifact := newOCIArtifact(mirrorConfig("ghcr.io"), "latest", "1.0.0")
			Expect(k8sClient.Create(ctx, ociArtifact)).To(Succeed())

			reconcileOCIArtifact()

			Expect(getOCIArtifact().Status.LatestVersion).To(HaveValue(Equal("1.0.0")))
		})

		It("should garbage collect Versions that are no longer declared", func() {
			ociArtifact := newOCIArtifact(githubConfig("Generic"), "1.0.0", "2.0.0")
			Expect(k8sClient.Create(ctx, ociArtifact)).To(Succeed())

			reconcileOCIArtifact()
			Expect(listVersions()).To(HaveLen(2))

			ociArtifact = getOCIArtifact()
			ociArtifact.Spec.Versions = ociArtifact.Spec.Versions[1:]
			Expect(k8sClient.Update(ctx, ociArtifact)).To(Succeed())

			reconcileOCIArtifact()

			versions := listVersions()
			Expect(versions).To(HaveLen(1))
			Expect(versions[0].Name).To(Equal(nameFor("2.0.0")))
		})

		It("should report the digest and sync state of each Version", func() {
			ociArtifact := newOCIArtifact(mirrorConfig("ghcr.io"), "6.5.0", "6.5.1")
			Expect(k8sClient.Create(ctx, ociArtifact)).To(Succeed())

			reconcileOCIArtifact()
			Expect(getOCIArtifact().Status.Synced).To(BeFalse())
			Expect(getOCIArtifact().Status.SyncStatus).To(Equal("Syncing 0 of 2 versions"))

			setVersionSynced(nameFor("6.5.0"))
			reconcileOCIArtifact()

			status := getOCIArtifact().Status
			Expect(status.Synced).To(BeFalse())
			Expect(status.SyncStatus).To(Equal("Syncing 1 of 2 versions"))
			Expect(status.VersionRefs).To(HaveLen(2))
			Expect(status.VersionRefs["6.5.0"]).To(Equal(&opendepotv1alpha1.OCIArtifactVersion{
				Name:    nameFor("6.5.0"),
				Version: "6.5.0",
				Digest:  digest,
				Synced:  true,
			}))
			Expect(status.VersionRefs["6.5.1"].Synced).To(BeFalse())
			Expect(status.VersionRefs["6.5.1"].Digest).To(BeEmpty())

			setVersionSynced(nameFor("6.5.1"))
			reconcileOCIArtifact()

			status = getOCIArtifact().Status
			Expect(status.Synced).To(BeTrue())
			Expect(status.SyncStatus).To(Equal("Successfully synced OCIArtifact"))
			Expect(status.LatestVersion).To(HaveValue(Equal("6.5.1")))
		})

		It("should propagate ForceSync to existing Versions and reset it on the OCIArtifact", func() {
			ociArtifact := newOCIArtifact(githubConfig("Generic"), "1.0.0")
			Expect(k8sClient.Create(ctx, ociArtifact)).To(Succeed())

			reconcileOCIArtifact()
			Expect(getVersion(nameFor("1.0.0")).Spec.ForceSync).To(BeFalse())

			ociArtifact = getOCIArtifact()
			ociArtifact.Spec.ForceSync = true
			Expect(k8sClient.Update(ctx, ociArtifact)).To(Succeed())

			reconcileOCIArtifact()

			Expect(getVersion(nameFor("1.0.0")).Spec.ForceSync).To(BeTrue())
			Expect(getOCIArtifact().Spec.ForceSync).To(BeFalse())
		})

		It("should refresh the config reference on existing Versions", func() {
			ociArtifact := newOCIArtifact(githubConfig("Generic"), "1.0.0")
			Expect(k8sClient.Create(ctx, ociArtifact)).To(Succeed())

			reconcileOCIArtifact()

			ociArtifact = getOCIArtifact()
			ociArtifact.Spec.OCIArtifactConfig.Immutable = new(false)
			Expect(k8sClient.Update(ctx, ociArtifact)).To(Succeed())

			reconcileOCIArtifact()

			ref := getVersion(nameFor("1.0.0")).Spec.OCIArtifactConfigRef
			Expect(ref.Immutable).To(HaveValue(BeFalse()))
		})

		It("should not write unchanged objects on repeated reconciles", func() {
			ociArtifact := newOCIArtifact(mirrorConfig("ghcr.io"), "6.5.0")
			Expect(k8sClient.Create(ctx, ociArtifact)).To(Succeed())

			reconcileOCIArtifact()
			artifactVersion := getOCIArtifact().ResourceVersion
			versionVersion := getVersion(nameFor("6.5.0")).ResourceVersion

			reconcileOCIArtifact()

			Expect(getOCIArtifact().ResourceVersion).To(Equal(artifactVersion))
			Expect(getVersion(nameFor("6.5.0")).ResourceVersion).To(Equal(versionVersion))
		})

		Context("when OCIArtifact names collide", func() {
			const otherName = "other-oci"
			otherNamespacedName := types.NamespacedName{Name: otherName, Namespace: testNamespace}

			reconcileNamed := func(key types.NamespacedName) {
				controllerReconciler := &OCIArtifactReconciler{
					Client:            k8sClient,
					Scheme:            k8sClient.Scheme(),
					AllowedRegistries: []string{"ghcr.io"},
				}

				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key})
				Expect(err).NotTo(HaveOccurred())
			}

			AfterEach(func() {
				Expect(k8sClient.DeleteAllOf(ctx, &opendepotv1alpha1.Version{}, client.InNamespace(testNamespace),
					client.MatchingLabels{ociArtifactLabelKey: otherName})).To(Succeed())

				// Removes the Version created without labels by the collision specs.
				Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &opendepotv1alpha1.Version{
					ObjectMeta: metav1.ObjectMeta{Name: nameFor("1.0.0"), Namespace: testNamespace},
				}))).To(Succeed())

				other := &opendepotv1alpha1.OCIArtifact{}
				if err := k8sClient.Get(ctx, otherNamespacedName, other); err == nil {
					Expect(k8sClient.Delete(ctx, other)).To(Succeed())
				}
			})

			It("should keep Versions distinct when repository names normalize to the same Version name", func() {
				first := githubConfig("Helm")
				first.Name = new("charts/app")
				Expect(k8sClient.Create(ctx, newOCIArtifact(first, "1.0"))).To(Succeed())

				second := githubConfig("Helm")
				second.Name = new("charts-app")
				otherArtifact := newOCIArtifact(second, "1.0")
				otherArtifact.Name = otherName
				Expect(k8sClient.Create(ctx, otherArtifact)).To(Succeed())

				reconcileOCIArtifact()
				reconcileNamed(otherNamespacedName)

				firstVersion := getVersion(ociVersionName("charts/app", "1.0"))
				secondVersion := getVersion(ociVersionName("charts-app", "1.0"))
				Expect(firstVersion.Name).NotTo(Equal(secondVersion.Name))
				Expect(firstVersion.Labels).To(HaveKeyWithValue(ociArtifactLabelKey, resourceName))
				Expect(secondVersion.Labels).To(HaveKeyWithValue(ociArtifactLabelKey, otherName))
				Expect(firstVersion.Spec.OCIArtifactConfigRef.Name).To(HaveValue(Equal("charts/app")))
				Expect(secondVersion.Spec.OCIArtifactConfigRef.Name).To(HaveValue(Equal("charts-app")))
				Expect(getOCIArtifact().Status.SyncStatus).NotTo(ContainSubstring("collision"))
			})

			It("should skip and flag a non-OCI Version with the same name without patching it", func() {
				Expect(k8sClient.Create(ctx, &opendepotv1alpha1.Version{
					ObjectMeta: metav1.ObjectMeta{Name: nameFor("1.0.0"), Namespace: testNamespace},
					Spec: opendepotv1alpha1.VersionSpec{
						Type:    opendepotv1alpha1.OpenDepotModule,
						Version: "1.0.0",
					},
				})).To(Succeed())

				Expect(k8sClient.Create(ctx, newOCIArtifact(githubConfig("Generic"), "1.0.0"))).To(Succeed())

				reconcileOCIArtifact()

				existing := getVersion(nameFor("1.0.0"))
				Expect(existing.Spec.Type).To(Equal(opendepotv1alpha1.OpenDepotModule))
				Expect(existing.Spec.OCIArtifactConfigRef).To(BeNil())
				Expect(existing.OwnerReferences).To(BeEmpty())
				Expect(existing.Labels).To(BeEmpty())

				status := getOCIArtifact().Status
				Expect(status.Synced).To(BeFalse())
				Expect(status.SyncStatus).To(ContainSubstring("collision"))
				Expect(status.VersionRefs).To(BeEmpty())
			})

			It("should skip and flag an OCI Version with matching labels that the OCIArtifact does not control", func() {
				Expect(k8sClient.Create(ctx, &opendepotv1alpha1.Version{
					ObjectMeta: metav1.ObjectMeta{
						Name:      nameFor("1.0.0"),
						Namespace: testNamespace,
						Labels: map[string]string{
							ociArtifactLabelKey:  resourceName,
							ociNamespaceLabelKey: testNamespace,
						},
					},
					Spec: opendepotv1alpha1.VersionSpec{
						Type:    opendepotv1alpha1.OpenDepotOCIArtifact,
						Version: "1.0.0",
					},
				})).To(Succeed())

				Expect(k8sClient.Create(ctx, newOCIArtifact(githubConfig("Generic"), "1.0.0"))).To(Succeed())

				reconcileOCIArtifact()

				existing := getVersion(nameFor("1.0.0"))
				Expect(existing.Spec.OCIArtifactConfigRef).To(BeNil())
				Expect(existing.OwnerReferences).To(BeEmpty())
				Expect(getOCIArtifact().Status.SyncStatus).To(ContainSubstring("collision"))
			})

			It("should keep distinct Versions for tags that normalize to the same Version name", func() {
				Expect(k8sClient.Create(ctx, newOCIArtifact(githubConfig("Generic"), "1.2.3", "1-2-3"))).To(Succeed())

				reconcileOCIArtifact()

				dotted := getVersion(nameFor("1.2.3"))
				dashed := getVersion(nameFor("1-2-3"))
				Expect(dotted.Name).NotTo(Equal(dashed.Name))
				Expect(dotted.Spec.Version).To(Equal("1.2.3"))
				Expect(dashed.Spec.Version).To(Equal("1-2-3"))
				Expect(listVersions()).To(HaveLen(2))
			})

			It("should flag duplicate repository names and create no Versions for either OCIArtifact", func() {
				Expect(k8sClient.Create(ctx, newOCIArtifact(githubConfig("Generic"), "1.0.0"))).To(Succeed())

				duplicate := githubConfig("Generic")
				duplicate.Name = new(resourceName)
				otherArtifact := newOCIArtifact(duplicate, "1.0.0")
				otherArtifact.Name = otherName
				Expect(k8sClient.Create(ctx, otherArtifact)).To(Succeed())

				reconcileOCIArtifact()
				reconcileNamed(otherNamespacedName)

				versionList := &opendepotv1alpha1.VersionList{}
				Expect(k8sClient.List(ctx, versionList, client.InNamespace(testNamespace),
					client.MatchingLabels{ociNamespaceLabelKey: testNamespace})).To(Succeed())
				Expect(versionList.Items).To(BeEmpty())

				for _, key := range []types.NamespacedName{typeNamespacedName, otherNamespacedName} {
					artifact := &opendepotv1alpha1.OCIArtifact{}
					Expect(k8sClient.Get(ctx, key, artifact)).To(Succeed())
					Expect(artifact.Status.Synced).To(BeFalse())
					Expect(artifact.Status.SyncStatus).To(ContainSubstring("duplicate OCIArtifact repository name"))
				}
			})
		})
	})
})
