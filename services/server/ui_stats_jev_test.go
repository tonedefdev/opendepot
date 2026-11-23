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

package main

import (
	"testing"
	"time"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestJevExemptionActive(t *testing.T) {
	now := time.Now()
	past := metav1.NewTime(now.Add(-time.Hour))
	future := metav1.NewTime(now.Add(time.Hour))

	tests := []struct {
		name string
		a    *opendepotv1alpha1.JevAssessment
		want bool
	}{
		{name: "not exempted", a: &opendepotv1alpha1.JevAssessment{}, want: false},
		{name: "exempted without expiry", a: &opendepotv1alpha1.JevAssessment{Exempted: true}, want: true},
		{name: "exempted until future expiry", a: &opendepotv1alpha1.JevAssessment{Exempted: true, ExemptionExpires: &future}, want: true},
		{name: "exempted past expiry", a: &opendepotv1alpha1.JevAssessment{Exempted: true, ExemptionExpires: &past}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := jevExemptionActive(tt.a, now); got != tt.want {
				t.Errorf("jevExemptionActive() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAccumulateJevStatsNeedsReviewRespectsExemption(t *testing.T) {
	now := time.Now()
	past := metav1.NewTime(now.Add(-time.Hour))
	future := metav1.NewTime(now.Add(time.Hour))

	var stats JevStats
	accumulateJevStats(&stats, &opendepotv1alpha1.JevAssessment{RiskLevel: "Critical", NeedsReview: true, Exempted: true, ExemptionExpires: &future})
	accumulateJevStats(&stats, &opendepotv1alpha1.JevAssessment{RiskLevel: "Critical", NeedsReview: true, Exempted: true, ExemptionExpires: &past})
	accumulateJevStats(&stats, &opendepotv1alpha1.JevAssessment{RiskLevel: "High", NeedsReview: true})

	if stats.NeedsReviewVersions != 2 {
		t.Errorf("NeedsReviewVersions = %d, want 2 (active exemption excluded, expired and unexempted included)", stats.NeedsReviewVersions)
	}

	if stats.ExemptedVersions != 2 {
		t.Errorf("ExemptedVersions = %d, want 2", stats.ExemptedVersions)
	}
}

func TestAccumulateJevStats(t *testing.T) {
	assessments := []*opendepotv1alpha1.JevAssessment{
		{RiskLevel: "Minimal", InputTokens: 1000, AssessedChunks: 2},
		{RiskLevel: "High", InputTokens: 500, AssessedChunks: 1, Blocked: true, NeedsReview: true},
		{RiskLevel: "Critical", InputTokens: 2000, AssessedChunks: 3, Exempted: true},
		// A failed assessment still counts its billed tokens but is not scored.
		{RiskLevel: "Low", InputTokens: 300, AssessedChunks: 1, Error: "jev call failed"},
		nil,
	}

	var stats JevStats
	for _, a := range assessments {
		accumulateJevStats(&stats, a)
	}

	if stats.AssessedVersions != 4 {
		t.Errorf("AssessedVersions = %d, want 4", stats.AssessedVersions)
	}

	if stats.BlockedVersions != 1 {
		t.Errorf("BlockedVersions = %d, want 1", stats.BlockedVersions)
	}

	if stats.NeedsReviewVersions != 1 {
		t.Errorf("NeedsReviewVersions = %d, want 1", stats.NeedsReviewVersions)
	}

	if stats.ExemptedVersions != 1 {
		t.Errorf("ExemptedVersions = %d, want 1", stats.ExemptedVersions)
	}

	if stats.FailedVersions != 1 {
		t.Errorf("FailedVersions = %d, want 1", stats.FailedVersions)
	}

	if stats.InputTokens != 3800 {
		t.Errorf("InputTokens = %d, want 3800", stats.InputTokens)
	}

	if stats.AssessedChunks != 7 {
		t.Errorf("AssessedChunks = %d, want 7", stats.AssessedChunks)
	}

	wantRisk := JevRiskLevelStats{Minimal: 1, High: 1, Critical: 1}
	if stats.RiskLevels != wantRisk {
		t.Errorf("RiskLevels = %+v, want %+v", stats.RiskLevels, wantRisk)
	}
}

func TestAccumulateJevStatsRiskLevelIsCaseInsensitive(t *testing.T) {
	var stats JevStats
	accumulateJevStats(&stats, &opendepotv1alpha1.JevAssessment{RiskLevel: "moderate"})

	if stats.RiskLevels.Moderate != 1 {
		t.Errorf("RiskLevels.Moderate = %d, want 1", stats.RiskLevels.Moderate)
	}
}
