package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"github.com/go-chi/chi/v5"
	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	opendepotSigning "github.com/tonedefdev/opendepot/pkg/signing"
	"github.com/tonedefdev/opendepot/pkg/testutils"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
)

func TestBuildAgentVersionsResponse(t *testing.T) {
	entries := []struct {
		Version string `json:"version"`
		Synced  bool   `json:"synced"`
	}{
		{Version: "1.0.0", Synced: true},
		{Version: "1.1.0", Synced: true},
		{Version: "1.2.0", Synced: true},
		{Version: "0.9.0", Synced: false},
		{Version: "2.0.0", Synced: true},
	}

	yanked := agentTestSkillVersion("1.1.0")
	yanked.Spec.Yanked = true
	blocked := agentTestSkillVersion("1.2.0")
	blocked.Status.JevAssessment = &opendepotv1alpha1.JevAssessment{Blocked: true}
	unsynced := agentTestSkillVersion("0.9.0")
	unsynced.Status.Synced = false
	versions := map[string]opendepotv1alpha1.Version{
		"skill-my-skill-1-0-0": agentTestSkillVersion("1.0.0"),
		"skill-my-skill-1-1-0": yanked,
		"skill-my-skill-1-2-0": blocked,
		"skill-my-skill-0-9-0": unsynced,
	}

	response := buildAgentVersionsResponse(agentTestSkillKind(), "my-skill", entries, versions)
	if len(response.Versions) != 2 {
		t.Fatalf("buildAgentVersionsResponse() returned %d versions, want 2: %+v", len(response.Versions), response.Versions)
	}

	if response.Versions[0].Version != "1.0.0" || response.Versions[0].Yanked {
		t.Fatalf("versions[0] = %+v, want 1.0.0 not yanked", response.Versions[0])
	}

	if response.Versions[1].Version != "1.1.0" || !response.Versions[1].Yanked {
		t.Fatalf("versions[1] = %+v, want 1.1.0 yanked", response.Versions[1])
	}
}

func TestBuildAgentVersionsResponseExcludesMismatchedVersions(t *testing.T) {
	entries := []struct {
		Version string `json:"version"`
		Synced  bool   `json:"synced"`
	}{
		{Version: "1.0.0", Synced: true},
		{Version: "1.1.0", Synced: true},
		{Version: "1.2.0", Synced: true},
	}

	wrongType := agentTestSkillVersion("1.1.0")
	wrongType.Spec.Type = opendepotv1alpha1.OpenDepotAgent
	otherSource := agentTestSkillVersion("1.2.0")
	otherName := "other-skill"
	otherSource.Spec.AgentSourceRef.Name = &otherName
	versions := map[string]opendepotv1alpha1.Version{
		"skill-my-skill-1-0-0": agentTestSkillVersion("1.0.0"),
		"skill-my-skill-1-1-0": wrongType,
		"skill-my-skill-1-2-0": otherSource,
	}

	response := buildAgentVersionsResponse(agentTestSkillKind(), "my-skill", entries, versions)
	if len(response.Versions) != 1 || response.Versions[0].Version != "1.0.0" {
		t.Fatalf("buildAgentVersionsResponse() = %+v, want only 1.0.0", response.Versions)
	}
}

func agentTestSkillKind() agentKind {
	return agentKind{resource: "skills", prefix: "skill", versionType: opendepotv1alpha1.OpenDepotSkill}
}

func agentTestSkillVersion(version string) opendepotv1alpha1.Version {
	skillName := "my-skill"

	return opendepotv1alpha1.Version{
		Spec: opendepotv1alpha1.VersionSpec{
			Type:           opendepotv1alpha1.OpenDepotSkill,
			Version:        version,
			AgentSourceRef: &opendepotv1alpha1.AgentSourceConfig{Name: &skillName},
		},
		Status: opendepotv1alpha1.VersionStatus{Synced: true},
	}
}

func TestAgentVersionResourceName(t *testing.T) {
	if got := agentVersionResourceName("agent", "reviewer", "1.2.0"); got != "agent-reviewer-1-2-0" {
		t.Fatalf("agentVersionResourceName() = %q, want agent-reviewer-1-2-0", got)
	}
}

func TestAgentSigningKeys(t *testing.T) {
	privateKeyBase64, keyID := agentTestSigningKey(t)

	t.Run("not configured", func(t *testing.T) {
		_, err := agentSigningKeys("", keyID)
		if !errors.Is(err, errAgentSigningKeyNotConfigured) {
			t.Fatalf("agentSigningKeys() error = %v, want %v", err, errAgentSigningKeyNotConfigured)
		}
	})

	t.Run("matching fingerprint", func(t *testing.T) {
		keys, err := agentSigningKeys(privateKeyBase64, keyID)
		if err != nil {
			t.Fatalf("agentSigningKeys() error = %v", err)
		}

		if len(keys.GPGPublicKeys) != 1 {
			t.Fatalf("agentSigningKeys() returned %d keys, want 1", len(keys.GPGPublicKeys))
		}

		if !strings.Contains(keys.GPGPublicKeys[0].ASCIIArmor, "BEGIN PGP PUBLIC KEY BLOCK") {
			t.Fatalf("ascii_armor is not a public key block")
		}

		if strings.Contains(keys.GPGPublicKeys[0].ASCIIArmor, "PRIVATE KEY") {
			t.Fatalf("ascii_armor leaks private key material")
		}
	})

	t.Run("mismatched fingerprint is refused", func(t *testing.T) {
		keys, err := agentSigningKeys(privateKeyBase64, "0000000000000000000000000000000000000000")
		if err == nil {
			t.Fatalf("agentSigningKeys() error = nil, want fingerprint mismatch")
		}

		if keys != nil {
			t.Fatalf("agentSigningKeys() returned keys on mismatch: %+v", keys)
		}
	})

	t.Run("undecodable key", func(t *testing.T) {
		if _, err := agentSigningKeys(base64.StdEncoding.EncodeToString([]byte("not a key")), keyID); err == nil {
			t.Fatalf("agentSigningKeys() error = nil, want parse error")
		}
	})
}

func TestGetAgentDownloadMetadataSuccess(t *testing.T) {
	privateKeyBase64, fingerprint := agentTestSigningKey(t)
	t.Setenv("OPENDEPOT_PROVIDER_GPG_PRIVATE_KEY_BASE64", privateKeyBase64)

	version := agentTestVersion("agent-reviewer-1-2-0")
	version.Status.SigningKeyFingerprint = fingerprint
	api := agentTestVersionServer(t, version)
	defer api.Close()

	originalAnonymous := opendepotAnonymousAuth
	originalUseBearerToken := opendepotUseBearerToken
	originalVerifier := oidcVerifier
	defer func() {
		opendepotAnonymousAuth = originalAnonymous
		opendepotUseBearerToken = originalUseBearerToken
		oidcVerifier = originalVerifier
	}()

	anonymous := false
	useBearerToken := false
	opendepotAnonymousAuth = &anonymous
	opendepotUseBearerToken = &useBearerToken
	oidcVerifier = nil

	request := httptest.NewRequest(http.MethodGet, "http://registry.example.com/opendepot/agents/v1/team/agents/reviewer/1.2.0/download", nil)
	request.Header.Set("Authorization", base64.StdEncoding.EncodeToString([]byte(agentTestKubeconfig(api.URL, "download-shape-token"))))
	request.Header.Set("X-Forwarded-Proto", "https")

	response := httptest.NewRecorder()
	agentTestRouter().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("download metadata status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}

	var payload AgentDownloadResponse
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode download metadata: %v", err)
	}

	expectedBase := "https://registry.example.com/opendepot/agents/v1/team/agents/reviewer/1.2.0"
	if len(payload.Protocols) != 1 || payload.Protocols[0] != "1.0" {
		t.Fatalf("protocols = %#v, want [\"1.0\"]", payload.Protocols)
	}

	if payload.Kind != "agent" || payload.Name != "reviewer" || payload.Version != "1.2.0" {
		t.Fatalf("identity = %#v, want agent/reviewer/1.2.0", payload)
	}

	if payload.Filename != "reviewer-1.2.0.tar.gz" {
		t.Fatalf("filename = %q, want reviewer-1.2.0.tar.gz", payload.Filename)
	}

	if payload.DownloadURL != expectedBase+"/archive" || payload.ShasumsURL != expectedBase+"/SHA256SUMS" || payload.ShasumsSignatureURL != expectedBase+"/SHA256SUMS.sig" {
		t.Fatalf("unexpected URLs: %+v", payload)
	}

	if payload.Shasum != agentTestChecksumHex() {
		t.Fatalf("shasum = %q, want %q", payload.Shasum, agentTestChecksumHex())
	}

	if payload.SigningKeys == nil || len(payload.SigningKeys.GPGPublicKeys) != 1 {
		t.Fatalf("signing_keys = %#v, want one public key", payload.SigningKeys)
	}

	if payload.SigningKeys.GPGPublicKeys[0].KeyID != fingerprint {
		t.Fatalf("key_id = %q, want %q", payload.SigningKeys.GPGPublicKeys[0].KeyID, fingerprint)
	}

	if !strings.Contains(payload.SigningKeys.GPGPublicKeys[0].ASCIIArmor, "BEGIN PGP PUBLIC KEY BLOCK") {
		t.Fatalf("ascii_armor is not a public key block")
	}

	if strings.Contains(response.Body.String(), "PRIVATE KEY") {
		t.Fatalf("response leaked private key material: %s", response.Body.String())
	}
}

func TestServeAgentSHA256SUMSNotImplementedWhenAbsent(t *testing.T) {
	agentTestAnonymousAuth(t)
	version := agentTestVersion("agent-reviewer-1-2-0")
	version.Status.ShaSums = ""
	version.Status.ShaSumsSignature = ""
	api := agentTestVersionServer(t, version)
	defer api.Close()

	originalInClusterConfig := inClusterConfig
	defer func() { inClusterConfig = originalInClusterConfig }()
	inClusterConfig = func() (*rest.Config, error) {
		return &rest.Config{Host: api.URL}, nil
	}

	request := httptest.NewRequest(http.MethodGet, "http://registry.example.com/opendepot/agents/v1/team/agents/reviewer/1.2.0/SHA256SUMS", nil)
	response := httptest.NewRecorder()
	agentTestRouter().ServeHTTP(response, request)
	if response.Code != http.StatusNotImplemented {
		t.Fatalf("SHA256SUMS status = %d, want %d: %s", response.Code, http.StatusNotImplemented, response.Body.String())
	}
}

func TestServeAgentSHA256SUMSSignatureNotImplementedWhenAbsent(t *testing.T) {
	agentTestAnonymousAuth(t)
	version := agentTestVersion("agent-reviewer-1-2-0")
	version.Status.ShaSums = ""
	version.Status.ShaSumsSignature = ""
	api := agentTestVersionServer(t, version)
	defer api.Close()

	originalInClusterConfig := inClusterConfig
	defer func() { inClusterConfig = originalInClusterConfig }()
	inClusterConfig = func() (*rest.Config, error) {
		return &rest.Config{Host: api.URL}, nil
	}

	request := httptest.NewRequest(http.MethodGet, "http://registry.example.com/opendepot/agents/v1/team/agents/reviewer/1.2.0/SHA256SUMS.sig", nil)
	response := httptest.NewRecorder()
	agentTestRouter().ServeHTTP(response, request)
	if response.Code != http.StatusNotImplemented {
		t.Fatalf("SHA256SUMS.sig status = %d, want %d: %s", response.Code, http.StatusNotImplemented, response.Body.String())
	}
}

func TestGetAgentDownloadMetadataNotImplementedWhenSigningKeyNotConfigured(t *testing.T) {
	t.Setenv("OPENDEPOT_PROVIDER_GPG_PRIVATE_KEY_BASE64", "")

	version := agentTestVersion("agent-reviewer-1-2-0")
	api := agentTestVersionServer(t, version)
	defer api.Close()

	originalAnonymous := opendepotAnonymousAuth
	originalUseBearerToken := opendepotUseBearerToken
	originalVerifier := oidcVerifier
	defer func() {
		opendepotAnonymousAuth = originalAnonymous
		opendepotUseBearerToken = originalUseBearerToken
		oidcVerifier = originalVerifier
	}()

	anonymous := false
	useBearerToken := false
	opendepotAnonymousAuth = &anonymous
	opendepotUseBearerToken = &useBearerToken
	oidcVerifier = nil

	request := httptest.NewRequest(http.MethodGet, "http://registry.example.com/opendepot/agents/v1/team/agents/reviewer/1.2.0/download", nil)
	request.Header.Set("Authorization", base64.StdEncoding.EncodeToString([]byte(agentTestKubeconfig(api.URL, "missing-signing-key-token"))))
	response := httptest.NewRecorder()
	agentTestRouter().ServeHTTP(response, request)
	if response.Code != http.StatusNotImplemented {
		t.Fatalf("download metadata status = %d, want %d: %s", response.Code, http.StatusNotImplemented, response.Body.String())
	}
}

func TestServeAgentSHA256SUMSSignatureNotImplementedWhenSigningKeyNotConfigured(t *testing.T) {
	agentTestAnonymousAuth(t)
	t.Setenv("OPENDEPOT_PROVIDER_GPG_PRIVATE_KEY_BASE64", "")

	version := agentTestVersion("agent-reviewer-1-2-0")
	api := agentTestVersionServer(t, version)
	defer api.Close()

	originalInClusterConfig := inClusterConfig
	defer func() { inClusterConfig = originalInClusterConfig }()
	inClusterConfig = func() (*rest.Config, error) {
		return &rest.Config{Host: api.URL}, nil
	}

	request := httptest.NewRequest(http.MethodGet, "http://registry.example.com/opendepot/agents/v1/team/agents/reviewer/1.2.0/SHA256SUMS.sig", nil)
	response := httptest.NewRecorder()
	agentTestRouter().ServeHTTP(response, request)
	if response.Code != http.StatusNotImplemented {
		t.Fatalf("SHA256SUMS.sig status = %d, want %d: %s", response.Code, http.StatusNotImplemented, response.Body.String())
	}
}

func TestServeAgentArchiveServiceUnavailableWhenStorageConfigMissing(t *testing.T) {
	agentTestAnonymousAuth(t)
	version := agentTestVersion("agent-reviewer-1-2-0")
	version.Spec.AgentSourceRef.StorageConfig = nil
	api := agentTestVersionServer(t, version)
	defer api.Close()

	originalInClusterConfig := inClusterConfig
	defer func() { inClusterConfig = originalInClusterConfig }()
	inClusterConfig = func() (*rest.Config, error) {
		return &rest.Config{Host: api.URL}, nil
	}

	request := httptest.NewRequest(http.MethodGet, "http://registry.example.com/opendepot/agents/v1/team/agents/reviewer/1.2.0/archive", nil)
	response := httptest.NewRecorder()
	agentTestRouter().ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("archive status = %d, want %d: %s", response.Code, http.StatusServiceUnavailable, response.Body.String())
	}
}

func TestGetAgentDownloadMetadataInternalServerErrorOnFingerprintMismatch(t *testing.T) {
	privateKeyBase64, _ := agentTestSigningKey(t)
	t.Setenv("OPENDEPOT_PROVIDER_GPG_PRIVATE_KEY_BASE64", privateKeyBase64)

	version := agentTestVersion("agent-reviewer-1-2-0")
	version.Status.SigningKeyFingerprint = "0000000000000000000000000000000000000000"
	api := agentTestVersionServer(t, version)
	defer api.Close()

	originalAnonymous := opendepotAnonymousAuth
	originalUseBearerToken := opendepotUseBearerToken
	originalVerifier := oidcVerifier
	defer func() {
		opendepotAnonymousAuth = originalAnonymous
		opendepotUseBearerToken = originalUseBearerToken
		oidcVerifier = originalVerifier
	}()

	anonymous := false
	useBearerToken := false
	opendepotAnonymousAuth = &anonymous
	opendepotUseBearerToken = &useBearerToken
	oidcVerifier = nil

	request := httptest.NewRequest(http.MethodGet, "http://registry.example.com/opendepot/agents/v1/team/agents/reviewer/1.2.0/download", nil)
	request.Header.Set("Authorization", base64.StdEncoding.EncodeToString([]byte(agentTestKubeconfig(api.URL, "mismatch-token"))))
	response := httptest.NewRecorder()
	agentTestRouter().ServeHTTP(response, request)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("download metadata status = %d, want %d: %s", response.Code, http.StatusInternalServerError, response.Body.String())
	}

	body := response.Body.String()
	if strings.Contains(body, privateKeyBase64) || strings.Contains(body, "mismatch-token") || strings.Contains(body, "BEGIN PGP") || strings.Contains(body, "PRIVATE KEY") {
		t.Fatalf("response leaked key or token material: %s", body)
	}
}

func TestIsResourceAllowedSkillAndAgentPatterns(t *testing.T) {
	binding := &opendepotv1alpha1.GroupBinding{
		Spec: opendepotv1alpha1.GroupBindingSpec{
			SkillResources: []string{"release-*"},
			AgentResources: []string{"reviewer"},
		},
	}

	for _, test := range []struct {
		resourceType string
		name         string
		allowed      bool
	}{
		{resourceType: "skill", name: "release-tools", allowed: true},
		{resourceType: "skill", name: "reviewer", allowed: false},
		{resourceType: "agent", name: "reviewer", allowed: true},
		{resourceType: "agent", name: "release-tools", allowed: false},
	} {
		if got := isResourceAllowed(binding, test.resourceType, test.name); got != test.allowed {
			t.Errorf("isResourceAllowed(%s, %s) = %t, want %t", test.resourceType, test.name, got, test.allowed)
		}
	}
}

func TestIsSecurityResourceAllowedSkillAndAgentPatterns(t *testing.T) {
	binding := &opendepotv1alpha1.SecurityGroupBinding{
		Spec: opendepotv1alpha1.SecurityGroupBindingSpec{
			SkillResources: []string{"release-*"},
			AgentResources: []string{"reviewer"},
		},
	}

	for _, test := range []struct {
		resourceType string
		name         string
		allowed      bool
	}{
		{resourceType: "skill", name: "release-tools", allowed: true},
		{resourceType: "skill", name: "reviewer", allowed: false},
		{resourceType: "agent", name: "reviewer", allowed: true},
		{resourceType: "agent", name: "release-tools", allowed: false},
	} {
		if got := isSecurityResourceAllowed(binding, test.resourceType, test.name); got != test.allowed {
			t.Errorf("isSecurityResourceAllowed(%s, %s) = %t, want %t", test.resourceType, test.name, got, test.allowed)
		}
	}

	if isSecurityResourceAllowed(nil, "skill", "release-tools") {
		t.Fatalf("isSecurityResourceAllowed(nil) = true, want false")
	}
}

func TestAgentParentReadsControllerVersionRefs(t *testing.T) {
	raw := []byte(`{
		"spec": {"versions": [{"version": "1.0.0"}]},
		"status": {"versionRefs": {
			"1.1.0": {"version": "1.1.0", "synced": true},
			"0.9.0": {"version": "0.9.0", "synced": false},
			"1.0.0": {"version": "1.0.0", "synced": true}
		}}
	}`)

	var parent agentParent
	if err := json.Unmarshal(raw, &parent); err != nil {
		t.Fatalf("unmarshal agentParent: %v", err)
	}

	entries := parent.versionEntries()
	if len(entries) != 3 {
		t.Fatalf("versionEntries() returned %d entries, want 3: %+v", len(entries), entries)
	}

	if entries[0].Version != "0.9.0" || entries[1].Version != "1.0.0" || entries[2].Version != "1.1.0" {
		t.Fatalf("versionEntries() not sorted by version: %+v", entries)
	}

	versions := map[string]opendepotv1alpha1.Version{
		"skill-my-skill-1-0-0": agentTestSkillVersion("1.0.0"),
		"skill-my-skill-1-1-0": agentTestSkillVersion("1.1.0"),
	}

	response := buildAgentVersionsResponse(agentTestSkillKind(), "my-skill", entries, versions)
	if len(response.Versions) != 2 {
		t.Fatalf("buildAgentVersionsResponse() returned %d versions, want 2: %+v", len(response.Versions), response.Versions)
	}
}

func agentTestAnonymousAuth(t *testing.T) {
	t.Helper()

	original := opendepotAnonymousAuth
	anonymous := true
	opendepotAnonymousAuth = &anonymous
	t.Cleanup(func() { opendepotAnonymousAuth = original })
}

func agentTestRouter() http.Handler {
	router := chi.NewRouter()
	router.Get("/opendepot/agents/v1/{namespace}/{kind}/{name}/{version}/download", getAgentDownloadMetadata)
	router.Get("/opendepot/agents/v1/{namespace}/{kind}/{name}/{version}/archive", serveAgentArchive)
	router.Get("/opendepot/agents/v1/{namespace}/{kind}/{name}/{version}/SHA256SUMS", serveAgentSHA256SUMS)
	router.Get("/opendepot/agents/v1/{namespace}/{kind}/{name}/{version}/SHA256SUMS.sig", serveAgentSHA256SUMSSignature)

	return router
}

func agentTestKubeconfig(apiURL, token string) string {
	return fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
- name: test
  cluster:
    server: %s
users:
- name: test
  user:
    token: %s
contexts:
- name: test
  context:
    cluster: test
    user: test
current-context: test
`, apiURL, token)
}

func agentTestSigningKey(t *testing.T) (string, string) {
	t.Helper()

	if _, err := exec.LookPath("gpg"); err != nil {
		t.Skip("gpg binary not available")
	}

	gpgHome, err := os.MkdirTemp(".", "gpg-")
	if err != nil {
		t.Fatalf("os.MkdirTemp() error = %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(gpgHome) })

	if err = os.Chmod(gpgHome, 0700); err != nil {
		t.Fatalf("os.Chmod() error = %v", err)
	}

	_, _, privateKeyBase64, err := testutils.GenerateTestGPGKeyPair(gpgHome)
	if err != nil {
		t.Fatalf("GenerateTestGPGKeyPair() error = %v", err)
	}

	privateArmor, err := base64.StdEncoding.DecodeString(privateKeyBase64)
	if err != nil {
		t.Fatalf("DecodeString() error = %v", err)
	}

	signer, err := opendepotSigning.ParsePrivateKey(privateArmor, "")
	if err != nil {
		t.Fatalf("ParsePrivateKey() error = %v", err)
	}

	return privateKeyBase64, signer.Fingerprint()
}

func agentTestVersion(resourceName string) *opendepotv1alpha1.Version {
	fileName := "reviewer-1.2.0.tar.gz"
	checksum := agentTestChecksumBase64()
	archiveSize := int64(128)
	agentName := "reviewer"
	directoryPath := "artifacts"

	return &opendepotv1alpha1.Version{
		ObjectMeta: metav1.ObjectMeta{Name: resourceName},
		Spec: opendepotv1alpha1.VersionSpec{
			Type:     "Agent",
			Version:  "1.2.0",
			FileName: &fileName,
			AgentSourceRef: &opendepotv1alpha1.AgentSourceConfig{
				Name: &agentName,
				StorageConfig: &opendepotv1alpha1.StorageConfig{
					FileSystem: &opendepotv1alpha1.FileSystemConfig{DirectoryPath: &directoryPath},
				},
			},
		},
		Status: opendepotv1alpha1.VersionStatus{
			Synced:                true,
			Checksum:              &checksum,
			ArchiveSizeBytes:      &archiveSize,
			ShaSums:               "0123456789abcdef  reviewer-1.2.0.tar.gz\n",
			ShaSumsSignature:      "signed-shasums",
			SigningKeyFingerprint: "A4AD549F897092AB7A85F243664CA475A066D74C",
		},
	}
}

func agentTestVersionServer(t *testing.T, version *opendepotv1alpha1.Version) *httptest.Server {
	t.Helper()

	payload, err := json.Marshal(version)
	if err != nil {
		t.Fatalf("marshal version: %v", err)
	}

	path := "/apis/opendepot.defdev.io/v1alpha1/namespaces/team/versions/" + version.Name

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != path {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(payload)
	}))
}

func agentTestChecksumBase64() string {
	sum := sha256.Sum256([]byte("reviewer-1.2.0"))
	return base64.StdEncoding.EncodeToString(sum[:])
}

func agentTestChecksumHex() string {
	sum := sha256.Sum256([]byte("reviewer-1.2.0"))
	return fmt.Sprintf("%x", sum)
}

func TestGetAgentDownloadMetadataNotFoundForUnsyncedOrMismatchedVersion(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*opendepotv1alpha1.Version)
	}{
		{
			name:   "unsynced version",
			mutate: func(version *opendepotv1alpha1.Version) { version.Status.Synced = false },
		},
		{
			name:   "version type mismatch",
			mutate: func(version *opendepotv1alpha1.Version) { version.Spec.Type = opendepotv1alpha1.OpenDepotSkill },
		},
		{
			name: "source name mismatch",
			mutate: func(version *opendepotv1alpha1.Version) {
				other := "other-agent"
				version.Spec.AgentSourceRef.Name = &other
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			version := agentTestVersion("agent-reviewer-1-2-0")
			tc.mutate(version)
			api := agentTestVersionServer(t, version)
			defer api.Close()

			originalAnonymous := opendepotAnonymousAuth
			originalUseBearerToken := opendepotUseBearerToken
			originalVerifier := oidcVerifier
			defer func() {
				opendepotAnonymousAuth = originalAnonymous
				opendepotUseBearerToken = originalUseBearerToken
				oidcVerifier = originalVerifier
			}()

			anonymous := false
			useBearerToken := false
			opendepotAnonymousAuth = &anonymous
			opendepotUseBearerToken = &useBearerToken
			oidcVerifier = nil

			request := httptest.NewRequest(http.MethodGet, "http://registry.example.com/opendepot/agents/v1/team/agents/reviewer/1.2.0/download", nil)
			request.Header.Set("Authorization", base64.StdEncoding.EncodeToString([]byte(agentTestKubeconfig(api.URL, "not-found-token"))))
			response := httptest.NewRecorder()
			agentTestRouter().ServeHTTP(response, request)
			if response.Code != http.StatusNotFound {
				t.Fatalf("download metadata status = %d, want %d: %s", response.Code, http.StatusNotFound, response.Body.String())
			}
		})
	}
}

const agentTestIssuer = "https://issuer.example.com"

type agentTestKeySet struct{}

// VerifySignature skips signature checks so tests can mint tokens without a signing key.
func (agentTestKeySet) VerifySignature(_ context.Context, jwt string) ([]byte, error) {
	parts := strings.Split(jwt, ".")
	if len(parts) != 3 {
		return nil, errors.New("malformed token")
	}

	return base64.RawURLEncoding.DecodeString(parts[1])
}

func agentTestBearerToken(t *testing.T, groups []string) string {
	t.Helper()

	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	claims, err := json.Marshal(map[string]any{
		"iss":    agentTestIssuer,
		"aud":    "opendepot",
		"sub":    "alice",
		"iat":    time.Now().Unix(),
		"exp":    time.Now().Add(time.Hour).Unix(),
		"groups": groups,
	})
	if err != nil {
		t.Fatalf("marshal claims: %v", err)
	}

	return header + "." + base64.RawURLEncoding.EncodeToString(claims) + ".c2ln"
}

func TestAgentArtifactEndpointsEnforceGroupBinding(t *testing.T) {
	version := agentTestVersion("agent-reviewer-1-2-0")
	versionPath := "/apis/opendepot.defdev.io/v1alpha1/namespaces/team/versions/" + version.Name
	versionPayload, err := json.Marshal(version)
	if err != nil {
		t.Fatalf("marshal version: %v", err)
	}

	const namespace = "opendepot-system"
	originalAnonymous := opendepotAnonymousAuth
	originalVerifier := oidcVerifier
	originalInClusterConfig := inClusterConfig
	originalNamespace := opendepotServerNamespace
	originalGroupsClaim := opendepotOIDCGroupsClaim
	defer func() {
		opendepotAnonymousAuth = originalAnonymous
		oidcVerifier = originalVerifier
		inClusterConfig = originalInClusterConfig
		opendepotServerNamespace = originalNamespace
		opendepotOIDCGroupsClaim = originalGroupsClaim
	}()

	anonymous := false
	opendepotAnonymousAuth = &anonymous
	oidcVerifier = gooidc.NewVerifier(agentTestIssuer, agentTestKeySet{}, &gooidc.Config{ClientID: "opendepot"})
	opendepotServerNamespace = &[]string{namespace}[0]
	opendepotOIDCGroupsClaim = &[]string{"groups"}[0]

	cases := []struct {
		name           string
		agentResources []string
		denied         bool
	}{
		{name: "binding without agent grant", agentResources: []string{"other-agent"}, denied: true},
		{name: "binding without agent resources", denied: true},
		{name: "binding with agent grant", agentResources: []string{"reviewer"}, denied: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			binding := opendepotv1alpha1.GroupBinding{
				ObjectMeta: metav1.ObjectMeta{Name: "platform"},
				Spec: opendepotv1alpha1.GroupBindingSpec{
					Expression:     `"platform" in groups`,
					AgentResources: tc.agentResources,
				},
			}
			bindingPayload, err := json.Marshal(opendepotv1alpha1.GroupBindingList{Items: []opendepotv1alpha1.GroupBinding{binding}})
			if err != nil {
				t.Fatalf("marshal GroupBindingList: %v", err)
			}

			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case strings.HasSuffix(r.URL.Path, "/groupbindings"):
					_, _ = w.Write(bindingPayload)
				case r.URL.Path == versionPath:
					_, _ = w.Write(versionPayload)
				default:
					http.NotFound(w, r)
				}
			}))
			defer api.Close()

			inClusterConfig = func() (*rest.Config, error) {
				return &rest.Config{Host: api.URL}, nil
			}

			for _, endpoint := range []string{"download", "archive", "SHA256SUMS", "SHA256SUMS.sig"} {
				request := httptest.NewRequest(http.MethodGet, "http://registry.example.com/opendepot/agents/v1/team/agents/reviewer/1.2.0/"+endpoint, nil)
				request.Header.Set("Authorization", "Bearer "+agentTestBearerToken(t, []string{"platform"}))
				response := httptest.NewRecorder()
				agentTestRouter().ServeHTTP(response, request)

				if tc.denied && response.Code != http.StatusForbidden {
					t.Fatalf("%s status = %d, want %d: %s", endpoint, response.Code, http.StatusForbidden, response.Body.String())
				}

				if !tc.denied && response.Code == http.StatusForbidden {
					t.Fatalf("%s status = %d, want access granted: %s", endpoint, response.Code, response.Body.String())
				}
			}
		})
	}
}
