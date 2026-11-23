package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// covGModuleArchive writes a zip with a single wrapper directory to dir/name/fileName and returns the
// base64 raw URL encoding of dir, matching the serveModuleFromFileSystem route parameter.
func covGModuleArchive(t *testing.T, directory, name, fileName, wrapper string) []byte {
	t.Helper()

	moduleDirectory := filepath.Join(directory, name)
	if err := os.MkdirAll(moduleDirectory, 0700); err != nil {
		t.Fatalf("os.MkdirAll() error = %v", err)
	}

	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	entry, err := writer.Create(wrapper + "/main.tf")
	if err != nil {
		t.Fatalf("zip.Create() error = %v", err)
	}
	if _, err = entry.Write([]byte("# module")); err != nil {
		t.Fatalf("zip entry write error = %v", err)
	}
	if err = writer.Close(); err != nil {
		t.Fatalf("zip.Close() error = %v", err)
	}
	if err = os.WriteFile(filepath.Join(moduleDirectory, fileName), buf.Bytes(), 0600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}

	return buf.Bytes()
}

// covGChecksumFor returns the base64 SHA-256 digest of content, as stored in Version status.
func covGChecksumFor(content []byte) string {
	sum := sha256.Sum256(content)

	return base64.StdEncoding.EncodeToString(sum[:])
}

func covGModuleVersion(t *testing.T, directory, name, version, fileName, checksum string) opendepotv1alpha1.Version {
	t.Helper()

	moduleName := name
	v := opendepotv1alpha1.Version{ObjectMeta: metav1.ObjectMeta{Name: name + "-" + sanitizeModuleVersionForLookup(version), Namespace: "team"}}
	v.Spec.Version = version
	v.Spec.Type = "module"
	v.Spec.FileName = &fileName
	v.Spec.ModuleConfigRef = &opendepotv1alpha1.ModuleConfig{
		Name: &moduleName,
		StorageConfig: &opendepotv1alpha1.StorageConfig{
			FileSystem: &opendepotv1alpha1.FileSystemConfig{DirectoryPath: &directory},
		},
	}
	v.Status.Synced = true
	v.Status.Checksum = &checksum

	return v
}

func TestCovGSanitizeModuleVersionForLookup(t *testing.T) {
	tests := map[string]string{
		"v1.2.3":   "1-2-3",
		"1.2_RC.1": "1-2-rc-1",
		"":         "",
	}

	for input, want := range tests {
		if got := sanitizeModuleVersionForLookup(input); got != want {
			t.Fatalf("sanitizeModuleVersionForLookup(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestCovGModuleArchiveTypeAndWrapper(t *testing.T) {
	if got := moduleArchiveType("module.tar"); got != "tar.gz" {
		t.Fatalf("moduleArchiveType(module.tar) = %q, want tar.gz", got)
	}

	if got := moduleArchiveType("module.zip"); got != "zip" {
		t.Fatalf("moduleArchiveType(module.zip) = %q, want zip", got)
	}

	if _, err := moduleArchiveWrapper(strings.NewReader("not a zip archive at all, definitely"), "zip"); err == nil {
		t.Fatal("moduleArchiveWrapper(zip) error = nil for invalid header")
	}

	if _, err := moduleArchiveWrapper(strings.NewReader("PK"), "zip"); err == nil {
		t.Fatal("moduleArchiveWrapper(zip) error = nil for truncated header")
	}

	if _, err := moduleArchiveWrapper(strings.NewReader("data"), "rar"); err == nil {
		t.Fatal("moduleArchiveWrapper(rar) error = nil, want unsupported type error")
	}

	if _, err := moduleArchiveWrapper(strings.NewReader("not gzip"), "tar.gz"); err == nil {
		t.Fatal("moduleArchiveWrapper(tar.gz) error = nil for non-gzip input")
	}

	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	entry, err := writer.Create("root-dir/main.tf")
	if err != nil {
		t.Fatalf("zip.Create() error = %v", err)
	}
	if _, err = entry.Write([]byte("#")); err != nil {
		t.Fatalf("zip entry write error = %v", err)
	}
	if err = writer.Close(); err != nil {
		t.Fatalf("zip.Close() error = %v", err)
	}

	wrapper, err := moduleArchiveWrapper(bytes.NewReader(buf.Bytes()), "zip")
	if err != nil {
		t.Fatalf("moduleArchiveWrapper(zip) unexpected error: %v", err)
	}

	if wrapper != "root-dir" {
		t.Fatalf("moduleArchiveWrapper(zip) = %q, want root-dir", wrapper)
	}
}

func TestCovGGetModuleVersion(t *testing.T) {
	directory := t.TempDir()
	version := covGModuleVersion(t, directory, "kms", "1.2.0", "module.zip", "Y2hlY2tzdW0=")
	versionBody, err := json.Marshal(version)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	api := covGAPIServer(t, map[string]covGResponse{
		"/apis/opendepot.defdev.io/v1alpha1/namespaces/team/versions/kms-1-2-0": {raw: string(versionBody)},
	})
	covGUseCluster(t, api)

	clientset, _, _, _, err := getKubeClientFromRequest(httptest.NewRecorder(), covGRequest(http.MethodGet, "http://registry.example.com/", nil))
	if err != nil {
		t.Fatalf("getKubeClientFromRequest() error = %v", err)
	}

	t.Run("returns the version resource", func(t *testing.T) {
		request := covGRequest(http.MethodGet, "http://registry.example.com/", map[string]string{
			"namespace": "team", "name": "kms", "version": "v1.2.0",
		})
		response := httptest.NewRecorder()

		got, err := getModuleVersion(clientset, response, request)
		if err != nil {
			t.Fatalf("getModuleVersion() unexpected error: %v", err)
		}

		if got.Name != "kms-1-2-0" || *got.Spec.FileName != "module.zip" {
			t.Fatalf("getModuleVersion() = %s/%s, want kms-1-2-0/module.zip", got.Name, *got.Spec.FileName)
		}
	})

	t.Run("missing version returns the api error", func(t *testing.T) {
		request := covGRequest(http.MethodGet, "http://registry.example.com/", map[string]string{
			"namespace": "team", "name": "kms", "version": "9.9.9",
		})
		response := httptest.NewRecorder()

		if _, err := getModuleVersion(clientset, response, request); err == nil {
			t.Fatal("getModuleVersion() error = nil for missing version")
		}
	})
}

func TestCovGGetModuleVersionsResponse(t *testing.T) {
	api := covGAPIServer(t, map[string]covGResponse{
		"/apis/opendepot.defdev.io/v1alpha1/namespaces/team/modules/kms": {
			body: opendepotv1alpha1.Module{
				ObjectMeta: metav1.ObjectMeta{Name: "kms", Namespace: "team"},
				Spec: opendepotv1alpha1.ModuleSpec{
					Versions: []opendepotv1alpha1.ModuleVersion{{Version: "1.2.0"}, {Version: "1.1.0"}},
				},
			},
		},
		"/apis/opendepot.defdev.io/v1alpha1/namespaces/team/modules/forbidden": {status: http.StatusForbidden, body: map[string]string{"kind": "Status"}},
	})
	covGUseCluster(t, api)

	t.Run("returns module versions", func(t *testing.T) {
		request := covGRequest(http.MethodGet, "http://registry.example.com/", map[string]string{"namespace": "team", "name": "kms"})
		response := httptest.NewRecorder()
		getModuleVersions(response, request)

		if response.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
		}

		var body ModuleVersionsResponse
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode body: %v", err)
		}

		if len(body.Modules) != 1 || len(body.Modules[0].Versions) != 2 || body.Modules[0].Versions[0].Version != "1.2.0" {
			t.Fatalf("modules = %+v, want versions [1.2.0 1.1.0]", body.Modules)
		}
	})

	t.Run("unknown module is not found", func(t *testing.T) {
		request := covGRequest(http.MethodGet, "http://registry.example.com/", map[string]string{"namespace": "team", "name": "missing"})
		response := httptest.NewRecorder()
		getModuleVersions(response, request)

		if response.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
		}
	})
}

func TestCovGGetDownloadModuleURL(t *testing.T) {
	directory := t.TempDir()
	covGModuleArchive(t, directory, "kms", "module.zip", "terraform-aws-kms-abc")
	version := covGModuleVersion(t, directory, "kms", "1.2.0", "module.zip", "Y2hlY2tzdW0=")
	versionBody, err := json.Marshal(version)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	noChecksum := version
	noChecksum.Name = "kms-1-3-0"
	noChecksum.Status.Checksum = nil
	noChecksumBody, err := json.Marshal(noChecksum)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	noArtifact := version
	noArtifact.Name = "kms-1-4-0"
	noArtifact.Spec.FileName = nil
	noArtifactBody, err := json.Marshal(noArtifact)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	missingArchive := covGModuleVersion(t, t.TempDir(), "kms", "1.5.0", "absent.zip", "Y2hlY2tzdW0=")
	missingArchiveBody, err := json.Marshal(missingArchive)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	api := covGAPIServer(t, map[string]covGResponse{
		"/apis/opendepot.defdev.io/v1alpha1/namespaces/team/versions/kms-1-2-0": {raw: string(versionBody)},
		"/apis/opendepot.defdev.io/v1alpha1/namespaces/team/versions/kms-1-3-0": {raw: string(noChecksumBody)},
		"/apis/opendepot.defdev.io/v1alpha1/namespaces/team/versions/kms-1-4-0": {raw: string(noArtifactBody)},
		"/apis/opendepot.defdev.io/v1alpha1/namespaces/team/versions/kms-1-5-0": {raw: string(missingArchiveBody)},
	})
	covGUseCluster(t, api)

	params := func(version string) map[string]string {
		return map[string]string{"namespace": "team", "name": "kms", "version": version}
	}

	t.Run("redirects through X-Terraform-Get", func(t *testing.T) {
		response := httptest.NewRecorder()
		getDownloadModuleUrl(response, covGRequest(http.MethodGet, "http://registry.example.com/", params("1.2.0")))

		if response.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusNoContent, response.Body.String())
		}

		header := response.Header().Get("X-Terraform-Get")
		if !strings.HasPrefix(header, "/opendepot/modules/v1/download/") {
			t.Fatalf("X-Terraform-Get = %q, want download path prefix", header)
		}

		if !strings.Contains(header, "archive=zip") || !strings.Contains(header, "fileChecksum=Y2hlY2tzdW0%3D") {
			t.Fatalf("X-Terraform-Get = %q, want archive and fileChecksum query", header)
		}

		if !strings.Contains(header, "terraform-aws-kms-abc") {
			t.Fatalf("X-Terraform-Get = %q, want wrapper directory from archive", header)
		}
	})

	t.Run("missing checksum is unavailable", func(t *testing.T) {
		response := httptest.NewRecorder()
		getDownloadModuleUrl(response, covGRequest(http.MethodGet, "http://registry.example.com/", params("1.3.0")))

		if response.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
		}
	})

	t.Run("missing artifact filename is unavailable", func(t *testing.T) {
		response := httptest.NewRecorder()
		getDownloadModuleUrl(response, covGRequest(http.MethodGet, "http://registry.example.com/", params("1.4.0")))

		if response.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
		}
	})

	t.Run("archive missing from storage is unavailable", func(t *testing.T) {
		response := httptest.NewRecorder()
		getDownloadModuleUrl(response, covGRequest(http.MethodGet, "http://registry.example.com/", params("1.5.0")))

		if response.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
		}
	})

	t.Run("unknown version is not found", func(t *testing.T) {
		response := httptest.NewRecorder()
		getDownloadModuleUrl(response, covGRequest(http.MethodGet, "http://registry.example.com/", params("9.9.9")))

		if response.Code == http.StatusNoContent {
			t.Fatal("status = 204 for unknown version, want an error status")
		}
	})
}

func TestCovGServeModuleFromFileSystem(t *testing.T) {
	directory := t.TempDir()
	archive := covGModuleArchive(t, directory, "kms", "module.zip", "terraform-aws-kms-abc")
	encodedDir := base64.RawURLEncoding.EncodeToString([]byte(directory))
	checksum := covGChecksumFor(archive)

	previousMountPath := opendepotFilesystemMountPath
	opendepotFilesystemMountPath = &directory
	t.Cleanup(func() { opendepotFilesystemMountPath = previousMountPath })

	t.Run("serves archive with matching checksum", func(t *testing.T) {
		request := covGRequest(http.MethodGet, "http://registry.example.com/?fileChecksum="+checksum, map[string]string{
			"directory": encodedDir, "name": "kms", "fileName": "module.zip",
		})
		response := httptest.NewRecorder()
		serveModuleFromFileSystem(response, request)

		if response.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
		}

		if !bytes.Equal(response.Body.Bytes(), archive) {
			t.Fatal("served body does not match the stored archive")
		}
	})

	t.Run("checksum mismatch is rejected", func(t *testing.T) {
		request := covGRequest(http.MethodGet, "http://registry.example.com/?fileChecksum=bad", map[string]string{
			"directory": encodedDir, "name": "kms", "fileName": "module.zip",
		})
		response := httptest.NewRecorder()
		serveModuleFromFileSystem(response, request)

		if response.Code == http.StatusOK {
			t.Fatal("status = 200 for checksum mismatch, want an error status")
		}
	})

	t.Run("undecodable directory is an error", func(t *testing.T) {
		request := covGRequest(http.MethodGet, "http://registry.example.com/", map[string]string{
			"directory": "%%%", "name": "kms", "fileName": "module.zip",
		})
		response := httptest.NewRecorder()
		serveModuleFromFileSystem(response, request)

		if response.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
		}
	})
}

func TestCovGServeModuleFromAzureBlobInvalidAccountURL(t *testing.T) {
	request := covGRequest(http.MethodGet, "http://registry.example.com/", map[string]string{
		"account": "acct", "accountUrl": "%zz", "rg": "rg", "subID": "sub", "name": "kms", "fileName": "module.zip",
	})
	response := httptest.NewRecorder()
	serveModuleFromAzureBlob(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d for an undecodable account URL", response.Code, http.StatusInternalServerError)
	}
}

func TestCovGModuleResolveArchiveWrapperFailure(t *testing.T) {
	missing := &opendepotv1alpha1.Version{}
	missingName := "kms"
	missingFile := "absent.zip"
	missing.Spec.FileName = &missingFile
	missing.Spec.ModuleConfigRef = &opendepotv1alpha1.ModuleConfig{
		Name: &missingName,
		StorageConfig: &opendepotv1alpha1.StorageConfig{
			FileSystem: &opendepotv1alpha1.FileSystemConfig{DirectoryPath: covGStringPtr(t.TempDir())},
		},
	}

	if _, err := resolveModuleArchiveWrapper(context.Background(), missing); err == nil {
		t.Fatal("resolveModuleArchiveWrapper() error = nil for missing archive")
	}
}
