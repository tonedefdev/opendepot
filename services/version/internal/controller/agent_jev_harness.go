//go:build jevharness

package controller

import (
	"context"
	"fmt"
	"sort"
	"strings"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	"github.com/tonedefdev/opendepot/pkg/jev"
	"golang.org/x/sync/errgroup"
)

// HarnessPart is the outcome of one Jev call made by RunJevHarness.
type HarnessPart struct {
	Label            string
	Status           string
	Level            string
	RiskScore        float64
	SafeProbability  float64
	Confidence       float64
	InjectionProb    float64
	ExfiltrationProb float64
	DestructiveProb  float64
	HiddenProb       float64
	ScopeProb        float64
	RemoteExecProb   float64
	InputTokens      int64
	Files            int
	Error            string
}

// HarnessReport is the aggregate of one RunJevHarness call, mirroring what runAgentJev writes to status.
type HarnessReport struct {
	Parts           []HarnessPart
	ChunkCount      int
	Evidence        bool
	Level           string
	RiskScore       float64
	SafeProbability float64
	Confidence      float64
	InputTokens     int64
	Gate            []string
}

// RunJevHarness assesses an extracted Skill or Agent directory with Jev using the same chunking, evidence view,
// questions, composite weighting, and aggregation as runAgentJev. Trivy is not run, so flagged-file hints are
// absent; the caller must supply text-only content.
func RunJevHarness(ctx context.Context, dir, name, typ, token, endpoint string) (*HarnessReport, error) {
	version := &opendepotv1alpha1.Version{
		Spec: opendepotv1alpha1.VersionSpec{
			Type:           typ,
			AgentSourceRef: &opendepotv1alpha1.AgentSourceConfig{Name: &name},
		},
	}

	content, err := loadAgentContent(dir, version)
	if err != nil {
		return nil, err
	}

	kind := strings.ToLower(typ)
	in := &jevChunkInput{
		dir:          content.dir,
		entry:        content.entryRel,
		entryFlagged: content.entryFlagged,
		flagged:      content.flaggedFiles,
		files:        content.files,
		kind:         kind,
		name:         content.definition.Name,
		description:  content.definition.Description,
		tools:        content.definition.Tools,
	}

	layout, err := jevChunkPlan(in)
	if err != nil {
		return nil, fmt.Errorf("split: %w", err)
	}

	if layout.over {
		return nil, fmt.Errorf("content needs %d parts, above the limit of %d", layout.count, jevMaxChunks)
	}

	if layout.skippedCount > 0 {
		return nil, fmt.Errorf("%d bundled file(s) withheld or skipped", layout.skippedCount)
	}

	sent := layout.parts
	report := &HarnessReport{}
	if len(layout.parts) > 1 && len(layout.parts) < jevMaxChunks {
		evidence, found, err := jevEvidencePart(in)
		if err != nil {
			return nil, fmt.Errorf("evidence view: %w", err)
		}

		if found {
			sent = append(sent, evidence)
			report.Evidence = true
		}
	}

	run := &jevRun{
		client: jev.NewClient(jev.WithEndpoint(endpoint)),
		token:  token,
		kind:   kind,
		in:     in,
		total:  len(layout.parts),
	}
	run.calls.Store(int64(jevMaxChunks - len(sent)))

	sendCtx, cancel := context.WithTimeout(ctx, jevPartDeadline)
	defer cancel()

	var g errgroup.Group
	g.SetLimit(jevMaxInFlight)
	slots := make([][]jevPartOutcome, len(sent))
	for i := range sent {
		g.Go(func() error {
			slots[i] = run.assess(sendCtx, sent[i])

			return nil
		})
	}

	_ = g.Wait()

	var outcomes []jevPartOutcome
	for _, slot := range slots {
		outcomes = append(outcomes, slot...)
	}

	sort.SliceStable(outcomes, func(i, j int) bool {
		return jevPartLabelLess(outcomes[i].result.label, outcomes[j].result.label)
	})

	parts := make([]jevPartResult, len(outcomes))
	for i, o := range outcomes {
		parts[i] = o.result
	}

	normalized := jevApplyCompositeRisk(jevNormalizeParts(parts))
	for i := range outcomes {
		if outcomes[i].result.status == jevPartScored && normalized[i].status == jevPartFailed {
			outcomes[i].errText = "unrecognized risk level"
		}

		outcomes[i].result = normalized[i]
	}

	result, assessed := aggregateJevParts(normalized)
	gateReasons, _ := evaluateJevPartsGate(&opendepotv1alpha1.JevPolicy{}, normalized)

	report.ChunkCount = len(layout.parts)
	report.InputTokens = jevInputTokens(outcomes)
	report.Gate = gateReasons
	if assessed > 0 {
		report.Level = result.RiskLevel
		report.RiskScore = result.RiskScore
		report.SafeProbability = result.SafeProbability
		report.Confidence = result.RiskConfidence
	}

	for _, o := range outcomes {
		p := HarnessPart{
			Label:       o.result.label,
			Status:      o.result.status,
			Files:       len(o.files),
			Error:       o.errText,
			InputTokens: o.result.result.InputTokens,
		}

		if o.result.status == jevPartScored {
			r := o.result.result
			p.Level = r.RiskLevel
			p.RiskScore = r.RiskScore
			p.SafeProbability = r.SafeProbability
			p.Confidence = r.RiskConfidence
			p.InjectionProb = r.PromptInjectionProbability
			p.ExfiltrationProb = r.DataExfiltrationProbability
			p.DestructiveProb = r.DestructiveActionsProbability
			p.HiddenProb = r.HiddenInstructionsProbability
			p.ScopeProb = r.ScopeMismatchProbability
			p.RemoteExecProb = r.RemoteExecutionProbability
		}

		report.Parts = append(report.Parts, p)
	}

	return report, nil
}
