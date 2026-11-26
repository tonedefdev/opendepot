package agentassembly

import (
	"fmt"
	"regexp"
	"slices"
	"unicode/utf8"
)

var (
	validKinds   = []string{KindAgent, KindSkill, KindMCPServer}
	validLinks   = []string{LinkUses, LinkCalls, LinkDelegates, LinkRetry}
	validModes   = []string{ModeSubagent, ModeHandoff, ModeParallel}
	validContext = []string{ContextTaskOnly, ContextSummary, ContextFullTranscript, ContextOutputs}
	validTargets = []string{TargetCopilot, TargetClaude, TargetAgentsMD}
	validDecided = []string{DecidedByJev, DecidedByUser}

	// refNamePattern bounds the namespace and name of a published resource so they cannot carry separators, quotes,
	// or control characters into a rendered file.
	refNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$`)
	// refVersionPattern admits exact versions and the constraints that Version may hold before export. Commas, '#',
	// and control characters are excluded. Export still requires an exact version.
	refVersionPattern = regexp.MustCompile(`^[0-9A-Za-z.+<>=~^!* -]{0,64}$`)
	toolNamePattern   = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,128}$`)
)

// checkStructure rejects documents that are malformed. It runs before the rules, so the rules can assume that every
// ID resolves, every enum is valid, and every field is used by the link type that owns it.
func checkStructure(doc Document) error {
	if !slices.Contains(validTargets, doc.Target) {
		return structural("target %q is not one of copilot, claude, agents-md", doc.Target)
	}

	nodes := make(map[string]bool, len(doc.Nodes))
	for _, n := range doc.Nodes {
		if err := checkNode(n, nodes); err != nil {
			return err
		}

		nodes[n.ID] = true
	}

	links := make(map[string]bool, len(doc.Links))
	for _, l := range doc.Links {
		if err := checkLink(l, nodes, links); err != nil {
			return err
		}

		links[l.ID] = true
	}

	return nil
}

func checkNode(n Node, seen map[string]bool) error {
	if !nodeIDPattern.MatchString(n.ID) {
		return structural("node id %q must match %s", n.ID, nodeIDPattern.String())
	}

	if seen[n.ID] {
		return structural("node id %q is duplicated", n.ID)
	}

	if !slices.Contains(validKinds, n.Kind) {
		return structural("node %q has unknown kind %q", n.ID, n.Kind)
	}

	if n.Ref.Namespace == "" || n.Ref.Name == "" {
		return structural("node %q needs ref namespace and name", n.ID)
	}

	if !refNamePattern.MatchString(n.Ref.Namespace) || !refNamePattern.MatchString(n.Ref.Name) {
		return structural("node %q ref namespace and name must match %s", n.ID, refNamePattern.String())
	}

	if !refVersionPattern.MatchString(n.Ref.Version) {
		return structural("node %q ref version %q contains characters that are not allowed", n.ID, n.Ref.Version)
	}

	return nil
}

func checkLink(l Link, nodes, seen map[string]bool) error {
	if l.ID == "" {
		return structural("link id is required")
	}

	if seen[l.ID] {
		return structural("link id %q is duplicated", l.ID)
	}

	if !nodes[l.From] || !nodes[l.To] {
		return structural("link %q references an unknown node", l.ID)
	}

	if !slices.Contains(validLinks, l.Type) {
		return structural("link %q has unknown type %q", l.ID, l.Type)
	}

	if err := checkLinkFields(l); err != nil {
		return err
	}

	if l.Decision != nil {
		return checkDecision(l)
	}

	return nil
}

// checkLinkFields rejects a field that the link type does not own. The rules then check the values of the fields
// that each type does own.
func checkLinkFields(l Link) error {
	if l.Type != LinkCalls && len(l.Tools) > 0 {
		return structural("link %q sets tools but is not a calls link", l.ID)
	}

	for _, tool := range l.Tools {
		if tool == "" {
			return structural("link %q has an empty tool name", l.ID)
		}

		if !toolNamePattern.MatchString(tool) {
			return structural("link %q has tool name %q that does not match %s", l.ID, tool, toolNamePattern.String())
		}
	}

	if l.Type != LinkDelegates {
		if l.Mode != "" || l.Context != "" || len(l.Outputs) > 0 || l.Group != "" {
			return structural("link %q sets delegation fields but is not a delegates link", l.ID)
		}
	}

	if l.Type == LinkDelegates {
		if !slices.Contains(validModes, l.Mode) {
			return structural("link %q has unknown mode %q", l.ID, l.Mode)
		}

		if !slices.Contains(validContext, l.Context) {
			return structural("link %q has unknown context %q", l.ID, l.Context)
		}

		for _, output := range l.Outputs {
			if output == "" {
				return structural("link %q has an empty output name", l.ID)
			}
		}
	}

	if l.Type != LinkRetry {
		if l.MaxIterations != 0 || l.Until != "" {
			return structural("link %q sets retry fields but is not a retry link", l.ID)
		}

		return nil
	}

	if l.Until == "" {
		return structural("retry link %q needs an until condition", l.ID)
	}

	if utf8.RuneCountInString(l.Until) > MaxUntilRunes {
		return structural("retry link %q has an until condition longer than %d characters", l.ID, MaxUntilRunes)
	}

	return nil
}

func checkDecision(l Link) error {
	if l.Type != LinkDelegates && l.Type != LinkRetry {
		return structural("link %q has a decision but is not a delegates or retry link", l.ID)
	}

	d := l.Decision
	if !slices.Contains(validDecided, d.DecidedBy) {
		return structural("link %q decision has unknown decidedBy %q", l.ID, d.DecidedBy)
	}

	values := []struct {
		name  string
		value float64
	}{
		{"confidence", d.Confidence},
		{"probabilities.subagent", d.Probabilities.Subagent},
		{"probabilities.handoff", d.Probabilities.Handoff},
		{"probabilities.parallel", d.Probabilities.Parallel},
	}
	for _, v := range values {
		if v.value < 0 || v.value > 1 {
			return structural("link %q decision %s must be between 0 and 1", l.ID, v.name)
		}
	}

	return nil
}

func structural(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidDocument, fmt.Sprintf(format, args...))
}
