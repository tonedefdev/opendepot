package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path"
	"slices"
	"strings"
	"testing"
	"time"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// agentCatalogTestFixture holds the objects served by the fake Kubernetes API for the catalog.
// failing names a resource whose list returns a server error.
type agentCatalogTestFixture struct {
	namespaces map[string]map[string]string
	skills     []map[string]any
	agents     []map[string]any
	versions   []opendepotv1alpha1.Version
	bindings   []opendepotv1alpha1.GroupBinding
	failing    string
}

// agentCatalogResource returns the metadata of a Skill or Agent as the catalog lists it.
func agentCatalogResource(namespace, name string, labels map[string]string) map[string]any {
	return map[string]any{"metadata": map[string]any{"name": name, "namespace": namespace, "labels": labels}}
}

// agentCatalogVersion returns a Version sourced from the named Skill or Agent.
func agentCatalogVersion(versionType, namespace, sourceName, version string, synced bool) opendepotv1alpha1.Version {
	source := sourceName
	return opendepotv1alpha1.Version{
		ObjectMeta: metav1.ObjectMeta{Name: sourceName + "-" + version, Namespace: namespace},
		Spec: opendepotv1alpha1.VersionSpec{
			Type:           versionType,
			Version:        version,
			AgentSourceRef: &opendepotv1alpha1.AgentSourceConfig{Name: &source},
		},
		Status: opendepotv1alpha1.VersionStatus{Synced: synced},
	}
}

// agentCatalogBaseFixture has a public Skill and Agent in the public team namespace, an internal Skill in the
// same namespace, and a Skill in a private namespace. Each has one synced Version.
func agentCatalogBaseFixture() agentCatalogTestFixture {
	public := map[string]string{labelPublic: "true"}

	return agentCatalogTestFixture{
		namespaces: map[string]map[string]string{
			"team":    {labelPublic: "true"},
			"private": {},
		},
		skills: []map[string]any{
			agentCatalogResource("team", "public-skill", public),
			agentCatalogResource("team", "internal-skill", nil),
			agentCatalogResource("private", "secret-skill", nil),
		},
		agents: []map[string]any{
			agentCatalogResource("team", "public-agent", public),
		},
		versions: []opendepotv1alpha1.Version{
			agentCatalogVersion(opendepotv1alpha1.OpenDepotSkill, "team", "public-skill", "1.0.0", true),
			agentCatalogVersion(opendepotv1alpha1.OpenDepotSkill, "team", "internal-skill", "1.0.0", true),
			agentCatalogVersion(opendepotv1alpha1.OpenDepotSkill, "private", "secret-skill", "1.0.0", true),
			agentCatalogVersion(opendepotv1alpha1.OpenDepotAgent, "team", "public-agent", "1.0.0", true),
		},
	}
}

// agentCatalogTestServer serves the namespace, Skill, Agent, Version, and GroupBinding lists for the catalog.
func agentCatalogTestServer(t *testing.T, fixture agentCatalogTestFixture) *httptest.Server {
	t.Helper()

	nsItems := make([]map[string]any, 0, len(fixture.namespaces))
	for name, labels := range fixture.namespaces {
		nsItems = append(nsItems, map[string]any{"metadata": map[string]any{"name": name, "labels": labels}})
	}

	payloads := map[string]any{
		"namespaces":    map[string]any{"items": nsItems},
		"skills":        map[string]any{"items": fixture.skills},
		"agents":        map[string]any{"items": fixture.agents},
		"versions":      opendepotv1alpha1.VersionList{Items: fixture.versions},
		"groupbindings": opendepotv1alpha1.GroupBindingList{Items: fixture.bindings},
	}

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		resource := path.Base(r.URL.Path)
		if resource == fixture.failing {
			http.Error(w, "list failed", http.StatusInternalServerError)
			return
		}

		payload, ok := payloads[resource]
		if !ok {
			http.NotFound(w, r)
			return
		}

		body, err := json.Marshal(payload)
		if err != nil {
			t.Errorf("marshal %s: %v", resource, err)
			http.Error(w, "marshal failed", http.StatusInternalServerError)
			return
		}

		_, _ = w.Write(body)
	}))
	t.Cleanup(api.Close)

	return api
}

// agentCatalogGet calls the catalog handler directly with an optional bearer token.
func agentCatalogGet(t *testing.T, target, token string) *httptest.ResponseRecorder {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, "http://registry.example.com"+target, nil)
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}

	response := httptest.NewRecorder()
	handleAgentCatalog(response, request)

	return response
}

// agentCatalogDecode decodes a successful catalog response.
func agentCatalogDecode(t *testing.T, response *httptest.ResponseRecorder) AgentCatalogResponse {
	t.Helper()

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}

	var catalog AgentCatalogResponse
	if err := json.Unmarshal(response.Body.Bytes(), &catalog); err != nil {
		t.Fatalf("unmarshal catalog: %v", err)
	}

	return catalog
}

// agentCatalogKeys returns each item as namespace/kind/name in response order.
func agentCatalogKeys(items []AgentCatalogItem) []string {
	keys := make([]string, 0, len(items))
	for _, item := range items {
		keys = append(keys, item.Namespace+"/"+item.Kind+"/"+item.Name)
	}

	return keys
}

// agentCatalogToken returns an unsigned test token. The test key set skips signature checks.
func agentCatalogToken(t *testing.T, claims map[string]any) string {
	t.Helper()

	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	claims["iss"] = agentTestIssuer
	claims["iat"] = time.Now().Unix()
	claims["exp"] = time.Now().Add(time.Hour).Unix()

	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshal claims: %v", err)
	}

	return header + "." + base64.RawURLEncoding.EncodeToString(payload) + ".c2ln"
}

// agentCatalogClientCredentials enables the client-credentials path with the test issuer.
func agentCatalogClientCredentials(t *testing.T) {
	t.Helper()

	originalAllow := opendepotOIDCAllowClientCredentials
	originalVerifier := oidcCCVerifier
	originalIssuer := opendepotOIDCIssuerURL
	t.Cleanup(func() {
		opendepotOIDCAllowClientCredentials = originalAllow
		oidcCCVerifier = originalVerifier
		opendepotOIDCIssuerURL = originalIssuer
	})

	allow := true
	issuer := agentTestIssuer
	opendepotOIDCAllowClientCredentials = &allow
	opendepotOIDCIssuerURL = &issuer
	oidcCCVerifier = gooidc.NewVerifier(agentTestIssuer, agentTestKeySet{}, &gooidc.Config{SkipClientIDCheck: true})
}

func TestAgentCatalogAnonymousAllowedWhenConfigured(t *testing.T) {
	api := agentCatalogTestServer(t, agentCatalogBaseFixture())
	browseAgentTestSetup(t, api, true, false)

	catalog := agentCatalogDecode(t, agentCatalogGet(t, "/opendepot/agents/v1/catalog", ""))

	want := []string{
		"private/skill/secret-skill",
		"team/agent/public-agent",
		"team/skill/internal-skill",
		"team/skill/public-skill",
	}
	if !slices.Equal(agentCatalogKeys(catalog.Items), want) {
		t.Fatalf("items = %v, want %v", agentCatalogKeys(catalog.Items), want)
	}

	if catalog.TotalCount != len(want) || catalog.Page != 1 || catalog.PageSize != agentCatalogDefaultPageSize {
		t.Fatalf("envelope = total %d page %d size %d", catalog.TotalCount, catalog.Page, catalog.PageSize)
	}
}

func TestAgentCatalogAnonymousDeniedWhenNotConfigured(t *testing.T) {
	api := agentCatalogTestServer(t, agentCatalogBaseFixture())
	browseAgentTestSetup(t, api, false, true)

	response := agentCatalogGet(t, "/opendepot/agents/v1/catalog", "")
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestAgentCatalogGroupBindingAccess(t *testing.T) {
	platform := opendepotv1alpha1.GroupBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "platform"},
		Spec: opendepotv1alpha1.GroupBindingSpec{
			Expression:     `"platform" in groups`,
			SkillResources: []string{"secret-*"},
		},
	}

	cases := []struct {
		name   string
		groups []string
		want   []string
	}{
		{
			name:   "matching GroupBinding sees its granted private Skill",
			groups: []string{"platform"},
			want: []string{
				"private/skill/secret-skill",
				"team/agent/public-agent",
				"team/skill/public-skill",
			},
		},
		{
			name:   "authenticated caller without a matching GroupBinding sees public resources only",
			groups: []string{"other"},
			want: []string{
				"team/agent/public-agent",
				"team/skill/public-skill",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fixture := agentCatalogBaseFixture()
			fixture.bindings = []opendepotv1alpha1.GroupBinding{platform}
			api := agentCatalogTestServer(t, fixture)
			browseAgentTestSetup(t, api, false, true)

			token := agentCatalogToken(t, map[string]any{"aud": "opendepot", "sub": "alice", "groups": tc.groups})
			catalog := agentCatalogDecode(t, agentCatalogGet(t, "/opendepot/agents/v1/catalog", token))

			if !slices.Equal(agentCatalogKeys(catalog.Items), tc.want) {
				t.Fatalf("items = %v, want %v", agentCatalogKeys(catalog.Items), tc.want)
			}
		})
	}
}

func TestAgentCatalogServiceAccountAccess(t *testing.T) {
	fixture := agentCatalogBaseFixture()
	fixture.bindings = []opendepotv1alpha1.GroupBinding{{
		ObjectMeta: metav1.ObjectMeta{Name: "ci"},
		Spec: opendepotv1alpha1.GroupBindingSpec{
			Expression:     `"client:svc-catalog" in groups`,
			SkillResources: []string{"internal-*"},
		},
	}}
	api := agentCatalogTestServer(t, fixture)
	browseAgentTestSetup(t, api, false, true)
	agentCatalogClientCredentials(t)

	token := agentCatalogToken(t, map[string]any{"aud": "other-client", "sub": "svc-catalog"})
	catalog := agentCatalogDecode(t, agentCatalogGet(t, "/opendepot/agents/v1/catalog", token))

	want := []string{
		"team/agent/public-agent",
		"team/skill/internal-skill",
		"team/skill/public-skill",
	}
	if !slices.Equal(agentCatalogKeys(catalog.Items), want) {
		t.Fatalf("items = %v, want %v", agentCatalogKeys(catalog.Items), want)
	}
}

func TestAgentCatalogNamespaceFilter(t *testing.T) {
	api := agentCatalogTestServer(t, agentCatalogBaseFixture())
	browseAgentTestSetup(t, api, true, false)

	catalog := agentCatalogDecode(t, agentCatalogGet(t, "/opendepot/agents/v1/catalog?namespace=private", ""))
	if !slices.Equal(agentCatalogKeys(catalog.Items), []string{"private/skill/secret-skill"}) || catalog.TotalCount != 1 {
		t.Fatalf("items = %v total = %d", agentCatalogKeys(catalog.Items), catalog.TotalCount)
	}

	for _, target := range []string{
		"/opendepot/agents/v1/catalog?namespace=Bad_Name",
		"/opendepot/agents/v1/catalog?namespace=",
		"/opendepot/agents/v1/catalog?namespace=a&namespace=b",
	} {
		response := agentCatalogGet(t, target, "")
		if response.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d, want %d", target, response.Code, http.StatusBadRequest)
		}
	}
}

func TestAgentCatalogPagination(t *testing.T) {
	fixture := agentCatalogBaseFixture()
	for _, name := range []string{"page-a", "page-b", "page-c", "page-d", "page-e"} {
		fixture.skills = append(fixture.skills, agentCatalogResource("team", name, map[string]string{labelPublic: "true"}))
	}

	api := agentCatalogTestServer(t, fixture)
	browseAgentTestSetup(t, api, true, false)

	want := []string{
		"private/skill/secret-skill",
		"team/agent/public-agent",
		"team/skill/internal-skill",
		"team/skill/page-a",
		"team/skill/page-b",
		"team/skill/page-c",
		"team/skill/page-d",
		"team/skill/page-e",
		"team/skill/public-skill",
	}

	var collected []string
	for page := 1; page <= 5; page++ {
		target := fmt.Sprintf("/opendepot/agents/v1/catalog?page=%d&page_size=2", page)
		catalog := agentCatalogDecode(t, agentCatalogGet(t, target, ""))
		if catalog.TotalCount != len(want) || catalog.Page != page || catalog.PageSize != 2 {
			t.Fatalf("page %d envelope = total %d page %d size %d", page, catalog.TotalCount, catalog.Page, catalog.PageSize)
		}

		collected = append(collected, agentCatalogKeys(catalog.Items)...)
	}

	if !slices.Equal(collected, want) {
		t.Fatalf("paged items = %v, want %v", collected, want)
	}

	past := agentCatalogGet(t, "/opendepot/agents/v1/catalog?page=6&page_size=2", "")
	if past.Code != http.StatusOK || !strings.Contains(past.Body.String(), `"items":[]`) {
		t.Fatalf("past last page: status = %d body = %s", past.Code, past.Body.String())
	}

	for _, target := range []string{
		"/opendepot/agents/v1/catalog?page_size=101",
		"/opendepot/agents/v1/catalog?page_size=0",
		"/opendepot/agents/v1/catalog?page=0",
		"/opendepot/agents/v1/catalog?page=x",
	} {
		response := agentCatalogGet(t, target, "")
		if response.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d, want %d", target, response.Code, http.StatusBadRequest)
		}
	}
}

func TestAgentCatalogRiskMatchesLatestInstallableVersion(t *testing.T) {
	low := &opendepotv1alpha1.JevAssessment{RiskLevel: "Low", RiskScore: new(0.2)}
	critical := &opendepotv1alpha1.JevAssessment{Blocked: true, RiskLevel: "Critical", RiskScore: new(0.95), BlockReasons: []string{"cvss above threshold"}}
	agentRisk := &opendepotv1alpha1.JevAssessment{RiskLevel: "Critical", RiskScore: new(0.99)}

	riskSkill := agentCatalogVersion(opendepotv1alpha1.OpenDepotSkill, "team", "risk-skill", "1.0.0", true)
	riskSkill.Status.JevAssessment = low
	blocked := agentCatalogVersion(opendepotv1alpha1.OpenDepotSkill, "team", "risk-skill", "2.0.0", false)
	blocked.Status.JevAssessment = critical
	sameNameAgent := agentCatalogVersion(opendepotv1alpha1.OpenDepotAgent, "team", "risk-skill", "3.0.0", true)
	sameNameAgent.Status.JevAssessment = agentRisk

	noRisk := agentCatalogVersion(opendepotv1alpha1.OpenDepotSkill, "team", "no-risk-skill", "1.0.0", true)

	public := map[string]string{labelPublic: "true"}
	fixture := agentCatalogTestFixture{
		namespaces: map[string]map[string]string{"team": {labelPublic: "true"}},
		skills: []map[string]any{
			agentCatalogResource("team", "risk-skill", public),
			agentCatalogResource("team", "no-risk-skill", public),
		},
		versions: []opendepotv1alpha1.Version{riskSkill, blocked, sameNameAgent, noRisk},
	}

	api := agentCatalogTestServer(t, fixture)
	browseAgentTestSetup(t, api, true, false)

	catalog := agentCatalogDecode(t, agentCatalogGet(t, "/opendepot/agents/v1/catalog", ""))
	if len(catalog.Items) != 2 {
		t.Fatalf("items = %v, want 2", agentCatalogKeys(catalog.Items))
	}

	risk := catalog.Items[1]
	if risk.Name != "risk-skill" || risk.LatestVersion != "1.0.0" || risk.RiskLevel != "Low" || risk.RiskScore == nil || *risk.RiskScore != 0.2 {
		t.Fatalf("risk-skill = %+v, want latest 1.0.0 with Low risk 0.2", risk)
	}

	if risk.Status != AgentAvailabilityBlocked || risk.UnavailableCandidate == nil ||
		risk.UnavailableCandidate.Version != "2.0.0" || risk.UnavailableCandidate.Status != AgentAvailabilityBlocked {
		t.Fatalf("risk-skill status = %s candidate = %+v, want blocked 2.0.0", risk.Status, risk.UnavailableCandidate)
	}

	unknown := catalog.Items[0]
	if unknown.Name != "no-risk-skill" || unknown.LatestVersion != "1.0.0" || unknown.RiskScore != nil || unknown.RiskLevel != "" || unknown.Status != AgentAvailabilityAvailable {
		t.Fatalf("no-risk-skill = %+v, want available 1.0.0 with unknown risk", unknown)
	}

	var raw struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(agentCatalogGet(t, "/opendepot/agents/v1/catalog", "").Body.Bytes(), &raw); err != nil {
		t.Fatalf("unmarshal raw catalog: %v", err)
	}

	if _, ok := raw.Items[0]["riskLevel"]; ok {
		t.Fatalf("riskLevel present for unknown risk: %v", raw.Items[0])
	}

	if _, ok := raw.Items[0]["riskScore"]; ok {
		t.Fatalf("riskScore present for unknown risk: %v", raw.Items[0])
	}
}

func TestAgentCatalogRejectedCandidateDeterminesStatus(t *testing.T) {
	installable := agentCatalogVersion(opendepotv1alpha1.OpenDepotSkill, "team", "older-rejected", "1.0.0", true)
	installable.Status.JevAssessment = &opendepotv1alpha1.JevAssessment{RiskLevel: "High", RiskScore: new(0.7)}
	rejected := agentTestBranchVersion("older-rejected", branchPhaseRejected, "Scan policy violation: secrets found")
	rejected.Namespace = "team"
	rejected.Name = "branch-older-rejected"
	rejected.CreationTimestamp = metav1.NewTime(time.Now().Add(time.Hour))

	public := map[string]string{labelPublic: "true"}
	fixture := agentCatalogTestFixture{
		namespaces: map[string]map[string]string{"team": {labelPublic: "true"}},
		skills:     []map[string]any{agentCatalogResource("team", "older-rejected", public)},
		versions:   []opendepotv1alpha1.Version{installable, rejected},
	}

	api := agentCatalogTestServer(t, fixture)
	browseAgentTestSetup(t, api, true, false)

	catalog := agentCatalogDecode(t, agentCatalogGet(t, "/opendepot/agents/v1/catalog", ""))
	if len(catalog.Items) != 1 {
		t.Fatalf("items = %v, want 1", agentCatalogKeys(catalog.Items))
	}

	item := catalog.Items[0]
	if item.Status != AgentAvailabilityRejected || item.LatestVersion != "1.0.0" || item.RiskLevel != "High" || item.RiskScore == nil || *item.RiskScore != 0.7 {
		t.Fatalf("item = %+v, want rejected status with latest 1.0.0 High 0.7", item)
	}

	candidate := item.UnavailableCandidate
	if candidate == nil || candidate.Status != AgentAvailabilityRejected || candidate.Commit != agentTestCommit || candidate.Version != "" ||
		!slices.Equal(candidate.Reasons, []string{agentReasonScanPolicy}) {
		t.Fatalf("candidate = %+v, want rejected unversioned commit", candidate)
	}
}

func TestAgentCatalogPendingIsNotRejected(t *testing.T) {
	installable := agentCatalogVersion(opendepotv1alpha1.OpenDepotSkill, "team", "pending-skill", "1.0.0", true)
	pending := agentTestBranchVersion("pending-skill", branchPhasePending, "")
	pending.Namespace = "team"
	pending.Name = "branch-pending-skill"
	pending.CreationTimestamp = metav1.NewTime(time.Now().Add(time.Hour))

	onlyPending := agentTestBranchVersion("only-pending", branchPhasePending, "")
	onlyPending.Namespace = "team"
	onlyPending.Name = "branch-only-pending"

	public := map[string]string{labelPublic: "true"}
	fixture := agentCatalogTestFixture{
		namespaces: map[string]map[string]string{"team": {labelPublic: "true"}},
		skills: []map[string]any{
			agentCatalogResource("team", "only-pending", public),
			agentCatalogResource("team", "pending-skill", public),
		},
		versions: []opendepotv1alpha1.Version{installable, pending, onlyPending},
	}

	api := agentCatalogTestServer(t, fixture)
	browseAgentTestSetup(t, api, true, false)

	catalog := agentCatalogDecode(t, agentCatalogGet(t, "/opendepot/agents/v1/catalog", ""))
	if len(catalog.Items) != 2 {
		t.Fatalf("items = %v, want 2", agentCatalogKeys(catalog.Items))
	}

	onlyItem := catalog.Items[0]
	if onlyItem.Status != AgentAvailabilityPending || onlyItem.LatestVersion != "" || onlyItem.UnavailableCandidate == nil ||
		onlyItem.UnavailableCandidate.Status != AgentAvailabilityPending {
		t.Fatalf("only-pending = %+v, want pending with no installable version", onlyItem)
	}

	item := catalog.Items[1]
	if item.Status != AgentAvailabilityAvailable || item.LatestVersion != "1.0.0" || item.UnavailableCandidate == nil ||
		item.UnavailableCandidate.Status != AgentAvailabilityPending {
		t.Fatalf("pending-skill = %+v, want available 1.0.0 with pending candidate", item)
	}
}

func TestAgentCatalogFailsWhenRequiredListFails(t *testing.T) {
	for _, failing := range []string{"versions", "skills", "namespaces"} {
		t.Run(failing, func(t *testing.T) {
			fixture := agentCatalogBaseFixture()
			fixture.failing = failing
			api := agentCatalogTestServer(t, fixture)
			browseAgentTestSetup(t, api, true, false)

			response := agentCatalogGet(t, "/opendepot/agents/v1/catalog", "")
			if response.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusInternalServerError, response.Body.String())
			}

			if strings.Contains(response.Body.String(), `"items"`) {
				t.Fatalf("partial catalog returned on failure: %s", response.Body.String())
			}
		})
	}
}
