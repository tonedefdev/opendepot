package main

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestAgentCatalogDescriptionFromLatestInstallableVersion(t *testing.T) {
	older := agentCatalogVersion(opendepotv1alpha1.OpenDepotSkill, "team", "lint", "1.0.0", true)
	older.Status.AgentMetadata = &opendepotv1alpha1.AgentMetadata{Description: "Old description"}
	latest := agentCatalogVersion(opendepotv1alpha1.OpenDepotSkill, "team", "lint", "2.0.0", true)
	latest.Status.AgentMetadata = &opendepotv1alpha1.AgentMetadata{Description: "Lints code"}
	candidate := agentCatalogVersion(opendepotv1alpha1.OpenDepotSkill, "team", "lint", "3.0.0", false)
	candidate.Status.JevAssessment = &opendepotv1alpha1.JevAssessment{Blocked: true, RiskLevel: "Critical", RiskScore: new(0.95), BlockReasons: []string{"cvss above threshold"}}
	candidate.Status.AgentMetadata = &opendepotv1alpha1.AgentMetadata{Description: "Candidate description"}

	noMetadata := agentCatalogVersion(opendepotv1alpha1.OpenDepotSkill, "team", "no-metadata", "1.0.0", true)

	unsynced := agentCatalogVersion(opendepotv1alpha1.OpenDepotSkill, "team", "unsynced", "1.0.0", false)
	unsynced.Status.AgentMetadata = &opendepotv1alpha1.AgentMetadata{Description: "Pending description"}

	secret := agentCatalogVersion(opendepotv1alpha1.OpenDepotSkill, "private", "secret", "1.0.0", true)
	secret.Status.AgentMetadata = &opendepotv1alpha1.AgentMetadata{Description: "Secret description"}

	public := map[string]string{labelPublic: "true"}
	platform := opendepotv1alpha1.GroupBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "platform"},
		Spec: opendepotv1alpha1.GroupBindingSpec{
			Expression:     `"platform" in groups`,
			SkillResources: []string{"secret"},
		},
	}
	fixture := agentCatalogTestFixture{
		namespaces: map[string]map[string]string{"team": {labelPublic: "true"}, "private": {}},
		skills: []map[string]any{
			agentCatalogResource("team", "lint", public),
			agentCatalogResource("team", "no-metadata", public),
			agentCatalogResource("team", "unsynced", public),
			agentCatalogResource("private", "secret", nil),
		},
		versions: []opendepotv1alpha1.Version{older, latest, candidate, noMetadata, unsynced, secret},
		bindings: []opendepotv1alpha1.GroupBinding{platform},
	}

	api := agentCatalogTestServer(t, fixture)
	browseAgentTestSetup(t, api, false, false)

	anonymous := agentCatalogGet(t, "/opendepot/agents/v1/catalog", "")
	catalog := agentCatalogDecode(t, anonymous)

	want := []string{"team/skill/lint", "team/skill/no-metadata", "team/skill/unsynced"}
	if !slices.Equal(agentCatalogKeys(catalog.Items), want) {
		t.Fatalf("items = %v, want %v", agentCatalogKeys(catalog.Items), want)
	}

	descriptions := map[string]string{}
	for _, item := range catalog.Items {
		descriptions[item.Name] = item.Description
	}

	if descriptions["lint"] != "Lints code" || descriptions["no-metadata"] != "" || descriptions["unsynced"] != "" {
		t.Fatalf("descriptions = %v, want lint from latest installable Version only", descriptions)
	}

	for _, hidden := range []string{"Old description", "Candidate description", "Pending description", "Secret description"} {
		if strings.Contains(anonymous.Body.String(), hidden) {
			t.Fatalf("anonymous catalog leaked %q: %s", hidden, anonymous.Body.String())
		}
	}

	var raw struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(anonymous.Body.Bytes(), &raw); err != nil {
		t.Fatalf("unmarshal raw catalog: %v", err)
	}

	for _, item := range raw.Items {
		if item["name"] == "no-metadata" {
			if _, ok := item["description"]; ok {
				t.Fatalf("description present without metadata: %v", item)
			}
		}
	}

	browseAgentTestSetup(t, api, false, true)

	token := agentCatalogToken(t, map[string]any{"aud": "opendepot", "sub": "alice", "groups": []string{"platform"}})
	authorized := agentCatalogDecode(t, agentCatalogGet(t, "/opendepot/agents/v1/catalog?namespace=private", token))
	if len(authorized.Items) != 1 || authorized.Items[0].Description != "Secret description" {
		t.Fatalf("granted private Skill = %+v, want its description", authorized.Items)
	}
}

func TestAgentDescriptionForReturnsLatestVersionMetadataOnly(t *testing.T) {
	withMetadata := opendepotv1alpha1.Version{Spec: opendepotv1alpha1.VersionSpec{Version: "2.0.0"}}
	withMetadata.Status.AgentMetadata = &opendepotv1alpha1.AgentMetadata{Description: "Lints code"}
	withoutMetadata := opendepotv1alpha1.Version{Spec: opendepotv1alpha1.VersionSpec{Version: "1.0.0"}}
	versions := []*opendepotv1alpha1.Version{&withoutMetadata, &withMetadata}

	cases := []struct {
		name     string
		latest   string
		versions []*opendepotv1alpha1.Version
		want     string
	}{
		{name: "no latest installable Version", latest: "", versions: versions, want: ""},
		{name: "latest matches no Version", latest: "3.0.0", versions: versions, want: ""},
		{name: "latest has no metadata", latest: "1.0.0", versions: versions, want: ""},
		{name: "latest has metadata", latest: "2.0.0", versions: versions, want: "Lints code"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := agentDescriptionFor(tc.versions, tc.latest); got != tc.want {
				t.Fatalf("agentDescriptionFor(%q) = %q, want %q", tc.latest, got, tc.want)
			}
		})
	}
}

func TestAgentCatalogDescriptionEmptyForBlockedOnlyItem(t *testing.T) {
	blocked := agentCatalogVersion(opendepotv1alpha1.OpenDepotSkill, "team", "blocked-only", "1.0.0", false)
	blocked.Status.JevAssessment = &opendepotv1alpha1.JevAssessment{Blocked: true, RiskLevel: "Critical", RiskScore: new(0.95), BlockReasons: []string{"cvss above threshold"}}
	blocked.Status.AgentMetadata = &opendepotv1alpha1.AgentMetadata{Description: "Blocked description"}

	fixture := agentCatalogTestFixture{
		namespaces: map[string]map[string]string{"team": {labelPublic: "true"}},
		skills:     []map[string]any{agentCatalogResource("team", "blocked-only", map[string]string{labelPublic: "true"})},
		versions:   []opendepotv1alpha1.Version{blocked},
	}

	api := agentCatalogTestServer(t, fixture)
	browseAgentTestSetup(t, api, false, false)

	catalog := agentCatalogDecode(t, agentCatalogGet(t, "/opendepot/agents/v1/catalog", ""))
	if len(catalog.Items) != 1 {
		t.Fatalf("items = %v, want one blocked item", agentCatalogKeys(catalog.Items))
	}

	item := catalog.Items[0]
	if item.Status != AgentAvailabilityBlocked || item.LatestVersion != "" || item.Description != "" {
		t.Fatalf("item = %+v, want blocked with no latest version and no description", item)
	}
}
