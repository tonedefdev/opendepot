package agentspec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFindAgentEntry(t *testing.T) {
	tests := []struct {
		name     string
		files    []string
		agent    string
		wantFile string
		wantErr  bool
	}{
		{name: "copilot layout", files: []string{"code-review.agent.md"}, agent: "code-review", wantFile: "code-review.agent.md"},
		{name: "claude layout", files: []string{"code-review.md"}, agent: "code-review", wantFile: "code-review.md"},
		{name: "neither layout", files: []string{"other.md"}, agent: "code-review", wantErr: true},
		{name: "both layouts is ambiguous", files: []string{"code-review.agent.md", "code-review.md"}, agent: "code-review", wantErr: true},
		{name: "path traversal rejected", files: []string{"code-review.md"}, agent: "../code-review", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()

			for _, f := range tt.files {
				if err := os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			got, err := FindAgentEntry(dir, tt.agent)
			if (err != nil) != tt.wantErr {
				t.Fatalf("FindAgentEntry() error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.wantErr {
				return
			}

			if filepath.Base(got) != tt.wantFile {
				t.Fatalf("FindAgentEntry() = %s, want %s", filepath.Base(got), tt.wantFile)
			}
		})
	}
}

func TestParseAgentNameIsDisplayMetadata(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		content  string
		wantName string
		wantErr  bool
	}{
		{
			name:     "file name matches frontmatter",
			path:     "/tmp/agents/code-review.agent.md",
			content:  "---\nname: code-review\ndescription: Reviews code changes.\n---\nBody.\n",
			wantName: "code-review",
		},
		{
			name:     "display name differs from file name",
			path:     "/tmp/agents/code-review.md",
			content:  "---\nname: \"OpenDepot Code Review\"\ndescription: Reviews code changes.\n---\nBody.\n",
			wantName: "OpenDepot Code Review",
		},
		{
			name:    "missing name",
			path:    "/tmp/agents/code-review.md",
			content: "---\ndescription: Reviews code changes.\n---\nBody.\n",
			wantErr: true,
		},
		{
			name:    "name over length limit",
			path:    "/tmp/agents/code-review.md",
			content: "---\nname: " + strings.Repeat("a", MaxNameLength+1) + "\ndescription: Reviews code changes.\n---\nBody.\n",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := ParseAgent(tt.path, []byte(tt.content))
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseAgent() error = %v, findings = %+v, wantErr %v", err, res.Findings, tt.wantErr)
			}

			if !tt.wantErr && res.Name != tt.wantName {
				t.Fatalf("Name = %q, want %q", res.Name, tt.wantName)
			}
		})
	}
}

func TestParseSkillStillRequiresDirectoryMatch(t *testing.T) {
	content := []byte("---\nname: Code Review\ndescription: Reviews code changes.\n---\nBody.\n")

	if _, err := Parse("/tmp/skills/code-review/SKILL.md", content); err == nil {
		t.Fatal("Parse() expected an error for a skill whose name is not a valid slug")
	}
}

func TestParseSkillNamedUsesExplicitName(t *testing.T) {
	content := []byte("---\nname: gh-actions-debug\ndescription: Debugs workflows.\n---\nBody.\n")

	if _, err := ParseSkillNamed("/tmp/opendepot-agentscan-123/SKILL.md", content, "gh-actions-debug"); err != nil {
		t.Fatalf("ParseSkillNamed() unexpected error: %v", err)
	}

	if _, err := ParseSkillNamed("/tmp/opendepot-agentscan-123/SKILL.md", content, "other-skill"); err == nil {
		t.Fatal("ParseSkillNamed() expected an error when the name does not match the explicit name")
	}
}
