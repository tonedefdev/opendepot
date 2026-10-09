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
// Rules run against the full extracted content of the Skill or Agent, not the truncated Jev state.
type AgentRuleEnv struct {
	Name           string
	Description    string
	Tools          []string
	Body           string
	Scripts        string
	Domains        []string
	AllowedDomains []string
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
