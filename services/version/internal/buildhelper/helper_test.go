package buildhelper

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/opencontainers/go-digest"
	specs "github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/tonedefdev/opendepot/pkg/ocibuild"
)

const (
	testBuildPath = "/internal/builds/v1/opendepot-builds/app/abcdefghijklmnopqrst"
	testSHA       = "0123456789abcdef0123456789abcdef01234567"
	testPlatform  = "linux/amd64"
)

// newToken returns a valid build token.
func newToken(t *testing.T) string {
	t.Helper()
	token, err := ocibuild.NewToken()
	if err != nil {
		t.Fatal(err)
	}

	return token
}

// writeToken writes token to a file under dir and returns the path.
func writeToken(t *testing.T, dir, token string) string {
	t.Helper()
	path := filepath.Join(dir, "token")
	if err := os.WriteFile(path, []byte(token+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	return path
}

// githubTarball returns a gzip-compressed tar with the top-level directory that GitHub archives use.
func githubTarball(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		full := "owner-repo-" + testSHA + "/" + name
		if err := tw.WriteHeader(&tar.Header{Name: full, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}

		if _, err := tw.Write([]byte(body)); err != nil {
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

// stagedContext returns the context archive that StageContext produces and its digest.
func stagedContext(t *testing.T) ([]byte, digest.Digest) {
	t.Helper()
	src := githubTarball(t, map[string]string{
		"Dockerfile":   "FROM scratch\nCOPY src/main.txt /main.txt\n",
		"src/main.txt": "hello",
	})

	var out bytes.Buffer
	if err := ocibuild.StageContext(&out, bytes.NewReader(src), t.TempDir(), ocibuild.ContextSpec{}, ocibuild.Limits{}); err != nil {
		t.Fatal(err)
	}

	return out.Bytes(), digest.FromBytes(out.Bytes())
}

// writeLayout writes a valid single-manifest OCI directory layout for linux/amd64 under root, plus an ingest directory
// that the reader must ignore. It returns the manifest digest and the manifest bytes.
func writeLayout(t *testing.T, root string) (digest.Digest, []byte) {
	t.Helper()
	blobDir := filepath.Join(root, "blobs", "sha256")
	if err := os.MkdirAll(blobDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(filepath.Join(root, "ingest"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(root, "ingest", "partial"), []byte("junk"), 0o644); err != nil {
		t.Fatal(err)
	}

	put := func(data []byte) ocispec.Descriptor {
		d := digest.FromBytes(data)
		if err := os.WriteFile(filepath.Join(blobDir, d.Encoded()), data, 0o644); err != nil {
			t.Fatal(err)
		}

		return ocispec.Descriptor{Digest: d, Size: int64(len(data))}
	}

	configJSON, err := json.Marshal(ocispec.Image{
		Platform: ocispec.Platform{OS: "linux", Architecture: "amd64"},
		RootFS:   ocispec.RootFS{Type: "layers"},
	})
	if err != nil {
		t.Fatal(err)
	}

	config := put(configJSON)
	config.MediaType = ocispec.MediaTypeImageConfig
	layer := put([]byte("layer bytes"))
	layer.MediaType = ocispec.MediaTypeImageLayerGzip

	manifestJSON, err := json.Marshal(ocispec.Manifest{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ocispec.MediaTypeImageManifest,
		Config:    config,
		Layers:    []ocispec.Descriptor{layer},
	})
	if err != nil {
		t.Fatal(err)
	}

	manifest := put(manifestJSON)
	manifest.MediaType = ocispec.MediaTypeImageManifest
	manifest.Platform = &ocispec.Platform{OS: "linux", Architecture: "amd64"}

	indexJSON, err := json.Marshal(ocispec.Index{
		Versioned: specs.Versioned{SchemaVersion: 2},
		Manifests: []ocispec.Descriptor{manifest},
	})
	if err != nil {
		t.Fatal(err)
	}

	mustWrite := func(name string, data []byte) {
		if err := os.WriteFile(filepath.Join(root, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	mustWrite("oci-layout", []byte(`{"imageLayoutVersion":"1.0.0"}`))
	mustWrite("index.json", indexJSON)

	return manifest.Digest, manifestJSON
}

// fakeBroker is an httptest broker that records what the helpers send.
type fakeBroker struct {
	mu        sync.Mutex
	wantToken string
	context   []byte
	blobs     map[string][]byte
	manifests map[string][]byte
	auth      []string
	// storedDigest, when set, is returned as the manifest digest instead of the real one.
	storedDigest string
}

func (b *fakeBroker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.auth = append(b.auth, r.Header.Get("Authorization"))
	if r.Header.Get("Authorization") != "Bearer "+b.wantToken {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	rest := strings.TrimPrefix(r.URL.EscapedPath(), testBuildPath)
	switch {
	case r.Method == http.MethodGet && rest == "/context":
		_, _ = w.Write(b.context)
	case r.Method == http.MethodPut && strings.HasPrefix(rest, "/blobs/"):
		data, _ := io.ReadAll(r.Body)
		if b.blobs == nil {
			b.blobs = map[string][]byte{}
		}

		b.blobs[strings.TrimPrefix(rest, "/blobs/")] = data
		w.WriteHeader(http.StatusCreated)
	case r.Method == http.MethodPut && strings.HasPrefix(rest, "/manifests/"):
		data, _ := io.ReadAll(r.Body)
		if b.manifests == nil {
			b.manifests = map[string][]byte{}
		}

		d := digest.FromBytes(data)
		b.manifests[strings.TrimPrefix(rest, "/manifests/")] = data
		stored := d.String()
		if b.storedDigest != "" {
			stored = b.storedDigest
		}

		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"manifestDigest": stored})
	default:
		http.NotFound(w, r)
	}
}

func newBroker(t *testing.T, token string) (*fakeBroker, *httptest.Server) {
	t.Helper()
	b := &fakeBroker{wantToken: token}
	server := httptest.NewServer(b)
	t.Cleanup(server.Close)

	return b, server
}

func TestFetchVerifiesExtractsAndUsesTokenFile(t *testing.T) {
	token := newToken(t)
	archive, d := stagedContext(t)
	broker, server := newBroker(t, token)
	broker.context = archive

	workspace := t.TempDir()
	t.Setenv("BUILD_TOKEN", newToken(t))
	err := Run(context.Background(), Config{
		Mode:          ModeFetch,
		BuildURL:      server.URL + testBuildPath,
		TokenFile:     writeToken(t, t.TempDir(), token),
		ContextDigest: d.String(),
		Workspace:     workspace,
	})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}

	if got, err := os.ReadFile(filepath.Join(workspace, "context", "src", "main.txt")); err != nil || string(got) != "hello" {
		t.Fatalf("context file = %q, %v", got, err)
	}

	if _, err := os.Stat(filepath.Join(workspace, "dockerfile", "Dockerfile")); err != nil {
		t.Fatalf("dockerfile missing: %v", err)
	}

	entries, _ := os.ReadDir(workspace)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".context-") {
			t.Fatalf("spool file left behind: %s", e.Name())
		}
	}

	if len(broker.auth) != 1 || broker.auth[0] != "Bearer "+token {
		t.Fatalf("authorization headers = %v", broker.auth)
	}
}

func TestFetchRejectsDigestMismatchBeforeExtracting(t *testing.T) {
	token := newToken(t)
	archive, _ := stagedContext(t)
	broker, server := newBroker(t, token)
	broker.context = archive

	workspace := t.TempDir()
	wrong := digest.FromString("something else")
	err := Run(context.Background(), Config{
		Mode:          ModeFetch,
		BuildURL:      server.URL + testBuildPath,
		TokenFile:     writeToken(t, t.TempDir(), token),
		ContextDigest: wrong.String(),
		Workspace:     workspace,
	})
	if err == nil || !strings.Contains(err.Error(), "digest mismatch") {
		t.Fatalf("fetch error = %v, want digest mismatch", err)
	}

	if _, statErr := os.Stat(filepath.Join(workspace, "context")); !os.IsNotExist(statErr) {
		t.Fatalf("context extracted despite digest mismatch: %v", statErr)
	}
}

func TestFetchRejectsBadTokenAndBadInputs(t *testing.T) {
	token := newToken(t)
	archive, d := stagedContext(t)
	broker, server := newBroker(t, token)
	broker.context = archive

	cases := []struct {
		name string
		cfg  Config
	}{
		{
			name: "wrong token",
			cfg: Config{Mode: ModeFetch, BuildURL: server.URL + testBuildPath,
				TokenFile: writeToken(t, t.TempDir(), newToken(t)), ContextDigest: d.String()},
		},
		{
			name: "missing token file",
			cfg: Config{Mode: ModeFetch, BuildURL: server.URL + testBuildPath,
				TokenFile: filepath.Join(t.TempDir(), "absent"), ContextDigest: d.String()},
		},
		{
			name: "malformed token",
			cfg: Config{Mode: ModeFetch, BuildURL: server.URL + testBuildPath,
				TokenFile: writeToken(t, t.TempDir(), "not-a-token"), ContextDigest: d.String()},
		},
		{
			name: "non sha256 digest",
			cfg: Config{Mode: ModeFetch, BuildURL: server.URL + testBuildPath,
				TokenFile: writeToken(t, t.TempDir(), token), ContextDigest: "sha512:" + strings.Repeat("a", 128)},
		},
		{
			name: "relative build URL",
			cfg: Config{Mode: ModeFetch, BuildURL: "/internal/builds/v1/a/b/c",
				TokenFile: writeToken(t, t.TempDir(), token), ContextDigest: d.String()},
		},
		{
			name: "unknown mode",
			cfg: Config{Mode: "build", BuildURL: server.URL + testBuildPath,
				TokenFile: writeToken(t, t.TempDir(), token), ContextDigest: d.String()},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.cfg.Workspace = t.TempDir()
			if err := Run(context.Background(), tc.cfg); err == nil {
				t.Fatal("expected an error")
			} else if strings.Contains(err.Error(), token) {
				t.Fatal("error leaks the token")
			}
		})
	}
}

func TestUploadSendsBlobsAndManifestAndWritesTerminationMessage(t *testing.T) {
	token := newToken(t)
	broker, server := newBroker(t, token)

	layout := t.TempDir()
	wantDigest, manifestJSON := writeLayout(t, layout)
	termination := filepath.Join(t.TempDir(), "termination-log")

	t.Setenv("BUILD_TOKEN", newToken(t))
	err := Run(context.Background(), Config{
		Mode:               ModeUpload,
		BuildURL:           server.URL + testBuildPath,
		TokenFile:          writeToken(t, t.TempDir(), token),
		Layout:             layout,
		Platform:           testPlatform,
		TerminationLogPath: termination,
	})
	if err != nil {
		t.Fatalf("upload: %v", err)
	}

	if len(broker.blobs) != 2 {
		t.Fatalf("uploaded %d blobs, want 2 (config and layer; ingest ignored)", len(broker.blobs))
	}

	for d, data := range broker.blobs {
		if digest.FromBytes(data).String() != d {
			t.Fatalf("blob %s has mismatched content", d)
		}
	}

	got, ok := broker.manifests["linux%2Famd64"]
	if !ok || !bytes.Equal(got, manifestJSON) {
		t.Fatalf("manifest for linux/amd64 was not uploaded as sent")
	}

	msg, err := os.ReadFile(termination)
	if err != nil {
		t.Fatal(err)
	}

	var out struct {
		ManifestDigest string `json:"manifestDigest"`
	}
	if err := json.Unmarshal(msg, &out); err != nil {
		t.Fatalf("termination message is not JSON: %v", err)
	}

	if out.ManifestDigest != wantDigest.String() {
		t.Fatalf("termination manifestDigest = %s, want %s", out.ManifestDigest, wantDigest)
	}
}

func TestUploadRejectsWrongTokenAndBadLayouts(t *testing.T) {
	token := newToken(t)
	_, server := newBroker(t, token)
	layout := t.TempDir()
	writeLayout(t, layout)

	t.Run("broker rejects the token", func(t *testing.T) {
		termination := filepath.Join(t.TempDir(), "termination-log")
		err := Run(context.Background(), Config{
			Mode:               ModeUpload,
			BuildURL:           server.URL + testBuildPath,
			TokenFile:          writeToken(t, t.TempDir(), newToken(t)),
			Layout:             layout,
			Platform:           testPlatform,
			TerminationLogPath: termination,
		})
		if err == nil || !strings.Contains(err.Error(), "401") {
			t.Fatalf("upload error = %v, want 401", err)
		}

		if _, statErr := os.Stat(termination); !os.IsNotExist(statErr) {
			t.Fatal("termination message written after a failed upload")
		}
	})

	t.Run("layout platform mismatch", func(t *testing.T) {
		err := Run(context.Background(), Config{
			Mode:      ModeUpload,
			BuildURL:  server.URL + testBuildPath,
			TokenFile: writeToken(t, t.TempDir(), token),
			Layout:    layout,
			Platform:  "linux/arm64",
		})
		if err == nil {
			t.Fatal("expected a platform mismatch error")
		}
	})

	t.Run("unsupported platform", func(t *testing.T) {
		err := Run(context.Background(), Config{
			Mode:      ModeUpload,
			BuildURL:  server.URL + testBuildPath,
			TokenFile: writeToken(t, t.TempDir(), token),
			Layout:    layout,
			Platform:  "windows/amd64",
		})
		if err == nil {
			t.Fatal("expected an unsupported platform error")
		}
	})

	t.Run("broker stores a different digest", func(t *testing.T) {
		broker, mismatched := newBroker(t, token)
		broker.storedDigest = digest.FromString("other").String()
		err := Run(context.Background(), Config{
			Mode:               ModeUpload,
			BuildURL:           mismatched.URL + testBuildPath,
			TokenFile:          writeToken(t, t.TempDir(), token),
			Layout:             layout,
			Platform:           testPlatform,
			TerminationLogPath: filepath.Join(t.TempDir(), "termination-log"),
		})
		if err == nil || !strings.Contains(err.Error(), "broker stored") {
			t.Fatalf("upload error = %v, want a stored digest mismatch", err)
		}
	})
}
