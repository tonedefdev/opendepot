package agentassembly

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the golden files under testdata/golden")

const (
	fixturePath  = "testdata/assembly.json"
	goldenRoot   = "testdata/golden"
	hclGoldenDir = "hcl"
	testRegistry = "registry.example.com"
)

func loadFixture(t *testing.T) Document {
	t.Helper()

	raw, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()

	var doc Document
	if err := dec.Decode(&doc); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}

	return doc
}

func fileByPath(t *testing.T, files []File, p string) string {
	t.Helper()

	for _, f := range files {
		if f.Path == p {
			return string(f.Content)
		}
	}

	t.Fatalf("no rendered file %q in %v", p, filePaths(files))

	return ""
}

func filePaths(files []File) []string {
	out := make([]string, 0, len(files))
	for _, f := range files {
		out = append(out, f.Path)
	}

	return out
}

func mustRender(t *testing.T, doc Document, target string) []File {
	t.Helper()

	files, err := Render(doc, target)
	if err != nil {
		t.Fatalf("Render(%s): %v", target, err)
	}

	return files
}

func TestRenderGolden(t *testing.T) {
	doc := loadFixture(t)

	for _, target := range []string{TargetCopilot, TargetClaude, TargetAgentsMD} {
		t.Run(target, func(t *testing.T) {
			files := mustRender(t, doc, target)
			dir := filepath.Join(goldenRoot, target)

			if *update {
				writeGolden(t, dir, files)
			}

			checkGolden(t, dir, files)
		})
	}
}

func TestRenderHCLGolden(t *testing.T) {
	doc := loadFixture(t)

	out, err := RenderHCL(doc, TargetCopilot, testRegistry)
	if err != nil {
		t.Fatalf("RenderHCL: %v", err)
	}

	dir := filepath.Join(goldenRoot, hclGoldenDir)
	files := []File{{Path: "opendepot.hcl", Content: out}}
	if *update {
		writeGolden(t, dir, files)
	}

	checkGolden(t, dir, files)
}

func writeGolden(t *testing.T, dir string, files []File) {
	t.Helper()

	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("clear golden dir: %v", err)
	}

	for _, f := range files {
		p := filepath.Join(dir, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("create golden dir: %v", err)
		}

		if err := os.WriteFile(p, f.Content, 0o644); err != nil {
			t.Fatalf("write golden file: %v", err)
		}
	}
}

// checkGolden compares the rendered files with the golden files, and fails when a golden file has no rendered match.
func checkGolden(t *testing.T, dir string, files []File) {
	t.Helper()

	var expected []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			return nil
		}

		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}

		expected = append(expected, filepath.ToSlash(rel))

		return nil
	})
	if err != nil {
		t.Fatalf("walk golden dir %s: %v", dir, err)
	}

	rendered := filePaths(files)
	slices.Sort(rendered)
	slices.Sort(expected)
	if !slices.Equal(rendered, expected) {
		t.Fatalf("golden files %v do not match rendered files %v", expected, rendered)
	}

	for _, f := range files {
		want, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(f.Path)))
		if err != nil {
			t.Fatalf("read golden %s: %v", f.Path, err)
		}

		if !bytes.Equal(want, f.Content) {
			t.Errorf("%s differs from golden file:\n--- want\n%s\n--- got\n%s", f.Path, want, f.Content)
		}
	}
}

func TestRenderCopilotNeverWritesHandoffs(t *testing.T) {
	doc := loadFixture(t)

	for _, f := range mustRender(t, doc, TargetCopilot) {
		if strings.Contains(string(f.Content), "handoffs") {
			t.Fatalf("%s contains handoffs frontmatter", f.Path)
		}
	}
}

func TestRenderToolsAreOwnLinksOnly(t *testing.T) {
	doc := loadFixture(t)
	files := mustRender(t, doc, TargetCopilot)

	tests := []struct {
		path string
		want string
	}{
		{".github/agents/planner.agent.md", `tools: ["github/get_pull_request", "github/list_issues", "task"]`},
		{".github/agents/reviewer.agent.md", `tools: ["files/*"]`},
		{".github/agents/writer.agent.md", "tools: []"},
		{".github/agents/scanner.agent.md", "tools: []"},
	}

	for _, tt := range tests {
		content := fileByPath(t, files, tt.path)
		if !strings.Contains(content, tt.want+"\n") {
			t.Errorf("%s: want line %q, got:\n%s", tt.path, tt.want, content)
		}
	}
}

func TestRenderSkillsAreOwnLinksOnly(t *testing.T) {
	doc := loadFixture(t)
	files := mustRender(t, doc, TargetCopilot)

	writer := fileByPath(t, files, ".github/agents/writer.agent.md")
	if !strings.Contains(writer, `skills: ["changelog"]`) {
		t.Fatalf("writer should use only its changelog skill, got:\n%s", writer)
	}

	if content := fileByPath(t, files, ".github/agents/reviewer.agent.md"); strings.Contains(content, "skills:") {
		t.Fatalf("reviewer uses no skills and must not list any, got:\n%s", content)
	}
}

func TestRenderClaudeAgentAllowlist(t *testing.T) {
	doc := loadFixture(t)
	files := mustRender(t, doc, TargetClaude)

	tests := []struct {
		path string
		want string
	}{
		{".claude/agents/planner.md",
			"tools: mcp__github__get_pull_request, mcp__github__list_issues, Agent(reviewer, scanner, writer)"},
		{".claude/agents/reviewer.md", "tools: mcp__files"},
		{".claude/agents/writer.md", `tools: ""`},
	}

	for _, tt := range tests {
		content := fileByPath(t, files, tt.path)
		if !strings.Contains(content, tt.want+"\n") {
			t.Errorf("%s: want line %q, got:\n%s", tt.path, tt.want, content)
		}
	}
}

func TestRenderDelegationSectionOnlyWhenLinked(t *testing.T) {
	doc := loadFixture(t)

	for _, target := range []string{TargetCopilot, TargetClaude, TargetAgentsMD} {
		files := mustRender(t, doc, target)
		for _, f := range files {
			linked := strings.Contains(f.Path, "planner") || strings.Contains(f.Path, "reviewer")
			has := strings.Contains(string(f.Content), "## Delegation")
			if linked != has {
				t.Errorf("%s (%s): Delegation section present=%v, want %v", f.Path, target, has, linked)
			}
		}
	}
}

func TestRenderDeterministic(t *testing.T) {
	doc := loadFixture(t)
	want := mustRender(t, doc, TargetClaude)

	shuffled := doc
	shuffled.Nodes = slices.Clone(doc.Nodes)
	slices.Reverse(shuffled.Nodes)
	shuffled.Links = slices.Clone(doc.Links)
	slices.Reverse(shuffled.Links)

	got := mustRender(t, shuffled, TargetClaude)
	if len(got) != len(want) {
		t.Fatalf("file count changed: %d != %d", len(got), len(want))
	}

	for i := range want {
		if got[i].Path != want[i].Path || !bytes.Equal(got[i].Content, want[i].Content) {
			t.Fatalf("%s changed when the input order changed", want[i].Path)
		}
	}
}

func TestRenderTargetOverride(t *testing.T) {
	doc := loadFixture(t)

	files := mustRender(t, doc, TargetAgentsMD)
	if got := filePaths(files); !slices.Contains(got, "planner/AGENTS.md") {
		t.Fatalf("override should render agents-md files, got %v", got)
	}

	if _, err := Render(doc, "vscode"); err == nil {
		t.Fatal("an unknown target must be rejected")
	}
}

func TestRenderRefusesBlockedDocuments(t *testing.T) {
	doc := loadFixture(t)
	doc.Tutorial = true
	for i := range doc.Nodes {
		if doc.Nodes[i].ID == "changelog" {
			doc.Nodes[i].Placeholder = &Placeholder{Description: testSample}
		}
	}

	_, err := Render(doc, TargetCopilot)

	var exportErr *ExportError
	if !errors.As(err, &exportErr) {
		t.Fatalf("want ExportError for a placeholder, got %v", err)
	}

	if !strings.Contains(exportErr.Error(), CodePlaceholderNotExportable) {
		t.Fatalf("error should name the blocking code, got %q", exportErr.Error())
	}
}

func TestRenderSanitizesLabels(t *testing.T) {
	doc := loadFixture(t)
	for i := range doc.Nodes {
		if doc.Nodes[i].ID == "planner" {
			doc.Nodes[i].Label = "line one\n---\ninjected: true"
		}
	}

	files := mustRender(t, doc, TargetCopilot)
	content := fileByPath(t, files, ".github/agents/planner.agent.md")
	if strings.Count(content, "---\n") != 2 {
		t.Fatalf("a label must not add frontmatter lines, got:\n%s", content)
	}
}

func TestRenderHCLPinsAllNodes(t *testing.T) {
	doc := loadFixture(t)

	out, err := RenderHCL(doc, TargetCopilot, testRegistry)
	if err != nil {
		t.Fatalf("RenderHCL: %v", err)
	}

	text := string(out)
	for _, want := range []string{
		`agent "planner" {`,
		`agent_skill "release-notes" {`,
		`mcp_server "github" {`,
		`source  = "registry.example.com/acme/release-planner"`,
		`version = "1.2.0"`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("opendepot.hcl missing %q", want)
		}
	}
}

func TestRenderHCLRejectsConstraints(t *testing.T) {
	doc := loadFixture(t)
	for i := range doc.Nodes {
		if doc.Nodes[i].ID == "reviewer" {
			doc.Nodes[i].Ref.Version = "~> 2.0"
		}
	}

	if _, err := RenderHCL(doc, TargetCopilot, testRegistry); err == nil {
		t.Fatal("a constraint is not a pinned version and must be rejected")
	}
}

func TestRenderHCLRequiresRegistry(t *testing.T) {
	if _, err := RenderHCL(loadFixture(t), TargetCopilot, ""); err == nil {
		t.Fatal("a registry host is required")
	}
}

func TestRenderHCLBlocksPlaceholderExport(t *testing.T) {
	doc := loadFixture(t)
	doc.Tutorial = true
	for i := range doc.Nodes {
		if doc.Nodes[i].ID == "github" {
			doc.Nodes[i].Placeholder = &Placeholder{Description: testSample}
		}
	}

	if _, err := RenderHCL(doc, TargetCopilot, testRegistry); err == nil {
		t.Fatal("a placeholder must block the HCL export")
	}
}

func TestHCLQuote(t *testing.T) {
	got := hclQuote("a\"b\\c${d}%{e}\nf\t\r")
	want := `"a\"b\\c$${d}%%{e}\nf\t\r"`
	if got != want {
		t.Fatalf("hclQuote = %s, want %s", got, want)
	}
}
