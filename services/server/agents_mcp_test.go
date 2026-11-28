package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
)

const (
	agentMCPTestVersionName     = "mcpserver-weather-1-2-0"
	agentMCPTestVersionNewer    = "mcpserver-weather-1-3-0"
	agentMCPTestSecretName      = "mcpserver-secret-mcp-1-2-0"
	agentMCPTestVersion         = "1.2.0"
	agentMCPTestNewerVersion    = "1.3.0"
	agentMCPTestDigest          = "sha256:5f1c0e9a7b2d4c6e8f0a1b3c5d7e9f1a2b4c6d8e0f1a3b5c7d9e1f2a4b6c8d0e"
	agentMCPTestHost            = "registry.example.com"
	agentMCPTestRegistry        = "registry.internal"
	agentMCPTestWeather         = "weather"
	agentMCPTestSecret          = "secret-mcp"
	agentMCPTestSkill           = "public-skill"
	agentMCPTestNpm             = "Npm"
	agentMCPTestPyPI            = "PyPI"
	agentMCPTestSource          = "Source"
	agentMCPTestImage           = "weather-image"
	agentMCPTestTransport       = "stdio"
	agentMCPTestPrivate         = "private"
	agentMCPTestSignature       = "signed-shasums"
	agentMCPTestDownload        = "download"
	agentMCPTestArchive         = "archive"
	agentMCPTestSHASums         = "SHA256SUMS"
	agentMCPTestSHASumsSig      = "SHA256SUMS.sig"
	agentMCPTestVersions        = "versions"
	agentMCPTestKind            = "mcpserver"
	agentMCPTestSkillKind       = "skill"
	agentMCPTestAgentKind       = "agent"
	agentMCPTestAlice           = "alice"
	agentMCPTestOther           = "other"
	agentMCPTestEndpointPath    = "/opendepot/agents/v1/team/mcpserver/weather/1.2.0/"
	agentMCPTestVersionsURL     = "http://registry.example.com/opendepot/agents/v1/team/mcpserver/weather/versions"
	agentMCPTestMCPServers      = "mcpservers"
	agentMCPTestNamespaces      = "namespaces"
	agentMCPTestSkills          = "skills"
	agentMCPTestAgents          = "agents"
	agentMCPTestGroupBinds      = "groupbindings"
	agentMCPTestNamespaceKey    = "namespace"
	agentMCPTestSkillVersion    = "1.0.0"
	agentMCPTestTeam            = "team"
	agentMCPTestCatalogURL      = "/opendepot/agents/v1/catalog"
	agentMCPTestBindingName     = "platform"
	agentMCPTestExpression      = `"platform" in groups`
	agentMCPTestSkillKey        = "team/skill/public-skill"
	agentMCPTestSecretKey       = "private/mcpserver/secret-mcp"
	agentMCPTestWeatherKey      = "team/mcpserver/weather"
	agentMCPTestSourceJSON      = "sourceType"
	agentMCPTestTransportKey    = "transport"
	agentMCPTestRunnableKey     = "runnable"
	agentMCPTestKindKey         = "kind"
	agentMCPTestMetadataKey     = "metadata"
	agentMCPTestStatusKey       = "status"
	agentMCPTestNameKey         = "name"
	agentMCPTestLabelsKey       = "labels"
	agentMCPTestItemsKey        = "items"
	agentMCPTestVersionKey      = "version"
	agentMCPTestSyncedKey       = "synced"
	agentMCPTestRefsKey         = "versionRefs"
	agentMCPTestBlocked         = "blocked"
	agentMCPTestPackageRoot     = "artifacts"
	agentMCPTestArchiveBody     = "reviewer-1.2.0"
	agentMCPTestWeatherTools    = "weather-tools"
	agentMCPTestUnsynced        = "unsynced"
	agentMCPTestTrue            = "true"
	agentMCPTestAudKey          = "aud"
	agentMCPTestSubKey          = "sub"
	agentMCPTestGroupsKey       = "groups"
	agentMCPTestOCIArtifactPath = "/apis/opendepot.defdev.io/v1alpha1/namespaces/team/ociartifacts/"
)

// agentTestMCPSourceTypes are the archive-backed MCPServer source types.
var agentTestMCPSourceTypes = []string{agentMCPTestNpm, agentMCPTestPyPI, agentMCPTestSource}

// agentTestMCPVersion returns a synced MCPServer Version sourced from the named MCPServer. An Image source has no
// archive fields. The other source types carry an archive served from the mcpserver storage container.
func agentTestMCPVersion(resourceName, sourceName, sourceType string) *opendepotv1alpha1.Version {
	metadata := &opendepotv1alpha1.MCPMetadata{
		ServerName: sourceName,
		Transport:  agentMCPTestTransport,
		Runnable:   sourceType != agentMCPTestSource,
	}
	source := opendepotv1alpha1.MCPServerSource{Type: sourceType}

	switch sourceType {
	case mcpImageSourceType:
		source.Image = &opendepotv1alpha1.MCPImageSource{
			OCIArtifactRef: corev1.LocalObjectReference{Name: agentMCPTestImage},
		}
		metadata.PackageDigest = agentMCPTestDigest
	case agentMCPTestPyPI:
		source.PyPI = &opendepotv1alpha1.MCPPyPISource{Package: sourceName}
	case agentMCPTestSource:
		source.Source = &opendepotv1alpha1.MCPRepoSource{RepoOwner: "tonedefdev", Path: "servers/" + sourceName}
	default:
		source.Npm = &opendepotv1alpha1.MCPNpmSource{Package: sourceName}
	}

	name := sourceName
	version := &opendepotv1alpha1.Version{
		ObjectMeta: metav1.ObjectMeta{Name: resourceName},
		Spec: opendepotv1alpha1.VersionSpec{
			Type:    opendepotv1alpha1.OpenDepotMCPServer,
			Version: agentMCPTestVersion,
			MCPServerSourceRef: &opendepotv1alpha1.MCPServerSourceConfig{
				Name:      &name,
				Source:    source,
				Transport: agentMCPTestTransport,
			},
		},
		Status: opendepotv1alpha1.VersionStatus{
			Synced:      true,
			MCPMetadata: metadata,
		},
	}

	if sourceType == mcpImageSourceType {
		return version
	}

	fileName := sourceName + "-" + agentMCPTestVersion + ".tar.gz"
	checksum := agentTestChecksumBase64()
	archiveSize := int64(len(agentMCPTestArchiveBody))
	directoryPath := agentMCPTestPackageRoot
	version.Spec.FileName = &fileName
	version.Spec.MCPServerSourceRef.StorageConfig = &opendepotv1alpha1.StorageConfig{
		FileSystem: &opendepotv1alpha1.FileSystemConfig{DirectoryPath: &directoryPath},
	}
	version.Status.Checksum = &checksum
	version.Status.ArchiveSizeBytes = &archiveSize
	version.Status.ShaSums = "0123456789abcdef  " + fileName + "\n"
	version.Status.ShaSumsSignature = agentMCPTestSignature

	return version
}

// agentTestMCPUseFileSystemArchive stores the archive of an MCPServer Version under the mcpserver storage container
// in a test-local mount root. The mount path is restored on cleanup.
func agentTestMCPUseFileSystemArchive(t *testing.T, version *opendepotv1alpha1.Version) {
	t.Helper()

	root := t.TempDir()
	directory := filepath.Join(root, agentMCPTestPackageRoot)
	archiveDir := filepath.Join(directory, agentMCPTestKind+"-"+*version.Spec.MCPServerSourceRef.Name)
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		t.Fatalf("os.MkdirAll() error = %v", err)
	}

	archive := filepath.Join(archiveDir, *version.Spec.FileName)
	if err := os.WriteFile(archive, []byte(agentMCPTestArchiveBody), 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}

	original := opendepotFilesystemMountPath
	opendepotFilesystemMountPath = &root
	t.Cleanup(func() { opendepotFilesystemMountPath = original })

	version.Spec.MCPServerSourceRef.StorageConfig.FileSystem.DirectoryPath = &directory
}

// agentTestMCPSetRegistryHost sets the external registry host for the test and restores it on cleanup.
func agentTestMCPSetRegistryHost(t *testing.T, host string) {
	t.Helper()

	original := opendepotRegistryHost
	opendepotRegistryHost = &host
	t.Cleanup(func() { opendepotRegistryHost = original })
}

// agentTestMCPDownload sends an authenticated request for the weather MCPServer at version 1.2.0.
func agentTestMCPDownload(t *testing.T, api *httptest.Server, endpoint string) *httptest.ResponseRecorder {
	t.Helper()

	return agentTestGet(t, api, agentTestMCPEndpointURL(endpoint))
}

// agentMCPTestVersionURL returns the fake API path for the MCP test Version in the team namespace.
func agentMCPTestVersionURL() string {
	return agentTestVersionsPath + "/" + agentMCPTestVersionName
}

func TestAgentMCPServerDownloadArchivedSources(t *testing.T) {
	privateKeyBase64, fingerprint := agentTestSigningKey(t)
	t.Setenv("OPENDEPOT_PROVIDER_GPG_PRIVATE_KEY_BASE64", privateKeyBase64)

	for _, sourceType := range agentTestMCPSourceTypes {
		t.Run(sourceType, func(t *testing.T) {
			version := agentTestMCPVersion(agentMCPTestVersionName, agentMCPTestWeather, sourceType)
			agentTestMCPUseFileSystemArchive(t, version)
			version.Status.SigningKeyFingerprint = fingerprint
			api, _ := agentTestAPIServer(t, map[string][]byte{
				agentMCPTestVersionURL(): agentTestJSON(t, version),
			})
			agentTestUseKubeconfigAuth(t)

			response := agentTestMCPDownload(t, api, agentMCPTestDownload)
			if response.Code != http.StatusOK {
				t.Fatalf("download status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
			}

			var download AgentDownloadResponse
			if err := json.Unmarshal(response.Body.Bytes(), &download); err != nil {
				t.Fatalf("unmarshal download: %v", err)
			}

			if !strings.HasSuffix(download.DownloadURL, "/mcpserver/weather/1.2.0/archive") {
				t.Fatalf("DownloadURL = %q, want the mcpserver archive route", download.DownloadURL)
			}

			if download.Shasum == "" || download.ShasumsURL == "" {
				t.Fatalf("download = %+v, want shasum and shasums URL", download)
			}

			var raw map[string]json.RawMessage
			if err := json.Unmarshal(response.Body.Bytes(), &raw); err != nil {
				t.Fatalf("unmarshal raw download: %v", err)
			}

			for _, key := range []string{"filename", "download_url", "shasum", "shasums_url", "shasums_signature_url", "signing_keys"} {
				if _, ok := raw[key]; !ok {
					t.Fatalf("archive download missing %q: %s", key, response.Body.String())
				}
			}

			for _, key := range []string{"ociReference", "ociDigest"} {
				if _, ok := raw[key]; ok {
					t.Fatalf("archive download carries OCI field %q: %s", key, response.Body.String())
				}
			}
		})
	}
}

func TestAgentMCPServerArchiveRoutesServeVerifiedArtifacts(t *testing.T) {
	privateKeyBase64, fingerprint := agentTestSigningKey(t)
	t.Setenv("OPENDEPOT_PROVIDER_GPG_PRIVATE_KEY_BASE64", privateKeyBase64)

	for _, sourceType := range agentTestMCPSourceTypes {
		t.Run(sourceType, func(t *testing.T) {
			version := agentTestMCPVersion(agentMCPTestVersionName, agentMCPTestWeather, sourceType)
			agentTestMCPUseFileSystemArchive(t, version)
			version.Status.SigningKeyFingerprint = fingerprint
			api, _ := agentTestAPIServer(t, map[string][]byte{
				agentMCPTestVersionURL(): agentTestJSON(t, version),
			})
			agentTestUseKubeconfigAuth(t)

			want := map[string]string{
				agentMCPTestArchive:    agentMCPTestArchiveBody,
				agentMCPTestSHASums:    version.Status.ShaSums,
				agentMCPTestSHASumsSig: agentMCPTestSignature,
			}
			for endpoint, body := range want {
				response := agentTestMCPDownload(t, api, endpoint)
				if response.Code != http.StatusOK {
					t.Fatalf("%s status = %d, want %d: %s", endpoint, response.Code, http.StatusOK, response.Body.String())
				}

				if response.Body.String() != body {
					t.Fatalf("%s body = %q, want %q", endpoint, response.Body.String(), body)
				}
			}
		})
	}
}

func TestAgentMCPServerImageDownloadReturnsOCIReference(t *testing.T) {
	privateKeyBase64, _ := agentTestSigningKey(t)
	t.Setenv("OPENDEPOT_PROVIDER_GPG_PRIVATE_KEY_BASE64", privateKeyBase64)

	artifact := agentTestJSON(t, opendepotv1alpha1.OCIArtifact{
		ObjectMeta: metav1.ObjectMeta{Name: agentMCPTestImage, Namespace: agentMCPTestTeam},
	})
	version := agentTestMCPVersion(agentMCPTestVersionName, agentMCPTestWeather, mcpImageSourceType)
	api, _ := agentTestAPIServer(t, map[string][]byte{
		agentMCPTestVersionURL():                        agentTestJSON(t, version),
		agentMCPTestOCIArtifactPath + agentMCPTestImage: artifact,
	})
	agentTestUseKubeconfigAuth(t)

	originalInClusterConfig := inClusterConfig
	inClusterConfig = func() (*rest.Config, error) {
		return &rest.Config{Host: api.URL}, nil
	}
	t.Cleanup(func() { inClusterConfig = originalInClusterConfig })

	tests := []struct {
		name     string
		host     string
		wantHost string
	}{
		{name: "request host when no registry host is configured", host: "", wantHost: agentMCPTestHost},
		{name: "configured registry host", host: agentMCPTestRegistry, wantHost: agentMCPTestRegistry},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			agentTestMCPSetRegistryHost(t, tc.host)

			response := agentTestMCPDownload(t, api, agentMCPTestDownload)
			if response.Code != http.StatusOK {
				t.Fatalf("download status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
			}

			var download AgentImageDownloadResponse
			if err := json.Unmarshal(response.Body.Bytes(), &download); err != nil {
				t.Fatalf("unmarshal download: %v", err)
			}

			wantRef := tc.wantHost + "/" + agentMCPTestTeam + "/" + agentMCPTestImage + "@" + agentMCPTestDigest
			if download.OCIReference != wantRef {
				t.Fatalf("OCIReference = %q, want %q", download.OCIReference, wantRef)
			}

			if download.OCIDigest != agentMCPTestDigest {
				t.Fatalf("OCIDigest = %q, want %q", download.OCIDigest, agentMCPTestDigest)
			}

			var raw map[string]json.RawMessage
			if err := json.Unmarshal(response.Body.Bytes(), &raw); err != nil {
				t.Fatalf("unmarshal raw download: %v", err)
			}

			for _, key := range []string{"download_url", "filename", "shasum", "shasums_url", "shasums_signature_url", "signing_keys"} {
				if _, ok := raw[key]; ok {
					t.Fatalf("Image download carries archive field %q: %s", key, response.Body.String())
				}
			}
		})
	}
}

func TestAgentMCPServerImageArtifactsNotServed(t *testing.T) {
	version := agentTestMCPVersion(agentMCPTestVersionName, agentMCPTestWeather, mcpImageSourceType)
	api, _ := agentTestAPIServer(t, map[string][]byte{
		agentMCPTestVersionURL(): agentTestJSON(t, version),
	})
	agentTestUseKubeconfigAuth(t)

	want := map[string]int{
		agentMCPTestArchive:    http.StatusServiceUnavailable,
		agentMCPTestSHASums:    http.StatusNotImplemented,
		agentMCPTestSHASumsSig: http.StatusNotImplemented,
	}
	for endpoint, status := range want {
		response := agentTestMCPDownload(t, api, endpoint)
		if response.Code != status {
			t.Fatalf("%s status = %d, want %d: %s", endpoint, response.Code, status, response.Body.String())
		}
	}
}

func TestAgentMCPServerVersionsListServableOnly(t *testing.T) {
	synced := agentTestMCPVersion(agentMCPTestVersionName, agentMCPTestWeather, agentMCPTestNpm)
	api, _ := agentTestAPIServer(t, map[string][]byte{
		agentTestVersionsPath: agentTestJSON(t, opendepotv1alpha1.VersionList{Items: []opendepotv1alpha1.Version{*synced}}),
		"/apis/opendepot.defdev.io/v1alpha1/namespaces/team/mcpservers/weather": agentTestJSON(t, map[string]any{
			agentMCPTestMetadataKey: map[string]any{
				agentMCPTestNameKey:      agentMCPTestWeather,
				agentMCPTestNamespaceKey: agentMCPTestTeam,
			},
			agentMCPTestStatusKey: map[string]any{agentMCPTestRefsKey: map[string]any{
				agentMCPTestVersion: agentTestVersionRef(agentMCPTestVersionName, agentMCPTestVersion),
				agentMCPTestNewerVersion: map[string]any{
					agentMCPTestNameKey:    agentMCPTestVersionNewer,
					agentMCPTestVersionKey: agentMCPTestNewerVersion,
					agentMCPTestSyncedKey:  false,
				},
			}},
		}),
	})
	agentTestUseKubeconfigAuth(t)

	response := agentTestGet(t, api, agentMCPTestVersionsURL)
	if response.Code != http.StatusOK {
		t.Fatalf("versions status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}

	var list AgentVersionsResponse
	if err := json.Unmarshal(response.Body.Bytes(), &list); err != nil {
		t.Fatalf("unmarshal versions: %v", err)
	}

	if len(list.Versions) != 1 || list.Versions[0].Version != agentMCPTestVersion {
		t.Fatalf("Versions = %+v, want only [%s]", list.Versions, agentMCPTestVersion)
	}
}

func TestAgentMCPServerUnservableVersionsNotFound(t *testing.T) {
	unsynced := agentTestMCPVersion(agentMCPTestVersionName, agentMCPTestWeather, agentMCPTestNpm)
	unsynced.Status.Synced = false

	blocked := agentTestMCPVersion(agentMCPTestVersionName, agentMCPTestWeather, agentMCPTestNpm)
	blocked.Status.JevAssessment = &opendepotv1alpha1.JevAssessment{Blocked: true}

	cases := map[string]*opendepotv1alpha1.Version{
		agentMCPTestUnsynced: unsynced,
		agentMCPTestBlocked:  blocked,
	}
	for name, version := range cases {
		t.Run(name, func(t *testing.T) {
			agentTestMCPUseFileSystemArchive(t, version)
			api, _ := agentTestAPIServer(t, map[string][]byte{
				agentMCPTestVersionURL(): agentTestJSON(t, version),
			})
			agentTestUseKubeconfigAuth(t)

			for _, endpoint := range []string{agentMCPTestDownload, agentMCPTestArchive} {
				response := agentTestMCPDownload(t, api, endpoint)
				if response.Code != http.StatusNotFound {
					t.Fatalf("%s status = %d, want %d: %s", endpoint, response.Code, http.StatusNotFound, response.Body.String())
				}
			}
		})
	}
}

func TestAgentMCPServerGroupBindingVisibility(t *testing.T) {
	privateKeyBase64, fingerprint := agentTestSigningKey(t)
	t.Setenv("OPENDEPOT_PROVIDER_GPG_PRIVATE_KEY_BASE64", privateKeyBase64)

	version := agentTestMCPVersion(agentMCPTestVersionName, agentMCPTestWeather, agentMCPTestNpm)
	agentTestMCPUseFileSystemArchive(t, version)
	version.Status.SigningKeyFingerprint = fingerprint
	agentTestOIDCAuth(t)

	cases := []struct {
		name      string
		resources []string
		denied    bool
	}{
		{name: "binding without MCPServer grant", resources: []string{agentMCPTestOther}, denied: true},
		{name: "binding without MCPServer resources", denied: true},
		{name: "binding with MCPServer grant", resources: []string{agentMCPTestWeather}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			agentTestClusterAPI(t, version, []opendepotv1alpha1.GroupBinding{{
				ObjectMeta: metav1.ObjectMeta{Name: agentMCPTestBindingName},
				Spec: opendepotv1alpha1.GroupBindingSpec{
					Expression:         agentMCPTestExpression,
					MCPServerResources: tc.resources,
				},
			}})

			for _, endpoint := range []string{agentMCPTestDownload, agentMCPTestArchive} {
				target := agentTestMCPEndpointURL(endpoint)
				token := "Bearer " + agentTestBearerToken(t, []string{agentMCPTestBindingName})
				response := agentTestServe(target, token)

				if tc.denied {
					if response.Code != http.StatusForbidden {
						t.Fatalf("%s status = %d, want %d: %s", endpoint, response.Code, http.StatusForbidden, response.Body.String())
					}

					if strings.Contains(response.Body.String(), agentMCPTestWeather) {
						t.Fatalf("%s denial leaked the MCPServer name: %s", endpoint, response.Body.String())
					}

					continue
				}

				if response.Code == http.StatusForbidden {
					t.Fatalf("%s status = %d, want access granted: %s", endpoint, response.Code, response.Body.String())
				}
			}
		})
	}
}

// agentTestMCPEndpointURL returns the absolute URL for an endpoint of the weather MCPServer at version 1.2.0.
func agentTestMCPEndpointURL(endpoint string) string {
	return "http://" + agentMCPTestHost + agentMCPTestEndpointPath + endpoint
}

func TestIsResourceAllowedMCPServerPatterns(t *testing.T) {
	binding := &opendepotv1alpha1.GroupBinding{
		Spec: opendepotv1alpha1.GroupBindingSpec{
			SkillResources:     []string{"weather-*"},
			MCPServerResources: []string{"weather-*"},
		},
	}

	cases := []struct {
		resourceType string
		name         string
		allowed      bool
	}{
		{resourceType: agentMCPTestKind, name: agentMCPTestWeatherTools, allowed: true},
		{resourceType: agentMCPTestKind, name: "billing", allowed: false},
		{resourceType: agentMCPTestSkillKind, name: agentMCPTestWeatherTools, allowed: true},
		{resourceType: agentMCPTestAgentKind, name: agentMCPTestWeatherTools, allowed: false},
	}

	for _, tc := range cases {
		if got := isResourceAllowed(binding, tc.resourceType, tc.name); got != tc.allowed {
			t.Errorf("isResourceAllowed(%s, %s) = %t, want %t", tc.resourceType, tc.name, got, tc.allowed)
		}
	}
}

// agentMCPCatalogServer serves the namespaces, Skills, MCPServers, Versions, and GroupBindings for the catalog.
func agentMCPCatalogServer(
	t *testing.T,
	skills, mcpservers []map[string]any,
	versions []opendepotv1alpha1.Version,
	bindings []opendepotv1alpha1.GroupBinding,
) *httptest.Server {
	t.Helper()

	public := map[string]string{labelPublic: agentMCPTestTrue}
	payloads := map[string]any{
		agentMCPTestNamespaces: map[string]any{agentMCPTestItemsKey: []map[string]any{
			{agentMCPTestMetadataKey: map[string]any{
				agentMCPTestNameKey:   agentMCPTestTeam,
				agentMCPTestLabelsKey: public,
			}},
			{agentMCPTestMetadataKey: map[string]any{
				agentMCPTestNameKey:   agentMCPTestPrivate,
				agentMCPTestLabelsKey: map[string]string{},
			}},
		}},
		agentMCPTestSkills:     map[string]any{agentMCPTestItemsKey: skills},
		agentMCPTestAgents:     map[string]any{agentMCPTestItemsKey: []map[string]any{}},
		agentMCPTestMCPServers: map[string]any{agentMCPTestItemsKey: mcpservers},
		agentMCPTestVersions:   opendepotv1alpha1.VersionList{Items: versions},
		agentMCPTestGroupBinds: opendepotv1alpha1.GroupBindingList{Items: bindings},
	}

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		payload, ok := payloads[path.Base(r.URL.Path)]
		if !ok {
			http.NotFound(w, r)
			return
		}

		body, err := json.Marshal(payload)
		if err != nil {
			t.Errorf("marshal payload: %v", err)
			http.Error(w, "marshal failed", http.StatusInternalServerError)
			return
		}

		_, _ = w.Write(body)
	}))
	t.Cleanup(api.Close)

	return api
}

// agentMCPCatalogFixture returns a public weather MCPServer with a synced 1.2.0 and an unsynced 1.3.0, a private
// secret-mcp Source MCPServer that is not runnable, and a public skill for the non-regression check.
func agentMCPCatalogFixture() (skills, mcpservers []map[string]any, versions []opendepotv1alpha1.Version) {
	public := map[string]string{labelPublic: agentMCPTestTrue}

	skills = []map[string]any{agentCatalogResource(agentMCPTestTeam, agentMCPTestSkill, public)}
	mcpservers = []map[string]any{
		agentCatalogResource(agentMCPTestTeam, agentMCPTestWeather, public),
		agentCatalogResource(agentMCPTestPrivate, agentMCPTestSecret, nil),
	}

	weather := agentTestMCPVersion(agentMCPTestVersionName, agentMCPTestWeather, agentMCPTestNpm)
	weather.Namespace = agentMCPTestTeam

	unsynced := agentTestMCPVersion(agentMCPTestVersionNewer, agentMCPTestWeather, agentMCPTestNpm)
	unsynced.Namespace = agentMCPTestTeam
	unsynced.Spec.Version = agentMCPTestNewerVersion
	unsynced.Status.Synced = false

	secret := agentTestMCPVersion(agentMCPTestSecretName, agentMCPTestSecret, agentMCPTestSource)
	secret.Namespace = agentMCPTestPrivate

	skill := agentCatalogVersion(
		opendepotv1alpha1.OpenDepotSkill,
		agentMCPTestTeam,
		agentMCPTestSkill,
		agentMCPTestSkillVersion,
		true,
	)

	versions = []opendepotv1alpha1.Version{*weather, *unsynced, *secret, skill}

	return skills, mcpservers, versions
}

func TestAgentMCPServerCatalogListsMCPServers(t *testing.T) {
	skills, mcpservers, versions := agentMCPCatalogFixture()
	api := agentMCPCatalogServer(t, skills, mcpservers, versions, nil)
	browseAgentTestSetup(t, api, true, false)

	response := agentCatalogGet(t, agentMCPTestCatalogURL, "")
	catalog := agentCatalogDecode(t, response)

	want := []string{agentMCPTestSecretKey, agentMCPTestWeatherKey, agentMCPTestSkillKey}
	if !slices.Equal(agentCatalogKeys(catalog.Items), want) {
		t.Fatalf("items = %v, want %v", agentCatalogKeys(catalog.Items), want)
	}

	secret := catalog.Items[0]
	if secret.SourceType != agentMCPTestSource || secret.Runnable == nil || *secret.Runnable {
		t.Fatalf("secret-mcp fields = sourceType %q runnable %v, want Source and an explicit false",
			secret.SourceType, secret.Runnable)
	}

	weather := catalog.Items[1]
	if weather.SourceType != agentMCPTestNpm || weather.Transport != agentMCPTestTransport ||
		weather.Runnable == nil || !*weather.Runnable {
		t.Fatalf("mcpserver fields = sourceType %q transport %q runnable %v",
			weather.SourceType, weather.Transport, weather.Runnable)
	}

	if weather.LatestVersion != agentMCPTestVersion {
		t.Fatalf("LatestVersion = %q, want the synced %s and not the unsynced %s",
			weather.LatestVersion, agentMCPTestVersion, agentMCPTestNewerVersion)
	}

	var raw struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &raw); err != nil {
		t.Fatalf("unmarshal raw catalog: %v", err)
	}

	skill := raw.Items[2]
	if skill[agentMCPTestKindKey] != agentMCPTestSkillKind {
		t.Fatalf("raw item 2 kind = %v, want %s", skill[agentMCPTestKindKey], agentMCPTestSkillKind)
	}

	for _, key := range []string{agentMCPTestSourceJSON, agentMCPTestTransportKey, agentMCPTestRunnableKey} {
		if _, ok := skill[key]; ok {
			t.Fatalf("skill catalog item carries %q: %v", key, skill)
		}
	}
}

func TestAgentMCPServerCatalogGroupBindingVisibility(t *testing.T) {
	skills, mcpservers, versions := agentMCPCatalogFixture()
	bindings := []opendepotv1alpha1.GroupBinding{{
		ObjectMeta: metav1.ObjectMeta{Name: agentMCPTestBindingName},
		Spec: opendepotv1alpha1.GroupBindingSpec{
			Expression:         agentMCPTestExpression,
			MCPServerResources: []string{"secret-*"},
		},
	}}
	api := agentMCPCatalogServer(t, skills, mcpservers, versions, bindings)
	browseAgentTestSetup(t, api, false, true)

	token := agentCatalogToken(t, map[string]any{
		agentMCPTestAudKey:    ociRealm,
		agentMCPTestSubKey:    agentMCPTestAlice,
		agentMCPTestGroupsKey: []string{agentMCPTestBindingName},
	})
	catalog := agentCatalogDecode(t, agentCatalogGet(t, agentMCPTestCatalogURL, token))

	want := []string{agentMCPTestSecretKey, agentMCPTestWeatherKey, agentMCPTestSkillKey}
	if !slices.Equal(agentCatalogKeys(catalog.Items), want) {
		t.Fatalf("items = %v, want %v", agentCatalogKeys(catalog.Items), want)
	}
}
