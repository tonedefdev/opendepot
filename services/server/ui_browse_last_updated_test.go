/*
Copyright 2026 Tony Owens.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// browseLastUpdatedRows lists browse resources of the given kind and returns each row's lastUpdated keyed by name.
// A row whose lastUpdated key is absent from the JSON is recorded as an empty string.
func browseLastUpdatedRows(t *testing.T, api *httptest.Server, kind string) map[string]string {
	t.Helper()

	browseAgentTestSetup(t, api, true, false)

	req := httptest.NewRequest(http.MethodGet, "/opendepot/ui/v1/resources?kind="+kind, nil)
	rec := httptest.NewRecorder()
	handleBrowseResources(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var raw struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("unmarshal list: %v", err)
	}

	rows := make(map[string]string, len(raw.Items))
	for _, item := range raw.Items {
		name, _ := item["name"].(string)
		value, present := item["lastUpdated"]
		if !present {
			rows[name] = ""
			continue
		}

		rows[name], _ = value.(string)
	}

	return rows
}

func TestBrowseResourcesLastUpdated(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	created := metav1.NewTime(base)
	older := base.Add(24 * time.Hour)
	newest := base.Add(72 * time.Hour)

	skillWithVersions := opendepotv1alpha1.Skill{
		ObjectMeta: metav1.ObjectMeta{Name: "sk", Namespace: browseAgentTestNamespace, CreationTimestamp: created},
	}
	skillNoVersions := opendepotv1alpha1.Skill{
		ObjectMeta: metav1.ObjectMeta{Name: "fresh", Namespace: browseAgentTestNamespace, CreationTimestamp: created},
	}
	skillUnknown := opendepotv1alpha1.Skill{
		ObjectMeta: metav1.ObjectMeta{Name: "unknown", Namespace: browseAgentTestNamespace},
	}
	agentWithVersion := opendepotv1alpha1.Agent{
		ObjectMeta: metav1.ObjectMeta{Name: "ag", Namespace: browseAgentTestNamespace, CreationTimestamp: created},
	}

	t.Run("latest version creation time wins", func(t *testing.T) {
		versions := []opendepotv1alpha1.Version{
			registryTestOwned("sk-1-0-0", "Skill", "sk", base.Add(time.Hour), nil, true),
			registryTestOwned("sk-1-1-0", "Skill", "sk", newest, nil, true),
			registryTestOwned("sk-0-9-0", "Skill", "sk", older, nil, true),
		}

		api := registryTestServer(t, map[string]any{
			registryTestSkills:   opendepotv1alpha1.SkillList{Items: []opendepotv1alpha1.Skill{skillWithVersions}},
			registryTestVersions: opendepotv1alpha1.VersionList{Items: versions},
		})

		rows := browseLastUpdatedRows(t, api, "skill")
		if got, want := rows["sk"], newest.Format(time.RFC3339); got != want {
			t.Errorf("lastUpdated = %q, want %q", got, want)
		}
	})

	t.Run("falls back to resource creation time without versions", func(t *testing.T) {
		api := registryTestServer(t, map[string]any{
			registryTestSkills: opendepotv1alpha1.SkillList{Items: []opendepotv1alpha1.Skill{skillNoVersions}},
		})

		rows := browseLastUpdatedRows(t, api, "skill")
		if got, want := rows["fresh"], base.Format(time.RFC3339); got != want {
			t.Errorf("lastUpdated = %q, want %q", got, want)
		}
	})

	t.Run("ignores versions owned by another resource", func(t *testing.T) {
		versions := []opendepotv1alpha1.Version{
			registryTestOwned("other-9-9-9", "Skill", "other", newest, nil, true),
			registryTestOwned("ag-1-0-0", "Agent", "sk", newest, nil, true),
		}

		api := registryTestServer(t, map[string]any{
			registryTestSkills:   opendepotv1alpha1.SkillList{Items: []opendepotv1alpha1.Skill{skillWithVersions}},
			registryTestVersions: opendepotv1alpha1.VersionList{Items: versions},
		})

		rows := browseLastUpdatedRows(t, api, "skill")
		if got, want := rows["sk"], base.Format(time.RFC3339); got != want {
			t.Errorf("lastUpdated = %q, want %q", got, want)
		}
	})

	t.Run("agent uses its latest version", func(t *testing.T) {
		versions := []opendepotv1alpha1.Version{
			registryTestOwned("ag-1-0-0", "Agent", "ag", older, nil, true),
		}

		api := registryTestServer(t, map[string]any{
			registryTestAgents:   opendepotv1alpha1.AgentList{Items: []opendepotv1alpha1.Agent{agentWithVersion}},
			registryTestVersions: opendepotv1alpha1.VersionList{Items: versions},
		})

		rows := browseLastUpdatedRows(t, api, "agent")
		if got, want := rows["ag"], older.Format(time.RFC3339); got != want {
			t.Errorf("lastUpdated = %q, want %q", got, want)
		}
	})

	t.Run("omitted when nothing is known", func(t *testing.T) {
		api := registryTestServer(t, map[string]any{
			registryTestSkills: opendepotv1alpha1.SkillList{Items: []opendepotv1alpha1.Skill{skillUnknown}},
		})

		browseAgentTestSetup(t, api, true, false)
		req := httptest.NewRequest(http.MethodGet, "/opendepot/ui/v1/resources?kind=skill", nil)
		rec := httptest.NewRecorder()
		handleBrowseResources(rec, req)

		var raw struct {
			Items []map[string]any `json:"items"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
			t.Fatalf("unmarshal list: %v", err)
		}

		if len(raw.Items) != 1 {
			t.Fatalf("items = %d, want 1", len(raw.Items))
		}

		if _, present := raw.Items[0]["lastUpdated"]; present {
			t.Errorf("lastUpdated present = %v, want omitted", raw.Items[0]["lastUpdated"])
		}
	})
}
