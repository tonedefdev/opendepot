package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestPrintBlockedVersionListsColumnsWithReasons(t *testing.T) {
	var buf bytes.Buffer
	e := &blockedVersionError{version: "1.0.0", reasons: []string{"part 1 is rated Critical risk, which always blocks; strongest signal is hidden instructions (0.62)"}}

	printBlockedVersion(&buf, "opendepot.localtest.me:8443/opendepot-system/anti-ui-slop", e)

	out := buf.String()
	if strings.Contains(out, "\x1b[") {
		t.Fatalf("expected no ANSI escapes for non-terminal output, got %q", out)
	}

	for _, box := range []string{"╭", "╰", "│", "├", "─"} {
		if strings.Contains(out, box) {
			t.Fatalf("output should not draw outlines, found %q:\n%s", box, out)
		}
	}

	for _, want := range []string{
		"✖ opendepot.localtest.me:8443/opendepot-system/anti-ui-slop@1.0.0",
		"  Status       blocked by a Jev policy · not installable",
		"  Reason       part 1 is rated Critical risk, which always blocks; strongest",
		"signal is hidden instructions (0.62)",
		"  Next         installs are refused until the policy allows this version",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestPrintBlockedVersionAlignsWrappedReasons(t *testing.T) {
	var buf bytes.Buffer
	reason := strings.Repeat("highest signal is hidden instructions at 0.62 ", 4)

	printBlockedVersion(&buf, "reg/ns/skill", &blockedVersionError{version: "1.0.0", reasons: []string{reason, "second reason"}})

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	valueColumn := strings.Index(lines[3], "highest")
	if valueColumn != 2+blockedLabelWidth+1 {
		t.Fatalf("reason value starts at column %d, want %d:\n%s", valueColumn, 2+blockedLabelWidth+1, buf.String())
	}

	var continuations int
	for _, line := range lines {
		if strings.HasPrefix(line, strings.Repeat(" ", valueColumn)) {
			continuations++
		}

		if n := utf8.RuneCountInString(line); n > valueColumn+blockedValueWidth {
			t.Fatalf("line is %d runes, wider than the value column allows: %q", n, line)
		}
	}

	if continuations == 0 {
		t.Fatalf("expected wrapped continuation lines aligned under the value column:\n%s", buf.String())
	}
}

func TestPrintBlockedVersionLockedHintAndNoReasons(t *testing.T) {
	var buf bytes.Buffer

	printBlockedVersion(&buf, "", &blockedVersionError{version: "2.0.0", locked: true})

	out := buf.String()
	for _, want := range []string{"✖ version 2.0.0", "none recorded", "the lock file pins this version", "re-run with --upgrade"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestPrintErrorUsesColumnsForBlockedSource(t *testing.T) {
	var buf bytes.Buffer
	err := &sourceError{
		source: "reg/ns/skill",
		err:    &blockedVersionError{version: "1.0.0", reasons: []string{"injection above limit"}},
	}

	printError(&buf, err)

	out := buf.String()
	if strings.HasPrefix(strings.TrimSpace(out), "error:") {
		t.Fatalf("blocked version should not use the plain error prefix: %q", out)
	}

	for _, want := range []string{"reg/ns/skill@1.0.0", "injection above limit"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestPrintErrorUsesColumnsForOtherFailures(t *testing.T) {
	var buf bytes.Buffer

	printError(&buf, errors.New("boom"))

	if got, want := buf.String(), "\n✖ Error\n  Error        boom\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestPrintErrorUsesSourceAndPreservesDetails(t *testing.T) {
	var buf bytes.Buffer
	source := "opendepot.localtest.me:8443/opendepot-system/anti-ui-slop"
	detail := "list versions of anti-ui-slop: GET https://opendepot.localtest.me:8443/opendepot/agents/v1/opendepot-system/skills/anti-ui-slop/versions: 401 Unauthorized"

	printError(&buf, &sourceError{source: source, err: errors.New(detail)})

	out := buf.String()
	for _, want := range []string{"✖ " + source, "  Error        list versions of anti-ui-slop: GET", "https://opendepot.localtest.me:8443/opendepot/agents/v1/opendepot-system/skills/anti-ui-slop/versions:", "401 Unauthorized"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}

	if strings.Contains(out, "\x1b[") {
		t.Fatalf("expected no ANSI escapes for non-terminal output, got %q", out)
	}
}

func TestPrintErrorKeepsWrappedAndMultilineContext(t *testing.T) {
	var buf bytes.Buffer
	err := fmt.Errorf("planning failed: %w", &sourceError{source: "reg/ns/skill", err: errors.New("first detail\nsecond detail")})

	printError(&buf, err)

	for _, want := range []string{"✖ Error", "planning failed: reg/ns/skill", "first detail", "second detail"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("output missing %q:\n%s", want, buf.String())
		}
	}
}

func TestPrintFallbackWarningListsSkippedCandidates(t *testing.T) {
	var buf bytes.Buffer
	skipped := []unavailableCandidate{
		{Version: "2.0.0", Status: statusBlocked, Reasons: []string{"cvss above threshold"}},
		{Commit: "3f2a9c1d0b7e4a5f6c8d9e0a1b2c3d4e5f607182", Status: statusRejected, Reasons: []string{"Superseded by a newer commit"}},
	}

	printFallbackWarning(&buf, "opendepot.localtest.me/acme/lint", "1.0.0", skipped)

	out := buf.String()
	if strings.Contains(out, "\x1b[") {
		t.Fatalf("expected no ANSI escapes for non-terminal output, got %q", out)
	}

	for _, want := range []string{
		"⚠ opendepot.localtest.me/acme/lint@1.0.0",
		"Skipped",
		"version 2.0.0 is blocked by a Jev policy",
		"- cvss above threshold",
		"commit 3f2a9c1d0b7e4a5f6c8d9e0a1b2c3d4e5f607182 is rejected; no",
		"version was assigned, so it is not compared with the constraint",
		"Next",
		"older version 1.0.0 installed because --allow-older-versions is",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("fallback warning missing %q:\n%s", want, out)
		}
	}
}

func TestAgentVersionsResponseDecodesUnavailableCandidates(t *testing.T) {
	body := `{
  "versions": [{"version": "1.0.0", "yanked": false, "blocked": false, "blockReasons": []}],
  "unavailableCandidates": [
    {"version": "1.2.0", "status": "blocked", "reasons": ["cvss above threshold"]},
    {"commit": "3f2a9c1d0b7e4a5f6c8d9e0a1b2c3d4e5f607182", "status": "rejected", "reasons": ["Superseded by a newer commit"]}
  ]
}`

	var resp agentVersionsResponse
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(resp.Versions) != 1 || len(resp.UnavailableCandidates) != 2 {
		t.Fatalf("unexpected response: %+v", resp)
	}

	if c := resp.UnavailableCandidates[0]; c.Version != "1.2.0" || c.Status != statusBlocked || c.Commit != "" {
		t.Fatalf("versioned candidate: %+v", c)
	}

	if c := resp.UnavailableCandidates[1]; c.Version != "" || c.Commit == "" || c.Status != statusRejected {
		t.Fatalf("unversioned candidate: %+v", c)
	}
}

func TestAgentVersionsResponseOldServerHasNoCandidates(t *testing.T) {
	var resp agentVersionsResponse
	if err := json.Unmarshal([]byte(`{"versions": [{"version": "1.0.0"}]}`), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(resp.UnavailableCandidates) != 0 {
		t.Fatalf("old server response produced candidates: %+v", resp.UnavailableCandidates)
	}
}
