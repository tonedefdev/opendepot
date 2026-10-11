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
	"context"
	"encoding/json"
	"errors"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	"golang.org/x/time/rate"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/yaml"
)

// jevSentinelToken is the Jev token stored in the test Secret. No test may see it in a Jev state or in an assessment.
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

// jevEntry is a Skill entry file with frontmatter, so the definition parses.
const jevEntry = "---\nname: agent\ndescription: Summarizes documents.\n---\nSummarize documents.\n"

type jevStub func(state string) (int, string)

func f64(v float64) *float64 {
	return &v
}

func jevOK(string) (int, string) {
	return http.StatusOK, jevOKResponse
}

// jevRejectMark rejects the first times requests whose state contains mark, with a 400 token-limit error.
func jevRejectMark(mark string, times int32) jevStub {
	var rejected atomic.Int32

	return func(state string) (int, string) {
		if strings.Contains(state, mark) && rejected.Add(1) <= times {
			return http.StatusBadRequest, "max_tokens_exceeded"
		}

		return http.StatusOK, jevOKResponse
	}
}

// newJevEndpoint starts a Jev stub. It checks the bearer token and that the token never reaches a Jev state.
func newJevEndpoint(t *testing.T, stub jevStub) (string, *atomic.Int32) {
	t.Helper()

	var calls atomic.Int32
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

		status, body := stub(payload.State)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)

	return server.URL, &calls
}

func newJevTestReconciler(t *testing.T, endpoint string) *VersionReconciler {
	t.Helper()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "jev-token", Namespace: "default"},
		Data:       map[string][]byte{"jevToken": []byte(jevSentinelToken)},
	}

	return &VersionReconciler{
		Client:        fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build(),
		JevEnabled:    true,
		JevEndpoint:   endpoint,
		JevMaxRetries: 3,
	}
}

func newJevTestVersion(policy *opendepotv1alpha1.JevPolicy, cached *opendepotv1alpha1.JevAssessment) *opendepotv1alpha1.Version {
	name := "agent"
	version := &opendepotv1alpha1.Version{
		ObjectMeta: metav1.ObjectMeta{Name: "agent-1-0-0", Namespace: "default"},
		Spec: opendepotv1alpha1.VersionSpec{
			Type: opendepotv1alpha1.OpenDepotSkill,
			AgentSourceRef: &opendepotv1alpha1.AgentSourceConfig{
				Name:      &name,
				JevPolicy: policy,
				JevSecretRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "jev-token"},
				},
			},
		},
	}
	version.Status.JevAssessment = cached

	return version
}

// jevLoadContent writes files into a new directory and loads it the way the controller loads an extracted Skill.
func jevLoadContent(t *testing.T, files map[string]string) *agentContent {
	t.Helper()

	dir := t.TempDir()
	for rel, body := range files {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	content, err := loadAgentContent(dir, newJevTestVersion(nil, nil))
	if err != nil {
		t.Fatalf("loadAgentContent: %v", err)
	}

	return content
}

// jevMarkedContent returns a Skill whose bundled body spans parts parts. Each mark is placed near the tail of the given
// 1-based part, so a halving keeps the mark in the second half and a request for that part carries the mark.
func jevMarkedContent(t *testing.T, parts int, marks map[int]string) *agentContent {
	t.Helper()

	body := []byte(strings.Repeat("a", parts*jevChunkBodyBudget-4096))
	for part, mark := range marks {
		copy(body[part*jevChunkBodyBudget-8192:], mark)
	}

	return jevLoadContent(t, map[string]string{"SKILL.md": jevEntry, "big.txt": string(body)})
}

func jevLabels(assessment *opendepotv1alpha1.JevAssessment) []string {
	labels := make([]string, 0, len(assessment.Chunks))
	for _, c := range assessment.Chunks {
		labels = append(labels, c.Label)
	}

	return labels
}

func jevChunkStatus(assessment *opendepotv1alpha1.JevAssessment, label string) string {
	for _, c := range assessment.Chunks {
		if c.Label == label {
			return c.Status
		}
	}

	return ""
}

func TestJevPublishedStatus(t *testing.T) {
	if got := jevPublishedStatus(nil); got != "Successfully synced version" {
		t.Fatalf("status without an assessment = %q", got)
	}

	withError := &opendepotv1alpha1.JevAssessment{Error: "jev assessment incomplete"}
	if got := jevPublishedStatus(withError); !strings.Contains(got, "Jev assessment did not complete") {
		t.Fatalf("status with an error = %q", got)
	}
}

func TestRunAgentJevNoSecretRefSkipsJev(t *testing.T) {
	endpoint, calls := newJevEndpoint(t, jevOK)
	reconciler := newJevTestReconciler(t, endpoint)
	version := newJevTestVersion(nil, nil)
	version.Spec.AgentSourceRef.JevSecretRef = nil

	assessment, err := reconciler.runAgentJev(context.Background(), version, jevLoadContent(t, map[string]string{"SKILL.md": jevEntry}))
	if err != nil || assessment != nil {
		t.Fatalf("runAgentJev without a secret ref = (%v, %v), want (nil, nil)", assessment, err)
	}

	if calls.Load() != 0 {
		t.Fatalf("Jev calls = %d, want 0", calls.Load())
	}
}

func TestRunAgentJevBlocksOnThresholdWithoutLeakingToken(t *testing.T) {
	endpoint, calls := newJevEndpoint(t, jevOK)
	reconciler := newJevTestReconciler(t, endpoint)
	version := newJevTestVersion(&opendepotv1alpha1.JevPolicy{MinSafeProbability: f64(0.995)}, nil)

	assessment, err := reconciler.runAgentJev(context.Background(), version, jevLoadContent(t, map[string]string{"SKILL.md": jevEntry}))
	if err != nil {
		t.Fatalf("runAgentJev: %v", err)
	}

	if !assessment.Blocked || len(assessment.BlockReasons) == 0 {
		t.Fatalf("assessment not blocked by minSafeProbability: %+v", assessment)
	}

	if calls.Load() != 1 {
		t.Fatalf("Jev calls = %d, want 1", calls.Load())
	}

	if strings.Contains(strings.Join(assessment.BlockReasons, " "), jevSentinelToken) {
		t.Fatalf("token appeared in block reasons")
	}
}

func TestRunAgentJevAssessesWithoutThresholds(t *testing.T) {
	endpoint, calls := newJevEndpoint(t, jevOK)
	reconciler := newJevTestReconciler(t, endpoint)
	version := newJevTestVersion(nil, nil)

	assessment, err := reconciler.runAgentJev(context.Background(), version, jevLoadContent(t, map[string]string{"SKILL.md": jevEntry}))
	if err != nil {
		t.Fatalf("runAgentJev: %v", err)
	}

	if assessment.Blocked || assessment.Error != "" || assessment.ChunkCount != 1 {
		t.Fatalf("assessment = %+v, want a clean single-part assessment", assessment)
	}

	if got := jevChunkStatus(assessment, "1"); got != jevPartScored {
		t.Fatalf("part 1 status = %q, want %q", got, jevPartScored)
	}

	if calls.Load() != 1 {
		t.Fatalf("Jev calls = %d, want 1", calls.Load())
	}
}

func TestRunAgentJevFailsClosedOnServiceFailureWithoutThresholds(t *testing.T) {
	endpoint, calls := newJevEndpoint(t, func(string) (int, string) {
		return http.StatusBadRequest, "bad request"
	})
	reconciler := newJevTestReconciler(t, endpoint)
	version := newJevTestVersion(nil, &opendepotv1alpha1.JevAssessment{Error: "old", ChunkCount: 1, RetryCount: 2})

	assessment, err := reconciler.runAgentJev(context.Background(), version, jevLoadContent(t, map[string]string{"SKILL.md": jevEntry}))
	if err == nil || !assessment.Blocked || !strings.Contains(assessment.Error, "part 1 failed") {
		t.Fatalf("assessment = %+v, err = %v; want a blocked failure naming part 1", assessment, err)
	}

	if assessment.RetryCount != 3 {
		t.Fatalf("RetryCount = %d, want 3", assessment.RetryCount)
	}

	if calls.Load() != 1 {
		t.Fatalf("Jev calls = %d, want 1", calls.Load())
	}
}

func TestRunAgentJevServiceUnavailableBlocksWithoutThresholds(t *testing.T) {
	endpoint, _ := newJevEndpoint(t, func(string) (int, string) {
		return http.StatusServiceUnavailable, "unavailable"
	})
	reconciler := newJevTestReconciler(t, endpoint)

	assessment, err := reconciler.runAgentJev(context.Background(), newJevTestVersion(nil, nil), jevLoadContent(t, map[string]string{"SKILL.md": jevEntry}))
	if err == nil || !assessment.Blocked || !strings.Contains(assessment.Error, "part 1 failed") {
		t.Fatalf("assessment = %+v, err = %v; want a blocked 503 naming part 1", assessment, err)
	}
}

func TestRunAgentJevMissingTokenBlocks(t *testing.T) {
	endpoint, calls := newJevEndpoint(t, jevOK)
	reconciler := newJevTestReconciler(t, endpoint)
	reconciler.Client = fake.NewClientBuilder().WithScheme(runtime.NewScheme()).Build()

	assessment, err := reconciler.runAgentJev(context.Background(), newJevTestVersion(nil, nil), jevLoadContent(t, map[string]string{"SKILL.md": jevEntry}))
	if err == nil || !assessment.Blocked {
		t.Fatalf("assessment = %+v, err = %v; want a blocked missing-token failure", assessment, err)
	}

	if calls.Load() != 0 {
		t.Fatalf("Jev calls = %d, want 0", calls.Load())
	}
}

// TestRunAgentJevMissingTokenCountsRetry covers D11: an unreadable token Secret is a Service outcome. It returns an
// error, adds 1 to retryCount, and makes no call.
func TestRunAgentJevMissingTokenCountsRetry(t *testing.T) {
	endpoint, calls := newJevEndpoint(t, jevOK)
	reconciler := newJevTestReconciler(t, endpoint)
	reconciler.Client = fake.NewClientBuilder().WithScheme(runtime.NewScheme()).Build()
	cached := &opendepotv1alpha1.JevAssessment{Error: "old", ChunkCount: 1, RetryCount: 1}

	assessment, err := reconciler.runAgentJev(context.Background(), newJevTestVersion(nil, cached), jevLoadContent(t, map[string]string{"SKILL.md": jevEntry}))
	var serviceErr *jevServiceError
	if !errors.As(err, &serviceErr) || !assessment.Blocked || assessment.RetryCount != 2 {
		t.Fatalf("assessment = %+v, err = %v; want a blocked Service failure with retryCount 2", assessment, err)
	}

	if calls.Load() != 0 {
		t.Fatalf("Jev calls = %d, want 0", calls.Load())
	}
}

// TestRunAgentJevSplitFailureIsContentOutcome covers D11: a content split failure is a Content outcome. It returns no
// error, keeps retryCount, and makes no call.
func TestRunAgentJevSplitFailureIsContentOutcome(t *testing.T) {
	endpoint, calls := newJevEndpoint(t, jevOK)
	reconciler := newJevTestReconciler(t, endpoint)
	cached := &opendepotv1alpha1.JevAssessment{Error: "old", ChunkCount: 1, RetryCount: 2}

	content := jevLoadContent(t, map[string]string{"SKILL.md": jevEntry})
	content.entryRel = "missing.md"

	assessment, err := reconciler.runAgentJev(context.Background(), newJevTestVersion(nil, cached), content)
	if err != nil || !assessment.Blocked || assessment.Error != "" || assessment.RetryCount != 2 || assessment.ChunkCount != 0 {
		t.Fatalf("assessment = %+v, err = %v; want a blocked Content outcome with retryCount 2 and no error", assessment, err)
	}

	if len(assessment.BlockReasons) != 1 || !strings.HasPrefix(assessment.BlockReasons[0], "jev assessment incomplete:") {
		t.Fatalf("BlockReasons = %v; want one incomplete reason", assessment.BlockReasons)
	}

	if calls.Load() != 0 {
		t.Fatalf("Jev calls = %d, want 0", calls.Load())
	}
}

func TestRunAgentJevMalformedResponseBlocks(t *testing.T) {
	endpoint, _ := newJevEndpoint(t, func(string) (int, string) {
		return http.StatusOK, "not json"
	})
	reconciler := newJevTestReconciler(t, endpoint)

	assessment, err := reconciler.runAgentJev(context.Background(), newJevTestVersion(nil, nil), jevLoadContent(t, map[string]string{"SKILL.md": jevEntry}))
	if err == nil || !assessment.Blocked || assessment.Error == "" {
		t.Fatalf("assessment = %+v, err = %v; want a blocked malformed-response failure", assessment, err)
	}
}

// TestRunAgentJevDisabledFailsClosedWithoutCall covers D10: a source with jevSecretRef on a controller with Jev
// disabled fails closed, makes no call, and leaves retryCount unchanged on every reconcile.
func TestRunAgentJevDisabledFailsClosedWithoutCall(t *testing.T) {
	for _, policy := range []*opendepotv1alpha1.JevPolicy{nil, {MinSafeProbability: f64(0.995)}} {
		endpoint, calls := newJevEndpoint(t, jevOK)
		reconciler := newJevTestReconciler(t, endpoint)
		reconciler.JevEnabled = false
		cached := &opendepotv1alpha1.JevAssessment{Error: "old", ChunkCount: 1, RetryCount: 2}
		version := newJevTestVersion(policy, cached)

		assessment, err := reconciler.runAgentJev(context.Background(), version, jevLoadContent(t, map[string]string{"SKILL.md": jevEntry}))
		if err == nil || assessment.Error != jevDisabledError || !assessment.Blocked {
			t.Fatalf("assessment = %+v, err = %v; want a blocked disabled failure", assessment, err)
		}

		if len(assessment.BlockReasons) != 1 || assessment.BlockReasons[0] != jevDisabledError {
			t.Fatalf("BlockReasons = %v, want [%s]", assessment.BlockReasons, jevDisabledError)
		}

		if assessment.RetryCount != 2 {
			t.Fatalf("RetryCount = %d, want 2", assessment.RetryCount)
		}

		version.Status.JevAssessment = assessment
		again, _ := reconciler.runAgentJev(context.Background(), version, jevLoadContent(t, map[string]string{"SKILL.md": jevEntry}))
		if again.RetryCount != 2 {
			t.Fatalf("RetryCount after a second reconcile = %d, want 2", again.RetryCount)
		}

		if calls.Load() != 0 {
			t.Fatalf("Jev calls = %d, want 0", calls.Load())
		}
	}
}

// TestRunAgentJevEnabledAfterDisabledIsAttemptZero covers D10: once Jev is enabled, a failure is attempt 0.
func TestRunAgentJevEnabledAfterDisabledIsAttemptZero(t *testing.T) {
	endpoint, _ := newJevEndpoint(t, func(string) (int, string) {
		return http.StatusBadRequest, "bad request"
	})
	reconciler := newJevTestReconciler(t, endpoint)
	cached := &opendepotv1alpha1.JevAssessment{Error: jevDisabledError, ChunkCount: 1, RetryCount: 1}
	version := newJevTestVersion(nil, cached)

	assessment, _ := reconciler.runAgentJev(context.Background(), version, jevLoadContent(t, map[string]string{"SKILL.md": jevEntry}))
	if assessment.RetryCount != 1 {
		t.Fatalf("RetryCount = %d, want 1", assessment.RetryCount)
	}
}

// TestRunAgentJevCachedAssessmentIsNotReassessed covers D7: legacy, synced, and errored assessments are returned
// as they are with no Jev call.
func TestRunAgentJevCachedAssessmentIsNotReassessed(t *testing.T) {
	tests := []struct {
		name        string
		policy      *opendepotv1alpha1.JevPolicy
		cached      *opendepotv1alpha1.JevAssessment
		wantErr     bool
		wantBlocked bool
	}{
		{
			name:   "legacy truncated error publishes without thresholds",
			cached: &opendepotv1alpha1.JevAssessment{Error: "jev assessment incomplete: request state truncated"},
		},
		{
			name:        "legacy blocked assessment stays blocked",
			cached:      &opendepotv1alpha1.JevAssessment{Blocked: true, BlockReasons: []string{"legacy"}},
			wantBlocked: true,
		},
		{
			name:   "legacy minConfidence only is not blocked by low confidence",
			policy: &opendepotv1alpha1.JevPolicy{MinConfidence: f64(0.95)},
			cached: &opendepotv1alpha1.JevAssessment{RiskConfidence: f64(0.1)},
		},
		{
			name:    "legacy errored assessment with thresholds reports the cached error",
			policy:  &opendepotv1alpha1.JevPolicy{MinSafeProbability: f64(0.5)},
			cached:  &opendepotv1alpha1.JevAssessment{Error: "legacy failure"},
			wantErr: true,
		},
		{
			name:   "synced assessment is reused on restart",
			cached: &opendepotv1alpha1.JevAssessment{ChunkCount: 1, Chunks: []opendepotv1alpha1.JevChunkResult{{Index: 1, Label: "1", Status: jevPartScored}}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			endpoint, calls := newJevEndpoint(t, jevOK)
			reconciler := newJevTestReconciler(t, endpoint)
			version := newJevTestVersion(tt.policy, tt.cached)

			got, err := reconciler.runAgentJev(context.Background(), version, jevLoadContent(t, map[string]string{"SKILL.md": jevEntry}))
			if got != tt.cached {
				t.Fatalf("assessment was replaced, want the cached one")
			}

			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %t", err, tt.wantErr)
			}

			if got.Blocked != tt.wantBlocked {
				t.Fatalf("Blocked = %t, want %t", got.Blocked, tt.wantBlocked)
			}

			if calls.Load() != 0 {
				t.Fatalf("Jev calls = %d, want 0", calls.Load())
			}
		})
	}
}

func TestRunAgentJevCachedDisabledErrorReRunsWhenEnabled(t *testing.T) {
	endpoint, calls := newJevEndpoint(t, jevOK)
	reconciler := newJevTestReconciler(t, endpoint)
	cached := &opendepotv1alpha1.JevAssessment{Error: jevDisabledError, Blocked: true}

	assessment, err := reconciler.runAgentJev(context.Background(), newJevTestVersion(nil, cached), jevLoadContent(t, map[string]string{"SKILL.md": jevEntry}))
	if err != nil || assessment.Error != "" || calls.Load() != 1 {
		t.Fatalf("assessment = %+v, err = %v, calls = %d; want a clean re-run", assessment, err, calls.Load())
	}
}

// TestRunAgentJevForceSyncResetsRetryCount covers D7 and D11: ForceSync runs as attempt 0 and resets retryCount.
func TestRunAgentJevForceSyncResetsRetryCount(t *testing.T) {
	endpoint, calls := newJevEndpoint(t, jevOK)
	reconciler := newJevTestReconciler(t, endpoint)
	cached := &opendepotv1alpha1.JevAssessment{Error: "old", ChunkCount: 1, RetryCount: 4}
	version := newJevTestVersion(nil, cached)
	version.Spec.ForceSync = true

	assessment, err := reconciler.runAgentJev(context.Background(), version, jevLoadContent(t, map[string]string{"SKILL.md": jevEntry}))
	if err != nil || assessment.Error != "" || assessment.RetryCount != 0 {
		t.Fatalf("assessment = %+v, err = %v; want a scored assessment with retryCount 0", assessment, err)
	}

	if calls.Load() != 1 {
		t.Fatalf("Jev calls = %d, want 1", calls.Load())
	}
}

// TestRunAgentJevRetryLimit covers D11: automatic retries stop after four failed attempts.
func TestRunAgentJevRetryLimit(t *testing.T) {
	tests := []struct {
		name       string
		retryCount int32
		wantCalls  int32
		wantCount  int32
	}{
		{name: "first automatic retry", retryCount: 0, wantCalls: 1, wantCount: 1},
		{name: "last automatic retry", retryCount: 3, wantCalls: 1, wantCount: 4},
		{name: "budget exhausted", retryCount: 4, wantCalls: 0, wantCount: 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			endpoint, calls := newJevEndpoint(t, func(string) (int, string) {
				return http.StatusBadRequest, "bad request"
			})
			reconciler := newJevTestReconciler(t, endpoint)
			cached := &opendepotv1alpha1.JevAssessment{Error: "old", ChunkCount: 1, RetryCount: tt.retryCount}

			assessment, _ := reconciler.runAgentJev(context.Background(), newJevTestVersion(nil, cached), jevLoadContent(t, map[string]string{"SKILL.md": jevEntry}))
			if assessment.RetryCount != tt.wantCount {
				t.Fatalf("RetryCount = %d, want %d", assessment.RetryCount, tt.wantCount)
			}

			if calls.Load() != tt.wantCalls {
				t.Fatalf("Jev calls = %d, want %d", calls.Load(), tt.wantCalls)
			}
		})
	}
}

// TestRunAgentJevAuthFailureKeepsRetryCount covers D11: a 401 or 403 does not increment retryCount.
func TestRunAgentJevAuthFailureKeepsRetryCount(t *testing.T) {
	endpoint, _ := newJevEndpoint(t, func(string) (int, string) {
		return http.StatusUnauthorized, "unauthorized"
	})
	reconciler := newJevTestReconciler(t, endpoint)
	cached := &opendepotv1alpha1.JevAssessment{Error: "old", ChunkCount: 1, RetryCount: 2}

	assessment, err := reconciler.runAgentJev(context.Background(), newJevTestVersion(nil, cached), jevLoadContent(t, map[string]string{"SKILL.md": jevEntry}))
	if err == nil || !assessment.Blocked || assessment.RetryCount != 2 {
		t.Fatalf("assessment = %+v, err = %v; want a blocked 401 with retryCount 2", assessment, err)
	}
}

// TestRunAgentJevCeilingBlocksWithoutCalls covers D2 and D11: a Version over 16 parts makes no call and keeps
// retryCount.
func TestRunAgentJevCeilingBlocksWithoutCalls(t *testing.T) {
	endpoint, calls := newJevEndpoint(t, jevOK)
	reconciler := newJevTestReconciler(t, endpoint)
	cached := &opendepotv1alpha1.JevAssessment{Error: "old", ChunkCount: 1, RetryCount: 1}

	assessment, err := reconciler.runAgentJev(context.Background(), newJevTestVersion(nil, cached), jevMarkedContent(t, 17, nil))
	if err != nil || !assessment.Blocked || assessment.ChunkCount < 17 || len(assessment.Chunks) != 0 {
		t.Fatalf("assessment = %+v, err = %v; want a blocked ceiling result", assessment, err)
	}

	if assessment.RetryCount != 1 {
		t.Fatalf("RetryCount = %d, want 1", assessment.RetryCount)
	}

	if calls.Load() != 0 {
		t.Fatalf("Jev calls = %d, want 0", calls.Load())
	}
}

// TestRunAgentJevUnreadableContentBlocksBeforeCalls covers D8: binary and withheld files block before any call.
func TestRunAgentJevUnreadableContentBlocksBeforeCalls(t *testing.T) {
	endpoint, calls := newJevEndpoint(t, jevOK)
	reconciler := newJevTestReconciler(t, endpoint)

	binary := jevLoadContent(t, map[string]string{
		"SKILL.md": jevEntry,
		"img.png":  "\x89PNG\x00\x00\x00\rIHDR",
		"empty.md": "",
	})

	assessment, err := reconciler.runAgentJev(context.Background(), newJevTestVersion(nil, nil), binary)
	if err != nil || !assessment.Blocked {
		t.Fatalf("assessment = %+v, err = %v; want a blocked binary result", assessment, err)
	}

	if len(assessment.SkippedFiles) != 1 || assessment.SkippedFiles[0].Path != "img.png" || assessment.SkippedFiles[0].Reason != "Binary" {
		t.Fatalf("SkippedFiles = %+v, want img.png as Binary", assessment.SkippedFiles)
	}

	if got := jevChunkStatus(assessment, "1"); got != jevPartNotSent {
		t.Fatalf("part 1 status = %q, want %q", got, jevPartNotSent)
	}

	flagged := jevLoadContent(t, map[string]string{"SKILL.md": jevEntry, "bad.md": "leaked"})
	flagged.flaggedFiles = map[string]struct{}{"bad.md": {}}

	assessment, err = reconciler.runAgentJev(context.Background(), newJevTestVersion(nil, nil), flagged)
	if err != nil || !assessment.Blocked || len(assessment.SkippedFiles) != 1 || assessment.SkippedFiles[0].Reason != "Withheld" {
		t.Fatalf("assessment = %+v, err = %v; want a withheld flagged file", assessment, err)
	}

	if calls.Load() != 0 {
		t.Fatalf("Jev calls = %d, want 0", calls.Load())
	}
}

// TestRunAgentJevHalvesTokenLimitPart covers D6: a token-limit rejection of part 3 sends 3a and 3b.
func TestRunAgentJevHalvesTokenLimitPart(t *testing.T) {
	endpoint, calls := newJevEndpoint(t, jevRejectMark("MARK-3", 1))
	reconciler := newJevTestReconciler(t, endpoint)

	assessment, err := reconciler.runAgentJev(context.Background(), newJevTestVersion(nil, nil), jevMarkedContent(t, 3, map[int]string{3: "MARK-3"}))
	if err != nil || assessment.Blocked || assessment.Error != "" {
		t.Fatalf("assessment = %+v, err = %v; want a clean halved assessment", assessment, err)
	}

	for _, label := range []string{"3a", "3b"} {
		if got := jevChunkStatus(assessment, label); got != jevPartScored {
			t.Fatalf("part %s status = %q, want %q", label, got, jevPartScored)
		}
	}

	if calls.Load() != 5 {
		t.Fatalf("Jev calls = %d, want 5 (2 parts, 1 rejection, 2 halves)", calls.Load())
	}
}

// TestRunAgentJevHalvesAgainOnSecondRejection covers D6: a second rejection splits the second half again.
func TestRunAgentJevHalvesAgainOnSecondRejection(t *testing.T) {
	endpoint, calls := newJevEndpoint(t, jevRejectMark("MARK-3", 2))
	reconciler := newJevTestReconciler(t, endpoint)

	assessment, err := reconciler.runAgentJev(context.Background(), newJevTestVersion(nil, nil), jevMarkedContent(t, 3, map[int]string{3: "MARK-3"}))
	if err != nil || assessment.Blocked {
		t.Fatalf("assessment = %+v, err = %v; want a clean assessment", assessment, err)
	}

	for _, label := range []string{"3a", "3ba", "3bb"} {
		if got := jevChunkStatus(assessment, label); got != jevPartScored {
			t.Fatalf("part %s status = %q, want %q", label, got, jevPartScored)
		}
	}

	if calls.Load() != 7 {
		t.Fatalf("Jev calls = %d, want 7", calls.Load())
	}
}

// TestRunAgentJevDepthTwoRejectionIsTokenLimitExceeded covers D6: a rejection past the second halving is not split.
func TestRunAgentJevDepthTwoRejectionIsTokenLimitExceeded(t *testing.T) {
	endpoint, _ := newJevEndpoint(t, jevRejectMark("MARK-3", 1<<30))
	reconciler := newJevTestReconciler(t, endpoint)

	assessment, err := reconciler.runAgentJev(context.Background(), newJevTestVersion(nil, nil), jevMarkedContent(t, 3, map[int]string{3: "MARK-3"}))
	if err != nil || !assessment.Blocked {
		t.Fatalf("assessment = %+v, err = %v; want a blocked token-limit result", assessment, err)
	}

	var found bool
	for _, c := range assessment.Chunks {
		if c.Status == jevPartTokenLimitExceeded {
			found = true
		}
	}

	if !found {
		t.Fatalf("no TokenLimitExceeded part in %+v", assessment.Chunks)
	}

	if len(assessment.BlockReasons) == 0 || !strings.Contains(strings.Join(assessment.BlockReasons, " "), "cannot be split further") {
		t.Fatalf("BlockReasons = %v, want the token-limit reason", assessment.BlockReasons)
	}
}

// TestRunAgentJevNoBudgetLeftIsTokenLimitExceeded covers D6: with 16 parts no halving budget remains.
func TestRunAgentJevNoBudgetLeftIsTokenLimitExceeded(t *testing.T) {
	endpoint, calls := newJevEndpoint(t, jevRejectMark("MARK-16", 1<<30))
	reconciler := newJevTestReconciler(t, endpoint)

	assessment, err := reconciler.runAgentJev(context.Background(), newJevTestVersion(nil, nil), jevMarkedContent(t, 16, map[int]string{16: "MARK-16"}))
	if err != nil || !assessment.Blocked || jevChunkStatus(assessment, "16") != jevPartTokenLimitExceeded {
		t.Fatalf("assessment = %+v, err = %v; want part 16 TokenLimitExceeded", assessment, err)
	}

	if calls.Load() != 16 {
		t.Fatalf("Jev calls = %d, want exactly 16", calls.Load())
	}
}

// TestRunAgentJevCallBudgetWithTwoTokenLimitParts covers D6: with 15 parts the budget never exceeds 16 calls.
func TestRunAgentJevCallBudgetWithTwoTokenLimitParts(t *testing.T) {
	endpoint, calls := newJevEndpoint(t, func(state string) (int, string) {
		if strings.Contains(state, "MARK-14") || strings.Contains(state, "MARK-15") {
			return http.StatusBadRequest, "max_tokens_exceeded"
		}

		return http.StatusOK, jevOKResponse
	})
	reconciler := newJevTestReconciler(t, endpoint)

	assessment, err := reconciler.runAgentJev(context.Background(), newJevTestVersion(nil, nil), jevMarkedContent(t, 15, map[int]string{14: "MARK-14", 15: "MARK-15"}))
	if err != nil || !assessment.Blocked {
		t.Fatalf("assessment = %+v, err = %v; want a blocked result", assessment, err)
	}

	if calls.Load() > 16 {
		t.Fatalf("Jev calls = %d, want at most 16", calls.Load())
	}
}

// TestRunAgentJevOtherBadRequestDoesNotHalve covers D6: a 400 without max_tokens_exceeded is Failed and never split.
func TestRunAgentJevOtherBadRequestDoesNotHalve(t *testing.T) {
	endpoint, calls := newJevEndpoint(t, func(state string) (int, string) {
		return http.StatusBadRequest, "malformed input"
	})
	reconciler := newJevTestReconciler(t, endpoint)

	assessment, err := reconciler.runAgentJev(context.Background(), newJevTestVersion(nil, nil), jevMarkedContent(t, 3, nil))
	if err == nil || jevChunkStatus(assessment, "1") != jevPartFailed {
		t.Fatalf("assessment = %+v, err = %v; want Failed parts", assessment, err)
	}

	if calls.Load() != 3 {
		t.Fatalf("Jev calls = %d, want 3", calls.Load())
	}
}

// inFlightTracker records the highest number of requests in flight at once.
type inFlightTracker struct {
	current atomic.Int32
	peak    atomic.Int32
}

func (tr *inFlightTracker) stub(delay time.Duration) jevStub {
	return func(string) (int, string) {
		n := tr.current.Add(1)
		for {
			peak := tr.peak.Load()
			if n <= peak || tr.peak.CompareAndSwap(peak, n) {
				break
			}
		}

		time.Sleep(delay)
		tr.current.Add(-1)

		return http.StatusOK, jevOKResponse
	}
}

// TestRunAgentJevLimitsInFlightToSixteen covers D1: 16 parts never run more than 16 requests at once.
func TestRunAgentJevLimitsInFlightToSixteen(t *testing.T) {
	var tracker inFlightTracker
	endpoint, calls := newJevEndpoint(t, tracker.stub(30*time.Millisecond))
	reconciler := newJevTestReconciler(t, endpoint)
	reconciler.jevSem = make(chan struct{}, jevMaxInFlight)

	assessment, err := reconciler.runAgentJev(context.Background(), newJevTestVersion(nil, nil), jevMarkedContent(t, 16, nil))
	if err != nil || assessment.Blocked || assessment.AssessedChunks != 16 {
		t.Fatalf("assessment = %+v, err = %v; want 16 assessed parts", assessment, err)
	}

	if calls.Load() != 16 {
		t.Fatalf("Jev calls = %d, want 16", calls.Load())
	}

	if tracker.peak.Load() > jevMaxInFlight || tracker.peak.Load() < 2 {
		t.Fatalf("peak in flight = %d, want between 2 and %d", tracker.peak.Load(), jevMaxInFlight)
	}
}

// TestRunAgentJevSharesSemaphoreAcrossVersions covers D1: two Versions on one reconciler share the 16 slots.
func TestRunAgentJevSharesSemaphoreAcrossVersions(t *testing.T) {
	var tracker inFlightTracker
	endpoint, _ := newJevEndpoint(t, tracker.stub(30*time.Millisecond))
	reconciler := newJevTestReconciler(t, endpoint)
	reconciler.jevSem = make(chan struct{}, jevMaxInFlight)

	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			if _, err := reconciler.runAgentJev(context.Background(), newJevTestVersion(nil, nil), jevMarkedContent(t, 16, nil)); err != nil {
				t.Errorf("runAgentJev: %v", err)
			}
		})
	}

	wg.Wait()

	if tracker.peak.Load() > jevMaxInFlight {
		t.Fatalf("peak in flight across two Versions = %d, want at most %d", tracker.peak.Load(), jevMaxInFlight)
	}
}

// TestRunAgentJevPartFailureDoesNotStopOthers covers D3 and Concurrency: one 503 leaves the other parts recorded.
func TestRunAgentJevPartFailureDoesNotStopOthers(t *testing.T) {
	endpoint, _ := newJevEndpoint(t, func(state string) (int, string) {
		if strings.Contains(state, "MARK-3") {
			return http.StatusServiceUnavailable, "unavailable"
		}

		return http.StatusOK, jevOKResponse
	})
	reconciler := newJevTestReconciler(t, endpoint)

	assessment, err := reconciler.runAgentJev(context.Background(), newJevTestVersion(nil, nil), jevMarkedContent(t, 3, map[int]string{3: "MARK-3"}))
	if err == nil || !assessment.Blocked {
		t.Fatalf("assessment = %+v, err = %v; want a blocked result", assessment, err)
	}

	if assessment.AssessedChunks != 2 {
		t.Fatalf("AssessedChunks = %d, want 2", assessment.AssessedChunks)
	}

	if !strings.Contains(assessment.Error, "part 3 failed") {
		t.Fatalf("Error = %q, want it to name part 3", assessment.Error)
	}
}

// TestRunAgentJevDeadlineLeavesPartsUnsent covers Concurrency: an expired deadline leaves parts NotSent or Failed,
// and every semaphore slot is released.
func TestRunAgentJevDeadlineLeavesPartsUnsent(t *testing.T) {
	endpoint, _ := newJevEndpoint(t, func(string) (int, string) {
		time.Sleep(300 * time.Millisecond)

		return http.StatusOK, jevOKResponse
	})
	reconciler := newJevTestReconciler(t, endpoint)
	reconciler.jevSem = make(chan struct{}, jevMaxInFlight)
	reconciler.jevDeadline = 50 * time.Millisecond

	assessment, _ := reconciler.runAgentJev(context.Background(), newJevTestVersion(nil, nil), jevMarkedContent(t, 4, nil))
	for _, c := range assessment.Chunks {
		if c.Status != jevPartNotSent && c.Status != jevPartFailed {
			t.Fatalf("part %s status = %q, want NotSent or Failed", c.Label, c.Status)
		}
	}

	if len(reconciler.jevSem) != 0 {
		t.Fatalf("semaphore length = %d after the deadline, want 0", len(reconciler.jevSem))
	}
}

// TestRunAgentJevCancelWhileWaitingOnSemaphore covers Concurrency: a canceled context returns while parts wait for a slot.
func TestRunAgentJevCancelWhileWaitingOnSemaphore(t *testing.T) {
	endpoint, calls := newJevEndpoint(t, jevOK)
	reconciler := newJevTestReconciler(t, endpoint)
	reconciler.jevSem = make(chan struct{}, 1)
	reconciler.jevSem <- struct{}{}

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)

	done := make(chan *opendepotv1alpha1.JevAssessment, 1)
	go func() {
		assessment, _ := reconciler.runAgentJev(ctx, newJevTestVersion(nil, nil), jevMarkedContent(t, 2, nil))
		done <- assessment
	}()

	select {
	case assessment := <-done:
		if assessment == nil {
			t.Fatal("runAgentJev returned a nil assessment")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runAgentJev did not return after the context was canceled")
	}

	if calls.Load() != 0 {
		t.Fatalf("Jev calls = %d, want 0", calls.Load())
	}
}

// TestRunAgentJevOrderIsStableAcrossCompletionOrders covers Concurrency: results are sorted by label whatever the
// completion order.
func TestRunAgentJevOrderIsStableAcrossCompletionOrders(t *testing.T) {
	endpoint, _ := newJevEndpoint(t, func(string) (int, string) {
		time.Sleep(time.Duration(rand.IntN(30)) * time.Millisecond)

		return http.StatusOK, jevOKResponse
	})
	reconciler := newJevTestReconciler(t, endpoint)

	assessment, err := reconciler.runAgentJev(context.Background(), newJevTestVersion(nil, nil), jevMarkedContent(t, 4, nil))
	if err != nil {
		t.Fatalf("runAgentJev: %v", err)
	}

	want := []string{"1", "2", "3", "4"}
	got := jevLabels(assessment)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("labels = %v, want %v", got, want)
	}
}

// TestRunAgentJevStillLimitsRateWhenLimiterIsSet covers D1: a shared limiter does not change the outcome.
func TestRunAgentJevStillLimitsRateWhenLimiterIsSet(t *testing.T) {
	endpoint, calls := newJevEndpoint(t, jevOK)
	reconciler := newJevTestReconciler(t, endpoint)
	reconciler.jevLimiter = rate.NewLimiter(rate.Limit(1000)/60, jevRateBurst)

	assessment, err := reconciler.runAgentJev(context.Background(), newJevTestVersion(nil, nil), jevMarkedContent(t, 2, nil))
	if err != nil || assessment.Blocked || calls.Load() != 2 {
		t.Fatalf("assessment = %+v, err = %v, calls = %d; want two assessed parts", assessment, err, calls.Load())
	}
}

// jevCRDSchema is the part of an OpenAPI v3 schema that the chunk index bounds are read from.
type jevCRDSchema struct {
	Properties map[string]jevCRDSchema `json:"properties"`
	Items      *jevCRDSchema           `json:"items"`
	Minimum    *int64                  `json:"minimum"`
	Maximum    *int64                  `json:"maximum"`
}

// jevCRDIndexBounds returns the minimum and maximum of JevChunkResult.index from the Versions CRD manifest.
func jevCRDIndexBounds(t *testing.T) (int64, int64) {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "chart", "opendepot", "crds", "opendepot.defdev.io_versions.yaml"))
	if err != nil {
		t.Fatalf("read Versions CRD: %v", err)
	}

	var crd struct {
		Spec struct {
			Versions []struct {
				Schema struct {
					OpenAPIV3Schema jevCRDSchema `json:"openAPIV3Schema"`
				} `json:"schema"`
			} `json:"versions"`
		} `json:"spec"`
	}
	if err := yaml.Unmarshal(data, &crd); err != nil || len(crd.Spec.Versions) == 0 {
		t.Fatalf("parse Versions CRD: %v", err)
	}

	index := crd.Spec.Versions[0].Schema.OpenAPIV3Schema.Properties["status"].
		Properties["jevAssessment"].Properties["chunks"].Items.Properties["index"]
	if index.Minimum == nil || index.Maximum == nil {
		t.Fatal("Versions CRD does not bound JevChunkResult.index")
	}

	return *index.Minimum, *index.Maximum
}

// TestJevChunksIndexesAreOneBasedWithinCRDBounds checks that chunk indexes run 1 to n, match their labels, and stay
// within the bounds the CRD enforces. The API server rejects a status write with an index outside those bounds.
func TestJevChunksIndexesAreOneBasedWithinCRDBounds(t *testing.T) {
	minimum, maximum := jevCRDIndexBounds(t)
	if minimum != 1 || maximum != jevMaxChunks {
		t.Fatalf("CRD index bounds = [%d, %d], want [1, %d]", minimum, maximum, jevMaxChunks)
	}

	plain := make([]string, 0, jevMaxChunks)
	for i := 1; i <= jevMaxChunks; i++ {
		plain = append(plain, strconv.Itoa(i))
	}

	halved := []string{"1", "2a", "2b"}
	for i := 3; i < jevMaxChunks; i++ {
		halved = append(halved, strconv.Itoa(i))
	}

	for name, labels := range map[string][]string{"plain": plain, "halved": halved} {
		outcomes := make([]jevPartOutcome, 0, len(labels))
		for _, label := range labels {
			outcomes = append(outcomes, jevPartOutcome{result: jevPartResult{label: label, status: jevPartNotSent}})
		}

		chunks := jevChunks(outcomes)
		if len(chunks) != len(labels) {
			t.Fatalf("%s: got %d chunks, want %d", name, len(chunks), len(labels))
		}

		for i, c := range chunks {
			wantIndex, _ := jevSplitLabel(labels[i])
			if int64(c.Index) != int64(wantIndex) {
				t.Fatalf("%s: chunks[%d].Index = %d, want %d from label %q", name, i, c.Index, wantIndex, labels[i])
			}

			if int64(c.Index) < minimum || int64(c.Index) > maximum {
				t.Fatalf("%s: chunks[%d].Index = %d is outside the CRD bounds [%d, %d]", name, i, c.Index, minimum, maximum)
			}

			if c.Label != labels[i] {
				t.Fatalf("%s: chunks[%d].Label = %q, want %q", name, i, c.Label, labels[i])
			}
		}
	}
}
