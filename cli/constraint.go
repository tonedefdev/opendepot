package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/hashicorp/go-version"
)

const (
	statusBlocked          = "blocked"
	statusRejected         = "rejected"
	statusPending          = "pending"
	statusAssessmentFailed = "assessmentFailed"
)

// agentVersion is one entry of the agents.v1 versions response.
type agentVersion struct {
	Version      string   `json:"version"`
	Yanked       bool     `json:"yanked"`
	Blocked      bool     `json:"blocked"`
	BlockReasons []string `json:"blockReasons"`
}

// unavailableCandidate is one entry of the unavailableCandidates field of the agents.v1 versions response.
// Version is set for a versioned candidate and Commit for an unversioned one. Servers that predate the field
// send none.
type unavailableCandidate struct {
	Version string   `json:"version"`
	Commit  string   `json:"commit"`
	Status  string   `json:"status"`
	Reasons []string `json:"reasons"`
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
		b.WriteString("\nthe lock file pins this version; re-run with --upgrade to choose another")
	}

	return b.String()
}

// noVersionError reports that no published version satisfies a constraint and lists what is published.
// unavailable holds versioned candidates that versions does not list, and unversioned holds rejected or
// failed commits, which are never compared with the constraint.
type noVersionError struct {
	constraint  string
	versions    []agentVersion
	unavailable []unavailableCandidate
	unversioned []unavailableCandidate
}

func (e *noVersionError) Error() string {
	labels := make([]string, 0, len(e.versions)+len(e.unavailable))
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

	for _, c := range e.unavailable {
		labels = append(labels, fmt.Sprintf("%s (%s)", c.Version, statusLabel(c.Status)))
	}

	var b strings.Builder
	if len(labels) == 0 {
		fmt.Fprintf(&b, "no version satisfies %q; no versions are published", e.constraint)
	} else {
		fmt.Fprintf(&b, "no version satisfies %q\navailable versions: %s", e.constraint, strings.Join(labels, ", "))
	}

	for _, c := range e.unversioned {
		fmt.Fprintf(&b, "\n%s", describeCandidate(c))
	}

	return b.String()
}

// fallbackRefusedError reports that an older matching version is installable, but a newer matching version
// is blocked, rejected, or failed assessment. The older version is not installed unless the caller allows it.
type fallbackRefusedError struct {
	constraint  string
	chosen      string
	newer       []unavailableCandidate
	unversioned []unavailableCandidate
}

func (e *fallbackRefusedError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "version %s satisfies %q, but a newer matching version is not installable", e.chosen, e.constraint)
	for _, c := range e.newer {
		fmt.Fprintf(&b, "\n%s", describeCandidate(c))
	}

	for _, c := range e.unversioned {
		fmt.Fprintf(&b, "\n%s", describeCandidate(c))
	}

	fmt.Fprintf(&b, "\nre-run with --allow-older-versions to install %s instead", e.chosen)
	return b.String()
}

// versionChoice is the version selected for a constraint. skipped lists the newer matching candidates that
// were passed over to reach it.
type versionChoice struct {
	version string
	skipped []unavailableCandidate
}

// matchedCandidate is an unavailable or blocked candidate that matches a constraint.
type matchedCandidate struct {
	parsed *version.Version
	cand   unavailableCandidate
}

// candidatePhrase completes "version X" or "commit X" for an unavailable status.
func candidatePhrase(status string) string {
	switch status {
	case statusBlocked:
		return "is blocked by a Jev policy"
	case statusRejected:
		return "is rejected"
	case statusAssessmentFailed:
		return "failed assessment"
	case statusPending:
		return "is pending assessment"
	}

	return "is unavailable"
}

// statusLabel is the short label used in version lists.
func statusLabel(status string) string {
	switch status {
	case statusBlocked:
		return "blocked"
	case statusRejected:
		return "rejected"
	case statusAssessmentFailed:
		return "assessment failed"
	case statusPending:
		return "pending"
	}

	return "unavailable"
}

// describeCandidate names an unavailable candidate on its first line and lists its reasons after it.
// An unversioned commit states that no version was assigned, so it is not compared with a constraint.
func describeCandidate(c unavailableCandidate) string {
	var b strings.Builder
	if c.Version != "" {
		fmt.Fprintf(&b, "version %s %s", c.Version, candidatePhrase(c.Status))
	} else {
		fmt.Fprintf(&b, "commit %s %s; no version was assigned, so it is not compared with the constraint", c.Commit, candidatePhrase(c.Status))
	}

	for _, reason := range c.Reasons {
		fmt.Fprintf(&b, "\n- %s", reason)
	}

	return b.String()
}

// isRefusal reports whether a candidate status stops the resolver from falling back to an older version.
// Pending is excluded because it is in progress, not a rejection.
func isRefusal(status string) bool {
	return status == statusBlocked || status == statusRejected || status == statusAssessmentFailed
}

// selectVersion returns the highest installable version satisfying the constraint. Versions that are blocked,
// yanked (unless allowYanked), or unavailable are skipped. Newer matching candidates that are blocked, rejected,
// or failed assessment are not silently passed over: with allowOlder they are reported in skipped, and without
// it a fallbackRefusedError is returned. Unversioned commits never take part in version comparison.
func selectVersion(versions []agentVersion, unavailable []unavailableCandidate, constraint string, allowYanked, allowOlder bool) (versionChoice, error) {
	c, err := version.NewConstraint(constraint)
	if err != nil {
		return versionChoice{}, fmt.Errorf("invalid version constraint %q: %w", constraint, err)
	}

	var best *version.Version
	var bestRaw string
	var newer []matchedCandidate
	var others []unavailableCandidate
	var unversioned []unavailableCandidate
	listed := map[string]bool{}
	for _, v := range versions {
		listed[v.Version] = true
		if v.Yanked && !allowYanked {
			continue
		}

		parsed, err := version.NewVersion(v.Version)
		if err != nil || !c.Check(parsed) {
			continue
		}

		if v.Blocked {
			newer = append(newer, matchedCandidate{parsed: parsed, cand: unavailableCandidate{Version: v.Version, Status: statusBlocked, Reasons: v.BlockReasons}})
			continue
		}

		if best == nil || parsed.GreaterThan(best) {
			best = parsed
			bestRaw = v.Version
		}
	}

	for _, cand := range unavailable {
		if cand.Version == "" {
			if isRefusal(cand.Status) {
				unversioned = append(unversioned, cand)
			}

			continue
		}

		if listed[cand.Version] {
			continue
		}

		parsed, err := version.NewVersion(cand.Version)
		if err != nil || !c.Check(parsed) {
			continue
		}

		listed[cand.Version] = true
		if isRefusal(cand.Status) {
			newer = append(newer, matchedCandidate{parsed: parsed, cand: cand})
		} else {
			others = append(others, cand)
		}
	}

	var above []matchedCandidate
	for _, m := range newer {
		if best == nil || m.parsed.GreaterThan(best) {
			above = append(above, m)
		}
	}

	sort.SliceStable(above, func(i, j int) bool {
		return above[i].parsed.GreaterThan(above[j].parsed)
	})

	if best == nil {
		if len(above) > 0 && above[0].cand.Status == statusBlocked {
			return versionChoice{}, &blockedVersionError{version: above[0].cand.Version, reasons: above[0].cand.Reasons}
		}

		var visible []unavailableCandidate
		for _, m := range above {
			visible = append(visible, m.cand)
		}

		return versionChoice{}, &noVersionError{constraint: constraint, versions: versions, unavailable: append(visible, others...), unversioned: unversioned}
	}

	if len(above) == 0 {
		return versionChoice{version: bestRaw}, nil
	}

	cands := make([]unavailableCandidate, 0, len(above))
	for _, m := range above {
		cands = append(cands, m.cand)
	}

	if allowOlder {
		return versionChoice{version: bestRaw, skipped: cands}, nil
	}

	return versionChoice{}, &fallbackRefusedError{constraint: constraint, chosen: bestRaw, newer: cands, unversioned: unversioned}
}

// resolveVersion returns the highest unblocked version satisfying the constraint. Yanked versions
// are skipped unless allowYanked is set. When the only matches are blocked, it returns a
// blockedVersionError for the highest one so the Jev reasons reach the user.
func resolveVersion(versions []agentVersion, constraint string, allowYanked bool) (string, error) {
	choice, err := selectVersion(versions, nil, constraint, allowYanked, true)
	if err != nil {
		return "", err
	}

	return choice.version, nil
}
