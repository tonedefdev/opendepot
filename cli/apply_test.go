package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	skillDoc = "---\nname: lint\ndescription: Lints things\n---\nLint body\n"
	agentDoc = "---\nname: reviewer\ndescription: Reviews things\n---\nReview body\n"
)

func testApp(reg *fakeRegistry, dir string) *app {
	return &app{
		in:         bufio.NewReader(strings.NewReader("")),
		out:        io.Discard,
		httpClient: reg.server.Client(),
		scheme:     "http",
		dir:        dir,
		tokenFor:   func(string) string { return reg.token },
	}
}

func writeProject(t *testing.T, dir, host, signingKey string) {
	t.Helper()
	pin := ""
	if signingKey != "" {
		pin = fmt.Sprintf("\n  signing_key = %q", signingKey)
	}

	body := fmt.Sprintf(`opendepot {
  targets = ["copilot", "claude", "agents-md"]
}

agent_skill "lint" {
  source  = "%[1]s/acme/lint"
  version = ">= 1.0.0"%[2]s
}

agent "reviewer" {
  source  = "%[1]s/acme/reviewer"
  version = ">= 1.0.0"%[2]s
}
`, host, pin)
	if err := os.WriteFile(filepath.Join(dir, configFileName), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func publishStandard(t *testing.T, reg *fakeRegistry) {
	t.Helper()
	reg.publish(t, kindSkill, "lint", release{version: "1.0.0", files: map[string]string{"SKILL.md": skillDoc}})
	reg.publish(t, kindAgent, "reviewer", release{version: "1.0.0", files: map[string]string{"reviewer.md": agentDoc}})
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	return string(content)
}

func isolateHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
}

func writeSkillProject(t *testing.T, dir, host, version string) {
	t.Helper()
	body := fmt.Sprintf(`opendepot {
  targets = ["copilot", "claude"]
}

agent_skill "lint" {
  source  = "%[1]s/acme/lint"
  version = "%[2]s"
}
`, host, version)
	if err := os.WriteFile(filepath.Join(dir, configFileName), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// assertNoWrites fails if any install target, AGENTS.md, or the lock file exists in dir.
func assertNoWrites(t *testing.T, dir string) {
	t.Helper()
	for _, p := range []string{".github", ".claude", agentsMDFile, lockFileName} {
		if _, err := os.Stat(filepath.Join(dir, p)); err == nil {
			t.Fatalf("%s was written despite a failed verification", p)
		}
	}
}

func TestInitInstallsAllTargetsAndIsIdempotent(t *testing.T) {
	reg := newFakeRegistry(t)
	publishStandard(t, reg)
	dir := t.TempDir()
	writeProject(t, dir, reg.host, "")
	a := testApp(reg, dir)
	ctx := context.Background()

	if err := a.applyProject(ctx, applyOptions{}); err != nil {
		t.Fatalf("apply: %v", err)
	}

	for _, p := range []string{
		".github/skills/lint/SKILL.md",
		".claude/skills/lint/SKILL.md",
		".github/agents/reviewer.agent.md",
		".claude/agents/reviewer.md",
	} {
		if _, err := os.Stat(filepath.Join(dir, p)); err != nil {
			t.Fatalf("missing install target %s: %v", p, err)
		}
	}

	agents := readFile(t, filepath.Join(dir, agentsMDFile))
	if strings.Count(agents, agentsBeginPrefix) != 2 {
		t.Fatalf("expected two AGENTS.md blocks:\n%s", agents)
	}

	lock := readFile(t, filepath.Join(dir, lockFileName))
	if err := a.applyProject(ctx, applyOptions{}); err != nil {
		t.Fatalf("second apply: %v", err)
	}

	if got := readFile(t, filepath.Join(dir, lockFileName)); got != lock {
		t.Fatalf("second apply changed the lock file:\n%s\n---\n%s", lock, got)
	}

	if got := readFile(t, filepath.Join(dir, agentsMDFile)); got != agents {
		t.Fatalf("second apply changed AGENTS.md")
	}

	if err := a.verifyProject(); err != nil {
		t.Fatalf("verify after apply: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, ".claude/agents/reviewer.md"), []byte("edited"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := a.verifyProject(); err == nil {
		t.Fatal("expected verify to fail after an installed file was modified")
	}
}

func TestTamperedArchiveFailsBeforeWrites(t *testing.T) {
	reg := newFakeRegistry(t)
	publishStandard(t, reg)
	reg.files["reviewer-1.0.0.tar.gz"] = []byte("tampered bytes")
	dir := t.TempDir()
	writeProject(t, dir, reg.host, "")

	err := testApp(reg, dir).applyProject(context.Background(), applyOptions{})
	if err == nil || !strings.Contains(err.Error(), "does not match signed SHA256SUMS") {
		t.Fatalf("expected zh mismatch, got %v", err)
	}

	assertNoWrites(t, dir)
}

func TestTamperedSumsFailsSignature(t *testing.T) {
	reg := newFakeRegistry(t)
	publishStandard(t, reg)
	reg.tamperSums()
	dir := t.TempDir()
	writeProject(t, dir, reg.host, "")

	err := testApp(reg, dir).applyProject(context.Background(), applyOptions{})
	if err == nil || !strings.Contains(err.Error(), "does not verify") {
		t.Fatalf("expected signature failure, got %v", err)
	}

	assertNoWrites(t, dir)
}

func TestRotatedKeyRequiresTrustNewKey(t *testing.T) {
	reg := newFakeRegistry(t)
	publishStandard(t, reg)
	dir := t.TempDir()
	writeProject(t, dir, reg.host, "")
	a := testApp(reg, dir)
	ctx := context.Background()

	if err := a.applyProject(ctx, applyOptions{}); err != nil {
		t.Fatalf("initial apply: %v", err)
	}

	before := readFile(t, filepath.Join(dir, lockFileName))
	oldKey := fingerprintOf(reg.signer)
	if !strings.Contains(before, oldKey) {
		t.Fatalf("lock does not pin the first signing key:\n%s", before)
	}

	reg.rotate(t, newTestKey(t, "Rotated Signer"))
	err := a.applyProject(ctx, applyOptions{noPrompt: true})
	if err == nil || !strings.Contains(err.Error(), "trust-new-key") {
		t.Fatalf("expected rotated key to fail under no-prompt, got %v", err)
	}

	if got := readFile(t, filepath.Join(dir, lockFileName)); got != before {
		t.Fatalf("failed rotation changed the lock file")
	}

	if err := a.applyProject(ctx, applyOptions{trustNewKey: true}); err != nil {
		t.Fatalf("apply with trust-new-key: %v", err)
	}

	after := readFile(t, filepath.Join(dir, lockFileName))
	if !strings.Contains(after, fingerprintOf(reg.signer)) || strings.Contains(after, oldKey) {
		t.Fatalf("lock was not re-pinned to the new key:\n%s", after)
	}
}

func TestResignedLockedVersionFailsSignatureHash(t *testing.T) {
	reg := newFakeRegistry(t)
	publishStandard(t, reg)
	dir := t.TempDir()
	writeProject(t, dir, reg.host, "")
	a := testApp(reg, dir)
	ctx := context.Background()

	if err := a.applyProject(ctx, applyOptions{}); err != nil {
		t.Fatalf("initial apply: %v", err)
	}

	before := readFile(t, filepath.Join(dir, lockFileName))
	reg.resignAt(t, time.Now().Add(time.Hour))
	err := a.applyProject(ctx, applyOptions{})
	if err == nil || !strings.Contains(err.Error(), "signature_hash") {
		t.Fatalf("expected re-signed locked version to fail the signature_hash check, got %v", err)
	}

	if got := readFile(t, filepath.Join(dir, lockFileName)); got != before {
		t.Fatalf("failed re-sign check changed the lock file")
	}
}

func TestSigningKeyPinMismatchFailsWithoutPrompt(t *testing.T) {
	reg := newFakeRegistry(t)
	publishStandard(t, reg)
	other := newTestKey(t, "Other Signer")
	dir := t.TempDir()
	writeProject(t, dir, reg.host, fingerprintOf(other))

	err := testApp(reg, dir).applyProject(context.Background(), applyOptions{trustNewKey: true})
	if err == nil || !strings.Contains(err.Error(), "does not match configured signing_key") {
		t.Fatalf("expected pin mismatch, got %v", err)
	}

	assertNoWrites(t, dir)

	writeProject(t, dir, reg.host, fingerprintOf(reg.signer))
	if err := testApp(reg, dir).applyProject(context.Background(), applyOptions{}); err != nil {
		t.Fatalf("apply with matching pin: %v", err)
	}
}

func TestConfiguredSigningKeyNeverPrompts(t *testing.T) {
	fatal := func(string) bool {
		t.Fatal("configured signing_key must not prompt")
		return false
	}

	if _, err := decideSigningKey("AAAA", "", "BBBB", false, false, fatal); err == nil {
		t.Fatal("expected mismatch error")
	}
}

func TestBlockedVersionIsNotInstalled(t *testing.T) {
	reg := newFakeRegistry(t)
	reg.publish(t, kindSkill, "lint", release{version: "1.0.0", blocked: true, files: map[string]string{"SKILL.md": skillDoc}})

	dir := t.TempDir()
	writeProject(t, dir, reg.host, "")
	err := testApp(reg, dir).applyProject(context.Background(), applyOptions{})
	if err == nil || !strings.Contains(err.Error(), "blocked by a Jev policy") {
		t.Fatalf("expected blocked version to fail apply, got %v", err)
	}

	if _, statErr := os.Stat(filepath.Join(dir, lockFileName)); !os.IsNotExist(statErr) {
		t.Fatalf("blocked version wrote a lock file: %v", statErr)
	}
}

func TestYankedSkippedUnlessAllowed(t *testing.T) {
	reg := newFakeRegistry(t)
	reg.publish(t, kindSkill, "lint",
		release{version: "1.0.0", files: map[string]string{"SKILL.md": skillDoc}},
		release{version: "1.1.0", yanked: true, files: map[string]string{"SKILL.md": skillDoc}},
	)
	reg.publish(t, kindAgent, "reviewer", release{version: "1.0.0", files: map[string]string{"reviewer.md": agentDoc}})

	dir := t.TempDir()
	writeProject(t, dir, reg.host, "")
	if err := testApp(reg, dir).applyProject(context.Background(), applyOptions{}); err != nil {
		t.Fatalf("apply: %v", err)
	}

	lock := readFile(t, filepath.Join(dir, lockFileName))
	if strings.Contains(lock, `"1.1.0"`) {
		t.Fatalf("yanked version was installed without -allow-yanked:\n%s", lock)
	}

	allowDir := t.TempDir()
	writeProject(t, allowDir, reg.host, "")
	if err := testApp(reg, allowDir).applyProject(context.Background(), applyOptions{allowYanked: true}); err != nil {
		t.Fatalf("apply with allow-yanked: %v", err)
	}

	if !strings.Contains(readFile(t, filepath.Join(allowDir, lockFileName)), `"1.1.0"`) {
		t.Fatal("expected -allow-yanked to install the yanked version")
	}
}

func TestPathValidationRejectsUnsafeLockPaths(t *testing.T) {
	for _, p := range []string{"", ".", "AGENTS.md", "../escape", "/abs/path"} {
		if safeRelPath(p) {
			t.Fatalf("safeRelPath(%q) = true, want false", p)
		}
	}

	if !safeRelPath(".github/skills/lint") {
		t.Fatal("expected a normal install path to be safe")
	}
}

func TestInitHTTPE2EInstallsSkillTargetsAndWritesLockMetadata(t *testing.T) {
	isolateHome(t)
	reg := newFakeRegistry(t)
	reg.publish(t, kindSkill, "lint", release{version: "1.0.0", files: map[string]string{
		"SKILL.md":  skillDoc,
		"rules.txt": "rule one\n",
	}})
	dir := t.TempDir()
	writeSkillProject(t, dir, reg.host, ">= 1.0.0, < 2.0.0")
	a := testApp(reg, dir)

	if err := a.applyProject(context.Background(), applyOptions{}); err != nil {
		t.Fatalf("apply: %v", err)
	}

	for _, p := range []string{
		".github/skills/lint/SKILL.md",
		".github/skills/lint/rules.txt",
		".claude/skills/lint/SKILL.md",
		".claude/skills/lint/rules.txt",
	} {
		if _, err := os.Stat(filepath.Join(dir, p)); err != nil {
			t.Fatalf("missing install target %s: %v", p, err)
		}
	}

	lock := readFile(t, filepath.Join(dir, lockFileName))
	for _, want := range []string{
		`signing_key`,
		fingerprintOf(reg.signer),
		`signature_hash = "sha256:`,
		`h1:`,
		`zh:`,
	} {
		if !strings.Contains(lock, want) {
			t.Fatalf("lock missing %q:\n%s", want, lock)
		}
	}
}

func TestInitHTTPE2ENoopRerunLeavesInstalledFilesAndLockUntouched(t *testing.T) {
	isolateHome(t)
	reg := newFakeRegistry(t)
	reg.publish(t, kindSkill, "lint", release{version: "1.0.0", files: map[string]string{
		"SKILL.md":  skillDoc,
		"rules.txt": "rule one\n",
	}})
	dir := t.TempDir()
	writeSkillProject(t, dir, reg.host, ">= 1.0.0, < 2.0.0")
	a := testApp(reg, dir)

	if err := a.applyProject(context.Background(), applyOptions{}); err != nil {
		t.Fatalf("first apply: %v", err)
	}

	paths := []string{
		filepath.Join(dir, ".github/skills/lint/SKILL.md"),
		filepath.Join(dir, ".github/skills/lint/rules.txt"),
		filepath.Join(dir, ".claude/skills/lint/SKILL.md"),
		filepath.Join(dir, ".claude/skills/lint/rules.txt"),
		filepath.Join(dir, lockFileName),
	}
	before := map[string]time.Time{}
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatalf("stat %s: %v", p, err)
		}

		before[p] = info.ModTime()
	}

	lock := readFile(t, filepath.Join(dir, lockFileName))
	time.Sleep(1100 * time.Millisecond)

	if err := a.applyProject(context.Background(), applyOptions{}); err != nil {
		t.Fatalf("second apply: %v", err)
	}

	if got := readFile(t, filepath.Join(dir, lockFileName)); got != lock {
		t.Fatalf("second apply changed the lock file:\n%s\n---\n%s", lock, got)
	}

	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatalf("stat after rerun %s: %v", p, err)
		}

		if !info.ModTime().Equal(before[p]) {
			t.Fatalf("expected %s to stay untouched, mtime %v -> %v", p, before[p], info.ModTime())
		}
	}
}

func TestInitHTTPE2EUpgradeMinorVersionRewritesLock(t *testing.T) {
	isolateHome(t)
	reg := newFakeRegistry(t)
	reg.publish(t, kindSkill, "lint", release{version: "1.0.0", files: map[string]string{
		"SKILL.md":  skillDoc,
		"rules.txt": "rule one\n",
	}})
	dir := t.TempDir()
	writeSkillProject(t, dir, reg.host, ">= 1.0.0, < 2.0.0")
	a := testApp(reg, dir)

	if err := a.applyProject(context.Background(), applyOptions{}); err != nil {
		t.Fatalf("first apply: %v", err)
	}

	before := readFile(t, filepath.Join(dir, lockFileName))
	reg.publish(t, kindSkill, "lint", release{version: "1.1.0", files: map[string]string{
		"SKILL.md": `---
name: lint
description: Lints things better
---
Lint body v1.1
`,
	}})

	if err := a.applyProject(context.Background(), applyOptions{upgrade: true}); err != nil {
		t.Fatalf("upgrade apply: %v", err)
	}

	after := readFile(t, filepath.Join(dir, lockFileName))
	if before == after {
		t.Fatal("expected -upgrade to rewrite the lock file")
	}

	if !strings.Contains(after, `version        = "1.1.0"`) || strings.Contains(after, `version        = "1.0.0"`) {
		t.Fatalf("lock did not move to 1.1.0:\n%s", after)
	}
}

func TestInitHTTPE2EUpgradeRemovesStaleFiles(t *testing.T) {
	isolateHome(t)
	reg := newFakeRegistry(t)
	reg.publish(t, kindSkill, "lint", release{version: "1.0.0", files: map[string]string{
		"SKILL.md":  skillDoc,
		"rules.txt": "rule one\n",
	}})
	dir := t.TempDir()
	writeSkillProject(t, dir, reg.host, ">= 1.0.0, < 2.0.0")
	a := testApp(reg, dir)

	if err := a.applyProject(context.Background(), applyOptions{}); err != nil {
		t.Fatalf("first apply: %v", err)
	}

	reg.publish(t, kindSkill, "lint", release{version: "1.1.0", files: map[string]string{
		"SKILL.md": `---
name: lint
description: Lints things better
---
Lint body v1.1
`,
	}})
	if err := a.applyProject(context.Background(), applyOptions{upgrade: true}); err != nil {
		t.Fatalf("upgrade apply: %v", err)
	}

	for _, p := range []string{
		".github/skills/lint/rules.txt",
		".claude/skills/lint/rules.txt",
	} {
		if _, err := os.Stat(filepath.Join(dir, p)); !os.IsNotExist(err) {
			t.Fatalf("expected stale file %s to be removed, stat err = %v", p, err)
		}
	}
}

func TestVerifyHTTPE2EPassesUntouchedInstall(t *testing.T) {
	isolateHome(t)
	reg := newFakeRegistry(t)
	reg.publish(t, kindSkill, "lint", release{version: "1.0.0", files: map[string]string{
		"SKILL.md":  skillDoc,
		"rules.txt": "rule one\n",
	}})
	dir := t.TempDir()
	writeSkillProject(t, dir, reg.host, ">= 1.0.0, < 2.0.0")
	a := testApp(reg, dir)

	if err := a.applyProject(context.Background(), applyOptions{}); err != nil {
		t.Fatalf("apply: %v", err)
	}

	if err := a.verifyProject(); err != nil {
		t.Fatalf("verify: %v", err)
	}
}

func TestInitAndVerifyHTTPE2ETamperingFailures(t *testing.T) {
	t.Run("archive zh mismatch", func(t *testing.T) {
		isolateHome(t)
		reg := newFakeRegistry(t)
		reg.publish(t, kindSkill, "lint", release{version: "1.0.0", files: map[string]string{
			"SKILL.md":  skillDoc,
			"rules.txt": "rule one\n",
		}})
		reg.files["lint-1.0.0.tar.gz"] = []byte("tampered bytes")
		dir := t.TempDir()
		writeSkillProject(t, dir, reg.host, ">= 1.0.0, < 2.0.0")

		err := testApp(reg, dir).applyProject(context.Background(), applyOptions{})
		if err == nil || !strings.Contains(err.Error(), "does not match signed SHA256SUMS") {
			t.Fatalf("expected zh mismatch, got %v", err)
		}

		assertNoWrites(t, dir)
	})

	t.Run("SHA256SUMS signature failure", func(t *testing.T) {
		isolateHome(t)
		reg := newFakeRegistry(t)
		reg.publish(t, kindSkill, "lint", release{version: "1.0.0", files: map[string]string{
			"SKILL.md":  skillDoc,
			"rules.txt": "rule one\n",
		}})
		reg.tamperSums()
		dir := t.TempDir()
		writeSkillProject(t, dir, reg.host, ">= 1.0.0, < 2.0.0")

		err := testApp(reg, dir).applyProject(context.Background(), applyOptions{})
		if err == nil || !strings.Contains(err.Error(), "does not verify") {
			t.Fatalf("expected signature failure, got %v", err)
		}

		assertNoWrites(t, dir)
	})

	t.Run("modified installed file fails verify", func(t *testing.T) {
		isolateHome(t)
		reg := newFakeRegistry(t)
		reg.publish(t, kindSkill, "lint", release{version: "1.0.0", files: map[string]string{
			"SKILL.md":  skillDoc,
			"rules.txt": "rule one\n",
		}})
		dir := t.TempDir()
		a := testApp(reg, dir)
		writeSkillProject(t, dir, reg.host, ">= 1.0.0, < 2.0.0")

		if err := a.applyProject(context.Background(), applyOptions{}); err != nil {
			t.Fatalf("apply: %v", err)
		}

		if err := os.WriteFile(filepath.Join(dir, ".claude/skills/lint/SKILL.md"), []byte("edited"), 0o644); err != nil {
			t.Fatal(err)
		}

		err := a.verifyProject()
		if err == nil || !strings.Contains(err.Error(), ".claude/skills/lint has been modified") {
			t.Fatalf("expected verify failure for modified install, got %v", err)
		}
	})
}

func TestInitHTTPE2ERejectsUnsupportedProtocolMajor(t *testing.T) {
	isolateHome(t)
	reg := newFakeRegistry(t)
	reg.publish(t, kindSkill, "lint", release{version: "1.0.0", protocols: []string{"2.0"}, files: map[string]string{
		"SKILL.md": skillDoc,
	}})
	dir := t.TempDir()
	writeSkillProject(t, dir, reg.host, ">= 1.0.0, < 2.0.0")

	err := testApp(reg, dir).applyProject(context.Background(), applyOptions{})
	if err == nil || !strings.Contains(err.Error(), `unsupported agents protocol "2.0"`) {
		t.Fatalf("expected unsupported protocol error, got %v", err)
	}
}
