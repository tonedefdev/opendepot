package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const codexAgentDoc = "name = \"reviewer\"\ndescription = \"Reviews things\"\ndeveloper_instructions = \"Review body\"\n"

func writeAgentProject(t *testing.T, dir, host string, targets []string) {
	t.Helper()
	quoted := make([]string, len(targets))
	for i, target := range targets {
		quoted[i] = `"` + target + `"`
	}

	body := `opendepot {
  targets = [` + strings.Join(quoted, ", ") + `]
}

agent "reviewer" {
  source  = "` + host + `/acme/reviewer"
  version = ">= 1.0.0"
}
`
	if err := os.WriteFile(filepath.Join(dir, configFileName), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestTOMLAgentInstallsOnlyToCodex(t *testing.T) {
	isolateHome(t)
	home := os.Getenv("HOME")
	reg := newFakeRegistry(t)
	reg.publish(t, kindAgent, "reviewer", release{version: "1.0.0", files: map[string]string{"reviewer.toml": codexAgentDoc}})
	dir := t.TempDir()
	writeAgentProject(t, dir, reg.host, []string{"copilot", "claude", "codex", "agents-md"})
	a := testApp(reg, dir)

	if err := a.applyProject(context.Background(), applyOptions{}); err != nil {
		t.Fatalf("apply: %v", err)
	}

	if got := readFile(t, filepath.Join(home, ".codex", "agents", "reviewer.toml")); got != codexAgentDoc {
		t.Fatalf("codex agent was not copied verbatim:\n%s", got)
	}

	for _, p := range []string{
		filepath.Join(home, ".copilot", "agents", "reviewer.agent.md"),
		filepath.Join(home, ".claude", "agents", "reviewer.md"),
	} {
		if _, err := os.Stat(p); err == nil {
			t.Fatalf("TOML agent must not install to %s", p)
		}
	}

	if agents, err := os.ReadFile(filepath.Join(dir, agentsMDFile)); err == nil && strings.Contains(string(agents), agentsBeginPrefix) {
		t.Fatalf("TOML agent must not be written to AGENTS.md:\n%s", agents)
	}

	if err := a.verifyProject(); err != nil {
		t.Fatalf("verify after apply: %v", err)
	}
}

func TestTOMLAgentWithoutCodexTargetFailsBeforeWrites(t *testing.T) {
	isolateHome(t)
	reg := newFakeRegistry(t)
	reg.publish(t, kindAgent, "reviewer", release{version: "1.0.0", files: map[string]string{"reviewer.toml": codexAgentDoc}})
	dir := t.TempDir()
	writeAgentProject(t, dir, reg.host, []string{"copilot", "claude", "agents-md"})

	err := testApp(reg, dir).applyProject(context.Background(), applyOptions{})
	if err == nil || !strings.Contains(err.Error(), "no install target accepts this agent") {
		t.Fatalf("apply error = %v, want a no-target error", err)
	}

	assertNoWrites(t, dir)
}

func TestMarkdownAgentNeverInstallsToCodex(t *testing.T) {
	isolateHome(t)
	home := os.Getenv("HOME")
	reg := newFakeRegistry(t)
	reg.publish(t, kindAgent, "reviewer", release{version: "1.0.0", files: map[string]string{"reviewer.md": agentDoc}})
	dir := t.TempDir()
	writeAgentProject(t, dir, reg.host, []string{"codex", "copilot"})

	if err := testApp(reg, dir).applyProject(context.Background(), applyOptions{}); err != nil {
		t.Fatalf("apply: %v", err)
	}

	if _, err := os.Stat(filepath.Join(home, ".copilot", "agents", "reviewer.agent.md")); err != nil {
		t.Fatalf("copilot target missing: %v", err)
	}

	if _, err := os.Stat(filepath.Join(home, ".codex")); err == nil {
		t.Fatal("Markdown agent must not install under ~/.codex")
	}
}
