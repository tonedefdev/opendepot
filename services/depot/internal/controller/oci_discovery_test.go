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
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
)

const (
	discoveryRepoOwner     = "tonedefdev"
	discoveryArtifactImage = "Image"
	discoveryContentType   = "Content-Type"
	discoveryJSONType      = "application/json"
	discoveryDraftField    = "draft"
	discoveryAssetsKey     = "assets"
	discoveryTagNameKey    = "tag_name"
	discoveryNameKey       = "name"
	discoveryAssetTgz      = "module.tgz"
	discoveryAssetZip      = "module.zip"
	discoveryTagV100       = "v1.0.0"
	discoveryTagV110       = "v1.1.0"
	discoveryTagV120       = "v1.2.0"
	discoveryTagV200       = "v2.0.0"
	discoveryTagV210       = "v2.1.0"
	discoveryTagV300       = "v3.0.0"
	discoveryTagNotSemver  = "not-a-version"
)

// githubReleaseAssetsTransport answers GitHub release listing requests with the given releases and GitHub App
// installation token requests with a placeholder token.
type githubReleaseAssetsTransport struct {
	releases []map[string]any
}

func (t githubReleaseAssetsTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if strings.HasSuffix(req.URL.Path, "/access_tokens") {
		return &http.Response{
			StatusCode: http.StatusCreated,
			Header:     http.Header{discoveryContentType: {discoveryJSONType}},
			Body:       io.NopCloser(strings.NewReader(`{"token":"test-installation-token","expires_at":"2099-01-01T00:00:00Z"}`)),
			Request:    req,
		}, nil
	}

	body, err := json.Marshal(t.releases)
	if err != nil {
		return nil, err
	}

	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{discoveryContentType: {discoveryJSONType}},
		Body:       io.NopCloser(bytes.NewReader(body)),
		Request:    req,
	}, nil
}

// githubRelease builds a GitHub release payload with a single asset named assetName.
func githubRelease(tag string, draft bool, assetName string) map[string]any {
	return map[string]any{
		discoveryTagNameKey: tag,
		discoveryDraftField: draft,
		discoveryAssetsKey:  []map[string]string{{discoveryNameKey: assetName}},
	}
}

// newDiscoveryReconciler returns a DepotReconciler wired to the envtest client with empty allow-lists, so every
// external registry is denied unless a spec sets one.
func newDiscoveryReconciler() *DepotReconciler {
	return &DepotReconciler{
		Client:                  k8sClient,
		APIReader:               k8sClient,
		Scheme:                  k8sClient.Scheme(),
		MCPPyPIDefaultPlatforms: []string{"manylinux_2_28_x86_64"},
	}
}

// reconcileDiscoveryDepot runs one reconcile of the Depot identified by key with the given reconciler.
func reconcileDiscoveryDepot(ctx context.Context, key types.NamespacedName, reconciler *DepotReconciler) error {
	_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key})
	return err
}

// setDiscoveryDepotSpec applies mutate to the Depot identified by key and persists the result.
func setDiscoveryDepotSpec(ctx context.Context, key types.NamespacedName, mutate func(*opendepotv1alpha1.Depot)) {
	depot := &opendepotv1alpha1.Depot{}
	Expect(k8sClient.Get(ctx, key, depot)).To(Succeed())
	mutate(depot)
	Expect(k8sClient.Update(ctx, depot)).To(Succeed())
}

var _ = Describe("Depot Controller OCI artifact discovery", func() {
	const depotName = "oci-discovery-depot"

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

	It("discovers GitHub release assets filtered by tagPrefix, assetPattern, and versionConstraints", func() {
		http.DefaultTransport = githubReleaseAssetsTransport{releases: []map[string]any{
			githubRelease(discoveryTagV100, false, discoveryAssetTgz),
			githubRelease(discoveryTagV200, false, discoveryAssetZip),
			githubRelease(discoveryTagV210, false, discoveryAssetTgz),
			githubRelease(discoveryTagV300, true, discoveryAssetTgz),
			githubRelease("beta-4.0.0", false, discoveryAssetTgz),
		}}

		artifactName := "release-assets"
		tagPrefix := "v"
		setDiscoveryDepotSpec(ctx, depotKey, func(depot *opendepotv1alpha1.Depot) {
			depot.Spec.OCIArtifactConfigs = []opendepotv1alpha1.OCIArtifactConfig{{
				Name:         &artifactName,
				ArtifactType: "Generic",
				Github: &opendepotv1alpha1.OCIGithubSource{
					RepoOwner:    discoveryRepoOwner,
					TagPrefix:    &tagPrefix,
					AssetPattern: "*.tgz",
				},
				VersionConstraints: ">= 1.0.0",
			}}
		})

		Expect(reconcileDiscoveryDepot(ctx, depotKey, newDiscoveryReconciler())).To(Succeed())

		artifact := &opendepotv1alpha1.OCIArtifact{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: artifactName, Namespace: "default"}, artifact)).To(Succeed())
		DeferCleanup(func() {
			_ = k8sClient.Delete(ctx, artifact)
		})

		names := make([]string, 0, len(artifact.Spec.Versions))
		for _, v := range artifact.Spec.Versions {
			names = append(names, v.Name)
		}
		Expect(names).To(ConsistOf(discoveryTagV100, discoveryTagV210))
		Expect(artifact.Spec.Versions).To(ContainElement(opendepotv1alpha1.OCIArtifactVersion{Name: discoveryTagV210, Version: "2.1.0"}))
		Expect(metav1.IsControlledBy(artifact, mustGetDepot(ctx, depotKey))).To(BeTrue())

		depot := mustGetDepot(ctx, depotKey)
		Expect(depot.Status.OCIArtifacts).To(ConsistOf(artifactName))
	})

	It("is idempotent when the same OCI artifact config is reconciled again", func() {
		http.DefaultTransport = githubReleaseAssetsTransport{releases: []map[string]any{
			githubRelease(discoveryTagV100, false, discoveryAssetTgz),
		}}

		artifactName := "idempotent-assets"
		setDiscoveryDepotSpec(ctx, depotKey, func(depot *opendepotv1alpha1.Depot) {
			depot.Spec.OCIArtifactConfigs = []opendepotv1alpha1.OCIArtifactConfig{{
				Name:         &artifactName,
				ArtifactType: "Generic",
				Github: &opendepotv1alpha1.OCIGithubSource{
					RepoOwner:    discoveryRepoOwner,
					AssetPattern: "*.tgz",
				},
			}}
		})

		reconciler := newDiscoveryReconciler()
		Expect(reconcileDiscoveryDepot(ctx, depotKey, reconciler)).To(Succeed())

		artifact := &opendepotv1alpha1.OCIArtifact{}
		key := types.NamespacedName{Name: artifactName, Namespace: "default"}
		Expect(k8sClient.Get(ctx, key, artifact)).To(Succeed())
		DeferCleanup(func() {
			_ = k8sClient.Delete(ctx, artifact)
		})
		firstVersion := artifact.ResourceVersion

		Expect(reconcileDiscoveryDepot(ctx, depotKey, reconciler)).To(Succeed())
		Expect(k8sClient.Get(ctx, key, artifact)).To(Succeed())
		Expect(artifact.ResourceVersion).To(Equal(firstVersion))
		Expect(artifact.Spec.Versions).To(HaveLen(1))
	})

	It("denies mirror discovery when the registry is not in the allow-list", func() {
		artifactName := "denied-mirror"
		setDiscoveryDepotSpec(ctx, depotKey, func(depot *opendepotv1alpha1.Depot) {
			depot.Spec.OCIArtifactConfigs = []opendepotv1alpha1.OCIArtifactConfig{{
				Name:         &artifactName,
				ArtifactType: discoveryArtifactImage,
				Mirror: &opendepotv1alpha1.OCIMirrorSource{
					Registry:   "registry.example.com",
					Repository: "team/tool",
				},
			}}
		})

		Expect(reconcileDiscoveryDepot(ctx, depotKey, newDiscoveryReconciler())).To(HaveOccurred())

		err := k8sClient.Get(ctx, types.NamespacedName{Name: artifactName, Namespace: "default"}, &opendepotv1alpha1.OCIArtifact{})
		Expect(apierrors.IsNotFound(err)).To(BeTrue())
	})

	It("keeps synced entries and trims unsynced tail entries to the version history limit", func() {
		limit := 2
		existing := []opendepotv1alpha1.OCIArtifactVersion{
			{Name: "a", Version: "a", Synced: true},
			{Name: "b", Version: "b"},
			{Name: "c", Version: "c"},
		}
		discovered := []opendepotv1alpha1.OCIArtifactVersion{
			{Name: "c", Version: "c"},
			{Name: "d", Version: "d"},
		}

		merged := mergeOCIVersions(existing, discovered, &limit)
		names := make([]string, 0, len(merged))
		for _, v := range merged {
			names = append(names, v.Name)
		}
		Expect(names).To(Equal([]string{"a", "c"}))
	})

	It("filters tags by tagPattern and rejects patterns longer than the maximum", func() {
		pattern := `^1\.`
		re, err := compileTagPattern(&pattern)
		Expect(err).NotTo(HaveOccurred())

		matched, err := filterTagNames([]string{discoveryTagV100, "v1.1.0-rc1", discoveryTagV200, "other-1.0.0"}, "v", re, "")
		Expect(err).NotTo(HaveOccurred())
		Expect(matched).To(Equal([]string{discoveryTagV100, "v1.1.0-rc1"}))

		long := strings.Repeat("a", maxTagPatternLength+1)
		_, err = compileTagPattern(&long)
		Expect(err).To(HaveOccurred())
	})

	It("updates an existing OCIArtifact when newer GitHub releases are discovered", func() {
		http.DefaultTransport = githubReleaseAssetsTransport{releases: []map[string]any{
			githubRelease(discoveryTagV100, false, discoveryAssetTgz),
		}}

		artifactName := "updated-assets"
		setDiscoveryDepotSpec(ctx, depotKey, func(depot *opendepotv1alpha1.Depot) {
			depot.Spec.OCIArtifactConfigs = []opendepotv1alpha1.OCIArtifactConfig{{
				Name:         &artifactName,
				ArtifactType: "Generic",
				Github: &opendepotv1alpha1.OCIGithubSource{
					RepoOwner:    discoveryRepoOwner,
					AssetPattern: "*.tgz",
				},
			}}
		})

		reconciler := newDiscoveryReconciler()
		Expect(reconcileDiscoveryDepot(ctx, depotKey, reconciler)).To(Succeed())

		http.DefaultTransport = githubReleaseAssetsTransport{releases: []map[string]any{
			githubRelease(discoveryTagV100, false, discoveryAssetTgz),
			githubRelease(discoveryTagV110, false, discoveryAssetTgz),
		}}
		Expect(reconcileDiscoveryDepot(ctx, depotKey, reconciler)).To(Succeed())

		artifact := &opendepotv1alpha1.OCIArtifact{}
		key := types.NamespacedName{Name: artifactName, Namespace: "default"}
		Expect(k8sClient.Get(ctx, key, artifact)).To(Succeed())
		DeferCleanup(func() {
			_ = k8sClient.Delete(ctx, artifact)
		})

		names := make([]string, 0, len(artifact.Spec.Versions))
		for _, v := range artifact.Spec.Versions {
			names = append(names, v.Name)
		}
		Expect(names).To(ConsistOf(discoveryTagV100, discoveryTagV110))
	})

	It("fails GitHub discovery when an assetPattern is malformed", func() {
		http.DefaultTransport = githubReleaseAssetsTransport{releases: []map[string]any{
			githubRelease(discoveryTagV100, false, discoveryAssetTgz),
		}}

		artifactName := "bad-pattern"
		setDiscoveryDepotSpec(ctx, depotKey, func(depot *opendepotv1alpha1.Depot) {
			depot.Spec.OCIArtifactConfigs = []opendepotv1alpha1.OCIArtifactConfig{{
				Name:         &artifactName,
				ArtifactType: "Generic",
				Github: &opendepotv1alpha1.OCIGithubSource{
					RepoOwner:    discoveryRepoOwner,
					AssetPattern: "[",
				},
			}}
		})

		Expect(reconcileDiscoveryDepot(ctx, depotKey, newDiscoveryReconciler())).To(HaveOccurred())

		err := k8sClient.Get(ctx, types.NamespacedName{Name: artifactName, Namespace: "default"}, &opendepotv1alpha1.OCIArtifact{})
		Expect(apierrors.IsNotFound(err)).To(BeTrue())
	})

	It("rejects mirror discovery when the credentials secret is missing or has the wrong type", func() {
		artifactName := "mirror-secret"
		secretName := "mirror-credentials"
		setDiscoveryDepotSpec(ctx, depotKey, func(depot *opendepotv1alpha1.Depot) {
			depot.Spec.OCIArtifactConfigs = []opendepotv1alpha1.OCIArtifactConfig{{
				Name:         &artifactName,
				ArtifactType: discoveryArtifactImage,
				Mirror: &opendepotv1alpha1.OCIMirrorSource{
					Registry:             "registry.example.com",
					Repository:           "team/tool",
					CredentialsSecretRef: &corev1.LocalObjectReference{Name: secretName},
				},
			}}
		})

		Expect(reconcileDiscoveryDepot(ctx, depotKey, newDiscoveryReconciler())).To(HaveOccurred())

		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: "default"},
			Type:       corev1.SecretTypeOpaque,
		}
		Expect(k8sClient.Create(ctx, secret)).To(Succeed())
		DeferCleanup(func() {
			_ = k8sClient.Delete(ctx, secret)
		})

		Expect(reconcileDiscoveryDepot(ctx, depotKey, newDiscoveryReconciler())).To(HaveOccurred())
	})
})

func TestCompileTagPatternBlankAndInvalid(t *testing.T) {
	blank := "   "
	re, err := compileTagPattern(&blank)
	if err != nil || re != nil {
		t.Fatalf("blank pattern: re=%v err=%v", re, err)
	}

	invalid := "["
	if _, err := compileTagPattern(&invalid); err == nil {
		t.Fatal("expected an invalid tagPattern error")
	}
}

func TestFilterTagNamesWithVersionConstraints(t *testing.T) {
	names := []string{discoveryTagV100, discoveryTagV200, discoveryNameKey, "other-1.5.0", discoveryTagNotSemver}

	matched, err := filterTagNames(names, "v", nil, ">= 1.0.0, < 2.0.0")
	if err != nil {
		t.Fatalf("constraints: %v", err)
	}

	if len(matched) != 1 || matched[0] != discoveryTagV100 {
		t.Fatalf("constraints kept %v", matched)
	}

	if _, err := filterTagNames(names, "v", nil, "not a constraint"); err == nil {
		t.Fatal("expected an invalid constraint error")
	}
}

func TestRepoNameFromURLFallbacks(t *testing.T) {
	repoURL := "https://github.com/tonedefdev/opendepot/"
	if got := repoNameFromURL(&repoURL, nil); got != "opendepot" {
		t.Fatalf("repo from URL = %q", got)
	}

	blank := " "
	fallback := "fallback"
	if got := repoNameFromURL(&blank, &fallback); got != fallback {
		t.Fatalf("blank URL fallback = %q", got)
	}

	if got := repoNameFromURL(nil, nil); got != "" {
		t.Fatalf("nil inputs = %q", got)
	}
}

func TestOCIArtifactDiscoveryRejectsInvalidConfigs(t *testing.T) {
	r := &DepotReconciler{}
	ctx := context.Background()
	depot := &opendepotv1alpha1.Depot{}

	empty := "  "
	invalid := "Not_A_DNS_Name"
	for _, name := range []*string{nil, &empty, &invalid} {
		if _, err := r.reconcileOCIArtifactConfig(ctx, depot, opendepotv1alpha1.OCIArtifactConfig{Name: name}); err == nil {
			t.Fatalf("expected an error for name %v", name)
		}
	}

	name := "artifact"
	badPattern := "["
	if _, err := r.reconcileOCIArtifactConfig(ctx, depot, opendepotv1alpha1.OCIArtifactConfig{Name: &name, TagPattern: &badPattern}); err == nil {
		t.Fatal("expected an invalid tagPattern error")
	}

	if _, err := r.discoverOCIArtifactVersions(ctx, "default", &opendepotv1alpha1.OCIArtifactConfig{}, nil); err == nil {
		t.Fatal("expected an error when no source is set")
	}
}

func TestOCIDiscoveryDeniesInvalidRepositoriesAndRegistries(t *testing.T) {
	r := &DepotReconciler{}
	ctx := context.Background()

	mirror := &opendepotv1alpha1.OCIArtifactConfig{
		Mirror: &opendepotv1alpha1.OCIMirrorSource{Registry: "registry.example.com", Repository: "team/tool"},
	}
	if _, err := r.discoverMirrorVersions(ctx, "default", mirror, nil); err == nil {
		t.Fatal("expected mirror registry to be denied")
	}

	github := &opendepotv1alpha1.OCIArtifactConfig{
		Github: &opendepotv1alpha1.OCIGithubSource{RepoOwner: "bad owner!", AssetPattern: "*.tgz"},
	}
	if _, err := r.discoverGithubAssetVersions(ctx, "default", github, nil); err == nil {
		t.Fatal("expected invalid repository owner to be rejected")
	}
}

func TestMergeOCIVersionsWithoutLimit(t *testing.T) {
	discovered := []opendepotv1alpha1.OCIArtifactVersion{{Name: discoveryTagV100, Version: discoveryTagV100}}

	merged := mergeOCIVersions(nil, discovered, nil)
	if len(merged) != 1 || merged[0].Name != discoveryTagV100 {
		t.Fatalf("merged %v", merged)
	}
}

// mustGetDepot returns the current Depot identified by key.
func mustGetDepot(ctx context.Context, key types.NamespacedName) *opendepotv1alpha1.Depot {
	depot := &opendepotv1alpha1.Depot{}
	Expect(k8sClient.Get(ctx, key, depot)).To(Succeed())
	return depot
}
