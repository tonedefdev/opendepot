package main

import (
	"fmt"
	"strings"

	"github.com/hashicorp/go-version"
)

// agentVersion is one entry of the agents.v1 versions response.
type agentVersion struct {
	Version      string   `json:"version"`
	Yanked       bool     `json:"yanked"`
	Blocked      bool     `json:"blocked"`
	BlockReasons []string `json:"blockReasons"`
}

// blockedVersionError reports a version that matches a constraint but is blocked by a Jev policy.
// locked is set when the lock file pins the blocked version.
type blockedVersionError struct {
	version string
	reasons []string
	locked  bool
}

func (e *blockedVersionError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "version %s is blocked by a Jev policy", e.version)
	for _, reason := range e.reasons {
		fmt.Fprintf(&b, "\n- %s", reason)
	}

	if e.locked {
		b.WriteString("\nthe lock file pins this version; re-run with -upgrade to choose another")
	}

	return b.String()
}

// noVersionError reports that no published version satisfies a constraint and lists what is published.
type noVersionError struct {
	constraint string
	versions   []agentVersion
}

func (e *noVersionError) Error() string {
	if len(e.versions) == 0 {
		return fmt.Sprintf("no version satisfies %q; no versions are published", e.constraint)
	}

	labels := make([]string, 0, len(e.versions))
	for _, v := range e.versions {
		switch {
		case v.Blocked:
			labels = append(labels, v.Version+" (blocked)")
		case v.Yanked:
			labels = append(labels, v.Version+" (yanked)")
		default:
			labels = append(labels, v.Version)
		}
	}

	return fmt.Sprintf("no version satisfies %q\navailable versions: %s", e.constraint, strings.Join(labels, ", "))
}

// resolveVersion returns the highest unblocked version satisfying the constraint. Yanked versions
// are skipped unless allowYanked is set. When the only matches are blocked, it returns a
// blockedVersionError for the highest one so the Jev reasons reach the user.
func resolveVersion(versions []agentVersion, constraint string, allowYanked bool) (string, error) {
	c, err := version.NewConstraint(constraint)
	if err != nil {
		return "", fmt.Errorf("invalid version constraint %q: %w", constraint, err)
	}

	var best *version.Version
	var bestRaw string
	var blocked *agentVersion
	var blockedParsed *version.Version
	for i, v := range versions {
		if v.Yanked && !allowYanked {
			continue
		}

		parsed, err := version.NewVersion(v.Version)
		if err != nil {
			continue
		}

		if !c.Check(parsed) {
			continue
		}

		if v.Blocked {
			if blockedParsed == nil || parsed.GreaterThan(blockedParsed) {
				blockedParsed = parsed
				blocked = &versions[i]
			}

			continue
		}

		if best == nil || parsed.GreaterThan(best) {
			best = parsed
			bestRaw = v.Version
		}
	}

	if best != nil {
		return bestRaw, nil
	}

	if blocked != nil {
		return "", &blockedVersionError{version: blocked.Version, reasons: blocked.BlockReasons}
	}

	return "", &noVersionError{constraint: constraint, versions: versions}
}
