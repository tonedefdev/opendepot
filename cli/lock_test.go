package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLockRoundTrip(t *testing.T) {
	lock := &lockFile{
		Skills: []lockEntry{{
			Source:      "registry.example.com/acme/lint",
			Version:     "1.2.0",
			Constraints: ">= 1.0.0",
			SigningKey:  "0123456789ABCDEF0123456789ABCDEF01234567",
			Signature:   "sha256:1111",
			Hashes:      []string{"h1:abc=", "zh:def"},
			Installed: []lockInstalled{
				{Target: "copilot", Path: ".github/skills/lint", Hash: "h1:abc="},
				{Target: "claude", Path: ".claude/skills/lint", Hash: "h1:abc="},
			},
		}},
		Agents: []lockEntry{{
			Source:      "registry.example.com/acme/reviewer",
			Version:     "0.3.0",
			Constraints: "~> 0.3",
			SigningKey:  "0123456789ABCDEF0123456789ABCDEF01234567",
			Signature:   "sha256:2222",
			Hashes:      []string{"h1:xyz=", "zh:123"},
			Installed: []lockInstalled{
				{Target: "agents-md", Path: "AGENTS.md", Hash: "h1:block="},
			},
		}},
	}

	first := renderLock(lock)
	path := filepath.Join(t.TempDir(), lockFileName)
	if err := os.WriteFile(path, first, 0o644); err != nil {
		t.Fatal(err)
	}

	loaded, err := loadLock(path)
	if err != nil {
		t.Fatalf("loadLock: %v", err)
	}

	second := renderLock(loaded)
	if string(first) != string(second) {
		t.Fatalf("lock round-trip changed the file:\n%s\n---\n%s", first, second)
	}

	if got := loaded.Skills[0].hash("zh:"); got != "zh:def" {
		t.Fatalf("zh hash = %q, want zh:def", got)
	}

	if !strings.Contains(string(first), `agent_skill "registry.example.com/acme/lint"`) {
		t.Fatalf("lock missing agent_skill block:\n%s", first)
	}

	if !strings.Contains(string(first), `agent "registry.example.com/acme/reviewer"`) {
		t.Fatalf("lock missing agent block:\n%s", first)
	}
}

func TestLoadLockMissingFileIsEmpty(t *testing.T) {
	lock, err := loadLock(filepath.Join(t.TempDir(), lockFileName))
	if err != nil {
		t.Fatalf("loadLock: %v", err)
	}

	if len(lock.Skills) != 0 || len(lock.Agents) != 0 {
		t.Fatalf("expected empty lock, got %+v", lock)
	}
}
