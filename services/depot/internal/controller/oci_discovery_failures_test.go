package controller

import (
	"context"
	"errors"
	"net/http"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
)

func TestOCIArtifactDiscoveryRequiresSource(t *testing.T) {
	r := &DepotReconciler{}
	name := "no-source"
	config := &opendepotv1alpha1.OCIArtifactConfig{Name: &name, ArtifactType: "Generic"}

	if _, err := r.discoverOCIArtifactVersions(context.Background(), "default", config, nil); err == nil {
		t.Fatal("expected an error when no mirror, github, or build source is set")
	}
}

func TestOCIMirrorDiscoveryRejectsInvalidRegistryConfig(t *testing.T) {
	name := "mirror"
	config := &opendepotv1alpha1.OCIArtifactConfig{
		Name:         &name,
		ArtifactType: "Generic",
		Mirror: &opendepotv1alpha1.OCIMirrorSource{
			Registry:   "registry.denied.example.com",
			Repository: "team/module",
		},
	}

	blankAllowList := &DepotReconciler{OCIAllowedRegistries: []string{" "}}
	if _, err := blankAllowList.discoverMirrorVersions(context.Background(), "default", config, nil); err == nil {
		t.Fatal("expected a blank allow-list entry to be rejected")
	}

	denied := &DepotReconciler{OCIAllowedRegistries: []string{"registry.example.com"}}
	if _, err := denied.discoverMirrorVersions(context.Background(), "default", config, nil); err == nil {
		t.Fatal("expected an unlisted mirror registry to be denied")
	}
}

func TestOCIGithubAssetDiscoveryRejectsInvalidInputs(t *testing.T) {
	originalTransport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = originalTransport })

	name := "github-assets"
	badOwner := &opendepotv1alpha1.OCIArtifactConfig{
		Name:         &name,
		ArtifactType: "Generic",
		Github: &opendepotv1alpha1.OCIGithubSource{
			RepoOwner:    "bad owner!",
			AssetPattern: "*.tgz",
		},
	}
	if _, err := (&DepotReconciler{}).discoverGithubAssetVersions(context.Background(), "default", badOwner, nil); err == nil {
		t.Fatal("expected an invalid repository owner to be rejected")
	}

	http.DefaultTransport = githubReleaseAssetsTransport{releases: []map[string]any{
		githubRelease(discoveryTagV100, false, discoveryAssetTgz),
	}}
	badPattern := &opendepotv1alpha1.OCIArtifactConfig{
		Name:         &name,
		ArtifactType: "Generic",
		Github: &opendepotv1alpha1.OCIGithubSource{
			RepoOwner:    discoveryRepoOwner,
			AssetPattern: "[",
		},
	}
	if _, err := (&DepotReconciler{}).discoverGithubAssetVersions(context.Background(), "default", badPattern, nil); err == nil {
		t.Fatal("expected a malformed assetPattern to be rejected")
	}
}

var _ = Describe("Depot Controller OCI artifact discovery failures", func() {
	const depotName = "oci-failure-depot"

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

	It("rejects a mirror credentials secret that is missing or of the wrong type", func() {
		name := "mirror-creds"
		secretName := "oci-failure-opaque"
		config := &opendepotv1alpha1.OCIArtifactConfig{
			Name:         &name,
			ArtifactType: "Generic",
			Mirror: &opendepotv1alpha1.OCIMirrorSource{
				Registry:             "registry.example.com",
				Repository:           "team/module",
				CredentialsSecretRef: &corev1.LocalObjectReference{Name: secretName},
			},
		}
		reconciler := &DepotReconciler{
			APIReader:            k8sClient,
			OCIAllowedRegistries: []string{"registry.example.com"},
		}

		_, err := reconciler.discoverOCIArtifactVersions(ctx, "default", config, nil)
		Expect(err).To(MatchError(ContainSubstring("read credentials secret")))

		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: "default"},
			Type:       corev1.SecretTypeOpaque,
		}
		Expect(k8sClient.Create(ctx, secret)).To(Succeed())
		DeferCleanup(func() {
			_ = k8sClient.Delete(ctx, secret)
		})

		_, err = reconciler.discoverOCIArtifactVersions(ctx, "default", config, nil)
		Expect(err).To(MatchError(ContainSubstring("must be of type")))
	})

	It("surfaces OCIArtifact create, read, and update failures", func() {
		http.DefaultTransport = githubReleaseAssetsTransport{releases: []map[string]any{
			githubRelease(discoveryTagV100, false, discoveryAssetTgz),
		}}

		artifactName := "failing-oci-artifact"
		config := opendepotv1alpha1.OCIArtifactConfig{
			Name:         &artifactName,
			ArtifactType: "Generic",
			Github: &opendepotv1alpha1.OCIGithubSource{
				RepoOwner:    discoveryRepoOwner,
				AssetPattern: "*.tgz",
			},
		}

		createFailing := covInterceptedClient(interceptor.Funcs{
			Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
				if _, ok := obj.(*opendepotv1alpha1.OCIArtifact); ok {
					return errors.New("oci artifact create failed")
				}

				return c.Create(ctx, obj, opts...)
			},
		})

		depot := mustGetDepot(ctx, depotKey)
		_, err := (&DepotReconciler{Client: createFailing, APIReader: k8sClient, Scheme: k8sClient.Scheme()}).reconcileOCIArtifactConfig(ctx, depot, config)
		Expect(err).To(MatchError("oci artifact create failed"))

		_, err = newDiscoveryReconciler().reconcileOCIArtifactConfig(ctx, depot, config)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			_ = k8sClient.Delete(ctx, &opendepotv1alpha1.OCIArtifact{ObjectMeta: metav1.ObjectMeta{Name: artifactName, Namespace: "default"}})
		})

		getFailing := covInterceptedClient(interceptor.Funcs{
			Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if _, ok := obj.(*opendepotv1alpha1.OCIArtifact); ok {
					return errors.New("oci artifact read failed")
				}

				return c.Get(ctx, key, obj, opts...)
			},
		})

		_, err = (&DepotReconciler{Client: getFailing, APIReader: k8sClient, Scheme: k8sClient.Scheme()}).reconcileOCIArtifactConfig(ctx, depot, config)
		Expect(err).To(MatchError("oci artifact read failed"))

		http.DefaultTransport = githubReleaseAssetsTransport{releases: []map[string]any{
			githubRelease(discoveryTagV100, false, discoveryAssetTgz),
			githubRelease(discoveryTagV110, false, discoveryAssetTgz),
		}}
		updateFailing := covInterceptedClient(interceptor.Funcs{
			Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
				if _, ok := obj.(*opendepotv1alpha1.OCIArtifact); ok {
					return errors.New("oci artifact update failed")
				}

				return c.Update(ctx, obj, opts...)
			},
		})

		_, err = (&DepotReconciler{Client: updateFailing, APIReader: k8sClient, Scheme: k8sClient.Scheme()}).reconcileOCIArtifactConfig(ctx, depot, config)
		Expect(err).To(MatchError("oci artifact update failed"))
	})
})
