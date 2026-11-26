package jev

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"
)

const (
	modeSubagent          = "subagent"
	modeHandoff           = "handoff"
	modeParallel          = "parallel"
	contextTaskOnly       = "task-only"
	contextSummary        = "summary"
	contextFullTranscript = "full-transcript"
	testUntil             = "done"
)

func linkProfiles() (AgentProfile, AgentProfile) {
	from := AgentProfile{
		Name:        "planner",
		Description: "plans the release",
		Tools:       []string{"read", "run"},
		Skills:      []SkillProfile{{Name: "release-notes", Description: "writes release notes"}},
	}

	to := AgentProfile{
		Name:        "writer",
		Description: "writes docs",
		Tools:       []string{"read", "write"},
		Skills:      []SkillProfile{{Name: "docs", Description: "writes docs pages"}},
	}

	return from, to
}

// linkAnswers answers the link question set with the given mode, context, and risk probabilities.
func linkAnswers(mode, contexts []float64, exfiltration, overlap float64) answerFunc {
	return func(_ *testing.T, _ jevRequest) (int, map[string]any) {
		return http.StatusOK, map[string]any{
			idMode:         choiceAnswer(mode...),
			idContext:      choiceAnswer(contexts...),
			idExfiltration: noulAnswer(exfiltration),
			idOverlap:      noulAnswer(overlap),
		}
	}
}

func TestAdviseLinkRecommendsModeAndContext(t *testing.T) {
	from, to := linkProfiles()
	c, fake := newFakeJev(t, linkAnswers([]float64{0.1, 0.2, 0.7}, []float64{0.1, 0.6, 0.2, 0.1}, 0.2, 0.1))

	got, err := c.AdviseLink(context.Background(), sentinelToken, from, to)
	if err != nil {
		t.Fatalf("AdviseLink() error = %v", err)
	}

	if got.Model != testModel {
		t.Errorf("Model = %q, want %q", got.Model, testModel)
	}

	if got.RecommendedMode != modeParallel {
		t.Errorf("RecommendedMode = %q, want %s", got.RecommendedMode, modeParallel)
	}

	if len(got.ModeProbabilities) != 3 || got.ModeProbabilities[modeParallel] != 0.7 ||
		got.ModeProbabilities[modeSubagent] != 0.1 {
		t.Errorf("ModeProbabilities = %v", got.ModeProbabilities)
	}

	if got.RecommendedContext != contextSummary {
		t.Errorf("RecommendedContext = %q, want %s", got.RecommendedContext, contextSummary)
	}

	if len(got.ContextProbabilities) != 4 || got.ContextProbabilities[contextFullTranscript] != 0.2 {
		t.Errorf("ContextProbabilities = %v", got.ContextProbabilities)
	}

	if got.RiskFlags != nil {
		t.Errorf("RiskFlags = %v, want none", got.RiskFlags)
	}

	reqs := fake.requests()
	if len(reqs) != 1 {
		t.Fatalf("requests = %d, want 1 (one call per link)", len(reqs))
	}

	wantModes := []any{modeSubagent, modeHandoff, modeParallel}
	if criteria := reqs[0].Questions[idMode].Criteria.([]any); !slices.Equal(criteria, wantModes) {
		t.Errorf("mode criteria = %v", criteria)
	}

	state := stateObject(t, reqs[0].State)
	if state["from"].(map[string]any)["name"] != "planner" || state["to"].(map[string]any)["name"] != "writer" {
		t.Errorf("state = %s, want both agents named", reqs[0].State)
	}
}

func TestAdviseLinkRiskFlags(t *testing.T) {
	from, to := linkProfiles()

	tests := []struct {
		name         string
		exfiltration float64
		overlap      float64
		wantFlags    []string
	}{
		{name: "both at or above threshold", exfiltration: 0.8, overlap: 0.6,
			wantFlags: []string{FlagExfiltrationRisk, FlagOverlappingSkills}},
		{name: "exfiltration at threshold", exfiltration: 0.5, overlap: 0.49,
			wantFlags: []string{FlagExfiltrationRisk}},
		{name: "overlap at threshold", exfiltration: 0.49, overlap: 0.5,
			wantFlags: []string{FlagOverlappingSkills}},
		{name: "both below threshold", exfiltration: 0.1, overlap: 0.1, wantFlags: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := newFakeJev(t, linkAnswers([]float64{0.4, 0.3, 0.3}, []float64{0.25, 0.25, 0.25, 0.25},
				tt.exfiltration, tt.overlap))

			got, err := c.AdviseLink(context.Background(), sentinelToken, from, to)
			if err != nil {
				t.Fatalf("AdviseLink() error = %v", err)
			}

			if !slices.Equal(got.RiskFlags, tt.wantFlags) {
				t.Errorf("RiskFlags = %v, want %v", got.RiskFlags, tt.wantFlags)
			}

			if got.ExfiltrationProbability != tt.exfiltration || got.OverlapProbability != tt.overlap {
				t.Errorf("probabilities = %v, %v, want %v, %v", got.ExfiltrationProbability,
					got.OverlapProbability, tt.exfiltration, tt.overlap)
			}
		})
	}
}

func TestAdviseLinkTiesResolveToLowestIndex(t *testing.T) {
	from, to := linkProfiles()
	c, _ := newFakeJev(t, linkAnswers([]float64{0.3, 0.3, 0.3}, []float64{0.25, 0.25, 0.25, 0.25}, 0, 0))

	got, err := c.AdviseLink(context.Background(), sentinelToken, from, to)
	if err != nil {
		t.Fatalf("AdviseLink() error = %v", err)
	}

	if got.RecommendedMode != modeSubagent || got.RecommendedContext != contextTaskOnly {
		t.Errorf("recommended = %q, %q; want %s, %s", got.RecommendedMode, got.RecommendedContext,
			modeSubagent, contextTaskOnly)
	}
}

func TestAdviseLinkValidation(t *testing.T) {
	from, to := linkProfiles()

	tooManyTools := from
	tooManyTools.Tools = make([]string, MaxProfileItems+1)

	noName := to
	noName.Name = " "

	tests := []struct {
		name     string
		from, to AgentProfile
		token    string
	}{
		{name: "from has no name", from: AgentProfile{Name: ""}, to: to, token: sentinelToken},
		{name: "to has no name", from: from, to: noName, token: sentinelToken},
		{name: "too many tools", from: tooManyTools, to: to, token: sentinelToken},
		{name: "missing token", from: from, to: to, token: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, fake := newFakeJev(t, linkAnswers([]float64{1, 0, 0}, []float64{1, 0, 0, 0}, 0, 0))

			if _, err := c.AdviseLink(context.Background(), tt.token, tt.from, tt.to); err == nil {
				t.Fatal("AdviseLink() error = nil, want error")
			}

			if n := len(fake.requests()); n != 0 {
				t.Errorf("requests = %d, want 0", n)
			}
		})
	}
}

func TestAdviseLinkTruncatesProfileText(t *testing.T) {
	from, to := linkProfiles()
	from.Description = strings.Repeat("p", 5000)
	from.Skills = []SkillProfile{{Name: "s", Description: strings.Repeat("k", 5000)}}

	c, fake := newFakeJev(t, linkAnswers([]float64{1, 0, 0}, []float64{1, 0, 0, 0}, 0, 0))

	if _, err := c.AdviseLink(context.Background(), sentinelToken, from, to); err != nil {
		t.Fatalf("AdviseLink() error = %v", err)
	}

	state := stateObject(t, fake.requests()[0].State)
	gotFrom := state["from"].(map[string]any)

	if got := len(gotFrom["description"].(string)); got != maxProfileDescBytes {
		t.Errorf("description = %d bytes, want %d", got, maxProfileDescBytes)
	}

	skill := gotFrom["skills"].([]any)[0].(map[string]any)
	if got := len(skill["description"].(string)); got != maxSkillDescBytes {
		t.Errorf("skill description = %d bytes, want %d", got, maxSkillDescBytes)
	}
}

func TestAdviseLinkAPIErrorIsRetriedThenReturned(t *testing.T) {
	from, to := linkProfiles()
	c, fake := newFakeJev(t, func(_ *testing.T, _ jevRequest) (int, map[string]any) {
		return http.StatusServiceUnavailable, nil
	})

	_, err := c.AdviseLink(context.Background(), sentinelToken, from, to)

	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("AdviseLink() error = %v, want APIError 503", err)
	}

	if n := len(fake.requests()); n != maxRetries+1 {
		t.Errorf("requests = %d, want %d", n, maxRetries+1)
	}
}

func TestAdviseLinkMissingAnswer(t *testing.T) {
	from, to := linkProfiles()
	c, _ := newFakeJev(t, func(_ *testing.T, _ jevRequest) (int, map[string]any) {
		return http.StatusOK, map[string]any{
			idMode:         choiceAnswer(0.4, 0.3, 0.3),
			idContext:      choiceAnswer(0.25, 0.25, 0.25, 0.25),
			idExfiltration: noulAnswer(0),
		}
	})

	if _, err := c.AdviseLink(context.Background(), sentinelToken, from, to); err == nil ||
		!strings.Contains(err.Error(), idOverlap) {
		t.Fatalf("AdviseLink() error = %v, want missing %q answer", err, idOverlap)
	}
}

func retryInput(summary string) RetryInput {
	return RetryInput{Until: "the tests pass", Iteration: 2, Summary: summary}
}

func retryAnswers(proceed float64, contexts []float64) answerFunc {
	return func(_ *testing.T, _ jevRequest) (int, map[string]any) {
		return http.StatusOK, map[string]any{
			idProceed: noulAnswer(proceed),
			idContext: choiceAnswer(contexts...),
		}
	}
}

func TestAdviseRetryProceedProbability(t *testing.T) {
	tests := []struct {
		name        string
		probability float64
		wantProceed bool
	}{
		{name: "condition met", probability: 0.8, wantProceed: true},
		{name: "at threshold", probability: ProceedThreshold, wantProceed: true},
		{name: "condition not met", probability: 0.3, wantProceed: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := newFakeJev(t, retryAnswers(tt.probability, []float64{0.1, 0.2, 0.6, 0.1}))

			got, err := c.AdviseRetry(context.Background(), sentinelToken, retryInput("tests are green"))
			if err != nil {
				t.Fatalf("AdviseRetry() error = %v", err)
			}

			if got.Proceed != tt.wantProceed || got.ProceedProbability != tt.probability {
				t.Errorf("Proceed = %v, ProceedProbability = %v; want %v, %v",
					got.Proceed, got.ProceedProbability, tt.wantProceed, tt.probability)
			}

			if got.Model != testModel {
				t.Errorf("Model = %q, want %q", got.Model, testModel)
			}

			if got.RecommendedContext != contextFullTranscript {
				t.Errorf("RecommendedContext = %q, want %s", got.RecommendedContext, contextFullTranscript)
			}
		})
	}
}

func TestAdviseRetrySendsUntilIterationAndSummary(t *testing.T) {
	c, fake := newFakeJev(t, retryAnswers(0.9, []float64{1, 0, 0, 0}))

	if _, err := c.AdviseRetry(context.Background(), sentinelToken, retryInput("all green")); err != nil {
		t.Fatalf("AdviseRetry() error = %v", err)
	}

	reqs := fake.requests()
	if len(reqs) != 1 {
		t.Fatalf("requests = %d, want 1", len(reqs))
	}

	state := stateObject(t, reqs[0].State)
	if state["until"] != "the tests pass" || state["iteration"] != float64(2) || state["summary"] != "all green" {
		t.Errorf("state = %s", reqs[0].State)
	}

	if _, ok := reqs[0].Questions[idProceed]; !ok {
		t.Errorf("questions = %v, want %q", reqs[0].Questions, idProceed)
	}
}

func TestAdviseRetryValidation(t *testing.T) {
	tests := []struct {
		name  string
		in    RetryInput
		token string
	}{
		{name: "empty until", in: RetryInput{Until: " ", Iteration: 1}, token: sentinelToken},
		{name: "until over 512 characters", in: RetryInput{Until: strings.Repeat("é", MaxUntilRunes+1), Iteration: 1},
			token: sentinelToken},
		{name: "iteration zero", in: RetryInput{Until: testUntil, Iteration: 0}, token: sentinelToken},
		{name: "iteration over the limit", in: RetryInput{Until: testUntil, Iteration: MaxRetryIterations + 1},
			token: sentinelToken},
		{name: "summary over 4 KiB", in: RetryInput{Until: testUntil, Iteration: 1,
			Summary: strings.Repeat("s", MaxSummaryBytes+1)}, token: sentinelToken},
		{name: "missing token", in: RetryInput{Until: testUntil, Iteration: 1}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, fake := newFakeJev(t, retryAnswers(0.5, []float64{1, 0, 0, 0}))

			if _, err := c.AdviseRetry(context.Background(), tt.token, tt.in); err == nil {
				t.Fatal("AdviseRetry() error = nil, want error")
			}

			if n := len(fake.requests()); n != 0 {
				t.Errorf("requests = %d, want 0", n)
			}
		})
	}
}

func TestAdviseRetryAcceptsSummaryAtCap(t *testing.T) {
	c, fake := newFakeJev(t, retryAnswers(0.5, []float64{1, 0, 0, 0}))

	in := RetryInput{Until: testUntil, Iteration: MaxRetryIterations, Summary: strings.Repeat("s", MaxSummaryBytes)}
	if _, err := c.AdviseRetry(context.Background(), sentinelToken, in); err != nil {
		t.Fatalf("AdviseRetry() error = %v", err)
	}

	if n := len(fake.requests()); n != 1 {
		t.Errorf("requests = %d, want 1", n)
	}
}

func TestAdviseRetryAPIErrorIsNotRetried(t *testing.T) {
	c, fake := newFakeJev(t, func(_ *testing.T, _ jevRequest) (int, map[string]any) {
		return http.StatusUnauthorized, nil
	})

	_, err := c.AdviseRetry(context.Background(), sentinelToken, retryInput("summary"))

	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusUnauthorized {
		t.Fatalf("AdviseRetry() error = %v, want APIError 401", err)
	}

	if n := len(fake.requests()); n != 1 {
		t.Errorf("requests = %d, want 1", n)
	}
}

func TestAdviseRetryRejectsMalformedAnswer(t *testing.T) {
	c, _ := newFakeJev(t, func(_ *testing.T, _ jevRequest) (int, map[string]any) {
		return http.StatusOK, map[string]any{idProceed: choiceAnswer(0.5, 0.5), idContext: choiceAnswer(1, 0, 0, 0)}
	})

	if _, err := c.AdviseRetry(context.Background(), sentinelToken, retryInput("")); err == nil {
		t.Fatal("AdviseRetry() error = nil, want error for a choice answer where noul was expected")
	}
}
