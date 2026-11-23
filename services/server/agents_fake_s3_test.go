package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
)

// agentTestFakeS3 is an httptest S3 endpoint that serves one object for every GET and records the request paths.
type agentTestFakeS3 struct {
	url      string
	mu       sync.Mutex
	paths    []string
	body     []byte
	checksum string
	failures int
}

// newAgentTestFakeS3 serves body with the x-amz-checksum-sha256 header set to checksum. The first failures GET
// requests return 403 AccessDenied, which the SDK does not retry. It points the S3 client at the endpoint through
// the environment; t.Setenv restores the variables on cleanup.
func newAgentTestFakeS3(t *testing.T, body []byte, checksum string, failures int) *agentTestFakeS3 {
	t.Helper()

	fake := &agentTestFakeS3{body: body, checksum: checksum, failures: failures}
	s3Server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fake.mu.Lock()
		fake.paths = append(fake.paths, r.URL.Path)
		fail := fake.failures > 0
		if fail {
			fake.failures--
		}
		fake.mu.Unlock()

		if fail {
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?><Error><Code>AccessDenied</Code><Message>Access Denied</Message></Error>`)

			return
		}

		w.Header().Set("x-amz-checksum-sha256", fake.checksum)
		_, _ = w.Write(fake.body)
	}))
	t.Cleanup(s3Server.Close)
	fake.url = s3Server.URL

	t.Setenv("AWS_ENDPOINT_URL_S3", s3Server.URL)
	t.Setenv("AWS_ACCESS_KEY_ID", "agent-test-access-key")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "agent-test-secret-key")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(t.TempDir(), "config"))
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(t.TempDir(), "credentials"))
	t.Setenv("OPENDEPOT_S3_USE_PATH_STYLE", "true")

	return fake
}

// requests returns the request paths received so far in arrival order.
func (f *agentTestFakeS3) requests() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]string(nil), f.paths...)
}
