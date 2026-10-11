package main

import (
	"bytes"
	"strings"
	"testing"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
)

func TestPrintAssessmentPlainWhenNotTerminal(t *testing.T) {
	var buf bytes.Buffer
	a := &app{out: &buf}
	risk := 0.4
	safe := 0.77
	injection := 0.67
	s := &stagedEntry{
		entry:      entry{source: "opendepot.localtest.me:8443/opendepot-system/code-review"},
		version:    "0.9.0",
		assessment: &opendepotv1alpha1.JevAssessment{RiskLevel: "Minimal", RiskScore: &risk, SafeProbability: &safe, InjectionProbability: &injection},
	}

	a.printAssessment(s)

	out := buf.String()
	if strings.Contains(out, "\x1b[") {
		t.Fatalf("expected no ANSI escapes for non-terminal output, got %q", out)
	}

	for _, want := range []string{"◆ opendepot.localtest.me:8443/opendepot-system/code-review@0.9.0", "Risk", "Minimal", "Safe", "0.77", "Injection", "0.67", "Exfiltration", "n/a", "⚠"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected output to contain %q, got:\n%s", want, out)
		}
	}
}

func TestPrintAssessmentShowsReviewAndBlockReasons(t *testing.T) {
	var buf bytes.Buffer
	a := &app{out: &buf}
	s := &stagedEntry{
		entry:   entry{source: "opendepot.localtest.me:8443/opendepot-system/code-review"},
		version: "0.9.0",
		assessment: &opendepotv1alpha1.JevAssessment{
			RiskLevel:    "High",
			NeedsReview:  true,
			Blocked:      true,
			BlockReasons: []string{"risk score 0.8 above 0.5"},
		},
	}

	a.printAssessment(s)

	out := buf.String()
	for _, want := range []string{"Review       needed", "Blocked      yes · risk score 0.8 above 0.5"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected output to contain %q, got:\n%s", want, out)
		}
	}
}

func TestTokensLineShowsInputTokensAndCost(t *testing.T) {
	st := newStyler(&bytes.Buffer{})

	if got := tokensLine(st, 0); got != "n/a" {
		t.Fatalf("tokensLine(0) = %q, want n/a", got)
	}

	got := tokensLine(st, 15034)
	for _, want := range []string{"15,034 input", "est. cost $0.000631"} {
		if !strings.Contains(got, want) {
			t.Errorf("tokensLine(15034) = %q, missing %q", got, want)
		}
	}
}

func TestGroupThousands(t *testing.T) {
	cases := map[int64]string{0: "0", 999: "999", 1000: "1,000", 1234567: "1,234,567"}
	for in, want := range cases {
		if got := groupThousands(in); got != want {
			t.Errorf("groupThousands(%d) = %q, want %q", in, got, want)
		}
	}
}
