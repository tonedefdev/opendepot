//go:build jevharness

/*
Copyright 2026 Tony Owens.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
)

// wave7mHarnessDir writes files into a new directory for RunJevHarness.
func wave7mHarnessDir(t *testing.T, files map[string]string) string {
	t.Helper()

	dir := t.TempDir()
	for rel, body := range files {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	return dir
}

// wave7mRecordingStub returns a Jev stub that records every state it receives.
func wave7mRecordingStub(states *[]string, mu *sync.Mutex) jevStub {
	return func(state string) (int, string) {
		mu.Lock()
		defer mu.Unlock()

		*states = append(*states, state)

		return http.StatusOK, jevOKResponse
	}
}

func TestWave7MRunJevHarnessScoresSkill(t *testing.T) {
	endpoint, calls := newJevEndpoint(t, jevOK)
	dir := wave7mHarnessDir(t, map[string]string{"SKILL.md": jevEntry})

	report, err := RunJevHarness(context.Background(), dir, "agent", "Skill", jevSentinelToken, endpoint, nil)
	if err != nil {
		t.Fatalf("RunJevHarness: %v", err)
	}

	if calls.Load() != 1 {
		t.Fatalf("Jev calls = %d, want 1", calls.Load())
	}

	if report.ChunkCount != 1 || report.Evidence {
		t.Fatalf("ChunkCount = %d, Evidence = %t; want 1, false", report.ChunkCount, report.Evidence)
	}

	if report.Level == "" || report.RiskScore == 0 || report.SafeProbability == 0 || report.Confidence == 0 {
		t.Fatalf("aggregate not populated: %+v", report)
	}

	if report.InputTokens != 1234 {
		t.Fatalf("InputTokens = %d, want 1234", report.InputTokens)
	}

	if len(report.Parts) != 1 {
		t.Fatalf("Parts = %d, want 1", len(report.Parts))
	}

	part := report.Parts[0]
	if part.Status != jevPartScored || part.Model != "jev-1.13.0" || part.Files != 1 {
		t.Fatalf("part = %+v, want scored by jev-1.13.0 with one file", part)
	}

	if part.InjectionProb != 0.02 || part.RemoteExecProb != 0.06 {
		t.Fatalf("part probabilities = %+v", part)
	}

	if len(report.Gate) != 0 {
		t.Fatalf("Gate = %v with a nil policy, want none", report.Gate)
	}
}

func TestWave7MRunJevHarnessPolicyGatesResponse(t *testing.T) {
	endpoint, _ := newJevEndpoint(t, jevOK)
	dir := wave7mHarnessDir(t, map[string]string{"SKILL.md": jevEntry})

	strict := &opendepotv1alpha1.JevPolicy{MaxRiskScore: f64(0.1)}
	report, err := RunJevHarness(context.Background(), dir, "agent", "Skill", jevSentinelToken, endpoint, strict)
	if err != nil {
		t.Fatalf("RunJevHarness: %v", err)
	}

	if len(report.Gate) == 0 {
		t.Fatalf("Gate empty, want a block reason for the strict risk threshold")
	}
}

func TestWave7MRunJevHarnessPoliciesGatesEachPolicy(t *testing.T) {
	var (
		mu     sync.Mutex
		states []string
	)

	endpoint, calls := newJevEndpoint(t, wave7mRecordingStub(&states, &mu))
	dir := wave7mHarnessDir(t, map[string]string{"SKILL.md": jevEntry})

	lenient := &opendepotv1alpha1.JevPolicy{MaxRiskScore: f64(0.99)}
	strict := &opendepotv1alpha1.JevPolicy{MaxRiskScore: f64(0.1)}
	scan := "Static scanner findings (test): marker-wave7m-scan"

	report, err := RunJevHarnessPolicies(context.Background(), dir, "agent", "Skill", jevSentinelToken, endpoint, scan, []*opendepotv1alpha1.JevPolicy{lenient, strict})
	if err != nil {
		t.Fatalf("RunJevHarnessPolicies: %v", err)
	}

	if calls.Load() != 1 {
		t.Fatalf("Jev calls = %d, want one shared response for all policies", calls.Load())
	}

	if len(report.Gates) != 2 {
		t.Fatalf("Gates = %d, want 2", len(report.Gates))
	}

	if len(report.Gates[0]) != 0 || len(report.Gates[1]) == 0 {
		t.Fatalf("Gates = %v, want lenient to pass and strict to block", report.Gates)
	}

	if len(report.Gate) != len(report.Gates[0]) {
		t.Fatalf("Gate = %v does not mirror Gates[0] = %v", report.Gate, report.Gates[0])
	}

	mu.Lock()
	defer mu.Unlock()
	if len(states) != 1 || !strings.Contains(states[0], "marker-wave7m-scan") {
		t.Fatalf("scanner block was not sent in the Jev state: %q", states)
	}
}

func TestWave7MRunJevHarnessNoPoliciesReportsRawScores(t *testing.T) {
	endpoint, _ := newJevEndpoint(t, jevOK)
	dir := wave7mHarnessDir(t, map[string]string{"SKILL.md": jevEntry})

	report, err := RunJevHarnessPolicies(context.Background(), dir, "agent", "Skill", jevSentinelToken, endpoint, "", nil)
	if err != nil {
		t.Fatalf("RunJevHarnessPolicies: %v", err)
	}

	if len(report.Gates) != 0 || report.Gate != nil {
		t.Fatalf("Gates = %v, Gate = %v; want none without policies", report.Gates, report.Gate)
	}

	if report.Level == "" {
		t.Fatalf("Level empty, want the raw aggregate")
	}
}

func TestWave7MRunJevHarnessRecordsFailedPart(t *testing.T) {
	endpoint, _ := newJevEndpoint(t, func(string) (int, string) {
		return http.StatusInternalServerError, "upstream failure"
	})
	dir := wave7mHarnessDir(t, map[string]string{"SKILL.md": jevEntry})

	report, err := RunJevHarness(context.Background(), dir, "agent", "Skill", jevSentinelToken, endpoint, nil)
	if err != nil {
		t.Fatalf("RunJevHarness: %v", err)
	}

	if len(report.Parts) != 1 || report.Parts[0].Status != jevPartFailed || report.Parts[0].Error == "" {
		t.Fatalf("Parts = %+v, want one failed part with an error", report.Parts)
	}

	if report.Level != "" || report.RiskScore != 0 {
		t.Fatalf("aggregate = %+v, want empty when nothing was assessed", report)
	}
}

func TestWave7MRunJevHarnessMissingEntryFails(t *testing.T) {
	dir := wave7mHarnessDir(t, map[string]string{"notes.txt": "no entry file here"})

	_, err := RunJevHarness(context.Background(), dir, "agent", "Skill", jevSentinelToken, "http://127.0.0.1:1", nil)
	if err == nil {
		t.Fatal("RunJevHarness succeeded without an entry file")
	}
}

func TestWave7MRunJevHarnessRejectsSkippedBinary(t *testing.T) {
	dir := wave7mHarnessDir(t, map[string]string{"SKILL.md": jevEntry})
	if err := os.WriteFile(filepath.Join(dir, "payload.bin"), []byte{0x00, 0x01, 0x02, 0x03}, 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := RunJevHarness(context.Background(), dir, "agent", "Skill", jevSentinelToken, "http://127.0.0.1:1", nil)
	if err == nil || !strings.Contains(err.Error(), "withheld or skipped") {
		t.Fatalf("err = %v, want a withheld-or-skipped error", err)
	}
}

func TestWave7MRunJevHarnessRejectsOversizedContent(t *testing.T) {
	body := jevEntry + strings.Repeat("a", (jevMaxChunks+1)*jevChunkBodyBudget)
	dir := wave7mHarnessDir(t, map[string]string{"SKILL.md": body})

	_, err := RunJevHarness(context.Background(), dir, "agent", "Skill", jevSentinelToken, "http://127.0.0.1:1", nil)
	if err == nil || !strings.Contains(err.Error(), "above the limit") {
		t.Fatalf("err = %v, want a part-limit error", err)
	}
}

func TestWave7MScannerBlockFromReport(t *testing.T) {
	report := []byte(`{
  "execution_successful": true,
  "risk_assessment": {"recommendation": "block", "max_issue_severity": "high"},
  "issues": [
    {"id": "SS-001", "category": "exfiltration", "severity": "high", "location": {"file": "run.sh", "start_line": 7}}
  ]
}`)

	block, err := ScannerBlockFromReport(report)
	if err != nil {
		t.Fatalf("ScannerBlockFromReport: %v", err)
	}

	for _, want := range []string{"SS-001", "exfiltration", "run.sh:7", "Recommendation: block"} {
		if !strings.Contains(block, want) {
			t.Fatalf("block missing %q:\n%s", want, block)
		}
	}
}

func TestWave7MScannerBlockFromReportRejectsIncompleteScan(t *testing.T) {
	_, err := ScannerBlockFromReport([]byte(`{"execution_successful": false}`))
	if err == nil {
		t.Fatal("ScannerBlockFromReport accepted a scan that did not complete")
	}

	_, err = ScannerBlockFromReport([]byte(`not json`))
	if err == nil {
		t.Fatal("ScannerBlockFromReport accepted invalid JSON")
	}
}

func TestWave7MRunJevHarnessNilPolicyBlocksCriticalPart(t *testing.T) {
	const critical = `{
  "model": "jev-1.13.0",
  "answers": {
    "safe": {"type": "noul", "noul": 0.10},
    "prompt_injection": {"type": "noul", "noul": 0.10},
    "data_exfiltration": {"type": "noul", "noul": 0.10},
    "destructive_actions": {"type": "noul", "noul": 0.10},
    "hidden_instructions": {"type": "noul", "noul": 0.10},
    "scope_mismatch": {"type": "noul", "noul": 0.97},
    "remote_execution": {"type": "noul", "noul": 0.10},
    "risk": {
      "type": "score",
      "score": 4,
      "legend": {"0": "Minimal", "1": "Low", "2": "Moderate", "3": "High", "4": "Critical"},
      "probabilities": {"0": 0, "1": 0, "2": 0, "3": 0, "4": 1},
      "confidence": 0.9
    }
  },
  "usage": {"input_tokens": 10, "output_tokens": 1}
}`

	endpoint, _ := newJevEndpoint(t, func(string) (int, string) { return http.StatusOK, critical })
	dir := wave7mHarnessDir(t, map[string]string{"SKILL.md": jevEntry})

	report, err := RunJevHarness(context.Background(), dir, "agent", "Skill", jevSentinelToken, endpoint, nil)
	if err != nil {
		t.Fatalf("RunJevHarness: %v", err)
	}

	if len(report.Parts) != 1 || report.Parts[0].Level != "Critical" {
		t.Fatalf("parts = %+v, want one Critical part", report.Parts)
	}

	if len(report.Gate) != 1 || !strings.Contains(report.Gate[0], "rated Critical risk, which always blocks") {
		t.Fatalf("Gate = %v, want a Critical block reason with no policy set", report.Gate)
	}

	if !strings.Contains(report.Gate[0], "scope mismatch (0.97)") {
		t.Fatalf("Gate reason does not name the strongest signal: %v", report.Gate)
	}
}
