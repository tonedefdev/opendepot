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
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
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
	if len(response.Versions) != 3 {
		t.Fatalf("buildAgentVersionsResponse() returned %d versions, want 3: %+v", len(response.Versions), response.Versions)
	}

	if response.Versions[0].Version != "1.0.0" || response.Versions[0].Yanked || response.Versions[0].Blocked {
		t.Fatalf("versions[0] = %+v, want 1.0.0 not yanked or blocked", response.Versions[0])
	}

	if response.Versions[1].Version != "1.1.0" || !response.Versions[1].Yanked {
		t.Fatalf("versions[1] = %+v, want 1.1.0 yanked", response.Versions[1])
	}

	if response.Versions[2].Version != "1.2.0" || !response.Versions[2].Blocked {
		t.Fatalf("versions[2] = %+v, want 1.2.0 blocked", response.Versions[2])
	}

	if agentVersionServable(&blocked, agentTestSkillKind(), "my-skill") {
		t.Fatal("blocked Version must not be servable")
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

// agentTestJSON marshals the value for use as a stubbed API response.
func agentTestJSON(t *testing.T, value any) []byte {
	t.Helper()

	payload, err := json.Marshal(value)

	if err != nil {
		t.Fatalf("marshal test payload: %v", err)
	}

	return payload
}

// agentTestAPIServer serves the payloads by request path and counts the requests made to each path.
func agentTestAPIServer(t *testing.T, payloads map[string][]byte) (*httptest.Server, func(string) int) {
	t.Helper()

	var mu sync.Mutex
	hits := map[string]int{}
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits[r.URL.Path]++
		mu.Unlock()

		payload, ok := payloads[r.URL.Path]

		if !ok {
			http.NotFound(w, r)

			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(payload)
	}))
	t.Cleanup(api.Close)

	return api, func(path string) int {
		mu.Lock()
		defer mu.Unlock()

		return hits[path]
	}
}

// agentTestUseKubeconfigAuth authenticates requests with the kubeconfig token and restores the globals on cleanup.
func agentTestUseKubeconfigAuth(t *testing.T) {
	t.Helper()

	originalAnonymous := opendepotAnonymousAuth
	originalUseBearerToken := opendepotUseBearerToken
	originalVerifier := oidcVerifier
	t.Cleanup(func() {
		opendepotAnonymousAuth = originalAnonymous
		opendepotUseBearerToken = originalUseBearerToken
		oidcVerifier = originalVerifier
	})

	anonymous := false
	useBearerToken := false
	opendepotAnonymousAuth = &anonymous
	opendepotUseBearerToken = &useBearerToken
	oidcVerifier = nil
}

// agentTestGet sends an authenticated GET to the agent router against the stubbed API server.
func agentTestGet(t *testing.T, api *httptest.Server, target string) *httptest.ResponseRecorder {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, target, nil)
	request.Header.Set("Authorization", base64.StdEncoding.EncodeToString([]byte(agentTestKubeconfig(api.URL, "agent-test-token"))))
	response := httptest.NewRecorder()
	agentTestRouter().ServeHTTP(response, request)

	return response
}

// agentTestVersionRef returns a Skill or Agent status.versionRefs entry for a synced Version.
func agentTestVersionRef(name, version string) map[string]any {
	return map[string]any{"name": name, "version": version, "commit": "3f2a9c1d", "synced": true}
}

// agentTestReviewerPayload returns the Agent resource with the given status.versionRefs.
func agentTestReviewerPayload(t *testing.T, versionRefs map[string]any) []byte {
	t.Helper()

	return agentTestJSON(t, map[string]any{
		"metadata": map[string]any{"name": "reviewer", "namespace": "team"},
		"status":   map[string]any{"versionRefs": versionRefs},
	})
}

const (
	agentTestBranchVersionName = "agent-reviewer-3f2a9c1d"
	agentTestVersionsPath      = "/apis/opendepot.defdev.io/v1alpha1/namespaces/team/versions"
	agentTestAgentPath         = "/apis/opendepot.defdev.io/v1alpha1/namespaces/team/agents/reviewer"
)

func TestAgentBranchVersionResolvesThroughParentRef(t *testing.T) {
	privateKeyBase64, fingerprint := agentTestSigningKey(t)
	t.Setenv("OPENDEPOT_PROVIDER_GPG_PRIVATE_KEY_BASE64", privateKeyBase64)

	version := agentTestVersion(agentTestBranchVersionName)
	version.Labels = map[string]string{"opendepot.defdev.io/agent": "reviewer"}
	agentTestUseFileSystemArchive(t, version)
	version.Status.SigningKeyFingerprint = fingerprint
	api, hits := agentTestAPIServer(t, map[string][]byte{
		agentTestVersionsPath + "/" + agentTestBranchVersionName: agentTestJSON(t, version),
		agentTestAgentPath: agentTestReviewerPayload(t, map[string]any{"1.2.0": agentTestVersionRef(agentTestBranchVersionName, "1.2.0")}),
	})
	agentTestUseKubeconfigAuth(t)

	for _, endpoint := range []string{"download", "archive", "SHA256SUMS", "SHA256SUMS.sig"} {
		response := agentTestGet(t, api, "http://registry.example.com/opendepot/agents/v1/team/agents/reviewer/1.2.0/"+endpoint)

		if response.Code == http.StatusNotFound || response.Code == http.StatusForbidden {
			t.Fatalf("%s status = %d, want the branch Version resolved: %s", endpoint, response.Code, response.Body.String())
		}

		if endpoint == "download" && response.Code != http.StatusOK {
			t.Fatalf("download status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
		}
	}

	if hits(agentTestAgentPath) == 0 {
		t.Fatalf("expected the fallback to read the parent Agent, got no request to %s", agentTestAgentPath)
	}
}

func TestAgentBranchVersionRefRejectsForeignOrMismatchedVersion(t *testing.T) {
	tests := []struct {
		name       string
		mutate     func(*opendepotv1alpha1.Version)
		versionRef map[string]any
	}{
		{
			name: "source name of another agent",
			mutate: func(version *opendepotv1alpha1.Version) {
				other := "other-agent"
				version.Spec.AgentSourceRef.Name = &other
			},
			versionRef: agentTestVersionRef(agentTestBranchVersionName, "1.2.0"),
		},
		{
			name: "label of another agent",
			mutate: func(version *opendepotv1alpha1.Version) {
				version.Labels = map[string]string{"opendepot.defdev.io/agent": "other-agent"}
			},
			versionRef: agentTestVersionRef(agentTestBranchVersionName, "1.2.0"),
		},
		{
			name:       "spec.version mismatch",
			mutate:     func(version *opendepotv1alpha1.Version) { version.Spec.Version = "1.3.0" },
			versionRef: agentTestVersionRef(agentTestBranchVersionName, "1.2.0"),
		},
		{
			name:       "no versionRefs entry for the requested version",
			mutate:     func(version *opendepotv1alpha1.Version) {},
			versionRef: agentTestVersionRef(agentTestBranchVersionName, "1.1.0"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			version := agentTestVersion(agentTestBranchVersionName)
			version.Labels = map[string]string{"opendepot.defdev.io/agent": "reviewer"}
			tc.mutate(version)
			api, _ := agentTestAPIServer(t, map[string][]byte{
				agentTestVersionsPath + "/" + agentTestBranchVersionName: agentTestJSON(t, version),
				agentTestAgentPath: agentTestReviewerPayload(t, map[string]any{tc.versionRef["version"].(string): tc.versionRef}),
			})
			agentTestUseKubeconfigAuth(t)

			response := agentTestGet(t, api, "http://registry.example.com/opendepot/agents/v1/team/agents/reviewer/1.2.0/download")

			if response.Code != http.StatusNotFound {
				t.Fatalf("download status = %d, want %d: %s", response.Code, http.StatusNotFound, response.Body.String())
			}
		})
	}
}

func TestAgentTagModeLookupMakesNoParentGet(t *testing.T) {
	privateKeyBase64, fingerprint := agentTestSigningKey(t)
	t.Setenv("OPENDEPOT_PROVIDER_GPG_PRIVATE_KEY_BASE64", privateKeyBase64)

	version := agentTestVersion("agent-reviewer-1-2-0")
	version.Status.SigningKeyFingerprint = fingerprint
	api, hits := agentTestAPIServer(t, map[string][]byte{
		agentTestVersionsPath + "/agent-reviewer-1-2-0": agentTestJSON(t, version),
	})
	agentTestUseKubeconfigAuth(t)

	response := agentTestGet(t, api, "http://registry.example.com/opendepot/agents/v1/team/agents/reviewer/1.2.0/download")

	if response.Code != http.StatusOK {
		t.Fatalf("download status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}

	if hits(agentTestAgentPath) != 0 {
		t.Fatalf("tag-mode lookup read the parent Agent %d times, want 0", hits(agentTestAgentPath))
	}
}

func TestGetAgentVersionsListsBranchVersionsByRefName(t *testing.T) {
	branch := agentTestVersion(agentTestBranchVersionName)
	branch.Labels = map[string]string{"opendepot.defdev.io/agent": "reviewer"}

	pending := agentTestVersion("agent-reviewer-9b8e7f6a")
	pending.Spec.Version = ""
	pending.Status.Synced = false

	api, _ := agentTestAPIServer(t, map[string][]byte{
		agentTestVersionsPath: agentTestJSON(t, opendepotv1alpha1.VersionList{Items: []opendepotv1alpha1.Version{*branch, *pending}}),
		agentTestAgentPath: agentTestReviewerPayload(t, map[string]any{
			"1.2.0": agentTestVersionRef(agentTestBranchVersionName, "1.2.0"),
			"1.3.0": agentTestVersionRef("agent-reviewer-missing", "1.3.0"),
		}),
	})
	agentTestUseKubeconfigAuth(t)

	response := agentTestGet(t, api, "http://registry.example.com/opendepot/agents/v1/team/agents/reviewer/versions")

	if response.Code != http.StatusOK {
		t.Fatalf("versions status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}

	var list AgentVersionsResponse

	if err := json.Unmarshal(response.Body.Bytes(), &list); err != nil {
		t.Fatalf("unmarshal versions: %v", err)
	}

	if len(list.Versions) != 1 || list.Versions[0].Version != "1.2.0" {
		t.Fatalf("Versions = %+v, want only [1.2.0]", list.Versions)
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
	router.Get("/opendepot/agents/v1/{namespace}/{kind}/{name}/versions", getAgentVersions)
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

	gpgHome, err := os.MkdirTemp("", "gpg-")
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
	agentTestUseFileSystemArchive(t, version)
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

// agentTestUseFileSystemArchive stores the reviewer archive under a test-local mount root and points the Version
// at it. The mount path is restored on cleanup.
func agentTestUseFileSystemArchive(t *testing.T, version *opendepotv1alpha1.Version) {
	t.Helper()

	root := t.TempDir()
	directory := filepath.Join(root, "artifacts")
	archiveDir := filepath.Join(directory, "agent-reviewer")
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		t.Fatalf("os.MkdirAll() error = %v", err)
	}

	if err := os.WriteFile(filepath.Join(archiveDir, "reviewer-1.2.0.tar.gz"), []byte("reviewer-1.2.0"), 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}

	original := opendepotFilesystemMountPath
	opendepotFilesystemMountPath = &root
	t.Cleanup(func() { opendepotFilesystemMountPath = original })

	version.Spec.AgentSourceRef.StorageConfig.FileSystem.DirectoryPath = &directory
}

// agentTestS3Version returns the reviewer Version stored in S3 with presign enabled.
func agentTestS3Version(key *string, fallbackToProxy bool) *opendepotv1alpha1.Version {
	enabled := true
	fallback := fallbackToProxy
	version := agentTestVersion("agent-reviewer-1-2-0")
	version.Spec.AgentSourceRef.StorageConfig = &opendepotv1alpha1.StorageConfig{
		S3:      &opendepotv1alpha1.AmazonS3Config{Bucket: "archives", Region: "us-east-1", Key: key},
		Presign: &opendepotv1alpha1.PresignConfig{Enabled: &enabled, FallbackToProxy: &fallback},
	}

	return version
}

// agentTestOIDCAuth authenticates requests with OIDC bearer tokens and restores the globals on cleanup.
func agentTestOIDCAuth(t *testing.T) {
	t.Helper()

	originalAnonymous := opendepotAnonymousAuth
	originalVerifier := oidcVerifier
	originalNamespace := opendepotServerNamespace
	originalGroupsClaim := opendepotOIDCGroupsClaim
	t.Cleanup(func() {
		opendepotAnonymousAuth = originalAnonymous
		oidcVerifier = originalVerifier
		opendepotServerNamespace = originalNamespace
		opendepotOIDCGroupsClaim = originalGroupsClaim
	})

	anonymous := false
	opendepotAnonymousAuth = &anonymous
	oidcVerifier = gooidc.NewVerifier(agentTestIssuer, agentTestKeySet{}, &gooidc.Config{ClientID: "opendepot"})
	opendepotServerNamespace = &[]string{"opendepot-system"}[0]
	opendepotOIDCGroupsClaim = &[]string{"groups"}[0]
}

// agentTestClusterAPI serves the Version and the GroupBinding list to the server's in-cluster client and points
// inClusterConfig at it. The restore runs on cleanup.
func agentTestClusterAPI(t *testing.T, version *opendepotv1alpha1.Version, bindings []opendepotv1alpha1.GroupBinding) {
	t.Helper()

	versionPayload := agentTestJSON(t, version)
	bindingPayload := agentTestJSON(t, opendepotv1alpha1.GroupBindingList{Items: bindings})
	versionPath := "/apis/opendepot.defdev.io/v1alpha1/namespaces/team/versions/" + version.Name

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
	t.Cleanup(api.Close)

	original := inClusterConfig
	inClusterConfig = func() (*rest.Config, error) {
		return &rest.Config{Host: api.URL}, nil
	}
	t.Cleanup(func() { inClusterConfig = original })
}

// agentTestServe sends a GET through the agent router with an optional Authorization header.
func agentTestServe(target, authorization string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, target, nil)
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}

	response := httptest.NewRecorder()
	agentTestRouter().ServeHTTP(response, request)

	return response
}

const agentTestArchiveURL = "http://registry.example.com/opendepot/agents/v1/team/agents/reviewer/1.2.0/archive"

func TestAgentArchiveRequiresTokenForPrivateAgent(t *testing.T) {
	agentTestOIDCAuth(t)

	response := agentTestServe(agentTestArchiveURL, "")
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("archive status = %d, want %d: %s", response.Code, http.StatusUnauthorized, response.Body.String())
	}

	if location := response.Header().Get("Location"); location != "" {
		t.Fatalf("unauthenticated request redirected to %q", location)
	}
}

func TestAgentArchiveAllowsAnonymousPublicAgent(t *testing.T) {
	fake := newAgentTestFakeS3(t, []byte("reviewer-1.2.0"), agentTestChecksumBase64(), 0)
	agentTestClusterAPI(t, agentTestS3Version(nil, false), nil)
	agentTestAnonymousAuth(t)

	response := agentTestServe(agentTestArchiveURL, "")
	if response.Code != http.StatusTemporaryRedirect {
		t.Fatalf("archive status = %d, want %d: %s", response.Code, http.StatusTemporaryRedirect, response.Body.String())
	}

	if requests := fake.requests(); len(requests) != 1 {
		t.Fatalf("storage requests = %v, want the checksum read only", requests)
	}
}

func TestAgentArchiveRevokedGroupBindingDeniedBeforePresign(t *testing.T) {
	fake := newAgentTestFakeS3(t, []byte("reviewer-1.2.0"), agentTestChecksumBase64(), 0)
	agentTestOIDCAuth(t)

	binding := opendepotv1alpha1.GroupBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "platform"},
		Spec: opendepotv1alpha1.GroupBindingSpec{
			Expression:     `"platform" in groups`,
			AgentResources: []string{"other-agent"},
		},
	}
	agentTestClusterAPI(t, agentTestS3Version(nil, false), []opendepotv1alpha1.GroupBinding{binding})

	response := agentTestServe(agentTestArchiveURL, "Bearer "+agentTestBearerToken(t, []string{"platform"}))
	if response.Code != http.StatusForbidden {
		t.Fatalf("archive status = %d, want %d: %s", response.Code, http.StatusForbidden, response.Body.String())
	}

	if location := response.Header().Get("Location"); location != "" {
		t.Fatalf("revoked request redirected to %q", location)
	}

	if requests := fake.requests(); len(requests) != 0 {
		t.Fatalf("storage requests = %v, want none for a revoked binding", requests)
	}
}

func TestAgentArchivePresignRedirectsWithPrivateNoStore(t *testing.T) {
	prefix := "prefix"
	fake := newAgentTestFakeS3(t, []byte("reviewer-1.2.0"), agentTestChecksumBase64(), 0)
	api := agentTestVersionServer(t, agentTestS3Version(&prefix, false))
	defer api.Close()
	agentTestUseKubeconfigAuth(t)

	response := agentTestGet(t, api, agentTestArchiveURL)
	if response.Code != http.StatusTemporaryRedirect {
		t.Fatalf("archive status = %d, want %d: %s", response.Code, http.StatusTemporaryRedirect, response.Body.String())
	}

	if cacheControl := response.Header().Get("Cache-Control"); cacheControl != "private, no-store" {
		t.Fatalf("Cache-Control = %q, want %q", cacheControl, "private, no-store")
	}

	location, err := url.Parse(response.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse Location: %v", err)
	}

	s3URL, err := url.Parse(fake.url)
	if err != nil {
		t.Fatalf("parse fake S3 URL: %v", err)
	}

	if location.Host != s3URL.Host {
		t.Fatalf("Location host = %q, want %q", location.Host, s3URL.Host)
	}

	if location.Path != "/archives/prefix/agent-reviewer/reviewer-1.2.0.tar.gz" {
		t.Fatalf("Location path = %q, want the controller's S3 key", location.Path)
	}

	if location.Query().Get("X-Amz-Signature") == "" {
		t.Fatalf("Location is not presigned: %s", location.String())
	}
}

func TestAgentArchiveChecksumMismatchNeverRedirects(t *testing.T) {
	tamperedSum := sha256.Sum256([]byte("tampered"))
	tampered := base64.StdEncoding.EncodeToString(tamperedSum[:])

	for _, fallback := range []bool{false, true} {
		t.Run(fmt.Sprintf("fallbackToProxy=%t", fallback), func(t *testing.T) {
			newAgentTestFakeS3(t, []byte("reviewer-1.2.0"), tampered, 0)
			agentTestClusterAPI(t, agentTestS3Version(nil, fallback), nil)
			agentTestAnonymousAuth(t)

			response := agentTestServe(agentTestArchiveURL, "")
			if response.Code != http.StatusInternalServerError {
				t.Fatalf("archive status = %d, want %d: %s", response.Code, http.StatusInternalServerError, response.Body.String())
			}

			if location := response.Header().Get("Location"); location != "" {
				t.Fatalf("checksum mismatch redirected to %q", location)
			}
		})
	}
}

func TestAgentArchivePresignErrorWithoutFallbackFails(t *testing.T) {
	fake := newAgentTestFakeS3(t, []byte("reviewer-1.2.0"), agentTestChecksumBase64(), 1)
	agentTestClusterAPI(t, agentTestS3Version(nil, false), nil)
	agentTestAnonymousAuth(t)

	response := agentTestServe(agentTestArchiveURL, "")
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("archive status = %d, want %d: %s", response.Code, http.StatusInternalServerError, response.Body.String())
	}

	if location := response.Header().Get("Location"); location != "" {
		t.Fatalf("presign error redirected to %q", location)
	}

	if requests := fake.requests(); len(requests) != 1 {
		t.Fatalf("storage requests = %v, want the failed presign checksum read only", requests)
	}
}

func TestAgentArchivePresignErrorFallbackStreamsBytes(t *testing.T) {
	fake := newAgentTestFakeS3(t, []byte("reviewer-1.2.0"), agentTestChecksumBase64(), 1)
	agentTestClusterAPI(t, agentTestS3Version(nil, true), nil)
	agentTestAnonymousAuth(t)

	response := agentTestServe(agentTestArchiveURL, "")
	if response.Code != http.StatusOK {
		t.Fatalf("archive status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}

	if location := response.Header().Get("Location"); location != "" {
		t.Fatalf("fallback redirected to %q", location)
	}

	if body := response.Body.String(); body != "reviewer-1.2.0" {
		t.Fatalf("body = %q, want the archive bytes", body)
	}

	if requests := fake.requests(); len(requests) != 3 {
		t.Fatalf("storage requests = %v, want the failed presign read plus the proxy checksum and body reads", requests)
	}
}

func TestAgentArchiveObjectKeyMatchesControllerLayout(t *testing.T) {
	key := func(value string) *string { return &value }
	tests := []struct {
		name string
		key  *string
		want string
	}{
		{name: "no S3 key", want: "agent-reviewer/reviewer-1.2.0.tar.gz"},
		{name: "S3 key prefix", key: key("prefix"), want: "prefix/agent-reviewer/reviewer-1.2.0.tar.gz"},
		{name: "S3 key with trailing slash", key: key("prefix/"), want: "prefix/agent-reviewer/reviewer-1.2.0.tar.gz"},
		{name: "nested S3 key", key: key("team/archives"), want: "team/archives/agent-reviewer/reviewer-1.2.0.tar.gz"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			version := agentTestVersion("agent-reviewer-1-2-0")
			version.Spec.AgentSourceRef.StorageConfig = &opendepotv1alpha1.StorageConfig{
				S3: &opendepotv1alpha1.AmazonS3Config{Bucket: "archives", Region: "us-east-1", Key: tc.key},
			}

			got, err := agentArchiveObjectKey(version)
			if err != nil {
				t.Fatalf("agentArchiveObjectKey() error = %v", err)
			}

			if got != tc.want {
				t.Fatalf("agentArchiveObjectKey() = %q, want %q", got, tc.want)
			}
		})
	}
}

const agentTestCommit = "3f2a9c1d0b7e4a5f6c8d9e0a1b2c3d4e5f607182"

// agentTestBranchVersion returns an unversioned branch Version sourced from the named Skill.
func agentTestBranchVersion(skillName, branchPhase, syncStatus string) opendepotv1alpha1.Version {
	return opendepotv1alpha1.Version{
		Spec: opendepotv1alpha1.VersionSpec{
			Type:           opendepotv1alpha1.OpenDepotSkill,
			SourceCommit:   agentTestCommit,
			AgentSourceRef: &opendepotv1alpha1.AgentSourceConfig{Name: &skillName},
		},
		Status: opendepotv1alpha1.VersionStatus{
			Synced:      false,
			BranchPhase: branchPhase,
			SyncStatus:  syncStatus,
		},
	}
}

func TestBuildAgentVersionsResponseListsBlockedCandidateUnderUnsyncedParent(t *testing.T) {
	entries := []struct {
		Version string `json:"version"`
		Synced  bool   `json:"synced"`
	}{
		{Version: "1.2.0", Synced: false},
	}

	blocked := agentTestSkillVersion("1.2.0")
	blocked.Status.Synced = false
	blocked.Status.JevAssessment = &opendepotv1alpha1.JevAssessment{Blocked: true, BlockReasons: []string{"cvss above threshold"}}
	versions := map[string]opendepotv1alpha1.Version{
		"skill-my-skill-1-2-0": blocked,
	}

	response := buildAgentVersionsResponse(agentTestSkillKind(), "my-skill", entries, versions)
	if len(response.Versions) != 0 {
		t.Fatalf("versions = %+v, want none because the parent ref is unsynced", response.Versions)
	}

	want := []AgentUnavailableCandidate{
		{Version: "1.2.0", Status: AgentAvailabilityBlocked, Reasons: []string{"cvss above threshold"}},
	}
	if !reflect.DeepEqual(response.UnavailableCandidates, want) {
		t.Fatalf("unavailableCandidates = %+v, want %+v", response.UnavailableCandidates, want)
	}
}

func TestBuildAgentUnavailableCandidatesAssociatesBranchRejectionWithMatchingSource(t *testing.T) {
	rejected := agentTestBranchVersion("my-skill", branchPhaseRejected, "Superseded: a newer commit exists as Version 'skill-my-skill-abc'")
	otherSource := agentTestBranchVersion("other-skill", branchPhaseRejected, "Superseded: a newer commit exists as Version 'skill-other-skill-abc'")
	versions := map[string]opendepotv1alpha1.Version{
		"skill-my-skill-abc":    rejected,
		"skill-other-skill-abc": otherSource,
	}

	candidates := buildAgentUnavailableCandidates(agentTestSkillKind(), "my-skill", versions)
	want := []AgentUnavailableCandidate{
		{Commit: agentTestCommit, Status: AgentAvailabilityRejected, Reasons: []string{agentReasonSuperseded}},
	}
	if !reflect.DeepEqual(candidates, want) {
		t.Fatalf("unavailableCandidates = %+v, want %+v", candidates, want)
	}
}

func TestBuildAgentUnavailableCandidatesKeepsPendingDistinctFromRejected(t *testing.T) {
	pending := agentTestBranchVersion("my-skill", branchPhasePending, "Waiting for skill-my-skill-older")
	rejected := agentTestBranchVersion("my-skill", branchPhaseRejected, "Invalid sourceCommit: not a commit")
	rejected.Spec.SourceCommit = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	versions := map[string]opendepotv1alpha1.Version{
		"skill-my-skill-pending":  pending,
		"skill-my-skill-rejected": rejected,
	}

	candidates := buildAgentUnavailableCandidates(agentTestSkillKind(), "my-skill", versions)
	if len(candidates) != 2 {
		t.Fatalf("unavailableCandidates = %+v, want 2 candidates", candidates)
	}

	for _, candidate := range candidates {
		switch candidate.Commit {
		case agentTestCommit:
			if candidate.Status != AgentAvailabilityPending {
				t.Fatalf("pending candidate status = %q, want %q", candidate.Status, AgentAvailabilityPending)
			}

			if candidate.Reasons[0] == agentReasonBranchRejected || candidate.Reasons[0] == agentReasonInvalidCommit {
				t.Fatalf("pending candidate reasons = %+v, want an in-progress reason", candidate.Reasons)
			}
		case "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa":
			if candidate.Status != AgentAvailabilityRejected || candidate.Reasons[0] != agentReasonInvalidCommit {
				t.Fatalf("rejected candidate = %+v, want rejected with invalid source commit reason", candidate)
			}
		default:
			t.Fatalf("unexpected candidate %+v", candidate)
		}
	}
}

func TestBuildAgentUnavailableCandidatesOmitsInstallableAndSemanticInProgress(t *testing.T) {
	installable := agentTestSkillVersion("1.0.0")
	inProgress := agentTestSkillVersion("1.1.0")
	inProgress.Status.Synced = false
	versions := map[string]opendepotv1alpha1.Version{
		"skill-my-skill-1-0-0": installable,
		"skill-my-skill-1-1-0": inProgress,
	}

	if candidates := buildAgentUnavailableCandidates(agentTestSkillKind(), "my-skill", versions); candidates != nil {
		t.Fatalf("unavailableCandidates = %+v, want none", candidates)
	}
}

func TestBuildAgentUnavailableCandidatesExcludesKindAndNameMismatch(t *testing.T) {
	otherName := agentTestBranchVersion("other-skill", branchPhaseRejected, "Superseded: a newer commit exists")
	agentType := agentTestBranchVersion("my-skill", branchPhaseRejected, "Superseded: a newer commit exists")
	agentType.Spec.Type = opendepotv1alpha1.OpenDepotAgent
	blockedOther := agentTestSkillVersion("2.0.0")
	blockedOther.Status.Synced = false
	otherSkill := "other-skill"
	blockedOther.Spec.AgentSourceRef.Name = &otherSkill
	blockedOther.Status.JevAssessment = &opendepotv1alpha1.JevAssessment{Blocked: true}
	versions := map[string]opendepotv1alpha1.Version{
		"skill-other-skill-abc":   otherName,
		"agent-my-skill-abc":      agentType,
		"skill-other-skill-2-0-0": blockedOther,
	}

	if candidates := buildAgentUnavailableCandidates(agentTestSkillKind(), "my-skill", versions); candidates != nil {
		t.Fatalf("unavailableCandidates = %+v, want none for mismatched kind or name", candidates)
	}
}

func TestAgentUnavailableCandidatesNeverServableOrDownloadable(t *testing.T) {
	kind := agentTestSkillKind()

	blocked := agentTestSkillVersion("1.2.0")
	blocked.Status.Synced = false
	blocked.Status.JevAssessment = &opendepotv1alpha1.JevAssessment{Blocked: true}
	rejected := agentTestBranchVersion("my-skill", branchPhaseRejected, "Scan policy violation: critical")
	pending := agentTestBranchVersion("my-skill", branchPhasePending, "")

	for name, version := range map[string]opendepotv1alpha1.Version{"blocked": blocked, "rejected": rejected, "pending": pending} {
		if agentVersionServable(&version, kind, "my-skill") {
			t.Fatalf("%s candidate is servable, want not servable", name)
		}
	}

	api := agentTestVersionServer(t, rejectedDownloadFixture())
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
	request.Header.Set("Authorization", base64.StdEncoding.EncodeToString([]byte(agentTestKubeconfig(api.URL, "rejected-token"))))
	response := httptest.NewRecorder()
	agentTestRouter().ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("download metadata status = %d, want %d: %s", response.Code, http.StatusNotFound, response.Body.String())
	}
}

// rejectedDownloadFixture returns a blocked versioned Agent Version that the download endpoint must refuse.
func rejectedDownloadFixture() *opendepotv1alpha1.Version {
	version := agentTestVersion("agent-reviewer-1-2-0")
	version.Status.Synced = false
	version.Status.JevAssessment = &opendepotv1alpha1.JevAssessment{Blocked: true, BlockReasons: []string{"cvss above threshold"}}

	return version
}

// TestGetAgentDownloadMetadataWithholdsExemptedByFromOIDC covers the download metadata an OIDC caller receives for an
// exempted Agent Version. The assessment keeps its state, but the ScanPolicy that exempted it is withheld.
func TestGetAgentDownloadMetadataWithholdsExemptedByFromOIDC(t *testing.T) {
	privateKeyBase64, fingerprint := agentTestSigningKey(t)
	t.Setenv("OPENDEPOT_PROVIDER_GPG_PRIVATE_KEY_BASE64", privateKeyBase64)

	version := agentTestVersion("agent-reviewer-1-2-0")
	version.Status.SigningKeyFingerprint = fingerprint
	version.Status.JevAssessment = &opendepotv1alpha1.JevAssessment{
		Exempted:     true,
		ExemptedBy:   "release-exemptions",
		BlockReasons: []string{"cvss above threshold"},
	}
	versionPath := "/apis/opendepot.defdev.io/v1alpha1/namespaces/team/versions/" + version.Name
	versionPayload, err := json.Marshal(version)
	if err != nil {
		t.Fatalf("marshal version: %v", err)
	}

	binding := opendepotv1alpha1.GroupBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "platform"},
		Spec: opendepotv1alpha1.GroupBindingSpec{
			Expression:     `"platform" in groups`,
			AgentResources: []string{"reviewer"},
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
	inClusterConfig = func() (*rest.Config, error) {
		return &rest.Config{Host: api.URL}, nil
	}

	request := httptest.NewRequest(http.MethodGet, "http://registry.example.com/opendepot/agents/v1/team/agents/reviewer/1.2.0/download", nil)
	request.Header.Set("Authorization", "Bearer "+agentTestBearerToken(t, []string{"platform"}))
	response := httptest.NewRecorder()
	agentTestRouter().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("download metadata status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}

	var payload AgentDownloadResponse
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode download metadata: %v", err)
	}

	if payload.Assessment == nil || !payload.Assessment.Exempted {
		t.Fatalf("assessment = %+v, want exempted assessment kept", payload.Assessment)
	}

	if payload.Assessment.ExemptedBy != "" {
		t.Fatalf("assessment.exemptedBy = %q, want withheld from OIDC caller", payload.Assessment.ExemptedBy)
	}

	if strings.Contains(response.Body.String(), "release-exemptions") {
		t.Fatalf("OIDC download metadata exposes the exempting policy: %s", response.Body.String())
	}
}

func TestGetKubeClientFromRequestAnonymousWritesErrorStatusOnClientFailure(t *testing.T) {
	originalAnonymous := opendepotAnonymousAuth
	originalInClusterConfig := inClusterConfig
	defer func() {
		opendepotAnonymousAuth = originalAnonymous
		inClusterConfig = originalInClusterConfig
	}()

	anonymous := true
	opendepotAnonymousAuth = &anonymous
	inClusterConfig = func() (*rest.Config, error) {
		return nil, errors.New("not running in cluster")
	}

	request := httptest.NewRequest(http.MethodGet, "http://registry.example.com/opendepot/agents/v1/team/agents/reviewer/1.2.0/download", nil)
	response := httptest.NewRecorder()
	clientset, _, _, _, err := getKubeClientFromRequest(response, request)
	if err == nil {
		t.Fatal("expected client setup failure")
	}

	if clientset != nil {
		t.Fatal("expected no clientset on client setup failure")
	}

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
}

func TestGetKubeClientFromRequestWritesErrorStatusOnAuthFailure(t *testing.T) {
	originalAnonymous := opendepotAnonymousAuth
	originalUseBearerToken := opendepotUseBearerToken
	originalVerifier := oidcVerifier
	originalInClusterConfig := inClusterConfig
	defer func() {
		opendepotAnonymousAuth = originalAnonymous
		opendepotUseBearerToken = originalUseBearerToken
		oidcVerifier = originalVerifier
		inClusterConfig = originalInClusterConfig
	}()

	anonymous := false
	useBearerToken := true
	opendepotAnonymousAuth = &anonymous
	opendepotUseBearerToken = &useBearerToken
	oidcVerifier = nil
	inClusterConfig = func() (*rest.Config, error) {
		return nil, errors.New("not running in cluster")
	}

	tests := []struct {
		name       string
		authHeader string
		wantStatus int
	}{
		{name: "missing authorization header", authHeader: "", wantStatus: http.StatusUnauthorized},
		{name: "server client unavailable", authHeader: "Bearer user-token", wantStatus: http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "http://registry.example.com/opendepot/agents/v1/team/agents/reviewer/1.2.0/download", nil)
			if tt.authHeader != "" {
				request.Header.Set("Authorization", tt.authHeader)
			}

			response := httptest.NewRecorder()
			clientset, _, _, _, err := getKubeClientFromRequest(response, request)
			if err == nil {
				t.Fatal("expected auth failure")
			}

			if clientset != nil {
				t.Fatal("expected no clientset on auth failure")
			}

			if response.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", response.Code, tt.wantStatus)
			}
		})
	}
}
