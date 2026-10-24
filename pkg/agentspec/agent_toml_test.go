package agentspec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseCodexAgent(t *testing.T) {
	tests := []struct {
		name         string
		content      string
		platform     string
		wantErr      bool
		wantName     string
		wantDesc     string
		wantModel    string
		wantBody     string
		wantFindings []string
	}{
		{
			name: "valid codex agent",
			content: `name = "code-reviewer"
description = "Reviews code for security issues."
developer_instructions = """
You are a security reviewer.
"""
model = "gpt-5.5"
model_reasoning_effort = "high"
`,
			wantName:  "code-reviewer",
			wantDesc:  "Reviews code for security issues.",
			wantModel: "gpt-5.5",
			wantBody:  "You are a security reviewer.\n",
		},
		{
			name:         "missing developer_instructions",
			content:      "name = \"code-reviewer\"\ndescription = \"Reviews code.\"\n",
			wantErr:      true,
			wantName:     "code-reviewer",
			wantDesc:     "Reviews code.",
			wantFindings: []string{FindingMissingInstructions},
		},
		{
			name:         "blank developer_instructions",
			content:      "name = \"code-reviewer\"\ndescription = \"Reviews code.\"\ndeveloper_instructions = \"  \"\n",
			wantErr:      true,
			wantName:     "code-reviewer",
			wantDesc:     "Reviews code.",
			wantFindings: []string{FindingMissingInstructions},
		},
		{
			name:         "missing name and description",
			content:      "developer_instructions = \"Do the thing.\"\n",
			wantErr:      true,
			wantBody:     "Do the thing.",
			wantFindings: []string{FindingInvalidName, FindingMissingDescription},
		},
		{
			name:         "unknown key is a warning",
			content:      "name = \"code-reviewer\"\ndescription = \"Reviews code.\"\ndeveloper_instructions = \"Review.\"\ntools = \"Read\"\n",
			wantName:     "code-reviewer",
			wantDesc:     "Reviews code.",
			wantBody:     "Review.",
			wantFindings: []string{FindingUnknownKey},
		},
		{
			name:         "invalid toml",
			content:      "name = \"code-reviewer\n",
			wantErr:      true,
			wantFindings: []string{FindingInvalidFrontmatter},
		},
		{
			name:         "copilot platform rejected",
			content:      "name = \"code-reviewer\"\ndescription = \"Reviews code.\"\ndeveloper_instructions = \"Review.\"\n",
			platform:     PlatformCopilot,
			wantErr:      true,
			wantFindings: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseAgentFor("code-reviewer.toml", []byte(tt.content), tt.platform)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseAgentFor() error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.platform != "" && tt.platform != PlatformCodex {
				if len(got.Findings) != 0 {
					t.Fatalf("expected no findings for unsupported platform, got %+v", got.Findings)
				}

				return
			}

			if got.Name != tt.wantName {
				t.Errorf("Name = %q, want %q", got.Name, tt.wantName)
			}

			if got.Description != tt.wantDesc {
				t.Errorf("Description = %q, want %q", got.Description, tt.wantDesc)
			}

			if got.Model != tt.wantModel {
				t.Errorf("Model = %q, want %q", got.Model, tt.wantModel)
			}

			if got.Body != tt.wantBody {
				t.Errorf("Body = %q, want %q", got.Body, tt.wantBody)
			}

			gotIDs := make([]string, 0, len(got.Findings))
			for _, f := range got.Findings {
				gotIDs = append(gotIDs, f.VulnerabilityID)
			}

			if len(gotIDs) != len(tt.wantFindings) {
				t.Fatalf("findings = %v, want %v", gotIDs, tt.wantFindings)
			}

			for i := range gotIDs {
				if gotIDs[i] != tt.wantFindings[i] {
					t.Fatalf("findings = %v, want %v", gotIDs, tt.wantFindings)
				}
			}
		})
	}
}

func TestParseCodexAgentUnknownKeySeverity(t *testing.T) {
	content := "name = \"code-reviewer\"\ndescription = \"Reviews code.\"\ndeveloper_instructions = \"Review.\"\ntools = \"Read\"\n"

	got, err := ParseAgent("code-reviewer.toml", []byte(content))
	if err != nil {
		t.Fatalf("ParseAgent() returned error for a warning-only file: %v", err)
	}

	if len(got.Findings) != 1 || got.Findings[0].Severity != severityWarning {
		t.Fatalf("expected one warning finding, got %+v", got.Findings)
	}

	if !strings.Contains(got.Findings[0].Title, `unknown key "tools"`) {
		t.Fatalf("unexpected warning title %q", got.Findings[0].Title)
	}
}

func TestFindAgentEntryCodexLayout(t *testing.T) {
	tests := []struct {
		name     string
		files    []string
		wantFile string
		wantErr  bool
	}{
		{name: "codex layout", files: []string{"code-review.toml"}, wantFile: "code-review.toml"},
		{name: "codex and copilot is ambiguous", files: []string{"code-review.toml", "code-review.agent.md"}, wantErr: true},
		{name: "codex and claude is ambiguous", files: []string{"code-review.toml", "code-review.md"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()

			for _, f := range tt.files {
				if err := os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			got, err := FindAgentEntry(dir, "code-review")
			if (err != nil) != tt.wantErr {
				t.Fatalf("FindAgentEntry() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr && filepath.Base(got) != tt.wantFile {
				t.Fatalf("FindAgentEntry() = %s, want %s", filepath.Base(got), tt.wantFile)
			}
		})
	}
}
