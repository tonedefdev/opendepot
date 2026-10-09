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

package controller

import (
	"slices"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
)

func TestSanitizeAgentVersion(t *testing.T) {
	tests := map[string]string{
		"1.0.0":   "1-0-0",
		"v1.2.3":  "1-2-3",
		"1.0_rc1": "1-0-rc1",
	}

	for input, want := range tests {
		if got := sanitizeAgentVersion(input); got != want {
			t.Errorf("sanitizeAgentVersion(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestAgentVersionName(t *testing.T) {
	if got := agentVersionName(skillKind, "my-skill", "v1.0.0"); got != "skill-my-skill-1-0-0" {
		t.Errorf("skill version name = %q, want %q", got, "skill-my-skill-1-0-0")
	}

	if got := agentVersionName(agentKindDef, "my-agent", "2.1.0"); got != "agent-my-agent-2-1-0" {
		t.Errorf("agent version name = %q, want %q", got, "agent-my-agent-2-1-0")
	}
}

func TestLatestAgentVersion(t *testing.T) {
	if got := latestAgentVersion(nil); got != nil {
		t.Errorf("latestAgentVersion(nil) = %q, want nil", *got)
	}

	got := latestAgentVersion([]string{"1.10.0", "v1.9.0", "1.2.0"})
	if got == nil || *got != "v1.10.0" {
		t.Errorf("latestAgentVersion() = %v, want v1.10.0", got)
	}
}

func TestTrimAgentVersions(t *testing.T) {
	limit := func(n int) *int { return &n }

	tests := []struct {
		name     string
		versions []string
		limit    *int
		want     []string
	}{
		{name: "nil limit keeps all", versions: []string{"1.0.0", "2.0.0"}, limit: nil, want: []string{"1.0.0", "2.0.0"}},
		{name: "zero limit keeps all", versions: []string{"1.0.0", "2.0.0"}, limit: limit(0), want: []string{"1.0.0", "2.0.0"}},
		{name: "limit above count keeps all", versions: []string{"1.0.0"}, limit: limit(3), want: []string{"1.0.0"}},
		{name: "keeps newest versions", versions: []string{"1.10.0", "1.2.0", "1.9.0"}, limit: limit(2), want: []string{"1.9.0", "1.10.0"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := trimAgentVersions(tt.versions, tt.limit); !slices.Equal(got, tt.want) {
				t.Errorf("trimAgentVersions() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAgentEntryVersionName(t *testing.T) {
	const commit = "0123456789abcdef0123456789abcdef01234567"
	tag := opendepotv1alpha1.AgentVersion{Version: "1.0.0"}
	if got := agentEntryVersionName(skillKind, "my-skill", tag, false); got != "skill-my-skill-1-0-0" {
		t.Errorf("tag entry name = %q, want %q", got, "skill-my-skill-1-0-0")
	}

	branch := opendepotv1alpha1.AgentVersion{Commit: commit}
	if got := agentEntryVersionName(skillKind, "my-skill", branch, true); got != "skill-my-skill-"+commit {
		t.Errorf("branch entry name = %q, want %q", got, "skill-my-skill-"+commit)
	}

	if got := agentEntryVersionName(skillKind, "my-skill", opendepotv1alpha1.AgentVersion{}, true); got != "" {
		t.Errorf("branch entry without commit name = %q, want empty", got)
	}
}

func TestAgentVersionHistoryLimit(t *testing.T) {
	ref := "main"
	limit := 3

	if got := agentVersionHistoryLimit(opendepotv1alpha1.AgentSourceConfig{Ref: &ref}); got == nil || *got != 10 {
		t.Errorf("branch default limit = %v, want 10", got)
	}

	if got := agentVersionHistoryLimit(opendepotv1alpha1.AgentSourceConfig{}); got != nil {
		t.Errorf("tag default limit = %v, want nil", *got)
	}

	if got := agentVersionHistoryLimit(opendepotv1alpha1.AgentSourceConfig{Ref: &ref, VersionHistoryLimit: &limit}); got == nil || *got != 3 {
		t.Errorf("branch explicit limit = %v, want 3", got)
	}
}

func TestTrimAgentEntriesBranch(t *testing.T) {
	now := time.Now()
	at := func(d time.Duration) *metav1.Time {
		return &metav1.Time{Time: now.Add(d)}
	}

	entries := []opendepotv1alpha1.AgentVersion{
		{Commit: "c1", Version: "1.0.0", DiscoveredAt: at(1 * time.Hour)},
		{Commit: "c2", Version: "2.0.0", DiscoveredAt: at(2 * time.Hour)},
		{Commit: "c3", State: branchStatePending},
		{Commit: "c4", State: branchStateRejected, DiscoveredAt: at(4 * time.Hour)},
		{Commit: "c5", State: branchStateRejected, DiscoveredAt: at(0)},
		{Version: "0.1.0"},
	}

	limit := 1
	got := trimAgentEntries(entries, &limit, true)
	want := []string{"c2", "c3", "c4", ""}
	if len(got) != len(want) {
		t.Fatalf("trimAgentEntries() kept %d entries, want %d", len(got), len(want))
	}

	for i := range want {
		if got[i].Commit != want[i] {
			t.Errorf("trimAgentEntries()[%d].Commit = %q, want %q", i, got[i].Commit, want[i])
		}
	}
}

func TestSyncBranchEntries(t *testing.T) {
	entries := []opendepotv1alpha1.AgentVersion{
		{Commit: "c1", State: branchStatePending},
		{Commit: "c2", State: branchStatePending},
	}

	versions := map[string]*opendepotv1alpha1.Version{
		"skill-my-skill-c1": {Spec: opendepotv1alpha1.VersionSpec{Version: "1.0.0"}},
		"skill-my-skill-c2": {Status: opendepotv1alpha1.VersionStatus{BranchPhase: branchPhaseRejected}},
	}

	syncBranchEntries(skillKind, "my-skill", entries, versions)
	if entries[0].Version != "1.0.0" || entries[0].State != branchStateAssigned {
		t.Errorf("assigned entry = %+v, want version 1.0.0 and state %q", entries[0], branchStateAssigned)
	}

	if entries[1].Version != "" || entries[1].State != branchStateRejected {
		t.Errorf("rejected entry = %+v, want state %q", entries[1], branchStateRejected)
	}
}

func TestBranchVersionChanged(t *testing.T) {
	oldVersion := &opendepotv1alpha1.Version{Spec: opendepotv1alpha1.VersionSpec{SourceCommit: "c1"}}
	assigned := oldVersion.DeepCopy()
	assigned.Spec.Version = "1.0.0"

	if !branchVersionChanged(event.UpdateEvent{ObjectOld: oldVersion, ObjectNew: assigned}) {
		t.Error("branchVersionChanged() = false for an assigned branch Version, want true")
	}

	tagOld := &opendepotv1alpha1.Version{Spec: opendepotv1alpha1.VersionSpec{Version: "1.0.0"}}
	tagNew := tagOld.DeepCopy()
	tagNew.Spec.Yanked = true
	if branchVersionChanged(event.UpdateEvent{ObjectOld: tagOld, ObjectNew: tagNew}) {
		t.Error("branchVersionChanged() = true for a tag Version, want false")
	}

	if branchVersionChanged(event.UpdateEvent{ObjectOld: oldVersion, ObjectNew: oldVersion.DeepCopy()}) {
		t.Error("branchVersionChanged() = true without a change, want false")
	}
}
