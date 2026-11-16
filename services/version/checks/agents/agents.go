// Package agents evaluates the built-in Expr rules that flag risky Skill and Agent definitions.
package agents

import (
	_ "embed"
	"fmt"
	"net"
	"regexp"
	"strings"
	"sync"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"
	"sigs.k8s.io/yaml"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
)

//go:embed rules.yaml
var builtinRules []byte

// AgentRuleEnv is the environment each rule expression is compiled and evaluated against.
// Rules see all content, the body and every script segment, not the truncated Jev state.
type AgentRuleEnv struct {
	Name           string
	Description    string
	Tools          []string
	Body           string
	Scripts        string
	Domains        []string
	AllowedDomains []string
}

// ScriptSegment is one piece of a script file, streamed for evaluation. CutMidLine reports that the segment ends
// inside a line, so a URL that touches its end may continue in the next segment.
type ScriptSegment struct {
	Text       string
	CutMidLine bool
}

type ruleSpec struct {
	ID         string `json:"id"`
	Severity   string `json:"severity"`
	Title      string `json:"title"`
	Expression string `json:"expression"`
}

type ruleFile struct {
	Rules []ruleSpec `json:"rules"`
}

// Rule is a compiled deny expression. A true result is a finding.
type Rule struct {
	ID       string
	Severity string
	Title    string
	program  *vm.Program
}

// RuleSet is a compiled collection of agent rules.
type RuleSet struct {
	rules []Rule
}

var (
	defaultOnce sync.Once
	defaultSet  *RuleSet
	defaultErr  error
)

var domainPattern = regexp.MustCompile(`(?i)https?://[^\s"'<>\x60)\]]+`)

// Default returns the built-in rules compiled once per process. A compile error means the
// rules file is invalid and the controller must not start.
func Default() (*RuleSet, error) {
	defaultOnce.Do(func() {
		defaultSet, defaultErr = Compile(builtinRules)
	})

	return defaultSet, defaultErr
}

// Compile parses rule definitions from YAML and compiles each expression against AgentRuleEnv.
// An invalid expression, unknown severity, or duplicate rule ID returns an error.
func Compile(src []byte) (*RuleSet, error) {
	var file ruleFile
	if err := yaml.Unmarshal(src, &file); err != nil {
		return nil, fmt.Errorf("agents: parse rules: %w", err)
	}

	seen := make(map[string]struct{}, len(file.Rules))
	set := &RuleSet{rules: make([]Rule, 0, len(file.Rules))}
	for _, spec := range file.Rules {
		if spec.ID == "" {
			return nil, fmt.Errorf("agents: rule with title %q has no id", spec.Title)
		}

		if _, exists := seen[spec.ID]; exists {
			return nil, fmt.Errorf("agents: duplicate rule id %q", spec.ID)
		}

		seen[spec.ID] = struct{}{}

		program, err := expr.Compile(spec.Expression, expr.Env(AgentRuleEnv{}), expr.AsBool())
		if err != nil {
			return nil, fmt.Errorf("agents: rule %q: %w", spec.ID, err)
		}

		set.rules = append(set.rules, Rule{
			ID:       spec.ID,
			Severity: spec.Severity,
			Title:    spec.Title,
			program:  program,
		})
	}

	return set, nil
}

// Evaluate runs every rule against env and returns a finding for each rule that fires.
func (s *RuleSet) Evaluate(env AgentRuleEnv) ([]opendepotv1alpha1.SecurityFinding, error) {
	findings := make([]opendepotv1alpha1.SecurityFinding, 0)
	for _, rule := range s.rules {
		out, err := expr.Run(rule.program, env)
		if err != nil {
			return nil, fmt.Errorf("agents: rule %q: %w", rule.ID, err)
		}

		if fired, ok := out.(bool); ok && fired {
			findings = append(findings, opendepotv1alpha1.SecurityFinding{
				VulnerabilityID: rule.ID,
				Severity:        rule.Severity,
				Title:           rule.Title,
			})
		}
	}

	return findings, nil
}

// EvaluateSegments runs every rule against env once, then once per script segment with Scripts set to that segment.
// env.Scripts and env.Domains are set by this method: Domains is the union of the body's domains and the segments'
// domains, so the domain rule sees every URL. A rule that fires in any run produces one finding, in rules.yaml order.
func (s *RuleSet) EvaluateSegments(env AgentRuleEnv, segments []ScriptSegment) ([]opendepotv1alpha1.SecurityFinding, error) {
	env.Scripts = ""
	env.Domains = SegmentDomains(env.Body, segments)

	fired := make(map[string]struct{}, len(s.rules))
	if err := s.markFired(env, fired); err != nil {
		return nil, err
	}

	for _, segment := range segments {
		segmentEnv := env
		segmentEnv.Scripts = segment.Text
		if err := s.markFired(segmentEnv, fired); err != nil {
			return nil, err
		}
	}

	findings := make([]opendepotv1alpha1.SecurityFinding, 0, len(fired))
	for _, rule := range s.rules {
		if _, ok := fired[rule.ID]; ok {
			findings = append(findings, opendepotv1alpha1.SecurityFinding{
				VulnerabilityID: rule.ID,
				Severity:        rule.Severity,
				Title:           rule.Title,
			})
		}
	}

	return findings, nil
}

func (s *RuleSet) markFired(env AgentRuleEnv, fired map[string]struct{}) error {
	for _, rule := range s.rules {
		out, err := expr.Run(rule.program, env)
		if err != nil {
			return fmt.Errorf("agents: rule %q: %w", rule.ID, err)
		}

		if hit, isBool := out.(bool); isBool && hit {
			fired[rule.ID] = struct{}{}
		}
	}

	return nil
}

// SegmentDomains returns the deduplicated normalized hosts of the URLs in body and in every segment, in first-seen
// order. A URL that touches the end of a segment cut mid-line is skipped, because the next segment's overlap contains
// it whole. Otherwise its host would be truncated.
func SegmentDomains(body string, segments []ScriptSegment) []string {
	seen := make(map[string]struct{})
	domains := make([]string, 0)
	add := func(host string) {
		if _, ok := seen[host]; ok {
			return
		}

		seen[host] = struct{}{}
		domains = append(domains, host)
	}

	for _, host := range ExtractDomains(body) {
		add(host)
	}

	for _, segment := range segments {
		for _, loc := range domainPattern.FindAllStringIndex(segment.Text, -1) {
			if segment.CutMidLine && loc[1] == len(segment.Text) {
				continue
			}

			if host := NormalizeDomain(segment.Text[loc[0]:loc[1]]); host != "" {
				add(host)
			}
		}
	}

	return domains
}

// ExtractDomains returns the normalized host of every http or https URL found in text. Hosts are
// resolved with NormalizeDomain rather than net/url, which rejects some URLs a browser would still
// navigate to. Those URLs must not be dropped, so their hosts are still checked against the allowlist.
func ExtractDomains(text string) []string {
	matches := domainPattern.FindAllString(text, -1)
	domains := make([]string, 0, len(matches))
	for _, raw := range matches {
		if host := NormalizeDomain(raw); host != "" {
			domains = append(domains, host)
		}
	}

	return domains
}

// NormalizeDomain returns the lowercased host that a URL or allowlist entry resolves to, without
// scheme, userinfo, port, path, or IPv6 brackets. Authority ends at the first '/', '?', '#', or '\\'
// and userinfo ends at the last '@', matching how browsers resolve the host. Input that is not a
// valid host yields a value that no allowlist entry matches, so the domain rule still fires.
func NormalizeDomain(raw string) string {
	host := strings.TrimSpace(raw)
	if i := strings.Index(host, "://"); i >= 0 {
		host = host[i+len("://"):]
	}

	if i := strings.IndexAny(host, "/?#\\"); i >= 0 {
		host = host[:i]
	}

	if i := strings.LastIndex(host, "@"); i >= 0 {
		host = host[i+1:]
	}

	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}

	return strings.ToLower(strings.Trim(host, "[]"))
}
