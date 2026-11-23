package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/hashicorp/go-version"
	"github.com/hashicorp/hcl/v2/gohcl"
	"github.com/hashicorp/hcl/v2/hclparse"
)

const configFileName = "opendepot.hcl"

var (
	fingerprintPattern = regexp.MustCompile(`^[0-9A-F]{40}$`)
	validTargets       = map[string]bool{targetCopilot: true, targetClaude: true, targetCodex: true, targetAgentsMD: true}
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
	Targets  []string `hcl:"targets,optional"`
	Registry *string  `hcl:"registry,optional"`
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
	targets  []string
	registry string
	entries  []entry
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

		if cfg.Opendepot.Registry != nil {
			host, err := validateRegistryHost(*cfg.Opendepot.Registry)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", path, err)
			}

			proj.registry = host
		}
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

// loadOptionalProject returns the project at path, or nil when no opendepot.hcl exists.
func loadOptionalProject(path string) (*project, error) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}

	return loadConfig(path)
}

// validateRegistryHost checks that host is a bare registry host. It uses the same
// host/namespace/name parsing as source addresses.
func validateRegistryHost(host string) (string, error) {
	if host == "" {
		return "", fmt.Errorf("registry host must not be empty")
	}

	if _, err := parseSource(host + "/namespace/name"); err != nil {
		return "", fmt.Errorf("registry %q must be a bare host name", host)
	}

	return host, nil
}

// resolveListRegistry picks the registry used by listing commands. The order is the explicit
// --registry value, the project default registry, then the single host shared by all project
// sources. proj may be nil when no opendepot.hcl exists. Ambiguous or missing hosts are errors.
func resolveListRegistry(explicit string, proj *project) (string, error) {
	if explicit != "" {
		return validateRegistryHost(explicit)
	}

	if proj == nil {
		return "", errNoRegistry
	}

	if proj.registry != "" {
		return proj.registry, nil
	}

	hosts := map[string]bool{}
	for _, e := range proj.entries {
		src, err := parseSource(e.source)
		if err != nil {
			return "", err
		}

		hosts[src.host] = true
	}

	switch len(hosts) {
	case 0:
		return "", errNoRegistry
	case 1:
		for host := range hosts {
			return host, nil
		}
	}

	names := make([]string, 0, len(hosts))
	for host := range hosts {
		names = append(names, host)
	}

	sort.Strings(names)

	return "", fmt.Errorf("project sources use multiple registry hosts (%s); pass --registry to choose one", strings.Join(names, ", "))
}

var errNoRegistry = errors.New("no registry could be determined: pass --registry, or set registry in the opendepot block of opendepot.hcl")

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
// The copilot, claude, and codex targets are user-level folders under the home directory. Agent targets
// are filtered by entry format once the archive is staged. See agentTargetAccepts.
func (p *project) installTargets(e entry) ([]installTarget, error) {
	if e.path != "" {
		return []installTarget{{label: targetCustom, path: e.path}}, nil
	}

	var targets []installTarget
	for _, t := range p.targets {
		if t == targetAgentsMD {
			targets = append(targets, installTarget{label: t, path: agentsMDFile})

			continue
		}

		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("resolve home directory for %s target: %w", t, err)
		}

		switch t {
		case targetCopilot, targetClaude:
			root := copilotHomeDir
			if t == targetClaude {
				root = claudeHomeDir
			}

			if e.kind == kindSkill {
				targets = append(targets, installTarget{label: t, path: filepath.Join(home, root, "skills", e.name)})
			} else {
				file := e.name + ".agent.md"
				if t == targetClaude {
					file = e.name + ".md"
				}

				targets = append(targets, installTarget{label: t, path: filepath.Join(home, root, "agents", file)})
			}
		case targetCodex:
			if e.kind == kindSkill {
				targets = append(targets, installTarget{label: t, path: filepath.Join(home, agentsHomeDir, "skills", e.name)})
			} else {
				targets = append(targets, installTarget{label: t, path: filepath.Join(home, codexHomeDir, "agents", e.name+".toml")})
			}
		}
	}

	if len(targets) == 0 {
		return nil, fmt.Errorf("no install targets for %s: set targets in the opendepot block or a path on the entry", e.name)
	}

	return targets, nil
}
