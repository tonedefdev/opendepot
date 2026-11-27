package buildbroker

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/go-logr/logr"
	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	"github.com/tonedefdev/opendepot/pkg/ocibuild"
)

const (
	testNamespace = "opendepot"
	testVersion   = "app-1.0.0"
	testBuildID   = "abcdefghijklmnopqrst"
	testCommit    = "0123456789abcdef0123456789abcdef01234567"
)

// contextRoute is the path of the context route, relative to a build.
const contextRoute = "/context"

// amd64Arch is the architecture of the platform manifests in these tests.
const amd64Arch = "amd64"

// memSink is an in-memory BlobSink. When gate is set, GetBlob waits on it after reporting on started.
type memSink struct {
	mu        sync.Mutex
	blobs     map[digest.Digest][]byte
	manifests map[digest.Digest][]byte
	gate      chan struct{}
	started   chan struct{}
}

func newMemSink() *memSink {
	return &memSink{blobs: map[digest.Digest][]byte{}, manifests: map[digest.Digest][]byte{}}
}

func (m *memSink) GetBlob(_ context.Context, d digest.Digest) (io.ReadCloser, error) {
	if m.started != nil {
		m.started <- struct{}{}
	}

	if m.gate != nil {
		<-m.gate
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	data, ok := m.blobs[d]
	if !ok {
		return nil, io.ErrUnexpectedEOF
	}

	return io.NopCloser(bytes.NewReader(data)), nil
}

func (m *memSink) PutBlob(_ context.Context, d digest.Digest, content io.ReadSeeker) error {
	data, err := io.ReadAll(content)
	if err != nil {
		return err
	}

	if digest.FromBytes(data) != d {
		return io.ErrUnexpectedEOF
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.blobs[d] = data
	return nil
}

func (m *memSink) PutManifest(_ context.Context, content []byte) (digest.Digest, error) {
	d := digest.FromBytes(content)

	m.mu.Lock()
	defer m.mu.Unlock()

	m.manifests[d] = content
	return d, nil
}

func (m *memSink) has(d digest.Digest) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	_, ok := m.blobs[d]
	return ok
}

// fixture is a broker over an in-memory Version and sink, served by httptest.
type fixture struct {
	t         *testing.T
	server    *httptest.Server
	sink      *memSink
	fetch     string
	upload    string
	context   []byte
	ctxDigest digest.Digest
}

// newFixture returns a broker whose Version has a Running build. mutate may change the build status first.
func newFixture(t *testing.T, cfg Config, mutate func(*opendepotv1alpha1.OCIBuildStatus)) *fixture {
	t.Helper()
	fetch, err := ocibuild.NewToken()
	if err != nil {
		t.Fatal(err)
	}

	upload, err := ocibuild.NewToken()
	if err != nil {
		t.Fatal(err)
	}

	contextArchive := []byte("context archive bytes")
	build := &opendepotv1alpha1.OCIBuildStatus{
		BuildID:         testBuildID,
		SourceCommit:    testCommit,
		SourceTag:       "v1.0.0",
		ContextDigest:   digest.FromBytes(contextArchive).String(),
		FetchTokenHash:  "sha256:" + ocibuild.HashToken(fetch),
		UploadTokenHash: "sha256:" + ocibuild.HashToken(upload),
		Phase:           "Running",
	}

	if mutate != nil {
		mutate(build)
	}

	scheme := runtime.NewScheme()
	if err := opendepotv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	version := &opendepotv1alpha1.Version{
		ObjectMeta: metav1.ObjectMeta{Name: testVersion, Namespace: testNamespace},
		Status:     opendepotv1alpha1.VersionStatus{OCIBuild: build},
	}

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(version).Build()
	sink := newMemSink()
	cfg.Reader = c
	cfg.Log = logr.Discard()
	cfg.Sinks = func(_ context.Context, _, _ string) (BlobSink, error) {
		return sink, nil
	}

	srv, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(srv)
	t.Cleanup(server.Close)

	sink.blobs[digest.FromBytes(contextArchive)] = contextArchive

	return &fixture{
		t:         t,
		server:    server,
		sink:      sink,
		fetch:     fetch,
		upload:    upload,
		context:   contextArchive,
		ctxDigest: digest.FromBytes(contextArchive),
	}
}

// do sends one request and returns the status code and body.
func (f *fixture) do(method, path, token string, body io.Reader, contentType string) (int, []byte) {
	f.t.Helper()
	req, err := http.NewRequest(method, f.server.URL+path, body)
	if err != nil {
		f.t.Fatal(err)
	}

	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		f.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	data, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, data
}

func buildPath(buildID, rest string) string {
	return RoutePrefix + testNamespace + "/" + testVersion + "/" + buildID + rest
}

// imageFixture stores a config and a layer and returns a manifest that references them for platform.
func imageFixture(t *testing.T, sink *memSink, arch string) []byte {
	t.Helper()
	configJSON, err := json.Marshal(ocispec.Image{
		Platform: ocispec.Platform{OS: "linux", Architecture: arch},
		RootFS:   ocispec.RootFS{Type: "layers"},
	})
	if err != nil {
		t.Fatal(err)
	}

	layer := []byte("layer")
	sink.blobs[digest.FromBytes(configJSON)] = configJSON
	sink.blobs[digest.FromBytes(layer)] = layer

	manifest, err := json.Marshal(ocispec.Manifest{
		MediaType: ocispec.MediaTypeImageManifest,
		Config:    ocispec.Descriptor{MediaType: ocispec.MediaTypeImageConfig, Digest: digest.FromBytes(configJSON), Size: int64(len(configJSON))},
		Layers:    []ocispec.Descriptor{{MediaType: ocispec.MediaTypeImageLayerGzip, Digest: digest.FromBytes(layer), Size: int64(len(layer))}},
	})
	if err != nil {
		t.Fatal(err)
	}

	return manifest
}

func TestContextRouteStreamsTheStagedArchiveForTheFetchToken(t *testing.T) {
	f := newFixture(t, Config{}, nil)

	code, body := f.do(http.MethodGet, buildPath(testBuildID, "/context"), f.fetch, nil, "")
	if code != http.StatusOK || !bytes.Equal(body, f.context) {
		t.Fatalf("context = %d %q, want 200 with the archive", code, body)
	}
}

func TestRejectionsOfEveryAuthPhaseAndBuildCase(t *testing.T) {
	blob := []byte("blob")
	blobDigest := digest.FromBytes(blob).String()

	cases := []struct {
		name   string
		mutate func(*opendepotv1alpha1.OCIBuildStatus)
		// version overrides the Version name in the path.
		version string
		buildID string
		method  string
		path    string
		token   func(f *fixture) string
		want    int
	}{
		{name: "missing bearer", path: contextRoute, method: http.MethodGet, want: http.StatusUnauthorized, token: func(*fixture) string { return "" }},
		{name: "unknown token", path: contextRoute, method: http.MethodGet, want: http.StatusUnauthorized, token: func(*fixture) string {
			token, _ := ocibuild.NewToken()
			return token
		}},
		{name: "unknown version", path: contextRoute, method: http.MethodGet, version: "missing", want: http.StatusNotFound, token: func(f *fixture) string { return f.fetch }},
		{name: "stale build id", path: contextRoute, method: http.MethodGet, buildID: "zzzzzzzzzzzzzzzzzzzz", want: http.StatusNotFound, token: func(f *fixture) string { return f.fetch }},
		{name: "build not running", path: contextRoute, method: http.MethodGet, mutate: func(b *opendepotv1alpha1.OCIBuildStatus) { b.Phase = "Succeeded" }, want: http.StatusNotFound, token: func(f *fixture) string { return f.fetch }},
		{name: "fetch token on blob route", path: "/blobs/" + blobDigest, method: http.MethodPut, want: http.StatusForbidden, token: func(f *fixture) string { return f.fetch }},
		{name: "fetch token on manifest route", path: "/manifests/linux%2Famd64", method: http.MethodPut, want: http.StatusForbidden, token: func(f *fixture) string { return f.fetch }},
		{name: "upload token on context route", path: contextRoute, method: http.MethodGet, want: http.StatusForbidden, token: func(f *fixture) string { return f.upload }},
		{name: "unknown route", path: "/unknown", method: http.MethodGet, want: http.StatusNotFound, token: func(f *fixture) string { return f.fetch }},
		{name: "invalid blob digest", path: "/blobs/not-a-digest", method: http.MethodPut, want: http.StatusBadRequest, token: func(f *fixture) string { return f.upload }},
		{name: "non sha256 blob digest", path: "/blobs/sha512:" + strings.Repeat("a", 128), method: http.MethodPut, want: http.StatusBadRequest, token: func(f *fixture) string { return f.upload }},
		{name: "unsupported platform", path: "/manifests/windows%2Famd64", method: http.MethodPut, want: http.StatusBadRequest, token: func(f *fixture) string { return f.upload }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, Config{}, tc.mutate)
			buildID := testBuildID
			if tc.buildID != "" {
				buildID = tc.buildID
			}

			path := buildPath(buildID, tc.path)
			if tc.version != "" {
				path = strings.Replace(path, testVersion, tc.version, 1)
			}

			code, _ := f.do(tc.method, path, tc.token(f), bytes.NewReader(blob), "application/octet-stream")
			if code != tc.want {
				t.Fatalf("status = %d, want %d", code, tc.want)
			}
		})
	}
}

func TestBuildWithNoOCIBuildStatusIsNotFound(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := opendepotv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	version := &opendepotv1alpha1.Version{ObjectMeta: metav1.ObjectMeta{Name: testVersion, Namespace: testNamespace}}
	srv, err := New(Config{
		Reader: fake.NewClientBuilder().WithScheme(scheme).WithObjects(version).Build(),
		Sinks:  func(context.Context, string, string) (BlobSink, error) { return newMemSink(), nil },
		Log:    logr.Discard(),
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, buildPath(testBuildID, "/context"), nil)
	req.Header.Set("Authorization", "Bearer anything")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestBlobUploadStoresVerifiedContent(t *testing.T) {
	f := newFixture(t, Config{}, nil)
	blob := []byte("a layer for the build")
	d := digest.FromBytes(blob)

	code, _ := f.do(http.MethodPut, buildPath(testBuildID, "/blobs/"+d.String()), f.upload, bytes.NewReader(blob), "application/octet-stream")
	if code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", code)
	}

	if !f.sink.has(d) {
		t.Fatal("blob was not stored")
	}
}

func TestBlobUploadRejectsDigestMismatchWithoutStoring(t *testing.T) {
	f := newFixture(t, Config{}, nil)
	wrong := digest.FromString("something else")

	code, _ := f.do(http.MethodPut, buildPath(testBuildID, "/blobs/"+wrong.String()), f.upload,
		strings.NewReader("different bytes"), "application/octet-stream")
	if code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", code)
	}

	if f.sink.has(wrong) {
		t.Fatal("mismatched blob was stored")
	}
}

func TestBlobUploadRejectsOversizeWithKnownAndUnknownLength(t *testing.T) {
	f := newFixture(t, Config{MaxBlobBytes: 8}, nil)
	blob := []byte("more than eight bytes")
	d := digest.FromBytes(blob)

	code, _ := f.do(http.MethodPut, buildPath(testBuildID, "/blobs/"+d.String()), f.upload, bytes.NewReader(blob), "")
	if code != http.StatusRequestEntityTooLarge {
		t.Fatalf("known length: status = %d, want 413", code)
	}

	// A reader that is not a bytes or strings reader sends chunked, so the body limit is enforced while it is read.
	code, _ = f.do(http.MethodPut, buildPath(testBuildID, "/blobs/"+d.String()), f.upload,
		io.MultiReader(bytes.NewReader(blob)), "")
	if code != http.StatusRequestEntityTooLarge {
		t.Fatalf("unknown length: status = %d, want 413", code)
	}

	if f.sink.has(d) {
		t.Fatal("oversize blob was stored")
	}
}

func TestManifestUploadStoresAPlatformImage(t *testing.T) {
	f := newFixture(t, Config{}, nil)
	manifest := imageFixture(t, f.sink, "amd64")

	code, body := f.do(http.MethodPut, buildPath(testBuildID, "/manifests/linux%2Famd64"), f.upload,
		bytes.NewReader(manifest), ocispec.MediaTypeImageManifest)
	if code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", code, body)
	}

	var out struct {
		ManifestDigest string `json:"manifestDigest"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}

	if out.ManifestDigest != digest.FromBytes(manifest).String() {
		t.Fatalf("manifestDigest = %s, want %s", out.ManifestDigest, digest.FromBytes(manifest))
	}
}

func TestManifestUploadRejections(t *testing.T) {
	cases := []struct {
		name        string
		arch        string
		contentType string
		size        int64
		omitBlob    bool
		body        func(f *fixture, arch string) []byte
		want        int
	}{
		{name: "wrong media type", arch: amd64Arch, contentType: "application/json", want: http.StatusUnsupportedMediaType},
		{name: "oversize manifest", arch: amd64Arch, size: 16, want: http.StatusRequestEntityTooLarge},
		{name: "not json", arch: amd64Arch, body: func(*fixture, string) []byte { return []byte("{") }, want: http.StatusUnprocessableEntity},
		{name: "config platform mismatch", arch: "arm64", want: http.StatusUnprocessableEntity},
		{name: "missing referenced blob", arch: amd64Arch, omitBlob: true, want: http.StatusUnprocessableEntity},
		{name: "attestation annotation", arch: amd64Arch, body: func(f *fixture, arch string) []byte {
			m := ocispec.Manifest{
				MediaType:   ocispec.MediaTypeImageManifest,
				Annotations: map[string]string{"vnd.docker.reference.type": "attestation-manifest"},
			}
			data, _ := json.Marshal(m)
			return data
		}, want: http.StatusUnprocessableEntity},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Config{}
			if tc.size > 0 {
				cfg.MaxManifestBytes = tc.size
			}

			f := newFixture(t, cfg, nil)
			manifest := imageFixture(t, f.sink, tc.arch)
			if tc.omitBlob {
				f.sink.blobs = map[digest.Digest][]byte{}
				f.sink.blobs[digest.FromBytes(manifestConfig(t, tc.arch))] = manifestConfig(t, tc.arch)
			}

			if tc.body != nil {
				manifest = tc.body(f, tc.arch)
			}

			contentType := tc.contentType
			if contentType == "" {
				contentType = ocispec.MediaTypeImageManifest
			}

			code, _ := f.do(http.MethodPut, buildPath(testBuildID, "/manifests/linux%2Famd64"), f.upload,
				bytes.NewReader(manifest), contentType)
			if code != tc.want {
				t.Fatalf("status = %d, want %d", code, tc.want)
			}
		})
	}
}

// manifestConfig returns the config blob that imageFixture stores for arch.
func manifestConfig(t *testing.T, arch string) []byte {
	t.Helper()
	data, err := json.Marshal(ocispec.Image{
		Platform: ocispec.Platform{OS: "linux", Architecture: arch},
		RootFS:   ocispec.RootFS{Type: "layers"},
	})
	if err != nil {
		t.Fatal(err)
	}

	return data
}

func TestContextRouteLimitsInFlightRequestsPerBuild(t *testing.T) {
	f := newFixture(t, Config{MaxConcurrentPerBuild: 2}, nil)
	f.sink.gate = make(chan struct{})
	f.sink.started = make(chan struct{}, 2)

	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			f.do(http.MethodGet, buildPath(testBuildID, "/context"), f.fetch, nil, "")
		}()
	}

	<-f.sink.started
	<-f.sink.started

	code, _ := f.do(http.MethodGet, buildPath(testBuildID, "/context"), f.fetch, nil, "")
	if code != http.StatusTooManyRequests {
		t.Fatalf("third request status = %d, want 429", code)
	}

	close(f.sink.gate)
	wg.Wait()
}

func TestParseRouteKeepsEncodedPlatformAsOneSegment(t *testing.T) {
	rt, ok := parseRoute(RoutePrefix + "ns/app/" + testBuildID + "/manifests/linux%2Farm%2Fv7")
	if !ok || rt.kind != "manifests" || rt.arg != "linux/arm/v7" {
		t.Fatalf("parseRoute = %+v, %v", rt, ok)
	}

	for _, bad := range []string{
		RoutePrefix + "ns/app/" + testBuildID,
		RoutePrefix + "ns/app/" + testBuildID + "/blobs",
		RoutePrefix + "ns/app/" + testBuildID + "/context/extra",
		"/other/ns/app/" + testBuildID + "/context",
		RoutePrefix + "ns//" + testBuildID + "/context",
	} {
		if _, ok := parseRoute(bad); ok {
			t.Fatalf("parseRoute(%q) accepted an invalid route", bad)
		}
	}
}

func TestTokenMatchesRejectsEmptyAndWrongHashes(t *testing.T) {
	token, err := ocibuild.NewToken()
	if err != nil {
		t.Fatal(err)
	}

	if !tokenMatches(token, "sha256:"+ocibuild.HashToken(token)) {
		t.Fatal("valid token did not match")
	}

	if tokenMatches(token, "") {
		t.Fatal("empty stored hash matched")
	}

	if tokenMatches(token, "sha256:"+ocibuild.HashToken("other")) {
		t.Fatal("wrong token matched")
	}
}

func TestNewRejectsMissingDependencies(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Fatal("New accepted a config with no reader or sink resolver")
	}

	srv, err := New(Config{Reader: fake.NewClientBuilder().Build(), Sinks: func(context.Context, string, string) (BlobSink, error) {
		return nil, nil
	}})
	if err != nil {
		t.Fatal(err)
	}

	if srv.NeedLeaderElection() {
		t.Fatal("the broker must run on every replica")
	}
}

func TestStartStopsWhenTheContextIsDone(t *testing.T) {
	srv, err := New(Config{Addr: "127.0.0.1:0", Reader: fake.NewClientBuilder().Build(), Sinks: func(context.Context, string, string) (BlobSink, error) {
		return nil, nil
	}})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := srv.Start(ctx); err != nil {
		t.Fatalf("Start returned %v after cancel", err)
	}
}
