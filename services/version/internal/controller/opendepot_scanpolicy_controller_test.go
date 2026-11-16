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

package controller

import (
	"testing"
	"time"

	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
)

// TestScanPolicyObserveCountsJevExemptions covers Step 8: Jev exemption counts are reported next to the existing
// ScanPolicy exemption counts, and the existing counts do not change.
func TestScanPolicyObserveCountsJevExemptions(t *testing.T) {
	now := time.Now()
	past := metav1.NewTime(now.Add(-time.Hour))
	future := metav1.NewTime(now.Add(time.Hour))

	scanPolicy := &opendepotv1alpha1.ScanPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "release", Namespace: "default"},
		Spec: opendepotv1alpha1.ScanPolicySpec{
			Exemptions: []opendepotv1alpha1.ScanExemption{
				{VulnerabilityIDs: []string{"CVE-1"}, Reason: "accepted", Expires: &future},
				{VulnerabilityIDs: []string{"CVE-2"}, Reason: "expired", Expires: &past},
			},
			JevExemptions: []opendepotv1alpha1.JevExemption{
				{ContentDigest: jevExemptTestDigest, Reason: "never expires"},
				{ContentDigest: jevExemptTestOther, Reason: "expired", Expires: &past},
				{ContentDigest: "c" + jevExemptTestDigest[1:], Reason: "future", Expires: &future},
			},
		},
	}

	reconciler := &ScanPolicyReconciler{Log: logr.Discard()}
	status := reconciler.observe(scanPolicy, []opendepotv1alpha1.ScanPolicy{*scanPolicy}, nil, now)

	if status.ActiveExemptions != 1 || status.ExpiredExemptions != 1 {
		t.Fatalf("exemptions = active %d expired %d; want 1 and 1", status.ActiveExemptions, status.ExpiredExemptions)
	}

	if status.ActiveJevExemptions != 2 || status.ExpiredJevExemptions != 1 {
		t.Fatalf("jev exemptions = active %d expired %d; want 2 and 1", status.ActiveJevExemptions, status.ExpiredJevExemptions)
	}
}

// TestScanPolicyNextExpiryIncludesJevExemptions covers Step 8: the requeue waits for the earliest future expiry
// across both exemption lists.
func TestScanPolicyNextExpiryIncludesJevExemptions(t *testing.T) {
	now := time.Now()
	past := metav1.NewTime(now.Add(-time.Hour))
	jevSoon := metav1.NewTime(now.Add(30 * time.Minute))
	exemptionLater := metav1.NewTime(now.Add(2 * time.Hour))
	jevLater := metav1.NewTime(now.Add(3 * time.Hour))

	tests := []struct {
		name       string
		exemptions []opendepotv1alpha1.ScanExemption
		jev        []opendepotv1alpha1.JevExemption
		want       time.Duration
	}{
		{
			name:       "jev expiry earlier than an exemption expiry",
			exemptions: []opendepotv1alpha1.ScanExemption{{VulnerabilityIDs: []string{"CVE-1"}, Reason: "accepted", Expires: &exemptionLater}},
			jev:        []opendepotv1alpha1.JevExemption{{ContentDigest: jevExemptTestDigest, Reason: "soon", Expires: &jevSoon}},
			want:       30 * time.Minute,
		},
		{
			name:       "exemption expiry earlier than a jev expiry",
			exemptions: []opendepotv1alpha1.ScanExemption{{VulnerabilityIDs: []string{"CVE-1"}, Reason: "accepted", Expires: &exemptionLater}},
			jev:        []opendepotv1alpha1.JevExemption{{ContentDigest: jevExemptTestDigest, Reason: "later", Expires: &jevLater}},
			want:       2 * time.Hour,
		},
		{
			name:       "expired jev expiry and no expiry do not schedule a requeue",
			exemptions: []opendepotv1alpha1.ScanExemption{{VulnerabilityIDs: []string{"CVE-1"}, Reason: "accepted"}},
			jev: []opendepotv1alpha1.JevExemption{
				{ContentDigest: jevExemptTestDigest, Reason: "never"},
				{ContentDigest: jevExemptTestOther, Reason: "expired", Expires: &past},
			},
			want: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scanPolicy := &opendepotv1alpha1.ScanPolicy{
				ObjectMeta: metav1.ObjectMeta{Name: "release", Namespace: "default"},
				Spec: opendepotv1alpha1.ScanPolicySpec{
					Exemptions:    tt.exemptions,
					JevExemptions: tt.jev,
				},
			}

			reconciler := &ScanPolicyReconciler{Log: logr.Discard()}
			got := reconciler.nextExpiry(scanPolicy, now)
			if got != tt.want {
				t.Fatalf("nextExpiry = %s; want %s", got, tt.want)
			}
		})
	}
}
