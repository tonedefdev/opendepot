package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	"github.com/tonedefdev/opendepot/pkg/hclschema/assemblyexport"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// covGNonFlushingWriter hides the Flush method of the wrapped recorder so streaming can be refused.
type covGNonFlushingWriter struct {
	http.ResponseWriter
}

func covGAssemblyExportRequest(t *testing.T, body string, headers map[string]string) *http.Request {
	t.Helper()

	request := httptest.NewRequest(http.MethodPost, "http://registry.example.com/opendepot/ui/assembly/export", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		request.Header.Set(key, value)
	}

	return request
}

func covGDecodeAssemblyError(t *testing.T, response *httptest.ResponseRecorder) assemblyexport.ErrorResponse {
	t.Helper()

	var body assemblyexport.ErrorResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode assembly error body %q: %v", response.Body.String(), err)
	}

	return body
}

func TestCovGHandleAssemblyExportRejectsBadRequests(t *testing.T) {
	previousMaxBytes := opendepotAssemblyMaxRequestBytes
	previousMaxNodes := opendepotAssemblyMaxNodes
	t.Cleanup(func() {
		opendepotAssemblyMaxRequestBytes = previousMaxBytes
		opendepotAssemblyMaxNodes = previousMaxNodes
	})

	maxBytes := int64(2 << 20)
	maxNodes := 100
	opendepotAssemblyMaxRequestBytes = &maxBytes
	opendepotAssemblyMaxNodes = &maxNodes

	t.Run("malformed json is a bad request", func(t *testing.T) {
		response := httptest.NewRecorder()
		handleAssemblyExport(response, covGAssemblyExportRequest(t, "{not json", nil))

		if response.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
		}

		if body := covGDecodeAssemblyError(t, response); body.Error != "invalid_request" {
			t.Fatalf("error = %q, want invalid_request", body.Error)
		}

		if got := response.Header().Get("Cache-Control"); got != "no-store" {
			t.Fatalf("Cache-Control = %q, want no-store", got)
		}
	})

	t.Run("unknown fields are rejected", func(t *testing.T) {
		response := httptest.NewRecorder()
		handleAssemblyExport(response, covGAssemblyExportRequest(t, `{"schemaVersion":"x","surprise":true}`, nil))

		if response.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
		}
	})

	t.Run("oversized body is too large", func(t *testing.T) {
		limit := int64(16)
		opendepotAssemblyMaxRequestBytes = &limit
		t.Cleanup(func() { opendepotAssemblyMaxRequestBytes = &maxBytes })

		response := httptest.NewRecorder()
		handleAssemblyExport(response, covGAssemblyExportRequest(t, `{"schemaVersion":"assembly.v1","variables":[]}`, nil))

		if response.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusRequestEntityTooLarge)
		}

		if body := covGDecodeAssemblyError(t, response); body.Error != "request_too_large" {
			t.Fatalf("error = %q, want request_too_large", body.Error)
		}
	})

	t.Run("trailing data after the document is rejected", func(t *testing.T) {
		response := httptest.NewRecorder()
		handleAssemblyExport(response, covGAssemblyExportRequest(t, `{"schemaVersion":"assembly.v1"} {"again":1}`, nil))

		if response.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
		}

		if body := covGDecodeAssemblyError(t, response); !strings.Contains(body.Message, "trailing data") {
			t.Fatalf("message = %q, want trailing data", body.Message)
		}
	})

	t.Run("node count above the limit is rejected", func(t *testing.T) {
		limit := 1
		opendepotAssemblyMaxNodes = &limit
		t.Cleanup(func() { opendepotAssemblyMaxNodes = &maxNodes })

		body := `{"schemaVersion":"assembly.v1","modules":[{"nodeId":"a"},{"nodeId":"b"}]}`
		response := httptest.NewRecorder()
		handleAssemblyExport(response, covGAssemblyExportRequest(t, body, nil))

		if response.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusRequestEntityTooLarge)
		}

		if decoded := covGDecodeAssemblyError(t, response); decoded.Error != "too_many_nodes" {
			t.Fatalf("error = %q, want too_many_nodes", decoded.Error)
		}
	})
}

func TestCovGHandleAssemblyExportResolution(t *testing.T) {
	previousMaxBytes := opendepotAssemblyMaxRequestBytes
	previousMaxNodes := opendepotAssemblyMaxNodes
	previousAnonymous := opendepotAnonymousAuth
	previousRegistryHost := opendepotRegistryHost
	t.Cleanup(func() {
		opendepotAssemblyMaxRequestBytes = previousMaxBytes
		opendepotAssemblyMaxNodes = previousMaxNodes
		opendepotAnonymousAuth = previousAnonymous
		opendepotRegistryHost = previousRegistryHost
	})

	maxBytes := int64(2 << 20)
	maxNodes := 100
	anonymous := true
	registryHost := "registry.example.com"
	opendepotAssemblyMaxRequestBytes = &maxBytes
	opendepotAssemblyMaxNodes = &maxNodes
	opendepotAnonymousAuth = &anonymous
	opendepotRegistryHost = &registryHost

	module := opendepotv1alpha1.Module{
		ObjectMeta: metav1.ObjectMeta{Name: "kms", Namespace: "team"},
	}
	namespace := map[string]any{
		"apiVersion": "v1",
		"kind":       "Namespace",
		"metadata":   map[string]any{"name": "team"},
	}

	api := covGAPIServer(t, map[string]covGResponse{
		"/apis/opendepot.defdev.io/v1alpha1/namespaces/team/modules/kms": {body: module},
		"/apis/opendepot.defdev.io/v1alpha1/namespaces/team/versions":    {body: opendepotv1alpha1.VersionList{}},
		"/api/v1/namespaces/team": {body: namespace},
		"/apis/opendepot.defdev.io/v1alpha1/namespaces/team/providers/aws": {body: opendepotv1alpha1.Provider{
			ObjectMeta: metav1.ObjectMeta{Name: "aws", Namespace: "team"},
			Spec: opendepotv1alpha1.ProviderSpec{
				ProviderConfig: opendepotv1alpha1.ProviderConfig{UpstreamRegistry: covGStringPtr(opendepotv1alpha1.TerraformRegistryHost)},
			},
		}},
	})
	covGUseCluster(t, api)

	t.Run("missing module is reported as module_not_found", func(t *testing.T) {
		body := `{"schemaVersion":"assembly.v1","modules":[{"nodeId":"m1","namespace":"team","name":"absent","version":"1.0.0","localName":"absent"}]}`
		response := httptest.NewRecorder()
		handleAssemblyExport(response, covGAssemblyExportRequest(t, body, nil))

		if response.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusNotFound, response.Body.String())
		}

		decoded := covGDecodeAssemblyError(t, response)
		if len(decoded.Diagnostics) != 1 || decoded.Diagnostics[0].Code != "module_not_found" || decoded.Diagnostics[0].Path != "modules[0]" {
			t.Fatalf("diagnostics = %+v, want module_not_found at modules[0]", decoded.Diagnostics)
		}
	})

	t.Run("module without a synced exact version is an invalid canvas", func(t *testing.T) {
		body := `{"schemaVersion":"assembly.v1","modules":[{"nodeId":"m1","namespace":"team","name":"kms","version":"1.2.0","localName":"kms"}]}`
		response := httptest.NewRecorder()
		handleAssemblyExport(response, covGAssemblyExportRequest(t, body, nil))

		if response.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusUnprocessableEntity, response.Body.String())
		}

		decoded := covGDecodeAssemblyError(t, response)
		if decoded.Error != "invalid_canvas" {
			t.Fatalf("error = %q, want invalid_canvas", decoded.Error)
		}

		found := false
		for _, diagnostic := range decoded.Diagnostics {
			if diagnostic.Code == "module_version_unavailable" && diagnostic.Path == "modules[0].version" && diagnostic.NodeID == "m1" {
				found = true
			}
		}
		if !found {
			t.Fatalf("diagnostics = %+v, want module_version_unavailable for m1", decoded.Diagnostics)
		}
	})

	t.Run("missing provider is reported as provider_not_found", func(t *testing.T) {
		body := `{"schemaVersion":"assembly.v1","providers":[{"nodeId":"p1","namespace":"team","name":"gcp","version":"1.0.0","localName":"gcp"}]}`
		response := httptest.NewRecorder()
		handleAssemblyExport(response, covGAssemblyExportRequest(t, body, nil))

		if response.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
		}

		if decoded := covGDecodeAssemblyError(t, response); decoded.Error != "provider_not_found" {
			t.Fatalf("error = %q, want provider_not_found", decoded.Error)
		}
	})

	t.Run("unsupported provider registry is an invalid canvas", func(t *testing.T) {
		body := `{"schemaVersion":"assembly.v1","providers":[{"nodeId":"p1","namespace":"team","name":"aws","version":"5.1.0","localName":"aws"}]}`
		response := httptest.NewRecorder()
		handleAssemblyExport(response, covGAssemblyExportRequest(t, body, nil))

		if response.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusUnprocessableEntity, response.Body.String())
		}

		decoded := covGDecodeAssemblyError(t, response)
		found := false
		for _, diagnostic := range decoded.Diagnostics {
			if diagnostic.Code == "provider_registry_unsupported" && diagnostic.Path == "providers[0]" {
				found = true
			}
		}
		if !found {
			t.Fatalf("diagnostics = %+v, want provider_registry_unsupported at providers[0]", decoded.Diagnostics)
		}
	})

	t.Run("streaming requires a flushable writer", func(t *testing.T) {
		body := `{"schemaVersion":"assembly.v1","modules":[]}`
		response := httptest.NewRecorder()
		request := covGAssemblyExportRequest(t, body, map[string]string{"Accept": "application/x-ndjson"})
		streamAssemblyExport(covGNonFlushingWriter{ResponseWriter: response}, request, assemblyexport.Files{}, nil, "")

		if response.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
		}

		if decoded := covGDecodeAssemblyError(t, response); decoded.Error != "streaming_unavailable" {
			t.Fatalf("error = %q, want streaming_unavailable", decoded.Error)
		}
	})
}

func TestCovGEnsureJSONEOF(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "single document", input: `{"a":1}`, wantErr: false},
		{name: "second document", input: `{"a":1} {"b":2}`, wantErr: true},
		{name: "trailing garbage", input: `{"a":1} xyz`, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			decoder := json.NewDecoder(strings.NewReader(tc.input))
			var first map[string]any
			if err := decoder.Decode(&first); err != nil {
				t.Fatalf("Decode() unexpected error: %v", err)
			}

			err := ensureJSONEOF(decoder)
			if (err != nil) != tc.wantErr {
				t.Fatalf("ensureJSONEOF() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestCovGRequestBearerToken(t *testing.T) {
	tests := map[string]string{
		"Bearer abc.def ": "abc.def",
		"Basic abc":       "",
		"":                "",
	}

	for header, want := range tests {
		request := httptest.NewRequest(http.MethodGet, "http://registry.example.com/", nil)
		if header != "" {
			request.Header.Set("Authorization", header)
		}

		if got := requestBearerToken(request); got != want {
			t.Fatalf("requestBearerToken(%q) = %q, want %q", header, got, want)
		}
	}
}

func TestCovGAssemblyFailureAndWriter(t *testing.T) {
	failure := assemblyFailure(http.StatusForbidden, "module_forbidden", "denied", "modules[1]", "m2")
	if failure.status != http.StatusForbidden || failure.diagnostic.Code != "module_forbidden" ||
		failure.diagnostic.Path != "modules[1]" || failure.diagnostic.NodeID != "m2" {
		t.Fatalf("assemblyFailure() = %+v, want status 403 with path and node", failure)
	}

	response := httptest.NewRecorder()
	writeAssemblyError(response, http.StatusTeapot, assemblyexport.ErrorResponse{Error: "brew", Message: "short and stout"})

	if response.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusTeapot)
	}

	if ct := response.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	if !bytes.Contains(body, []byte(`"error":"brew"`)) {
		t.Fatalf("body = %s, want error field brew", body)
	}
}
