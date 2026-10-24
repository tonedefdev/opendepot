package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"
)

func newCLIDownloadTestRouter(t *testing.T, distDir string) http.Handler {
	t.Helper()

	dir := distDir
	opendepotCLIDistDir = &dir

	r := chi.NewRouter()
	r.Get("/opendepot/cli/v1/SHA256SUMS", serveCLIChecksums)
	r.Get("/opendepot/cli/v1/{os}/{arch}", serveCLIBinary)
	return r
}

func TestServeCLIBinaryServesKnownTarget(t *testing.T) {
	dir := t.TempDir()
	want := []byte("binary-bytes")
	if err := os.WriteFile(filepath.Join(dir, "opendepot_linux_amd64"), want, 0o755); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/opendepot/cli/v1/linux/amd64", nil)
	newCLIDownloadTestRouter(t, dir).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	if got := rec.Body.String(); got != string(want) {
		t.Fatalf("body = %q, want %q", got, want)
	}

	if got := rec.Header().Get("Content-Disposition"); got != `attachment; filename="opendepot_linux_amd64"` {
		t.Fatalf("Content-Disposition = %q", got)
	}

	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want nosniff", got)
	}
}

func TestServeCLIBinaryWindowsUsesExeSuffix(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "opendepot_windows_amd64.exe"), []byte("exe"), 0o755); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/opendepot/cli/v1/windows/amd64", nil)
	newCLIDownloadTestRouter(t, dir).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestServeCLIBinaryRejectsUnknownTarget(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "opendepot_freebsd_amd64"), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{
		"/opendepot/cli/v1/freebsd/amd64",
		"/opendepot/cli/v1/linux/386",
		"/opendepot/cli/v1/..%2Fetc/passwd",
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		newCLIDownloadTestRouter(t, dir).ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s: status = %d, want %d", path, rec.Code, http.StatusNotFound)
		}
	}
}

func TestServeCLIBinaryNotFoundWhenArtifactsAbsent(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/opendepot/cli/v1/linux/arm64", nil)
	newCLIDownloadTestRouter(t, t.TempDir()).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestServeCLIChecksums(t *testing.T) {
	dir := t.TempDir()
	want := "abc123  opendepot_linux_amd64\n"
	if err := os.WriteFile(filepath.Join(dir, "SHA256SUMS"), []byte(want), 0o644); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/opendepot/cli/v1/SHA256SUMS", nil)
	newCLIDownloadTestRouter(t, dir).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	if got := rec.Body.String(); got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}
