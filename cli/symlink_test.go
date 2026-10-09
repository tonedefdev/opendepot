package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureNoSymlinksRefusesSymlinkedParent(t *testing.T) {
	project := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(project, ".github")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	a := &app{dir: project}
	if err := a.ensureNoSymlinks(".github/skills/review/SKILL.md"); err == nil {
		t.Fatal("ensureNoSymlinks() succeeded through a symlinked parent, want error")
	}

	if err := a.ensureNoSymlinks("missing/dir/file.md"); err != nil {
		t.Fatalf("ensureNoSymlinks() for missing path error = %v", err)
	}
}

func TestEnsureNoSymlinksRefusesSymlinkedFile(t *testing.T) {
	project := t.TempDir()
	target := filepath.Join(t.TempDir(), "AGENTS.md")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.Symlink(target, filepath.Join(project, "AGENTS.md")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	a := &app{dir: project}
	if err := a.ensureNoSymlinks("AGENTS.md"); err == nil {
		t.Fatal("ensureNoSymlinks() succeeded for a symlinked file, want error")
	}
}

func TestInstallEntryRefusesSymlinkedParent(t *testing.T) {
	project := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(project, ".github")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	a := &app{dir: project}
	target := installTarget{path: ".github/skills/review"}
	if err := a.installEntry(&stagedEntry{}, target); err == nil {
		t.Fatal("installEntry() succeeded through a symlinked parent, want error")
	}

	entries, err := os.ReadDir(outside)
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 0 {
		t.Fatalf("installEntry() wrote %d entries outside the project", len(entries))
	}
}

func TestVerifyProjectRefusesSymlinkedLockFile(t *testing.T) {
	project := t.TempDir()
	outside := t.TempDir()
	lockOutside := filepath.Join(outside, "opendepot.lock.hcl")
	if err := os.WriteFile(lockOutside, []byte("lock {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.Symlink(lockOutside, filepath.Join(project, lockFileName)); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	a := &app{dir: project}
	err := a.verifyProject()
	if err == nil || !strings.Contains(err.Error(), "refusing to follow symlink") {
		t.Fatalf("verifyProject() error = %v, want symlink refusal", err)
	}
}
