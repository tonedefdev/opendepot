package controller

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestHTTPStreamToFileRedactsCredentialsFromErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusForbidden)
	}))
	t.Cleanup(server.Close)

	requestURL := server.URL + "/provider.zip?X-Amz-Credential=secret#fragment"
	_, _, cleanup, err := httpStreamToFile(context.Background(), requestURL)
	t.Cleanup(cleanup)
	if err == nil {
		t.Fatal("httpStreamToFile() error = nil, want forbidden response error")
	}

	want := fmt.Sprintf("request to '%s/provider.zip' failed with status 403", server.URL)
	if err.Error() != want {
		t.Fatalf("httpStreamToFile() error = %q, want %q", err, want)
	}
}

func TestHTTPStreamToFileLimitedRejectsOversizeBody(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("TMPDIR", tmpDir)

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write(bytes.Repeat([]byte("a"), 17))
	}))
	t.Cleanup(server.Close)

	_, _, cleanup, err := httpStreamToFileLimited(context.Background(), server.URL+"/provider.zip", 16)
	t.Cleanup(cleanup)
	if !errors.Is(err, ErrProviderArchiveTooLarge) {
		t.Fatalf("httpStreamToFileLimited() error = %v, want ErrProviderArchiveTooLarge", err)
	}

	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}

	if len(entries) != 0 {
		t.Fatalf("temp files left behind after oversize download: %d", len(entries))
	}
}

func TestHTTPStreamToFileLimitedAcceptsBodyAtLimit(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	body := bytes.Repeat([]byte("a"), 16)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write(body)
	}))
	t.Cleanup(server.Close)

	path, checksum, cleanup, err := httpStreamToFileLimited(context.Background(), server.URL+"/provider.zip", 16)
	t.Cleanup(cleanup)
	if err != nil {
		t.Fatalf("httpStreamToFileLimited() error = %v, want nil", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	if !bytes.Equal(data, body) {
		t.Fatalf("downloaded body length = %d, want %d", len(data), len(body))
	}

	if want := fmt.Sprintf("%x", sha256.Sum256(body)); checksum != want {
		t.Fatalf("checksum = %q, want %q", checksum, want)
	}
}

func TestRedactedURLRemovesSensitiveComponents(t *testing.T) {
	if got := redactedURL("https://user:password@example.com/provider.zip?token=secret#fragment"); got != "https://example.com/provider.zip" {
		t.Fatalf("redactedURL() = %q, want redacted URL", got)
	}
	if got := redactedURL("https://%zz"); got != "<invalid URL>" {
		t.Fatalf("redactedURL() = %q, want invalid URL marker", got)
	}
}

func TestHTTPStreamToFileLimitedRejectsInvalidLimit(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("TMPDIR", tmpDir)

	for _, limit := range []int64{0, -1, math.MaxInt64} {
		_, _, cleanup, err := httpStreamToFileLimited(context.Background(), "http://127.0.0.1:1/provider.zip", limit)
		t.Cleanup(cleanup)
		if err == nil {
			t.Fatalf("httpStreamToFileLimited(limit=%d) error = nil, want invalid limit error", limit)
		}
	}

	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}

	if len(entries) != 0 {
		t.Fatalf("temp files left behind after invalid limit: %d", len(entries))
	}
}
