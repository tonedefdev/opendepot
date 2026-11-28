package mcp

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestFetchTarballReturnsBody(t *testing.T) {
	server, client := newRegistryServer(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Accept"); got != "application/octet-stream" {
			t.Errorf("Accept = %q, want application/octet-stream", got)
		}

		_, _ = w.Write([]byte("tarball-bytes"))
	})

	body, err := client.FetchTarball(context.Background(), server.URL+"/pkg/-/pkg-1.0.0.tgz")
	if err != nil {
		t.Fatalf("FetchTarball() error = %v", err)
	}

	if string(body) != "tarball-bytes" {
		t.Fatalf("body = %q, want tarball-bytes", body)
	}
}

func TestFetchTarballRejectsOversizedBody(t *testing.T) {
	server, client := newRegistryServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", maxTarballBytes+1)))
	})

	_, err := client.FetchTarball(context.Background(), server.URL+"/pkg/-/pkg-1.0.0.tgz")
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("FetchTarball() error = %v, want size bound error", err)
	}
}

func TestFetchTarballDeniesUnlistedHost(t *testing.T) {
	al, err := NewAllowList([]string{testAllowedHost})
	if err != nil {
		t.Fatalf("NewAllowList() error = %v", err)
	}

	client := NewClient(al)
	if _, err := client.FetchTarball(context.Background(), testUnlistedHost); !errors.Is(err, ErrDenied) {
		t.Fatalf("FetchTarball() error = %v, want ErrDenied", err)
	}
}
