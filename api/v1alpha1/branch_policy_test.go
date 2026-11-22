package v1alpha1

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestBranchPolicyEffectiveDefaults(t *testing.T) {
	var nilPolicy *BranchPolicy

	if got := nilPolicy.EffectiveMinVersionInterval(); got != DefaultMinVersionInterval {
		t.Fatalf("nil EffectiveMinVersionInterval() = %v, want %v", got, DefaultMinVersionInterval)
	}

	if got := nilPolicy.EffectiveMajorThreshold(); got != DefaultBranchThreshold {
		t.Fatalf("nil EffectiveMajorThreshold() = %v, want %v", got, DefaultBranchThreshold)
	}

	if got := nilPolicy.EffectiveMinorThreshold(); got != DefaultBranchThreshold {
		t.Fatalf("nil EffectiveMinorThreshold() = %v, want %v", got, DefaultBranchThreshold)
	}

	if got := nilPolicy.EffectiveMinConfidence(); got != DefaultBranchMinConfidence {
		t.Fatalf("nil EffectiveMinConfidence() = %v, want %v", got, DefaultBranchMinConfidence)
	}

	empty := &BranchPolicy{}
	if got := empty.EffectiveMinVersionInterval(); got != DefaultMinVersionInterval {
		t.Fatalf("empty EffectiveMinVersionInterval() = %v, want %v", got, DefaultMinVersionInterval)
	}

	if got := empty.EffectiveMajorThreshold(); got != DefaultBranchThreshold {
		t.Fatalf("empty EffectiveMajorThreshold() = %v, want %v", got, DefaultBranchThreshold)
	}

	if got := empty.EffectiveMinorThreshold(); got != DefaultBranchThreshold {
		t.Fatalf("empty EffectiveMinorThreshold() = %v, want %v", got, DefaultBranchThreshold)
	}

	if got := empty.EffectiveMinConfidence(); got != DefaultBranchMinConfidence {
		t.Fatalf("empty EffectiveMinConfidence() = %v, want %v", got, DefaultBranchMinConfidence)
	}
}

func TestBranchPolicyEffectiveOverrides(t *testing.T) {
	interval := metav1.Duration{Duration: 2 * time.Hour}
	major := 0.9
	minor := 0.7
	confidence := 0.8

	policy := &BranchPolicy{
		MinVersionInterval: &interval,
		MajorThreshold:     &major,
		MinorThreshold:     &minor,
		MinConfidence:      &confidence,
	}

	if got := policy.EffectiveMinVersionInterval(); got != 2*time.Hour {
		t.Fatalf("EffectiveMinVersionInterval() = %v, want 2h", got)
	}

	if got := policy.EffectiveMajorThreshold(); got != major {
		t.Fatalf("EffectiveMajorThreshold() = %v, want %v", got, major)
	}

	if got := policy.EffectiveMinorThreshold(); got != minor {
		t.Fatalf("EffectiveMinorThreshold() = %v, want %v", got, minor)
	}

	if got := policy.EffectiveMinConfidence(); got != confidence {
		t.Fatalf("EffectiveMinConfidence() = %v, want %v", got, confidence)
	}
}
