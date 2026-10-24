// Package agentspec parses and validates the YAML frontmatter of Agent Skills
// (SKILL.md) and agent (.md) definitions and reports the findings as SecurityFindings.
package agentspec

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
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
	// FindingMissingInstructions is reported when a TOML agent has no developer_instructions string.
	FindingMissingInstructions = "opendepot-agentspec-010"

	// MaxNameLength is the maximum length of a name.
	MaxNameLength = 64
	// MaxDescriptionLength is the maximum length of a description.
	MaxDescriptionLength = 1024

	severityError   = "HIGH"
	severityWarning = "LOW"
	skillFileName   = "SKILL.md"
	agentFileSuffix = ".agent.md"
	agentFileExt    = ".md"
	agentFileTOML   = ".toml"
)

// FindAgentEntry returns the path of the entry file for the agent called name inside dir. The Copilot
// layout (<name>.agent.md), the Claude layout (<name>.md), and the Codex layout (<name>.toml) are accepted.
// An error is returned when no file exists or when more than one exists, because the entry would be ambiguous.
func FindAgentEntry(dir string, name string) (string, error) {
	if name == "" || name == "." || name == ".." || filepath.Base(name) != name {
		return "", fmt.Errorf("invalid agent name %q", name)
	}

	candidates := []string{name + agentFileSuffix, name + agentFileExt, name + agentFileTOML}
	var found []string

	for _, candidate := range candidates {
		path := filepath.Join(dir, candidate)

		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			found = append(found, path)
		}
	}

	switch len(found) {
	case 0:
		return "", fmt.Errorf("no agent entry file for %q: expected one of %s", name, strings.Join(candidates, ", "))
	case 1:
		return found[0], nil
	default:
		names := make([]string, len(found))
		for i, path := range found {
			names[i] = filepath.Base(path)
		}

		return "", fmt.Errorf("ambiguous agent entry for %q: %s all exist", name, strings.Join(names, ", "))
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

// codexAgentKeys are the top-level keys of a Codex custom agent TOML file. Codex does not read a tools key
// from an agent file, so tools is not in this set and produces an unknown-key warning.
var codexAgentKeys = keySet(
	"name", "description", "developer_instructions", "model", "model_reasoning_effort",
	"model_reasoning_summary", "model_verbosity", "features", "skills",
)

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
	return parse(path, bytes.NewReader(content), "", false, KnownKeys, "")
}

// ParseSkillNamed parses a SKILL.md whose name must match name rather than its containing directory.
// It is used when the file is extracted outside the directory the Skill is installed into.
//
// The returned error is non-nil if and only if at least one error-level finding was raised.
// The returned Result is always non-nil and carries every finding, including warnings.
func ParseSkillNamed(path string, content []byte, name string) (*Result, error) {
	return parse(path, bytes.NewReader(content), name, false, KnownKeys, "")
}

// ParseSkillNamedFor is ParseSkillNamed with the frontmatter keys validated against platform. An empty
// platform accepts the keys of every platform. An unsupported platform returns an error and no findings.
func ParseSkillNamedFor(path string, content []byte, name string, platform string) (*Result, error) {
	return ParseSkillNamedForReader(path, bytes.NewReader(content), name, platform)
}

// ParseSkillNamedForReader is ParseSkillNamedFor for an entry read from r. The entry is parsed as it is read, so
// the file is not held in memory as a whole.
func ParseSkillNamedForReader(path string, r io.Reader, name string, platform string) (*Result, error) {
	keys, err := knownKeysFor(false, platform)
	if err != nil {
		return &Result{}, err
	}

	return parse(path, r, name, false, keys, platform)
}

// ParseAgent parses and validates an agent entry file, either {name}.agent.md or {name}.md.
// The name is display metadata: it must be present and within the length limit, but it does not
// need to match the file name. The Agent's configured name identifies it in the registry.
//
// The returned error is non-nil if and only if at least one error-level finding was raised.
// The returned Result is always non-nil and carries every finding, including warnings.
func ParseAgent(path string, content []byte) (*Result, error) {
	return parse(path, bytes.NewReader(content), "", true, KnownKeys, "")
}

// ParseAgentReader is ParseAgent for an entry read from r. The entry is parsed as it is read, so the file is not
// held in memory as a whole.
func ParseAgentReader(path string, r io.Reader) (*Result, error) {
	return parse(path, r, "", true, KnownKeys, "")
}

// ParseAgentFor is ParseAgent with the frontmatter keys validated against platform. An empty platform
// accepts the keys of every platform. An unsupported platform returns an error and no findings.
func ParseAgentFor(path string, content []byte, platform string) (*Result, error) {
	return ParseAgentForReader(path, bytes.NewReader(content), platform)
}

// ParseAgentForReader is ParseAgentFor for an entry read from r. The entry is parsed as it is read, so the file is
// not held in memory as a whole.
func ParseAgentForReader(path string, r io.Reader, platform string) (*Result, error) {
	keys, err := knownKeysFor(true, platform)
	if err != nil {
		return &Result{}, err
	}

	return parse(path, r, "", true, keys, platform)
}

// parse validates the file at path. When dirName is empty, a SKILL.md must match its containing directory.
// Top-level keys outside known produce a warning. The platform is named in that warning when it is set.
func parse(path string, r io.Reader, dirName string, agent bool, known map[string]struct{}, platform string) (*Result, error) {
	if strings.EqualFold(filepath.Ext(path), agentFileTOML) {
		return parseCodexAgent(path, r, platform)
	}

	result := &Result{}
	base := filepath.Base(path)
	expectedName := strings.TrimSuffix(base, filepath.Ext(base))

	if strings.EqualFold(base, skillFileName) {
		expectedName = dirName
		if expectedName == "" {
			expectedName = filepath.Base(filepath.Dir(path))
		}
	}

	frontmatter, body, ok, err := splitFrontmatter(r)
	if err != nil {
		return result, fmt.Errorf("read %s: %w", base, err)
	}

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

// parseCodexAgent validates a Codex custom agent TOML file. The file is the whole definition, so the
// developer_instructions string is the body. A TOML file can only be a Codex agent, so platform must be empty or codex.
func parseCodexAgent(path string, r io.Reader, platform string) (*Result, error) {
	result := &Result{}
	base := filepath.Base(path)

	if platform != "" && platform != PlatformCodex {
		return result, fmt.Errorf("%s is a Codex TOML agent and cannot be validated for platform %q", base, platform)
	}

	content, err := io.ReadAll(r)
	if err != nil {
		return result, fmt.Errorf("read %s: %w", base, err)
	}

	var fm map[string]any

	if err := toml.Unmarshal(content, &fm); err != nil {
		result.add(path, FindingInvalidFrontmatter, severityError, fmt.Sprintf("file is not valid TOML: %v", err))

		return result, result.err()
	}

	if fm == nil {
		fm = map[string]any{}
	}

	result.Frontmatter = fm
	validateName(result, path, "", fm, true)
	validateDescription(result, path, fm)
	validateModel(result, path, fm)

	instructions, ok := fm["developer_instructions"].(string)
	if !ok || strings.TrimSpace(instructions) == "" {
		result.add(path, FindingMissingInstructions, severityError, "developer_instructions is required and must be a non-empty string")
	} else {
		result.Body = instructions
	}

	for key := range fm {
		if _, ok := codexAgentKeys[key]; !ok {
			result.add(path, FindingUnknownKey, severityWarning, fmt.Sprintf("unknown key %q for platform %q", key, PlatformCodex))
		}
	}

	return result, result.err()
}

// splitFrontmatter reads the raw frontmatter and the body that follows it from r, one line at a time. The frontmatter
// must open with a "---" line on the first line and be closed by a second "---" line. CRLF line endings are read as LF.
func splitFrontmatter(r io.Reader) (string, string, bool, error) {
	br := bufio.NewReader(r)

	first, err := readLine(br)
	if err != nil && !errors.Is(err, io.EOF) {
		return "", "", false, err
	}

	if first != "---\n" {
		return "", "", false, nil
	}

	var frontmatter strings.Builder
	for {
		line, err := readLine(br)
		if err != nil && !errors.Is(err, io.EOF) {
			return "", "", false, err
		}

		switch {
		case line == "---\n":
			body, err := readBody(br, frontmatter.Len() > 0)
			if err != nil {
				return "", "", false, err
			}

			return frontmatter.String(), body, true, nil
		case line == "---" && errors.Is(err, io.EOF):
			return strings.TrimSuffix(frontmatter.String(), "\n"), "", true, nil
		case errors.Is(err, io.EOF):
			return "", "", false, nil
		}

		frontmatter.WriteString(line)
	}
}

// readBody reads the body that follows the closing frontmatter line. One newline that directly follows the closing
// line is not part of the body. When the frontmatter is empty, the closing line directly follows the opening line and
// the body keeps that newline, as splitFrontmatter has always done.
func readBody(br *bufio.Reader, trimLeadingNewline bool) (string, error) {
	var body strings.Builder
	for first := true; ; first = false {
		line, err := readLine(br)
		if err != nil && !errors.Is(err, io.EOF) {
			return "", err
		}

		if !first || !trimLeadingNewline || line != "\n" {
			body.WriteString(line)
		}

		if errors.Is(err, io.EOF) {
			return body.String(), nil
		}
	}
}

// readLine returns the next line of br with CRLF line endings read as LF.
func readLine(br *bufio.Reader) (string, error) {
	line, err := br.ReadString('\n')
	return strings.ReplaceAll(line, "\r\n", "\n"), err
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
