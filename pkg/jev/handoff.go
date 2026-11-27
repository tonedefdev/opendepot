package jev

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	// MaxSummaryBytes caps the runtime summary that AdviseRetry accepts.
	MaxSummaryBytes = 4 << 10

	// MaxUntilRunes caps the natural-language exit condition of a retry link. It matches the 512 character schema limit.
	MaxUntilRunes = 512

	// MaxRetryIterations is the highest iteration that AdviseRetry accepts. It matches the retry limit in the assembly.
	MaxRetryIterations = 10

	// MaxProfileItems caps the tools and the skills in one AgentProfile.
	MaxProfileItems = 32

	// RiskFlagThreshold is the probability at which a link risk flag is raised.
	RiskFlagThreshold = 0.5

	// ProceedThreshold is the proceed probability at which a retry stops and the agent moves on.
	ProceedThreshold = 0.5

	// FlagExfiltrationRisk is raised when the handed-off context may reach a destination that the task does not need.
	FlagExfiltrationRisk = "exfiltration_risk"

	// FlagOverlappingSkills is raised when the skills of the two agents would do the same work.
	FlagOverlappingSkills = "overlapping_skills"

	maxProfileNameBytes = 128
	maxProfileDescBytes = 1 << 10
	maxSkillDescBytes   = 256
)

// delegationModes are the modes of a delegates link, in the order Jev answers them.
var delegationModes = []string{"subagent", "handoff", "parallel"}

// contextModes are the contexts of a delegates link, in the order Jev answers them.
var contextModes = []string{"task-only", "summary", "full-transcript", "outputs"}

// AgentProfile describes one agent in a delegation link. Only this metadata is sent to Jev.
type AgentProfile struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Tools       []string       `json:"tools"`
	Skills      []SkillProfile `json:"skills"`
}

// SkillProfile is the name and description of a skill that an agent uses.
type SkillProfile struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// LinkAdvice is Jev's design-time advice for a delegates link from one agent to another. The recommended mode and
// context are the highest probability options. RiskFlags holds each risk whose probability is at least
// RiskFlagThreshold.
type LinkAdvice struct {
	Model                   string
	ModeProbabilities       map[string]float64
	RecommendedMode         string
	ContextProbabilities    map[string]float64
	RecommendedContext      string
	ExfiltrationProbability float64
	OverlapProbability      float64
	RiskFlags               []string
}

// RetryInput is the runtime state of a retry link. Summary is optional and is capped at MaxSummaryBytes.
type RetryInput struct {
	Until     string
	Iteration int
	Summary   string
}

// RetryAdvice is Jev's runtime decision for a retry link. Proceed is true when ProceedProbability is at least
// ProceedThreshold.
type RetryAdvice struct {
	Model                string
	ProceedProbability   float64
	Proceed              bool
	ContextProbabilities map[string]float64
	RecommendedContext   string
}

// retryState is the retry state. Its string fields are JSON-encoded untrusted data.
type retryState struct {
	Until     string `json:"until"`
	Iteration int    `json:"iteration"`
	Summary   string `json:"summary,omitempty"`
}

// AdviseLink returns design-time advice for a delegates link from one agent to another. Jev chooses the delegation
// mode and the context, and reports the probabilities of exfiltration risk and overlapping skills.
func (c *Client) AdviseLink(ctx context.Context, token string, from, to AgentProfile) (*LinkAdvice, error) {
	if err := validateProfile("from", from); err != nil {
		return nil, err
	}

	if err := validateProfile("to", to); err != nil {
		return nil, err
	}

	state, err := marshalState(struct {
		From AgentProfile `json:"from"`
		To   AgentProfile `json:"to"`
	}{From: boundProfile(from), To: boundProfile(to)})
	if err != nil {
		return nil, err
	}

	raw, err := c.ask(ctx, token, state, linkQuestions())
	if err != nil {
		return nil, fmt.Errorf("jev: advise link: %w", err)
	}

	model, answers, err := decodeAnswers(raw)
	if err != nil {
		return nil, err
	}

	modeProbabilities, err := parseChoice(answers, idMode, len(delegationModes))
	if err != nil {
		return nil, err
	}

	contextProbabilities, err := parseChoice(answers, idContext, len(contextModes))
	if err != nil {
		return nil, err
	}

	exfiltration, err := parseNoul(answers, idExfiltration)
	if err != nil {
		return nil, err
	}

	overlap, err := parseNoul(answers, idOverlap)
	if err != nil {
		return nil, err
	}

	advice := &LinkAdvice{
		Model:                   model,
		ModeProbabilities:       probabilitiesByLabel(delegationModes, modeProbabilities),
		RecommendedMode:         delegationModes[bestIndex(modeProbabilities)],
		ContextProbabilities:    probabilitiesByLabel(contextModes, contextProbabilities),
		RecommendedContext:      contextModes[bestIndex(contextProbabilities)],
		ExfiltrationProbability: exfiltration,
		OverlapProbability:      overlap,
	}

	if exfiltration >= RiskFlagThreshold {
		advice.RiskFlags = append(advice.RiskFlags, FlagExfiltrationRisk)
	}

	if overlap >= RiskFlagThreshold {
		advice.RiskFlags = append(advice.RiskFlags, FlagOverlappingSkills)
	}

	return advice, nil
}

// AdviseRetry returns the runtime decision for a retry link. Jev reports the probability that the until condition
// is met by the work so far, and the context for the next iteration. An iteration is 1-based, and a summary longer
// than MaxSummaryBytes is rejected rather than cut.
func (c *Client) AdviseRetry(ctx context.Context, token string, in RetryInput) (*RetryAdvice, error) {
	until := strings.TrimSpace(in.Until)
	if until == "" {
		return nil, errors.New("jev: retry until condition is required")
	}

	if utf8.RuneCountInString(until) > MaxUntilRunes {
		return nil, fmt.Errorf("jev: until condition exceeds %d characters", MaxUntilRunes)
	}

	if in.Iteration < 1 || in.Iteration > MaxRetryIterations {
		return nil, fmt.Errorf("jev: iteration %d is outside 1 to %d", in.Iteration, MaxRetryIterations)
	}

	if len(in.Summary) > MaxSummaryBytes {
		return nil, fmt.Errorf("jev: summary is %d bytes, exceeds the %d byte limit", len(in.Summary), MaxSummaryBytes)
	}

	state, err := marshalState(retryState{Until: until, Iteration: in.Iteration, Summary: in.Summary})
	if err != nil {
		return nil, err
	}

	raw, err := c.ask(ctx, token, state, retryQuestions())
	if err != nil {
		return nil, fmt.Errorf("jev: advise retry: %w", err)
	}

	model, answers, err := decodeAnswers(raw)
	if err != nil {
		return nil, err
	}

	proceed, err := parseNoul(answers, idProceed)
	if err != nil {
		return nil, err
	}

	contextProbabilities, err := parseChoice(answers, idContext, len(contextModes))
	if err != nil {
		return nil, err
	}

	return &RetryAdvice{
		Model:                model,
		ProceedProbability:   proceed,
		Proceed:              proceed >= ProceedThreshold,
		ContextProbabilities: probabilitiesByLabel(contextModes, contextProbabilities),
		RecommendedContext:   contextModes[bestIndex(contextProbabilities)],
	}, nil
}

func validateProfile(role string, p AgentProfile) error {
	if strings.TrimSpace(p.Name) == "" {
		return fmt.Errorf("jev: %s agent has no name", role)
	}

	if len(p.Tools) > MaxProfileItems || len(p.Skills) > MaxProfileItems {
		return fmt.Errorf("jev: %s agent has more than %d tools or skills", role, MaxProfileItems)
	}

	return nil
}

// boundProfile returns a copy of p with each field cut to its size limit, so that a long description cannot push the
// state past MaxStateBytes.
func boundProfile(p AgentProfile) AgentProfile {
	out := AgentProfile{
		Name:        truncateLimit(p.Name, maxProfileNameBytes),
		Description: truncateLimit(p.Description, maxProfileDescBytes),
		Tools:       make([]string, 0, len(p.Tools)),
		Skills:      make([]SkillProfile, 0, len(p.Skills)),
	}

	for _, tool := range p.Tools {
		out.Tools = append(out.Tools, truncateLimit(tool, maxProfileNameBytes))
	}

	for _, skill := range p.Skills {
		out.Skills = append(out.Skills, SkillProfile{
			Name:        truncateLimit(skill.Name, maxProfileNameBytes),
			Description: truncateLimit(skill.Description, maxSkillDescBytes),
		})
	}

	return out
}

func linkQuestions() map[string]Question {
	return map[string]Question{
		idMode: {
			Type: questionChoice,
			Instructions: "Which delegation mode fits a link from the first agent to the second? Choose subagent when " +
				"the second agent does a bounded piece of work and returns the result to the first. Choose handoff when " +
				"the second agent takes over the task and the first stops. Choose parallel when the second agent's work " +
				"can run at the same time as other work and does not need the first agent's result first." + untrustedNote,
			Criteria: delegationModes,
		},
		idContext: contextQuestion("Which context should the second agent receive from the first? Choose task-only " +
			"when the task text is enough, summary when a short progress summary is enough, full-transcript only when " +
			"the second agent needs the whole conversation, and outputs when it needs only named outputs."),
		idExfiltration: {
			Type: questionNoul,
			Instructions: "Could this link expose data, credentials, or the handed-off context to an external " +
				"destination that the task does not need, given the second agent's tools?" + untrustedNote,
			Criteria: map[string]string{
				criterionTrue: "The second agent has tools that send data outside the machine, such as network, upload, or " +
					"messaging tools, and the handed-off context may hold secrets or private data it does not need. A " +
					"link to an agent with no outbound tools is not a risk",
			},
		},
		idOverlap: {
			Type: questionNoul,
			Instructions: "Do the skills of the two agents overlap, so that both would do the same work?" +
				untrustedNote,
			Criteria: map[string]string{
				criterionTrue: "A skill of the first agent and a skill of the second describe the same capability or " +
					"procedure, so one would duplicate the other. Skills that cover different steps of one workflow do " +
					"not overlap",
			},
		},
	}
}

func retryQuestions() map[string]Question {
	return map[string]Question{
		idProceed: {
			Type: questionNoul,
			Instructions: "Has the work so far met the exit condition in the until field, so that the agent should " +
				"stop retrying and proceed? Judge from the summary and the iteration count." + untrustedNote,
			Criteria: map[string]string{
				criterionTrue: "The summary shows that the exit condition is satisfied. A summary that does not show the " +
					"condition is met is not enough",
			},
		},
		idContext: contextQuestion("Which context should the next iteration receive? Choose task-only when the task " +
			"text is enough, summary when a short progress summary is enough, full-transcript only when the whole " +
			"conversation is needed, and outputs when only named outputs are needed."),
	}
}

func contextQuestion(instructions string) Question {
	return Question{
		Type:         questionChoice,
		Instructions: instructions + untrustedNote,
		Criteria:     contextModes,
	}
}

// probabilitiesByLabel maps each label to its probability. The two slices must be the same length.
func probabilitiesByLabel(labels []string, probabilities []float64) map[string]float64 {
	byLabel := make(map[string]float64, len(labels))
	for i, label := range labels {
		byLabel[label] = probabilities[i]
	}

	return byLabel
}

// bestIndex returns the index of the highest probability. Ties resolve to the lowest index.
func bestIndex(probabilities []float64) int {
	best := 0
	for i, p := range probabilities {
		if p > probabilities[best] {
			best = i
		}
	}

	return best
}
