package main

import (
	"errors"
	"strings"
	"testing"
)

func TestResolveVersionOperators(t *testing.T) {
	versions := []agentVersion{{Version: "1.0.0"}, {Version: "1.5.0"}, {Version: "2.0.0"}}
	tests := []struct {
		constraint string
		want       string
	}{
		{"= 1.5.0", "1.5.0"},
		{"!= 2.0.0", "1.5.0"},
		{"> 1.0.0", "2.0.0"},
		{"< 2.0.0", "1.5.0"},
		{">= 1.5.0", "2.0.0"},
		{"<= 1.5.0", "1.5.0"},
		{"~> 1.0", "1.5.0"},
		{"~> 1.0.0", "1.0.0"},
	}

	for _, tt := range tests {
		t.Run(tt.constraint, func(t *testing.T) {
			got, err := resolveVersion(versions, tt.constraint, false)
			if err != nil {
				t.Fatalf("resolveVersion(%q): %v", tt.constraint, err)
			}

			if got != tt.want {
				t.Fatalf("resolveVersion(%q) = %s, want %s", tt.constraint, got, tt.want)
			}
		})
	}
}

func TestResolveVersionSkipsYanked(t *testing.T) {
	versions := []agentVersion{{Version: "1.0.0"}, {Version: "1.1.0", Yanked: true}}

	got, err := resolveVersion(versions, ">= 1.0.0", false)
	if err != nil || got != "1.0.0" {
		t.Fatalf("resolveVersion without allowYanked = %q, %v; want 1.0.0", got, err)
	}

	got, err = resolveVersion(versions, ">= 1.0.0", true)
	if err != nil || got != "1.1.0" {
		t.Fatalf("resolveVersion with allowYanked = %q, %v; want 1.1.0", got, err)
	}
}

func TestResolveVersionSkipsBlockedVersions(t *testing.T) {
	versions := []agentVersion{
		{Version: "1.0.0"},
		{Version: "1.1.0", Blocked: true, BlockReasons: []string{"risk score 0.8 above 0.5"}},
	}

	got, err := resolveVersion(versions, ">= 1.0.0", false)
	if err != nil || got != "1.0.0" {
		t.Fatalf("resolveVersion() = %q, %v; want 1.0.0 with the blocked 1.1.0 skipped", got, err)
	}
}

func TestResolveVersionReportsBlockedMatch(t *testing.T) {
	versions := []agentVersion{{Version: "1.0.0", Blocked: true, BlockReasons: []string{"risk score 0.8 above 0.5"}}}

	_, err := resolveVersion(versions, "1.0.0", false)
	var blockedErr *blockedVersionError
	if !errors.As(err, &blockedErr) {
		t.Fatalf("resolveVersion() error = %v, want blockedVersionError", err)
	}

	want := "version 1.0.0 is blocked by a Jev policy\n- risk score 0.8 above 0.5"
	if err.Error() != want {
		t.Fatalf("blocked error = %q, want %q", err.Error(), want)
	}
}

func TestResolveVersionNoMatchListsAvailableVersions(t *testing.T) {
	versions := []agentVersion{
		{Version: "0.9.0"},
		{Version: "1.1.0", Yanked: true},
		{Version: "1.2.0", Blocked: true, BlockReasons: []string{"risk score 0.8 above 0.5"}},
	}

	_, err := resolveVersion(versions, "2.0.0", false)
	want := "no version satisfies \"2.0.0\"\navailable versions: 0.9.0, 1.1.0 (yanked), 1.2.0 (blocked)"
	if err == nil || err.Error() != want {
		t.Fatalf("resolveVersion() error = %v, want %q", err, want)
	}

	_, err = resolveVersion(nil, "1.0.0", false)
	if err == nil || err.Error() != "no version satisfies \"1.0.0\"; no versions are published" {
		t.Fatalf("resolveVersion(nil) error = %v", err)
	}
}

func TestSourceErrorIndentsDetail(t *testing.T) {
	err := &sourceError{source: "registry.example/ns/skill", err: &blockedVersionError{version: "1.0.0", reasons: []string{"risk"}}}

	want := "registry.example/ns/skill\n  version 1.0.0 is blocked by a Jev policy\n  - risk"
	if err.Error() != want {
		t.Fatalf("sourceError = %q, want %q", err.Error(), want)
	}
}

func TestSelectVersionRefusesNewerBlockedFallback(t *testing.T) {
	versions := []agentVersion{{Version: "1.0.0"}, {Version: "2.0.0", Blocked: true, BlockReasons: []string{"cvss above threshold"}}}

	_, err := selectVersion(versions, nil, ">= 1.0.0", false, false)
	var refused *fallbackRefusedError
	if !errors.As(err, &refused) {
		t.Fatalf("expected fallbackRefusedError, got %v", err)
	}

	if refused.chosen != "1.0.0" || len(refused.newer) != 1 || refused.newer[0].Version != "2.0.0" {
		t.Fatalf("unexpected refusal: %+v", refused)
	}

	if !strings.Contains(err.Error(), "version 2.0.0 is blocked by a Jev policy\n- cvss above threshold") || !strings.Contains(err.Error(), "--allow-older-versions") {
		t.Fatalf("refusal text: %q", err.Error())
	}
}

func TestSelectVersionAllowOlderSkipsNewerCandidates(t *testing.T) {
	versions := []agentVersion{{Version: "1.0.0"}, {Version: "2.0.0", Blocked: true, BlockReasons: []string{"policy"}}}
	unavailable := []unavailableCandidate{{Version: "3.0.0", Status: statusRejected, Reasons: []string{"Superseded"}}}

	choice, err := selectVersion(versions, unavailable, ">= 1.0.0", false, true)
	if err != nil {
		t.Fatalf("selectVersion: %v", err)
	}

	if choice.version != "1.0.0" || len(choice.skipped) != 2 {
		t.Fatalf("unexpected choice: %+v", choice)
	}

	if choice.skipped[0].Version != "3.0.0" || choice.skipped[1].Version != "2.0.0" {
		t.Fatalf("skipped not sorted newest first: %+v", choice.skipped)
	}
}

func TestSelectVersionPendingDoesNotRefuse(t *testing.T) {
	versions := []agentVersion{{Version: "1.0.0"}}
	unavailable := []unavailableCandidate{{Version: "2.0.0", Status: statusPending}}

	choice, err := selectVersion(versions, unavailable, ">= 1.0.0", false, false)
	if err != nil {
		t.Fatalf("pending candidate refused: %v", err)
	}

	if choice.version != "1.0.0" || len(choice.skipped) != 0 {
		t.Fatalf("unexpected choice: %+v", choice)
	}
}

func TestSelectVersionUnversionedCommitNeverMatchesConstraint(t *testing.T) {
	versions := []agentVersion{{Version: "1.0.0"}}
	unavailable := []unavailableCandidate{{Commit: "3f2a9c1d0b7e4a5f6c8d9e0a1b2c3d4e5f607182", Status: statusRejected, Reasons: []string{"Superseded by a newer commit"}}}

	choice, err := selectVersion(versions, unavailable, ">= 1.0.0", false, false)
	if err != nil {
		t.Fatalf("unversioned commit refused a matching version: %v", err)
	}

	if choice.version != "1.0.0" || len(choice.skipped) != 0 {
		t.Fatalf("unexpected choice: %+v", choice)
	}

	_, err = selectVersion(nil, unavailable, ">= 1.0.0", false, false)
	var noVersion *noVersionError
	if !errors.As(err, &noVersion) {
		t.Fatalf("expected noVersionError, got %v", err)
	}

	if !strings.Contains(err.Error(), "commit 3f2a9c1d0b7e4a5f6c8d9e0a1b2c3d4e5f607182 is rejected; no version was assigned, so it is not compared with the constraint") {
		t.Fatalf("unversioned text: %q", err.Error())
	}

	if strings.Contains(err.Error(), "available versions") {
		t.Fatalf("unversioned commit listed as a version: %q", err.Error())
	}
}

func TestSelectVersionIgnoresCandidatesOutsideConstraint(t *testing.T) {
	versions := []agentVersion{{Version: "1.0.0"}}
	unavailable := []unavailableCandidate{{Version: "3.0.0", Status: statusBlocked}}

	choice, err := selectVersion(versions, unavailable, "< 2.0.0", false, false)
	if err != nil {
		t.Fatalf("out-of-constraint candidate refused: %v", err)
	}

	if choice.version != "1.0.0" {
		t.Fatalf("unexpected version %q", choice.version)
	}
}

func TestSelectVersionBlockedOnlyReturnsBlockedError(t *testing.T) {
	unavailable := []unavailableCandidate{{Version: "1.0.0", Status: statusBlocked, Reasons: []string{"policy"}}}

	_, err := selectVersion(nil, unavailable, ">= 1.0.0", false, true)
	var blocked *blockedVersionError
	if !errors.As(err, &blocked) || blocked.version != "1.0.0" {
		t.Fatalf("expected blockedVersionError for 1.0.0, got %v", err)
	}
}

func TestSelectVersionNoBestListsRejectedNewer(t *testing.T) {
	unavailable := []unavailableCandidate{{Version: "2.0.0", Status: statusAssessmentFailed}}

	_, err := selectVersion(nil, unavailable, ">= 1.0.0", false, true)
	if err == nil || !strings.Contains(err.Error(), "2.0.0 (assessment failed)") || strings.Contains(err.Error(), "blocked by a Jev policy") {
		t.Fatalf("assessment failure text: %v", err)
	}
}

func TestSelectVersionDedupesListedVersions(t *testing.T) {
	versions := []agentVersion{{Version: "1.0.0"}, {Version: "2.0.0", Blocked: true, BlockReasons: []string{"policy"}}}
	unavailable := []unavailableCandidate{{Version: "2.0.0", Status: statusBlocked, Reasons: []string{"policy"}}}

	_, err := selectVersion(versions, unavailable, ">= 1.0.0", false, false)
	var refused *fallbackRefusedError
	if !errors.As(err, &refused) || len(refused.newer) != 1 {
		t.Fatalf("expected one refused candidate, got %v", err)
	}
}

func TestResolveVersionKeepsSilentFallback(t *testing.T) {
	versions := []agentVersion{{Version: "1.0.0"}, {Version: "2.0.0", Blocked: true}}

	got, err := resolveVersion(versions, ">= 1.0.0", false)
	if err != nil || got != "1.0.0" {
		t.Fatalf("resolveVersion = %q, %v", got, err)
	}
}
