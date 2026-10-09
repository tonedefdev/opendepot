package main

import (
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

const browseAgentTestNamespace = "team"

func browseAgentTestObject(kind, name string, labels map[string]string, storage *opendepotv1alpha1.StorageConfig) any {
	meta := metav1.ObjectMeta{Name: name, Namespace: browseAgentTestNamespace, Labels: labels}
	source := opendepotv1alpha1.AgentSourceConfig{StorageConfig: storage}
	if kind == "agent" {
		return opendepotv1alpha1.Agent{ObjectMeta: meta, Spec: opendepotv1alpha1.AgentSpec{AgentSourceConfig: source}}
	}

	return opendepotv1alpha1.Skill{ObjectMeta: meta, Spec: opendepotv1alpha1.SkillSpec{AgentSourceConfig: source}}
}

func browseAgentTestVersion(kind, name string) opendepotv1alpha1.Version {
	versionType := opendepotv1alpha1.OpenDepotSkill
	if kind == "agent" {
		versionType = opendepotv1alpha1.OpenDepotAgent
	}

	sourceName := name
	return opendepotv1alpha1.Version{
		ObjectMeta: metav1.ObjectMeta{Name: name + "-1-0-0", Namespace: browseAgentTestNamespace},
		Spec: opendepotv1alpha1.VersionSpec{
			Type:           versionType,
			Version:        "1.0.0",
			AgentSourceRef: &opendepotv1alpha1.AgentSourceConfig{Name: &sourceName},
		},
		Status: opendepotv1alpha1.VersionStatus{Synced: true, SourceScan: &opendepotv1alpha1.SourceScan{}},
	}
}

// browseAgentTestServer serves the Kubernetes API paths read by the Skill and Agent browse handlers.
func browseAgentTestServer(t *testing.T, kind, name string, nsLabels map[string]string, object any, versions []opendepotv1alpha1.Version, bindings []opendepotv1alpha1.GroupBinding) *httptest.Server {
	t.Helper()

	namespacePayload, err := json.Marshal(map[string]any{"metadata": map[string]any{"name": browseAgentTestNamespace, "labels": nsLabels}})
	if err != nil {
		t.Fatalf("marshal namespace: %v", err)
	}

	resourcePayload, err := json.Marshal(object)
	if err != nil {
		t.Fatalf("marshal %s: %v", kind, err)
	}

	versionPayload, err := json.Marshal(opendepotv1alpha1.VersionList{Items: versions})
	if err != nil {
		t.Fatalf("marshal versions: %v", err)
	}

	bindingPayload, err := json.Marshal(opendepotv1alpha1.GroupBindingList{Items: bindings})
	if err != nil {
		t.Fatalf("marshal GroupBindingList: %v", err)
	}

	namespacePath := "/apis/opendepot.defdev.io/v1alpha1/namespaces/" + browseAgentTestNamespace
	resourcePath := namespacePath + "/" + browseAgentResource(kind) + "/" + name

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/v1/namespaces/"+browseAgentTestNamespace:
			_, _ = w.Write(namespacePayload)
		case r.URL.Path == resourcePath:
			_, _ = w.Write(resourcePayload)
		case r.URL.Path == namespacePath+"/versions":
			_, _ = w.Write(versionPayload)
		case strings.HasSuffix(r.URL.Path, "/groupbindings"):
			_, _ = w.Write(bindingPayload)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(api.Close)

	return api
}

// browseAgentTestSetup points the browse handlers at the fake API server and sets the auth mode.
// oidc enables OIDC verification with GroupBinding lookup; otherwise non-OIDC public-only mode applies
// unless anonymous is true.
func browseAgentTestSetup(t *testing.T, api *httptest.Server, anonymous, oidc bool) {
	t.Helper()

	originalAnonymous := opendepotAnonymousAuth
	originalVerifier := oidcVerifier
	originalInClusterConfig := inClusterConfig
	originalNamespace := opendepotServerNamespace
	originalGroupsClaim := opendepotOIDCGroupsClaim
	t.Cleanup(func() {
		opendepotAnonymousAuth = originalAnonymous
		oidcVerifier = originalVerifier
		inClusterConfig = originalInClusterConfig
		opendepotServerNamespace = originalNamespace
		opendepotOIDCGroupsClaim = originalGroupsClaim
	})

	opendepotAnonymousAuth = &anonymous
	oidcVerifier = nil
	if oidc {
		oidcVerifier = gooidc.NewVerifier(agentTestIssuer, agentTestKeySet{}, &gooidc.Config{ClientID: "opendepot"})
	}

	opendepotServerNamespace = &[]string{"opendepot-system"}[0]
	opendepotOIDCGroupsClaim = &[]string{"groups"}[0]
	inClusterConfig = func() (*rest.Config, error) {
		return &rest.Config{Host: api.URL}, nil
	}
}

func browseAgentTestRouter() http.Handler {
	router := chi.NewRouter()
	router.Get("/opendepot/ui/v1/resources/{namespace}/{kind}/{name}", handleBrowseResourceDetail)
	router.Get("/opendepot/ui/v1/resources/{namespace}/{kind}/{name}/versions", handleBrowseVersionsList)
	router.Get("/opendepot/ui/v1/resources/{namespace}/{kind}/{name}/scan-findings", handleBrowseScanFindings)

	return router
}

func TestBrowseAgentResourceDetailStorageConfig(t *testing.T) {
	s3Storage := &opendepotv1alpha1.StorageConfig{
		S3: &opendepotv1alpha1.AmazonS3Config{Bucket: "agent-bucket", Region: "us-west-2"},
	}

	cases := []struct {
		name        string
		storage     *opendepotv1alpha1.StorageConfig
		wantBackend string
		wantBucket  string
		wantRegion  string
	}{
		{name: "s3 storage config is returned", storage: s3Storage, wantBackend: "s3", wantBucket: "agent-bucket", wantRegion: "us-west-2"},
		{name: "missing storage config is nil"},
	}

	for _, kind := range []string{"skill", "agent"} {
		for _, tc := range cases {
			t.Run(kind+"/"+tc.name, func(t *testing.T) {
				const name = "reviewer"
				labels := map[string]string{labelPublic: "true"}
				api := browseAgentTestServer(t, kind, name, labels, browseAgentTestObject(kind, name, labels, tc.storage), nil, nil)
				browseAgentTestSetup(t, api, false, false)

				request := httptest.NewRequest(http.MethodGet, "http://registry.example.com/opendepot/ui/v1/resources/"+browseAgentTestNamespace+"/"+kind+"/"+name, nil)
				response := httptest.NewRecorder()
				browseAgentTestRouter().ServeHTTP(response, request)

				if response.Code != http.StatusOK {
					t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
				}

				var detail BrowseResourceDetail
				if err := json.Unmarshal(response.Body.Bytes(), &detail); err != nil {
					t.Fatalf("unmarshal detail: %v", err)
				}

				if tc.storage == nil {
					if detail.StorageConfig != nil {
						t.Fatalf("StorageConfig = %+v, want nil", detail.StorageConfig)
					}

					return
				}

				if detail.StorageConfig == nil {
					t.Fatalf("StorageConfig is nil, want backend %q", tc.wantBackend)
				}

				if detail.StorageConfig.Backend != tc.wantBackend || detail.StorageConfig.Bucket != tc.wantBucket || detail.StorageConfig.Region != tc.wantRegion {
					t.Fatalf("StorageConfig = %+v, want backend=%s bucket=%s region=%s", detail.StorageConfig, tc.wantBackend, tc.wantBucket, tc.wantRegion)
				}
			})
		}
	}
}

func TestBrowseAgentResourceDetailVisibility(t *testing.T) {
	cases := []struct {
		name           string
		kind           string
		resourceName   string
		nsPublic       bool
		resourcePublic bool
		anonymous      bool
		oidc           bool
		skillResources []string
		agentResources []string
		want           int
	}{
		{name: "private skill hidden without GroupBinding", kind: "skill", resourceName: "release-tools", nsPublic: true, want: http.StatusNotFound},
		{name: "public skill visible", kind: "skill", resourceName: "release-tools", nsPublic: true, resourcePublic: true, want: http.StatusOK},
		{name: "public agent in private namespace hidden", kind: "agent", resourceName: "reviewer", resourcePublic: true, want: http.StatusNotFound},
		{name: "anonymous auth shows private agent", kind: "agent", resourceName: "reviewer", anonymous: true, want: http.StatusOK},
		{name: "binding grants skill pattern", kind: "skill", resourceName: "release-tools", oidc: true, skillResources: []string{"release-*"}, want: http.StatusOK},
		{name: "binding denies skill outside pattern", kind: "skill", resourceName: "reviewer", oidc: true, skillResources: []string{"release-*"}, want: http.StatusNotFound},
		{name: "binding grants agent by name", kind: "agent", resourceName: "reviewer", oidc: true, agentResources: []string{"reviewer"}, want: http.StatusOK},
		{name: "binding without agent grant hides agent", kind: "agent", resourceName: "reviewer", oidc: true, agentResources: []string{"other-agent"}, want: http.StatusNotFound},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var labels map[string]string
			if tc.resourcePublic {
				labels = map[string]string{labelPublic: "true"}
			}

			var nsLabels map[string]string
			if tc.nsPublic {
				nsLabels = map[string]string{labelPublic: "true"}
			}

			binding := opendepotv1alpha1.GroupBinding{
				ObjectMeta: metav1.ObjectMeta{Name: "platform"},
				Spec: opendepotv1alpha1.GroupBindingSpec{
					Expression:     `"platform" in groups`,
					SkillResources: tc.skillResources,
					AgentResources: tc.agentResources,
				},
			}

			api := browseAgentTestServer(t, tc.kind, tc.resourceName, nsLabels, browseAgentTestObject(tc.kind, tc.resourceName, labels, nil), nil, []opendepotv1alpha1.GroupBinding{binding})
			browseAgentTestSetup(t, api, tc.anonymous, tc.oidc)

			request := httptest.NewRequest(http.MethodGet, "http://registry.example.com/opendepot/ui/v1/resources/"+browseAgentTestNamespace+"/"+tc.kind+"/"+tc.resourceName, nil)
			if tc.oidc {
				request.Header.Set("Authorization", "Bearer "+agentTestBearerToken(t, []string{"platform"}))
			}

			response := httptest.NewRecorder()
			browseAgentTestRouter().ServeHTTP(response, request)

			if response.Code != tc.want {
				t.Fatalf("status = %d, want %d: %s", response.Code, tc.want, response.Body.String())
			}
		})
	}
}

func TestBrowseAgentKindMappingVersionsAndScanFindings(t *testing.T) {
	cases := []struct {
		kind     string
		endpoint string
	}{
		{kind: "skill", endpoint: "versions"},
		{kind: "skill", endpoint: "scan-findings"},
		{kind: "agent", endpoint: "versions"},
		{kind: "agent", endpoint: "scan-findings"},
	}

	for _, tc := range cases {
		t.Run(tc.kind+"/"+tc.endpoint, func(t *testing.T) {
			const name = "reviewer"
			labels := map[string]string{labelPublic: "true"}
			versions := []opendepotv1alpha1.Version{browseAgentTestVersion(tc.kind, name)}
			api := browseAgentTestServer(t, tc.kind, name, labels, browseAgentTestObject(tc.kind, name, labels, nil), versions, nil)
			browseAgentTestSetup(t, api, false, false)

			request := httptest.NewRequest(http.MethodGet, "http://registry.example.com/opendepot/ui/v1/resources/"+browseAgentTestNamespace+"/"+tc.kind+"/"+name+"/"+tc.endpoint, nil)
			response := httptest.NewRecorder()
			browseAgentTestRouter().ServeHTTP(response, request)

			if response.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
			}

			if tc.endpoint == "versions" {
				var list BrowseVersionList
				if err := json.Unmarshal(response.Body.Bytes(), &list); err != nil {
					t.Fatalf("unmarshal versions: %v", err)
				}

				if list.TotalCount != 1 {
					t.Fatalf("TotalCount = %d, want 1", list.TotalCount)
				}

				return
			}

			var findings BrowseScanFindings
			if err := json.Unmarshal(response.Body.Bytes(), &findings); err != nil {
				t.Fatalf("unmarshal scan findings: %v", err)
			}

			if len(findings.ScannedVersions) != 1 || findings.ScannedVersions[0] != "1.0.0" {
				t.Fatalf("ScannedVersions = %v, want [1.0.0]", findings.ScannedVersions)
			}
		})
	}
}

func TestAgentVersionSummariesOmitDownloadMetadataForNonServableVersions(t *testing.T) {
	const name = "reviewer"
	cases := []struct {
		name         string
		mutate       func(v *opendepotv1alpha1.Version)
		wantDownload bool
	}{
		{name: "servable", mutate: func(v *opendepotv1alpha1.Version) {}, wantDownload: true},
		{name: "unsynced", mutate: func(v *opendepotv1alpha1.Version) { v.Status.Synced = false }},
		{name: "jev blocked", mutate: func(v *opendepotv1alpha1.Version) {
			v.Status.JevAssessment = &opendepotv1alpha1.JevAssessment{Blocked: true}
		}},
		{name: "mismatched source", mutate: func(v *opendepotv1alpha1.Version) {
			other := "other"
			v.Spec.AgentSourceRef.Name = &other
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := browseAgentTestVersion("skill", name)
			fileName := "reviewer-1.0.0.zip"
			v.Spec.FileName = &fileName
			checksum := "abc123"
			v.Status.Checksum = &checksum
			size := int64(2048)
			v.Status.ArchiveSizeBytes = &size
			tc.mutate(&v)

			summaries := agentVersionSummaries(browseAgentKind("skill"), name, []opendepotv1alpha1.Version{v})
			if len(summaries) != 1 {
				t.Fatalf("len(summaries) = %d, want 1", len(summaries))
			}

			s := summaries[0]
			if s.Version != "1.0.0" || s.Name != v.Name {
				t.Fatalf("row = %+v, want version row kept", s)
			}

			if tc.wantDownload {
				if s.FileName == nil || s.Checksum == nil || s.ArchiveSizeBytes == nil {
					t.Fatalf("download metadata = %+v, want populated", s)
				}

				return
			}

			if s.FileName != nil || s.Checksum != nil || s.ArchiveSizeBytes != nil {
				t.Fatalf("download metadata = %+v, want omitted", s)
			}
		})
	}
}
