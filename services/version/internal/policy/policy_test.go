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

package policy

import (
	"reflect"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
)

const (
	digestA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	digestB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	digestC = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
)

func providerVersion(name string, version string) *opendepotv1alpha1.Version {
	return &opendepotv1alpha1.Version{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name + "-" + version,
			Namespace: "opendepot",
			Labels: map[string]string{
				"opendepot.defdev.io/provider": name,
			},
		},
		Spec: opendepotv1alpha1.VersionSpec{
			Type:    opendepotv1alpha1.OpenDepotProvider,
			Version: version,
		},
	}
}

func agentVersion(kind string, name string, version string) *opendepotv1alpha1.Version {
	return &opendepotv1alpha1.Version{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name + "-" + version,
			Namespace: "opendepot",
			Labels: map[string]string{
				"opendepot.defdev.io/" + kind: name,
			},
		},
		Spec: opendepotv1alpha1.VersionSpec{
			Type:    map[string]string{"skill": opendepotv1alpha1.OpenDepotSkill, "agent": opendepotv1alpha1.OpenDepotAgent}[kind],
			Version: version,
		},
	}
}

func policyNamed(name string, priority int) opendepotv1alpha1.ScanPolicy {
	return opendepotv1alpha1.ScanPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			Namespace:         "opendepot",
			CreationTimestamp: metav1.NewTime(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)),
		},
		Spec: opendepotv1alpha1.ScanPolicySpec{Priority: priority},
	}
}

func TestSeverityRank(t *testing.T) {
	tests := []struct {
		severity string
		want     int
	}{
		{"CRITICAL", 5},
		{"critical", 5},
		{"  HIGH ", 4},
		{"MEDIUM", 3},
		{"LOW", 2},
		{"UNKNOWN", 1},
		{"", 1},
		{"NOT_A_SEVERITY", 1},
	}

	for _, tt := range tests {
		if got := SeverityRank(tt.severity); got != tt.want {
			t.Errorf("SeverityRank(%q) = %d, want %d", tt.severity, got, tt.want)
		}
	}
}

func TestBaselineThreshold(t *testing.T) {
	tests := []struct {
		name       string
		onCritical bool
		onHigh     bool
		want       int
	}{
		{"both flags off disables blocking", false, false, thresholdDisabled},
		{"critical only blocks at CRITICAL", true, false, 5},
		{"high only blocks at HIGH and above", false, true, 4},
		{"both flags block at HIGH and above", true, true, 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := baselineThreshold(tt.onCritical, tt.onHigh); got != tt.want {
				t.Errorf("baselineThreshold(%v, %v) = %d, want %d", tt.onCritical, tt.onHigh, got, tt.want)
			}
		})
	}
}

func TestMatchList(t *testing.T) {
	tests := []struct {
		name      string
		values    []string
		candidate string
		want      bool
	}{
		{"empty list matches everything", nil, "CVE-2024-1234", true},
		{"wildcard matches everything", []string{"*"}, "CVE-2024-1234", true},
		{"exact match", []string{"CVE-2024-1234"}, "CVE-2024-1234", true},
		{"case insensitive match", []string{"aws-0057"}, "AWS-0057", true},
		{"surrounding whitespace tolerated", []string{" aws-0057 "}, "aws-0057", true},
		{"non match", []string{"CVE-2024-1234"}, "CVE-2024-9999", false},
		{"prefix glob is not supported", []string{"CVE-2024-*"}, "CVE-2024-1234", false},
		{"wildcard among others still matches", []string{"CVE-1", "*"}, "CVE-2", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := matchList(tt.values, tt.candidate); got != tt.want {
				t.Errorf("matchList(%v, %q) = %v, want %v", tt.values, tt.candidate, got, tt.want)
			}
		})
	}
}

func TestMatchesEmptySpecMatchesEverything(t *testing.T) {
	policy := policyNamed("catch-all", 0)

	got, err := Matches(&policy, providerVersion("aws", "5.0.0"))
	if err != nil {
		t.Fatalf("Matches returned error: %v", err)
	}

	if !got {
		t.Error("a policy with no selector and no targetRefs must match every Version")
	}
}

func TestMatchesSelector(t *testing.T) {
	policy := policyNamed("by-selector", 0)
	policy.Spec.Selector = &metav1.LabelSelector{
		MatchLabels: map[string]string{"opendepot.defdev.io/provider": "aws"},
	}

	if got, _ := Matches(&policy, providerVersion("aws", "5.0.0")); !got {
		t.Error("selector should match the aws provider Version")
	}

	if got, _ := Matches(&policy, providerVersion("azurerm", "4.0.0")); got {
		t.Error("selector should not match the azurerm provider Version")
	}
}

func TestMatchesInvalidSelectorErrorsAndDoesNotMatch(t *testing.T) {
	policy := policyNamed("broken", 0)
	policy.Spec.Selector = &metav1.LabelSelector{
		MatchExpressions: []metav1.LabelSelectorRequirement{
			{Key: "opendepot.defdev.io/provider", Operator: "NotARealOperator"},
		},
	}

	got, err := Matches(&policy, providerVersion("aws", "5.0.0"))
	if err == nil {
		t.Fatal("an invalid selector must return an error")
	}

	if got {
		t.Error("an invalid selector must not match")
	}
}

func TestMatchesTargetRefs(t *testing.T) {
	tests := []struct {
		name    string
		ref     opendepotv1alpha1.ScanPolicyTargetRef
		version *opendepotv1alpha1.Version
		want    bool
	}{
		{
			name:    "exact name and no constraint",
			ref:     opendepotv1alpha1.ScanPolicyTargetRef{Kind: "Provider", Name: "aws"},
			version: providerVersion("aws", "5.0.0"),
			want:    true,
		},
		{
			name:    "wildcard name matches every provider",
			ref:     opendepotv1alpha1.ScanPolicyTargetRef{Kind: "Provider", Name: "*"},
			version: providerVersion("azurerm", "4.0.0"),
			want:    true,
		},
		{
			name:    "kind mismatch",
			ref:     opendepotv1alpha1.ScanPolicyTargetRef{Kind: "Module", Name: "aws"},
			version: providerVersion("aws", "5.0.0"),
			want:    false,
		},
		{
			name:    "version constraint satisfied",
			ref:     opendepotv1alpha1.ScanPolicyTargetRef{Kind: "Provider", Name: "aws", Versions: ">= 5.0.0, < 6.0.0"},
			version: providerVersion("aws", "5.4.0"),
			want:    true,
		},
		{
			name:    "version constraint not satisfied",
			ref:     opendepotv1alpha1.ScanPolicyTargetRef{Kind: "Provider", Name: "aws", Versions: ">= 6.0.0"},
			version: providerVersion("aws", "5.4.0"),
			want:    false,
		},
		{
			name:    "invalid constraint never matches",
			ref:     opendepotv1alpha1.ScanPolicyTargetRef{Kind: "Provider", Name: "aws", Versions: "not a constraint"},
			version: providerVersion("aws", "5.4.0"),
			want:    false,
		},
		{
			name:    "skill kind and name match",
			ref:     opendepotv1alpha1.ScanPolicyTargetRef{Kind: "Skill", Name: "pdf-tools"},
			version: agentVersion("skill", "pdf-tools", "1.0.0"),
			want:    true,
		},
		{
			name:    "skill kind does not match an agent",
			ref:     opendepotv1alpha1.ScanPolicyTargetRef{Kind: "Skill", Name: "reviewer"},
			version: agentVersion("agent", "reviewer", "1.0.0"),
			want:    false,
		},
		{
			name:    "agent kind and wildcard name match",
			ref:     opendepotv1alpha1.ScanPolicyTargetRef{Kind: "Agent", Name: "*", Versions: ">= 1.0.0"},
			version: agentVersion("agent", "reviewer", "1.2.0"),
			want:    true,
		},
		{
			name:    "agent version constraint not satisfied",
			ref:     opendepotv1alpha1.ScanPolicyTargetRef{Kind: "Agent", Name: "reviewer", Versions: ">= 2.0.0"},
			version: agentVersion("agent", "reviewer", "1.2.0"),
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy := policyNamed("by-ref", 0)
			policy.Spec.TargetRefs = []opendepotv1alpha1.ScanPolicyTargetRef{tt.ref}

			got, err := Matches(&policy, tt.version)
			if err != nil {
				t.Fatalf("Matches returned error: %v", err)
			}

			if got != tt.want {
				t.Errorf("Matches = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMatchesTargetRefFallsBackToSourceRef(t *testing.T) {
	name := "pdf-tools"
	version := agentVersion("skill", "pdf-tools", "1.0.0")
	version.Labels = nil
	version.Spec.AgentSourceRef = &opendepotv1alpha1.AgentSourceConfig{Name: &name}

	policy := policyNamed("by-ref", 0)
	policy.Spec.TargetRefs = []opendepotv1alpha1.ScanPolicyTargetRef{{Kind: "Skill", Name: "pdf-tools"}}

	if got, _ := Matches(&policy, version); !got {
		t.Error("target reference should fall back to the agent source reference when labels are absent")
	}
}

func TestResourceNameForAgents(t *testing.T) {
	name := "reviewer"
	tests := []struct {
		name    string
		version *opendepotv1alpha1.Version
		want    string
	}{
		{name: "skill label", version: agentVersion("skill", "pdf-tools", "1.0.0"), want: "pdf-tools"},
		{name: "agent label", version: agentVersion("agent", "reviewer", "1.0.0"), want: "reviewer"},
		{
			name: "agent source ref fallback",
			version: &opendepotv1alpha1.Version{
				Spec: opendepotv1alpha1.VersionSpec{
					Type:           opendepotv1alpha1.OpenDepotAgent,
					AgentSourceRef: &opendepotv1alpha1.AgentSourceConfig{Name: &name},
				},
			},
			want: "reviewer",
		},
		{
			name:    "no label or source ref",
			version: &opendepotv1alpha1.Version{Spec: opendepotv1alpha1.VersionSpec{Type: opendepotv1alpha1.OpenDepotSkill}},
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resourceName(tt.version); got != tt.want {
				t.Errorf("resourceName = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestApplyAgentScanType(t *testing.T) {
	resolved := Resolve(nil, true, false, time.Now())
	findings := []opendepotv1alpha1.SecurityFinding{{VulnerabilityID: "R-1", Severity: "CRITICAL"}}

	annotated, blocking := Apply(resolved, findings, ScanTypeAgent)
	if blocking == nil || blocking.VulnerabilityID != "R-1" {
		t.Fatalf("expected agent-scan CRITICAL finding to block, got %v", blocking)
	}

	if annotated[0].Exempted {
		t.Error("finding should not be exempted without an exemption")
	}
}

func TestMatchesTargetRefFallsBackToConfigRef(t *testing.T) {
	name := "aws"
	version := providerVersion("aws", "5.0.0")
	version.Labels = nil
	version.Spec.ProviderConfigRef = &opendepotv1alpha1.ProviderConfig{Name: &name}

	policy := policyNamed("by-ref", 0)
	policy.Spec.TargetRefs = []opendepotv1alpha1.ScanPolicyTargetRef{{Kind: "Provider", Name: "aws"}}

	if got, _ := Matches(&policy, version); !got {
		t.Error("target reference should fall back to the inherited config reference when labels are absent")
	}
}

func TestSelectHighestPriorityWins(t *testing.T) {
	low := policyNamed("low", 10)
	high := policyNamed("high", 100)

	winner, errs := Select([]opendepotv1alpha1.ScanPolicy{low, high}, providerVersion("aws", "5.0.0"))
	if len(errs) != 0 {
		t.Fatalf("Select returned errors: %v", errs)
	}

	if winner == nil || winner.Name != "high" {
		t.Errorf("expected the highest priority policy to win, got %v", winner)
	}
}

func TestSelectTieBreaksOnCreationTimestampThenName(t *testing.T) {
	older := policyNamed("zeta", 5)
	older.CreationTimestamp = metav1.NewTime(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	newer := policyNamed("alpha", 5)
	newer.CreationTimestamp = metav1.NewTime(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC))

	winner, _ := Select([]opendepotv1alpha1.ScanPolicy{newer, older}, providerVersion("aws", "5.0.0"))
	if winner == nil || winner.Name != "zeta" {
		t.Errorf("expected the oldest policy to win the tie, got %v", winner)
	}

	sameAge := policyNamed("beta", 5)
	alsoSameAge := policyNamed("alpha", 5)

	winner, _ = Select([]opendepotv1alpha1.ScanPolicy{sameAge, alsoSameAge}, providerVersion("aws", "5.0.0"))
	if winner == nil || winner.Name != "alpha" {
		t.Errorf("expected the alphabetically first policy to win the tie, got %v", winner)
	}
}

func TestSelectShadowsRatherThanMerges(t *testing.T) {
	lenient := policyNamed("lenient", 10)
	lenient.Spec.Exemptions = []opendepotv1alpha1.ScanExemption{
		{VulnerabilityIDs: []string{"CVE-2024-1234"}, Reason: "accepted risk"},
	}

	strict := policyNamed("strict", 100)

	winner, _ := Select([]opendepotv1alpha1.ScanPolicy{lenient, strict}, providerVersion("aws", "5.0.0"))
	if winner == nil || winner.Name != "strict" {
		t.Fatalf("expected the strict policy to win, got %v", winner)
	}

	resolved := Resolve(winner, true, false, time.Now())
	if len(resolved.Exemptions) != 0 {
		t.Errorf("the shadowed policy's exemptions must not leak into the winner, got %d", len(resolved.Exemptions))
	}
}

func TestSelectNoMatchReturnsNil(t *testing.T) {
	policy := policyNamed("other", 0)
	policy.Spec.TargetRefs = []opendepotv1alpha1.ScanPolicyTargetRef{{Kind: "Provider", Name: "azurerm"}}

	winner, errs := Select([]opendepotv1alpha1.ScanPolicy{policy}, providerVersion("aws", "5.0.0"))
	if len(errs) != 0 {
		t.Fatalf("Select returned errors: %v", errs)
	}

	if winner != nil {
		t.Errorf("expected no winner, got %v", winner)
	}
}

func TestResolveFallsBackToFlags(t *testing.T) {
	resolved := Resolve(nil, true, false, time.Now())
	if resolved.Threshold != severityRanks["CRITICAL"] {
		t.Errorf("expected the CRITICAL flag baseline, got %d", resolved.Threshold)
	}

	if resolved.PolicyName != "" {
		t.Errorf("expected no policy name, got %q", resolved.PolicyName)
	}
}

func TestResolveThresholdOverride(t *testing.T) {
	tests := []struct {
		name      string
		threshold string
		onHigh    bool
		want      int
	}{
		{"unset leaves the baseline in effect", "", true, 4},
		{"NONE disables blocking", "NONE", true, thresholdDisabled},
		{"CRITICAL relaxes the HIGH baseline", "CRITICAL", true, 5},
		{"MEDIUM tightens the HIGH baseline", "MEDIUM", true, 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy := policyNamed("override", 0)
			policy.Spec.SeverityThreshold = tt.threshold

			resolved := Resolve(&policy, false, tt.onHigh, time.Now())
			if resolved.Threshold != tt.want {
				t.Errorf("Resolve threshold = %d, want %d", resolved.Threshold, tt.want)
			}
		})
	}
}

func TestResolveDropsExpiredExemptionsAndReportsNextExpiry(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	policy := policyNamed("expiring", 0)
	policy.Spec.Exemptions = []opendepotv1alpha1.ScanExemption{
		{VulnerabilityIDs: []string{"CVE-EXPIRED"}, Reason: "expired", Expires: &metav1.Time{Time: now.Add(-time.Hour)}},
		{VulnerabilityIDs: []string{"CVE-LATER"}, Reason: "later", Expires: &metav1.Time{Time: now.Add(48 * time.Hour)}},
		{VulnerabilityIDs: []string{"CVE-SOONER"}, Reason: "sooner", Expires: &metav1.Time{Time: now.Add(2 * time.Hour)}},
		{VulnerabilityIDs: []string{"CVE-FOREVER"}, Reason: "no expiry"},
	}

	resolved := Resolve(&policy, true, false, now)
	if len(resolved.Exemptions) != 3 {
		t.Fatalf("expected 3 unexpired exemptions, got %d", len(resolved.Exemptions))
	}

	if resolved.NextExpiry == nil || !resolved.NextExpiry.Equal(now.Add(2*time.Hour)) {
		t.Errorf("expected the earliest future expiry, got %v", resolved.NextExpiry)
	}
}

func TestApplyExemptsAndBlocks(t *testing.T) {
	findings := []opendepotv1alpha1.SecurityFinding{
		{VulnerabilityID: "CVE-2024-1111", PkgName: "stdlib", Severity: "CRITICAL"},
		{VulnerabilityID: "CVE-2024-2222", PkgName: "golang.org/x/net", Severity: "CRITICAL"},
		{VulnerabilityID: "CVE-2024-3333", PkgName: "stdlib", Severity: "LOW"},
	}

	resolved := Resolved{
		PolicyName: "exempt-stdlib",
		Threshold:  severityRanks["CRITICAL"],
		Exemptions: []opendepotv1alpha1.ScanExemption{
			{PkgNames: []string{"stdlib"}, Reason: "patched by the base image"},
		},
	}

	annotated, blocking := Apply(resolved, findings, ScanTypeBinary)
	if len(annotated) != 3 {
		t.Fatalf("expected all findings returned, got %d", len(annotated))
	}

	if !annotated[0].Exempted || annotated[0].ExemptionReason != "patched by the base image" || annotated[0].ExemptedBy != "exempt-stdlib" {
		t.Errorf("expected the stdlib finding to be annotated as exempted, got %+v", annotated[0])
	}

	if annotated[1].Exempted {
		t.Error("the golang.org/x/net finding must not be exempted")
	}

	if blocking == nil || blocking.VulnerabilityID != "CVE-2024-2222" {
		t.Errorf("expected the non-exempt CRITICAL finding to block, got %v", blocking)
	}
}

func TestApplyDoesNotMutateInput(t *testing.T) {
	findings := []opendepotv1alpha1.SecurityFinding{
		{VulnerabilityID: "CVE-2024-1111", PkgName: "stdlib", Severity: "CRITICAL"},
	}

	resolved := Resolved{
		PolicyName: "exempt-all",
		Threshold:  severityRanks["CRITICAL"],
		Exemptions: []opendepotv1alpha1.ScanExemption{{VulnerabilityIDs: []string{"*"}, Reason: "all"}},
	}

	if _, blocking := Apply(resolved, findings, ScanTypeBinary); blocking != nil {
		t.Fatalf("expected no blocking finding, got %v", blocking)
	}

	if findings[0].Exempted {
		t.Error("Apply must not mutate the caller's findings slice")
	}
}

func TestApplyScanTypeScoping(t *testing.T) {
	findings := []opendepotv1alpha1.SecurityFinding{
		{VulnerabilityID: "CVE-2024-1111", PkgName: "stdlib", Severity: "CRITICAL"},
	}

	resolved := Resolved{
		PolicyName: "binary-only",
		Threshold:  severityRanks["CRITICAL"],
		Exemptions: []opendepotv1alpha1.ScanExemption{
			{VulnerabilityIDs: []string{"CVE-2024-1111"}, ScanTypes: []string{ScanTypeBinary}, Reason: "binary only"},
		},
	}

	if _, blocking := Apply(resolved, findings, ScanTypeBinary); blocking != nil {
		t.Errorf("expected the binary scan to be exempted, got %v", blocking)
	}

	if _, blocking := Apply(resolved, findings, ScanTypeSource); blocking == nil {
		t.Error("expected the source scan to still block")
	}
}

func TestApplyThresholdDisabledNeverBlocks(t *testing.T) {
	findings := []opendepotv1alpha1.SecurityFinding{
		{VulnerabilityID: "CVE-2024-1111", Severity: "CRITICAL"},
	}

	if _, blocking := Apply(Resolved{Threshold: thresholdDisabled}, findings, ScanTypeBinary); blocking != nil {
		t.Errorf("a disabled threshold must never block, got %v", blocking)
	}
}

func TestApplyBlocksAtOrAboveThreshold(t *testing.T) {
	tests := []struct {
		severity  string
		threshold int
		blocks    bool
	}{
		{"CRITICAL", severityRanks["HIGH"], true},
		{"HIGH", severityRanks["HIGH"], true},
		{"MEDIUM", severityRanks["HIGH"], false},
		{"CRITICAL", severityRanks["CRITICAL"], true},
		{"HIGH", severityRanks["CRITICAL"], false},
	}

	for _, tt := range tests {
		findings := []opendepotv1alpha1.SecurityFinding{{VulnerabilityID: "CVE-1", Severity: tt.severity}}
		_, blocking := Apply(Resolved{Threshold: tt.threshold}, findings, ScanTypeSource)

		if (blocking != nil) != tt.blocks {
			t.Errorf("severity %s at threshold %d: blocks = %v, want %v", tt.severity, tt.threshold, blocking != nil, tt.blocks)
		}
	}
}

func TestApplyNilFindings(t *testing.T) {
	annotated, blocking := Apply(Resolved{Threshold: severityRanks["CRITICAL"]}, nil, ScanTypeSource)
	if annotated != nil || blocking != nil {
		t.Errorf("nil findings must produce nil results, got %v and %v", annotated, blocking)
	}
}

func TestCountExemptions(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	policy := policyNamed("counted", 0)
	policy.Spec.Exemptions = []opendepotv1alpha1.ScanExemption{
		{Reason: "no expiry"},
		{Reason: "future", Expires: &metav1.Time{Time: now.Add(time.Hour)}},
		{Reason: "past", Expires: &metav1.Time{Time: now.Add(-time.Hour)}},
	}

	active, expired := CountExemptions(&policy, now)
	if active != 2 || expired != 1 {
		t.Errorf("CountExemptions = (%d, %d), want (2, 1)", active, expired)
	}
}

func TestResolveJevExemptionsDropExpiredAndTrackNextExpiry(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	policy := policyNamed("jev-expiring", 0)
	policy.Spec.JevExemptions = []opendepotv1alpha1.JevExemption{
		{ContentDigest: digestA, Reason: "expired", Expires: &metav1.Time{Time: now.Add(-time.Hour)}},
		{ContentDigest: digestB, Reason: "later", Expires: &metav1.Time{Time: now.Add(48 * time.Hour)}},
		{ContentDigest: digestC, Reason: "no expiry"},
	}

	resolved := Resolve(&policy, true, false, now)
	if len(resolved.JevExemptions) != 2 {
		t.Fatalf("expected 2 unexpired Jev exemptions, got %d", len(resolved.JevExemptions))
	}

	if resolved.NextExpiry == nil || !resolved.NextExpiry.Equal(now.Add(48*time.Hour)) {
		t.Errorf("expected the Jev expiry as NextExpiry, got %v", resolved.NextExpiry)
	}
}

func TestResolveNoExpiryJevExemptionDoesNotSetNextExpiry(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	policy := policyNamed("jev-forever", 0)
	policy.Spec.JevExemptions = []opendepotv1alpha1.JevExemption{
		{ContentDigest: digestA, Reason: "no expiry"},
	}

	resolved := Resolve(&policy, true, false, now)
	if len(resolved.JevExemptions) != 1 {
		t.Fatalf("expected the no-expiry Jev exemption to be kept, got %d", len(resolved.JevExemptions))
	}

	if resolved.NextExpiry != nil {
		t.Errorf("expected no NextExpiry for a no-expiry entry, got %v", resolved.NextExpiry)
	}
}

func TestResolveNextExpiryTakesEarliestAcrossLists(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	policy := policyNamed("both-lists", 0)
	policy.Spec.Exemptions = []opendepotv1alpha1.ScanExemption{
		{VulnerabilityIDs: []string{"CVE-LATER"}, Reason: "later", Expires: &metav1.Time{Time: now.Add(10 * time.Hour)}},
	}
	policy.Spec.JevExemptions = []opendepotv1alpha1.JevExemption{
		{ContentDigest: digestA, Reason: "sooner", Expires: &metav1.Time{Time: now.Add(2 * time.Hour)}},
	}

	resolved := Resolve(&policy, true, false, now)
	if resolved.NextExpiry == nil || !resolved.NextExpiry.Equal(now.Add(2*time.Hour)) {
		t.Errorf("expected the earliest expiry across both lists, got %v", resolved.NextExpiry)
	}

	policy.Spec.JevExemptions[0].Expires = &metav1.Time{Time: now.Add(20 * time.Hour)}
	resolved = Resolve(&policy, true, false, now)
	if resolved.NextExpiry == nil || !resolved.NextExpiry.Equal(now.Add(10*time.Hour)) {
		t.Errorf("expected the scanner exemption expiry when it is earliest, got %v", resolved.NextExpiry)
	}
}

func TestJevExemptionForMatchesExactDigest(t *testing.T) {
	resolved := Resolved{
		PolicyName: "jev-allow",
		JevExemptions: []opendepotv1alpha1.JevExemption{
			{ContentDigest: digestA, Reason: "reviewed"},
		},
	}

	if got := resolved.JevExemptionFor(digestA); got == nil || got.Reason != "reviewed" {
		t.Errorf("expected the exemption for the exact digest, got %v", got)
	}

	if got := resolved.JevExemptionFor(digestB); got != nil {
		t.Errorf("expected no exemption for a different digest, got %+v", got)
	}

	if got := resolved.JevExemptionFor(strings.ToUpper(digestA)); got != nil {
		t.Errorf("expected mixed case digest not to match, got %+v", got)
	}

	if got := resolved.JevExemptionFor(""); got != nil {
		t.Errorf("expected empty digest not to match, got %+v", got)
	}
}

func TestJevExemptionForRespectsPriorityShadowing(t *testing.T) {
	high := policyNamed("high", 10)
	low := policyNamed("low", 1)
	low.Spec.JevExemptions = []opendepotv1alpha1.JevExemption{
		{ContentDigest: digestA, Reason: "only the lower policy holds this"},
	}

	winner, _ := Select([]opendepotv1alpha1.ScanPolicy{low, high}, providerVersion("demo", "1.0.0"))
	if winner == nil || winner.Name != "high" {
		t.Fatalf("expected the high priority policy to win, got %v", winner)
	}

	resolved := Resolve(winner, true, false, time.Now())
	if got := resolved.JevExemptionFor(digestA); got != nil {
		t.Errorf("expected the shadowed policy's Jev exemption not to apply, got %+v", got)
	}
}

func TestCountJevExemptions(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	policy := policyNamed("jev-counted", 0)
	policy.Spec.JevExemptions = []opendepotv1alpha1.JevExemption{
		{ContentDigest: digestA, Reason: "no expiry"},
		{ContentDigest: digestB, Reason: "future", Expires: &metav1.Time{Time: now.Add(time.Hour)}},
		{ContentDigest: digestC, Reason: "past", Expires: &metav1.Time{Time: now.Add(-time.Hour)}},
	}

	active, expired := CountJevExemptions(&policy, now)
	if active != 2 || expired != 1 {
		t.Errorf("CountJevExemptions = (%d, %d), want (2, 1)", active, expired)
	}
}

func TestRankStartsWithSelectWinner(t *testing.T) {
	low := policyNamed("low", 10)
	high := policyNamed("high", 100)
	version := providerVersion("aws", "5.0.0")

	ranked, errs := Rank([]opendepotv1alpha1.ScanPolicy{low, high}, version)
	if len(errs) != 0 {
		t.Fatalf("Rank returned errors: %v", errs)
	}

	if len(ranked) != 2 || ranked[0].Name != "high" || ranked[1].Name != "low" {
		t.Errorf("Rank = %v, want high then low", ranked)
	}

	winner, _ := Select([]opendepotv1alpha1.ScanPolicy{low, high}, version)
	if winner == nil || winner.Name != ranked[0].Name {
		t.Errorf("Select winner %v does not match Rank[0] %s", winner, ranked[0].Name)
	}
}

func TestJevGovernance(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	future := &metav1.Time{Time: now.Add(time.Hour)}
	past := &metav1.Time{Time: now.Add(-time.Hour)}

	governing := policyNamed("governing", 100)
	governing.Spec.JevExemptions = []opendepotv1alpha1.JevExemption{
		{ContentDigest: digestA, Reason: "active", Expires: future},
		{ContentDigest: digestB, Reason: "lapsed", Expires: past},
	}

	shadowed := policyNamed("shadowed", 10)
	shadowed.Spec.JevExemptions = []opendepotv1alpha1.JevExemption{
		{ContentDigest: digestA, Reason: "not in force"},
		{ContentDigest: digestC, Reason: "not in force either"},
	}

	tests := []struct {
		name   string
		ranked []opendepotv1alpha1.ScanPolicy
		digest string
		want   opendepotv1alpha1.JevExemptionGovernance
	}{
		{
			name:   "no governing policy and no digest",
			digest: digestA,
			want:   opendepotv1alpha1.JevExemptionGovernance{MatchingState: opendepotv1alpha1.JevMatchingStateNone},
		},
		{
			name:   "empty digest reports noDigest even when a policy governs",
			ranked: []opendepotv1alpha1.ScanPolicy{governing},
			want: opendepotv1alpha1.JevExemptionGovernance{
				GoverningPolicy:   "governing",
				GoverningPriority: 100,
				MatchingState:     opendepotv1alpha1.JevMatchingStateNoDigest,
			},
		},
		{
			name:   "no entry matches",
			ranked: []opendepotv1alpha1.ScanPolicy{governing, shadowed},
			digest: digestB + "0",
			want: opendepotv1alpha1.JevExemptionGovernance{
				GoverningPolicy:   "governing",
				GoverningPriority: 100,
				MatchingState:     opendepotv1alpha1.JevMatchingStateNone,
			},
		},
		{
			name:   "unexpired entry in the governing policy is exempted",
			ranked: []opendepotv1alpha1.ScanPolicy{governing, shadowed},
			digest: digestA,
			want: opendepotv1alpha1.JevExemptionGovernance{
				GoverningPolicy:   "governing",
				GoverningPriority: 100,
				MatchingPolicy:    "governing",
				MatchingPriority:  100,
				MatchingExpires:   future,
				MatchingState:     opendepotv1alpha1.JevMatchingStateExempted,
			},
		},
		{
			name:   "expired entry in the governing policy is expired",
			ranked: []opendepotv1alpha1.ScanPolicy{governing, shadowed},
			digest: digestB,
			want: opendepotv1alpha1.JevExemptionGovernance{
				GoverningPolicy:   "governing",
				GoverningPriority: 100,
				MatchingPolicy:    "governing",
				MatchingPriority:  100,
				MatchingExpires:   past,
				MatchingState:     opendepotv1alpha1.JevMatchingStateExpired,
			},
		},
		{
			name:   "entry only in a non-governing policy is superseded",
			ranked: []opendepotv1alpha1.ScanPolicy{governing, shadowed},
			digest: digestC,
			want: opendepotv1alpha1.JevExemptionGovernance{
				GoverningPolicy:   "governing",
				GoverningPriority: 100,
				MatchingPolicy:    "shadowed",
				MatchingPriority:  10,
				MatchingState:     opendepotv1alpha1.JevMatchingStateSuperseded,
				Superseded:        true,
			},
		},
		{
			name:   "governing policy entry takes precedence over a superseded match",
			ranked: []opendepotv1alpha1.ScanPolicy{governing, shadowed},
			digest: digestA,
			want: opendepotv1alpha1.JevExemptionGovernance{
				GoverningPolicy:   "governing",
				GoverningPriority: 100,
				MatchingPolicy:    "governing",
				MatchingPriority:  100,
				MatchingExpires:   future,
				MatchingState:     opendepotv1alpha1.JevMatchingStateExempted,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := JevGovernance(tt.ranked, tt.digest, now)
			if got.GoverningPolicy != tt.want.GoverningPolicy || got.GoverningPriority != tt.want.GoverningPriority ||
				got.MatchingPolicy != tt.want.MatchingPolicy || got.MatchingPriority != tt.want.MatchingPriority ||
				got.MatchingState != tt.want.MatchingState || got.Superseded != tt.want.Superseded {
				t.Errorf("JevGovernance() = %+v, want %+v", got, tt.want)
			}

			if (got.MatchingExpires == nil) != (tt.want.MatchingExpires == nil) ||
				(got.MatchingExpires != nil && !got.MatchingExpires.Equal(tt.want.MatchingExpires)) {
				t.Errorf("JevGovernance() matchingExpires = %v, want %v", got.MatchingExpires, tt.want.MatchingExpires)
			}
		})
	}
}

func TestJevGovernanceAgreesWithOverlay(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	governing := policyNamed("governing", 100)
	governing.Spec.JevExemptions = []opendepotv1alpha1.JevExemption{
		{ContentDigest: digestA, Reason: "active", Expires: &metav1.Time{Time: now.Add(time.Hour)}},
		{ContentDigest: digestB, Reason: "lapsed", Expires: &metav1.Time{Time: now.Add(-time.Hour)}},
	}

	ranked, _ := Rank([]opendepotv1alpha1.ScanPolicy{governing}, providerVersion("demo", "1.0.0"))
	resolved := Resolve(&ranked[0], true, false, now)

	for _, digest := range []string{digestA, digestB, digestC} {
		governance := JevGovernance(ranked, digest, now)
		exempted := resolved.JevExemptionFor(digest) != nil
		if exempted != (governance.MatchingState == opendepotv1alpha1.JevMatchingStateExempted) {
			t.Errorf("digest %s: overlay exempted=%v but governance state=%s", digest, exempted, governance.MatchingState)
		}
	}
}

func TestJevUnblock(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	future := &metav1.Time{Time: now.Add(time.Hour)}
	past := &metav1.Time{Time: now.Add(-time.Hour)}

	governing := policyNamed("governing", 100)
	governing.Spec.JevExemptions = []opendepotv1alpha1.JevExemption{
		{ContentDigest: digestA, Reason: "active", Expires: future},
		{ContentDigest: digestB, Reason: "lapsed", Expires: past},
	}

	shadowed := policyNamed("shadowed", 10)
	shadowed.Spec.JevExemptions = []opendepotv1alpha1.JevExemption{
		{ContentDigest: digestC, Reason: "not in force"},
	}

	tests := []struct {
		name    string
		ranked  []opendepotv1alpha1.ScanPolicy
		digest  string
		blocked bool
		want    opendepotv1alpha1.JevUnblock
	}{
		{
			name:    "not blocked and no exemption is none",
			digest:  digestA,
			blocked: false,
			want:    opendepotv1alpha1.JevUnblock{State: opendepotv1alpha1.JevUnblockStateNone, Digest: digestA},
		},
		{
			name:    "not blocked with an expired governing entry is none",
			ranked:  []opendepotv1alpha1.ScanPolicy{governing},
			digest:  digestB,
			blocked: false,
			want: opendepotv1alpha1.JevUnblock{
				State:             opendepotv1alpha1.JevUnblockStateNone,
				Digest:            digestB,
				GoverningPolicy:   "governing",
				GoverningPriority: 100,
				MatchingPolicy:    "governing",
				MatchingPriority:  100,
				Expires:           past,
			},
		},
		{
			name:    "not blocked with an exemption in force is exempted",
			ranked:  []opendepotv1alpha1.ScanPolicy{governing},
			digest:  digestA,
			blocked: false,
			want: opendepotv1alpha1.JevUnblock{
				State:             opendepotv1alpha1.JevUnblockStateExempted,
				Digest:            digestA,
				GoverningPolicy:   "governing",
				GoverningPriority: 100,
				MatchingPolicy:    "governing",
				MatchingPriority:  100,
				Expires:           future,
			},
		},
		{
			name:    "blocked with an exemption in force is pending",
			ranked:  []opendepotv1alpha1.ScanPolicy{governing},
			digest:  digestA,
			blocked: true,
			want: opendepotv1alpha1.JevUnblock{
				State:             opendepotv1alpha1.JevUnblockStatePending,
				Digest:            digestA,
				GoverningPolicy:   "governing",
				GoverningPriority: 100,
				MatchingPolicy:    "governing",
				MatchingPriority:  100,
				Expires:           future,
			},
		},
		{
			name:    "blocked with an expired governing entry is expired",
			ranked:  []opendepotv1alpha1.ScanPolicy{governing},
			digest:  digestB,
			blocked: true,
			want: opendepotv1alpha1.JevUnblock{
				State:             opendepotv1alpha1.JevUnblockStateExpired,
				Digest:            digestB,
				GoverningPolicy:   "governing",
				GoverningPriority: 100,
				MatchingPolicy:    "governing",
				MatchingPriority:  100,
				Expires:           past,
			},
		},
		{
			name:    "blocked with a match only in a lower-ranked policy is superseded",
			ranked:  []opendepotv1alpha1.ScanPolicy{governing, shadowed},
			digest:  digestC,
			blocked: true,
			want: opendepotv1alpha1.JevUnblock{
				State:             opendepotv1alpha1.JevUnblockStateSuperseded,
				Digest:            digestC,
				GoverningPolicy:   "governing",
				GoverningPriority: 100,
				MatchingPolicy:    "shadowed",
				MatchingPriority:  10,
			},
		},
		{
			name:    "blocked with no entry matching the digest needs an entry",
			ranked:  []opendepotv1alpha1.ScanPolicy{governing, shadowed},
			digest:  digestB + "0",
			blocked: true,
			want: opendepotv1alpha1.JevUnblock{
				State:             opendepotv1alpha1.JevUnblockStateNeedsEntry,
				Digest:            digestB + "0",
				GoverningPolicy:   "governing",
				GoverningPriority: 100,
			},
		},
		{
			name:    "blocked with no governing policy needs an entry",
			digest:  digestA,
			blocked: true,
			want:    opendepotv1alpha1.JevUnblock{State: opendepotv1alpha1.JevUnblockStateNeedsEntry, Digest: digestA},
		},
		{
			name:    "blocked legacy Version without a digest is no-digest",
			ranked:  []opendepotv1alpha1.ScanPolicy{governing},
			blocked: true,
			want: opendepotv1alpha1.JevUnblock{
				State:             opendepotv1alpha1.JevUnblockStateNoDigest,
				GoverningPolicy:   "governing",
				GoverningPriority: 100,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := JevUnblock(tc.ranked, tc.digest, tc.blocked, now)
			if got.State != tc.want.State || got.Digest != tc.want.Digest ||
				got.GoverningPolicy != tc.want.GoverningPolicy || got.GoverningPriority != tc.want.GoverningPriority ||
				got.MatchingPolicy != tc.want.MatchingPolicy || got.MatchingPriority != tc.want.MatchingPriority {
				t.Fatalf("JevUnblock() = %+v; want %+v", got, tc.want)
			}

			if !reflect.DeepEqual(got.Expires, tc.want.Expires) {
				t.Fatalf("JevUnblock() Expires = %v; want %v", got.Expires, tc.want.Expires)
			}

			if got.NamesHidden {
				t.Fatalf("JevUnblock() NamesHidden = true; want false before redaction")
			}
		})
	}
}

func TestUnblockFromGovernanceUnknownMatchingState(t *testing.T) {
	got := unblockFromGovernance(&opendepotv1alpha1.JevExemptionGovernance{MatchingState: "bogus"}, digestA, true)
	if got.State != opendepotv1alpha1.JevUnblockStateUnknown {
		t.Fatalf("State = %q; want %q", got.State, opendepotv1alpha1.JevUnblockStateUnknown)
	}
}
