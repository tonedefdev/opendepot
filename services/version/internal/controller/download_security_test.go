package controller

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
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

func TestRedactedURLRemovesSensitiveComponents(t *testing.T) {
	if got := redactedURL("https://user:password@example.com/provider.zip?token=secret#fragment"); got != "https://example.com/provider.zip" {
		t.Fatalf("redactedURL() = %q, want redacted URL", got)
	}
	if got := redactedURL("https://%zz"); got != "<invalid URL>" {
		t.Fatalf("redactedURL() = %q, want invalid URL marker", got)
	}
}
