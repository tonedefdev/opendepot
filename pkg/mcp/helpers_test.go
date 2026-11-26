package mcp

import (
	"net"
	"net/url"
	"testing"
)

// Shared literals for the package tests. Keeping them here avoids repeated string constants across test files.
const (
	testDigest        = "sha256:4d0c5a3e1f9b2c7d8e6f0a1b2c3d4e5f60718293a4b5c6d7e8f9011223344556"
	testSHA256        = "4d0c5a3e1f9b2c7d8e6f0a1b2c3d4e5f60718293a4b5c6d7e8f9011223344556"
	testVersion       = "1.0.0"
	testCP38          = "cp38"
	testNode          = "node"
	testPython        = ".opendepot/mcp/summarizer/.venv/bin/python"
	testEntryFile     = "dist/index.js"
	testImageRef      = "opendepot.example.com/mcp/search"
	testRemoteURL     = "http://127.0.0.1:8080/mcp"
	testShellError    = "is a shell"
	testPackage       = "example-tool"
	testNPMTool       = "tool"
	testPyPIIndex     = "https://pypi.org/simple"
	testDerivedJSON   = "https://pypi.org/pypi/x/json"
	testAllowedHost   = "registry.npmjs.org"
	testUnlistedHost  = "https://evil.example.net/pkg"
	testUnlistedIndex = "https://evil.example.net/simple"
)

// hostPort returns host:port for an httptest server URL, which is the form the allow-list takes.
func hostPort(t *testing.T, rawURL string) string {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse %q: %v", rawURL, err)
	}

	host, port, err := net.SplitHostPort(u.Host)
	if err != nil || host == "" {
		t.Fatalf("split %q: %v", u.Host, err)
	}

	return host + ":" + port
}
