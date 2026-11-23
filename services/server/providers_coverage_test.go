package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	covGProvidersListPath   = "/apis/opendepot.defdev.io/v1alpha1/namespaces/team/providers"
	covGProviderGetPath     = "/apis/opendepot.defdev.io/v1alpha1/namespaces/team/providers/aws"
	covGProviderVersionsURL = "/apis/opendepot.defdev.io/v1alpha1/namespaces/team/versions"
	covGProviderFileName    = "terraform-provider-aws_5.1.0_linux_amd64.zip"
)

// covGProviderChecksum returns the base64 SHA-256 digest stored in Version status along with its hex form.
func covGProviderChecksum(t *testing.T, content string) (string, string) {
	t.Helper()

	sum := sha256.Sum256([]byte(content))

	return base64.StdEncoding.EncodeToString(sum[:]), hex.EncodeToString(sum[:])
}

func covGProviderVersion(name, version, osName, arch, fileName string, synced bool, checksum *string, storage *opendepotv1alpha1.StorageConfig) opendepotv1alpha1.Version {
	providerName := "aws"
	providerNamespace := "hashicorp"
	v := opendepotv1alpha1.Version{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "team"}}
	v.Spec.Version = version
	v.Spec.Type = "provider"
	v.Spec.OperatingSystem = osName
	v.Spec.Architecture = arch
	v.Spec.FileName = &fileName
	v.Spec.ProviderConfigRef = &opendepotv1alpha1.ProviderConfig{
		Name:          &providerName,
		Namespace:     &providerNamespace,
		StorageConfig: storage,
	}
	v.Status.Synced = synced
	v.Status.Checksum = checksum

	return v
}

// covGProviderRoutes serves one provider "hashicorp/aws" from the default OpenTofu registry with one
// synced linux_amd64 and darwin_arm64 version of 5.1.0, and one unsynced 5.2.0 that must be ignored.
func covGProviderRoutes(t *testing.T, checksum string) map[string]covGResponse {
	t.Helper()

	storage := &opendepotv1alpha1.StorageConfig{
		FileSystem: &opendepotv1alpha1.FileSystemConfig{DirectoryPath: covGStringPtr("/data/providers")},
	}

	providerName := "aws"
	provider := opendepotv1alpha1.Provider{ObjectMeta: metav1.ObjectMeta{Name: "aws", Namespace: "team"}}
	provider.Spec.ProviderConfig.Name = &providerName

	versions := []opendepotv1alpha1.Version{
		covGProviderVersion("aws-5-1-0-linux", "5.1.0", "linux", "amd64", covGProviderFileName, true, &checksum, storage),
		covGProviderVersion("aws-5-1-0-darwin", "5.1.0", "darwin", "arm64", "terraform-provider-aws_5.1.0_darwin_arm64.zip", true, &checksum, storage),
		covGProviderVersion("aws-5-2-0-linux", "5.2.0", "linux", "amd64", "terraform-provider-aws_5.2.0_linux_amd64.zip", false, &checksum, storage),
	}

	return map[string]covGResponse{
		covGProvidersListPath:   {body: opendepotv1alpha1.ProviderList{Items: []opendepotv1alpha1.Provider{provider}}},
		covGProviderGetPath:     {body: provider},
		covGProviderVersionsURL: {body: opendepotv1alpha1.VersionList{Items: versions}},
	}
}

func covGStringPtr(value string) *string {
	return &value
}

func covGMirrorParams(extra map[string]string) map[string]string {
	params := map[string]string{
		"namespace":         "team",
		"hostname":          opendepotv1alpha1.OpenTofuRegistryHost,
		"providerNamespace": "hashicorp",
		"type":              "aws",
	}
	for key, value := range extra {
		params[key] = value
	}

	return params
}

func TestCovGProviderIdentityAndRegistry(t *testing.T) {
	registry, namespace, name := providerConfigIdentity(nil, "fallback")
	if registry != opendepotv1alpha1.OpenTofuRegistryHost || namespace != "hashicorp" || name != "fallback" {
		t.Fatalf("providerConfigIdentity(nil) = (%q, %q, %q), want OpenTofu host, hashicorp, fallback", registry, namespace, name)
	}

	blank := "  "
	custom := "custom"
	upstream := opendepotv1alpha1.TerraformRegistryHost
	registry, namespace, name = providerConfigIdentity(&opendepotv1alpha1.ProviderConfig{
		Name:             &custom,
		Namespace:        &blank,
		UpstreamRegistry: &upstream,
	}, "fallback")
	if registry != opendepotv1alpha1.TerraformRegistryHost || namespace != "hashicorp" || name != "custom" {
		t.Fatalf("providerConfigIdentity(custom) = (%q, %q, %q), want terraform host, hashicorp, custom", registry, namespace, name)
	}

	if !supportedProviderRegistry(opendepotv1alpha1.TerraformRegistryHost) || supportedProviderRegistry("example.com") {
		t.Fatal("supportedProviderRegistry() did not accept only the OpenTofu and Terraform hosts")
	}
}

func TestCovGDecodeSHA256Checksum(t *testing.T) {
	encoded, want := covGProviderChecksum(t, "provider")
	got, err := decodeSHA256Checksum(encoded)
	if err != nil {
		t.Fatalf("decodeSHA256Checksum() unexpected error: %v", err)
	}

	if got != want {
		t.Fatalf("decodeSHA256Checksum() = %q, want %q", got, want)
	}

	if _, err := decodeSHA256Checksum("%%not-base64%%"); err == nil {
		t.Fatal("decodeSHA256Checksum() error = nil for invalid base64")
	}
}

func TestCovGProviderSigningKeysFromEnv(t *testing.T) {
	t.Run("missing key id is an error", func(t *testing.T) {
		t.Setenv("OPENDEPOT_PROVIDER_GPG_KEY_ID", "")
		t.Setenv("OPENDEPOT_PROVIDER_GPG_ASCII_ARMOR", "armor")

		if _, err := getProviderSigningKeysFromEnv(); err == nil {
			t.Fatal("getProviderSigningKeysFromEnv() error = nil, want missing env error")
		}
	})

	t.Run("builds upper-cased key id and source url", func(t *testing.T) {
		t.Setenv("OPENDEPOT_PROVIDER_GPG_KEY_ID", " abc123 ")
		t.Setenv("OPENDEPOT_PROVIDER_GPG_ASCII_ARMOR", "-----BEGIN PGP PUBLIC KEY BLOCK-----")
		t.Setenv("OPENDEPOT_PROVIDER_GPG_SOURCE_URL", " https://keys.example.com ")

		keys, err := getProviderSigningKeysFromEnv()
		if err != nil {
			t.Fatalf("getProviderSigningKeysFromEnv() unexpected error: %v", err)
		}

		if len(keys.GPGPublicKeys) != 1 {
			t.Fatalf("GPGPublicKeys length = %d, want 1", len(keys.GPGPublicKeys))
		}

		key := keys.GPGPublicKeys[0]
		if key.KeyID != "ABC123" || key.SourceURL != "https://keys.example.com" {
			t.Fatalf("signing key = %+v, want KeyID ABC123 and trimmed source URL", key)
		}
	})
}

func TestCovGGetProviderMirrorVersions(t *testing.T) {
	encoded, _ := covGProviderChecksum(t, "provider")
	api := covGAPIServer(t, covGProviderRoutes(t, encoded))
	covGUseCluster(t, api)

	t.Run("lists synced versions only", func(t *testing.T) {
		request := covGRequest(http.MethodGet, "http://registry.example.com/opendepot/providers/mirror/v1/team/index.json", covGMirrorParams(nil))
		response := httptest.NewRecorder()
		getProviderMirrorVersions(response, request)

		if response.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
		}

		var body ProviderMirrorVersionsResponse
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode body: %v", err)
		}

		if _, ok := body.Versions["5.1.0"]; !ok || len(body.Versions) != 1 {
			t.Fatalf("versions = %v, want only 5.1.0", body.Versions)
		}
	})

	t.Run("unsupported hostname is not found", func(t *testing.T) {
		request := covGRequest(http.MethodGet, "http://registry.example.com/", covGMirrorParams(map[string]string{"hostname": "example.com"}))
		response := httptest.NewRecorder()
		getProviderMirrorVersions(response, request)

		if response.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
		}
	})

	t.Run("unknown provider is not found", func(t *testing.T) {
		request := covGRequest(http.MethodGet, "http://registry.example.com/", covGMirrorParams(map[string]string{"type": "gcp"}))
		response := httptest.NewRecorder()
		getProviderMirrorVersions(response, request)

		if response.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
		}
	})

}

func TestCovGGetProviderMirrorArchives(t *testing.T) {
	encoded, hashHex := covGProviderChecksum(t, "provider")
	api := covGAPIServer(t, covGProviderRoutes(t, encoded))
	covGUseCluster(t, api)

	t.Run("returns archives with zh hashes", func(t *testing.T) {
		request := covGRequest(http.MethodGet, "http://registry.example.com/", covGMirrorParams(map[string]string{"versionFile": "5.1.0.json"}))
		response := httptest.NewRecorder()
		getProviderMirrorArchives(response, request)

		if response.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
		}

		var body ProviderMirrorArchivesResponse
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode body: %v", err)
		}

		archive, ok := body.Archives["linux_amd64"]
		if !ok {
			t.Fatalf("archives = %v, want linux_amd64", body.Archives)
		}

		wantURL := "5.1.0/linux/amd64/" + covGProviderFileName
		if archive.URL != wantURL {
			t.Fatalf("archive URL = %q, want %q", archive.URL, wantURL)
		}

		if len(archive.Hashes) != 1 || archive.Hashes[0] != "zh:"+hashHex {
			t.Fatalf("archive hashes = %v, want [zh:%s]", archive.Hashes, hashHex)
		}
	})

	t.Run("version file without json suffix is not found", func(t *testing.T) {
		request := covGRequest(http.MethodGet, "http://registry.example.com/", covGMirrorParams(map[string]string{"versionFile": "5.1.0"}))
		response := httptest.NewRecorder()
		getProviderMirrorArchives(response, request)

		if response.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
		}
	})

	t.Run("unknown version is not found", func(t *testing.T) {
		request := covGRequest(http.MethodGet, "http://registry.example.com/", covGMirrorParams(map[string]string{"versionFile": "9.9.9.json"}))
		response := httptest.NewRecorder()
		getProviderMirrorArchives(response, request)

		if response.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
		}
	})
}

func TestCovGServeProviderMirrorArchive(t *testing.T) {
	encoded, _ := covGProviderChecksum(t, "provider")
	api := covGAPIServer(t, covGProviderRoutes(t, encoded))
	covGUseCluster(t, api)

	t.Run("redirects matching archive through the module download path", func(t *testing.T) {
		request := covGRequest(http.MethodGet, "http://registry.example.com/", covGMirrorParams(map[string]string{
			"version": "5.1.0", "os": "linux", "arch": "amd64", "filename": covGProviderFileName,
		}))
		response := httptest.NewRecorder()
		serveProviderMirrorArchive(response, request)

		if response.Code != http.StatusFound {
			t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusFound, response.Body.String())
		}

		location := response.Header().Get("Location")
		if !strings.HasPrefix(location, "/opendepot/modules/v1/download/fileSystem/") || !strings.Contains(location, "/aws/"+covGProviderFileName) {
			t.Fatalf("Location = %q, want fileSystem download path for the provider archive", location)
		}

		if !strings.Contains(location, "fileChecksum=") {
			t.Fatalf("Location = %q, want fileChecksum query", location)
		}
	})

	t.Run("filename mismatch is not found", func(t *testing.T) {
		request := covGRequest(http.MethodGet, "http://registry.example.com/", covGMirrorParams(map[string]string{
			"version": "5.1.0", "os": "linux", "arch": "amd64", "filename": "other.zip",
		}))
		response := httptest.NewRecorder()
		serveProviderMirrorArchive(response, request)

		if response.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
		}
	})

	t.Run("unsupported hostname is not found", func(t *testing.T) {
		request := covGRequest(http.MethodGet, "http://registry.example.com/", covGMirrorParams(map[string]string{"hostname": "example.com"}))
		response := httptest.NewRecorder()
		serveProviderMirrorArchive(response, request)

		if response.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
		}
	})
}

func TestCovGGetProviderVersions(t *testing.T) {
	encoded, _ := covGProviderChecksum(t, "provider")
	api := covGAPIServer(t, covGProviderRoutes(t, encoded))
	covGUseCluster(t, api)

	t.Run("lists distinct normalized versions for the provider", func(t *testing.T) {
		request := covGRequest(http.MethodGet, "http://registry.example.com/opendepot/providers/v1/team/aws/versions", map[string]string{
			"namespace": "team", "type": "aws",
		})
		response := httptest.NewRecorder()
		getProviderVersions(response, request)

		if response.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
		}

		var body ProviderVersionsResponse
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode body: %v", err)
		}

		got := make([]string, 0, len(body.Versions))
		for _, v := range body.Versions {
			got = append(got, v.Version)
		}

		if strings.Join(got, ",") != "5.1.0,5.2.0" {
			t.Fatalf("versions = %v, want [5.1.0 5.2.0] (deduplicated across platforms)", got)
		}
	})

	t.Run("unknown provider is not found", func(t *testing.T) {
		request := covGRequest(http.MethodGet, "http://registry.example.com/", map[string]string{"namespace": "team", "type": "gcp"})
		response := httptest.NewRecorder()
		getProviderVersions(response, request)

		if response.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
		}
	})
}

func TestCovGGetProviderPackageMetadata(t *testing.T) {
	encoded, hashHex := covGProviderChecksum(t, "provider")
	api := covGAPIServer(t, covGProviderRoutes(t, encoded))
	covGUseCluster(t, api)

	pathParams := map[string]string{
		"namespace": "team", "type": "aws", "version": "5.1.0", "os": "linux", "arch": "amd64",
	}

	t.Run("returns download metadata and signing keys", func(t *testing.T) {
		t.Setenv("OPENDEPOT_PROVIDER_GPG_KEY_ID", "abc123")
		t.Setenv("OPENDEPOT_PROVIDER_GPG_ASCII_ARMOR", "-----BEGIN PGP PUBLIC KEY BLOCK-----")

		request := covGRequest(http.MethodGet, "http://registry.example.com/opendepot/providers/v1/team/aws/5.1.0/download/linux/amd64", pathParams)
		response := httptest.NewRecorder()
		getProviderPackageMetadata(response, request)

		if response.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
		}

		var body ProviderPackageMetadataResponse
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode body: %v", err)
		}

		if body.SHASum != hashHex || body.Filename != covGProviderFileName {
			t.Fatalf("metadata shasum/filename = %q/%q, want %q/%q", body.SHASum, body.Filename, hashHex, covGProviderFileName)
		}

		wantDownload := "http://registry.example.com/opendepot/providers/v1/download/team/aws/5.1.0/linux/amd64"
		if body.DownloadURL != wantDownload {
			t.Fatalf("DownloadURL = %q, want %q", body.DownloadURL, wantDownload)
		}

		if !strings.HasSuffix(body.SHASumsURL, "/opendepot/providers/v1/team/aws/5.1.0/SHA256SUMS/linux/amd64") {
			t.Fatalf("SHASumsURL = %q, want SHA256SUMS path", body.SHASumsURL)
		}

		if len(body.SigningKeys.GPGPublicKeys) != 1 || body.SigningKeys.GPGPublicKeys[0].KeyID != "ABC123" {
			t.Fatalf("signing keys = %+v, want one key ABC123", body.SigningKeys)
		}
	})

	t.Run("missing signing configuration is not implemented", func(t *testing.T) {
		t.Setenv("OPENDEPOT_PROVIDER_GPG_KEY_ID", "")
		t.Setenv("OPENDEPOT_PROVIDER_GPG_ASCII_ARMOR", "")

		request := covGRequest(http.MethodGet, "http://registry.example.com/", pathParams)
		response := httptest.NewRecorder()
		getProviderPackageMetadata(response, request)

		if response.Code != http.StatusNotImplemented {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusNotImplemented)
		}
	})

	t.Run("unknown platform is not found", func(t *testing.T) {
		request := covGRequest(http.MethodGet, "http://registry.example.com/", map[string]string{
			"namespace": "team", "type": "aws", "version": "5.1.0", "os": "windows", "arch": "386",
		})
		response := httptest.NewRecorder()
		getProviderPackageMetadata(response, request)

		if response.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
		}
	})
}

func TestCovGServeProviderPackageDownload(t *testing.T) {
	encoded, _ := covGProviderChecksum(t, "provider")
	api := covGAPIServer(t, covGProviderRoutes(t, encoded))
	covGUseCluster(t, api)

	t.Run("redirects to proxied module download with checksum", func(t *testing.T) {
		request := covGRequest(http.MethodGet, "http://registry.example.com/", map[string]string{
			"namespace": "team", "type": "aws", "version": "5.1.0", "os": "linux", "arch": "amd64",
		})
		response := httptest.NewRecorder()
		serveProviderPackageDownload(response, request)

		if response.Code != http.StatusFound {
			t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusFound, response.Body.String())
		}

		if location := response.Header().Get("Location"); !strings.HasPrefix(location, "/opendepot/modules/v1/download/fileSystem/") {
			t.Fatalf("Location = %q, want fileSystem download path", location)
		}
	})

	t.Run("unknown package is not found", func(t *testing.T) {
		request := covGRequest(http.MethodGet, "http://registry.example.com/", map[string]string{
			"namespace": "team", "type": "aws", "version": "9.9.9", "os": "linux", "arch": "amd64",
		})
		response := httptest.NewRecorder()
		serveProviderPackageDownload(response, request)

		if response.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
		}
	})
}

func TestCovGServeProviderVersionDownloadWithoutStorage(t *testing.T) {
	encoded, _ := covGProviderChecksum(t, "provider")
	version := covGProviderVersion("aws-5-1-0-linux", "5.1.0", "linux", "amd64", covGProviderFileName, true, &encoded, nil)

	response := httptest.NewRecorder()
	request := covGRequest(http.MethodGet, "http://registry.example.com/", nil)
	serveProviderVersionDownload(response, request, "team", "aws", "5.1.0", &version)

	if response.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want %d without storage config", response.Code, http.StatusNotImplemented)
	}
}

func TestCovGProviderSHA256SUMS(t *testing.T) {
	encoded, hashHex := covGProviderChecksum(t, "provider")
	api := covGAPIServer(t, covGProviderRoutes(t, encoded))
	covGUseCluster(t, api)

	pathParams := map[string]string{
		"namespace": "team", "type": "aws", "version": "5.1.0", "os": "linux", "arch": "amd64",
	}

	t.Run("serves checksum line for the package", func(t *testing.T) {
		request := covGRequest(http.MethodGet, "http://registry.example.com/", pathParams)
		response := httptest.NewRecorder()
		getProviderPackageSHA256SUMS(response, request)

		if response.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
		}

		want := hashHex + "  " + covGProviderFileName + "\n"
		if response.Body.String() != want {
			t.Fatalf("body = %q, want %q", response.Body.String(), want)
		}
	})

	t.Run("signature without private key is not implemented", func(t *testing.T) {
		t.Setenv("OPENDEPOT_PROVIDER_GPG_PRIVATE_KEY_BASE64", "")

		request := covGRequest(http.MethodGet, "http://registry.example.com/", pathParams)
		response := httptest.NewRecorder()
		getProviderPackageSHA256SUMSSignature(response, request)

		if response.Code != http.StatusNotImplemented {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusNotImplemented)
		}
	})

	t.Run("signature with undecodable private key is an error", func(t *testing.T) {
		t.Setenv("OPENDEPOT_PROVIDER_GPG_PRIVATE_KEY_BASE64", "%%%")

		request := covGRequest(http.MethodGet, "http://registry.example.com/", pathParams)
		response := httptest.NewRecorder()
		getProviderPackageSHA256SUMSSignature(response, request)

		if response.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
		}
	})

	t.Run("signature with valid key returns a detached signature", func(t *testing.T) {
		privateKeyBase64, _ := agentTestSigningKey(t)
		t.Setenv("OPENDEPOT_PROVIDER_GPG_PRIVATE_KEY_BASE64", privateKeyBase64)

		request := covGRequest(http.MethodGet, "http://registry.example.com/", pathParams)
		response := httptest.NewRecorder()
		getProviderPackageSHA256SUMSSignature(response, request)

		if response.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
		}

		if ct := response.Header().Get("Content-Type"); ct != "application/octet-stream" {
			t.Fatalf("Content-Type = %q, want application/octet-stream", ct)
		}

		if response.Body.Len() == 0 {
			t.Fatal("signature body is empty")
		}
	})

	t.Run("unknown package is not found", func(t *testing.T) {
		request := covGRequest(http.MethodGet, "http://registry.example.com/", map[string]string{
			"namespace": "team", "type": "aws", "version": "9.9.9", "os": "linux", "arch": "amd64",
		})
		response := httptest.NewRecorder()
		getProviderPackageSHA256SUMS(response, request)

		if response.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
		}
	})
}
