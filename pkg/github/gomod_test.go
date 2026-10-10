package github

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type goModStubTransport func(*http.Request) (*http.Response, error)

func (f goModStubTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestGetProviderGoModRejectsOversizeFile(t *testing.T) {
	previous := http.DefaultClient.Transport
	t.Cleanup(func() { http.DefaultClient.Transport = previous })

	http.DefaultClient.Transport = goModStubTransport(func(req *http.Request) (*http.Response, error) {
		body := strings.Repeat("a", int(maxGoModBytes)+1)
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	})

	_, err := GetProviderGoMod(context.Background(), nil, "example", "module", "1.0.0")
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("GetProviderGoMod() error = %v; want go.mod size error", err)
	}
}
