package main

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
)

func TestWave7NStorageBackendName(t *testing.T) {
	tests := []struct {
		name string
		sc   *opendepotv1alpha1.StorageConfig
		want string
	}{
		{name: "nil config", sc: nil, want: "unknown"},
		{name: "s3", sc: &opendepotv1alpha1.StorageConfig{S3: &opendepotv1alpha1.AmazonS3Config{}}, want: "s3"},
		{name: "azure", sc: &opendepotv1alpha1.StorageConfig{AzureStorage: &opendepotv1alpha1.AzureStorageConfig{}}, want: "azureStorage"},
		{name: "gcs", sc: &opendepotv1alpha1.StorageConfig{GCS: &opendepotv1alpha1.GoogleCloudStorageConfig{}}, want: "gcs"},
		{name: "filesystem", sc: &opendepotv1alpha1.StorageConfig{FileSystem: &opendepotv1alpha1.FileSystemConfig{}}, want: "fileSystem"},
		{name: "empty config", sc: &opendepotv1alpha1.StorageConfig{}, want: "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := storageBackendName(tt.sc); got != tt.want {
				t.Fatalf("storageBackendName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestWave7NJevExemptionActive(t *testing.T) {
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	future := metav1.NewTime(now.Add(time.Hour))
	past := metav1.NewTime(now.Add(-time.Hour))

	tests := []struct {
		name string
		a    *opendepotv1alpha1.JevAssessment
		want bool
	}{
		{name: "not exempted", a: &opendepotv1alpha1.JevAssessment{}, want: false},
		{name: "exempted without expiry", a: &opendepotv1alpha1.JevAssessment{Exempted: true}, want: true},
		{name: "exempted until future", a: &opendepotv1alpha1.JevAssessment{Exempted: true, ExemptionExpires: &future}, want: true},
		{name: "exempted but lapsed", a: &opendepotv1alpha1.JevAssessment{Exempted: true, ExemptionExpires: &past}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := jevExemptionActive(tt.a, now); got != tt.want {
				t.Fatalf("jevExemptionActive() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWave7NAccumulateJevStats(t *testing.T) {
	future := metav1.NewTime(time.Now().Add(time.Hour))
	past := metav1.NewTime(time.Now().Add(-time.Hour))

	var stats JevStats
	accumulateJevStats(&stats, nil)
	if stats.AssessedVersions != 0 {
		t.Fatalf("nil assessment counted: %+v", stats)
	}

	accumulateJevStats(&stats, &opendepotv1alpha1.JevAssessment{RiskLevel: "Minimal", InputTokens: 10, AssessedChunks: 2})
	accumulateJevStats(&stats, &opendepotv1alpha1.JevAssessment{RiskLevel: "low", Blocked: true, InputTokens: 5, AssessedChunks: 1})
	accumulateJevStats(&stats, &opendepotv1alpha1.JevAssessment{RiskLevel: "MODERATE", Exempted: true, ExemptionExpires: &future, NeedsReview: true})
	accumulateJevStats(&stats, &opendepotv1alpha1.JevAssessment{RiskLevel: "high", NeedsReview: true, ExemptionExpires: &past, Exempted: true})
	accumulateJevStats(&stats, &opendepotv1alpha1.JevAssessment{RiskLevel: "critical"})
	accumulateJevStats(&stats, &opendepotv1alpha1.JevAssessment{Error: "model unavailable", RiskLevel: "critical", Blocked: true, InputTokens: 7})

	if stats.AssessedVersions != 6 {
		t.Fatalf("AssessedVersions = %d, want 6", stats.AssessedVersions)
	}

	if stats.InputTokens != 22 || stats.AssessedChunks != 3 {
		t.Fatalf("tokens=%d chunks=%d, want 22/3", stats.InputTokens, stats.AssessedChunks)
	}

	if stats.BlockedVersions != 2 || stats.ExemptedVersions != 2 {
		t.Fatalf("blocked=%d exempted=%d, want 2/2", stats.BlockedVersions, stats.ExemptedVersions)
	}

	if stats.NeedsReviewVersions != 1 {
		t.Fatalf("NeedsReviewVersions = %d, want 1 (active exemption excluded)", stats.NeedsReviewVersions)
	}

	if stats.FailedVersions != 1 {
		t.Fatalf("FailedVersions = %d, want 1", stats.FailedVersions)
	}

	want := JevRiskLevelStats{Minimal: 1, Low: 1, Moderate: 1, High: 1, Critical: 1}
	if stats.RiskLevels != want {
		t.Fatalf("RiskLevels = %+v, want %+v", stats.RiskLevels, want)
	}
}

func TestWave7NAccumulateFinding(t *testing.T) {
	var posture SecurityPostureStats

	findings := []opendepotv1alpha1.SecurityFinding{
		{Severity: "critical"},
		{Severity: "HIGH"},
		{Severity: "Medium"},
		{Severity: "low"},
		{Severity: "negligible"},
		{Severity: "HIGH", Exempted: true},
	}

	for _, f := range findings {
		accumulateFinding(&posture, f)
	}

	want := SecurityPostureStats{Critical: 1, High: 1, Medium: 1, Low: 1, Unknown: 1, Exempted: 1}
	if posture != want {
		t.Fatalf("posture = %+v, want %+v", posture, want)
	}
}

func TestWave7NCountSyncedAgentCards(t *testing.T) {
	cards := []agentCardObject{
		{Status: agentCardStatus{Synced: true}},
		{Status: agentCardStatus{Synced: false}},
		{Status: agentCardStatus{Synced: true}},
	}

	if got := countSyncedAgentCards(cards); got != 2 {
		t.Fatalf("countSyncedAgentCards() = %d, want 2", got)
	}

	if got := countSyncedAgentCards(nil); got != 0 {
		t.Fatalf("countSyncedAgentCards(nil) = %d, want 0", got)
	}
}
