/*
Copyright 2026 Tony Owens.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package policy resolves ScanPolicy resources into an effective blocking decision for a
// Version. Every function here is pure: no client, no context, no I/O. The version
// controller supplies the already-listed policies and the already-produced findings.
package policy

import (
	"cmp"
	"slices"
	"strings"
	"time"

	goversion "github.com/hashicorp/go-version"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
)

// The scan types an exemption can be scoped to.
const (
	ScanTypeBinary = "binary"
	ScanTypeSource = "source"
	ScanTypeModule = "module"
	ScanTypeAgent  = "agent"
)

// wildcard is the only pattern supported in exemption and target reference lists.
// Matching is otherwise exact; no prefix globs or regular expressions are accepted.
const wildcard = "*"

// severityNone disables blocking entirely for the matched Versions.
const severityNone = "NONE"

// thresholdDisabled is the resolved threshold that never blocks.
const thresholdDisabled = 0

// severityRanks orders the severities Trivy emits. A higher rank is more severe.
var severityRanks = map[string]int{
	"UNKNOWN":  1,
	"LOW":      2,
	"MEDIUM":   3,
	"HIGH":     4,
	"CRITICAL": 5,
}

// SeverityRank returns the ordered rank of a Trivy severity string. Unrecognized values
// rank as UNKNOWN so that an unexpected severity is never silently treated as harmless.
func SeverityRank(severity string) int {
	if rank, ok := severityRanks[strings.ToUpper(strings.TrimSpace(severity))]; ok {
		return rank
	}

	return severityRanks["UNKNOWN"]
}

// Resolved is the effective policy applied to a single Version.
type Resolved struct {
	// PolicyName is the name of the winning ScanPolicy, or empty when no policy matched
	// and the controller flags supplied the threshold.
	PolicyName string
	// Threshold is the minimum severity rank that blocks reconciliation. A value of
	// thresholdDisabled means no finding blocks.
	Threshold int
	// Exemptions are the unexpired exemptions from the winning policy.
	Exemptions []opendepotv1alpha1.ScanExemption
	// NextExpiry is the earliest future exemption expiry, used to schedule a requeue so
	// that an expired exemption re-enforces without waiting for an unrelated event.
	NextExpiry *time.Time
}

// Blocks reports whether a finding of the given severity blocks reconciliation.
func (r Resolved) Blocks(severity string) bool {
	if r.Threshold == thresholdDisabled {
		return false
	}

	return SeverityRank(severity) >= r.Threshold
}

// baselineThreshold converts the controller's --scan-block-on-critical and
// --scan-block-on-high flags into a severity rank threshold.
func baselineThreshold(blockOnCritical, blockOnHigh bool) int {
	if blockOnHigh {
		return severityRanks["HIGH"]
	}

	if blockOnCritical {
		return severityRanks["CRITICAL"]
	}

	return thresholdDisabled
}

// matchList reports whether candidate matches an exemption or target reference list.
// An empty list matches everything, the single literal "*" matches everything, and every
// other entry is compared for exact case insensitive equality.
func matchList(values []string, candidate string) bool {
	if len(values) == 0 {
		return true
	}

	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == wildcard {
			return true
		}

		if strings.EqualFold(trimmed, candidate) {
			return true
		}
	}

	return false
}

// resourceName returns the Module, Provider, Skill, or Agent name a Version belongs to, preferring
// the labels applied by the owning controllers and falling back to the inherited config or source
// reference for Versions created before those labels existed.
func resourceName(version *opendepotv1alpha1.Version) string {
	switch version.Spec.Type {
	case opendepotv1alpha1.OpenDepotModule:
		if name := version.Labels["opendepot.defdev.io/module"]; name != "" {
			return name
		}

		if version.Spec.ModuleConfigRef != nil && version.Spec.ModuleConfigRef.Name != nil {
			return *version.Spec.ModuleConfigRef.Name
		}
	case opendepotv1alpha1.OpenDepotProvider:
		if name := version.Labels["opendepot.defdev.io/provider"]; name != "" {
			return name
		}

		if version.Spec.ProviderConfigRef != nil && version.Spec.ProviderConfigRef.Name != nil {
			return *version.Spec.ProviderConfigRef.Name
		}
	case opendepotv1alpha1.OpenDepotSkill, opendepotv1alpha1.OpenDepotAgent:
		if name := version.Labels["opendepot.defdev.io/"+strings.ToLower(version.Spec.Type)]; name != "" {
			return name
		}

		if version.Spec.AgentSourceRef != nil && version.Spec.AgentSourceRef.Name != nil {
			return *version.Spec.AgentSourceRef.Name
		}
	}

	return ""
}

// matchesTargetRef reports whether a single target reference selects the Version.
func matchesTargetRef(ref opendepotv1alpha1.ScanPolicyTargetRef, version *opendepotv1alpha1.Version) bool {
	if !strings.EqualFold(ref.Kind, version.Spec.Type) {
		return false
	}

	if !matchList([]string{ref.Name}, resourceName(version)) {
		return false
	}

	constraintExpr := strings.TrimSpace(ref.Versions)
	if constraintExpr == "" {
		return true
	}

	constraints, err := goversion.NewConstraint(constraintExpr)
	if err != nil {
		return false
	}

	candidate, err := goversion.NewVersion(version.Spec.Version)
	if err != nil {
		return false
	}

	// Constraints returned from goversion.NewConstraint use AND semantics, so the
	// version must satisfy the full expression (e.g. >= 1.0.0, < 2.0.0).
	return constraints.Check(candidate)
}

// Matches reports whether a ScanPolicy applies to a Version. A policy with neither a
// selector nor any target references applies to every Version in its namespace. When both
// are set the policy matches if either does, so a broad selector can be widened with
// surgical references.
//
// An invalid selector returns an error and never matches, so a malformed policy cannot
// silently widen or narrow enforcement.
func Matches(policy *opendepotv1alpha1.ScanPolicy, version *opendepotv1alpha1.Version) (bool, error) {
	if policy.Spec.Selector == nil && len(policy.Spec.TargetRefs) == 0 {
		return true, nil
	}

	if policy.Spec.Selector != nil {
		selector, err := metav1.LabelSelectorAsSelector(policy.Spec.Selector)
		if err != nil {
			return false, err
		}

		if selector.Matches(labels.Set(version.Labels)) {
			return true, nil
		}
	}

	for _, ref := range policy.Spec.TargetRefs {
		if matchesTargetRef(ref, version) {
			return true, nil
		}
	}

	return false, nil
}

// Select returns the single ScanPolicy that governs a Version. The highest priority policy
// wins outright and supplies both the threshold and the exemption set; lower priority
// policies are shadowed entirely rather than merged, so a second policy can never be used
// to quietly widen the exemptions granted by the first.
//
// Ties are broken by the oldest creation timestamp then by name ascending, so the outcome
// never depends on the order the API server returned the list in. Policies with an invalid
// selector are skipped and reported in the returned error slice.
func Select(policies []opendepotv1alpha1.ScanPolicy, version *opendepotv1alpha1.Version) (*opendepotv1alpha1.ScanPolicy, []error) {
	var errs []error
	matched := make([]opendepotv1alpha1.ScanPolicy, 0, len(policies))

	for _, policy := range policies {
		ok, err := Matches(&policy, version)
		if err != nil {
			errs = append(errs, err)
			continue
		}

		if ok {
			matched = append(matched, policy)
		}
	}

	if len(matched) == 0 {
		return nil, errs
	}

	slices.SortStableFunc(matched, func(a, b opendepotv1alpha1.ScanPolicy) int {
		if diff := cmp.Compare(b.Spec.Priority, a.Spec.Priority); diff != 0 {
			return diff
		}

		if diff := a.CreationTimestamp.Time.Compare(b.CreationTimestamp.Time); diff != 0 {
			return diff
		}

		return cmp.Compare(a.Name, b.Name)
	})

	return &matched[0], errs
}

// Resolve folds the winning policy and the controller flags into the effective decision for
// a Version. The flags are the baseline: when no policy matched, or when the winning policy
// leaves severityThreshold unset, the flags remain in effect unchanged.
func Resolve(winner *opendepotv1alpha1.ScanPolicy, blockOnCritical, blockOnHigh bool, now time.Time) Resolved {
	resolved := Resolved{Threshold: baselineThreshold(blockOnCritical, blockOnHigh)}
	if winner == nil {
		return resolved
	}

	resolved.PolicyName = winner.Name

	switch threshold := strings.ToUpper(strings.TrimSpace(winner.Spec.SeverityThreshold)); threshold {
	case "":
		// Leave the baseline in effect.
	case severityNone:
		resolved.Threshold = thresholdDisabled
	default:
		resolved.Threshold = SeverityRank(threshold)
	}

	for _, exemption := range winner.Spec.Exemptions {
		if exemption.Expires == nil {
			resolved.Exemptions = append(resolved.Exemptions, exemption)
			continue
		}

		if !exemption.Expires.Time.After(now) {
			continue
		}

		resolved.Exemptions = append(resolved.Exemptions, exemption)

		if resolved.NextExpiry == nil || exemption.Expires.Time.Before(*resolved.NextExpiry) {
			expiry := exemption.Expires.Time
			resolved.NextExpiry = &expiry
		}
	}

	return resolved
}

// covers reports whether an exemption covers a finding produced by the given scan type.
// Every populated field must match; omitted fields match everything.
func covers(exemption opendepotv1alpha1.ScanExemption, finding opendepotv1alpha1.SecurityFinding, scanType string) bool {
	return matchList(exemption.ScanTypes, scanType) &&
		matchList(exemption.Severities, finding.Severity) &&
		matchList(exemption.VulnerabilityIDs, finding.VulnerabilityID) &&
		matchList(exemption.PkgNames, finding.PkgName)
}

// Apply annotates findings with the exemption that covered them and returns the first
// finding that still blocks reconciliation, or nil when none do.
//
// The annotated findings are always returned, including when a finding blocks, so that the
// caller can persist the full auditable result alongside the blocking status. Exempted
// findings are marked rather than removed so they stay visible in the Registry Explorer.
func Apply(
	resolved Resolved,
	findings []opendepotv1alpha1.SecurityFinding,
	scanType string,
) ([]opendepotv1alpha1.SecurityFinding, *opendepotv1alpha1.SecurityFinding) {
	if findings == nil {
		return nil, nil
	}

	annotated := make([]opendepotv1alpha1.SecurityFinding, len(findings))
	copy(annotated, findings)

	var blocking *opendepotv1alpha1.SecurityFinding
	for i := range annotated {
		annotated[i].Exempted = false
		annotated[i].ExemptionReason = ""
		annotated[i].ExemptedBy = ""

		for _, exemption := range resolved.Exemptions {
			if !covers(exemption, annotated[i], scanType) {
				continue
			}

			annotated[i].Exempted = true
			annotated[i].ExemptionReason = exemption.Reason
			annotated[i].ExemptedBy = resolved.PolicyName

			break
		}

		if blocking != nil || annotated[i].Exempted {
			continue
		}

		if resolved.Blocks(annotated[i].Severity) {
			blocking = &annotated[i]
		}
	}

	return annotated, blocking
}

// CountExemptions splits a policy's exemptions into those currently in effect and those
// whose expiry has passed. It backs the ScanPolicy status without duplicating the expiry
// rule that Resolve applies.
func CountExemptions(policy *opendepotv1alpha1.ScanPolicy, now time.Time) (active int, expired int) {
	for _, exemption := range policy.Spec.Exemptions {
		if exemption.Expires != nil && !exemption.Expires.Time.After(now) {
			expired++
			continue
		}

		active++
	}

	return active, expired
}
