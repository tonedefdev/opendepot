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
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"sync/atomic"

	"github.com/go-logr/logr"
	"github.com/google/go-github/v81/github"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/opencontainers/go-digest"
	specs "github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	"github.com/tonedefdev/opendepot/pkg/oci"
)

// ociTestNamespace holds the OCIArtifact Versions and Secrets that the OCI specs create.
const ociTestNamespace = "oci-version-sync"

// fakeOCIManifest is a manifest or index that fakeOCIRegistry serves.
type fakeOCIManifest struct {
	mediaType string
	content   []byte
}

// fakeOCIRegistry is an in-memory ociRegistry. It serves tags, manifests, indexes, blobs, and referrers, and it checks
// blob digests the way pkg/oci does. pkg/oci only dials HTTPS, so specs use this fake for the registry itself.
type fakeOCIRegistry struct {
	tags      map[string]ocispec.Descriptor
	manifests map[digest.Digest]fakeOCIManifest
	blobs     map[digest.Digest][]byte
	referrers map[digest.Digest][]ocispec.Descriptor
	fetches   map[string]int
}

func newFakeOCIRegistry() *fakeOCIRegistry {
	return &fakeOCIRegistry{
		tags:      map[string]ocispec.Descriptor{},
		manifests: map[digest.Digest]fakeOCIManifest{},
		blobs:     map[digest.Digest][]byte{},
		referrers: map[digest.Digest][]ocispec.Descriptor{},
		fetches:   map[string]int{},
	}
}

func (f *fakeOCIRegistry) addBlob(mediaType string, data []byte) ocispec.Descriptor {
	d := digest.FromBytes(data)
	f.blobs[d] = data

	return ocispec.Descriptor{MediaType: mediaType, Digest: d, Size: int64(len(data))}
}

func (f *fakeOCIRegistry) addManifest(mediaType string, v any) ocispec.Descriptor {
	raw, err := json.Marshal(v)
	Expect(err).NotTo(HaveOccurred())

	d := digest.FromBytes(raw)
	f.manifests[d] = fakeOCIManifest{mediaType: mediaType, content: raw}

	return ocispec.Descriptor{MediaType: mediaType, Digest: d, Size: int64(len(raw))}
}

// addImage stores an image manifest with one config and the given layers.
func (f *fakeOCIRegistry) addImage(config ocispec.Descriptor, artifactType string, layers ...ocispec.Descriptor) ocispec.Descriptor {
	return f.addManifest(ocispec.MediaTypeImageManifest, ocispec.Manifest{
		Versioned:    specs.Versioned{SchemaVersion: 2},
		MediaType:    ocispec.MediaTypeImageManifest,
		ArtifactType: artifactType,
		Config:       config,
		Layers:       layers,
	})
}

// addIndex stores an image index over the given manifests.
func (f *fakeOCIRegistry) addIndex(children ...ocispec.Descriptor) ocispec.Descriptor {
	return f.addManifest(ocispec.MediaTypeImageIndex, ocispec.Index{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ocispec.MediaTypeImageIndex,
		Manifests: children,
	})
}

func (f *fakeOCIRegistry) tag(name string, desc ocispec.Descriptor) {
	f.tags[name] = desc
}

func (f *fakeOCIRegistry) lookup(ref string) (ocispec.Descriptor, error) {
	if desc, ok := f.tags[ref]; ok {
		return desc, nil
	}

	if d, err := digest.Parse(ref); err == nil {
		if m, ok := f.manifests[d]; ok {
			return ocispec.Descriptor{MediaType: m.mediaType, Digest: d, Size: int64(len(m.content))}, nil
		}
	}

	return ocispec.Descriptor{}, fmt.Errorf("manifest unknown: %s", ref)
}

func (f *fakeOCIRegistry) Resolve(_ context.Context, _, reference string) (ocispec.Descriptor, error) {
	return f.lookup(reference)
}

func (f *fakeOCIRegistry) FetchManifest(
	_ context.Context,
	_, reference string,
	platforms []string,
) (*oci.ResolvedManifest, error) {
	f.fetches[reference]++

	desc, err := f.lookup(reference)
	if err != nil {
		return nil, err
	}

	stored := f.manifests[desc.Digest]
	result := &oci.ResolvedManifest{Descriptor: desc, Content: stored.content}
	if desc.MediaType != ocispec.MediaTypeImageIndex {
		var manifest ocispec.Manifest
		if err := json.Unmarshal(stored.content, &manifest); err != nil {
			return nil, err
		}

		result.Manifests = []oci.PlatformManifest{{Descriptor: desc, Content: stored.content, Manifest: manifest}}
		return result, nil
	}

	var index ocispec.Index
	if err := json.Unmarshal(stored.content, &index); err != nil {
		return nil, err
	}

	for _, child := range index.Manifests {
		if child.Platform == nil || !fakePlatformWanted(child.Platform, platforms) {
			continue
		}

		childStored := f.manifests[child.Digest]
		var manifest ocispec.Manifest
		if err := json.Unmarshal(childStored.content, &manifest); err != nil {
			return nil, err
		}

		result.Manifests = append(result.Manifests, oci.PlatformManifest{
			Descriptor: child,
			Content:    childStored.content,
			Manifest:   manifest,
		})
	}

	return result, nil
}

// fakePlatformWanted reports whether an index entry matches the requested platforms. An empty list matches every entry.
func fakePlatformWanted(p *ocispec.Platform, platforms []string) bool {
	return len(platforms) == 0 || slices.Contains(platforms, p.OS+"/"+p.Architecture)
}

func (f *fakeOCIRegistry) FetchBlob(_ context.Context, _ string, desc ocispec.Descriptor) (io.ReadCloser, error) {
	data, ok := f.blobs[desc.Digest]
	if !ok {
		return nil, fmt.Errorf("blob unknown: %s", desc.Digest)
	}

	if digest.FromBytes(data) != desc.Digest {
		return nil, fmt.Errorf("%w: blob %s", oci.ErrDigestMismatch, desc.Digest)
	}

	return io.NopCloser(bytes.NewReader(data)), nil
}

func (f *fakeOCIRegistry) Referrers(
	_ context.Context,
	_ string,
	desc ocispec.Descriptor,
	_ string,
) ([]ocispec.Descriptor, error) {
	return f.referrers[desc.Digest], nil
}

// seedImageIndex stores an Image index with linux/amd64 and linux/arm64 platform manifests and one referrer. The marker
// changes the content so that two calls produce different digests.
func seedImageIndex(f *fakeOCIRegistry, marker string) ocispec.Descriptor {
	amd64Config := f.addBlob(ocispec.MediaTypeImageConfig, []byte(`{"arch":"amd64","marker":"`+marker+`"}`))
	amd64Layer := f.addBlob(ocispec.MediaTypeImageLayerGzip, []byte("amd64 layer "+marker))
	amd64 := f.addImage(amd64Config, "", amd64Layer)
	amd64.Platform = &ocispec.Platform{OS: "linux", Architecture: "amd64"}

	arm64Config := f.addBlob(ocispec.MediaTypeImageConfig, []byte(`{"arch":"arm64","marker":"`+marker+`"}`))
	arm64Layer := f.addBlob(ocispec.MediaTypeImageLayerGzip, []byte("arm64 layer "+marker))
	arm64 := f.addImage(arm64Config, "", arm64Layer)
	arm64.Platform = &ocispec.Platform{OS: "linux", Architecture: "arm64"}

	index := f.addIndex(amd64, arm64)

	sbomConfig := f.addBlob(ocispec.MediaTypeEmptyJSON, []byte("{}"))
	sbomLayer := f.addBlob("application/vnd.example.sbom.v1+json", []byte(`{"sbom":"`+marker+`"}`))
	referrer := f.addImage(sbomConfig, "application/vnd.example.sbom.v1", sbomLayer)
	f.referrers[index.Digest] = []ocispec.Descriptor{referrer}

	return index
}

// buildChartTgz returns a gzip-compressed Helm chart archive for chart mychart version 1.0.0.
func buildChartTgz() []byte {
	files := []struct {
		name string
		body string
	}{
		{name: "mychart/Chart.yaml", body: "apiVersion: v2\nname: mychart\nversion: 1.0.0\ndescription: test chart\n"},
		{name: "mychart/values.yaml", body: "replicas: 1\n"},
	}

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, file := range files {
		Expect(tw.WriteHeader(&tar.Header{Name: file.name, Mode: 0o644, Size: int64(len(file.body)), Typeflag: tar.TypeReg})).To(Succeed())
		_, err := tw.Write([]byte(file.body))
		Expect(err).NotTo(HaveOccurred())
	}

	Expect(tw.Close()).To(Succeed())
	Expect(gz.Close()).To(Succeed())

	return buf.Bytes()
}

// seedHelmChart stores a Helm chart as an image manifest with the Helm media types and tags it as 1.0.0.
func seedHelmChart(f *fakeOCIRegistry, chart []byte) {
	config := f.addBlob(oci.HelmConfigMediaType, []byte(`{"name":"mychart","version":"1.0.0"}`))
	layer := f.addBlob(oci.HelmChartLayerMediaType, chart)
	f.tag("1.0.0", f.addImage(config, "", layer))
}

// ensureOCITestNamespace creates the namespace that the OCI specs use. It is safe to call more than once.
func ensureOCITestNamespace() {
	err := k8sClient.Create(context.Background(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ociTestNamespace}})
	if err != nil && !k8serr.IsAlreadyExists(err) {
		Expect(err).NotTo(HaveOccurred())
	}
}

// ociStorageConfig returns a filesystem storage config rooted at dir.
func ociStorageConfig(dir string) *opendepotv1alpha1.StorageConfig {
	return &opendepotv1alpha1.StorageConfig{
		FileSystem: &opendepotv1alpha1.FileSystemConfig{DirectoryPath: &dir},
	}
}

// createOCIVersion creates an OCIArtifact Version with the finalizer already set, so the first reconcile syncs it.
func createOCIVersion(name string, config *opendepotv1alpha1.OCIArtifactConfig) {
	version := &opendepotv1alpha1.Version{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  ociTestNamespace,
			Finalizers: []string{opendepotv1alpha1.OpenDepotFinalizer},
		},
		Spec: opendepotv1alpha1.VersionSpec{
			Type:                 opendepotv1alpha1.OpenDepotOCIArtifact,
			Version:              "1.0.0",
			OCIArtifactConfigRef: config,
		},
	}
	Expect(k8sClient.Create(context.Background(), version)).To(Succeed())

	DeferCleanup(func() {
		current := &opendepotv1alpha1.Version{}
		if err := k8sClient.Get(context.Background(), k8stypes.NamespacedName{Name: name, Namespace: ociTestNamespace}, current); err == nil {
			current.Finalizers = nil
			_ = k8sClient.Update(context.Background(), current)
			_ = k8sClient.Delete(context.Background(), current)
		}
	})
}

// getOCIVersion reads the current state of an OCIArtifact Version.
func getOCIVersion(name string) *opendepotv1alpha1.Version {
	current := &opendepotv1alpha1.Version{}
	Expect(k8sClient.Get(context.Background(), k8stypes.NamespacedName{Name: name, Namespace: ociTestNamespace}, current)).To(Succeed())

	return current
}

// reconcileOCI runs one reconcile of the named OCIArtifact Version.
func reconcileOCI(reconciler *VersionReconciler, name string) (ctrl.Result, error) {
	return reconciler.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: k8stypes.NamespacedName{Name: name, Namespace: ociTestNamespace},
	})
}

// newOCIReconciler returns a VersionReconciler that uses registry as the upstream.
func newOCIReconciler(registry ociRegistry) *VersionReconciler {
	return &VersionReconciler{
		Client:               k8sClient,
		Scheme:               k8sClient.Scheme(),
		Log:                  logr.Discard(),
		OCIAllowedRegistries: []string{"registry.example.com"},
		ociRegistryFor: func(_ []string, _ []byte) (ociRegistry, error) {
			return registry, nil
		},
		scanSem: make(chan struct{}, 1),
	}
}

// mirrorConfig returns a mirror OCIArtifactConfig for registry.example.com.
func mirrorConfig(artifactType, dir string) *opendepotv1alpha1.OCIArtifactConfig {
	return &opendepotv1alpha1.OCIArtifactConfig{
		ArtifactType:  artifactType,
		Mirror:        &opendepotv1alpha1.OCIMirrorSource{Registry: "registry.example.com", Repository: "team/app"},
		StorageConfig: ociStorageConfig(dir),
	}
}

// blobStoragePath returns where the filesystem store keeps a blob or manifest of a Version.
func blobStoragePath(dir, name string, kind string, d digest.Digest) string {
	return fmt.Sprintf("%s/oci/%s/%s/%s/sha256/%s", dir, ociTestNamespace, name, kind, d.Encoded())
}

var _ = Describe("OCIArtifact mirror and GitHub sync", func() {
	var (
		storageDir string
		registry   *fakeOCIRegistry
	)

	BeforeEach(func() {
		ensureOCITestNamespace()
		storageDir = GinkgoT().TempDir()
		registry = newFakeOCIRegistry()
	})

	Context("when mirroring an Image index", func() {
		It("syncs the selected platforms, referrers, and storage, and records the manifest", func() {
			index := seedImageIndex(registry, "v1")
			registry.tag("1.0.0", index)

			config := mirrorConfig("Image", storageDir)
			config.Platforms = []string{"linux/amd64"}
			createOCIVersion("image-mirror-sync", config)

			result, err := reconcileOCI(newOCIReconciler(registry), "image-mirror-sync")
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(ociDriftRequeueInterval))

			current := getOCIVersion("image-mirror-sync")
			Expect(current.Status.Synced).To(BeTrue())
			Expect(current.Status.SyncStatus).To(Equal("Synced"))
			Expect(current.Status.OCIManifest).NotTo(BeNil())
			Expect(current.Status.OCIManifest.Digest).To(Equal(index.Digest.String()))
			Expect(current.Status.OCIManifest.UpstreamDigest).To(Equal(index.Digest.String()))
			Expect(current.Status.OCIManifest.MediaType).To(Equal(ocispec.MediaTypeImageIndex))
			Expect(current.Status.OCIManifest.TagDrift).To(BeFalse())
			Expect(current.Status.OCIManifest.Platforms).To(Equal([]string{"linux/amd64"}))
			Expect(current.Status.OCIManifest.SignatureVerified).To(BeNil())

			amd64Size := int64(len(`{"arch":"amd64","marker":"v1"}`) + len("amd64 layer v1"))
			Expect(current.Status.OCIManifest.TotalSizeBytes).To(Equal(amd64Size))
			Expect(current.Status.ArchiveSizeBytes).NotTo(BeNil())
			Expect(*current.Status.ArchiveSizeBytes).To(Equal(amd64Size))

			Expect(blobStoragePath(storageDir, "image-mirror-sync", "manifests", index.Digest)).To(BeAnExistingFile())
			Expect(blobStoragePath(storageDir, "image-mirror-sync", "blobs", digest.FromBytes([]byte("amd64 layer v1")))).To(BeAnExistingFile())
			Expect(blobStoragePath(storageDir, "image-mirror-sync", "blobs", digest.FromBytes([]byte("arm64 layer v1")))).NotTo(BeAnExistingFile())
		})
	})

	Context("when the upstream tag moves", func() {
		It("records tag drift and does not re-sync when immutable is unset", func() {
			index := seedImageIndex(registry, "v1")
			registry.tag("1.0.0", index)
			createOCIVersion("drift-immutable-default", mirrorConfig("Image", storageDir))

			reconciler := newOCIReconciler(registry)
			_, err := reconcileOCI(reconciler, "drift-immutable-default")
			Expect(err).NotTo(HaveOccurred())

			moved := seedImageIndex(registry, "v2")
			registry.tag("1.0.0", moved)
			fetchesBefore := registry.fetches[moved.Digest.String()]

			result, err := reconcileOCI(reconciler, "drift-immutable-default")
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(ociDriftRequeueInterval))

			current := getOCIVersion("drift-immutable-default")
			Expect(current.Status.Synced).To(BeTrue())
			Expect(current.Status.OCIManifest.Digest).To(Equal(index.Digest.String()))
			Expect(current.Status.OCIManifest.UpstreamDigest).To(Equal(moved.Digest.String()))
			Expect(current.Status.OCIManifest.TagDrift).To(BeTrue())
			Expect(current.Status.SyncStatus).To(ContainSubstring("not re-synced"))
			Expect(registry.fetches[moved.Digest.String()]).To(Equal(fetchesBefore))
		})

		It("re-syncs to the new digest when immutable is false", func() {
			index := seedImageIndex(registry, "v1")
			registry.tag("1.0.0", index)
			config := mirrorConfig("Image", storageDir)
			config.Immutable = ptr.To(false)
			createOCIVersion("drift-mutable", config)

			reconciler := newOCIReconciler(registry)
			_, err := reconcileOCI(reconciler, "drift-mutable")
			Expect(err).NotTo(HaveOccurred())

			moved := seedImageIndex(registry, "v2")
			registry.tag("1.0.0", moved)

			_, err = reconcileOCI(reconciler, "drift-mutable")
			Expect(err).NotTo(HaveOccurred())

			current := getOCIVersion("drift-mutable")
			Expect(current.Status.OCIManifest.Digest).To(Equal(moved.Digest.String()))
			Expect(current.Status.OCIManifest.TagDrift).To(BeFalse())
			Expect(current.Status.OCIManifest.UpstreamDigest).To(Equal(moved.Digest.String()))
		})

		It("re-syncs an unchanged digest when forceSync is set and clears forceSync", func() {
			index := seedImageIndex(registry, "v1")
			registry.tag("1.0.0", index)
			createOCIVersion("force-sync", mirrorConfig("Image", storageDir))

			reconciler := newOCIReconciler(registry)
			_, err := reconcileOCI(reconciler, "force-sync")
			Expect(err).NotTo(HaveOccurred())

			fetchesBefore := registry.fetches[index.Digest.String()]

			current := getOCIVersion("force-sync")
			current.Spec.ForceSync = true
			Expect(k8sClient.Update(context.Background(), current)).To(Succeed())

			_, err = reconcileOCI(reconciler, "force-sync")
			Expect(err).NotTo(HaveOccurred())
			Expect(registry.fetches[index.Digest.String()]).To(BeNumerically(">", fetchesBefore))
			Expect(getOCIVersion("force-sync").Spec.ForceSync).To(BeFalse())
		})
	})

	Context("when the upstream registry is not on the allow-list", func() {
		DescribeTable("denies the request before any network call",
			func(allowed []string) {
				name := "denied-" + fmt.Sprint(len(allowed))
				createOCIVersion(name, mirrorConfig("Image", storageDir))

				reconciler := &VersionReconciler{
					Client:               k8sClient,
					Scheme:               k8sClient.Scheme(),
					Log:                  logr.Discard(),
					OCIAllowedRegistries: allowed,
					scanSem:              make(chan struct{}, 1),
				}

				result, err := reconcileOCI(reconciler, name)
				Expect(err).NotTo(HaveOccurred())
				Expect(result.RequeueAfter).To(BeZero())

				current := getOCIVersion(name)
				Expect(current.Status.Synced).To(BeFalse())
				Expect(current.Status.SyncStatus).To(ContainSubstring("upstream request denied"))
				Expect(current.Status.OCIManifest).To(BeNil())
			},
			Entry("when the allow-list is empty", nil),
			Entry("when only another registry is allowed", []string{"ghcr.io"}),
		)
	})

	Context("when the mirror uses a credentials Secret", func() {
		It("parses the dockerconfigjson in memory and passes it to the upstream client", func() {
			dockerConfig := []byte(`{"auths":{"registry.example.com":{"auth":"dGVzdDp0ZXN0"}}}`)
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "registry-creds", Namespace: ociTestNamespace},
				Type:       corev1.SecretTypeDockerConfigJson,
				Data:       map[string][]byte{corev1.DockerConfigJsonKey: dockerConfig},
			}
			Expect(k8sClient.Create(context.Background(), secret)).To(Succeed())
			DeferCleanup(func() { _ = k8sClient.Delete(context.Background(), secret) })

			index := seedImageIndex(registry, "creds")
			registry.tag("1.0.0", index)

			config := mirrorConfig("Image", storageDir)
			config.Mirror.CredentialsSecretRef = &corev1.LocalObjectReference{Name: "registry-creds"}
			createOCIVersion("creds-mirror", config)

			var received []byte
			reconciler := newOCIReconciler(registry)
			reconciler.ociRegistryFor = func(_ []string, creds []byte) (ociRegistry, error) {
				received = creds
				return registry, nil
			}

			_, err := reconcileOCI(reconciler, "creds-mirror")
			Expect(err).NotTo(HaveOccurred())
			Expect(received).To(Equal(dockerConfig))
			Expect(getOCIVersion("creds-mirror").Status.Synced).To(BeTrue())
		})

		It("rejects a credentials Secret that is not a dockerconfigjson Secret", func() {
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "wrong-type-creds", Namespace: ociTestNamespace},
				Type:       corev1.SecretTypeOpaque,
				Data:       map[string][]byte{corev1.DockerConfigJsonKey: []byte(`{}`)},
			}
			Expect(k8sClient.Create(context.Background(), secret)).To(Succeed())
			DeferCleanup(func() { _ = k8sClient.Delete(context.Background(), secret) })

			config := mirrorConfig("Image", storageDir)
			config.Mirror.CredentialsSecretRef = &corev1.LocalObjectReference{Name: "wrong-type-creds"}
			createOCIVersion("wrong-type-creds-mirror", config)

			_, err := reconcileOCI(newOCIReconciler(registry), "wrong-type-creds-mirror")
			Expect(err).To(MatchError(ContainSubstring("must have type")))

			current := getOCIVersion("wrong-type-creds-mirror")
			Expect(current.Status.Synced).To(BeFalse())
			Expect(current.Status.SyncStatus).To(ContainSubstring("must have type"))
		})
	})

	Context("when mirroring a Helm chart", func() {
		It("parses Chart.yaml into status.helmChart", func() {
			seedHelmChart(registry, buildChartTgz())
			createOCIVersion("helm-mirror", mirrorConfig("Helm", storageDir))

			_, err := reconcileOCI(newOCIReconciler(registry), "helm-mirror")
			Expect(err).NotTo(HaveOccurred())

			current := getOCIVersion("helm-mirror")
			Expect(current.Status.Synced).To(BeTrue())
			Expect(current.Status.HelmChart).NotTo(BeNil())
			Expect(current.Status.HelmChart.Name).To(Equal("mychart"))
			Expect(current.Status.HelmChart.Version).To(Equal("1.0.0"))
			Expect(current.Status.HelmChart.Description).To(Equal("test chart"))
		})

		It("refuses a chart that decompresses past the size limit", func() {
			var buf bytes.Buffer
			gz := gzip.NewWriter(&buf)
			zeros := make([]byte, 1<<20)
			for range 65 {
				_, err := gz.Write(zeros)
				Expect(err).NotTo(HaveOccurred())
			}
			Expect(gz.Close()).To(Succeed())

			seedHelmChart(registry, buf.Bytes())
			createOCIVersion("helm-oversized", mirrorConfig("Helm", storageDir))

			_, err := reconcileOCI(newOCIReconciler(registry), "helm-oversized")
			Expect(err).NotTo(HaveOccurred())

			current := getOCIVersion("helm-oversized")
			Expect(current.Status.Synced).To(BeFalse())
			Expect(current.Status.SyncStatus).To(ContainSubstring("exceeds the maximum decompressed size"))
			Expect(current.Status.HelmChart).To(BeNil())
		})
	})

	Context("when packaging a GitHub release asset", func() {
		var (
			githubCalls atomic.Int32
			chartBytes  []byte
			provBytes   = []byte("provenance for mychart")
		)

		BeforeEach(func() {
			chartBytes = buildChartTgz()
		})

		// startGithub serves the GitHub API from handler and returns a client that points at the server.
		startGithub := func(handler http.HandlerFunc) *github.Client {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				githubCalls.Add(1)
				handler(w, r)
			}))
			DeferCleanup(server.Close)

			client := github.NewClient(nil)
			base, err := url.Parse(server.URL + "/")
			Expect(err).NotTo(HaveOccurred())
			client.BaseURL = base

			return client
		}

		// helmRelease serves a chart-1.0.0 release with a chart, its provenance file, and an unrelated note.
		helmRelease := func() http.HandlerFunc {
			return func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/repos/acme/charts/releases/tags/chart-1.0.0":
					w.Header().Set("Content-Type", "application/json")
					_, _ = fmt.Fprint(w, `{"id":1,"tag_name":"chart-1.0.0","assets":[{"id":11,"name":"mychart-1.0.0.tgz"},{"id":12,"name":"mychart-1.0.0.tgz.prov"},{"id":13,"name":"notes.txt"}]}`)
				case "/repos/acme/charts/releases/assets/11":
					_, _ = w.Write(chartBytes)
				case "/repos/acme/charts/releases/assets/12":
					_, _ = w.Write(provBytes)
				default:
					http.NotFound(w, r)
				}
			}
		}

		githubReconciler := func(client *github.Client) *VersionReconciler {
			reconciler := newOCIReconciler(registry)
			reconciler.ociGithubClientFor = func(_ context.Context, _ *opendepotv1alpha1.Version, _ *opendepotv1alpha1.OCIArtifactConfig) (*github.Client, error) {
				return client, nil
			}

			return reconciler
		}

		githubHelmConfig := func(storage string) *opendepotv1alpha1.OCIArtifactConfig {
			repoURL := "https://github.com/acme/charts"
			return &opendepotv1alpha1.OCIArtifactConfig{
				ArtifactType: "Helm",
				Github: &opendepotv1alpha1.OCIGithubSource{
					RepoOwner:    "acme",
					RepoUrl:      &repoURL,
					TagPrefix:    ptr.To("chart-"),
					AssetPattern: "*.tgz",
				},
				StorageConfig: ociStorageConfig(storage),
			}
		}

		It("packages a Helm chart with its provenance and records the chart metadata", func() {
			createOCIVersion("github-helm", githubHelmConfig(storageDir))

			_, err := reconcileOCI(githubReconciler(startGithub(helmRelease())), "github-helm")
			Expect(err).NotTo(HaveOccurred())

			current := getOCIVersion("github-helm")
			Expect(current.Status.Synced).To(BeTrue())
			Expect(current.Status.OCIManifest).NotTo(BeNil())
			Expect(current.Status.OCIManifest.Digest).To(HavePrefix("sha256:"))
			Expect(current.Status.OCIManifest.UpstreamDigest).To(BeEmpty())
			Expect(current.Status.HelmChart).NotTo(BeNil())
			Expect(current.Status.HelmChart.Name).To(Equal("mychart"))
			Expect(current.Status.OCIManifest.TotalSizeBytes).To(BeNumerically(">=", int64(len(chartBytes)+len(provBytes))))
			Expect(*current.Status.ArchiveSizeBytes).To(Equal(current.Status.OCIManifest.TotalSizeBytes))
			Expect(blobStoragePath(storageDir, "github-helm", "manifests", digest.Digest(current.Status.OCIManifest.Digest))).To(BeAnExistingFile())
		})

		It("does not call GitHub again once the asset is synced", func() {
			createOCIVersion("github-helm-stable", githubHelmConfig(storageDir))

			reconciler := githubReconciler(startGithub(helmRelease()))
			_, err := reconcileOCI(reconciler, "github-helm-stable")
			Expect(err).NotTo(HaveOccurred())

			callsAfterSync := githubCalls.Load()
			_, err = reconcileOCI(reconciler, "github-helm-stable")
			Expect(err).NotTo(HaveOccurred())
			Expect(githubCalls.Load()).To(Equal(callsAfterSync))
		})

		It("packages a Generic asset with the configured media types", func() {
			client := startGithub(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/repos/acme/charts/releases/tags/chart-1.0.0":
					w.Header().Set("Content-Type", "application/json")
					_, _ = fmt.Fprint(w, `{"id":2,"tag_name":"chart-1.0.0","assets":[{"id":21,"name":"tool-1.0.0.bin"}]}`)
				case "/repos/acme/charts/releases/assets/21":
					_, _ = w.Write([]byte("binary payload"))
				default:
					http.NotFound(w, r)
				}
			})

			repoURL := "https://github.com/acme/charts"
			config := &opendepotv1alpha1.OCIArtifactConfig{
				ArtifactType: "Generic",
				Github: &opendepotv1alpha1.OCIGithubSource{
					RepoOwner:         "acme",
					RepoUrl:           &repoURL,
					TagPrefix:         ptr.To("chart-"),
					AssetPattern:      "*.bin",
					MediaType:         ptr.To("application/vnd.example.binary"),
					ArtifactMediaType: ptr.To("application/vnd.example.artifact.v1"),
				},
				StorageConfig: ociStorageConfig(storageDir),
			}
			createOCIVersion("github-generic", config)

			_, err := reconcileOCI(githubReconciler(client), "github-generic")
			Expect(err).NotTo(HaveOccurred())

			current := getOCIVersion("github-generic")
			Expect(current.Status.Synced).To(BeTrue())
			Expect(current.Status.OCIManifest.ArtifactType).To(Equal("application/vnd.example.artifact.v1"))
			Expect(current.Status.OCIManifest.TotalSizeBytes).To(BeNumerically(">=", int64(len("binary payload"))))
			Expect(current.Status.HelmChart).To(BeNil())
		})

		It("blocks a Generic source without a media type", func() {
			repoURL := "https://github.com/acme/charts"
			config := &opendepotv1alpha1.OCIArtifactConfig{
				ArtifactType: "Generic",
				Github: &opendepotv1alpha1.OCIGithubSource{
					RepoOwner:    "acme",
					RepoUrl:      &repoURL,
					TagPrefix:    ptr.To("chart-"),
					AssetPattern: "*.tgz",
				},
				StorageConfig: ociStorageConfig(storageDir),
			}
			createOCIVersion("github-generic-no-type", config)

			result, err := reconcileOCI(githubReconciler(startGithub(helmRelease())), "github-generic-no-type")
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(BeZero())

			current := getOCIVersion("github-generic-no-type")
			Expect(current.Status.Synced).To(BeFalse())
			Expect(current.Status.SyncStatus).To(ContainSubstring("mediaType and artifactMediaType are required"))
		})
	})
})
