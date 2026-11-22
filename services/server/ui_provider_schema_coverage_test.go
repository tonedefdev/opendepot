package main

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	"github.com/tonedefdev/opendepot/pkg/hclschema/providerschema"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func covGProviderSchemaBytes(t *testing.T) ([]byte, string) {
	t.Helper()

	schema := providerschema.ReducedSchema{
		SchemaVersion: providerschema.SchemaVersion,
		Provider:      "registry.terraform.io/hashicorp/aws",
	}

	raw, err := json.Marshal(schema)
	if err != nil {
		t.Fatalf("marshal schema: %v", err)
	}

	sum := sha256.Sum256(raw)

	return raw, hex.EncodeToString(sum[:])
}

func covGGzipBytes(t *testing.T, raw []byte) []byte {
	t.Helper()

	var buffer bytes.Buffer
	writer := gzip.NewWriter(&buffer)
	if _, err := writer.Write(raw); err != nil {
		t.Fatalf("gzip write: %v", err)
	}

	if err := writer.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}

	return buffer.Bytes()
}

func TestCovGDecodeProviderSchema(t *testing.T) {
	raw, digest := covGProviderSchemaBytes(t)

	t.Run("decodes gzipped schema with matching digest", func(t *testing.T) {
		schema, err := decodeProviderSchema(bytes.NewReader(covGGzipBytes(t, raw)), digest)
		if err != nil {
			t.Fatalf("decodeProviderSchema() unexpected error: %v", err)
		}

		if schema.SchemaVersion != providerschema.SchemaVersion {
			t.Fatalf("SchemaVersion = %q, want %q", schema.SchemaVersion, providerschema.SchemaVersion)
		}
	})

	t.Run("rejects digest mismatch", func(t *testing.T) {
		if _, err := decodeProviderSchema(bytes.NewReader(covGGzipBytes(t, raw)), "deadbeef"); err == nil {
			t.Fatal("decodeProviderSchema() error = nil, want digest mismatch")
		}
	})

	t.Run("rejects data that is not gzip", func(t *testing.T) {
		if _, err := decodeProviderSchema(bytes.NewReader(raw), digest); err == nil {
			t.Fatal("decodeProviderSchema() error = nil, want gunzip failure")
		}
	})

	t.Run("rejects gzipped non-JSON with matching digest", func(t *testing.T) {
		notJSON := []byte("not json")
		sum := sha256.Sum256(notJSON)

		if _, err := decodeProviderSchema(bytes.NewReader(covGGzipBytes(t, notJSON)), hex.EncodeToString(sum[:])); err == nil {
			t.Fatal("decodeProviderSchema() error = nil, want JSON decode failure")
		}
	})
}

func TestCovGWriteProviderSchemaUnavailable(t *testing.T) {
	response := httptest.NewRecorder()
	writeProviderSchemaUnavailable(response, "1.2.0", "no provider configuration schema is available for this version")

	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
	}

	var body map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}

	if body["error"] != "provider_schema_unavailable" || body["version"] != "1.2.0" {
		t.Fatalf("body = %v, want error provider_schema_unavailable and version 1.2.0", body)
	}
}

func TestCovGBrowseProviderSchemaServesSchema(t *testing.T) {
	raw, digest := covGProviderSchemaBytes(t)
	directory := t.TempDir()
	schemaPath := filepath.Join(directory, "aws-schema.json.gz")
	if err := os.WriteFile(schemaPath, covGGzipBytes(t, raw), 0o600); err != nil {
		t.Fatalf("write schema: %v", err)
	}

	providerName := "aws"
	provider := opendepotv1alpha1.Provider{ObjectMeta: metav1.ObjectMeta{Name: "aws", Namespace: "team"}}
	provider.Spec.ProviderConfig.Name = &providerName
	latest := "1.2.0"
	provider.Status.LatestVersion = &latest

	version := opendepotv1alpha1.Version{ObjectMeta: metav1.ObjectMeta{Name: "aws-1-2-0", Namespace: "team"}}
	version.Spec.Version = "1.2.0"
	version.Spec.ProviderConfigRef = &opendepotv1alpha1.ProviderConfig{
		Name: &providerName,
		StorageConfig: &opendepotv1alpha1.StorageConfig{
			FileSystem: &opendepotv1alpha1.FileSystemConfig{DirectoryPath: &directory},
		},
	}
	version.Status.Synced = true
	version.Status.ProviderSchemaRef = &opendepotv1alpha1.ProviderSchemaRef{Key: schemaPath, Digest: digest}

	routes := map[string]covGResponse{
		"/apis/opendepot.defdev.io/v1alpha1/namespaces/team/providers/aws": {body: provider},
		"/api/v1/namespaces/team":                                     {body: map[string]any{"metadata": map[string]any{"name": "team"}}},
		"/apis/opendepot.defdev.io/v1alpha1/namespaces/team/versions": {body: opendepotv1alpha1.VersionList{Items: []opendepotv1alpha1.Version{version}}},
	}
	api := covGAPIServer(t, routes)
	covGUseCluster(t, api)

	request := covGRequest(http.MethodGet, "http://registry.example.com/opendepot/ui/v1/resources/team/provider/aws/provider-schema", map[string]string{
		"namespace": "team", "name": "aws",
	})
	response := httptest.NewRecorder()
	handleBrowseProviderSchema(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}

	var body providerConfigurationSchemaResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}

	if body.Version != "1.2.0" || body.ProviderName != "aws" || body.ProviderNamespace != "hashicorp" || body.SchemaVersion != providerschema.SchemaVersion {
		t.Fatalf("body = %+v, want version 1.2.0, provider aws in hashicorp, schema version %s", body, providerschema.SchemaVersion)
	}
}

func TestCovGBrowseProviderSchemaUnavailable(t *testing.T) {
	providerName := "aws"
	provider := opendepotv1alpha1.Provider{ObjectMeta: metav1.ObjectMeta{Name: "aws", Namespace: "team"}}
	provider.Spec.ProviderConfig.Name = &providerName
	latest := "1.2.0"
	provider.Status.LatestVersion = &latest

	version := opendepotv1alpha1.Version{ObjectMeta: metav1.ObjectMeta{Name: "aws-1-2-0", Namespace: "team"}}
	version.Spec.Version = "1.2.0"
	version.Spec.ProviderConfigRef = &opendepotv1alpha1.ProviderConfig{Name: &providerName}

	routes := map[string]covGResponse{
		"/apis/opendepot.defdev.io/v1alpha1/namespaces/team/providers/aws": {body: provider},
		"/api/v1/namespaces/team":                                     {body: map[string]any{"metadata": map[string]any{"name": "team"}}},
		"/apis/opendepot.defdev.io/v1alpha1/namespaces/team/versions": {body: opendepotv1alpha1.VersionList{Items: []opendepotv1alpha1.Version{version}}},
	}
	api := covGAPIServer(t, routes)
	covGUseCluster(t, api)

	request := covGRequest(http.MethodGet, "http://registry.example.com/opendepot/ui/v1/resources/team/provider/aws/provider-schema?version=1.2.0", map[string]string{
		"namespace": "team", "name": "aws",
	})
	response := httptest.NewRecorder()
	handleBrowseProviderSchema(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
	}

	if !bytes.Contains(response.Body.Bytes(), []byte("provider_schema_unavailable")) {
		t.Fatalf("body = %s, want provider_schema_unavailable", response.Body.String())
	}
}

func TestCovGBrowseProviderSchemaProviderNotFound(t *testing.T) {
	api := covGAPIServer(t, map[string]covGResponse{})
	covGUseCluster(t, api)

	request := covGRequest(http.MethodGet, "http://registry.example.com/opendepot/ui/v1/resources/team/provider/missing/provider-schema", map[string]string{
		"namespace": "team", "name": "missing",
	})
	response := httptest.NewRecorder()
	handleBrowseProviderSchema(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
	}
}
