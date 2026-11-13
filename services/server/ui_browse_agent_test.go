package main

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"github.com/go-chi/chi/v5"
	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
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

	return browseAgentTestServerWithConfigMaps(t, kind, name, nsLabels, object, versions, bindings, nil)
}

// browseAgentTestServerWithConfigMaps is browseAgentTestServer with ConfigMaps served by name. Each value holds
// the ConfigMap data as stored, so binary content must already be base64 encoded.
func browseAgentTestServerWithConfigMaps(t *testing.T, kind, name string, nsLabels map[string]string, object any, versions []opendepotv1alpha1.Version, bindings []opendepotv1alpha1.GroupBinding, configMaps map[string]map[string]string) *httptest.Server {
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
		case strings.HasPrefix(r.URL.Path, "/api/v1/namespaces/"+browseAgentTestNamespace+"/configmaps/"):
			cmName := strings.TrimPrefix(r.URL.Path, "/api/v1/namespaces/"+browseAgentTestNamespace+"/configmaps/")
			data, ok := configMaps[cmName]
			if !ok {
				http.NotFound(w, r)
				return
			}

			cmPayload, err := json.Marshal(map[string]any{"metadata": map[string]any{"name": cmName, "namespace": browseAgentTestNamespace}, "data": data})
			if err != nil {
				t.Fatalf("marshal configmap: %v", err)
			}

			_, _ = w.Write(cmPayload)
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

// TestBrowseAgentResourceDetailWithholdsExemptionNamesFromOIDC covers the newest Jev assessment in the detail response.
// The ScanPolicy that exempted it is withheld from OIDC callers, along with the governance names. Without OIDC the
// names are returned, which shows that the assessment carries them.
func TestBrowseAgentResourceDetailWithholdsExemptionNamesFromOIDC(t *testing.T) {
	const name = "reviewer"
	const policyName = "release-exemptions"
	labels := map[string]string{labelPublic: "true"}
	assessed := browseAgentTestVersion("agent", name)
	assessed.Status.JevAssessment = &opendepotv1alpha1.JevAssessment{
		EvaluatedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Exempted:    true,
		ExemptedBy:  policyName,
	}
	assessed.Status.JevExemptionGovernance = &opendepotv1alpha1.JevExemptionGovernance{
		GoverningPolicy: policyName,
		MatchingPolicy:  policyName,
		MatchingState:   opendepotv1alpha1.JevMatchingStateExempted,
	}
	assessed.Status.JevUnblock = &opendepotv1alpha1.JevUnblock{GoverningPolicy: policyName, MatchingPolicy: policyName}
	binding := opendepotv1alpha1.GroupBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "platform"},
		Spec: opendepotv1alpha1.GroupBindingSpec{
			Expression:     `"platform" in groups`,
			AgentResources: []string{name},
		},
	}

	cases := []struct {
		name           string
		oidc           bool
		wantExemptedBy string
	}{
		{name: "OIDC withholds policy names", oidc: true, wantExemptedBy: ""},
		{name: "non-OIDC returns policy names", wantExemptedBy: policyName},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			api := browseAgentTestServer(t, "agent", name, labels, browseAgentTestObject("agent", name, labels, nil), []opendepotv1alpha1.Version{assessed}, []opendepotv1alpha1.GroupBinding{binding})
			browseAgentTestSetup(t, api, false, tc.oidc)

			request := httptest.NewRequest(http.MethodGet, "http://registry.example.com/opendepot/ui/v1/resources/"+browseAgentTestNamespace+"/agent/"+name, nil)
			if tc.oidc {
				request.Header.Set("Authorization", "Bearer "+agentTestBearerToken(t, []string{"platform"}))
			}

			response := httptest.NewRecorder()
			browseAgentTestRouter().ServeHTTP(response, request)

			if response.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
			}

			var detail BrowseResourceDetail
			if err := json.Unmarshal(response.Body.Bytes(), &detail); err != nil {
				t.Fatalf("unmarshal detail: %v", err)
			}

			if detail.JevAssessment == nil || !detail.JevAssessment.Exempted {
				t.Fatalf("JevAssessment = %+v, want exempted assessment", detail.JevAssessment)
			}

			if detail.JevAssessment.ExemptedBy != tc.wantExemptedBy {
				t.Fatalf("JevAssessment.ExemptedBy = %q, want %q", detail.JevAssessment.ExemptedBy, tc.wantExemptedBy)
			}

			if detail.JevExemptionGovernance == nil || detail.JevExemptionGovernance.MatchingState != opendepotv1alpha1.JevMatchingStateExempted {
				t.Fatalf("JevExemptionGovernance = %+v, want exempted state kept", detail.JevExemptionGovernance)
			}

			if tc.oidc {
				if detail.JevExemptionGovernance.GoverningPolicy != "" || detail.JevExemptionGovernance.MatchingPolicy != "" {
					t.Fatalf("JevExemptionGovernance = %+v, want policy names withheld", detail.JevExemptionGovernance)
				}

				if detail.JevUnblock == nil || detail.JevUnblock.GoverningPolicy != "" || detail.JevUnblock.MatchingPolicy != "" || !detail.JevUnblock.NamesHidden {
					t.Fatalf("JevUnblock = %+v, want policy names withheld and NamesHidden", detail.JevUnblock)
				}

				if strings.Contains(response.Body.String(), `"exemptedBy"`) || strings.Contains(response.Body.String(), policyName) {
					t.Fatalf("OIDC detail exposes the exempting policy: %s", response.Body.String())
				}
			}
		})
	}
}

// TestBrowseScanFindingsWithholdsExemptedByFromOIDC covers the two responses that carry scan findings to ScanDrillDown:
// the scan-findings endpoint and the resource detail. The ScanPolicy that exempted a finding is withheld from OIDC
// callers. Without OIDC the names are returned, which shows that the findings carry them.
func TestBrowseScanFindingsWithholdsExemptedByFromOIDC(t *testing.T) {
	const name = "reviewer"
	const policyName = "release-exemptions"
	labels := map[string]string{labelPublic: "true"}
	scanned := browseAgentTestVersion("agent", name)
	scanned.Status.SourceScan = &opendepotv1alpha1.SourceScan{
		Findings: []opendepotv1alpha1.SecurityFinding{{
			VulnerabilityID:  "CVE-2026-0001",
			PkgName:          "left-pad",
			InstalledVersion: "1.0.0",
			Severity:         "HIGH",
			Exempted:         true,
			ExemptionReason:  "patched by the base image",
			ExemptedBy:       policyName,
		}},
	}
	binding := opendepotv1alpha1.GroupBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "platform"},
		Spec: opendepotv1alpha1.GroupBindingSpec{
			Expression:     `"platform" in groups`,
			AgentResources: []string{name},
		},
	}

	cases := []struct {
		name           string
		path           string
		oidc           bool
		wantExemptedBy string
	}{
		{name: "scan-findings withholds policy names from OIDC", path: "/scan-findings", oidc: true, wantExemptedBy: ""},
		{name: "scan-findings returns policy names without OIDC", path: "/scan-findings", wantExemptedBy: policyName},
		{name: "detail withholds policy names from OIDC", path: "", oidc: true, wantExemptedBy: ""},
		{name: "detail returns policy names without OIDC", path: "", wantExemptedBy: policyName},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			api := browseAgentTestServer(t, "agent", name, labels, browseAgentTestObject("agent", name, labels, nil), []opendepotv1alpha1.Version{scanned}, []opendepotv1alpha1.GroupBinding{binding})
			browseAgentTestSetup(t, api, false, tc.oidc)

			request := httptest.NewRequest(http.MethodGet, "http://registry.example.com/opendepot/ui/v1/resources/"+browseAgentTestNamespace+"/agent/"+name+tc.path, nil)
			if tc.oidc {
				request.Header.Set("Authorization", "Bearer "+agentTestBearerToken(t, []string{"platform"}))
			}

			response := httptest.NewRecorder()
			browseAgentTestRouter().ServeHTTP(response, request)

			if response.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
			}

			var findings []opendepotv1alpha1.SecurityFinding
			if tc.path == "/scan-findings" {
				var body BrowseScanFindings
				if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
					t.Fatalf("unmarshal scan findings: %v", err)
				}
				findings = body.SourceScanFindings
			} else {
				var body BrowseResourceDetail
				if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
					t.Fatalf("unmarshal detail: %v", err)
				}
				findings = body.SourceScanFindings
			}

			if len(findings) != 1 {
				t.Fatalf("len(SourceScanFindings) = %d, want 1", len(findings))
			}

			if findings[0].ExemptedBy != tc.wantExemptedBy {
				t.Fatalf("ExemptedBy = %q, want %q", findings[0].ExemptedBy, tc.wantExemptedBy)
			}

			if !findings[0].Exempted || findings[0].ExemptionReason != "patched by the base image" {
				t.Fatalf("finding = %+v, want exempted finding with its reason", findings[0])
			}
		})
	}
}

// TestBrowseBinaryScanFindingsWithholdsExemptedByFromOIDC covers provider binary findings. The agent handlers do not
// return them, so the helper is tested directly. The input must keep its policy name so the cached status is unchanged.
func TestBrowseBinaryScanFindingsWithholdsExemptedByFromOIDC(t *testing.T) {
	const policyName = "release-exemptions"
	findings := map[string][]opendepotv1alpha1.SecurityFinding{
		"linux/amd64": {{VulnerabilityID: "CVE-2026-0002", Severity: "HIGH", Exempted: true, ExemptedBy: policyName}},
	}

	cases := []struct {
		name           string
		oidc           bool
		wantExemptedBy string
	}{
		{name: "OIDC withholds policy names", oidc: true, wantExemptedBy: ""},
		{name: "non-OIDC returns policy names", wantExemptedBy: policyName},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			originalVerifier := oidcVerifier
			t.Cleanup(func() {
				oidcVerifier = originalVerifier
			})

			oidcVerifier = nil
			if tc.oidc {
				oidcVerifier = gooidc.NewVerifier(agentTestIssuer, agentTestKeySet{}, &gooidc.Config{ClientID: "opendepot"})
			}

			got := browseBinaryScanFindings(findings)
			if len(got["linux/amd64"]) != 1 {
				t.Fatalf("len(binary findings) = %d, want 1", len(got["linux/amd64"]))
			}

			if got["linux/amd64"][0].ExemptedBy != tc.wantExemptedBy {
				t.Fatalf("ExemptedBy = %q, want %q", got["linux/amd64"][0].ExemptedBy, tc.wantExemptedBy)
			}

			if findings["linux/amd64"][0].ExemptedBy != policyName {
				t.Fatalf("input ExemptedBy = %q, want %q: helper must not modify its input", findings["linux/amd64"][0].ExemptedBy, policyName)
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

func TestBrowseAgentVersionsSkipVersionsWithoutSpecVersion(t *testing.T) {
	const name = "reviewer"
	labels := map[string]string{labelPublic: "true"}
	pending := browseAgentTestVersion("agent", name)
	pending.Name = "agent-reviewer-9b8e7f6a"
	pending.Spec.Version = ""
	versions := []opendepotv1alpha1.Version{browseAgentTestVersion("agent", name), pending}
	api := browseAgentTestServer(t, "agent", name, labels, browseAgentTestObject("agent", name, labels, nil), versions, nil)
	browseAgentTestSetup(t, api, false, false)

	request := httptest.NewRequest(http.MethodGet, "http://registry.example.com/opendepot/ui/v1/resources/"+browseAgentTestNamespace+"/agent/"+name+"/versions", nil)
	response := httptest.NewRecorder()
	browseAgentTestRouter().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}

	var list BrowseVersionList

	if err := json.Unmarshal(response.Body.Bytes(), &list); err != nil {
		t.Fatalf("unmarshal versions: %v", err)
	}

	if list.TotalCount != 1 {
		t.Fatalf("TotalCount = %d, want 1", list.TotalCount)
	}
}

func TestBrowseAgentVersionsListsBlockedAndRejectedBranchVersions(t *testing.T) {
	const name = "reviewer"
	labels := map[string]string{labelPublic: "true"}
	blocked := browseAgentTestVersion("agent", name)
	blocked.Name = "agent-reviewer-blocked"
	blocked.Spec.Version = ""
	blocked.Status.Synced = false
	blocked.Status.SyncStatus = "JevBlocked: part 3 is rated Critical risk, which always blocks"
	blocked.Status.JevAssessment = &opendepotv1alpha1.JevAssessment{
		Blocked:      true,
		BlockReasons: []string{"part 3 is rated Critical risk, which always blocks"},
	}
	rejected := browseAgentTestVersion("agent", name)
	rejected.Name = "agent-reviewer-rejected"
	rejected.Spec.Version = ""
	rejected.Status.Synced = false
	rejected.Status.BranchPhase = branchPhaseRejected
	pending := browseAgentTestVersion("agent", name)
	pending.Name = "agent-reviewer-pending"
	pending.Spec.Version = ""
	versions := []opendepotv1alpha1.Version{browseAgentTestVersion("agent", name), blocked, rejected, pending}
	api := browseAgentTestServer(t, "agent", name, labels, browseAgentTestObject("agent", name, labels, nil), versions, nil)
	browseAgentTestSetup(t, api, false, false)

	request := httptest.NewRequest(http.MethodGet, "http://registry.example.com/opendepot/ui/v1/resources/"+browseAgentTestNamespace+"/agent/"+name+"/versions", nil)
	response := httptest.NewRecorder()
	browseAgentTestRouter().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}

	var list BrowseVersionList
	if err := json.Unmarshal(response.Body.Bytes(), &list); err != nil {
		t.Fatalf("unmarshal versions: %v", err)
	}

	if list.TotalCount != 3 {
		t.Fatalf("TotalCount = %d, want 3 (assigned, blocked, rejected; pending hidden)", list.TotalCount)
	}

	byName := make(map[string]BrowseVersionSummary, len(list.Items))
	for _, s := range list.Items {
		byName[s.Name] = s
	}

	if _, ok := byName[pending.Name]; ok {
		t.Fatalf("pending Version %s listed, want hidden", pending.Name)
	}

	b := byName[blocked.Name]
	if !b.Blocked || len(b.BlockReasons) != 1 || b.BlockReasons[0] != "part 3 is rated Critical risk, which always blocks" {
		t.Fatalf("blocked row = %+v, want Blocked with reason", b)
	}

	if b.FileName != nil || b.Checksum != nil {
		t.Fatalf("blocked row exposes download metadata: %+v", b)
	}

	r := byName[rejected.Name]
	if r.Blocked || r.BranchPhase != branchPhaseRejected {
		t.Fatalf("rejected row = %+v, want BranchPhase Rejected without Blocked", r)
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

func TestBrowseAgentDetailDefinitionAndSourceFields(t *testing.T) {
	const (
		name       = "reviewer"
		commitSHA  = "0123456789abcdef0123456789abcdef01234567"
		definition = "# Reviewer\n"
	)

	labels := map[string]string{labelPublic: "true"}
	for _, kind := range []string{"skill", "agent"} {
		t.Run(kind, func(t *testing.T) {
			fileName := "SKILL.md"
			if kind == "agent" {
				fileName = "AGENT.md"
			}

			source := opendepotv1alpha1.AgentSourceConfig{
				RepoOwner: "acme",
				RepoUrl:   &[]string{"https://github.com/acme/" + name}[0],
				Path:      "skills/" + name,
				Ref:       &[]string{"main"}[0],
			}

			var object any
			if kind == "agent" {
				object = opendepotv1alpha1.Agent{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: browseAgentTestNamespace, Labels: labels}, Spec: opendepotv1alpha1.AgentSpec{AgentSourceConfig: source}}
			} else {
				object = opendepotv1alpha1.Skill{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: browseAgentTestNamespace, Labels: labels}, Spec: opendepotv1alpha1.SkillSpec{AgentSourceConfig: source}}
			}

			version := browseAgentTestVersion(kind, name)
			version.Status.SourceCommit = commitSHA
			version.Status.BranchPhase = "Pending"
			version.Status.DefinitionConfigMapRef = &opendepotv1alpha1.DefinitionConfigMapRef{Name: name + "-definition", Key: "definition", FileName: fileName}
			configMaps := map[string]map[string]string{
				name + "-definition": {"definition": base64.StdEncoding.EncodeToString([]byte(definition))},
			}

			api := browseAgentTestServerWithConfigMaps(t, kind, name, labels, object, []opendepotv1alpha1.Version{version}, nil, configMaps)
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

			if detail.ReadmeContent != nil {
				t.Fatalf("ReadmeContent = %q, want nil for %s", *detail.ReadmeContent, kind)
			}

			if detail.DefinitionContent == nil || *detail.DefinitionContent != definition {
				t.Fatalf("DefinitionContent = %v, want %q", detail.DefinitionContent, definition)
			}

			if detail.DefinitionFileName != fileName {
				t.Fatalf("DefinitionFileName = %q, want %q", detail.DefinitionFileName, fileName)
			}

			if detail.RepoURL != "https://github.com/acme/"+name || detail.RepoOwner != "acme" || detail.SourcePath != "skills/"+name {
				t.Fatalf("repo fields = url %q owner %q path %q", detail.RepoURL, detail.RepoOwner, detail.SourcePath)
			}

			if detail.SourceRef != "main" || detail.SourceCommit != commitSHA {
				t.Fatalf("SourceRef = %q, SourceCommit = %q; want main and %s", detail.SourceRef, detail.SourceCommit, commitSHA)
			}

			if len(detail.Versions) != 1 || detail.Versions[0].Commit != commitSHA || detail.Versions[0].BranchPhase != "Pending" {
				t.Fatalf("Versions = %+v; want commit %s and phase %s", detail.Versions, commitSHA, "Pending")
			}
		})
	}
}

func TestBrowseAgentDetailTagModeOmitsRefAndCommit(t *testing.T) {
	const name = "reviewer"
	labels := map[string]string{labelPublic: "true"}
	object := opendepotv1alpha1.Skill{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: browseAgentTestNamespace, Labels: labels},
		Spec:       opendepotv1alpha1.SkillSpec{AgentSourceConfig: opendepotv1alpha1.AgentSourceConfig{RepoOwner: "acme"}},
	}

	api := browseAgentTestServer(t, "skill", name, labels, object, []opendepotv1alpha1.Version{browseAgentTestVersion("skill", name)}, nil)
	browseAgentTestSetup(t, api, false, false)

	request := httptest.NewRequest(http.MethodGet, "http://registry.example.com/opendepot/ui/v1/resources/"+browseAgentTestNamespace+"/skill/"+name, nil)
	response := httptest.NewRecorder()
	browseAgentTestRouter().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}

	var detail BrowseResourceDetail
	if err := json.Unmarshal(response.Body.Bytes(), &detail); err != nil {
		t.Fatalf("unmarshal detail: %v", err)
	}

	if detail.SourceRef != "" || detail.SourceCommit != "" {
		t.Fatalf("SourceRef = %q, SourceCommit = %q; want empty for tag mode", detail.SourceRef, detail.SourceCommit)
	}

	if detail.DefinitionContent != nil || detail.DefinitionFileName != "" {
		t.Fatalf("definition = %v / %q; want empty without a DefinitionConfigMapRef", detail.DefinitionContent, detail.DefinitionFileName)
	}
}

func Test_agentRepoURL(t *testing.T) {
	ptr := func(s string) *string { return &s }
	cases := []struct {
		name string
		cfg  opendepotv1alpha1.AgentSourceConfig
		want string
	}{
		{name: "github repo url is used as is", cfg: opendepotv1alpha1.AgentSourceConfig{RepoOwner: "other", RepoUrl: ptr("https://github.com/acme/skills/")}, want: "https://github.com/acme/skills"},
		{name: "subpath url is rebuilt from owner and repo name", cfg: opendepotv1alpha1.AgentSourceConfig{RepoOwner: "acme", RepoUrl: ptr("https://github.com/acme/mono/tree/main")}, want: "https://github.com/acme/main"},
		{name: "non github url is rebuilt from owner and repo name", cfg: opendepotv1alpha1.AgentSourceConfig{RepoOwner: "acme", RepoUrl: ptr("https://gitlab.com/acme/skills.git")}, want: "https://github.com/acme/skills.git"},
		{name: "missing repo url falls back to source name", cfg: opendepotv1alpha1.AgentSourceConfig{RepoOwner: "acme", Name: ptr("reviewer")}, want: "https://github.com/acme/reviewer"},
		{name: "missing owner yields empty url", cfg: opendepotv1alpha1.AgentSourceConfig{Name: ptr("reviewer")}, want: ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := agentRepoURL(tc.cfg); got != tc.want {
				t.Fatalf("agentRepoURL() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBrowseResourcesUnfilteredIncludesAllKinds(t *testing.T) {
	publicLabels := map[string]string{labelPublic: "true"}
	listItem := func(name string) map[string]any {
		return map[string]any{"metadata": map[string]any{"name": name, "namespace": browseAgentTestNamespace, "labels": publicLabels}, "spec": map[string]any{}, "status": map[string]any{}}
	}

	lists := map[string][]map[string]any{
		"/apis/opendepot.defdev.io/v1alpha1/modules":                                              {listItem("a-module")},
		"/apis/opendepot.defdev.io/v1alpha1/providers":                                            {listItem("b-provider")},
		"/apis/opendepot.defdev.io/v1alpha1/skills":                                               {listItem("c-skill")},
		"/apis/opendepot.defdev.io/v1alpha1/agents":                                               {listItem("d-agent")},
		"/apis/opendepot.defdev.io/v1alpha1/namespaces/" + browseAgentTestNamespace + "/versions": nil,
		"/apis/opendepot.defdev.io/v1alpha1/groupbindings":                                        nil,
	}

	namespacePayload, err := json.Marshal(map[string]any{"items": []map[string]any{{"metadata": map[string]any{"name": browseAgentTestNamespace, "labels": publicLabels}}}})
	if err != nil {
		t.Fatalf("marshal namespaces: %v", err)
	}

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/namespaces" {
			_, _ = w.Write(namespacePayload)
			return
		}

		items, ok := lists[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}

		payload, err := json.Marshal(map[string]any{"items": items})
		if err != nil {
			t.Errorf("marshal list: %v", err)
		}

		_, _ = w.Write(payload)
	}))
	t.Cleanup(api.Close)
	browseAgentTestSetup(t, api, false, false)

	cases := []struct {
		query     string
		wantNames []string
		wantTotal int
	}{
		{query: "", wantNames: []string{"a-module", "b-provider", "c-skill", "d-agent"}, wantTotal: 4},
		{query: "page=2&page_size=2", wantNames: []string{"c-skill", "d-agent"}, wantTotal: 4},
		{query: "kind=skill", wantNames: []string{"c-skill"}, wantTotal: 1},
		{query: "kind=agent", wantNames: []string{"d-agent"}, wantTotal: 1},
	}

	for _, tc := range cases {
		t.Run(tc.query, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "http://registry.example.com/opendepot/ui/v1/resources?"+tc.query, nil)
			response := httptest.NewRecorder()
			browseAgentTestListRouter().ServeHTTP(response, request)

			if response.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
			}

			var list BrowseResourceList
			if err := json.Unmarshal(response.Body.Bytes(), &list); err != nil {
				t.Fatalf("unmarshal list: %v", err)
			}

			if list.TotalCount != tc.wantTotal {
				t.Fatalf("TotalCount = %d, want %d", list.TotalCount, tc.wantTotal)
			}

			var gotNames []string
			for _, item := range list.Items {
				gotNames = append(gotNames, item.Name)
			}

			if strings.Join(gotNames, ",") != strings.Join(tc.wantNames, ",") {
				t.Fatalf("names = %v, want %v", gotNames, tc.wantNames)
			}
		})
	}
}

func browseAgentTestListRouter() http.Handler {
	router := chi.NewRouter()
	router.Get("/opendepot/ui/v1/resources", handleBrowseResources)

	return router
}

func TestBrowseAgentCardsCarryLatestJevRiskLevel(t *testing.T) {
	const name = "reviewer"

	latest := browseAgentTestVersion("skill", name)
	latest.Status.JevAssessment = &opendepotv1alpha1.JevAssessment{RiskLevel: "High"}

	older := browseAgentTestVersion("skill", name)
	older.Name = name + "-0-9-0"
	older.Spec.Version = "0.9.0"
	older.Status.JevAssessment = &opendepotv1alpha1.JevAssessment{RiskLevel: "Minimal"}

	unassessedLatest := browseAgentTestVersion("agent", name)
	unassessedLatest.Status.JevAssessment = nil
	olderAssessed := browseAgentTestVersion("agent", name)
	olderAssessed.Name = name + "-0-9-0"
	olderAssessed.Spec.Version = "0.9.0"
	olderAssessed.Status.JevAssessment = &opendepotv1alpha1.JevAssessment{RiskLevel: "Minimal"}

	cases := []struct {
		name     string
		kind     string
		versions []opendepotv1alpha1.Version
		want     string
	}{
		{name: "latest assessed version risk is returned", kind: "skill", versions: []opendepotv1alpha1.Version{older, latest}, want: "High"},
		{name: "older assessed version is not used for an unassessed latest", kind: "agent", versions: []opendepotv1alpha1.Version{olderAssessed, unassessedLatest}, want: ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			card := map[string]any{
				"metadata": map[string]any{"name": name, "namespace": browseAgentTestNamespace},
				"status":   map[string]any{"synced": true, "latestVersion": "1.0.0"},
			}
			skills, err := json.Marshal(map[string]any{"items": []any{card}})
			if err != nil {
				t.Fatalf("marshal %s list: %v", tc.kind, err)
			}
			versions, err := json.Marshal(opendepotv1alpha1.VersionList{Items: tc.versions})
			if err != nil {
				t.Fatalf("marshal versions: %v", err)
			}

			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/apis/opendepot.defdev.io/v1alpha1/" + browseAgentResource(tc.kind):
					_, _ = w.Write(skills)
				case "/apis/opendepot.defdev.io/v1alpha1/versions":
					_, _ = w.Write(versions)
				default:
					http.NotFound(w, r)
				}
			}))
			t.Cleanup(api.Close)

			cs, err := kubernetes.NewForConfig(&rest.Config{Host: api.URL})
			if err != nil {
				t.Fatalf("new clientset: %v", err)
			}

			request := httptest.NewRequest(http.MethodGet, "http://registry.example.com/opendepot/ui/v1/resources", nil)
			items, err := browseCollectAgentResources(cs, request, nil, map[string]bool{browseAgentTestNamespace: true}, nil, true, false, tc.kind)
			if err != nil {
				t.Fatalf("browseCollectAgentResources: %v", err)
			}

			if len(items) != 1 {
				t.Fatalf("len(items) = %d, want 1", len(items))
			}

			if items[0].JevRiskLevel != tc.want {
				t.Fatalf("JevRiskLevel = %q, want %q", items[0].JevRiskLevel, tc.want)
			}
		})
	}
}

func TestBrowseAgentDetailShowsNewestJevAssessmentWithoutServableVersion(t *testing.T) {
	const name = "csv-summarizer"
	labels := map[string]string{labelPublic: "true"}
	older := browseAgentTestVersion("skill", name)
	older.Name = "skill-csv-summarizer-older"
	older.Spec.Version = ""
	older.Status.Synced = false
	older.Status.BranchPhase = branchPhaseRejected
	older.Status.JevAssessment = &opendepotv1alpha1.JevAssessment{
		Blocked:      true,
		RiskLevel:    "High",
		EvaluatedAt:  "2026-10-23T10:00:00Z",
		BlockReasons: []string{"older attempt"},
	}
	newer := browseAgentTestVersion("skill", name)
	newer.Name = "skill-csv-summarizer-newer"
	newer.Spec.Version = ""
	newer.Status.Synced = false
	newer.Status.BranchPhase = branchPhaseRejected
	newer.Status.JevAssessment = &opendepotv1alpha1.JevAssessment{
		Blocked:      true,
		RiskLevel:    "Critical",
		EvaluatedAt:  "2026-10-24T10:00:00Z",
		BlockReasons: []string{"part 3 is rated Critical risk, which always blocks"},
	}
	api := browseAgentTestServer(t, "skill", name, labels, browseAgentTestObject("skill", name, labels, nil), []opendepotv1alpha1.Version{older, newer}, nil)
	browseAgentTestSetup(t, api, false, false)

	request := httptest.NewRequest(http.MethodGet, "http://registry.example.com/opendepot/ui/v1/resources/"+browseAgentTestNamespace+"/skill/"+name, nil)
	response := httptest.NewRecorder()
	browseAgentTestRouter().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}

	var detail BrowseResourceDetail
	if err := json.Unmarshal(response.Body.Bytes(), &detail); err != nil {
		t.Fatalf("unmarshal detail: %v", err)
	}

	if detail.JevAssessment == nil {
		t.Fatalf("JevAssessment = nil, want the newest blocked attempt's assessment")
	}

	if detail.JevAssessment.RiskLevel != "Critical" || !detail.JevAssessment.Blocked {
		t.Fatalf("JevAssessment = %+v, want blocked Critical from the newest attempt", detail.JevAssessment)
	}
}

func TestBrowseAgentCardsCarryBlockedState(t *testing.T) {
	const name = "reviewer"
	reasons := []string{"part 1 is rated Critical risk, which always blocks; strongest signal is prompt injection (0.97)"}

	versionedBlocked := browseAgentTestVersion("skill", name)
	versionedBlocked.Status.JevAssessment = &opendepotv1alpha1.JevAssessment{RiskLevel: "Critical", Blocked: true, BlockReasons: reasons, EvaluatedAt: "2026-10-24T22:00:00Z"}

	unversionedBlocked := browseAgentTestVersion("skill", name)
	unversionedBlocked.Name = name + "-branch"
	unversionedBlocked.Spec.Version = ""
	unversionedBlocked.Status.BranchPhase = branchPhaseRejected
	unversionedBlocked.Status.JevAssessment = &opendepotv1alpha1.JevAssessment{RiskLevel: "Critical", Blocked: true, BlockReasons: reasons, EvaluatedAt: "2026-10-24T22:00:00Z"}

	assessedUnblocked := browseAgentTestVersion("agent", name)
	assessedUnblocked.Status.JevAssessment = &opendepotv1alpha1.JevAssessment{RiskLevel: "Low", EvaluatedAt: "2026-10-24T22:00:00Z"}

	cases := []struct {
		name          string
		kind          string
		latestVersion string
		versions      []opendepotv1alpha1.Version
		wantBlocked   bool
		wantReasons   []string
	}{
		{name: "blocked latest version sets blocked and reasons", kind: "skill", latestVersion: "1.0.0", versions: []opendepotv1alpha1.Version{versionedBlocked}, wantBlocked: true, wantReasons: reasons},
		{name: "blocked unversioned branch version sets blocked on an unversioned card", kind: "skill", latestVersion: "", versions: []opendepotv1alpha1.Version{unversionedBlocked}, wantBlocked: true, wantReasons: reasons},
		{name: "unblocked assessment is not blocked", kind: "agent", latestVersion: "1.0.0", versions: []opendepotv1alpha1.Version{assessedUnblocked}, wantBlocked: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			card := map[string]any{
				"metadata": map[string]any{"name": name, "namespace": browseAgentTestNamespace},
				"status":   map[string]any{"synced": true, "latestVersion": tc.latestVersion},
			}
			skills, err := json.Marshal(map[string]any{"items": []any{card}})
			if err != nil {
				t.Fatalf("marshal %s list: %v", tc.kind, err)
			}
			versions, err := json.Marshal(opendepotv1alpha1.VersionList{Items: tc.versions})
			if err != nil {
				t.Fatalf("marshal versions: %v", err)
			}

			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/apis/opendepot.defdev.io/v1alpha1/" + browseAgentResource(tc.kind):
					_, _ = w.Write(skills)
				case "/apis/opendepot.defdev.io/v1alpha1/versions":
					_, _ = w.Write(versions)
				default:
					http.NotFound(w, r)
				}
			}))
			t.Cleanup(api.Close)

			cs, err := kubernetes.NewForConfig(&rest.Config{Host: api.URL})
			if err != nil {
				t.Fatalf("new clientset: %v", err)
			}

			request := httptest.NewRequest(http.MethodGet, "http://registry.example.com/opendepot/ui/v1/resources", nil)
			items, err := browseCollectAgentResources(cs, request, nil, map[string]bool{browseAgentTestNamespace: true}, nil, true, false, tc.kind)
			if err != nil {
				t.Fatalf("browseCollectAgentResources: %v", err)
			}

			if len(items) != 1 {
				t.Fatalf("len(items) = %d, want 1", len(items))
			}

			if items[0].Blocked != tc.wantBlocked {
				t.Fatalf("Blocked = %v, want %v", items[0].Blocked, tc.wantBlocked)
			}

			if len(items[0].BlockReasons) != len(tc.wantReasons) {
				t.Fatalf("BlockReasons = %v, want %v", items[0].BlockReasons, tc.wantReasons)
			}
		})
	}
}

func TestBrowseJevExemptionGovernanceHidesPolicyNamesFromOIDCCallers(t *testing.T) {
	governance := &opendepotv1alpha1.JevExemptionGovernance{
		GoverningPolicy:   "release-exemptions",
		GoverningPriority: 10,
		MatchingPolicy:    "release-exemptions",
		MatchingPriority:  10,
		MatchingState:     opendepotv1alpha1.JevMatchingStateExempted,
	}

	originalVerifier := oidcVerifier
	defer func() {
		oidcVerifier = originalVerifier
	}()

	oidcVerifier = nil
	open := browseJevExemptionGovernance(governance)
	if open.GoverningPolicy != "release-exemptions" || open.MatchingPolicy != "release-exemptions" {
		t.Fatalf("without OIDC names = %q/%q; want them kept", open.GoverningPolicy, open.MatchingPolicy)
	}

	oidcVerifier = &gooidc.IDTokenVerifier{}
	gated := browseJevExemptionGovernance(governance)
	if gated.GoverningPolicy != "" || gated.MatchingPolicy != "" {
		t.Fatalf("with OIDC names = %q/%q; want them cleared", gated.GoverningPolicy, gated.MatchingPolicy)
	}

	if gated.GoverningPriority != 10 || gated.MatchingState != opendepotv1alpha1.JevMatchingStateExempted {
		t.Fatalf("with OIDC = %+v; want the priority and state kept", gated)
	}

	if governance.GoverningPolicy != "release-exemptions" {
		t.Fatalf("input governance changed to %+v; want it unchanged", governance)
	}

	if browseJevExemptionGovernance(nil) != nil {
		t.Fatalf("nil governance returned non-nil")
	}
}

func TestBrowseJevUnblockHidesPolicyNamesFromOIDCCallers(t *testing.T) {
	expires := metav1.NewTime(time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC))
	unblock := &opendepotv1alpha1.JevUnblock{
		State:             opendepotv1alpha1.JevUnblockStatePending,
		Digest:            "abc123",
		GoverningPolicy:   "release-exemptions",
		GoverningPriority: 10,
		MatchingPolicy:    "release-exemptions",
		MatchingPriority:  10,
		Expires:           &expires,
	}

	originalVerifier := oidcVerifier
	defer func() {
		oidcVerifier = originalVerifier
	}()

	oidcVerifier = nil
	open := browseJevUnblock(unblock)
	if open.GoverningPolicy != "release-exemptions" || open.MatchingPolicy != "release-exemptions" {
		t.Fatalf("without OIDC names = %q/%q; want them kept", open.GoverningPolicy, open.MatchingPolicy)
	}

	if open.NamesHidden {
		t.Fatalf("without OIDC NamesHidden = true; want false")
	}

	oidcVerifier = &gooidc.IDTokenVerifier{}
	gated := browseJevUnblock(unblock)
	if gated.GoverningPolicy != "" || gated.MatchingPolicy != "" {
		t.Fatalf("with OIDC names = %q/%q; want them cleared", gated.GoverningPolicy, gated.MatchingPolicy)
	}

	if !gated.NamesHidden {
		t.Fatalf("with OIDC NamesHidden = false; want true")
	}

	if gated.State != opendepotv1alpha1.JevUnblockStatePending || gated.Digest != "abc123" || gated.GoverningPriority != 10 || gated.MatchingPriority != 10 {
		t.Fatalf("with OIDC = %+v; want the state, digest, and priorities kept", gated)
	}

	if gated.Expires == nil || !gated.Expires.Equal(&expires) {
		t.Fatalf("with OIDC Expires = %v; want %v kept", gated.Expires, expires)
	}

	if unblock.GoverningPolicy != "release-exemptions" || unblock.NamesHidden {
		t.Fatalf("input unblock changed to %+v; want it unchanged", unblock)
	}

	if browseJevUnblock(nil) != nil {
		t.Fatalf("nil unblock returned non-nil")
	}
}
