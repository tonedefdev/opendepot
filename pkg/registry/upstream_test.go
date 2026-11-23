package registry

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
)

// routeTransport answers requests from an in-memory route table so tests never reach
// the real OpenTofu or Terraform registries.
type routeTransport struct {
	routes map[string]func(*http.Request) *http.Response
	seen   []string
}

func (rt *routeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	rt.seen = append(rt.seen, req.URL.String())
	if handler, ok := rt.routes[req.URL.String()]; ok {
		return handler(req), nil
	}

	return fakeResponse(http.StatusNotFound, "not found"), nil
}

func fakeResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

// useRouteTransport swaps http.DefaultTransport, which the registry client uses, for the test duration.
func useRouteTransport(t *testing.T, routes map[string]func(*http.Request) *http.Response) *routeTransport {
	t.Helper()

	rt := &routeTransport{routes: routes}
	original := http.DefaultTransport
	http.DefaultTransport = rt
	t.Cleanup(func() {
		http.DefaultTransport = original
	})

	return rt
}

func TestListProviderVersions(t *testing.T) {
	const versionsURL = "https://registry.opentofu.org/v1/providers/hashicorp/random/versions"

	t.Run("returns trimmed non-empty versions", func(t *testing.T) {
		useRouteTransport(t, map[string]func(*http.Request) *http.Response{
			versionsURL: func(*http.Request) *http.Response {
				return fakeResponse(http.StatusOK, `{"versions":[{"version":"3.5.0"},{"version":" "},{"version":" 3.6.0 "}]}`)
			},
		})

		got, err := ListProviderVersions(context.Background(), "", "HashiCorp", "Random")
		if err != nil {
			t.Fatalf("ListProviderVersions returned error: %v", err)
		}

		if len(got) != 2 || got[0] != "3.5.0" || got[1] != "3.6.0" {
			t.Fatalf("ListProviderVersions = %v, want [3.5.0 3.6.0]", got)
		}
	})

	t.Run("defaults blank namespace to hashicorp", func(t *testing.T) {
		rt := useRouteTransport(t, map[string]func(*http.Request) *http.Response{
			versionsURL: func(*http.Request) *http.Response {
				return fakeResponse(http.StatusOK, `{"versions":[]}`)
			},
		})

		got, err := ListProviderVersions(context.Background(), "", "  ", "random")
		if err != nil {
			t.Fatalf("ListProviderVersions returned error: %v", err)
		}

		if len(got) != 0 {
			t.Fatalf("ListProviderVersions = %v, want empty", got)
		}

		if len(rt.seen) != 1 || rt.seen[0] != versionsURL {
			t.Fatalf("requested %v, want [%s]", rt.seen, versionsURL)
		}
	})

	t.Run("wraps upstream status errors", func(t *testing.T) {
		useRouteTransport(t, map[string]func(*http.Request) *http.Response{
			versionsURL: func(*http.Request) *http.Response {
				return fakeResponse(http.StatusInternalServerError, "boom")
			},
		})

		_, err := ListProviderVersions(context.Background(), "", "hashicorp", "random")
		if err == nil || !strings.Contains(err.Error(), "failed to list provider versions for hashicorp/random") {
			t.Fatalf("ListProviderVersions error = %v, want wrapped listing error", err)
		}
	})

	t.Run("rejects unsupported upstream registry", func(t *testing.T) {
		_, err := ListProviderVersions(context.Background(), "registry.example.com", "hashicorp", "random")
		if err == nil {
			t.Fatal("ListProviderVersions returned no error for unsupported registry")
		}
	})
}

func TestLookupProviderDownload(t *testing.T) {
	const downloadURL = "https://registry.terraform.io/v1/providers/hashicorp/random/3.6.0/download/linux/amd64"

	t.Run("returns download metadata and strips version prefix", func(t *testing.T) {
		rt := useRouteTransport(t, map[string]func(*http.Request) *http.Response{
			downloadURL: func(*http.Request) *http.Response {
				return fakeResponse(http.StatusOK, `{"download_url":"https://example.com/p.zip","filename":"p.zip","shasum":"abc"}`)
			},
		})

		got, err := LookupProviderDownload(context.Background(), opendepotv1alpha1.TerraformRegistryHost,
			"HashiCorp", "Random", "v3.6.0", "Linux", "AMD64")
		if err != nil {
			t.Fatalf("LookupProviderDownload returned error: %v", err)
		}

		if got.DownloadURL != "https://example.com/p.zip" || got.Filename != "p.zip" || got.Shasum != "abc" {
			t.Fatalf("LookupProviderDownload = %+v, unexpected metadata", got)
		}

		if len(rt.seen) != 1 || rt.seen[0] != downloadURL {
			t.Fatalf("requested %v, want [%s]", rt.seen, downloadURL)
		}
	})

	t.Run("rejects empty download url", func(t *testing.T) {
		useRouteTransport(t, map[string]func(*http.Request) *http.Response{
			downloadURL: func(*http.Request) *http.Response {
				return fakeResponse(http.StatusOK, `{"download_url":"  ","filename":"p.zip"}`)
			},
		})

		_, err := LookupProviderDownload(context.Background(), opendepotv1alpha1.TerraformRegistryHost,
			"hashicorp", "random", "3.6.0", "linux", "amd64")
		if err == nil || !strings.Contains(err.Error(), "empty download_url") {
			t.Fatalf("LookupProviderDownload error = %v, want empty download_url error", err)
		}
	})

	t.Run("wraps lookup failures", func(t *testing.T) {
		useRouteTransport(t, map[string]func(*http.Request) *http.Response{})

		_, err := LookupProviderDownload(context.Background(), opendepotv1alpha1.TerraformRegistryHost,
			"hashicorp", "random", "3.6.0", "linux", "amd64")
		if err == nil || !strings.Contains(err.Error(), "provider download lookup for hashicorp/random@3.6.0") {
			t.Fatalf("LookupProviderDownload error = %v, want wrapped lookup error", err)
		}
	})

	t.Run("rejects unsupported upstream registry", func(t *testing.T) {
		_, err := LookupProviderDownload(context.Background(), "registry.example.com",
			"hashicorp", "random", "3.6.0", "linux", "amd64")
		if err == nil {
			t.Fatal("LookupProviderDownload returned no error for unsupported registry")
		}
	})
}

func TestLookupProviderRepo(t *testing.T) {
	const docsURL = "https://api.opentofu.org/registry/docs/providers/hashicorp/random/index.json"

	t.Run("returns trimmed link", func(t *testing.T) {
		useRouteTransport(t, map[string]func(*http.Request) *http.Response{
			docsURL: func(*http.Request) *http.Response {
				return fakeResponse(http.StatusOK, `{"link":"  https://github.com/hashicorp/terraform-provider-random  "}`)
			},
		})

		got, err := LookupProviderRepo(context.Background(), "HashiCorp", "Random")
		if err != nil {
			t.Fatalf("LookupProviderRepo returned error: %v", err)
		}

		if got != "https://github.com/hashicorp/terraform-provider-random" {
			t.Fatalf("LookupProviderRepo = %q", got)
		}
	})

	t.Run("rejects empty link", func(t *testing.T) {
		useRouteTransport(t, map[string]func(*http.Request) *http.Response{
			docsURL: func(*http.Request) *http.Response {
				return fakeResponse(http.StatusOK, `{"link":""}`)
			},
		})

		_, err := LookupProviderRepo(context.Background(), "hashicorp", "random")
		if err == nil || !strings.Contains(err.Error(), "empty link") {
			t.Fatalf("LookupProviderRepo error = %v, want empty link error", err)
		}
	})

	t.Run("wraps lookup failures", func(t *testing.T) {
		useRouteTransport(t, map[string]func(*http.Request) *http.Response{})

		_, err := LookupProviderRepo(context.Background(), "hashicorp", "random")
		if err == nil || !strings.Contains(err.Error(), "opentofu registry lookup for hashicorp/random failed") {
			t.Fatalf("LookupProviderRepo error = %v, want wrapped lookup error", err)
		}
	})
}

func TestHTTPGetJSON(t *testing.T) {
	t.Run("reports invalid JSON", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, "{not json")
		}))
		t.Cleanup(srv.Close)

		var out ProviderVersionsResponse
		err := HTTPGetJSON(context.Background(), srv.URL, &out)
		if err == nil || !strings.Contains(err.Error(), "unable to parse JSON") {
			t.Fatalf("HTTPGetJSON error = %v, want parse error", err)
		}
	})

	t.Run("reports transport failures", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		url := srv.URL
		srv.Close()

		var out ProviderVersionsResponse
		err := HTTPGetJSON(context.Background(), url, &out)
		if err == nil || !strings.Contains(err.Error(), "request failed") {
			t.Fatalf("HTTPGetJSON error = %v, want request failure", err)
		}
	})

	t.Run("reports invalid request URL", func(t *testing.T) {
		var out ProviderVersionsResponse
		err := HTTPGetJSON(context.Background(), "://bad url", &out)
		if err == nil {
			t.Fatal("HTTPGetJSON returned no error for invalid URL")
		}
	})

	t.Run("decodes successful responses", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, `{"link":"https://example.com"}`)
		}))
		t.Cleanup(srv.Close)

		var out ProviderDocsResponse
		if err := HTTPGetJSON(context.Background(), srv.URL, &out); err != nil {
			t.Fatalf("HTTPGetJSON returned error: %v", err)
		}

		if out.Link != "https://example.com" {
			t.Fatalf("HTTPGetJSON link = %q", out.Link)
		}
	})
}
