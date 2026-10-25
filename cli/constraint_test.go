package main

import (
	"errors"
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
