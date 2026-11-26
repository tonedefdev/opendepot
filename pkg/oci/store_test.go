package oci

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/tonedefdev/opendepot/pkg/storage"
	"github.com/tonedefdev/opendepot/pkg/storage/types"
)

func newTestStore(t *testing.T) (*Store, string) {
	t.Helper()

	root := t.TempDir()
	store, err := NewStore(&storage.FileSystem{}, root)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	return store, root
}

func readAllClose(t *testing.T, rc io.ReadCloser) []byte {
	t.Helper()

	defer func() { _ = rc.Close() }()

	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	return data
}

func TestNewStoreRequiresBackend(t *testing.T) {
	if _, err := NewStore(nil, t.TempDir()); err == nil {
		t.Fatal("expected error for nil backend")
	}
}

func TestBlobRoundTripAndLayout(t *testing.T) {
	ctx := context.Background()
	store, root := newTestStore(t)
	content := []byte("layer bytes")
	d := digest.FromBytes(content)

	if err := store.PutBlob(ctx, "acme", "chart", d, bytes.NewReader(content)); err != nil {
		t.Fatalf("PutBlob: %v", err)
	}

	want := filepath.Join(root, "oci", "acme", "chart", "blobs", "sha256", d.Encoded())
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("blob not at content-addressed path %s: %v", want, err)
	}

	rc, err := store.GetBlob(ctx, "acme", "chart", d)
	if err != nil {
		t.Fatalf("GetBlob: %v", err)
	}

	if got := readAllClose(t, rc); !bytes.Equal(got, content) {
		t.Fatalf("GetBlob returned %q, want %q", got, content)
	}
}

func TestPutBlobIsIdempotent(t *testing.T) {
	ctx := context.Background()
	store, root := newTestStore(t)
	content := []byte("same bytes")
	d := digest.FromBytes(content)

	for i := range 2 {
		if err := store.PutBlob(ctx, "acme", "chart", d, bytes.NewReader(content)); err != nil {
			t.Fatalf("PutBlob attempt %d: %v", i, err)
		}
	}

	entries, err := os.ReadDir(filepath.Join(root, "oci", "acme", "chart", "blobs", "sha256"))
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("expected exactly one stored blob, found %d", len(entries))
	}
}

func TestPutBlobRejectsDigestMismatchBeforeWriting(t *testing.T) {
	ctx := context.Background()
	store, root := newTestStore(t)
	claimed := digest.FromBytes([]byte("what the caller claims"))

	err := store.PutBlob(ctx, "acme", "chart", claimed, bytes.NewReader([]byte("different bytes")))
	if !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("expected ErrDigestMismatch, got %v", err)
	}

	claimedPath := filepath.Join(root, "oci", "acme", "chart", "blobs", "sha256", claimed.Encoded())
	if _, statErr := os.Stat(claimedPath); !os.IsNotExist(statErr) {
		t.Fatalf("mismatched content must not reach storage, stat err=%v", statErr)
	}
}

func TestPutBlobReplacesCorruptExistingContent(t *testing.T) {
	ctx := context.Background()
	store, root := newTestStore(t)
	content := []byte("good bytes")
	d := digest.FromBytes(content)
	path := filepath.Join(root, "oci", "acme", "chart", "blobs", "sha256", d.Encoded())

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte("corrupt"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := store.PutBlob(ctx, "acme", "chart", d, bytes.NewReader(content)); err != nil {
		t.Fatalf("PutBlob over corrupt content: %v", err)
	}

	rc, err := store.GetBlob(ctx, "acme", "chart", d)
	if err != nil {
		t.Fatalf("GetBlob after repair: %v", err)
	}

	if got := readAllClose(t, rc); !bytes.Equal(got, content) {
		t.Fatalf("repaired blob = %q, want %q", got, content)
	}
}

func TestGetBlobRejectsStoredDigestMismatch(t *testing.T) {
	ctx := context.Background()
	store, root := newTestStore(t)
	content := []byte("original")
	d := digest.FromBytes(content)
	path := filepath.Join(root, "oci", "acme", "chart", "blobs", "sha256", d.Encoded())

	if err := store.PutBlob(ctx, "acme", "chart", d, bytes.NewReader(content)); err != nil {
		t.Fatalf("PutBlob: %v", err)
	}

	if err := os.WriteFile(path, []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}

	rc, err := store.GetBlob(ctx, "acme", "chart", d)
	if err != nil {
		t.Fatalf("GetBlob: %v", err)
	}

	_, err = io.ReadAll(rc)
	_ = rc.Close()
	if !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("expected ErrDigestMismatch on tampered blob, got %v", err)
	}
}

// fakeBackend serves a sequence of bodies, one per GetObject call, and counts the opens.
type fakeBackend struct {
	storage.FileSystem
	bodies [][]byte
	opens  int
}

func (f *fakeBackend) GetObject(_ context.Context, _ *types.StorageObjectInput) (io.Reader, error) {
	idx := min(f.opens, len(f.bodies)-1)
	f.opens++

	return bytes.NewReader(f.bodies[idx]), nil
}

func TestGetBlobOpensBackendOnceAndVerifiesReturnedStream(t *testing.T) {
	ctx := context.Background()
	good := []byte("original")
	d := digest.FromBytes(good)

	t.Run("second open returns different bytes", func(t *testing.T) {
		backend := &fakeBackend{bodies: [][]byte{good, []byte("tampered")}}
		store, err := NewStore(backend, "root")
		if err != nil {
			t.Fatalf("NewStore: %v", err)
		}

		rc, err := store.GetBlob(ctx, "acme", "chart", d)
		if err != nil {
			t.Fatalf("GetBlob: %v", err)
		}

		if got := readAllClose(t, rc); !bytes.Equal(got, good) {
			t.Fatalf("returned bytes %q, want %q", got, good)
		}

		if backend.opens != 1 {
			t.Fatalf("expected a single backend open, got %d", backend.opens)
		}
	})

	t.Run("tampered single stream is rejected at EOF", func(t *testing.T) {
		backend := &fakeBackend{bodies: [][]byte{[]byte("tampered")}}
		store, err := NewStore(backend, "root")
		if err != nil {
			t.Fatalf("NewStore: %v", err)
		}

		rc, err := store.GetBlob(ctx, "acme", "chart", d)
		if err != nil {
			t.Fatalf("GetBlob: %v", err)
		}

		_, err = io.ReadAll(rc)
		_ = rc.Close()
		if !errors.Is(err, ErrDigestMismatch) {
			t.Fatalf("expected ErrDigestMismatch, got %v", err)
		}

		if backend.opens != 1 {
			t.Fatalf("expected a single backend open, got %d", backend.opens)
		}
	})
}

func TestGetBlobNotFound(t *testing.T) {
	store, _ := newTestStore(t)
	d := digest.FromBytes([]byte("absent"))

	_, err := store.GetBlob(context.Background(), "acme", "chart", d)
	if !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("expected storage.ErrNotFound, got %v", err)
	}
}

func TestManifestRoundTripAndLayout(t *testing.T) {
	ctx := context.Background()
	store, root := newTestStore(t)
	manifest := []byte(`{"schemaVersion":2}`)

	d, err := store.PutManifest(ctx, "acme", "tool", manifest)
	if err != nil {
		t.Fatalf("PutManifest: %v", err)
	}

	if d != digest.FromBytes(manifest) {
		t.Fatalf("PutManifest digest = %s, want %s", d, digest.FromBytes(manifest))
	}

	if _, err := os.Stat(filepath.Join(root, "oci", "acme", "tool", "manifests", "sha256", d.Encoded())); err != nil {
		t.Fatalf("manifest not at content-addressed path: %v", err)
	}

	got, err := store.GetManifest(ctx, "acme", "tool", d)
	if err != nil {
		t.Fatalf("GetManifest: %v", err)
	}

	if !bytes.Equal(got, manifest) {
		t.Fatalf("GetManifest = %q, want %q", got, manifest)
	}
}

func TestGetManifestRejectsTampering(t *testing.T) {
	ctx := context.Background()
	store, root := newTestStore(t)
	manifest := []byte(`{"schemaVersion":2,"layers":[]}`)

	d, err := store.PutManifest(ctx, "acme", "tool", manifest)
	if err != nil {
		t.Fatalf("PutManifest: %v", err)
	}

	path := filepath.Join(root, "oci", "acme", "tool", "manifests", "sha256", d.Encoded())
	if err := os.WriteFile(path, []byte(`{"schemaVersion":2,"layers":["evil"]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := store.GetManifest(ctx, "acme", "tool", d); !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("expected ErrDigestMismatch for tampered manifest, got %v", err)
	}
}

func TestTagPointerRoundTrip(t *testing.T) {
	ctx := context.Background()
	store, root := newTestStore(t)
	d := digest.FromBytes([]byte("manifest"))
	pointer := TagPointer{
		Digest:       d.String(),
		MediaType:    "application/vnd.oci.image.index.v1+json",
		ArtifactType: "application/vnd.example.thing",
		UpdatedAt:    testTime(),
	}

	if err := store.PutTag(ctx, "acme", "tool", testVersion, pointer); err != nil {
		t.Fatalf("PutTag: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(root, "oci", "acme", "tool", "tags", testVersion))
	if err != nil {
		t.Fatalf("tag file not at tags/<tag>: %v", err)
	}

	for _, field := range []string{`"digest"`, `"mediaType"`, `"artifactType"`, `"updatedAt"`} {
		if !strings.Contains(string(raw), field) {
			t.Fatalf("tag pointer JSON missing %s: %s", field, raw)
		}
	}

	got, err := store.GetTag(ctx, "acme", "tool", testVersion)
	if err != nil {
		t.Fatalf("GetTag: %v", err)
	}

	if *got != pointer {
		t.Fatalf("GetTag = %+v, want %+v", *got, pointer)
	}
}

func TestPutTagOverwritesPointer(t *testing.T) {
	ctx := context.Background()
	store, _ := newTestStore(t)
	first := digest.FromBytes([]byte("one"))
	second := digest.FromBytes([]byte("two"))

	for _, d := range []digest.Digest{first, second} {
		if err := store.PutTag(ctx, "acme", "tool", "latest", TagPointer{Digest: d.String(), MediaType: "m"}); err != nil {
			t.Fatalf("PutTag: %v", err)
		}
	}

	got, err := store.GetTag(ctx, "acme", "tool", "latest")
	if err != nil {
		t.Fatalf("GetTag: %v", err)
	}

	if got.Digest != second.String() {
		t.Fatalf("tag digest = %s, want %s", got.Digest, second)
	}
}

func TestGetTagNotFound(t *testing.T) {
	store, _ := newTestStore(t)

	_, err := store.GetTag(context.Background(), "acme", "tool", "missing")
	if !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("expected storage.ErrNotFound, got %v", err)
	}
}

func TestPutTagValidatesPointer(t *testing.T) {
	ctx := context.Background()
	store, _ := newTestStore(t)

	if err := store.PutTag(ctx, "acme", "tool", "v1", TagPointer{Digest: "not-a-digest", MediaType: "m"}); err == nil {
		t.Fatal("expected error for malformed pointer digest")
	}

	if err := store.PutTag(ctx, "acme", "tool", "v1", TagPointer{Digest: digest.FromBytes(nil).String()}); err == nil {
		t.Fatal("expected error for missing media type")
	}
}

func TestNamesRejectPathTraversal(t *testing.T) {
	ctx := context.Background()
	store, root := newTestStore(t)
	valid := digest.FromBytes([]byte("x"))
	pointer := TagPointer{Digest: valid.String(), MediaType: "m"}

	badNames := []string{"", "..", ".", "../x", "x/../y", "a/b", `a\b`, ".hidden", "a..b", "a b", "a\x00b", "-lead"}
	for _, bad := range badNames {
		if err := store.PutBlob(ctx, bad, "repo", valid, bytes.NewReader(nil)); !errors.Is(err, ErrInvalidName) {
			t.Errorf("namespace %q: expected ErrInvalidName, got %v", bad, err)
		}

		if err := store.PutBlob(ctx, "ns", bad, valid, bytes.NewReader(nil)); !errors.Is(err, ErrInvalidName) {
			t.Errorf("repo %q: expected ErrInvalidName, got %v", bad, err)
		}

		if err := store.PutTag(ctx, "ns", "repo", bad, pointer); !errors.Is(err, ErrInvalidName) {
			t.Errorf("tag %q: expected ErrInvalidName, got %v", bad, err)
		}
	}

	for _, bad := range []string{"../../etc/passwd", "a/b", "-leading", ".hidden", "a..b"} {
		if _, err := store.GetTag(ctx, "ns", "repo", bad); !errors.Is(err, ErrInvalidName) {
			t.Errorf("GetTag %q: expected ErrInvalidName, got %v", bad, err)
		}
	}

	if _, err := os.Stat(filepath.Join(root, "etc")); !os.IsNotExist(err) {
		t.Fatalf("traversal attempt must not create paths outside the store, stat err=%v", err)
	}
}

func TestDigestsMustBeCanonicalSHA256(t *testing.T) {
	ctx := context.Background()
	store, _ := newTestStore(t)
	content := []byte("payload")
	good := digest.FromBytes(content)

	bad := []digest.Digest{
		digest.Digest("sha512:" + strings.Repeat("a", 128)),
		digest.Digest("sha256:" + strings.ToUpper(good.Encoded())),
		digest.Digest("sha256:../../" + strings.Repeat("a", 58)),
		digest.Digest("sha256:abc"),
	}
	for _, d := range bad {
		if err := store.PutBlob(ctx, "ns", "repo", d, bytes.NewReader(content)); !errors.Is(err, ErrInvalidName) {
			t.Errorf("digest %q: expected ErrInvalidName, got %v", d, err)
		}
	}
}

func TestValidNamesAreAccepted(t *testing.T) {
	ctx := context.Background()
	store, _ := newTestStore(t)
	content := []byte("ok")
	d := digest.FromBytes(content)

	if err := store.PutBlob(ctx, "my-namespace", "chart.v2_beta", d, bytes.NewReader(content)); err != nil {
		t.Fatalf("valid names rejected: %v", err)
	}

	if err := store.PutTag(ctx, "my-namespace", "chart.v2_beta", "1.0.0-rc.1", TagPointer{
		Digest:    d.String(),
		MediaType: "m",
	}); err != nil {
		t.Fatalf("valid tag rejected: %v", err)
	}
}

func TestPutAndGetBlobFromSeekerReadsFromStart(t *testing.T) {
	ctx := context.Background()
	store, _ := newTestStore(t)
	content := []byte("0123456789")
	d := digest.FromBytes(content)

	r := bytes.NewReader(content)
	if _, err := r.Seek(4, io.SeekStart); err != nil {
		t.Fatal(err)
	}

	if err := store.PutBlob(ctx, "ns", "repo", d, r); err != nil {
		t.Fatalf("PutBlob from mid-stream seeker: %v", err)
	}

	rc, err := store.GetBlob(ctx, "ns", "repo", d)
	if err != nil {
		t.Fatalf("GetBlob: %v", err)
	}

	if got := readAllClose(t, rc); !bytes.Equal(got, content) {
		t.Fatalf("stored %q, want full content %q", got, content)
	}
}
