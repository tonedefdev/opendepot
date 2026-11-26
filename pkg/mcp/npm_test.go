package mcp

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

// newRegistryServer serves the handler and returns it with a client that trusts only that loopback server.
func newRegistryServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *Client) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	al, err := NewAllowList([]string{hostPort(t, server.URL)})
	if err != nil {
		t.Fatalf("NewAllowList() error = %v", err)
	}

	return server, newClient(al, allowLoopbackOnly, true)
}

func TestResolveNPMSelectsVersions(t *testing.T) {
	const scoped = "@scope/" + testNPMTool
	var escapedPath string
	var server *httptest.Server
	server, client := newRegistryServer(t, func(w http.ResponseWriter, r *http.Request) {
		escapedPath = r.URL.EscapedPath()
		base := "http://" + r.Host
		_, _ = fmt.Fprintf(w, `{
  "name": %[1]q,
  "dist-tags": {"latest": "1.1.0", "beta": "2.0.0-beta.1"},
  "versions": {
    "1.0.0": {
      "version": "1.0.0", "bin": {"tool": "bin/tool.js"},
      "dist": {"tarball": "%[2]s/t/tool-1.0.0.tgz", "integrity": "sha512-AAA", "shasum": "aa"}
    },
    "1.1.0": {
      "version": "1.1.0", "bin": "bin/cli.js", "main": "index.js", "os": ["linux"], "cpu": ["x64"],
      "dist": {"tarball": "%[2]s/t/tool-1.1.0.tgz", "integrity": "sha512-BBB"}
    },
    "2.0.0-beta.1": {
      "version": "2.0.0-beta.1", "hasInstallScript": true,
      "dist": {"tarball": "%[2]s/t/tool-2.0.0-beta.1.tgz", "shasum": "bb"}
    }
  }
}`, scoped, base)
	})

	ctx := context.Background()
	latest, err := client.ResolveNPM(ctx, server.URL, scoped, "")
	if err != nil {
		t.Fatalf("ResolveNPM(latest) error = %v", err)
	}

	if escapedPath != "/@scope%2F"+testNPMTool {
		t.Errorf("packument path = %q, want the scoped name escaped as one segment", escapedPath)
	}

	if latest.Version != "1.1.0" || latest.Integrity != "sha512-BBB" {
		t.Errorf("latest = %+v, want version 1.1.0 with its integrity", latest)
	}

	if latest.Bin[testNPMTool] != "bin/cli.js" || latest.Main != "index.js" {
		t.Errorf("string bin not normalized to the unscoped name: bin=%v main=%q", latest.Bin, latest.Main)
	}

	if !slices.Equal(latest.OS, []string{"linux"}) || !slices.Equal(latest.CPU, []string{"x64"}) {
		t.Errorf("os/cpu = %v/%v, want linux/x64", latest.OS, latest.CPU)
	}

	exact, err := client.ResolveNPM(ctx, server.URL, scoped, testVersion)
	if err != nil {
		t.Fatalf("ResolveNPM(1.0.0) error = %v", err)
	}

	if exact.Bin[testNPMTool] != "bin/tool.js" || exact.Shasum != "aa" {
		t.Errorf("exact = %+v, want the object bin and shasum", exact)
	}

	beta, err := client.ResolveNPM(ctx, server.URL, scoped, "beta")
	if err != nil {
		t.Fatalf("ResolveNPM(beta) error = %v", err)
	}

	if beta.Version != "2.0.0-beta.1" || !beta.HasInstallScript {
		t.Errorf("dist-tag beta = %+v, want 2.0.0-beta.1 with hasInstallScript", beta)
	}

	versions, err := client.NPMVersions(ctx, server.URL, scoped)
	if err != nil {
		t.Fatalf("NPMVersions() error = %v", err)
	}

	if !slices.Equal(versions, []string{testVersion, "1.1.0", "2.0.0-beta.1"}) {
		t.Errorf("NPMVersions() = %v, want the sorted published versions", versions)
	}
}

func TestResolveNPMRejects(t *testing.T) {
	tests := []struct {
		name    string
		pkg     string
		body    string
		version string
		wantErr string
	}{
		{
			name: "unknown version",
			pkg:  testNPMTool,
			body: `{"name": %[2]q, "dist-tags": {"latest": "1.0.0"}, "versions": {"1.0.0": {"version": "1.0.0",
  "dist": {"tarball": "%[1]s/t.tgz", "shasum": "aa"}}}}`,
			version: "9.9.9",
			wantErr: ErrVersionNotFound.Error(),
		},
		{
			name:    "packument name mismatch",
			pkg:     testNPMTool,
			body:    `{"name": "other", "dist-tags": {"latest": "1.0.0"}, "versions": {}}`,
			wantErr: "does not match",
		},
		{
			name: "tarball on an unlisted host",
			pkg:  testNPMTool,
			body: `{"name": %[2]q, "dist-tags": {"latest": "1.0.0"}, "versions": {"1.0.0": {"version": "1.0.0",
  "dist": {"tarball": "https://evil.example.net/t.tgz", "shasum": "aa"}}}}`,
			wantErr: "not on the allow-list",
		},
		{
			name: "no integrity or shasum",
			pkg:  testNPMTool,
			body: `{"name": %[2]q, "dist-tags": {"latest": "1.0.0"}, "versions": {"1.0.0": {"version": "1.0.0",
  "dist": {"tarball": "%[1]s/t.tgz"}}}}`,
			wantErr: "no integrity or shasum",
		},
		{
			name: "bin escapes the package",
			pkg:  testNPMTool,
			body: `{"name": %[2]q, "dist-tags": {"latest": "1.0.0"}, "versions": {"1.0.0": {"version": "1.0.0",
  "bin": {"tool": "../../etc/x"}, "dist": {"tarball": "%[1]s/t.tgz", "shasum": "aa"}}}}`,
			wantErr: "must be a local path",
		},
		{
			name: "absolute main",
			pkg:  testNPMTool,
			body: `{"name": %[2]q, "dist-tags": {"latest": "1.0.0"}, "versions": {"1.0.0": {"version": "1.0.0",
  "main": "/usr/bin/sh", "dist": {"tarball": "%[1]s/t.tgz", "shasum": "aa"}}}}`,
			wantErr: "must be a local path",
		},
		{
			name:    "invalid package name never reaches the registry",
			pkg:     "../escape",
			body:    `{}`,
			wantErr: "invalid npm package name",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server, client := newRegistryServer(t, func(w http.ResponseWriter, r *http.Request) {
				body := strings.NewReplacer("%[1]s", "http://"+r.Host, "%[2]q", `"`+testNPMTool+`"`).Replace(tc.body)
				_, _ = fmt.Fprint(w, body)
			})

			_, err := client.ResolveNPM(context.Background(), server.URL, tc.pkg, tc.version)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("ResolveNPM() error = %v, want it to contain %q", err, tc.wantErr)
			}
		})
	}
}

func TestResolveNPMRejectsHTTPError(t *testing.T) {
	server, client := newRegistryServer(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "missing", http.StatusNotFound)
	})

	if _, err := client.ResolveNPM(context.Background(), server.URL, testNPMTool, ""); err == nil {
		t.Error("ResolveNPM() accepted a 404 packument")
	}
}
