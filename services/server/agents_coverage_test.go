package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"github.com/go-chi/chi/v5"
	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
)

// agentCovRequestWithKind builds a request carrying a chi route context with the kind URL parameter set.
func agentCovRequestWithKind(kind string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("kind", kind)

	request := httptest.NewRequest(http.MethodGet, "http://registry.example.com/opendepot/agents/v1/team/"+kind+"/reviewer/1.2.0/archive", nil)
	return request.WithContext(context.WithValue(request.Context(), chi.RouteCtxKey, rctx))
}

func TestAgentCovKindFromRequest(t *testing.T) {
	cases := []struct {
		kind   string
		want   agentKind
		wantOK bool
	}{
		{kind: "skills", want: agentKind{resource: "skills", prefix: "skill", versionType: opendepotv1alpha1.OpenDepotSkill}, wantOK: true},
		{kind: "agents", want: agentKind{resource: "agents", prefix: "agent", versionType: opendepotv1alpha1.OpenDepotAgent}, wantOK: true},
		{kind: "modules"},
		{kind: ""},
	}

	for _, tc := range cases {
		t.Run("kind="+tc.kind, func(t *testing.T) {
			got, ok := agentKindFromRequest(agentCovRequestWithKind(tc.kind))
			if ok != tc.wantOK {
				t.Fatalf("agentKindFromRequest(%q) ok = %t, want %t", tc.kind, ok, tc.wantOK)
			}

			if got != tc.want {
				t.Fatalf("agentKindFromRequest(%q) = %+v, want %+v", tc.kind, got, tc.want)
			}
		})
	}
}

func TestAgentCovHandlersRejectUnknownKind(t *testing.T) {
	handlers := map[string]http.HandlerFunc{
		"getAgentVersions":              getAgentVersions,
		"serveAgentArchive":             serveAgentArchive,
		"serveAgentSHA256SUMS":          serveAgentSHA256SUMS,
		"serveAgentSHA256SUMSSignature": serveAgentSHA256SUMSSignature,
	}

	for name, handler := range handlers {
		t.Run(name, func(t *testing.T) {
			// A request without a kind route parameter resolves to no agent kind.
			request := httptest.NewRequest(http.MethodGet, "http://registry.example.com/opendepot/agents/v1/team/modules/reviewer/versions", nil)
			response := httptest.NewRecorder()
			handler(response, request)

			if response.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusNotFound, response.Body.String())
			}
		})
	}
}

func TestAgentCovPresignAgentArchiveSkipsWhenNotConfigured(t *testing.T) {
	name := "reviewer"
	fileName := "reviewer-1.2.0.tar.gz"
	enabled := false

	cases := []struct {
		name    string
		version *opendepotv1alpha1.Version
	}{
		{
			name:    "missing agent source ref",
			version: &opendepotv1alpha1.Version{Spec: opendepotv1alpha1.VersionSpec{FileName: &fileName}},
		},
		{
			name: "missing storage config",
			version: &opendepotv1alpha1.Version{Spec: opendepotv1alpha1.VersionSpec{
				FileName:       &fileName,
				AgentSourceRef: &opendepotv1alpha1.AgentSourceConfig{Name: &name},
			}},
		},
		{
			name: "missing agent name",
			version: &opendepotv1alpha1.Version{Spec: opendepotv1alpha1.VersionSpec{
				FileName: &fileName,
				AgentSourceRef: &opendepotv1alpha1.AgentSourceConfig{
					StorageConfig: &opendepotv1alpha1.StorageConfig{},
				},
			}},
		},
		{
			name: "missing file name",
			version: &opendepotv1alpha1.Version{Spec: opendepotv1alpha1.VersionSpec{
				AgentSourceRef: &opendepotv1alpha1.AgentSourceConfig{
					Name:          &name,
					StorageConfig: &opendepotv1alpha1.StorageConfig{},
				},
			}},
		},
		{
			name: "presign without config",
			version: &opendepotv1alpha1.Version{Spec: opendepotv1alpha1.VersionSpec{
				FileName: &fileName,
				AgentSourceRef: &opendepotv1alpha1.AgentSourceConfig{
					Name:          &name,
					StorageConfig: &opendepotv1alpha1.StorageConfig{},
				},
			}},
		},
		{
			name: "presign disabled",
			version: &opendepotv1alpha1.Version{Spec: opendepotv1alpha1.VersionSpec{
				FileName: &fileName,
				AgentSourceRef: &opendepotv1alpha1.AgentSourceConfig{
					Name: &name,
					StorageConfig: &opendepotv1alpha1.StorageConfig{
						Presign: &opendepotv1alpha1.PresignConfig{Enabled: &enabled},
					},
				},
			}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			url, err := presignAgentArchive(context.Background(), tc.version)
			if err != nil {
				t.Fatalf("presignAgentArchive() error = %v, want nil", err)
			}

			if url != "" {
				t.Fatalf("presignAgentArchive() = %q, want empty URL", url)
			}
		})
	}
}

func TestAgentCovGeneratePresignedURLRequiresChecksum(t *testing.T) {
	version := agentTestVersion("agent-reviewer-1-2-0")
	version.Status.Checksum = nil

	presignCfg := &opendepotv1alpha1.PresignConfig{}
	_, err := generateAgentPresignedURL(context.Background(), version, version.Spec.AgentSourceRef.StorageConfig, presignCfg)
	if err == nil || !strings.Contains(err.Error(), "checksum unavailable") {
		t.Fatalf("generateAgentPresignedURL() error = %v, want checksum unavailable", err)
	}
}

func TestAgentCovServeFromStorageFileSystemRequiresDirectoryPath(t *testing.T) {
	version := agentTestVersion("agent-reviewer-1-2-0")
	version.Spec.AgentSourceRef.StorageConfig.FileSystem.DirectoryPath = nil

	request := httptest.NewRequest(http.MethodGet, agentTestArchiveURL, nil)
	response := httptest.NewRecorder()
	serveAgentFromStorage(response, request, version)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusServiceUnavailable, response.Body.String())
	}
}

func TestAgentCovSHA256SUMSServesStoredChecksums(t *testing.T) {
	agentTestAnonymousAuth(t)
	version := agentTestVersion("agent-reviewer-1-2-0")
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

	if response.Code != http.StatusOK {
		t.Fatalf("SHA256SUMS status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}

	if got := response.Header().Get("Content-Type"); got != "text/plain; charset=utf-8" {
		t.Fatalf("Content-Type = %q, want text/plain; charset=utf-8", got)
	}

	if body := response.Body.String(); body != version.Status.ShaSums {
		t.Fatalf("SHA256SUMS body = %q, want %q", body, version.Status.ShaSums)
	}
}

func TestAgentCovVersionsDeniedWithoutAgentGrant(t *testing.T) {
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

	// The binding grants only a different agent, so the versions list for "reviewer" must be forbidden.
	binding := opendepotv1alpha1.GroupBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "platform"},
		Spec: opendepotv1alpha1.GroupBindingSpec{
			Expression:     `"platform" in groups`,
			AgentResources: []string{"other-agent"},
		},
	}
	bindingPayload, err := json.Marshal(opendepotv1alpha1.GroupBindingList{Items: []opendepotv1alpha1.GroupBinding{binding}})
	if err != nil {
		t.Fatalf("marshal GroupBindingList: %v", err)
	}

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/groupbindings") {
			_, _ = w.Write(bindingPayload)
			return
		}

		http.NotFound(w, r)
	}))
	defer api.Close()

	inClusterConfig = func() (*rest.Config, error) {
		return &rest.Config{Host: api.URL}, nil
	}

	request := httptest.NewRequest(http.MethodGet, "http://registry.example.com/opendepot/agents/v1/team/agents/reviewer/versions", nil)
	request.Header.Set("Authorization", "Bearer "+agentTestBearerToken(t, []string{"platform"}))
	response := httptest.NewRecorder()
	agentTestRouter().ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("versions status = %d, want %d: %s", response.Code, http.StatusForbidden, response.Body.String())
	}
}
