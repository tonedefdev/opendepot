package oci

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/opencontainers/go-digest"
	specs "github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

const (
	testOS        = "linux"
	testUnknownOS = "unknown"
	testChart     = "demo"
	testVersion   = "1.2.3"
	testUser      = "robot"
	testPassword  = "s3cret"
	testGHCR      = "ghcr.io"
	testLoopback  = "http://127.0.0.1:5000/"
	testAMD64Plat = "linux/amd64"
)

func testTime() time.Time {
	return time.Date(2026, time.November, 26, 12, 0, 0, 0, time.UTC)
}

// buildChartTgz returns a chart archive whose top-level directory is name, as `helm package` produces.
func buildChartTgz(t *testing.T, name, version string, extra map[string]string) []byte {
	t.Helper()

	chartYAML := fmt.Sprintf(`apiVersion: v2
name: %s
version: %s
appVersion: "2.0"
description: Demo chart %s
kubeVersion: ">=1.25.0"
keywords:
  - demo
  - test
dependencies:
  - name: redis
    version: 17.x
    repository: https://charts.example.com
`, name, version, name)

	files := map[string]string{
		"Chart.yaml":  chartYAML,
		"values.yaml": "replicaCount: 1\n",
	}
	for k, v := range extra {
		files[k] = v
	}

	prefixed := make(map[string]string, len(files))
	for n, data := range files {
		prefixed[name+"/"+n] = data
	}

	return makeTgz(t, prefixed)
}

// makeTgz returns a gzipped tar holding files, keyed by archive path.
func makeTgz(t *testing.T, files map[string]string) []byte {
	t.Helper()

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)

	for _, n := range names {
		data := []byte(files[n])
		hdr := &tar.Header{Name: n, Mode: 0o644, Size: int64(len(data)), ModTime: testTime()}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}

		if _, err := tw.Write(data); err != nil {
			t.Fatal(err)
		}
	}

	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}

	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}

	return buf.Bytes()
}

// mockRegistry is an in-process OCI distribution endpoint for tests. It serves one repository.
type mockRegistry struct {
	t *testing.T

	mu        sync.Mutex
	blobs     map[digest.Digest][]byte
	tampered  map[digest.Digest][]byte
	redirects map[digest.Digest]string
	manifests map[digest.Digest]mockManifest
	tags      map[string]digest.Digest
	referrers map[digest.Digest][]ocispec.Descriptor

	// basicUser and basicPass, when set, require HTTP Basic auth on every request.
	basicUser, basicPass string

	server *httptest.Server
}

type mockManifest struct {
	mediaType string
	content   []byte
	artifact  string
}

func newMockRegistry(t *testing.T) *mockRegistry {
	t.Helper()

	m := &mockRegistry{
		t:         t,
		blobs:     map[digest.Digest][]byte{},
		tampered:  map[digest.Digest][]byte{},
		redirects: map[digest.Digest]string{},
		manifests: map[digest.Digest]mockManifest{},
		tags:      map[string]digest.Digest{},
		referrers: map[digest.Digest][]ocispec.Descriptor{},
	}
	m.server = httptest.NewServer(http.HandlerFunc(m.serve))
	t.Cleanup(m.server.Close)

	return m
}

func (m *mockRegistry) host() string {
	u, err := url.Parse(m.server.URL)
	if err != nil {
		m.t.Fatal(err)
	}

	return u.Host
}

func (m *mockRegistry) putBlob(content []byte) ocispec.Descriptor {
	d := digest.FromBytes(content)

	m.mu.Lock()
	defer m.mu.Unlock()
	m.blobs[d] = content
	return ocispec.Descriptor{MediaType: "application/octet-stream", Digest: d, Size: int64(len(content))}
}

func (m *mockRegistry) putManifest(mediaType, artifactType string, content []byte) ocispec.Descriptor {
	d := digest.FromBytes(content)

	m.mu.Lock()
	defer m.mu.Unlock()
	m.manifests[d] = mockManifest{mediaType: mediaType, content: content, artifact: artifactType}
	return ocispec.Descriptor{MediaType: mediaType, Digest: d, Size: int64(len(content)), ArtifactType: artifactType}
}

func (m *mockRegistry) tag(name string, d digest.Digest) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tags[name] = d
}

func (m *mockRegistry) serve(w http.ResponseWriter, r *http.Request) {
	if m.basicUser != "" {
		user, pass, ok := r.BasicAuth()
		if !ok || user != m.basicUser || pass != m.basicPass {
			w.Header().Set("WWW-Authenticate", `Basic realm="mock"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
	}

	path := r.URL.Path
	if path == "/v2/" {
		w.WriteHeader(http.StatusOK)
		return
	}

	const prefix = "/v2/org/chart/"
	if !strings.HasPrefix(path, prefix) {
		http.NotFound(w, r)
		return
	}

	rest := strings.TrimPrefix(path, prefix)
	switch {
	case rest == "tags/list":
		m.serveTags(w)
	case strings.HasPrefix(rest, "manifests/"):
		m.serveManifest(w, r, strings.TrimPrefix(rest, "manifests/"))
	case strings.HasPrefix(rest, "blobs/"):
		m.serveBlob(w, r, digest.Digest(strings.TrimPrefix(rest, "blobs/")))
	case strings.HasPrefix(rest, "referrers/"):
		m.serveReferrers(w, r, digest.Digest(strings.TrimPrefix(rest, "referrers/")))
	default:
		http.NotFound(w, r)
	}
}

func (m *mockRegistry) serveTags(w http.ResponseWriter) {
	m.mu.Lock()
	tags := make([]string, 0, len(m.tags))
	for name := range m.tags {
		tags = append(tags, name)
	}
	m.mu.Unlock()

	sort.Strings(tags)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"name": "org/chart", "tags": tags})
}

func (m *mockRegistry) serveManifest(w http.ResponseWriter, r *http.Request, ref string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var d digest.Digest
	if strings.Contains(ref, ":") {
		d = digest.Digest(ref)
	} else {
		var ok bool
		d, ok = m.tags[ref]
		if !ok {
			http.NotFound(w, r)
			return
		}
	}

	entry, ok := m.manifests[d]
	if !ok {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", entry.mediaType)
	w.Header().Set("Docker-Content-Digest", d.String())
	w.Header().Set("Content-Length", fmt.Sprint(len(entry.content)))
	if r.Method == http.MethodHead {
		return
	}

	_, _ = w.Write(entry.content)
}

func (m *mockRegistry) serveBlob(w http.ResponseWriter, r *http.Request, d digest.Digest) {
	m.mu.Lock()
	content, ok := m.blobs[d]
	tampered, isTampered := m.tampered[d]
	target, redirected := m.redirects[d]
	m.mu.Unlock()

	if redirected {
		http.Redirect(w, r, target, http.StatusTemporaryRedirect)
		return
	}

	if !ok {
		http.NotFound(w, r)
		return
	}

	if isTampered {
		content = tampered
	}

	w.Header().Set("Docker-Content-Digest", d.String())
	w.Header().Set("Content-Length", fmt.Sprint(len(content)))
	if r.Method == http.MethodHead {
		return
	}

	_, _ = w.Write(content)
}

func (m *mockRegistry) serveReferrers(w http.ResponseWriter, r *http.Request, subject digest.Digest) {
	m.mu.Lock()
	all := m.referrers[subject]
	m.mu.Unlock()

	filter := r.URL.Query().Get("artifactType")
	matches := []ocispec.Descriptor{}
	for _, desc := range all {
		if filter == "" || desc.ArtifactType == filter {
			matches = append(matches, desc)
		}
	}

	w.Header().Set("Content-Type", ocispec.MediaTypeImageIndex)
	_ = json.NewEncoder(w).Encode(ocispec.Index{
		Versioned: ocispecVersioned(),
		MediaType: ocispec.MediaTypeImageIndex,
		Manifests: matches,
	})
}

// mockImage describes the registry content built by seedImage.
type mockImage struct {
	index      ocispec.Descriptor
	amd64      ocispec.Descriptor
	arm64      ocispec.Descriptor
	amd64Layer ocispec.Descriptor
	signature  ocispec.Descriptor
}

// seedImage stores a multi-platform image: an index with linux/amd64, linux/arm64/v8, and an attestation entry, plus
// a signature referrer for the index. It tags the index as 1.0.0 and latest.
func seedImage(m *mockRegistry) mockImage {
	m.t.Helper()

	build := func(arch, variant string) (ocispec.Descriptor, ocispec.Descriptor) {
		layer := m.putBlob([]byte("layer for " + arch))
		cfg := m.putBlob([]byte(fmt.Sprintf(`{"architecture":%q,"os":"linux"}`, arch)))
		manifest := ocispec.Manifest{
			Versioned: ocispecVersioned(),
			MediaType: ocispec.MediaTypeImageManifest,
			Config:    cfg,
			Layers:    []ocispec.Descriptor{layer},
		}
		raw, err := json.Marshal(manifest)
		if err != nil {
			m.t.Fatal(err)
		}

		desc := m.putManifest(ocispec.MediaTypeImageManifest, "", raw)
		desc.Platform = &ocispec.Platform{OS: testOS, Architecture: arch, Variant: variant}
		return desc, layer
	}

	amd64, amd64Layer := build("amd64", "")
	arm64, _ := build("arm64", "v8")

	attestationLayer := m.putBlob([]byte(`{"_type":"https://in-toto.io/Statement/v0.1"}`))
	attestation := ocispec.Manifest{
		Versioned: ocispecVersioned(),
		MediaType: ocispec.MediaTypeImageManifest,
		Config:    ocispec.DescriptorEmptyJSON,
		Layers:    []ocispec.Descriptor{attestationLayer},
	}
	attRaw, err := json.Marshal(attestation)
	if err != nil {
		m.t.Fatal(err)
	}

	att := m.putManifest(ocispec.MediaTypeImageManifest, "", attRaw)
	att.Platform = &ocispec.Platform{OS: testUnknownOS, Architecture: testUnknownOS}
	att.Annotations = map[string]string{"vnd.docker.reference.type": "attestation-manifest"}

	idx := ocispec.Index{
		Versioned: ocispecVersioned(),
		MediaType: ocispec.MediaTypeImageIndex,
		Manifests: []ocispec.Descriptor{amd64, arm64, att},
	}
	idxRaw, err := json.Marshal(idx)
	if err != nil {
		m.t.Fatal(err)
	}

	index := m.putManifest(ocispec.MediaTypeImageIndex, "", idxRaw)
	m.tag("1.0.0", index.Digest)
	m.tag("latest", index.Digest)

	sigManifest := ocispec.Manifest{
		Versioned:    ocispecVersioned(),
		MediaType:    ocispec.MediaTypeImageManifest,
		ArtifactType: "application/vnd.example.signature.v1",
		Config:       ocispec.DescriptorEmptyJSON,
		Layers:       []ocispec.Descriptor{m.putBlob([]byte("signature payload"))},
		Subject:      &index,
	}
	sigRaw, err := json.Marshal(sigManifest)
	if err != nil {
		m.t.Fatal(err)
	}

	sig := m.putManifest(ocispec.MediaTypeImageManifest, "application/vnd.example.signature.v1", sigRaw)

	m.mu.Lock()
	m.referrers[index.Digest] = []ocispec.Descriptor{sig}
	m.mu.Unlock()

	return mockImage{index: index, amd64: amd64, arm64: arm64, amd64Layer: amd64Layer, signature: sig}
}

func ocispecVersioned() specs.Versioned {
	return specs.Versioned{SchemaVersion: 2}
}
