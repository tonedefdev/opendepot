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
	"k8s.io/apimachinery/pkg/types"
)

const (
	registryTestItemsKey      = "items"
	registryTestLabelsKey     = "labels"
	registryTestMetadataKey   = "metadata"
	registryTestNameKey       = "name"
	registryTestRegion        = "us-west-2"
	registryTestVersionNumber = "1.0.0"
	registryTestModule        = "mod"
	registryTestSkills        = "skills"
	registryTestAgents        = "agents"
	registryTestVersions      = "versions"
)

// registryTestServer serves a cluster-wide view of the registry for the stats and browse list handlers. Each key in
// items is a plural resource name (modules, providers, skills, agents, versions) and its value is marshaled as the
// list response. Missing resources return an empty list.
func registryTestServer(t *testing.T, items map[string]any) *httptest.Server {
	t.Helper()

	namespacePayload, err := json.Marshal(map[string]any{registryTestItemsKey: []any{
		map[string]any{
			registryTestMetadataKey: map[string]any{
				registryTestNameKey:   browseAgentTestNamespace,
				registryTestLabelsKey: map[string]string{},
			},
		},
	}})
	if err != nil {
		t.Fatalf("marshal namespaces: %v", err)
	}

	payloads := make(map[string][]byte, len(items))
	for resource, list := range items {
		payload, err := json.Marshal(list)
		if err != nil {
			t.Fatalf("marshal %s: %v", resource, err)
		}

		payloads[resource] = payload
	}

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/namespaces" {
			_, _ = w.Write(namespacePayload)
			return
		}

		const prefix = "/apis/opendepot.defdev.io/v1alpha1/"
		if len(r.URL.Path) <= len(prefix) || r.URL.Path[:len(prefix)] != prefix {
			http.NotFound(w, r)
			return
		}

		resource := r.URL.Path[len(prefix):]
		payload, ok := payloads[resource]
		if !ok {
			_, _ = w.Write([]byte(`{"items":[]}`))
			return
		}

		_, _ = w.Write(payload)
	}))
	t.Cleanup(api.Close)

	return api
}

// registryTestOwned returns a Version owned by the given kind and name in the test namespace.
func registryTestOwned(
	name, ownerKind, ownerName string, created time.Time, archiveBytes *int64, synced bool,
) opendepotv1alpha1.Version {
	return opendepotv1alpha1.Version{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			Namespace:         browseAgentTestNamespace,
			CreationTimestamp: metav1.NewTime(created),
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "opendepot.defdev.io/v1alpha1",
				Kind:       ownerKind,
				Name:       ownerName,
				UID:        types.UID("uid-" + ownerName),
			}},
		},
		Spec: opendepotv1alpha1.VersionSpec{Version: registryTestVersionNumber},
		Status: opendepotv1alpha1.VersionStatus{
			Synced:           synced,
			ArchiveSizeBytes: archiveBytes,
		},
	}
}

func int64Ptr(v int64) *int64 {
	return &v
}

func TestBrowseStatsCountsSkillsAndAgents(t *testing.T) {
	s3Storage := &opendepotv1alpha1.StorageConfig{
		S3: &opendepotv1alpha1.AmazonS3Config{Bucket: "b", Region: registryTestRegion},
	}
	gcsStorage := &opendepotv1alpha1.StorageConfig{GCS: &opendepotv1alpha1.GoogleCloudStorageConfig{Bucket: "g"}}
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	module := opendepotv1alpha1.Module{
		ObjectMeta: metav1.ObjectMeta{Name: registryTestModule, Namespace: browseAgentTestNamespace},
		Spec: opendepotv1alpha1.ModuleSpec{
			ModuleConfig: opendepotv1alpha1.ModuleConfig{StorageConfig: s3Storage},
		},
	}
	skill := opendepotv1alpha1.Skill{
		ObjectMeta: metav1.ObjectMeta{Name: "sk", Namespace: browseAgentTestNamespace},
		Spec: opendepotv1alpha1.SkillSpec{
			AgentSourceConfig: opendepotv1alpha1.AgentSourceConfig{StorageConfig: s3Storage},
		},
		Status: opendepotv1alpha1.SkillStatus{Synced: true},
	}
	unconfiguredSkill := opendepotv1alpha1.Skill{
		ObjectMeta: metav1.ObjectMeta{Name: "sk-nostore", Namespace: browseAgentTestNamespace},
		Status:     opendepotv1alpha1.SkillStatus{Synced: false},
	}
	agent := opendepotv1alpha1.Agent{
		ObjectMeta: metav1.ObjectMeta{Name: "ag", Namespace: browseAgentTestNamespace},
		Spec: opendepotv1alpha1.AgentSpec{
			AgentSourceConfig: opendepotv1alpha1.AgentSourceConfig{StorageConfig: gcsStorage},
		},
		Status: opendepotv1alpha1.AgentStatus{Synced: true},
	}

	versions := []opendepotv1alpha1.Version{
		registryTestOwned("mod-1-0-0", "Module", registryTestModule, created, int64Ptr(10), true),
		registryTestOwned("sk-1-0-0", "Skill", "sk", created, int64Ptr(100), true),
		registryTestOwned("sk-nostore-1-0-0", "Skill", "sk-nostore", created, int64Ptr(50), false),
		registryTestOwned("ag-1-0-0", "Agent", "ag", created, int64Ptr(200), true),
		// Owned by a Skill that does not exist, so it must not be counted.
		registryTestOwned("ghost-1-0-0", "Skill", "ghost", created, int64Ptr(1000), true),
	}

	api := registryTestServer(t, map[string]any{
		"modules":            opendepotv1alpha1.ModuleList{Items: []opendepotv1alpha1.Module{module}},
		registryTestSkills:   opendepotv1alpha1.SkillList{Items: []opendepotv1alpha1.Skill{skill, unconfiguredSkill}},
		registryTestAgents:   opendepotv1alpha1.AgentList{Items: []opendepotv1alpha1.Agent{agent}},
		registryTestVersions: opendepotv1alpha1.VersionList{Items: versions},
	})
	browseAgentTestSetup(t, api, true, false)

	req := httptest.NewRequest(http.MethodGet, "/opendepot/ui/v1/stats", nil)
	rec := httptest.NewRecorder()
	handleBrowseStats(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var stats BrowseStats
	if err := json.Unmarshal(rec.Body.Bytes(), &stats); err != nil {
		t.Fatalf("unmarshal stats: %v", err)
	}

	if stats.TotalSkills != 2 || stats.SkillsSynced != 1 {
		t.Errorf("skills = %d (synced %d), want 2 (synced 1)", stats.TotalSkills, stats.SkillsSynced)
	}

	if stats.TotalAgents != 1 || stats.AgentsSynced != 1 {
		t.Errorf("agents = %d (synced %d), want 1 (synced 1)", stats.TotalAgents, stats.AgentsSynced)
	}

	// The orphaned Version is excluded, leaving the module, two skill and one agent Versions.
	if stats.TotalVersions != 4 {
		t.Errorf("TotalVersions = %d, want 4", stats.TotalVersions)
	}

	if stats.TotalStorageBytes != 360 {
		t.Errorf("TotalStorageBytes = %d, want 360", stats.TotalStorageBytes)
	}

	if stats.VersionsSynced != 3 {
		t.Errorf("VersionsSynced = %d, want 3", stats.VersionsSynced)
	}

	if stats.SyncHealth.SyncedVersions != 3 || stats.SyncHealth.UnsyncedVersions != 1 {
		t.Errorf("SyncHealth = %+v, want synced 3 and unsynced 1", stats.SyncHealth)
	}

	gotDist := make(map[string]int, len(stats.StorageDistribution))
	for _, d := range stats.StorageDistribution {
		gotDist[d.Backend] = d.Count
	}

	wantDist := map[string]int{"s3": 2, "gcs": 1, "unknown": 1}
	if len(gotDist) != len(wantDist) {
		t.Fatalf("StorageDistribution = %v, want %v", gotDist, wantDist)
	}

	for backend, want := range wantDist {
		if gotDist[backend] != want {
			t.Errorf("StorageDistribution[%q] = %d, want %d", backend, gotDist[backend], want)
		}
	}
}
