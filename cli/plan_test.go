package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlanWritesNothing(t *testing.T) {
	isolateHome(t)
	reg := newFakeRegistry(t)
	publishStandard(t, reg)

	dir := t.TempDir()
	writeProject(t, dir, reg.host, "")
	var buf bytes.Buffer
	a := testApp(reg, dir)
	a.out = &buf

	if err := a.applyProject(context.Background(), applyOptions{dryRun: true, noPrompt: true}); err != nil {
		t.Fatalf("plan: %v", err)
	}

	assertNoWrites(t, dir)
	out := buf.String()
	if !strings.Contains(out, "reviewer") || !strings.Contains(out, "lint") {
		t.Fatalf("plan did not list the entries:\n%s", out)
	}

	if !strings.Contains(out, "No changes written") {
		t.Fatalf("plan did not state that nothing was written:\n%s", out)
	}
}

func TestPlanReportsNoChangesAfterInit(t *testing.T) {
	isolateHome(t)
	reg := newFakeRegistry(t)
	publishStandard(t, reg)

	dir := t.TempDir()
	writeProject(t, dir, reg.host, "")
	if err := testApp(reg, dir).applyProject(context.Background(), applyOptions{}); err != nil {
		t.Fatalf("init: %v", err)
	}

	var buf bytes.Buffer
	a := testApp(reg, dir)
	a.out = &buf
	if err := a.applyProject(context.Background(), applyOptions{dryRun: true, noPrompt: true}); err != nil {
		t.Fatalf("plan: %v", err)
	}

	if !strings.Contains(buf.String(), "No changes.") {
		t.Fatalf("expected no changes after init:\n%s", buf.String())
	}
}

func TestPlanShowsNewerRemoteVersionUntilUpgrade(t *testing.T) {
	isolateHome(t)
	reg := newFakeRegistry(t)
	reg.publish(t, kindSkill, "lint", release{version: "1.0.0", files: map[string]string{"SKILL.md": skillDoc}})

	dir := t.TempDir()
	writeSkillProject(t, dir, reg.host, ">= 1.0.0, < 2.0.0")
	if err := testApp(reg, dir).applyProject(context.Background(), applyOptions{}); err != nil {
		t.Fatalf("init: %v", err)
	}

	before := readFile(t, filepath.Join(dir, lockFileName))
	reg.publish(t, kindSkill, "lint", release{version: "1.1.0", files: map[string]string{"SKILL.md": skillDoc}})

	var buf bytes.Buffer
	a := testApp(reg, dir)
	a.out = &buf
	if err := a.applyProject(context.Background(), applyOptions{dryRun: true, noPrompt: true}); err != nil {
		t.Fatalf("plan: %v", err)
	}

	if !strings.Contains(buf.String(), "1.1.0 is available (locked 1.0.0)") {
		t.Fatalf("plan did not report the newer version:\n%s", buf.String())
	}

	if after := readFile(t, filepath.Join(dir, lockFileName)); after != before {
		t.Fatal("plan rewrote the lock file")
	}

	buf.Reset()
	if err := a.applyProject(context.Background(), applyOptions{dryRun: true, noPrompt: true, upgrade: true}); err != nil {
		t.Fatalf("plan upgrade: %v", err)
	}

	if !strings.Contains(buf.String(), "1.0.0 → 1.1.0") {
		t.Fatalf("upgrade plan did not show the version change:\n%s", buf.String())
	}

	if after := readFile(t, filepath.Join(dir, lockFileName)); after != before {
		t.Fatal("upgrade plan rewrote the lock file")
	}
}

func TestPlanFlagsLocallyEditedFileWithoutOverwriting(t *testing.T) {
	isolateHome(t)
	reg := newFakeRegistry(t)
	publishStandard(t, reg)

	dir := t.TempDir()
	writeProject(t, dir, reg.host, "")
	if err := testApp(reg, dir).applyProject(context.Background(), applyOptions{}); err != nil {
		t.Fatalf("init: %v", err)
	}

	lock, err := loadLock(filepath.Join(dir, lockFileName))
	if err != nil {
		t.Fatal(err)
	}

	var copilot string
	for _, in := range lock.Skills[0].Installed {
		if in.Target == "copilot" {
			copilot = in.Path
		}
	}

	installed := filepath.Join(dir, copilot, "SKILL.md")
	const edited = "---\nname: lint\ndescription: Hand edited\n---\nHand edit\n"
	if err := os.WriteFile(installed, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	a := testApp(reg, dir)
	a.out = &buf
	if err := a.applyProject(context.Background(), applyOptions{dryRun: true, noPrompt: true}); err != nil {
		t.Fatalf("plan: %v", err)
	}

	if !strings.Contains(buf.String(), "edited locally; will be overwritten") {
		t.Fatalf("plan did not flag the hand-edited file:\n%s", buf.String())
	}

	if got := readFile(t, installed); got != edited {
		t.Fatalf("plan overwrote the hand-edited file:\n%s", got)
	}
}

func TestPlanReportsStaleRemovals(t *testing.T) {
	isolateHome(t)
	reg := newFakeRegistry(t)
	publishStandard(t, reg)

	dir := t.TempDir()
	writeProject(t, dir, reg.host, "")
	if err := testApp(reg, dir).applyProject(context.Background(), applyOptions{}); err != nil {
		t.Fatalf("init: %v", err)
	}

	writeSkillProject(t, dir, reg.host, ">= 1.0.0")
	var buf bytes.Buffer
	a := testApp(reg, dir)
	a.out = &buf
	if err := a.applyProject(context.Background(), applyOptions{dryRun: true, noPrompt: true}); err != nil {
		t.Fatalf("plan: %v", err)
	}

	if !strings.Contains(buf.String(), "no longer managed by opendepot.hcl") {
		t.Fatalf("plan did not report the removed entry:\n%s", buf.String())
	}

	if _, err := os.Stat(filepath.Join(dir, ".github", "agents")); err != nil {
		t.Fatalf("plan removed the agent install: %v", err)
	}
}
