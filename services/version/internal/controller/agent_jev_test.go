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
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/go-logr/logr/funcr"
	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	"github.com/tonedefdev/opendepot/pkg/jev"
	"github.com/tonedefdev/opendepot/services/version/internal/policy"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func f64(v float64) *float64 {
	return &v
}

// safeResult is a Jev result that crosses no threshold, so each case can raise exactly one probability.
func safeResult() jev.Result {
	return jev.Result{
		SafeProbability:               0.95,
		PromptInjectionProbability:    0.01,
		DataExfiltrationProbability:   0.01,
		DestructiveActionsProbability: 0.01,
		HiddenInstructionsProbability: 0.01,
		ScopeMismatchProbability:      0.01,
		RemoteExecutionProbability:    0.01,
		RiskScore:                     0.05,
		RiskConfidence:                0.9,
	}
}

func TestEvaluateJevGate(t *testing.T) {
	tests := []struct {
		name            string
		policy          *opendepotv1alpha1.JevPolicy
		result          jev.Result
		wantBlockCount  int
		wantNeedsReview bool
	}{
		{
			name:   "nil policy never blocks",
			policy: nil,
			result: func() jev.Result {
				r := safeResult()
				r.RiskScore = 1
				return r
			}(),
		},
		{
			name:   "empty policy is informational only",
			policy: &opendepotv1alpha1.JevPolicy{},
			result: func() jev.Result {
				r := safeResult()
				r.RiskScore = 1
				return r
			}(),
		},
		{
			name:           "minSafeProbability blocks below threshold",
			policy:         &opendepotv1alpha1.JevPolicy{MinSafeProbability: f64(0.99)},
			result:         safeResult(),
			wantBlockCount: 1,
		},
		{
			name:   "minSafeProbability allows at threshold",
			policy: &opendepotv1alpha1.JevPolicy{MinSafeProbability: f64(0.95)},
			result: safeResult(),
		},
		{
			name:           "maxInjectionProbability blocks above threshold",
			policy:         &opendepotv1alpha1.JevPolicy{MaxInjectionProbability: f64(0.5)},
			result:         func() jev.Result { r := safeResult(); r.PromptInjectionProbability = 0.9; return r }(),
			wantBlockCount: 1,
		},
		{
			name:           "maxExfiltrationProbability blocks above threshold",
			policy:         &opendepotv1alpha1.JevPolicy{MaxExfiltrationProbability: f64(0.5)},
			result:         func() jev.Result { r := safeResult(); r.DataExfiltrationProbability = 0.9; return r }(),
			wantBlockCount: 1,
		},
		{
			name:           "maxDestructiveProbability blocks above threshold",
			policy:         &opendepotv1alpha1.JevPolicy{MaxDestructiveProbability: f64(0.5)},
			result:         func() jev.Result { r := safeResult(); r.DestructiveActionsProbability = 0.9; return r }(),
			wantBlockCount: 1,
		},
		{
			name:           "maxHiddenInstructionsProbability blocks above threshold",
			policy:         &opendepotv1alpha1.JevPolicy{MaxHiddenInstructionsProbability: f64(0.5)},
			result:         func() jev.Result { r := safeResult(); r.HiddenInstructionsProbability = 0.9; return r }(),
			wantBlockCount: 1,
		},
		{
			name:           "maxScopeMismatchProbability blocks above threshold",
			policy:         &opendepotv1alpha1.JevPolicy{MaxScopeMismatchProbability: f64(0.5)},
			result:         func() jev.Result { r := safeResult(); r.ScopeMismatchProbability = 0.9; return r }(),
			wantBlockCount: 1,
		},
		{
			name:           "maxRemoteExecutionProbability blocks above threshold",
			policy:         &opendepotv1alpha1.JevPolicy{MaxRemoteExecutionProbability: f64(0.5)},
			result:         func() jev.Result { r := safeResult(); r.RemoteExecutionProbability = 0.9; return r }(),
			wantBlockCount: 1,
		},
		{
			name:           "maxRiskScore blocks above threshold",
			policy:         &opendepotv1alpha1.JevPolicy{MaxRiskScore: f64(0.5)},
			result:         func() jev.Result { r := safeResult(); r.RiskScore = 0.9; return r }(),
			wantBlockCount: 1,
		},
		{
			name:            "minConfidence never blocks and sets needsReview",
			policy:          &opendepotv1alpha1.JevPolicy{MinConfidence: f64(0.95)},
			result:          safeResult(),
			wantNeedsReview: true,
		},
		{
			name: "minConfidence met does not set needsReview",
			policy: &opendepotv1alpha1.JevPolicy{
				MinConfidence: f64(0.5),
			},
			result: safeResult(),
		},
		{
			name: "all thresholds crossed together block once each",
			policy: &opendepotv1alpha1.JevPolicy{
				MinSafeProbability:               f64(0.99),
				MaxInjectionProbability:          f64(0.5),
				MaxExfiltrationProbability:       f64(0.5),
				MaxDestructiveProbability:        f64(0.5),
				MaxHiddenInstructionsProbability: f64(0.5),
				MaxScopeMismatchProbability:      f64(0.5),
				MaxRemoteExecutionProbability:    f64(0.5),
				MaxRiskScore:                     f64(0.5),
				MinConfidence:                    f64(0.99),
			},
			result: jev.Result{
				SafeProbability:               0.1,
				PromptInjectionProbability:    0.9,
				DataExfiltrationProbability:   0.9,
				DestructiveActionsProbability: 0.9,
				HiddenInstructionsProbability: 0.9,
				ScopeMismatchProbability:      0.9,
				RemoteExecutionProbability:    0.9,
				RiskScore:                     0.9,
				RiskConfidence:                0.1,
			},
			wantBlockCount:  8,
			wantNeedsReview: true,
		},
		{
			name: "combined thresholds where only one is crossed",
			policy: &opendepotv1alpha1.JevPolicy{
				MinSafeProbability:      f64(0.5),
				MaxInjectionProbability: f64(0.5),
				MaxRiskScore:            f64(0.5),
			},
			result:         func() jev.Result { r := safeResult(); r.RiskScore = 0.7; return r }(),
			wantBlockCount: 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			reasons, needsReview := evaluateJevGate(tc.policy, tc.result)
			if len(reasons) != tc.wantBlockCount {
				t.Fatalf("block reasons = %d (%v), want %d", len(reasons), reasons, tc.wantBlockCount)
			}

			if needsReview != tc.wantNeedsReview {
				t.Fatalf("needsReview = %v, want %v", needsReview, tc.wantNeedsReview)
			}
		})
	}
}

func TestHasJevThresholds(t *testing.T) {
	if hasJevThresholds(nil) {
		t.Fatal("nil policy reported thresholds")
	}

	if hasJevThresholds(&opendepotv1alpha1.JevPolicy{}) {
		t.Fatal("empty policy reported thresholds")
	}

	if hasJevThresholds(&opendepotv1alpha1.JevPolicy{MinConfidence: f64(0.5)}) {
		t.Fatal("minConfidence alone must not count as a blocking threshold")
	}
}

func TestBuildJevStateOmitsFlaggedEntry(t *testing.T) {
	content := &agentContent{
		entryBytes:   []byte("secret body"),
		entryFlagged: true,
		files:        []string{"SKILL.md", "scripts/run.sh"},
	}

	state := buildJevState(content)
	if strings.Contains(state, "secret body") {
		t.Fatalf("flagged entry content was sent to Jev: %q", state)
	}

	if !strings.Contains(state, "- scripts/run.sh\n") {
		t.Fatalf("file list missing from state: %q", state)
	}
}

func TestBuildJevStateTruncatesAtLimit(t *testing.T) {
	content := &agentContent{
		entryBytes: []byte(strings.Repeat("é", jev.MaxStateBytes)),
		files:      []string{"SKILL.md"},
	}

	state := buildJevState(content)
	if len(state) > jev.MaxStateBytes {
		t.Fatalf("state length = %d, want at most %d", len(state), jev.MaxStateBytes)
	}

	if !strings.HasPrefix(state, "éé") {
		t.Fatalf("state does not start with the entry content")
	}
}

func TestSignAgentArchiveRequiresKey(t *testing.T) {
	t.Setenv(agentSigningKeyEnv, "")

	_, err := signAgentArchive([]byte("archive"), "agent.tar.gz")
	if err == nil {
		t.Fatal("expected an error when the signing key is not configured")
	}
}

const jevSentinelToken = "sentinel-jev-token-value"

const jevOKResponse = `{
  "model": "jev-1.13.0",
  "answers": {
    "safe": {"type": "noul", "noul": 0.99},
    "prompt_injection": {"type": "noul", "noul": 0.02},
    "data_exfiltration": {"type": "noul", "noul": 0.01},
    "destructive_actions": {"type": "noul", "noul": 0.03},
    "hidden_instructions": {"type": "noul", "noul": 0.04},
    "scope_mismatch": {"type": "noul", "noul": 0.05},
    "remote_execution": {"type": "noul", "noul": 0.06},
    "risk": {
      "type": "score",
      "score": 0.75,
      "legend": {"0": "Minimal", "1": "Low", "2": "Moderate", "3": "High", "4": "Critical"},
      "probabilities": {"0": 0.6, "1": 0.25, "2": 0.1, "3": 0.04, "4": 0.01},
      "confidence": 0.88
    }
  },
  "usage": {"input_tokens": 1234, "output_tokens": 56}
}`

func newJevRunnerFixture(t *testing.T, status int, body string, calls *atomic.Int32) (*httptest.Server, *VersionReconciler, *opendepotv1alpha1.Version) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer "+jevSentinelToken {
			t.Errorf("Authorization header did not carry the Secret token")
		}

		var payload struct {
			State string `json:"state"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode request: %v", err)
		}

		if strings.Contains(payload.State, jevSentinelToken) {
			t.Errorf("token leaked into the Jev state")
		}

		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "jev-token", Namespace: "default"},
		Data:       map[string][]byte{"jevToken": []byte(jevSentinelToken)},
	}

	reconciler := &VersionReconciler{
		Client:      fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build(),
		JevEnabled:  true,
		JevEndpoint: server.URL,
	}

	name := "agent"
	version := &opendepotv1alpha1.Version{
		ObjectMeta: metav1.ObjectMeta{Name: "agent-1-0-0", Namespace: "default"},
		Spec: opendepotv1alpha1.VersionSpec{
			Type: opendepotv1alpha1.OpenDepotSkill,
			AgentSourceRef: &opendepotv1alpha1.AgentSourceConfig{
				Name: &name,
				JevPolicy: &opendepotv1alpha1.JevPolicy{
					MinSafeProbability: f64(0.995),
				},
				JevSecretRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "jev-token"},
				},
			},
		},
	}

	return server, reconciler, version
}

func jevTestContent() *agentContent {
	return &agentContent{
		entryName:  "SKILL.md",
		entryBytes: []byte("# Skill\nSummarize documents."),
		files:      []string{"SKILL.md"},
	}
}

func TestRunAgentJevBlocksOnThresholdWithoutLeakingToken(t *testing.T) {
	var calls atomic.Int32
	_, reconciler, version := newJevRunnerFixture(t, http.StatusOK, jevOKResponse, &calls)

	assessment, err := reconciler.runAgentJev(context.Background(), version, jevTestContent())
	if err != nil {
		t.Fatalf("runAgentJev: %v", err)
	}

	if assessment == nil || !assessment.Blocked {
		t.Fatalf("assessment = %+v, want blocked", assessment)
	}

	if assessment.Model != "jev-1.13.0" {
		t.Fatalf("model = %q, want jev-1.13.0", assessment.Model)
	}

	if len(assessment.BlockReasons) == 0 {
		t.Fatal("block reasons were not recorded")
	}

	encoded, _ := json.Marshal(assessment)
	if strings.Contains(string(encoded), jevSentinelToken) {
		t.Fatal("token appeared in the Jev assessment")
	}

	if calls.Load() != 1 {
		t.Fatalf("Jev calls = %d, want 1", calls.Load())
	}
}

func TestRunAgentJevRecordsDisabledWithoutCall(t *testing.T) {
	var calls atomic.Int32
	_, reconciler, version := newJevRunnerFixture(t, http.StatusOK, jevOKResponse, &calls)
	reconciler.JevEnabled = false
	version.Spec.AgentSourceRef.JevPolicy = nil

	assessment, err := reconciler.runAgentJev(context.Background(), version, jevTestContent())
	if err != nil {
		t.Fatalf("runAgentJev: %v", err)
	}

	if assessment == nil || assessment.Error != jevDisabledError || assessment.Blocked {
		t.Fatalf("assessment = %+v, want recorded as disabled", assessment)
	}

	if calls.Load() != 0 {
		t.Fatalf("Jev was called %d times while disabled", calls.Load())
	}
}

func TestRunAgentJevFailsClosedWhenDisabledWithThresholds(t *testing.T) {
	var calls atomic.Int32
	_, reconciler, version := newJevRunnerFixture(t, http.StatusOK, jevOKResponse, &calls)
	reconciler.JevEnabled = false

	assessment, err := reconciler.runAgentJev(context.Background(), version, jevTestContent())
	if err == nil {
		t.Fatalf("expected an error, got assessment %+v", assessment)
	}

	if calls.Load() != 0 {
		t.Fatalf("Jev was called %d times while disabled", calls.Load())
	}
}

func TestRunAgentJevDoesNotCallWithoutSecretRef(t *testing.T) {
	var calls atomic.Int32
	_, reconciler, version := newJevRunnerFixture(t, http.StatusOK, jevOKResponse, &calls)
	version.Spec.AgentSourceRef.JevSecretRef = nil

	assessment, err := reconciler.runAgentJev(context.Background(), version, jevTestContent())
	if err != nil || assessment != nil {
		t.Fatalf("assessment = %+v, err = %v; want nil, nil", assessment, err)
	}

	if calls.Load() != 0 {
		t.Fatalf("Jev was called %d times without jevSecretRef", calls.Load())
	}
}

func TestRunAgentJevAssessesWithoutThresholds(t *testing.T) {
	var calls atomic.Int32
	_, reconciler, version := newJevRunnerFixture(t, http.StatusOK, jevOKResponse, &calls)
	version.Spec.AgentSourceRef.JevPolicy = nil

	assessment, err := reconciler.runAgentJev(context.Background(), version, jevTestContent())
	if err != nil {
		t.Fatalf("runAgentJev: %v", err)
	}

	if assessment == nil || assessment.Blocked || assessment.SafeProbability == nil {
		t.Fatalf("assessment = %+v, want informational assessment", assessment)
	}

	if calls.Load() != 1 {
		t.Fatalf("Jev calls = %d, want 1", calls.Load())
	}
}

func TestRunAgentJevCachesErroredAssessment(t *testing.T) {
	var calls atomic.Int32
	_, reconciler, version := newJevRunnerFixture(t, http.StatusUnauthorized, `{"error":"denied"}`, &calls)
	version.Spec.AgentSourceRef.JevPolicy = nil

	assessment, err := reconciler.runAgentJev(context.Background(), version, jevTestContent())
	if err != nil {
		t.Fatalf("runAgentJev: %v", err)
	}

	version.Status.JevAssessment = assessment
	if _, err := reconciler.runAgentJev(context.Background(), version, jevTestContent()); err != nil {
		t.Fatalf("cached runAgentJev: %v", err)
	}

	if calls.Load() != 1 {
		t.Fatalf("Jev calls = %d, want 1 after a cached error", calls.Load())
	}

	version.Spec.ForceSync = true
	if _, err := reconciler.runAgentJev(context.Background(), version, jevTestContent()); err != nil {
		t.Fatalf("forced runAgentJev: %v", err)
	}

	if calls.Load() != 2 {
		t.Fatalf("Jev calls = %d, want 2 after ForceSync", calls.Load())
	}
}

func TestRunAgentJevCachedErrorFailsClosedWhenThresholdsSet(t *testing.T) {
	var calls atomic.Int32
	_, reconciler, version := newJevRunnerFixture(t, http.StatusOK, jevOKResponse, &calls)
	version.Status.JevAssessment = &opendepotv1alpha1.JevAssessment{EvaluatedAt: "2026-10-08T00:00:00Z", Error: "jev: request failed"}

	if _, err := reconciler.runAgentJev(context.Background(), version, jevTestContent()); err == nil {
		t.Fatal("expected a cached error to fail closed when thresholds are set")
	}

	if calls.Load() != 0 {
		t.Fatalf("Jev was called %d times from a cached error", calls.Load())
	}
}

func TestRunAgentJevKeepsTokenOutOfLogs(t *testing.T) {
	var logs strings.Builder
	var calls atomic.Int32
	_, reconciler, version := newJevRunnerFixture(t, http.StatusUnauthorized, `{"error":"denied"}`, &calls)
	reconciler.Log = funcr.New(func(prefix, args string) {
		logs.WriteString(prefix + args)
	}, funcr.Options{})

	_, _ = reconciler.runAgentJev(context.Background(), version, jevTestContent())
	version.Spec.AgentSourceRef.JevPolicy = &opendepotv1alpha1.JevPolicy{MinSafeProbability: f64(0.5)}
	_, _ = reconciler.runAgentJev(context.Background(), version, jevTestContent())

	if strings.Contains(logs.String(), jevSentinelToken) {
		t.Fatal("token appeared in the controller logs")
	}
}

func jevTestArchive(t *testing.T, files map[string]string) []byte {
	t.Helper()

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}

		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}

	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}

	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}

	return buf.Bytes()
}

func TestRunAgentScanRunsTrivyWithJevEnabledAndDisabled(t *testing.T) {
	var trivyCalls atomic.Int32
	originalTrivy := runTrivy
	runTrivy = func(_ context.Context, _ ...string) ([]byte, error) {
		trivyCalls.Add(1)
		return []byte(`{"Results":[]}`), nil
	}
	t.Cleanup(func() { runTrivy = originalTrivy })

	archive := jevTestArchive(t, map[string]string{
		"SKILL.md": "---\nname: agent\ndescription: Summarizes documents.\n---\nSummarize documents.\n",
	})

	for _, enabled := range []bool{true, false} {
		var jevCalls atomic.Int32
		_, reconciler, version := newJevRunnerFixture(t, http.StatusOK, jevOKResponse, &jevCalls)
		reconciler.JevEnabled = enabled
		reconciler.scanSem = make(chan struct{}, 1)

		before := trivyCalls.Load()
		if _, _, _, err := reconciler.runAgentScan(context.Background(), version, archive, "", false, policy.Resolved{}); err != nil {
			t.Fatalf("runAgentScan (jev enabled=%t): %v", enabled, err)
		}

		if trivyCalls.Load() != before+1 {
			t.Fatalf("Trivy calls with jev enabled=%t = %d, want 1", enabled, trivyCalls.Load()-before)
		}

		if _, err := reconciler.runAgentJev(context.Background(), version, jevTestContent()); err != nil && enabled {
			t.Fatalf("runAgentJev (jev enabled=%t): %v", enabled, err)
		}

		if want := map[bool]int32{true: 1, false: 0}[enabled]; jevCalls.Load() != want {
			t.Fatalf("Jev calls with jev enabled=%t = %d, want %d", enabled, jevCalls.Load(), want)
		}
	}
}

func TestRunAgentJevFailsClosedWhenThresholdsSetAndAPIFails(t *testing.T) {
	var calls atomic.Int32
	_, reconciler, version := newJevRunnerFixture(t, http.StatusUnauthorized, `{"error":"denied"}`, &calls)

	assessment, err := reconciler.runAgentJev(context.Background(), version, jevTestContent())
	if err == nil {
		t.Fatalf("expected an error, got assessment %+v", assessment)
	}

	if strings.Contains(err.Error(), jevSentinelToken) {
		t.Fatal("token appeared in the Jev error")
	}
}

func TestRunAgentJevRecordsErrorWhenNoThresholds(t *testing.T) {
	var calls atomic.Int32
	_, reconciler, version := newJevRunnerFixture(t, http.StatusUnauthorized, `{"error":"denied"}`, &calls)
	version.Spec.AgentSourceRef.JevPolicy = &opendepotv1alpha1.JevPolicy{}

	assessment, err := reconciler.runAgentJev(context.Background(), version, jevTestContent())
	if err != nil {
		t.Fatalf("runAgentJev: %v", err)
	}

	if assessment == nil || assessment.Error == "" || assessment.Blocked {
		t.Fatalf("assessment = %+v, want informational error", assessment)
	}

	if strings.Contains(assessment.Error, jevSentinelToken) {
		t.Fatal("token appeared in the recorded Jev error")
	}
}
