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
	"fmt"
	"sort"
	"strconv"
	"strings"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	"github.com/tonedefdev/opendepot/pkg/jev"
)

// Outcomes of one Jev part, matching the Status enum of JevChunkResult.
const (
	jevPartScored             = "Scored"
	jevPartFailed             = "Failed"
	jevPartTokenLimitExceeded = "TokenLimitExceeded"
	jevPartNotSent            = "NotSent"
)

// jevPartResult pairs the Jev answer for one part with the part's label.
type jevPartResult struct {
	label  string
	status string
	result jev.Result
}

// jevRiskRanks orders the risk levels from least to most severe.
var jevRiskRanks = map[string]int{
	"minimal":  0,
	"low":      1,
	"moderate": 2,
	"high":     3,
	"critical": 4,
}

// jevRiskRank returns the rank of a risk level, or -1 when the level is not one of the known labels.
func jevRiskRank(level string) int {
	rank, ok := jevRiskRanks[strings.ToLower(level)]
	if !ok {
		return -1
	}

	return rank
}

// jevNormalizeParts marks a scored part as Failed when its risk level is not a known label. The result is a copy.
func jevNormalizeParts(parts []jevPartResult) []jevPartResult {
	out := make([]jevPartResult, len(parts))
	copy(out, parts)
	for i := range out {
		if out[i].status == jevPartScored && jevRiskRank(out[i].result.RiskLevel) < 0 {
			out[i].status = jevPartFailed
		}
	}

	return out
}

// jevScoredByLabel returns the Scored parts ordered by label.
func jevScoredByLabel(parts []jevPartResult) []jevPartResult {
	var scored []jevPartResult
	for _, p := range parts {
		if p.status == jevPartScored {
			scored = append(scored, p)
		}
	}

	sort.SliceStable(scored, func(i, j int) bool {
		return jevPartLabelLess(scored[i].label, scored[j].label)
	})

	return scored
}

// jevPartLabelLess orders labels by their numeric prefix, then by their suffix, so that 2 sorts before 10 and 1a
// sorts before 1b.
func jevPartLabelLess(a, b string) bool {
	na, sa := jevSplitLabel(a)
	nb, sb := jevSplitLabel(b)
	if na != nb {
		return na < nb
	}

	return sa < sb
}

// jevSplitLabel splits a label into its numeric prefix and the rest.
func jevSplitLabel(label string) (int, string) {
	i := 0
	for i < len(label) && label[i] >= '0' && label[i] <= '9' {
		i++
	}

	n, _ := strconv.Atoi(label[:i])
	return n, label[i:]
}

// jevPartMax returns the scored part with the highest value. Ties go to the lowest label, because scored is
// ordered by label and only a strictly higher value replaces the current part.
func jevPartMax(scored []jevPartResult, get func(jev.Result) float64) jevPartResult {
	w := scored[0]
	for _, p := range scored[1:] {
		if get(p.result) > get(w.result) {
			w = p
		}
	}

	return w
}

// jevPartMin returns the scored part with the lowest value. Ties go to the lowest label.
func jevPartMin(scored []jevPartResult, get func(jev.Result) float64) jevPartResult {
	w := scored[0]
	for _, p := range scored[1:] {
		if get(p.result) < get(w.result) {
			w = p
		}
	}

	return w
}

// aggregateJevParts combines the Scored parts into one result and returns how many parts were scored. The worst
// risk level and score give the level, score and Model. The six risk probabilities take the maximum, and the safe
// probability and risk confidence take the minimum. The parts must already be normalized. With no scored part, the
// result is zero and the count is 0.
func aggregateJevParts(parts []jevPartResult) (jev.Result, int) {
	scored := jevScoredByLabel(parts)
	if len(scored) == 0 {
		return jev.Result{}, 0
	}

	worst := scored[0]
	for _, p := range scored[1:] {
		wr := jevRiskRank(worst.result.RiskLevel)
		pr := jevRiskRank(p.result.RiskLevel)
		if pr > wr || (pr == wr && p.result.RiskScore > worst.result.RiskScore) {
			worst = p
		}
	}

	return jev.Result{
		Model:                         worst.result.Model,
		SafeProbability:               jevPartMin(scored, func(r jev.Result) float64 { return r.SafeProbability }).result.SafeProbability,
		PromptInjectionProbability:    jevPartMax(scored, func(r jev.Result) float64 { return r.PromptInjectionProbability }).result.PromptInjectionProbability,
		DataExfiltrationProbability:   jevPartMax(scored, func(r jev.Result) float64 { return r.DataExfiltrationProbability }).result.DataExfiltrationProbability,
		DestructiveActionsProbability: jevPartMax(scored, func(r jev.Result) float64 { return r.DestructiveActionsProbability }).result.DestructiveActionsProbability,
		HiddenInstructionsProbability: jevPartMax(scored, func(r jev.Result) float64 { return r.HiddenInstructionsProbability }).result.HiddenInstructionsProbability,
		ScopeMismatchProbability:      jevPartMax(scored, func(r jev.Result) float64 { return r.ScopeMismatchProbability }).result.ScopeMismatchProbability,
		RemoteExecutionProbability:    jevPartMax(scored, func(r jev.Result) float64 { return r.RemoteExecutionProbability }).result.RemoteExecutionProbability,
		RiskScore:                     jevPartMax(scored, func(r jev.Result) float64 { return r.RiskScore }).result.RiskScore,
		RiskLevel:                     worst.result.RiskLevel,
		RiskConfidence:                jevPartMin(scored, func(r jev.Result) float64 { return r.RiskConfidence }).result.RiskConfidence,
	}, len(scored)
}

// evaluateJevPartsGate applies the policy to each part and returns one reason per breached threshold. Each reason
// names the part with the worst value, and ties go to the lowest label. A Critical part always blocks. A minConfidence
// breach blocks and sets needsReview. The parts must already be normalized.
func evaluateJevPartsGate(policy *opendepotv1alpha1.JevPolicy, parts []jevPartResult) (reasons []string, needsReview bool) {
	scored := jevScoredByLabel(parts)
	if len(scored) == 0 {
		return nil, false
	}

	for _, p := range scored {
		if strings.EqualFold(p.result.RiskLevel, "Critical") {
			reasons = append(reasons, fmt.Sprintf("risk level Critical (chunk %s) is always blocked", p.label))
			break
		}
	}

	if policy == nil {
		return reasons, false
	}

	if policy.MinSafeProbability != nil {
		w := jevPartMin(scored, func(r jev.Result) float64 { return r.SafeProbability })
		if w.result.SafeProbability < *policy.MinSafeProbability {
			reasons = append(reasons, fmt.Sprintf("safe probability %.3f (chunk %s) is below minSafeProbability %.3f", w.result.SafeProbability, w.label, *policy.MinSafeProbability))
		}
	}

	if policy.MaxInjectionProbability != nil {
		w := jevPartMax(scored, func(r jev.Result) float64 { return r.PromptInjectionProbability })
		if w.result.PromptInjectionProbability > *policy.MaxInjectionProbability {
			reasons = append(reasons, fmt.Sprintf("prompt injection probability %.3f (chunk %s) exceeds maxInjectionProbability %.3f", w.result.PromptInjectionProbability, w.label, *policy.MaxInjectionProbability))
		}
	}

	if policy.MaxExfiltrationProbability != nil {
		w := jevPartMax(scored, func(r jev.Result) float64 { return r.DataExfiltrationProbability })
		if w.result.DataExfiltrationProbability > *policy.MaxExfiltrationProbability {
			reasons = append(reasons, fmt.Sprintf("data exfiltration probability %.3f (chunk %s) exceeds maxExfiltrationProbability %.3f", w.result.DataExfiltrationProbability, w.label, *policy.MaxExfiltrationProbability))
		}
	}

	if policy.MaxDestructiveProbability != nil {
		w := jevPartMax(scored, func(r jev.Result) float64 { return r.DestructiveActionsProbability })
		if w.result.DestructiveActionsProbability > *policy.MaxDestructiveProbability {
			reasons = append(reasons, fmt.Sprintf("destructive actions probability %.3f (chunk %s) exceeds maxDestructiveProbability %.3f", w.result.DestructiveActionsProbability, w.label, *policy.MaxDestructiveProbability))
		}
	}

	if policy.MaxHiddenInstructionsProbability != nil {
		w := jevPartMax(scored, func(r jev.Result) float64 { return r.HiddenInstructionsProbability })
		if w.result.HiddenInstructionsProbability > *policy.MaxHiddenInstructionsProbability {
			reasons = append(reasons, fmt.Sprintf("hidden instructions probability %.3f (chunk %s) exceeds maxHiddenInstructionsProbability %.3f", w.result.HiddenInstructionsProbability, w.label, *policy.MaxHiddenInstructionsProbability))
		}
	}

	if policy.MaxScopeMismatchProbability != nil {
		w := jevPartMax(scored, func(r jev.Result) float64 { return r.ScopeMismatchProbability })
		if w.result.ScopeMismatchProbability > *policy.MaxScopeMismatchProbability {
			reasons = append(reasons, fmt.Sprintf("scope mismatch probability %.3f (chunk %s) exceeds maxScopeMismatchProbability %.3f", w.result.ScopeMismatchProbability, w.label, *policy.MaxScopeMismatchProbability))
		}
	}

	if policy.MaxRemoteExecutionProbability != nil {
		w := jevPartMax(scored, func(r jev.Result) float64 { return r.RemoteExecutionProbability })
		if w.result.RemoteExecutionProbability > *policy.MaxRemoteExecutionProbability {
			reasons = append(reasons, fmt.Sprintf("remote execution probability %.3f (chunk %s) exceeds maxRemoteExecutionProbability %.3f", w.result.RemoteExecutionProbability, w.label, *policy.MaxRemoteExecutionProbability))
		}
	}

	if policy.MaxRiskScore != nil {
		w := jevPartMax(scored, func(r jev.Result) float64 { return r.RiskScore })
		if w.result.RiskScore > *policy.MaxRiskScore {
			reasons = append(reasons, fmt.Sprintf("risk score %.3f (chunk %s) exceeds maxRiskScore %.3f", w.result.RiskScore, w.label, *policy.MaxRiskScore))
		}
	}

	if policy.MinConfidence != nil {
		w := jevPartMin(scored, func(r jev.Result) float64 { return r.RiskConfidence })
		if w.result.RiskConfidence < *policy.MinConfidence {
			reasons = append(reasons, fmt.Sprintf("risk confidence %.3f (chunk %s) is below minConfidence %.3f", w.result.RiskConfidence, w.label, *policy.MinConfidence))
			needsReview = true
		}
	}

	return reasons, needsReview
}
