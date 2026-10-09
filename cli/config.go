package main

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/hashicorp/go-version"
	"github.com/hashicorp/hcl/v2/gohcl"
	"github.com/hashicorp/hcl/v2/hclparse"
)

const configFileName = "opendepot.hcl"

var (
	fingerprintPattern = regexp.MustCompile(`^[0-9A-F]{40}$`)
	validTargets       = map[string]bool{targetCopilot: true, targetClaude: true, targetAgentsMD: true}
)

type entryKind string

const (
	kindSkill entryKind = "agent_skill"
	kindAgent entryKind = "agent"
)

func (k entryKind) plural() string {
	if k == kindSkill {
		return "skills"
	}

	return "agents"
}

func (k entryKind) singular() string {
	if k == kindSkill {
		return "skill"
	}

	return "agent"
}

type configFile struct {
	Opendepot *opendepotBlock `hcl:"opendepot,block"`
	Skills    []entryBlock    `hcl:"agent_skill,block"`
	Agents    []entryBlock    `hcl:"agent,block"`
}

type opendepotBlock struct {
	Targets []string `hcl:"targets,optional"`
}

type entryBlock struct {
	Name       string  `hcl:"name,label"`
	Source     string  `hcl:"source"`
	Version    string  `hcl:"version"`
	SigningKey *string `hcl:"signing_key,optional"`
	Path       *string `hcl:"path,optional"`
}

// entry is a validated opendepot.hcl agent_skill or agent block.
type entry struct {
	kind       entryKind
	name       string
	source     string
	constraint string
	signingKey string
	path       string
}

// project is the validated contents of opendepot.hcl.
type project struct {
	targets []string
	entries []entry
}

func loadConfig(path string) (*project, error) {
	parser := hclparse.NewParser()
	file, diags := parser.ParseHCLFile(path)
	if diags.HasErrors() {
		return nil, fmt.Errorf("parse %s: %s", path, diags.Error())
	}

	var cfg configFile
	diags = gohcl.DecodeBody(file.Body, nil, &cfg)
	if diags.HasErrors() {
		return nil, fmt.Errorf("decode %s: %s", path, diags.Error())
	}

	proj := &project{}
	if cfg.Opendepot != nil {
		for _, t := range cfg.Opendepot.Targets {
			if !validTargets[t] {
				return nil, fmt.Errorf("%s: unknown target %q", path, t)
			}
		}

		proj.targets = cfg.Opendepot.Targets
	}

	seenNames := map[string]bool{}
	seenSources := map[string]bool{}
	add := func(kind entryKind, blocks []entryBlock) error {
		for _, b := range blocks {
			e, err := newEntry(kind, b)
			if err != nil {
				return fmt.Errorf("%s %q: %w", kind, b.Name, err)
			}

			if seenNames[b.Name] {
				return fmt.Errorf("duplicate block name %q", b.Name)
			}

			if seenSources[e.source] {
				return fmt.Errorf("duplicate source %q", e.source)
			}

			seenNames[b.Name] = true
			seenSources[e.source] = true
			proj.entries = append(proj.entries, e)
		}

		return nil
	}

	if err := add(kindSkill, cfg.Skills); err != nil {
		return nil, err
	}

	if err := add(kindAgent, cfg.Agents); err != nil {
		return nil, err
	}

	return proj, nil
}

func newEntry(kind entryKind, b entryBlock) (entry, error) {
	if b.Name == "" {
		return entry{}, fmt.Errorf("block label must not be empty")
	}

	if _, err := parseSource(b.Source); err != nil {
		return entry{}, err
	}

	if _, err := version.NewConstraint(b.Version); err != nil {
		return entry{}, fmt.Errorf("invalid version constraint %q: %w", b.Version, err)
	}

	e := entry{kind: kind, name: b.Name, source: b.Source, constraint: b.Version}
	if b.SigningKey != nil {
		key := strings.ToUpper(strings.TrimSpace(*b.SigningKey))
		if !fingerprintPattern.MatchString(key) {
			return entry{}, fmt.Errorf("signing_key must be a 40-hex fingerprint")
		}

		e.signingKey = key
	}

	if b.Path != nil {
		p := filepath.Clean(*b.Path)
		if p == "." || !filepath.IsLocal(p) || p == agentsMDFile {
			return entry{}, fmt.Errorf("path %q must be a relative path inside the project", *b.Path)
		}

		e.path = p
	}

	return e, nil
}

// installTarget is a resolved install destination. The label is recorded in the lock file.
type installTarget struct {
	label string
	path  string
}

// installTargets resolves where an entry is installed. A custom path replaces the default targets.
func (p *project) installTargets(e entry) ([]installTarget, error) {
	if e.path != "" {
		return []installTarget{{label: targetCustom, path: e.path}}, nil
	}

	var targets []installTarget
	for _, t := range p.targets {
		switch t {
		case targetCopilot:
			if e.kind == kindSkill {
				targets = append(targets, installTarget{label: t, path: filepath.Join(".github", "skills", e.name)})
			} else {
				targets = append(targets, installTarget{label: t, path: filepath.Join(".github", "agents", e.name+".agent.md")})
			}
		case targetClaude:
			if e.kind == kindSkill {
				targets = append(targets, installTarget{label: t, path: filepath.Join(".claude", "skills", e.name)})
			} else {
				targets = append(targets, installTarget{label: t, path: filepath.Join(".claude", "agents", e.name+".md")})
			}
		case targetAgentsMD:
			targets = append(targets, installTarget{label: t, path: agentsMDFile})
		}
	}

	if len(targets) == 0 {
		return nil, fmt.Errorf("no install targets for %s: set targets in the opendepot block or a path on the entry", e.name)
	}

	return targets, nil
}
