package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSkillFileChanges(t *testing.T) {
	root := t.TempDir()
	dst := t.TempDir()

	writeTestFile(t, root, "SKILL.md", "new body")
	writeTestFile(t, root, "rules/a.txt", "same")
	writeTestFile(t, root, "rules/b.txt", "changed upstream")
	writeTestFile(t, root, "rules/c.txt", "added")

	writeTestFile(t, dst, "SKILL.md", "old body")
	writeTestFile(t, dst, "rules/a.txt", "same")
	writeTestFile(t, dst, "rules/b.txt", "local edit")
	writeTestFile(t, dst, "stale.txt", "gone upstream")

	changes, err := skillFileChanges(root, dst)
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]string{
		"SKILL.md":    planChange,
		"rules/a.txt": planSame,
		"rules/b.txt": planChange,
		"rules/c.txt": planAdd,
		"stale.txt":   planRemove,
	}

	if len(changes) != len(want) {
		t.Fatalf("got %d changes, want %d: %+v", len(changes), len(want), changes)
	}

	for _, c := range changes {
		if want[c.rel] != c.sym {
			t.Errorf("%s: got %q, want %q", c.rel, c.sym, want[c.rel])
		}
	}

	if !hasFileChanges(changes) {
		t.Error("hasFileChanges = false, want true")
	}
}

func TestSkillFileChangesMissingDestination(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "SKILL.md", "body")

	changes, err := skillFileChanges(root, filepath.Join(t.TempDir(), "missing"))
	if err != nil {
		t.Fatal(err)
	}

	if len(changes) != 1 || changes[0].rel != "SKILL.md" || changes[0].sym != planAdd {
		t.Fatalf("got %+v, want one add of SKILL.md", changes)
	}
}

func TestSkillTreeLinesRendersConnectors(t *testing.T) {
	changes := []fileChange{
		{rel: "SKILL.md", sym: planChange},
		{rel: "rules/b.txt", sym: planChange},
		{rel: "rules/c.txt", sym: planAdd},
	}

	lines := skillTreeLines(newStyler(&strings.Builder{}), buildSkillTree(changes), "")
	got := strings.Join(lines, "\n")

	for _, want := range []string{"├── SKILL.md  ", "└── rules/", "    ├── b.txt  ", "    └── c.txt  "} {
		if !strings.Contains(got, want) {
			t.Errorf("tree missing %q:\n%s", want, got)
		}
	}
}

func writeTestFile(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
