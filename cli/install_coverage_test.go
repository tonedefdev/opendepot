package main

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfirmReadsAnswer(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  bool
	}{
		{"lowercase yes", "y\n", true},
		{"uppercase yes word", "YES\n", true},
		{"no", "n\n", false},
		{"empty answer", "\n", false},
		{"end of input", "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			a := &app{in: bufio.NewReader(strings.NewReader(tc.input)), out: &out}

			if got := a.confirm("Install skill?"); got != tc.want {
				t.Fatalf("confirm() = %t, want %t", got, tc.want)
			}

			if got := out.String(); got != "Install skill? [y/N]: " {
				t.Fatalf("prompt = %q, want the install prompt", got)
			}
		})
	}
}

func TestValidatePathAcceptsSkillDirectoryAndAgentFile(t *testing.T) {
	skillDir := filepath.Join(t.TempDir(), "lint")
	if err := os.Mkdir(skillDir, 0o755); err != nil {
		t.Fatalf("Mkdir failed: %v", err)
	}

	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillDoc), 0o600); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	agentFile := filepath.Join(t.TempDir(), "reviewer.md")
	if err := os.WriteFile(agentFile, []byte(agentDoc), 0o600); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	for _, path := range []string{skillDir, agentFile} {
		var out bytes.Buffer
		if err := validatePath(&out, path); err != nil {
			t.Fatalf("validatePath(%q) error = %v", path, err)
		}

		if !strings.Contains(out.String(), "is valid") {
			t.Fatalf("validatePath(%q) output = %q, want an is valid message", path, out.String())
		}
	}
}

func TestValidatePathRejectsMissingAndInvalidSkills(t *testing.T) {
	var out bytes.Buffer
	if err := validatePath(&out, filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("validatePath() on a missing path returned nil error")
	}

	skillDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("Lint body without frontmatter\n"), 0o600); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	out.Reset()
	if err := validatePath(&out, skillDir); err == nil {
		t.Fatalf("validatePath() on a skill without frontmatter returned nil error, output = %q", out.String())
	}
}
