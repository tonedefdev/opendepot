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
	"strings"
	"testing"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	"github.com/tonedefdev/opendepot/pkg/jev"
)

// jevAggTestPart returns a scored part with a risk level, score, safe probability and confidence.
func jevAggTestPart(label, level string, score, safe, confidence float64) jevPartResult {
	return jevPartResult{
		label:  label,
		status: jevPartScored,
		result: jev.Result{
			Model:           "model-" + label,
			RiskLevel:       level,
			RiskScore:       score,
			SafeProbability: safe,
			RiskConfidence:  confidence,
		},
	}
}

func TestJevAggregateWorstValues(t *testing.T) {
	parts := []jevPartResult{
		jevAggTestPart("1", "Low", 0.2, 0.9, 0.95),
		jevAggTestPart("2", "High", 0.6, 0.4, 0.7),
		jevAggTestPart("3", "Moderate", 0.8, 0.7, 0.5),
	}

	parts[0].result.PromptInjectionProbability = 0.1
	parts[2].result.PromptInjectionProbability = 0.3
	parts[1].result.DataExfiltrationProbability = 0.25

	got, assessed := aggregateJevParts(jevNormalizeParts(parts))
	if assessed != 3 {
		t.Fatalf("want 3 assessed parts, got %d", assessed)
	}

	if got.RiskLevel != "High" || got.Model != "model-2" {
		t.Fatalf("want High from model-2, got %s from %s", got.RiskLevel, got.Model)
	}

	if got.RiskScore != 0.8 {
		t.Fatalf("want max score 0.8, got %v", got.RiskScore)
	}

	if got.SafeProbability != 0.4 || got.RiskConfidence != 0.5 {
		t.Fatalf("want min safe 0.4 and min confidence 0.5, got %v and %v", got.SafeProbability, got.RiskConfidence)
	}

	if got.PromptInjectionProbability != 0.3 || got.DataExfiltrationProbability != 0.25 {
		t.Fatalf("want max probabilities, got injection %v exfiltration %v", got.PromptInjectionProbability, got.DataExfiltrationProbability)
	}
}

func TestJevAggregateWorstLevelTieGoesToHigherScore(t *testing.T) {
	parts := []jevPartResult{
		jevAggTestPart("1", "High", 0.5, 0.9, 0.9),
		jevAggTestPart("2", "High", 0.7, 0.9, 0.9),
	}

	got, _ := aggregateJevParts(parts)
	if got.Model != "model-2" || got.RiskScore != 0.7 {
		t.Fatalf("want model-2 with score 0.7, got %s with %v", got.Model, got.RiskScore)
	}
}

func TestJevAggregateSinglePartMatchesCall(t *testing.T) {
	single := jev.Result{
		Model:                         "solo",
		SafeProbability:               0.8,
		PromptInjectionProbability:    0.1,
		DataExfiltrationProbability:   0.2,
		DestructiveActionsProbability: 0.3,
		HiddenInstructionsProbability: 0.4,
		ScopeMismatchProbability:      0.5,
		RemoteExecutionProbability:    0.6,
		RiskScore:                     0.42,
		RiskLevel:                     "Moderate",
		RiskConfidence:                0.77,
	}

	got, assessed := aggregateJevParts([]jevPartResult{{label: "1", status: jevPartScored, result: single}})
	if assessed != 1 || got != single {
		t.Fatalf("single part must equal the call result, got %+v (assessed %d)", got, assessed)
	}
}

func TestJevAggregateUnknownLabelFailsPart(t *testing.T) {
	parts := jevNormalizeParts([]jevPartResult{
		jevAggTestPart("1", "Low", 0.1, 0.9, 0.9),
		jevAggTestPart("2", "Severe", 0.99, 0.01, 0.01),
	})

	if parts[1].status != jevPartFailed {
		t.Fatalf("unknown label must mark the part Failed, got %s", parts[1].status)
	}

	got, assessed := aggregateJevParts(parts)
	if assessed != 1 || got.RiskLevel != "Low" || got.RiskScore != 0.1 {
		t.Fatalf("failed part must not be aggregated, got %+v (assessed %d)", got, assessed)
	}
}

func TestJevAggregateNoScoredParts(t *testing.T) {
	got, assessed := aggregateJevParts([]jevPartResult{{label: "1", status: jevPartTokenLimitExceeded}})
	if assessed != 0 || got != (jev.Result{}) {
		t.Fatalf("want zero result with no scored parts, got %+v (assessed %d)", got, assessed)
	}
}

func TestJevAggregateLabelOrderNumeric(t *testing.T) {
	parts := []jevPartResult{
		jevAggTestPart("10", "High", 0.5, 0.9, 0.9),
		jevAggTestPart("2", "High", 0.5, 0.9, 0.9),
	}

	got, _ := aggregateJevParts(parts)
	if got.Model != "model-2" {
		t.Fatalf("ties must go to the lowest label 2, got %s", got.Model)
	}
}

func TestJevPartsGateCriticalBlocksAnyPosition(t *testing.T) {
	for _, pos := range []string{"first", "middle", "last"} {
		parts := []jevPartResult{
			jevAggTestPart("1", "Low", 0.1, 0.9, 0.9),
			jevAggTestPart("2", "Low", 0.1, 0.9, 0.9),
			jevAggTestPart("3", "Low", 0.1, 0.9, 0.9),
		}

		idx := map[string]int{"first": 0, "middle": 1, "last": 2}[pos]
		parts[idx].result.RiskLevel = "Critical"
		reasons, _ := evaluateJevPartsGate(nil, jevNormalizeParts(parts))
		want := "risk level Critical (chunk " + parts[idx].label + ") is always blocked"
		if len(reasons) != 1 || reasons[0] != want {
			t.Fatalf("%s: want %q, got %v", pos, want, reasons)
		}
	}
}

func TestJevPartsGateNamesWorstPart(t *testing.T) {
	parts := []jevPartResult{
		jevAggTestPart("1", "Low", 0.1, 0.9, 0.9),
		jevAggTestPart("2", "Low", 0.1, 0.2, 0.9),
		jevAggTestPart("3", "Low", 0.1, 0.2, 0.9),
	}

	minSafe := 0.5
	reasons, _ := evaluateJevPartsGate(&opendepotv1alpha1.JevPolicy{MinSafeProbability: &minSafe}, parts)
	if len(reasons) != 1 || !strings.Contains(reasons[0], "(chunk 2)") {
		t.Fatalf("want one reason naming chunk 2 (tie to lowest label), got %v", reasons)
	}

	if !strings.Contains(reasons[0], "safe probability 0.200") {
		t.Fatalf("reason must show the worst value, got %q", reasons[0])
	}
}

func TestJevPartsGateEachThresholdReason(t *testing.T) {
	parts := []jevPartResult{jevAggTestPart("1", "Low", 0.9, 0.1, 0.1)}
	parts[0].result.PromptInjectionProbability = 0.9
	parts[0].result.DataExfiltrationProbability = 0.9
	parts[0].result.DestructiveActionsProbability = 0.9
	parts[0].result.HiddenInstructionsProbability = 0.9
	parts[0].result.ScopeMismatchProbability = 0.9
	parts[0].result.RemoteExecutionProbability = 0.9
	half := 0.5
	policy := &opendepotv1alpha1.JevPolicy{
		MinSafeProbability:               &half,
		MaxInjectionProbability:          &half,
		MaxExfiltrationProbability:       &half,
		MaxDestructiveProbability:        &half,
		MaxHiddenInstructionsProbability: &half,
		MaxScopeMismatchProbability:      &half,
		MaxRemoteExecutionProbability:    &half,
		MaxRiskScore:                     &half,
	}

	reasons, _ := evaluateJevPartsGate(policy, parts)
	if len(reasons) != 8 {
		t.Fatalf("want 8 reasons, got %d: %v", len(reasons), reasons)
	}
}

func TestJevPartsGateMinConfidence(t *testing.T) {
	seventy := 0.7
	policy := &opendepotv1alpha1.JevPolicy{MinConfidence: &seventy}

	unset := []jevPartResult{jevAggTestPart("1", "Low", 0.1, 0.9, 0.1)}
	reasons, review := evaluateJevPartsGate(nil, unset)
	if len(reasons) != 0 || review {
		t.Fatalf("unset minConfidence must not block, got %v review=%v", reasons, review)
	}

	below := []jevPartResult{
		jevAggTestPart("1", "Low", 0.1, 0.9, 0.9),
		jevAggTestPart("2", "Low", 0.1, 0.9, 0.41),
	}

	reasons, review = evaluateJevPartsGate(policy, below)
	if len(reasons) != 1 || !review || !strings.Contains(reasons[0], "(chunk 2)") {
		t.Fatalf("one part below minConfidence must block and set needsReview, got %v review=%v", reasons, review)
	}

	equal := []jevPartResult{jevAggTestPart("1", "Low", 0.1, 0.9, 0.7)}
	reasons, review = evaluateJevPartsGate(policy, equal)
	if len(reasons) != 0 || review {
		t.Fatalf("equal to minConfidence must not block, got %v review=%v", reasons, review)
	}
}

func TestJevPartsGateSkipsFailedParts(t *testing.T) {
	parts := jevNormalizeParts([]jevPartResult{
		jevAggTestPart("1", "Low", 0.1, 0.9, 0.9),
		jevAggTestPart("2", "Unknown", 0.1, 0.1, 0.1),
	})

	reasons, _ := evaluateJevPartsGate(nil, parts)
	if len(reasons) != 0 {
		t.Fatalf("failed part must not be gated as a value, got %v", reasons)
	}
}
