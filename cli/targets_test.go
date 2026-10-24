package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstallTargetsDefaultToUserHomeFolders(t *testing.T) {
	isolateHome(t)
	home := os.Getenv("HOME")
	p := &project{targets: []string{targetCopilot, targetClaude}}

	cases := []struct {
		name string
		e    entry
		want []string
	}{
		{
			name: "skill",
			e:    entry{kind: kindSkill, name: "lint"},
			want: []string{
				filepath.Join(home, ".copilot", "skills", "lint"),
				filepath.Join(home, ".claude", "skills", "lint"),
			},
		},
		{
			name: "agent",
			e:    entry{kind: kindAgent, name: "reviewer"},
			want: []string{
				filepath.Join(home, ".copilot", "agents", "reviewer.agent.md"),
				filepath.Join(home, ".claude", "agents", "reviewer.md"),
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := p.installTargets(tc.e)
			if err != nil {
				t.Fatalf("installTargets() error = %v", err)
			}

			if len(got) != len(tc.want) {
				t.Fatalf("installTargets() returned %d targets, want %d", len(got), len(tc.want))
			}

			for i, target := range got {
				if target.path != tc.want[i] {
					t.Fatalf("target %s path = %q, want %q", target.label, target.path, tc.want[i])
				}

				if !safeInstallPath(target.path) {
					t.Fatalf("default target %q is rejected by safeInstallPath", target.path)
				}
			}
		})
	}
}

func TestInstallTargetsCodexUsesCodexLayouts(t *testing.T) {
	isolateHome(t)
	home := os.Getenv("HOME")
	p := &project{targets: []string{targetCodex}}

	skill, err := p.installTargets(entry{kind: kindSkill, name: "lint"})
	if err != nil {
		t.Fatalf("installTargets() skill error = %v", err)
	}

	if want := filepath.Join(home, ".agents", "skills", "lint"); len(skill) != 1 || skill[0].path != want {
		t.Fatalf("codex skill target = %+v, want %q", skill, want)
	}

	agent, err := p.installTargets(entry{kind: kindAgent, name: "reviewer"})
	if err != nil {
		t.Fatalf("installTargets() agent error = %v", err)
	}

	if want := filepath.Join(home, ".codex", "agents", "reviewer.toml"); len(agent) != 1 || agent[0].path != want {
		t.Fatalf("codex agent target = %+v, want %q", agent, want)
	}

	for _, target := range append(skill, agent...) {
		if !safeInstallPath(target.path) {
			t.Fatalf("codex target %q is rejected by safeInstallPath", target.path)
		}
	}
}

func TestAgentTargetAcceptsByFormat(t *testing.T) {
	cases := []struct {
		label string
		toml  bool
		want  bool
	}{
		{targetCodex, true, true},
		{targetCodex, false, false},
		{targetCopilot, true, false},
		{targetClaude, true, false},
		{targetAgentsMD, true, false},
		{targetCopilot, false, true},
		{targetClaude, false, true},
		{targetAgentsMD, false, true},
		{targetCustom, true, true},
		{targetCustom, false, true},
	}

	for _, tc := range cases {
		if got := agentTargetAccepts(tc.label, tc.toml); got != tc.want {
			t.Fatalf("agentTargetAccepts(%q, toml=%v) = %v, want %v", tc.label, tc.toml, got, tc.want)
		}
	}
}

func TestInstallTargetsCustomPathStaysProjectRelative(t *testing.T) {
	p := &project{targets: []string{targetCopilot}}
	got, err := p.installTargets(entry{kind: kindSkill, name: "lint", path: ".github/skills/release-notes"})
	if err != nil {
		t.Fatalf("installTargets() error = %v", err)
	}

	if len(got) != 1 || got[0].label != targetCustom || got[0].path != ".github/skills/release-notes" {
		t.Fatalf("installTargets() = %+v, want a single custom project-relative target", got)
	}
}
