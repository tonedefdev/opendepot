package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	"github.com/tonedefdev/opendepot/pkg/oci"
	"github.com/tonedefdev/opendepot/pkg/storage"
)

const (
	ociTestNamespace         = "team"
	ociTestArtifact          = "app"
	ociTestTagOne            = "1.0.0"
	ociTestTagTwo            = "2.0.0"
	ociTestTagThree          = "3.0.0"
	ociTestGroup             = "platform"
	ociTestOther             = "other"
	ociTestTagMiddle         = "1.10.0"
	ociTestRegion            = "us-east-1"
	ociTestSecretRepo        = "secret"
	ociTestBindingExpression = `"platform" in groups`
	ociTestSyncStatus        = "Synced"
	ociTestChallenge         = `Basic realm="opendepot"`
	ociTestBucket            = "archives"
	ociTestAMD64             = "amd64"
	ociTestARM64             = "arm64"
	ociTestOS                = "linux"
	ociTestRangeHeader       = "Range"
	ociTestSchemaKey         = "schemaVersion"
	ociTestMediaTypeKey      = "mediaType"
	ociTestAuthorization     = "Authorization"
	ociTestLocation          = "Location"
	ociTestContentLength     = "Content-Length"
	ociTestContentType       = "Content-Type"
	ociTestDigestHeader      = "Docker-Content-Digest"
	ociTestBasicPrefix       = "Basic "
	ociTestBearerPrefix      = "Bearer "
)

// ociTestCluster is a fake Kubernetes API that serves the OCIArtifacts, Versions, and GroupBindings of the test
// namespace. Each test edits the lists under the lock.
type ociTestCluster struct {
	mu        sync.Mutex
	artifacts []opendepotv1alpha1.OCIArtifact
	versions  []opendepotv1alpha1.Version
	bindings  []opendepotv1alpha1.GroupBinding
}

func (c *ociTestCluster) serve(w http.ResponseWriter, r *http.Request) {
	c.mu.Lock()
	defer c.mu.Unlock()

	var payload any
	switch {
	case strings.HasSuffix(r.URL.Path, "/ociartifacts"):
		payload = opendepotv1alpha1.OCIArtifactList{Items: c.artifacts}
	case strings.HasSuffix(r.URL.Path, "/versions"):
		payload = opendepotv1alpha1.VersionList{Items: c.versions}
	case strings.HasSuffix(r.URL.Path, "/groupbindings"):
		payload = opendepotv1alpha1.GroupBindingList{Items: c.bindings}
	case strings.HasSuffix(r.URL.Path, "/securitygroupbindings"):
		payload = opendepotv1alpha1.SecurityGroupBindingList{}
	default:
		http.NotFound(w, r)

		return
	}

	body, err := json.Marshal(payload)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)

		return
	}

	w.Header().Set(ociTestContentType, "application/json")
	_, _ = w.Write(body)
}

// ociTestEnv holds the fake cluster, a filesystem store under a temporary root, and the globals that the tests set.
type ociTestEnv struct {
	root    string
	store   *oci.Store
	cluster *ociTestCluster
}

// newOCITestEnv sets the auth globals and the filesystem mount path. With anonymous set, requests need no credentials.
// Otherwise OIDC mode is on, and bearer tokens for the platform group are accepted.
func newOCITestEnv(t *testing.T, anonymous bool) *ociTestEnv {
	t.Helper()

	wave6AuthGlobals(t)
	if anonymous {
		enabled := true
		opendepotAnonymousAuth = &enabled
	}

	root := t.TempDir()
	originalMount := opendepotFilesystemMountPath
	opendepotFilesystemMountPath = &root
	t.Cleanup(func() { opendepotFilesystemMountPath = originalMount })

	store, err := oci.NewStore(&storage.FileSystem{}, root)
	if err != nil {
		t.Fatalf("oci.NewStore() error = %v", err)
	}

	cluster := &ociTestCluster{}
	api := httptest.NewServer(http.HandlerFunc(cluster.serve))
	t.Cleanup(api.Close)

	originalInCluster := inClusterConfig
	inClusterConfig = func() (*rest.Config, error) {
		return &rest.Config{Host: api.URL}, nil
	}
	t.Cleanup(func() { inClusterConfig = originalInCluster })

	return &ociTestEnv{root: root, store: store, cluster: cluster}
}

func (env *ociTestEnv) storageConfig() *opendepotv1alpha1.StorageConfig {
	return &opendepotv1alpha1.StorageConfig{
		FileSystem: &opendepotv1alpha1.FileSystemConfig{DirectoryPath: &env.root},
	}
}

// addArtifact adds an OCIArtifact resource. An empty repository leaves the config name unset, so the resource name is
// the repository path.
func (env *ociTestEnv) addArtifact(resourceName, repository, artifactType string) {
	env.cluster.mu.Lock()
	defer env.cluster.mu.Unlock()

	config := opendepotv1alpha1.OCIArtifactConfig{ArtifactType: artifactType}
	if repository != "" {
		config.Name = &repository
	}

	env.cluster.artifacts = append(env.cluster.artifacts, opendepotv1alpha1.OCIArtifact{
		ObjectMeta: metav1.ObjectMeta{Name: resourceName, Namespace: ociTestNamespace, ResourceVersion: "1"},
		Spec:       opendepotv1alpha1.OCIArtifactSpec{OCIArtifactConfig: config},
	})
}

// addBinding adds a GroupBinding that matches the platform group and grants the artifact patterns.
func (env *ociTestEnv) addBinding(patterns ...string) {
	env.cluster.mu.Lock()
	defer env.cluster.mu.Unlock()

	env.cluster.bindings = append(env.cluster.bindings, opendepotv1alpha1.GroupBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "ocireader", ResourceVersion: "7"},
		Spec: opendepotv1alpha1.GroupBindingSpec{
			Expression:           ociTestBindingExpression,
			OCIArtifactResources: patterns,
		},
	})
}

// addVersion adds a Version to the fake cluster.
func (env *ociTestEnv) addVersion(version *opendepotv1alpha1.Version) {
	env.cluster.mu.Lock()
	defer env.cluster.mu.Unlock()

	env.cluster.versions = append(env.cluster.versions, *version)
}

// updateVersion edits the Version with the given resource name in the fake cluster.
func (env *ociTestEnv) updateVersion(name string, edit func(*opendepotv1alpha1.Version)) {
	env.cluster.mu.Lock()
	defer env.cluster.mu.Unlock()

	for i := range env.cluster.versions {
		if env.cluster.versions[i].Name == name {
			edit(&env.cluster.versions[i])

			return
		}
	}
}

// ociTestVersionName returns the Version resource name for an artifact and tag, as the OCI controller names it.
func ociTestVersionName(artifact, tag string) string {
	return "oci-" + artifact + "-" + strings.ReplaceAll(tag, ".", "-")
}

// newOCITestVersion returns a synced Version for a tag of an artifact. Callers set its manifest.
func (env *ociTestEnv) newOCITestVersion(artifact, tag, artifactType string) *opendepotv1alpha1.Version {
	return &opendepotv1alpha1.Version{
		ObjectMeta: metav1.ObjectMeta{
			Name:              ociTestVersionName(artifact, tag),
			Namespace:         ociTestNamespace,
			Labels:            map[string]string{ociArtifactLabelKey: artifact},
			CreationTimestamp: metav1.NewTime(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)),
			ResourceVersion:   "1",
		},
		Spec: opendepotv1alpha1.VersionSpec{
			Type:    opendepotv1alpha1.OpenDepotOCIArtifact,
			Version: tag,
			OCIArtifactConfigRef: &opendepotv1alpha1.OCIArtifactConfig{
				ArtifactType:  artifactType,
				StorageConfig: env.storageConfig(),
			},
		},
		Status: opendepotv1alpha1.VersionStatus{Synced: true, SyncStatus: ociTestSyncStatus},
	}
}

// putBlob stores data as a blob of the repository and returns its descriptor.
func (env *ociTestEnv) putBlob(t *testing.T, repository, mediaType string, data []byte) ocispec.Descriptor {
	t.Helper()

	d := digest.FromBytes(data)
	if err := env.store.PutBlob(context.Background(), ociTestNamespace, repository, d, bytes.NewReader(data)); err != nil {
		t.Fatalf("PutBlob() error = %v", err)
	}

	return ocispec.Descriptor{MediaType: mediaType, Digest: d, Size: int64(len(data))}
}

// putManifest stores a manifest or index body and returns its digest.
func (env *ociTestEnv) putManifest(t *testing.T, repository string, body []byte) digest.Digest {
	t.Helper()

	d, err := env.store.PutManifest(context.Background(), ociTestNamespace, repository, body)
	if err != nil {
		t.Fatalf("PutManifest() error = %v", err)
	}

	return d
}

// publishImage stores an image manifest with one config and the given layers, and adds its Version. It returns the
// Version and the manifest digest.
func (env *ociTestEnv) publishImage(
	t *testing.T,
	artifact, tag string,
	layers ...[]byte,
) (*opendepotv1alpha1.Version, digest.Digest) {
	t.Helper()

	repository := ociTestVersionName(artifact, tag)
	config := env.putBlob(t, repository, ocispec.MediaTypeImageConfig,
		[]byte(`{"architecture":"`+ociTestAMD64+`","os":"`+ociTestOS+`"}`))

	layerDescriptors := make([]ocispec.Descriptor, 0, len(layers))
	for _, layer := range layers {
		layerDescriptors = append(layerDescriptors, env.putBlob(t, repository, ocispec.MediaTypeImageLayerGzip, layer))
	}

	manifest, err := json.Marshal(map[string]any{
		ociTestSchemaKey:    2,
		ociTestMediaTypeKey: ocispec.MediaTypeImageManifest,
		"config":            config,
		"layers":            layerDescriptors,
	})
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}

	manifestDigest := env.putManifest(t, repository, manifest)

	version := env.newOCITestVersion(artifact, tag, "Image")
	version.Status.OCIManifest = &opendepotv1alpha1.OCIManifestStatus{
		Digest:    manifestDigest.String(),
		MediaType: ocispec.MediaTypeImageManifest,
	}
	env.addVersion(version)

	return version, manifestDigest
}

// publishIndex stores a two-platform index with one layer per platform, and adds its Version. It returns the Version,
// the index digest, and the digests of the platform manifests followed by their layers.
func (env *ociTestEnv) publishIndex(
	t *testing.T,
	artifact, tag string,
) (*opendepotv1alpha1.Version, digest.Digest, []digest.Digest) {
	t.Helper()

	repository := ociTestVersionName(artifact, tag)
	platforms := []string{ociTestAMD64, ociTestARM64}
	children := make([]ocispec.Descriptor, 0, len(platforms))
	manifestDigests := make([]digest.Digest, 0, len(platforms))
	layerDigests := make([]digest.Digest, 0, len(platforms))
	for _, arch := range platforms {
		config := env.putBlob(t, repository, ocispec.MediaTypeImageConfig, []byte(`{"architecture":"`+arch+`"}`))
		layer := env.putBlob(t, repository, ocispec.MediaTypeImageLayerGzip, []byte("layer-"+arch))
		body, err := json.Marshal(map[string]any{
			ociTestSchemaKey:    2,
			ociTestMediaTypeKey: ocispec.MediaTypeImageManifest,
			"config":            config,
			"layers":            []ocispec.Descriptor{layer},
		})
		if err != nil {
			t.Fatalf("marshal platform manifest: %v", err)
		}

		childDigest := env.putManifest(t, repository, body)
		manifestDigests = append(manifestDigests, childDigest)
		layerDigests = append(layerDigests, layer.Digest)
		children = append(children, ocispec.Descriptor{
			MediaType: ocispec.MediaTypeImageManifest,
			Digest:    childDigest,
			Size:      int64(len(body)),
			Platform:  &ocispec.Platform{OS: ociTestOS, Architecture: arch},
		})
	}

	index, err := json.Marshal(map[string]any{
		ociTestSchemaKey:    2,
		ociTestMediaTypeKey: ocispec.MediaTypeImageIndex,
		"manifests":         children,
	})
	if err != nil {
		t.Fatalf("marshal index: %v", err)
	}

	indexDigest := env.putManifest(t, repository, index)

	version := env.newOCITestVersion(artifact, tag, "Image")
	version.Status.OCIManifest = &opendepotv1alpha1.OCIManifestStatus{
		Digest:    indexDigest.String(),
		MediaType: ocispec.MediaTypeImageIndex,
		Platforms: platforms,
	}
	env.addVersion(version)

	return version, indexDigest, append(manifestDigests, layerDigests...)
}

// ociTestRequest sends a request to the OCI handler and returns the recorded response.
func ociTestRequest(method, target string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	rec := httptest.NewRecorder()
	OCIDistributionHandler(rec, req)

	return rec
}

// ociTestBearer returns the Authorization header for a bearer token in the platform group.
func ociTestBearer(t *testing.T) map[string]string {
	t.Helper()

	return map[string]string{ociTestAuthorization: ociTestBearerPrefix + agentTestBearerToken(t, []string{ociTestGroup})}
}

// ociTestErrorCode returns the code of the first entry of an OCI error body.
func ociTestErrorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()

	var envelope ociErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode OCI error body %q: %v", rec.Body.String(), err)
	}

	if len(envelope.Errors) == 0 {
		t.Fatalf("OCI error body has no errors: %q", rec.Body.String())
	}

	return envelope.Errors[0].Code
}

// assertOCIStatus fails the test unless the response has the wanted status and, when code is set, the OCI error code.
func assertOCIStatus(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) {
	t.Helper()

	if rec.Code != status {
		t.Fatalf("status = %d, want %d: %s", rec.Code, status, rec.Body.String())
	}

	if code != "" {
		if got := ociTestErrorCode(t, rec); got != code {
			t.Fatalf("error code = %q, want %q", got, code)
		}
	}
}

// digestOfEmpty returns the digest of empty content, which no fixture stores.
func digestOfEmpty() string {
	return digest.FromBytes(nil).String()
}

// filepathForBlob returns the on-disk path of a blob that the filesystem store wrote for a repository.
func filepathForBlob(root, repository string, d digest.Digest) string {
	return filepath.Join(root, "oci", ociTestNamespace, repository, "blobs", "sha256", d.Encoded())
}

// overwriteFile replaces the content of a stored object, to simulate corruption on disk.
func overwriteFile(name string, data []byte) error {
	return os.WriteFile(name, data, 0o600)
}

// readAllOCIBody reads a recorded response body for assertions on streamed content.
func readAllOCIBody(t *testing.T, body io.Reader) []byte {
	t.Helper()

	data, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	return data
}

// newOCITestS3 serves the objects that the filesystem store wrote under root, by key, from a fake S3 endpoint, and
// points the S3 client at it through the environment. With tamperBlobs set, blob objects report a checksum that does
// not match their content, so the presign checksum gate must refuse them.
func newOCITestS3(t *testing.T, root string, tamperBlobs bool) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.URL.Path, "/"+ociTestBucket+"/")
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(key)))
		if err != nil {
			w.Header().Set(ociTestContentType, "application/xml")
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?><Error><Code>NoSuchKey</Code>`+
				`<Message>The specified key does not exist.</Message></Error>`)

			return
		}

		sum := sha256.Sum256(data)
		if tamperBlobs && strings.Contains(key, "/blobs/sha256/") {
			sum = sha256.Sum256([]byte("tampered"))
		}

		w.Header().Set("x-amz-checksum-sha256", base64.StdEncoding.EncodeToString(sum[:]))
		_, _ = w.Write(data)
	}))
	t.Cleanup(server.Close)

	t.Setenv("AWS_ENDPOINT_URL_S3", server.URL)
	t.Setenv("AWS_ACCESS_KEY_ID", "oci-test-access-key")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "oci-test-secret-key")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(t.TempDir(), "config"))
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(t.TempDir(), "credentials"))
	t.Setenv("OPENDEPOT_S3_USE_PATH_STYLE", "true")

	return server
}

func TestOCIDistributionAPIVersion(t *testing.T) {
	t.Run("anonymous mode needs no credentials", func(t *testing.T) {
		newOCITestEnv(t, true)

		rec := ociTestRequest(http.MethodGet, "/v2/", nil)
		assertOCIStatus(t, rec, http.StatusOK, "")

		if got := rec.Header().Get(ociAPIVersionHeader); got != ociAPIVersion {
			t.Fatalf("%s = %q, want %q", ociAPIVersionHeader, got, ociAPIVersion)
		}

		if rec.Body.String() != "{}" {
			t.Fatalf("body = %q, want {}", rec.Body.String())
		}
	})

	t.Run("head with anonymous mode succeeds without a body", func(t *testing.T) {
		newOCITestEnv(t, true)

		rec := ociTestRequest(http.MethodHead, "/v2", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
	})

	t.Run("OIDC mode without credentials challenges", func(t *testing.T) {
		newOCITestEnv(t, false)

		rec := ociTestRequest(http.MethodGet, "/v2/", nil)
		assertOCIStatus(t, rec, http.StatusUnauthorized, ociCodeUnauthorized)

		if challenge := rec.Header().Get("WWW-Authenticate"); challenge != ociTestChallenge {
			t.Fatalf("WWW-Authenticate = %q", challenge)
		}
	})

	t.Run("OIDC mode with a bearer token succeeds", func(t *testing.T) {
		env := newOCITestEnv(t, false)
		env.addBinding("*")

		rec := ociTestRequest(http.MethodGet, "/v2/", ociTestBearer(t))
		assertOCIStatus(t, rec, http.StatusOK, "")
	})
}

func TestOCIDistributionWriteMethodsAreUnsupported(t *testing.T) {
	newOCITestEnv(t, false)

	targets := []string{
		"/v2/",
		"/v2/" + ociTestNamespace + "/" + ociTestArtifact + "/manifests/" + ociTestTagOne,
		"/v2/" + ociTestNamespace + "/" + ociTestArtifact + "/blobs/" + digestOfEmpty(),
	}

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		for _, target := range targets {
			rec := ociTestRequest(method, target, nil)
			assertOCIStatus(t, rec, http.StatusMethodNotAllowed, ociCodeUnsupported)
		}
	}
}

func TestOCIDistributionRejectsInvalidNames(t *testing.T) {
	newOCITestEnv(t, true)

	base := "/v2/" + ociTestNamespace + "/" + ociTestArtifact
	sha512Digest := "sha512:" + strings.Repeat("a", 128)

	cases := []struct {
		name   string
		target string
		status int
		code   string
	}{
		{
			name:   "uppercase namespace",
			target: "/v2/Team/" + ociTestArtifact + "/manifests/" + ociTestTagOne,
			status: http.StatusBadRequest,
			code:   ociCodeNameInvalid,
		},
		{
			name:   "traversal in name",
			target: "/v2/" + ociTestNamespace + "/../" + ociTestArtifact + "/manifests/" + ociTestTagOne,
			status: http.StatusBadRequest,
			code:   ociCodeNameInvalid,
		},
		{
			name:   "invalid digest",
			target: base + "/blobs/sha256:xyz",
			status: http.StatusBadRequest,
			code:   ociCodeDigestInvalid,
		},
		{
			name:   "wrong digest algorithm",
			target: base + "/manifests/" + sha512Digest,
			status: http.StatusBadRequest,
			code:   ociCodeDigestInvalid,
		},
		{
			name:   "invalid tag",
			target: base + "/manifests/-bad",
			status: http.StatusBadRequest,
			code:   ociCodeTagInvalid,
		},
		{
			name:   "unknown route",
			target: base + "/unknown",
			status: http.StatusNotFound,
			code:   ociCodeNameUnknown,
		},
		{
			name:   "too few segments",
			target: "/v2/" + ociTestNamespace,
			status: http.StatusNotFound,
			code:   ociCodeNameUnknown,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := ociTestRequest(http.MethodGet, tc.target, nil)
			assertOCIStatus(t, rec, tc.status, tc.code)
		})
	}
}

func TestOCIDistributionManifestsByTagAndDigest(t *testing.T) {
	env := newOCITestEnv(t, true)
	env.addArtifact(ociTestArtifact, "", "Image")
	_, manifestDigest := env.publishImage(t, ociTestArtifact, ociTestTagOne, []byte("layer"))
	manifestPath := "/v2/" + ociTestNamespace + "/" + ociTestArtifact + "/manifests/"

	t.Run("GET by tag returns the manifest with its digest and type", func(t *testing.T) {
		rec := ociTestRequest(http.MethodGet, manifestPath+ociTestTagOne, nil)
		assertOCIStatus(t, rec, http.StatusOK, "")

		if got := rec.Header().Get(ociTestDigestHeader); got != manifestDigest.String() {
			t.Fatalf("%s = %q, want %q", ociTestDigestHeader, got, manifestDigest)
		}

		if got := rec.Header().Get(ociTestContentType); got != ocispec.MediaTypeImageManifest {
			t.Fatalf("%s = %q", ociTestContentType, got)
		}

		if got := rec.Header().Get(ociTestContentLength); got != fmt.Sprint(rec.Body.Len()) {
			t.Fatalf("%s = %q, body length %d", ociTestContentLength, got, rec.Body.Len())
		}

		if digest.FromBytes(rec.Body.Bytes()) != manifestDigest {
			t.Fatalf("manifest body does not hash to %s", manifestDigest)
		}
	})

	t.Run("HEAD by tag returns headers without a body", func(t *testing.T) {
		rec := ociTestRequest(http.MethodHead, manifestPath+ociTestTagOne, nil)
		assertOCIStatus(t, rec, http.StatusOK, "")

		if rec.Body.Len() != 0 {
			t.Fatalf("HEAD body length = %d, want 0", rec.Body.Len())
		}

		if got := rec.Header().Get(ociTestDigestHeader); got != manifestDigest.String() {
			t.Fatalf("%s = %q", ociTestDigestHeader, got)
		}
	})

	t.Run("GET by digest returns the same manifest", func(t *testing.T) {
		rec := ociTestRequest(http.MethodGet, manifestPath+manifestDigest.String(), nil)
		assertOCIStatus(t, rec, http.StatusOK, "")
	})

	t.Run("unknown tag is MANIFEST_UNKNOWN", func(t *testing.T) {
		rec := ociTestRequest(http.MethodGet, manifestPath+"9.9.9", nil)
		assertOCIStatus(t, rec, http.StatusNotFound, ociCodeManifestUnknown)
	})

	t.Run("unknown digest is MANIFEST_UNKNOWN", func(t *testing.T) {
		rec := ociTestRequest(http.MethodGet, manifestPath+digestOfEmpty(), nil)
		assertOCIStatus(t, rec, http.StatusNotFound, ociCodeManifestUnknown)
	})

	t.Run("unknown artifact is NAME_UNKNOWN", func(t *testing.T) {
		rec := ociTestRequest(http.MethodGet, "/v2/"+ociTestNamespace+"/missing/manifests/"+ociTestTagOne, nil)
		assertOCIStatus(t, rec, http.StatusNotFound, ociCodeNameUnknown)
	})
}

func TestOCIDistributionServesPlatformManifestsOfAnIndex(t *testing.T) {
	env := newOCITestEnv(t, true)
	env.addArtifact(ociTestArtifact, "", "Image")
	_, indexDigest, stored := env.publishIndex(t, ociTestArtifact, ociTestTagTwo)
	manifestPath := "/v2/" + ociTestNamespace + "/" + ociTestArtifact + "/manifests/"

	rec := ociTestRequest(http.MethodGet, manifestPath+ociTestTagTwo, nil)
	assertOCIStatus(t, rec, http.StatusOK, "")

	if got := rec.Header().Get(ociTestContentType); got != ocispec.MediaTypeImageIndex {
		t.Fatalf("index %s = %q", ociTestContentType, got)
	}

	if got := rec.Header().Get(ociTestDigestHeader); got != indexDigest.String() {
		t.Fatalf("index %s = %q, want %q", ociTestDigestHeader, got, indexDigest)
	}

	child := stored[0]
	rec = ociTestRequest(http.MethodGet, manifestPath+child.String(), nil)
	assertOCIStatus(t, rec, http.StatusOK, "")

	if got := rec.Header().Get(ociTestDigestHeader); got != child.String() {
		t.Fatalf("child %s = %q, want %q", ociTestDigestHeader, got, child)
	}
}

func TestOCIDistributionBlobs(t *testing.T) {
	env := newOCITestEnv(t, true)
	env.addArtifact(ociTestArtifact, "", "Image")
	layer := []byte("0123456789-layer-content")
	version, _ := env.publishImage(t, ociTestArtifact, ociTestTagOne, layer)
	layerDigest := digest.FromBytes(layer)
	blobPath := "/v2/" + ociTestNamespace + "/" + ociTestArtifact + "/blobs/" + layerDigest.String()

	t.Run("GET streams the blob with digest and length", func(t *testing.T) {
		rec := ociTestRequest(http.MethodGet, blobPath, nil)
		assertOCIStatus(t, rec, http.StatusOK, "")

		if !bytes.Equal(rec.Body.Bytes(), layer) {
			t.Fatalf("blob body = %q, want %q", rec.Body.String(), layer)
		}

		if got := rec.Header().Get(ociTestContentLength); got != fmt.Sprint(len(layer)) {
			t.Fatalf("%s = %q, want %d", ociTestContentLength, got, len(layer))
		}

		if got := rec.Header().Get(ociTestDigestHeader); got != layerDigest.String() {
			t.Fatalf("%s = %q", ociTestDigestHeader, got)
		}
	})

	t.Run("HEAD returns the length without a body", func(t *testing.T) {
		rec := ociTestRequest(http.MethodHead, blobPath, nil)
		assertOCIStatus(t, rec, http.StatusOK, "")

		if rec.Body.Len() != 0 {
			t.Fatalf("HEAD body length = %d, want 0", rec.Body.Len())
		}

		if got := rec.Header().Get(ociTestContentLength); got != fmt.Sprint(len(layer)) {
			t.Fatalf("HEAD %s = %q, want %d", ociTestContentLength, got, len(layer))
		}
	})

	t.Run("Range returns the requested bytes", func(t *testing.T) {
		rec := ociTestRequest(http.MethodGet, blobPath, map[string]string{ociTestRangeHeader: "bytes=2-5"})
		if rec.Code != http.StatusPartialContent {
			t.Fatalf("status = %d, want 206: %s", rec.Code, rec.Body.String())
		}

		if rec.Body.String() != "2345" {
			t.Fatalf("range body = %q, want 2345", rec.Body.String())
		}

		if got := rec.Header().Get("Content-Range"); got != fmt.Sprintf("bytes 2-5/%d", len(layer)) {
			t.Fatalf("Content-Range = %q", got)
		}
	})

	t.Run("HEAD with Range reports the partial length without a body", func(t *testing.T) {
		rec := ociTestRequest(http.MethodHead, blobPath, map[string]string{ociTestRangeHeader: "bytes=0-3"})
		if rec.Code != http.StatusPartialContent {
			t.Fatalf("status = %d, want 206", rec.Code)
		}

		if rec.Body.Len() != 0 {
			t.Fatalf("HEAD body length = %d, want 0", rec.Body.Len())
		}

		if got := rec.Header().Get(ociTestContentLength); got != "4" {
			t.Fatalf("%s = %q, want 4", ociTestContentLength, got)
		}
	})

	t.Run("unknown digest is BLOB_UNKNOWN", func(t *testing.T) {
		rec := ociTestRequest(http.MethodGet, "/v2/"+ociTestNamespace+"/"+ociTestArtifact+"/blobs/"+digestOfEmpty(), nil)
		assertOCIStatus(t, rec, http.StatusNotFound, ociCodeBlobUnknown)
	})

	t.Run("blocked Version stops serving its blobs", func(t *testing.T) {
		env.updateVersion(version.Name, func(v *opendepotv1alpha1.Version) {
			v.Status.JevAssessment = &opendepotv1alpha1.JevAssessment{Blocked: true}
		})

		rec := ociTestRequest(http.MethodGet, blobPath, nil)
		assertOCIStatus(t, rec, http.StatusNotFound, ociCodeBlobUnknown)

		env.updateVersion(version.Name, func(v *opendepotv1alpha1.Version) { v.Status.JevAssessment = nil })
		rec = ociTestRequest(http.MethodGet, blobPath, nil)
		assertOCIStatus(t, rec, http.StatusOK, "")
	})
}

func TestOCIDistributionBlobsStayWithinTheirArtifact(t *testing.T) {
	env := newOCITestEnv(t, true)
	env.addArtifact(ociTestArtifact, "", "Image")
	env.addArtifact(ociTestOther, "", "Image")
	secret := []byte("only-in-other")
	env.publishImage(t, ociTestOther, ociTestTagOne, secret)
	env.publishImage(t, ociTestArtifact, ociTestTagOne, []byte("app-layer"))

	secretDigest := digest.FromBytes(secret).String()
	rec := ociTestRequest(http.MethodGet, "/v2/"+ociTestNamespace+"/"+ociTestArtifact+"/blobs/"+secretDigest, nil)
	assertOCIStatus(t, rec, http.StatusNotFound, ociCodeBlobUnknown)

	rec = ociTestRequest(http.MethodGet, "/v2/"+ociTestNamespace+"/other/blobs/"+secretDigest, nil)
	assertOCIStatus(t, rec, http.StatusOK, "")
}

func TestOCIDistributionHidesUnsyncedAndBlockedVersions(t *testing.T) {
	env := newOCITestEnv(t, true)
	env.addArtifact(ociTestArtifact, "", "Image")

	env.publishImage(t, ociTestArtifact, ociTestTagOne)
	unsynced, _ := env.publishImage(t, ociTestArtifact, ociTestTagTwo)
	blocked, _ := env.publishImage(t, ociTestArtifact, ociTestTagThree)
	env.updateVersion(unsynced.Name, func(v *opendepotv1alpha1.Version) { v.Status.Synced = false })
	env.updateVersion(blocked.Name, func(v *opendepotv1alpha1.Version) {
		v.Status.JevAssessment = &opendepotv1alpha1.JevAssessment{Blocked: true}
	})

	manifestPath := "/v2/" + ociTestNamespace + "/" + ociTestArtifact + "/manifests/"
	for _, tag := range []string{ociTestTagTwo, ociTestTagThree} {
		rec := ociTestRequest(http.MethodGet, manifestPath+tag, nil)
		assertOCIStatus(t, rec, http.StatusNotFound, ociCodeManifestUnknown)
	}

	rec := ociTestRequest(http.MethodGet, "/v2/"+ociTestNamespace+"/"+ociTestArtifact+"/tags/list", nil)
	assertOCIStatus(t, rec, http.StatusOK, "")

	var tags struct {
		Tags []string `json:"tags"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &tags); err != nil {
		t.Fatalf("decode tags: %v", err)
	}

	if strings.Join(tags.Tags, ",") != ociTestTagOne {
		t.Fatalf("tags = %v, want [%s]", tags.Tags, ociTestTagOne)
	}
}

func TestOCIDistributionTagsListSortedAndServableOnly(t *testing.T) {
	env := newOCITestEnv(t, true)
	env.addArtifact(ociTestArtifact, "", "Image")
	env.publishImage(t, ociTestArtifact, ociTestTagTwo)
	env.publishImage(t, ociTestArtifact, ociTestTagOne)
	env.publishImage(t, ociTestArtifact, ociTestTagMiddle)

	rec := ociTestRequest(http.MethodGet, "/v2/"+ociTestNamespace+"/"+ociTestArtifact+"/tags/list", nil)
	assertOCIStatus(t, rec, http.StatusOK, "")

	var tags struct {
		Name string   `json:"name"`
		Tags []string `json:"tags"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &tags); err != nil {
		t.Fatalf("decode tags: %v", err)
	}

	if tags.Name != ociTestArtifact {
		t.Fatalf("name = %q, want %s", tags.Name, ociTestArtifact)
	}

	want := strings.Join([]string{ociTestTagOne, ociTestTagMiddle, ociTestTagTwo}, ",")
	if strings.Join(tags.Tags, ",") != want {
		t.Fatalf("tags = %v, want sorted %s", tags.Tags, want)
	}
}

func TestOCIDistributionReferrers(t *testing.T) {
	env := newOCITestEnv(t, true)
	env.addArtifact(ociTestArtifact, "", "Image")
	_, manifestDigest := env.publishImage(t, ociTestArtifact, ociTestTagOne, []byte("layer"))

	t.Run("a served manifest returns an OCI 1.1 referrers index", func(t *testing.T) {
		rec := ociTestRequest(http.MethodGet, "/v2/"+ociTestNamespace+"/"+ociTestArtifact+"/referrers/"+
			manifestDigest.String(), nil)
		assertOCIStatus(t, rec, http.StatusOK, "")

		if got := rec.Header().Get(ociTestContentType); got != ocispec.MediaTypeImageIndex {
			t.Fatalf("%s = %q", ociTestContentType, got)
		}

		var index struct {
			SchemaVersion int    `json:"schemaVersion"`
			MediaType     string `json:"mediaType"`
			Manifests     []any  `json:"manifests"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &index); err != nil {
			t.Fatalf("decode referrers: %v", err)
		}

		if index.SchemaVersion != 2 || index.MediaType != ocispec.MediaTypeImageIndex || index.Manifests == nil {
			t.Fatalf("referrers index = %+v", index)
		}
	})

	t.Run("an unknown subject is MANIFEST_UNKNOWN", func(t *testing.T) {
		rec := ociTestRequest(http.MethodGet, "/v2/"+ociTestNamespace+"/"+ociTestArtifact+"/referrers/"+
			digestOfEmpty(), nil)
		assertOCIStatus(t, rec, http.StatusNotFound, ociCodeManifestUnknown)
	})
}

func TestOCIDistributionVisibilityUsesOCIArtifactResources(t *testing.T) {
	env := newOCITestEnv(t, false)
	env.addArtifact("podinfo-app", "podinfo", "Image")
	env.addArtifact("secret-app", ociTestSecretRepo, "Image")
	env.publishImage(t, "podinfo-app", ociTestTagOne)
	env.publishImage(t, "secret-app", ociTestTagOne)
	env.addBinding("podinfo-*")

	rec := ociTestRequest(http.MethodGet, "/v2/"+ociTestNamespace+"/podinfo/manifests/"+ociTestTagOne, ociTestBearer(t))
	assertOCIStatus(t, rec, http.StatusOK, "")

	hidden := ociTestRequest(http.MethodGet, "/v2/"+ociTestNamespace+"/secret/manifests/"+ociTestTagOne, ociTestBearer(t))
	assertOCIStatus(t, hidden, http.StatusNotFound, ociCodeNameUnknown)

	tags := ociTestRequest(http.MethodGet, "/v2/"+ociTestNamespace+"/secret/tags/list", ociTestBearer(t))
	assertOCIStatus(t, tags, http.StatusNotFound, ociCodeNameUnknown)
}

func TestOCIDistributionDeniesFilesystemRootsOutsideTheMountPath(t *testing.T) {
	env := newOCITestEnv(t, true)
	env.addArtifact(ociTestArtifact, "", "Image")
	version, _ := env.publishImage(t, ociTestArtifact, ociTestTagOne, []byte("layer"))

	outside := t.TempDir()
	env.updateVersion(version.Name, func(v *opendepotv1alpha1.Version) {
		v.Spec.OCIArtifactConfigRef.StorageConfig.FileSystem.DirectoryPath = &outside
	})

	rec := ociTestRequest(http.MethodGet, "/v2/"+ociTestNamespace+"/"+ociTestArtifact+"/manifests/"+ociTestTagOne, nil)
	assertOCIStatus(t, rec, http.StatusForbidden, ociCodeDenied)
}

func TestOCIDistributionBlobPresignRedirect(t *testing.T) {
	layer := []byte("presigned-layer")
	layerDigest := digest.FromBytes(layer)

	env := newOCITestEnv(t, true)
	env.addArtifact(ociTestArtifact, "", "Image")
	version, _ := env.publishImage(t, ociTestArtifact, ociTestTagOne, layer)
	fake := newOCITestS3(t, env.root, false)

	enabled := true
	fallback := false
	env.updateVersion(version.Name, func(v *opendepotv1alpha1.Version) {
		v.Spec.OCIArtifactConfigRef.StorageConfig = &opendepotv1alpha1.StorageConfig{
			S3:      &opendepotv1alpha1.AmazonS3Config{Bucket: ociTestBucket, Region: ociTestRegion},
			Presign: &opendepotv1alpha1.PresignConfig{Enabled: &enabled, FallbackToProxy: &fallback},
		}
	})

	rec := ociTestRequest(http.MethodGet, "/v2/"+ociTestNamespace+"/"+ociTestArtifact+"/blobs/"+layerDigest.String(), nil)
	if rec.Code != http.StatusTemporaryRedirect {
		t.Fatalf("status = %d, want 307: %s", rec.Code, rec.Body.String())
	}

	if cacheControl := rec.Header().Get("Cache-Control"); cacheControl != "private, no-store" {
		t.Fatalf("Cache-Control = %q", cacheControl)
	}

	location := rec.Header().Get(ociTestLocation)
	wantPrefix := fake.URL + "/" + ociTestBucket + "/oci/" + ociTestNamespace + "/" + version.Name +
		"/blobs/sha256/" + layerDigest.Encoded()
	if !strings.HasPrefix(location, wantPrefix) {
		t.Fatalf("Location %q does not point at the controller's blob key %q", location, wantPrefix)
	}

	if !strings.Contains(location, "X-Amz-Signature=") {
		t.Fatalf("Location is not presigned: %q", location)
	}
}

func TestOCIDistributionPresignChecksumMismatchNeverRedirects(t *testing.T) {
	layer := []byte("tampered-layer")
	layerDigest := digest.FromBytes(layer)

	env := newOCITestEnv(t, true)
	env.addArtifact(ociTestArtifact, "", "Image")
	version, _ := env.publishImage(t, ociTestArtifact, ociTestTagOne, layer)
	newOCITestS3(t, env.root, true)

	enabled := true
	fallback := false
	env.updateVersion(version.Name, func(v *opendepotv1alpha1.Version) {
		v.Spec.OCIArtifactConfigRef.StorageConfig = &opendepotv1alpha1.StorageConfig{
			S3:      &opendepotv1alpha1.AmazonS3Config{Bucket: ociTestBucket, Region: ociTestRegion},
			Presign: &opendepotv1alpha1.PresignConfig{Enabled: &enabled, FallbackToProxy: &fallback},
		}
	})

	rec := ociTestRequest(http.MethodGet, "/v2/"+ociTestNamespace+"/"+ociTestArtifact+"/blobs/"+layerDigest.String(), nil)
	assertOCIStatus(t, rec, http.StatusInternalServerError, ociCodeUnknown)

	if location := rec.Header().Get(ociTestLocation); location != "" {
		t.Fatalf("checksum mismatch redirected to %q", location)
	}
}

func TestOCIDistributionCorruptBlobNeverCompletes(t *testing.T) {
	env := newOCITestEnv(t, true)
	env.addArtifact(ociTestArtifact, "", "Image")
	layer := []byte("original-layer")
	env.publishImage(t, ociTestArtifact, ociTestTagOne, layer)

	layerDigest := digest.FromBytes(layer)
	corrupt := filepathForBlob(env.root, ociTestVersionName(ociTestArtifact, ociTestTagOne), layerDigest)
	if err := overwriteFile(corrupt, []byte("tampered-layer!")); err != nil {
		t.Fatalf("overwrite blob: %v", err)
	}

	blobPath := "/v2/" + ociTestNamespace + "/" + ociTestArtifact + "/blobs/" + layerDigest.String()

	rec := ociTestRequest(http.MethodGet, blobPath, nil)
	if rec.Code == http.StatusOK && bytes.Equal(readAllOCIBody(t, rec.Body), layer) {
		t.Fatal("corrupt blob was served as the original content")
	}

	ranged := ociTestRequest(http.MethodGet, blobPath, map[string]string{ociTestRangeHeader: "bytes=0-3"})
	if ranged.Code != http.StatusInternalServerError {
		t.Fatalf("ranged status = %d, want 500 for a corrupt blob", ranged.Code)
	}
}

func TestOCIDistributionSameLengthCorruptBlobNeverCompletesWith200(t *testing.T) {
	env := newOCITestEnv(t, true)
	env.addArtifact(ociTestArtifact, "", "Image")
	layer := []byte("original-layer")
	env.publishImage(t, ociTestArtifact, ociTestTagOne, layer)

	layerDigest := digest.FromBytes(layer)
	corrupt := filepathForBlob(env.root, ociTestVersionName(ociTestArtifact, ociTestTagOne), layerDigest)

	// The tampered bytes match the original length, so only the digest check can catch the corruption.
	tampered := []byte("tampered-layer")
	if len(tampered) != len(layer) {
		t.Fatalf("tampered length = %d, want %d", len(tampered), len(layer))
	}

	if err := overwriteFile(corrupt, tampered); err != nil {
		t.Fatalf("overwrite blob: %v", err)
	}

	blobPath := "/v2/" + ociTestNamespace + "/" + ociTestArtifact + "/blobs/" + layerDigest.String()
	rec := ociTestRequest(http.MethodGet, blobPath, nil)
	if rec.Code == http.StatusOK {
		t.Fatal("same-length corrupt blob completed with 200")
	}

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 for a corrupt blob", rec.Code)
	}

	if bytes.Contains(rec.Body.Bytes(), tampered) {
		t.Fatal("corrupt bytes reached the response body")
	}
}

func TestOCIDistributionDuplicateRepositoryIsAmbiguous(t *testing.T) {
	env := newOCITestEnv(t, false)
	env.addBinding("*")
	env.addArtifact("dup-first", "shared", "Image")
	env.addArtifact("dup-second", "shared", "Image")
	env.publishImage(t, "dup-first", ociTestTagOne)

	manifest := ociTestRequest(http.MethodGet, "/v2/"+ociTestNamespace+"/shared/manifests/"+ociTestTagOne,
		ociTestBearer(t))
	assertOCIStatus(t, manifest, http.StatusConflict, ociCodeUnknown)

	tags := ociTestRequest(http.MethodGet, "/v2/"+ociTestNamespace+"/shared/tags/list", ociTestBearer(t))
	assertOCIStatus(t, tags, http.StatusConflict, ociCodeUnknown)
}
