package main

import (
	"fmt"

	"github.com/hashicorp/go-version"
)

// agentVersion is one entry of the agents.v1 versions response.
type agentVersion struct {
	Version string `json:"version"`
	Yanked  bool   `json:"yanked"`
}

// resolveVersion returns the highest version satisfying the constraint. Yanked versions are
// skipped unless allowYanked is set.
func resolveVersion(versions []agentVersion, constraint string, allowYanked bool) (string, error) {
	c, err := version.NewConstraint(constraint)
	if err != nil {
		return "", fmt.Errorf("invalid version constraint %q: %w", constraint, err)
	}

	var best *version.Version
	var bestRaw string
	for _, v := range versions {
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

		if best == nil || parsed.GreaterThan(best) {
			best = parsed
			bestRaw = v.Version
		}
	}

	if best == nil {
		return "", fmt.Errorf("no version satisfies %q", constraint)
	}

	return bestRaw, nil
}
