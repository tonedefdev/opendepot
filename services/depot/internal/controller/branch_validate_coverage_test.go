package controller

import (
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
)

func TestBranchCovValidateBranchSourceRejectsInvalidConfig(t *testing.T) {
	ref := "main"
	tagPrefix := "v"
	negative := &metav1.Duration{Duration: -time.Minute}

	cases := []struct {
		name    string
		enabled bool
		source  opendepotv1alpha1.AgentSourceConfig
		wantErr string
	}{
		{
			name:    "branch sourcing disabled",
			enabled: false,
			source:  opendepotv1alpha1.AgentSourceConfig{Ref: &ref},
			wantErr: "branch sourcing is disabled",
		},
		{
			name:    "ref and versionConstraints are exclusive",
			enabled: true,
			source:  opendepotv1alpha1.AgentSourceConfig{Ref: &ref, VersionConstraints: ">= 1.0.0"},
			wantErr: "mutually exclusive",
		},
		{
			name:    "tagPrefix with ref",
			enabled: true,
			source:  opendepotv1alpha1.AgentSourceConfig{Ref: &ref, TagPrefix: &tagPrefix},
			wantErr: "tagPrefix cannot be used with ref",
		},
		{
			name:    "negative minimum interval",
			enabled: true,
			source: opendepotv1alpha1.AgentSourceConfig{
				Ref:          &ref,
				BranchPolicy: &opendepotv1alpha1.BranchPolicy{MinVersionInterval: negative},
			},
			wantErr: "must not be negative",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := &DepotReconciler{BranchSourcingEnabled: tc.enabled}

			err := r.validateBranchSource(&tc.source)
			if err == nil {
				t.Fatal("expected validation to reject the source")
			}

			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestBranchCovEntryDiscoveredAt(t *testing.T) {
	if got := entryDiscoveredAt(branchEntry{}); !got.IsZero() {
		t.Fatalf("an entry without a discovery time must sort as the oldest, got %v", got)
	}

	when := time.Date(2025, 3, 4, 5, 6, 7, 0, time.UTC)
	got := entryDiscoveredAt(branchEntry{DiscoveredAt: &metav1.Time{Time: when}})
	if !got.Equal(when) {
		t.Fatalf("entryDiscoveredAt = %v, want %v", got, when)
	}
}
