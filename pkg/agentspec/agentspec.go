// Package agentspec parses and validates the YAML frontmatter of Agent Skills
// (SKILL.md) and agent (.md) definitions and reports the findings as SecurityFindings.
package agentspec

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	"sigs.k8s.io/yaml"
)

const (
	// FindingNoFrontmatter is reported when the file does not start with a closed frontmatter block.
	FindingNoFrontmatter = "opendepot-agentspec-001"
	// FindingInvalidFrontmatter is reported when the frontmatter is not a YAML mapping.
	FindingInvalidFrontmatter = "opendepot-agentspec-002"
	// FindingInvalidName is reported when name is missing or violates the name format or length rules.
	FindingInvalidName = "opendepot-agentspec-003"
	// FindingNameMismatch is reported when a SKILL.md name does not match its containing directory.
	FindingNameMismatch = "opendepot-agentspec-004"
	// FindingMissingDescription is reported when description is missing, empty, or not a string.
	FindingMissingDescription = "opendepot-agentspec-005"
	// FindingDescriptionTooLong is reported when description exceeds MaxDescriptionLength characters.
	FindingDescriptionTooLong = "opendepot-agentspec-006"
	// FindingInvalidTools is reported when tools is neither a string nor a list of non-empty strings.
	FindingInvalidTools = "opendepot-agentspec-007"
	// FindingInvalidModel is reported when model is present but not a non-empty string.
	FindingInvalidModel = "opendepot-agentspec-008"
	// FindingUnknownKey is reported as a warning for top-level frontmatter keys outside KnownKeys.
	FindingUnknownKey = "opendepot-agentspec-009"

	// MaxNameLength is the maximum length of a name.
	MaxNameLength = 64
	// MaxDescriptionLength is the maximum length of a description.
	MaxDescriptionLength = 1024

	severityError   = "HIGH"
	severityWarning = "LOW"
	skillFileName   = "SKILL.md"
	agentFileSuffix = ".agent.md"
	agentFileExt    = ".md"
)

// FindAgentEntry returns the path of the entry file for the agent called name inside dir. Both
// the Copilot layout (<name>.agent.md) and the Claude layout (<name>.md) are accepted. An error
// is returned when neither file exists or when both exist, because the entry would be ambiguous.
func FindAgentEntry(dir string, name string) (string, error) {
	if name == "" || name == "." || name == ".." || filepath.Base(name) != name {
		return "", fmt.Errorf("invalid agent name %q", name)
	}

	candidates := []string{name + agentFileSuffix, name + agentFileExt}
	var found []string

	for _, candidate := range candidates {
		path := filepath.Join(dir, candidate)

		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			found = append(found, path)
		}
	}

	switch len(found) {
	case 0:
		return "", fmt.Errorf("no agent entry file for %q: expected %s or %s", name, candidates[0], candidates[1])
	case 1:
		return found[0], nil
	default:
		return "", fmt.Errorf("ambiguous agent entry for %q: both %s and %s exist", name, candidates[0], candidates[1])
	}
}

// Platform names an agent platform whose frontmatter keys can be enforced by ParseSkillNamedFor and ParseAgentFor.
const (
	PlatformAgentSkills = "agentskills"
	PlatformClaude      = "claude"
	PlatformCopilot     = "copilot"
	PlatformCodex       = "codex"
	PlatformOpenCode    = "opencode"
	PlatformPi          = "pi"
)

var (
	agentSkillsSkillKeys = keySet("name", "description", "license", "compatibility", "metadata", "allowed-tools")
	agentSkillsAgentKeys = keySet("name", "description", "tools", "model", "license", "compatibility", "metadata", "allowed-tools")

	platformSkillKeys = map[string]map[string]struct{}{
		PlatformAgentSkills: agentSkillsSkillKeys,
		PlatformClaude: union(agentSkillsSkillKeys, keySet(
			"when_to_use", "argument-hint", "arguments", "disable-model-invocation", "user-invocable",
			"disallowed-tools", "model", "effort", "context", "agent", "hooks", "paths", "shell",
		)),
		PlatformCopilot:  union(agentSkillsSkillKeys, keySet("argument-hint")),
		PlatformCodex:    agentSkillsSkillKeys,
		PlatformOpenCode: agentSkillsSkillKeys,
		PlatformPi:       union(agentSkillsSkillKeys, keySet("disable-model-invocation")),
	}

	platformAgentKeys = map[string]map[string]struct{}{
		PlatformAgentSkills: agentSkillsAgentKeys,
		PlatformClaude: union(agentSkillsAgentKeys, keySet(
			"disallowedTools", "permissionMode", "mcpServers", "hooks", "maxTurns", "skills", "initialPrompt",
			"memory", "effort", "background", "omitClaudeMd", "isolation", "color",
		)),
		PlatformCopilot: union(agentSkillsAgentKeys, keySet(
			"target", "disable-model-invocation", "user-invocable", "mcp-servers", "argument-hint", "handoffs",
		)),
		PlatformCodex: agentSkillsAgentKeys,
		PlatformOpenCode: keySet(
			"name", "description", "mode", "model", "temperature", "top_p", "steps", "disable", "prompt",
			"tools", "permission", "hidden", "color",
		),
		PlatformPi: agentSkillsAgentKeys,
	}

	// KnownKeys is the union of the keys of every platform. It is used when no platform is set, so that
	// omitting the platform never produces a warning that a platform-specific key would have avoided.
	// Keys outside this set produce a FindingUnknownKey warning and do not fail validation.
	//   - name, description: required identity fields (see Parse for validation rules).
	//   - tools: a comma-separated string or a list of tool names.
	//   - model: a non-empty string naming the model to run the agent with.
	//   - allowed-tools, license, compatibility, metadata: optional Agent Skills fields.
	KnownKeys = union(unionAll(platformSkillKeys), unionAll(platformAgentKeys))
)

func keySet(names ...string) map[string]struct{} {
	set := make(map[string]struct{}, len(names))

	for _, name := range names {
		set[name] = struct{}{}
	}

	return set
}

func union(sets ...map[string]struct{}) map[string]struct{} {
	merged := map[string]struct{}{}

	for _, set := range sets {
		for key := range set {
			merged[key] = struct{}{}
		}
	}

	return merged
}

func unionAll(byPlatform map[string]map[string]struct{}) map[string]struct{} {
	sets := make([]map[string]struct{}, 0, len(byPlatform))

	for _, set := range byPlatform {
		sets = append(sets, set)
	}

	return union(sets...)
}

// knownKeysFor returns the frontmatter keys accepted for a skill or agent on platform. An empty platform
// accepts the union of every platform's keys. An unsupported platform is an error.
func knownKeysFor(agent bool, platform string) (map[string]struct{}, error) {
	if platform == "" {
		return KnownKeys, nil
	}

	byPlatform := platformSkillKeys
	if agent {
		byPlatform = platformAgentKeys
	}

	keys, ok := byPlatform[platform]
	if !ok {
		return nil, fmt.Errorf("unsupported agent platform %q", platform)
	}

	return keys, nil
}

var namePattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// Result is the parsed contents of a SKILL.md or agent .md file.
type Result struct {
	Name        string
	Description string
	Tools       []string
	Model       string
	Body        string
	Frontmatter map[string]any
	// Findings holds every finding raised while parsing, including warnings.
	Findings []opendepotv1alpha1.SecurityFinding
}

// Parse parses the frontmatter of the file at path with the given content and validates it.
//
// For a file named SKILL.md the name must match the containing directory.
//
// The returned error is non-nil if and only if at least one error-level finding was raised.
// The returned Result is always non-nil and carries every finding, including warnings.
func Parse(path string, content []byte) (*Result, error) {
	return parse(path, content, "", false, KnownKeys, "")
}

// ParseSkillNamed parses a SKILL.md whose name must match name rather than its containing directory.
// It is used when the file is extracted outside the directory the Skill is installed into.
//
// The returned error is non-nil if and only if at least one error-level finding was raised.
// The returned Result is always non-nil and carries every finding, including warnings.
func ParseSkillNamed(path string, content []byte, name string) (*Result, error) {
	return parse(path, content, name, false, KnownKeys, "")
}

// ParseSkillNamedFor is ParseSkillNamed with the frontmatter keys validated against platform. An empty
// platform accepts the keys of every platform. An unsupported platform returns an error and no findings.
func ParseSkillNamedFor(path string, content []byte, name string, platform string) (*Result, error) {
	keys, err := knownKeysFor(false, platform)
	if err != nil {
		return &Result{}, err
	}

	return parse(path, content, name, false, keys, platform)
}

// ParseAgent parses and validates an agent entry file, either {name}.agent.md or {name}.md.
// The name is display metadata: it must be present and within the length limit, but it does not
// need to match the file name. The Agent's configured name identifies it in the registry.
//
// The returned error is non-nil if and only if at least one error-level finding was raised.
// The returned Result is always non-nil and carries every finding, including warnings.
func ParseAgent(path string, content []byte) (*Result, error) {
	return parse(path, content, "", true, KnownKeys, "")
}

// ParseAgentFor is ParseAgent with the frontmatter keys validated against platform. An empty platform
// accepts the keys of every platform. An unsupported platform returns an error and no findings.
func ParseAgentFor(path string, content []byte, platform string) (*Result, error) {
	keys, err := knownKeysFor(true, platform)
	if err != nil {
		return &Result{}, err
	}

	return parse(path, content, "", true, keys, platform)
}

// parse validates the file at path. When dirName is empty, a SKILL.md must match its containing directory.
// Top-level keys outside known produce a warning. The platform is named in that warning when it is set.
func parse(path string, content []byte, dirName string, agent bool, known map[string]struct{}, platform string) (*Result, error) {
	result := &Result{}
	base := filepath.Base(path)
	expectedName := strings.TrimSuffix(base, filepath.Ext(base))

	if strings.EqualFold(base, skillFileName) {
		expectedName = dirName
		if expectedName == "" {
			expectedName = filepath.Base(filepath.Dir(path))
		}
	}

	frontmatter, body, ok := splitFrontmatter(string(content))

	if !ok {
		result.add(path, FindingNoFrontmatter, severityError, "file has no YAML frontmatter block")

		return result, result.err()
	}

	result.Body = body
	var fm map[string]any

	if err := yaml.Unmarshal([]byte(frontmatter), &fm); err != nil {
		result.add(path, FindingInvalidFrontmatter, severityError, fmt.Sprintf("frontmatter is not a valid YAML mapping: %v", err))

		return result, result.err()
	}

	if fm == nil {
		fm = map[string]any{}
	}

	result.Frontmatter = fm
	validateName(result, path, expectedName, fm, agent)
	validateDescription(result, path, fm)
	validateTools(result, path, fm)
	validateModel(result, path, fm)

	for key := range fm {
		if _, ok := known[key]; !ok {
			message := fmt.Sprintf("unknown frontmatter key %q", key)
			if platform != "" {
				message = fmt.Sprintf("unknown frontmatter key %q for platform %q", key, platform)
			}

			result.add(path, FindingUnknownKey, severityWarning, message)
		}
	}

	return result, result.err()
}

// splitFrontmatter returns the raw frontmatter and the body that follows it. The frontmatter
// must open with a "---" line on the first line and be closed by a second "---" line.
func splitFrontmatter(content string) (string, string, bool) {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	rest, ok := strings.CutPrefix(content, "---\n")

	if !ok {
		return "", "", false
	}

	if strings.HasPrefix(rest, "---\n") || rest == "---" {
		return "", strings.TrimPrefix(strings.TrimPrefix(rest, "---"), "\n"), true
	}

	end := strings.Index(rest, "\n---\n")

	if end < 0 {
		if !strings.HasSuffix(rest, "\n---") {
			return "", "", false
		}

		return strings.TrimSuffix(rest, "\n---"), "", true
	}

	return rest[:end+1], strings.TrimPrefix(rest[end+len("\n---\n"):], "\n"), true
}

func validateName(result *Result, path, expectedName string, fm map[string]any, agent bool) {
	name, ok := fm["name"].(string)
	switch {
	case !ok || name == "":
		result.add(path, FindingInvalidName, severityError, "name is required")

		return
	case len(name) > MaxNameLength:
		result.add(path, FindingInvalidName, severityError, fmt.Sprintf("name must be at most %d characters", MaxNameLength))

		return
	case !agent && !namePattern.MatchString(name):
		result.add(path, FindingInvalidName, severityError, fmt.Sprintf("name %q must contain only lowercase letters, digits, and single hyphens, without leading or trailing hyphens", name))

		return
	}

	result.Name = name

	if !agent && name != expectedName {
		result.add(path, FindingNameMismatch, severityError, fmt.Sprintf("name %q does not match %q", name, expectedName))
	}
}

func validateDescription(result *Result, path string, fm map[string]any) {
	description, ok := fm["description"].(string)

	if !ok || strings.TrimSpace(description) == "" {
		result.add(path, FindingMissingDescription, severityError, "description is required and must be a non-empty string")

		return
	}

	if len(description) > MaxDescriptionLength {
		result.add(path, FindingDescriptionTooLong, severityError, fmt.Sprintf("description must be at most %d characters", MaxDescriptionLength))

		return
	}

	result.Description = description
}

func validateTools(result *Result, path string, fm map[string]any) {
	raw, present := fm["tools"]

	if !present {
		return
	}

	switch value := raw.(type) {
	case string:
		for tool := range strings.SplitSeq(value, ",") {
			if tool = strings.TrimSpace(tool); tool != "" {
				result.Tools = append(result.Tools, tool)
			}
		}
	case []any:
		for _, item := range value {
			tool, ok := item.(string)

			if !ok || strings.TrimSpace(tool) == "" {
				result.add(path, FindingInvalidTools, severityError, "tools must be a string or a list of non-empty strings")

				return
			}

			result.Tools = append(result.Tools, strings.TrimSpace(tool))
		}
	default:
		result.add(path, FindingInvalidTools, severityError, "tools must be a string or a list of non-empty strings")
	}
}

func validateModel(result *Result, path string, fm map[string]any) {
	raw, present := fm["model"]

	if !present {
		return
	}

	model, ok := raw.(string)

	if !ok || strings.TrimSpace(model) == "" {
		result.add(path, FindingInvalidModel, severityError, "model must be a non-empty string")

		return
	}

	result.Model = strings.TrimSpace(model)
}

func (r *Result) add(path, id, severity, title string) {
	r.Findings = append(r.Findings, opendepotv1alpha1.SecurityFinding{
		VulnerabilityID: id,
		PkgName:         path,
		Severity:        severity,
		Title:           title,
	})
}

func (r *Result) err() error {
	var errs []error

	for _, f := range r.Findings {
		if f.Severity == severityError {
			errs = append(errs, fmt.Errorf("%s: %s: %s", f.VulnerabilityID, f.PkgName, f.Title))
		}
	}

	return errors.Join(errs...)
}
