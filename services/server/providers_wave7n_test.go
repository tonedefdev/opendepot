package main

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
)

func wave7nProviderVersionRoutes(t *testing.T, versions []opendepotv1alpha1.Version) map[string]covGResponse {
	t.Helper()

	encoded, _ := covGProviderChecksum(t, "provider")
	routes := covGProviderRoutes(t, encoded)
	routes[covGProviderVersionsURL] = covGResponse{body: opendepotv1alpha1.VersionList{Items: versions}}

	return routes
}

func wave7nPlainStorage() *opendepotv1alpha1.StorageConfig {
	return &opendepotv1alpha1.StorageConfig{
		FileSystem: &opendepotv1alpha1.FileSystemConfig{DirectoryPath: covGStringPtr("/data/providers")},
	}
}

func TestWave7NProviderMirrorLookupFailures(t *testing.T) {
	encoded, _ := covGProviderChecksum(t, "provider")

	t.Run("provider list error is internal server error", func(t *testing.T) {
		routes := covGProviderRoutes(t, encoded)
		routes[covGProvidersListPath] = covGResponse{status: http.StatusInternalServerError}
		covGUseCluster(t, covGAPIServer(t, routes))

		request := covGRequest(http.MethodGet, "http://registry.example.com/", covGMirrorParams(nil))
		response := httptest.NewRecorder()
		getProviderMirrorVersions(response, request)

		if response.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
		}
	})

	t.Run("provider with another namespace is not found", func(t *testing.T) {
		covGUseCluster(t, covGAPIServer(t, covGProviderRoutes(t, encoded)))

		request := covGRequest(http.MethodGet, "http://registry.example.com/", covGMirrorParams(map[string]string{"providerNamespace": "other"}))
		response := httptest.NewRecorder()
		getProviderMirrorVersions(response, request)

		if response.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
		}
	})

	t.Run("malformed provider list is internal server error", func(t *testing.T) {
		routes := covGProviderRoutes(t, encoded)
		routes[covGProvidersListPath] = covGResponse{raw: "{not-json"}
		covGUseCluster(t, covGAPIServer(t, routes))

		request := covGRequest(http.MethodGet, "http://registry.example.com/", covGMirrorParams(nil))
		response := httptest.NewRecorder()
		getProviderMirrorVersions(response, request)

		if response.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
		}
	})
}

func TestWave7NProviderMirrorVersionListFailures(t *testing.T) {
	encoded, _ := covGProviderChecksum(t, "provider")

	t.Run("version list error is internal server error for versions and archives", func(t *testing.T) {
		routes := covGProviderRoutes(t, encoded)
		routes[covGProviderVersionsURL] = covGResponse{status: http.StatusInternalServerError}
		covGUseCluster(t, covGAPIServer(t, routes))

		versions := httptest.NewRecorder()
		getProviderMirrorVersions(versions, covGRequest(http.MethodGet, "http://registry.example.com/", covGMirrorParams(nil)))

		if versions.Code != http.StatusInternalServerError {
			t.Fatalf("versions status = %d, want %d", versions.Code, http.StatusInternalServerError)
		}

		archives := httptest.NewRecorder()
		getProviderMirrorArchives(archives, covGRequest(http.MethodGet, "http://registry.example.com/", covGMirrorParams(map[string]string{"versionFile": "5.1.0.json"})))

		if archives.Code != http.StatusInternalServerError {
			t.Fatalf("archives status = %d, want %d", archives.Code, http.StatusInternalServerError)
		}
	})

	t.Run("malformed version list is internal server error", func(t *testing.T) {
		routes := covGProviderRoutes(t, encoded)
		routes[covGProviderVersionsURL] = covGResponse{raw: "[oops"}
		covGUseCluster(t, covGAPIServer(t, routes))

		request := covGRequest(http.MethodGet, "http://registry.example.com/", covGMirrorParams(nil))
		response := httptest.NewRecorder()
		getProviderMirrorVersions(response, request)

		if response.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
		}
	})

	t.Run("archive version file without json suffix is not found", func(t *testing.T) {
		covGUseCluster(t, covGAPIServer(t, covGProviderRoutes(t, encoded)))

		request := covGRequest(http.MethodGet, "http://registry.example.com/", covGMirrorParams(map[string]string{"versionFile": "5.1.0"}))
		response := httptest.NewRecorder()
		getProviderMirrorArchives(response, request)

		if response.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
		}
	})

	t.Run("empty archive version is not found", func(t *testing.T) {
		covGUseCluster(t, covGAPIServer(t, covGProviderRoutes(t, encoded)))

		request := covGRequest(http.MethodGet, "http://registry.example.com/", covGMirrorParams(map[string]string{"versionFile": ".json"}))
		response := httptest.NewRecorder()
		getProviderMirrorArchives(response, request)

		if response.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
		}
	})

	t.Run("unknown archive version is not found", func(t *testing.T) {
		covGUseCluster(t, covGAPIServer(t, covGProviderRoutes(t, encoded)))

		request := covGRequest(http.MethodGet, "http://registry.example.com/", covGMirrorParams(map[string]string{"versionFile": "9.9.9.json"}))
		response := httptest.NewRecorder()
		getProviderMirrorArchives(response, request)

		if response.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
		}
	})
}

func TestWave7NServeProviderMirrorArchiveBranches(t *testing.T) {
	encoded, _ := covGProviderChecksum(t, "provider")
	covGUseCluster(t, covGAPIServer(t, covGProviderRoutes(t, encoded)))

	archiveParams := covGMirrorParams(map[string]string{
		"version":  "5.1.0",
		"os":       "linux",
		"arch":     "amd64",
		"filename": covGProviderFileName,
	})

	t.Run("unsupported hostname is not found", func(t *testing.T) {
		request := covGRequest(http.MethodGet, "http://registry.example.com/", covGMirrorParams(map[string]string{"hostname": "example.com"}))
		response := httptest.NewRecorder()
		serveProviderMirrorArchive(response, request)

		if response.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
		}
	})

	t.Run("mismatched filename is not found", func(t *testing.T) {
		request := covGRequest(http.MethodGet, "http://registry.example.com/", covGMirrorParams(map[string]string{
			"version": "5.1.0", "os": "linux", "arch": "amd64", "filename": "other.zip",
		}))
		response := httptest.NewRecorder()
		serveProviderMirrorArchive(response, request)

		if response.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
		}
	})

	t.Run("matching archive redirects to proxied download", func(t *testing.T) {
		request := covGRequest(http.MethodGet, "http://registry.example.com/", archiveParams)
		response := httptest.NewRecorder()
		serveProviderMirrorArchive(response, request)

		if response.Code != http.StatusFound {
			t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusFound, response.Body.String())
		}

		if !strings.HasPrefix(response.Header().Get("Location"), "/opendepot/modules/v1/download/") {
			t.Fatalf("Location = %q, want proxied download path", response.Header().Get("Location"))
		}
	})

	t.Run("version list error is internal server error", func(t *testing.T) {
		routes := covGProviderRoutes(t, encoded)
		routes[covGProviderVersionsURL] = covGResponse{status: http.StatusInternalServerError}
		covGUseCluster(t, covGAPIServer(t, routes))

		request := covGRequest(http.MethodGet, "http://registry.example.com/", archiveParams)
		response := httptest.NewRecorder()
		serveProviderMirrorArchive(response, request)

		if response.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
		}
	})
}

func TestWave7NGetProviderVersionsFailures(t *testing.T) {
	encoded, _ := covGProviderChecksum(t, "provider")
	params := map[string]string{"namespace": "team", "type": "aws"}

	t.Run("provider lookup error is internal server error", func(t *testing.T) {
		routes := covGProviderRoutes(t, encoded)
		routes[covGProviderGetPath] = covGResponse{status: http.StatusInternalServerError}
		covGUseCluster(t, covGAPIServer(t, routes))

		response := httptest.NewRecorder()
		getProviderVersions(response, covGRequest(http.MethodGet, "http://registry.example.com/", params))

		if response.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
		}
	})

	t.Run("version list error is internal server error", func(t *testing.T) {
		routes := covGProviderRoutes(t, encoded)
		routes[covGProviderVersionsURL] = covGResponse{status: http.StatusInternalServerError}
		covGUseCluster(t, covGAPIServer(t, routes))

		response := httptest.NewRecorder()
		getProviderVersions(response, covGRequest(http.MethodGet, "http://registry.example.com/", params))

		if response.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
		}
	})

	t.Run("malformed version list is internal server error", func(t *testing.T) {
		routes := covGProviderRoutes(t, encoded)
		routes[covGProviderVersionsURL] = covGResponse{raw: "{bad"}
		covGUseCluster(t, covGAPIServer(t, routes))

		response := httptest.NewRecorder()
		getProviderVersions(response, covGRequest(http.MethodGet, "http://registry.example.com/", params))

		if response.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
		}
	})
}

func TestWave7NProviderPackageMetadataFailures(t *testing.T) {
	encoded, _ := covGProviderChecksum(t, "provider")
	t.Setenv("OPENDEPOT_PROVIDER_GPG_KEY_ID", "abc123")
	t.Setenv("OPENDEPOT_PROVIDER_GPG_ASCII_ARMOR", "-----BEGIN PGP PUBLIC KEY BLOCK-----")

	pathParams := map[string]string{
		"namespace": "team", "type": "aws", "version": "5.1.0", "os": "linux", "arch": "amd64",
	}

	t.Run("version list error is internal server error", func(t *testing.T) {
		routes := covGProviderRoutes(t, encoded)
		routes[covGProviderVersionsURL] = covGResponse{status: http.StatusInternalServerError}
		covGUseCluster(t, covGAPIServer(t, routes))

		response := httptest.NewRecorder()
		getProviderPackageMetadata(response, covGRequest(http.MethodGet, "http://registry.example.com/", pathParams))

		if response.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
		}
	})

	t.Run("incomplete version metadata is not implemented", func(t *testing.T) {
		versions := []opendepotv1alpha1.Version{covGProviderVersion("aws-5-1-0-linux", "5.1.0", "linux", "amd64", covGProviderFileName, true, nil, wave7nPlainStorage())}
		covGUseCluster(t, covGAPIServer(t, wave7nProviderVersionRoutes(t, versions)))

		response := httptest.NewRecorder()
		getProviderPackageMetadata(response, covGRequest(http.MethodGet, "http://registry.example.com/", pathParams))

		if response.Code != http.StatusNotImplemented {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusNotImplemented)
		}
	})

	t.Run("undecodable checksum is internal server error", func(t *testing.T) {
		bad := "%%%"
		versions := []opendepotv1alpha1.Version{covGProviderVersion("aws-5-1-0-linux", "5.1.0", "linux", "amd64", covGProviderFileName, true, &bad, wave7nPlainStorage())}
		covGUseCluster(t, covGAPIServer(t, wave7nProviderVersionRoutes(t, versions)))

		response := httptest.NewRecorder()
		getProviderPackageMetadata(response, covGRequest(http.MethodGet, "http://registry.example.com/", pathParams))

		if response.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
		}
	})

	t.Run("without a binding the request is allowed", func(t *testing.T) {
		covGUseCluster(t, covGAPIServer(t, covGProviderRoutes(t, encoded)))

		response := httptest.NewRecorder()
		getProviderPackageMetadata(response, covGRequest(http.MethodGet, "http://registry.example.com/", pathParams))

		if response.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
		}
	})
}

func TestWave7NProviderPackageDownloadFailures(t *testing.T) {
	encoded, _ := covGProviderChecksum(t, "provider")

	pathParams := map[string]string{
		"namespace": "team", "type": "aws", "version": "5.1.0", "os": "linux", "arch": "amd64",
	}

	t.Run("version list error is internal server error", func(t *testing.T) {
		routes := covGProviderRoutes(t, encoded)
		routes[covGProviderVersionsURL] = covGResponse{status: http.StatusInternalServerError}
		covGUseCluster(t, covGAPIServer(t, routes))

		response := httptest.NewRecorder()
		serveProviderPackageDownload(response, covGRequest(http.MethodGet, "http://registry.example.com/", pathParams))

		if response.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
		}
	})

	t.Run("unknown version is not found", func(t *testing.T) {
		covGUseCluster(t, covGAPIServer(t, covGProviderRoutes(t, encoded)))

		response := httptest.NewRecorder()
		serveProviderPackageDownload(response, covGRequest(http.MethodGet, "http://registry.example.com/", map[string]string{
			"namespace": "team", "type": "aws", "version": "9.9.9", "os": "linux", "arch": "amd64",
		}))

		if response.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
		}
	})

	t.Run("missing checksum is not implemented", func(t *testing.T) {
		versions := []opendepotv1alpha1.Version{covGProviderVersion("aws-5-1-0-linux", "5.1.0", "linux", "amd64", covGProviderFileName, true, nil, wave7nPlainStorage())}
		covGUseCluster(t, covGAPIServer(t, wave7nProviderVersionRoutes(t, versions)))

		response := httptest.NewRecorder()
		serveProviderPackageDownload(response, covGRequest(http.MethodGet, "http://registry.example.com/", pathParams))

		if response.Code != http.StatusNotImplemented {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusNotImplemented)
		}
	})

	t.Run("version without storage config cannot build download path", func(t *testing.T) {
		checksum := encoded
		version := covGProviderVersion("aws-5-1-0-linux", "5.1.0", "linux", "amd64", covGProviderFileName, true, &checksum, nil)
		version.Spec.ProviderConfigRef.StorageConfig = nil
		covGUseCluster(t, covGAPIServer(t, wave7nProviderVersionRoutes(t, []opendepotv1alpha1.Version{version})))

		response := httptest.NewRecorder()
		serveProviderPackageDownload(response, covGRequest(http.MethodGet, "http://registry.example.com/", pathParams))

		if response.Code != http.StatusNotImplemented {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusNotImplemented)
		}
	})

	t.Run("presign failure with fallback disabled is bad gateway", func(t *testing.T) {
		checksum := encoded
		enabled := true
		fallback := false
		storage := wave7nPlainStorage()
		storage.FileSystem.DirectoryPath = covGStringPtr(t.TempDir() + "/missing")
		storage.Presign = &opendepotv1alpha1.PresignConfig{Enabled: &enabled, FallbackToProxy: &fallback}
		version := covGProviderVersion("aws-5-1-0-linux", "5.1.0", "linux", "amd64", covGProviderFileName, true, &checksum, storage)
		covGUseCluster(t, covGAPIServer(t, wave7nProviderVersionRoutes(t, []opendepotv1alpha1.Version{version})))

		response := httptest.NewRecorder()
		serveProviderPackageDownload(response, covGRequest(http.MethodGet, "http://registry.example.com/", pathParams))

		if response.Code != http.StatusBadGateway {
			t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusBadGateway, response.Body.String())
		}
	})

	t.Run("presign failure with fallback enabled proxies the download", func(t *testing.T) {
		checksum := encoded
		enabled := true
		fallback := true
		storage := wave7nPlainStorage()
		storage.FileSystem.DirectoryPath = covGStringPtr(t.TempDir() + "/missing")
		storage.Presign = &opendepotv1alpha1.PresignConfig{Enabled: &enabled, FallbackToProxy: &fallback}
		version := covGProviderVersion("aws-5-1-0-linux", "5.1.0", "linux", "amd64", covGProviderFileName, true, &checksum, storage)
		covGUseCluster(t, covGAPIServer(t, wave7nProviderVersionRoutes(t, []opendepotv1alpha1.Version{version})))

		response := httptest.NewRecorder()
		serveProviderPackageDownload(response, covGRequest(http.MethodGet, "http://registry.example.com/", pathParams))

		if response.Code != http.StatusFound {
			t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusFound, response.Body.String())
		}
	})
}

func TestWave7NProviderSHA256SUMSFailures(t *testing.T) {
	encoded, _ := covGProviderChecksum(t, "provider")
	t.Setenv("OPENDEPOT_PROVIDER_GPG_PRIVATE_KEY_BASE64", base64.StdEncoding.EncodeToString([]byte("not a key")))

	pathParams := map[string]string{
		"namespace": "team", "type": "aws", "version": "5.1.0", "os": "linux", "arch": "amd64",
	}

	t.Run("version list error is internal server error", func(t *testing.T) {
		routes := covGProviderRoutes(t, encoded)
		routes[covGProviderVersionsURL] = covGResponse{status: http.StatusInternalServerError}
		covGUseCluster(t, covGAPIServer(t, routes))

		for name, handler := range map[string]http.HandlerFunc{
			"sums":           getProviderPackageSHA256SUMS,
			"sums signature": getProviderPackageSHA256SUMSSignature,
		} {
			response := httptest.NewRecorder()
			handler(response, covGRequest(http.MethodGet, "http://registry.example.com/", pathParams))

			if response.Code != http.StatusInternalServerError {
				t.Fatalf("%s status = %d, want %d", name, response.Code, http.StatusInternalServerError)
			}
		}
	})

	t.Run("missing checksum is not found", func(t *testing.T) {
		versions := []opendepotv1alpha1.Version{covGProviderVersion("aws-5-1-0-linux", "5.1.0", "linux", "amd64", covGProviderFileName, true, nil, wave7nPlainStorage())}
		covGUseCluster(t, covGAPIServer(t, wave7nProviderVersionRoutes(t, versions)))

		for name, handler := range map[string]http.HandlerFunc{
			"sums":           getProviderPackageSHA256SUMS,
			"sums signature": getProviderPackageSHA256SUMSSignature,
		} {
			response := httptest.NewRecorder()
			handler(response, covGRequest(http.MethodGet, "http://registry.example.com/", pathParams))

			if response.Code != http.StatusNotFound {
				t.Fatalf("%s status = %d, want %d", name, response.Code, http.StatusNotFound)
			}
		}
	})

	t.Run("undecodable checksum is internal server error", func(t *testing.T) {
		bad := "%%%"
		versions := []opendepotv1alpha1.Version{covGProviderVersion("aws-5-1-0-linux", "5.1.0", "linux", "amd64", covGProviderFileName, true, &bad, wave7nPlainStorage())}
		covGUseCluster(t, covGAPIServer(t, wave7nProviderVersionRoutes(t, versions)))

		for name, handler := range map[string]http.HandlerFunc{
			"sums":           getProviderPackageSHA256SUMS,
			"sums signature": getProviderPackageSHA256SUMSSignature,
		} {
			response := httptest.NewRecorder()
			handler(response, covGRequest(http.MethodGet, "http://registry.example.com/", pathParams))

			if response.Code != http.StatusInternalServerError {
				t.Fatalf("%s status = %d, want %d", name, response.Code, http.StatusInternalServerError)
			}
		}
	})

	t.Run("unparseable private key is internal server error", func(t *testing.T) {
		covGUseCluster(t, covGAPIServer(t, covGProviderRoutes(t, encoded)))

		response := httptest.NewRecorder()
		getProviderPackageSHA256SUMSSignature(response, covGRequest(http.MethodGet, "http://registry.example.com/", pathParams))

		if response.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
		}
	})
}

func TestWave7NProviderSigningKeysAndChecksumHelpers(t *testing.T) {
	t.Run("missing key id is an error", func(t *testing.T) {
		t.Setenv("OPENDEPOT_PROVIDER_GPG_KEY_ID", "")
		t.Setenv("OPENDEPOT_PROVIDER_GPG_ASCII_ARMOR", "armor")

		if _, err := getProviderSigningKeysFromEnv(); err == nil {
			t.Fatal("getProviderSigningKeysFromEnv() error = nil, want error")
		}
	})

	t.Run("source url is included when configured", func(t *testing.T) {
		t.Setenv("OPENDEPOT_PROVIDER_GPG_KEY_ID", "abc123")
		t.Setenv("OPENDEPOT_PROVIDER_GPG_ASCII_ARMOR", "armor")
		t.Setenv("OPENDEPOT_PROVIDER_GPG_SOURCE_URL", " https://keys.example.com ")

		keys, err := getProviderSigningKeysFromEnv()
		if err != nil {
			t.Fatalf("getProviderSigningKeysFromEnv() error = %v", err)
		}

		if got := keys.GPGPublicKeys[0].SourceURL; got != "https://keys.example.com" {
			t.Fatalf("SourceURL = %q, want trimmed URL", got)
		}
	})

	t.Run("invalid base64 checksum is an error", func(t *testing.T) {
		if _, err := decodeSHA256Checksum("%%%"); err == nil {
			t.Fatal("decodeSHA256Checksum() error = nil, want error")
		}
	})
}
