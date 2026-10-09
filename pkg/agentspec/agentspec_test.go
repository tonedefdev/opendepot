package agentspec

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name         string
		path         string
		wantErr      bool
		wantName     string
		wantDesc     string
		wantTools    []string
		wantModel    string
		wantBody     string
		wantFindings []string
		wantSeverity string
	}{
		{
			name:      "valid skill",
			path:      "testdata/valid-skill/SKILL.md",
			wantName:  "valid-skill",
			wantDesc:  "Summarizes pull requests into release notes.",
			wantTools: nil,
			wantBody:  "# Valid Skill\n\nRead the diff and write release notes.\n",
		},
		{
			name:      "valid agent",
			path:      "testdata/valid-agent.md",
			wantName:  "valid-agent",
			wantDesc:  "Reviews code for security issues.",
			wantTools: []string{"Read", "Grep", "Glob"},
			wantModel: "sonnet",
			wantBody:  "You are a security reviewer.\n",
		},
		{
			name:         "missing description",
			path:         "testdata/missing-description/SKILL.md",
			wantErr:      true,
			wantName:     "missing-description",
			wantBody:     "Body.\n",
			wantFindings: []string{FindingMissingDescription},
			wantSeverity: severityError,
		},
		{
			name:         "bad name",
			path:         "testdata/bad-name/SKILL.md",
			wantErr:      true,
			wantDesc:     "Name contains uppercase letters and a double hyphen.",
			wantBody:     "Body.\n",
			wantFindings: []string{FindingInvalidName},
			wantSeverity: severityError,
		},
		{
			name:         "name directory mismatch",
			path:         "testdata/mismatch/SKILL.md",
			wantErr:      true,
			wantName:     "other-name",
			wantDesc:     "Name does not match the directory.",
			wantBody:     "Body.\n",
			wantFindings: []string{FindingNameMismatch},
			wantSeverity: severityError,
		},
		{
			name:         "unknown key warning",
			path:         "testdata/unknown-key/SKILL.md",
			wantName:     "unknown-key",
			wantDesc:     "Contains a key that is not recognized.",
			wantBody:     "Body.\n",
			wantFindings: []string{FindingUnknownKey},
			wantSeverity: severityWarning,
		},
		{
			name:         "no frontmatter",
			path:         "testdata/no-frontmatter/SKILL.md",
			wantErr:      true,
			wantFindings: []string{FindingNoFrontmatter},
			wantSeverity: severityError,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			content, err := os.ReadFile(filepath.FromSlash(test.path))

			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}

			result, err := Parse(test.path, content)

			if (err != nil) != test.wantErr {
				t.Fatalf("Parse() error = %v, wantErr %v", err, test.wantErr)
			}

			if result == nil {
				t.Fatal("Parse() returned nil result")
			}

			if result.Name != test.wantName {
				t.Errorf("Name = %q, want %q", result.Name, test.wantName)
			}

			if result.Description != test.wantDesc {
				t.Errorf("Description = %q, want %q", result.Description, test.wantDesc)
			}

			if !slices.Equal(result.Tools, test.wantTools) {
				t.Errorf("Tools = %v, want %v", result.Tools, test.wantTools)
			}

			if result.Model != test.wantModel {
				t.Errorf("Model = %q, want %q", result.Model, test.wantModel)
			}

			if result.Body != test.wantBody {
				t.Errorf("Body = %q, want %q", result.Body, test.wantBody)
			}

			if len(result.Findings) != len(test.wantFindings) {
				t.Fatalf("Findings = %+v, want IDs %v", result.Findings, test.wantFindings)
			}

			for i, finding := range result.Findings {
				if finding.VulnerabilityID != test.wantFindings[i] {
					t.Errorf("finding[%d].VulnerabilityID = %q, want %q", i, finding.VulnerabilityID, test.wantFindings[i])
				}

				if finding.Severity != test.wantSeverity {
					t.Errorf("finding[%d].Severity = %q, want %q", i, finding.Severity, test.wantSeverity)
				}
			}
		})
	}
}

func TestParseFrontmatterMap(t *testing.T) {
	content := []byte("---\nname: valid-agent\ndescription: Reviews code.\ntools: [Read, Grep]\n---\nBody\n")

	result, err := Parse("valid-agent.md", content)

	if err != nil {
		t.Fatalf("Parse() unexpected error: %v", err)
	}

	if result.Frontmatter["description"] != "Reviews code." {
		t.Errorf("Frontmatter[description] = %v", result.Frontmatter["description"])
	}

	if !slices.Equal(result.Tools, []string{"Read", "Grep"}) {
		t.Errorf("Tools = %v, want [Read Grep]", result.Tools)
	}
}

func TestParseValidationFindings(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		content string
		wantIDs []string
	}{
		{
			name:    "name too long",
			path:    "agents/" + strings.Repeat("a", MaxNameLength+1) + ".md",
			content: "---\nname: " + strings.Repeat("a", MaxNameLength+1) + "\ndescription: d\n---\n",
			wantIDs: []string{FindingInvalidName},
		},
		{
			name:    "name with trailing hyphen",
			path:    "agents/trail-.md",
			content: "---\nname: trail-\ndescription: d\n---\n",
			wantIDs: []string{FindingInvalidName},
		},
		{
			name:    "description too long",
			path:    "agents/long.md",
			content: "---\nname: long\ndescription: " + strings.Repeat("d", MaxDescriptionLength+1) + "\n---\n",
			wantIDs: []string{FindingDescriptionTooLong},
		},
		{
			name:    "tools wrong type",
			path:    "agents/tools.md",
			content: "---\nname: tools\ndescription: d\ntools: 42\n---\n",
			wantIDs: []string{FindingInvalidTools},
		},
		{
			name:    "model wrong type",
			path:    "agents/model.md",
			content: "---\nname: model\ndescription: d\nmodel: [sonnet]\n---\n",
			wantIDs: []string{FindingInvalidModel},
		},
		{
			name:    "invalid yaml",
			path:    "agents/broken.md",
			content: "---\nname: [unclosed\n---\n",
			wantIDs: []string{FindingInvalidFrontmatter},
		},
		{
			name:    "crlf line endings are accepted",
			path:    "agents/crlf.md",
			content: "---\r\nname: crlf\r\ndescription: d\r\n---\r\nBody\r\n",
			wantIDs: nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := Parse(test.path, []byte(test.content))

			if (err != nil) != (len(test.wantIDs) > 0) {
				t.Fatalf("Parse() error = %v, wantIDs %v", err, test.wantIDs)
			}

			var got []string

			for _, finding := range result.Findings {
				got = append(got, finding.VulnerabilityID)
			}

			if !slices.Equal(got, test.wantIDs) {
				t.Errorf("finding IDs = %v, want %v", got, test.wantIDs)
			}
		})
	}
}

func TestParsePlatformKeys(t *testing.T) {
	tests := []struct {
		name     string
		agent    bool
		platform string
		content  string
		wantIDs  []string
		wantErr  bool
	}{
		{
			name:     "copilot skill accepts argument-hint",
			platform: PlatformCopilot,
			content:  "---\nname: skill\ndescription: d\nargument-hint: [topic]\n---\n",
			wantIDs:  nil,
		},
		{
			name:     "agentskills skill rejects argument-hint",
			platform: PlatformAgentSkills,
			content:  "---\nname: skill\ndescription: d\nargument-hint: [topic]\n---\n",
			wantIDs:  []string{FindingUnknownKey},
		},
		{
			name:     "empty platform accepts argument-hint",
			platform: "",
			content:  "---\nname: skill\ndescription: d\nargument-hint: [topic]\n---\n",
			wantIDs:  nil,
		},
		{
			name:     "agentskills accepts compatibility",
			platform: PlatformAgentSkills,
			content:  "---\nname: skill\ndescription: d\ncompatibility: Requires git\n---\n",
			wantIDs:  nil,
		},
		{
			name:     "claude agent accepts disallowedTools",
			agent:    true,
			platform: PlatformClaude,
			content:  "---\nname: reviewer\ndescription: d\ndisallowedTools: Write\n---\n",
			wantIDs:  nil,
		},
		{
			name:     "copilot agent rejects disallowedTools",
			agent:    true,
			platform: PlatformCopilot,
			content:  "---\nname: reviewer\ndescription: d\ndisallowedTools: Write\n---\n",
			wantIDs:  []string{FindingUnknownKey},
		},
		{
			name:     "copilot agent accepts handoffs",
			agent:    true,
			platform: PlatformCopilot,
			content:  "---\nname: planner\ndescription: d\nhandoffs: []\n---\n",
			wantIDs:  nil,
		},
		{
			name:     "opencode agent accepts temperature",
			agent:    true,
			platform: PlatformOpenCode,
			content:  "---\nname: writer\ndescription: d\ntemperature: 0.2\n---\n",
			wantIDs:  nil,
		},
		{
			name:     "codex agent falls back to the generic agent keys",
			agent:    true,
			platform: PlatformCodex,
			content:  "---\nname: writer\ndescription: d\nmode: primary\n---\n",
			wantIDs:  []string{FindingUnknownKey},
		},
		{
			name:     "unsupported platform is an error",
			agent:    true,
			platform: "unknown",
			content:  "---\nname: writer\ndescription: d\n---\n",
			wantErr:  true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var (
				result *Result
				err    error
			)

			if test.agent {
				result, err = ParseAgentFor("agents/agent.md", []byte(test.content), test.platform)
			} else {
				result, err = ParseSkillNamedFor("skills/skill/SKILL.md", []byte(test.content), "skill", test.platform)
			}

			if test.wantErr {
				if err == nil {
					t.Fatal("expected an error for unsupported platform")
				}

				if result == nil {
					t.Fatal("result must be non-nil even when the platform is unsupported")
				}

				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			var got []string

			for _, finding := range result.Findings {
				got = append(got, finding.VulnerabilityID)
			}

			if !slices.Equal(got, test.wantIDs) {
				t.Errorf("finding IDs = %v, want %v", got, test.wantIDs)
			}
		})
	}
}
