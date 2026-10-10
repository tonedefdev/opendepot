/*
Copyright 2026.

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
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	"github.com/tonedefdev/opendepot/pkg/archive"
)

const bumpSkillDocument = "---\nname: bump\ndescription: Summarize documents.\n---\n# Skill\nSummarize documents.\n"

func TestBumpCategory(t *testing.T) {
	tests := []struct {
		name   string
		major  float64
		minor  float64
		policy *opendepotv1alpha1.BranchPolicy
		want   string
	}{
		{name: "major at threshold", major: 0.5, minor: 0.0, want: "Major"},
		{name: "major above threshold beats minor", major: 0.6, minor: 0.4, want: "Major"},
		{name: "minor combined at threshold", major: 0.2, minor: 0.3, want: "Minor"},
		{name: "minor from major and minor", major: 0.4, minor: 0.1, want: "Minor"},
		{name: "patch below combined threshold", major: 0.2, minor: 0.2, want: "Patch"},
		{name: "patch all zero", major: 0.0, minor: 0.0, want: "Patch"},
		{
			name:   "lower major threshold makes a major probability of 0.4 MAJOR",
			major:  0.4,
			minor:  0.0,
			policy: &opendepotv1alpha1.BranchPolicy{MajorThreshold: ptr.To(0.3)},
			want:   "Major",
		},
		{
			name:   "higher major threshold keeps a major probability of 0.6 from being MAJOR",
			major:  0.6,
			minor:  0.1,
			policy: &opendepotv1alpha1.BranchPolicy{MajorThreshold: ptr.To(0.9)},
			want:   "Minor",
		},
		{
			name:   "lower minor threshold makes a combined probability of 0.2 MINOR",
			major:  0.1,
			minor:  0.1,
			policy: &opendepotv1alpha1.BranchPolicy{MinorThreshold: ptr.To(0.2)},
			want:   "Minor",
		},
		{
			name:   "higher minor threshold makes a combined probability of 0.6 PATCH",
			major:  0.3,
			minor:  0.3,
			policy: &opendepotv1alpha1.BranchPolicy{MinorThreshold: ptr.To(0.7)},
			want:   "Patch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := bumpCategory(tt.major, tt.minor, tt.policy); got != tt.want {
				t.Fatalf("bumpCategory(%v, %v) = %q, want %q", tt.major, tt.minor, got, tt.want)
			}
		})
	}
}

func TestBumpBackoffSequence(t *testing.T) {
	want := []time.Duration{time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 15 * time.Minute, 15 * time.Minute}
	for i, expected := range want {
		if got := bumpBackoff(i + 1); got != expected {
			t.Fatalf("bumpBackoff(%d) = %s, want %s", i+1, got, expected)
		}
	}

	if got := bumpBackoff(0); got != time.Minute {
		t.Fatalf("bumpBackoff(0) = %s, want 1m", got)
	}
}

func TestVerifyPreviousChecksum(t *testing.T) {
	sum := "sha256:abc"
	other := "sha256:def"

	if err := verifyPreviousChecksum(nil, &sum); err == nil {
		t.Fatal("nil recorded checksum: expected an error")
	}

	if err := verifyPreviousChecksum(&sum, nil); err == nil {
		t.Fatal("nil downloaded checksum: expected an error")
	}

	if err := verifyPreviousChecksum(&sum, &other); err == nil {
		t.Fatal("mismatched checksum: expected an error")
	}

	same := sum
	if err := verifyPreviousChecksum(&sum, &same); err != nil {
		t.Fatalf("matching checksum: unexpected error %v", err)
	}
}

func TestTruncateUTF8KeepsRuneBoundaries(t *testing.T) {
	input := strings.Repeat("é", 10)
	got := truncateUTF8(input, 5)
	if len(got) > 5 {
		t.Fatalf("len(truncated) = %d, want <= 5", len(got))
	}

	if got != "éé" {
		t.Fatalf("truncated = %q, want %q", got, "éé")
	}

	if truncateUTF8("short", 100) != "short" {
		t.Fatal("input under the limit must be returned unchanged")
	}
}

func TestSameArchiveProducesSameChecksum(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(bumpSkillDocument), 0o600); err != nil {
		t.Fatalf("write skill: %v", err)
	}

	first := packBumpTree(t, dir)
	time.Sleep(10 * time.Millisecond)
	second := packBumpTree(t, dir)
	if !bytes.Equal(first, second) {
		t.Fatal("packing the same tree twice must produce identical bytes so checksums match")
	}
}

func packBumpTree(t *testing.T, dir string) []byte {
	t.Helper()

	var buf bytes.Buffer
	if err := archive.WriteDeterministicTarGz(dir, &buf); err != nil {
		t.Fatalf("WriteDeterministicTarGz: %v", err)
	}

	return buf.Bytes()
}

func writeBumpDir(t *testing.T, files map[string]string) string {
	t.Helper()

	dir := t.TempDir()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	return dir
}

func writeBumpTree(t *testing.T, files map[string]string) []byte {
	t.Helper()

	return packBumpTree(t, writeBumpDir(t, files))
}

// bumpJevServer returns a Jev stub that records the request state and answers with the supplied status and body.
func bumpJevServer(t *testing.T, status int, body string, states *[]string, calls *atomic.Int32) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		raw, _ := io.ReadAll(r.Body)

		var payload struct {
			State string `json:"state"`
		}
		_ = json.Unmarshal(raw, &payload)
		*states = append(*states, payload.State)

		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)

	return server
}

func bumpJevResponse(major, minor, patch, confidence float64) string {
	return `{"model":"jev-test","answers":{
		"bump_removes_capability":{"type":"noul","noul":0.1},
		"bump_adds_capability":{"type":"noul","noul":0.1},
		"bump_non_behavioral":{"type":"noul","noul":0.1},
		"bump":{"type":"score","score":1.0,"legend":{"0":"Patch","1":"Minor","2":"Major"},
			"probabilities":{"0":` + ftoa(patch) + `,"1":` + ftoa(minor) + `,"2":` + ftoa(major) + `},
			"confidence":` + ftoa(confidence) + `}}}`
}

func ftoa(f float64) string {
	b, _ := json.Marshal(f)
	return string(b)
}

// newBumpReconciler builds a reconciler with the Jev token Secret and a stubbed Trivy. Trivy reports the previous tree
// as flagged for SKILL.md when flagPrevious is set.
func newBumpReconciler(t *testing.T, endpoint string, flagPrevious bool) (*VersionReconciler, *opendepotv1alpha1.Version) {
	t.Helper()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "jev-token", Namespace: "default"},
		Data:       map[string][]byte{"jevToken": []byte(jevSentinelToken)},
	}

	originalTrivy := runTrivy
	runTrivy = func(_ context.Context, args ...string) ([]byte, error) {
		dir := args[len(args)-1]
		if flagPrevious && strings.Contains(dir, "previous") {
			return []byte(`{"SchemaVersion":2,"Results":[{"Target":"SKILL.md","Secrets":[{"RuleID":"aws-key","Severity":"HIGH","Title":"AWS key"}]}]}`), nil
		}

		return []byte(`{"SchemaVersion":2,"Results":[]}`), nil
	}
	t.Cleanup(func() { runTrivy = originalTrivy })

	name := "bump"
	version := &opendepotv1alpha1.Version{
		ObjectMeta: metav1.ObjectMeta{Name: "bump-1-0-1", Namespace: "default"},
		Spec: opendepotv1alpha1.VersionSpec{
			Type: opendepotv1alpha1.OpenDepotSkill,
			AgentSourceRef: &opendepotv1alpha1.AgentSourceConfig{
				Name: &name,
				JevSecretRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "jev-token"},
				},
			},
		},
	}

	reconciler := &VersionReconciler{
		Client:      fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build(),
		JevEnabled:  true,
		JevEndpoint: endpoint,
		scanSem:     make(chan struct{}, 1),
	}

	return reconciler, version
}

func loadBumpContent(t *testing.T, dir string, version *opendepotv1alpha1.Version) *agentContent {
	t.Helper()

	content, err := loadAgentContent(dir, version)
	if err != nil {
		t.Fatalf("loadAgentContent: %v", err)
	}

	return content
}

func TestClassifyPreparedBumpUsesJevCategory(t *testing.T) {
	var calls atomic.Int32
	var states []string
	server := bumpJevServer(t, http.StatusOK, bumpJevResponse(0.7, 0.2, 0.1, 0.9), &states, &calls)
	reconciler, version := newBumpReconciler(t, server.URL, false)

	previous := writeBumpTree(t, map[string]string{"SKILL.md": bumpSkillDocument})
	nextDir := writeBumpDir(t, map[string]string{"SKILL.md": bumpSkillDocument})
	assessment := &opendepotv1alpha1.BumpAssessment{}
	category, err := reconciler.classifyPreparedBump(context.Background(), version, previous, loadBumpContent(t, nextDir, version), previous, assessment)
	if err != nil {
		t.Fatalf("classifyPreparedBump: %v", err)
	}

	if category != "Major" || assessment.Category != "Major" || assessment.Source != "Jev" {
		t.Fatalf("category = %q assessment = %+v, want Major from Jev", category, assessment)
	}

	if assessment.NeedsReview {
		t.Fatal("confidence 0.9 must not need review")
	}

	if calls.Load() != 1 {
		t.Fatalf("jev calls = %d, want 1", calls.Load())
	}
}

func TestClassifyPreparedBumpMinConfidenceFromPolicy(t *testing.T) {
	var calls atomic.Int32
	var states []string
	server := bumpJevServer(t, http.StatusOK, bumpJevResponse(0.1, 0.1, 0.8, 0.4), &states, &calls)
	reconciler, version := newBumpReconciler(t, server.URL, false)
	version.Spec.AgentSourceRef.BranchPolicy = &opendepotv1alpha1.BranchPolicy{MinConfidence: ptr.To(0.3)}

	previous := writeBumpTree(t, map[string]string{"SKILL.md": bumpSkillDocument})
	nextDir := writeBumpDir(t, map[string]string{"SKILL.md": bumpSkillDocument})
	assessment := &opendepotv1alpha1.BumpAssessment{}
	category, err := reconciler.classifyPreparedBump(context.Background(), version, previous, loadBumpContent(t, nextDir, version), previous, assessment)
	if err != nil {
		t.Fatalf("classifyPreparedBump: %v", err)
	}

	if category != "Patch" || assessment.NeedsReview {
		t.Fatalf("category = %q needsReview = %v, want Patch without needsReview at policy minConfidence 0.3", category, assessment.NeedsReview)
	}
}

func TestClassifyPreparedBumpLowConfidenceNeedsReview(t *testing.T) {
	var calls atomic.Int32
	var states []string
	server := bumpJevServer(t, http.StatusOK, bumpJevResponse(0.1, 0.1, 0.8, 0.4), &states, &calls)
	reconciler, version := newBumpReconciler(t, server.URL, false)

	previous := writeBumpTree(t, map[string]string{"SKILL.md": bumpSkillDocument})
	nextDir := writeBumpDir(t, map[string]string{"SKILL.md": bumpSkillDocument})
	assessment := &opendepotv1alpha1.BumpAssessment{}
	category, err := reconciler.classifyPreparedBump(context.Background(), version, previous, loadBumpContent(t, nextDir, version), previous, assessment)
	if err != nil {
		t.Fatalf("classifyPreparedBump: %v", err)
	}

	if category != "Patch" || !assessment.NeedsReview {
		t.Fatalf("category = %q needsReview = %v, want Patch with needsReview", category, assessment.NeedsReview)
	}
}

func TestClassifyPreparedBumpWithholdsFlaggedEntryWithoutJev(t *testing.T) {
	var calls atomic.Int32
	var states []string
	server := bumpJevServer(t, http.StatusOK, bumpJevResponse(0.0, 0.0, 1.0, 0.9), &states, &calls)
	reconciler, version := newBumpReconciler(t, server.URL, true)

	previous := writeBumpTree(t, map[string]string{"SKILL.md": bumpSkillDocument})
	nextDir := writeBumpDir(t, map[string]string{"SKILL.md": bumpSkillDocument})
	assessment := &opendepotv1alpha1.BumpAssessment{}
	category, err := reconciler.classifyPreparedBump(context.Background(), version, previous, loadBumpContent(t, nextDir, version), previous, assessment)
	if err != nil {
		t.Fatalf("classifyPreparedBump: %v", err)
	}

	if category != "Major" || assessment.Source != "Withheld" {
		t.Fatalf("category = %q source = %q, want Major Withheld", category, assessment.Source)
	}

	if calls.Load() != 0 {
		t.Fatalf("jev calls = %d, want 0 when the entry is flagged", calls.Load())
	}
}

func TestClassifyPreparedBumpExcludesFlaggedContentFromJevState(t *testing.T) {
	var calls atomic.Int32
	var states []string
	server := bumpJevServer(t, http.StatusOK, bumpJevResponse(0.0, 0.0, 1.0, 0.9), &states, &calls)
	reconciler, version := newBumpReconciler(t, server.URL, false)

	const secretValue = "LEAKED-CREDENTIAL-VALUE"
	nextDir := writeBumpDir(t, map[string]string{
		"SKILL.md":   bumpSkillDocument,
		"secret.txt": secretValue,
	})
	next := packBumpTree(t, nextDir)
	previous := writeBumpTree(t, map[string]string{"SKILL.md": bumpSkillDocument})

	tree := loadBumpContent(t, nextDir, version)
	tree.flaggedFiles = map[string]struct{}{"secret.txt": {}}

	assessment := &opendepotv1alpha1.BumpAssessment{}
	if _, err := reconciler.classifyPreparedBump(context.Background(), version, previous, tree, next, assessment); err != nil {
		t.Fatalf("classifyPreparedBump: %v", err)
	}

	if len(states) != 1 {
		t.Fatalf("states = %d, want 1", len(states))
	}

	if strings.Contains(states[0], secretValue) {
		t.Fatal("flagged file content must not reach Jev")
	}
}

func TestClassifyPreparedBumpJevErrorIsReturned(t *testing.T) {
	var calls atomic.Int32
	var states []string
	server := bumpJevServer(t, http.StatusBadRequest, `{"error":"bad"}`, &states, &calls)
	reconciler, version := newBumpReconciler(t, server.URL, false)

	previous := writeBumpTree(t, map[string]string{"SKILL.md": bumpSkillDocument})
	nextDir := writeBumpDir(t, map[string]string{"SKILL.md": bumpSkillDocument})
	assessment := &opendepotv1alpha1.BumpAssessment{}
	if _, err := reconciler.classifyPreparedBump(context.Background(), version, previous, loadBumpContent(t, nextDir, version), previous, assessment); err == nil {
		t.Fatal("expected an error when Jev rejects the request")
	}
}

var _ = Describe("Branch Version bump hold", func() {
	ctx := context.Background()

	newHeldVersion := func(name string, sha string) *opendepotv1alpha1.Version {
		parent := "bump-hold"
		return &opendepotv1alpha1.Version{
			ObjectMeta: metav1.ObjectMeta{
				Name:       name,
				Namespace:  "default",
				Finalizers: []string{opendepotv1alpha1.OpenDepotFinalizer},
				Labels:     map[string]string{"opendepot.defdev.io/skill": parent},
			},
			Spec: opendepotv1alpha1.VersionSpec{
				Type:         opendepotv1alpha1.OpenDepotSkill,
				SourceCommit: sha,
				AgentSourceRef: &opendepotv1alpha1.AgentSourceConfig{
					Name:      ptr.To(parent),
					RepoOwner: "example",
					Path:      "skills/" + parent,
					Ref:       ptr.To("main"),
					JevSecretRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: "jev-token"},
					},
				},
			},
		}
	}

	It("holds a branch Version without a semver when Jev classification fails and backs off", func() {
		first := newHeldVersion("bump-hold-a-skill", testBranchSHAOne)
		Expect(k8sClient.Create(ctx, first)).To(Succeed())
		second := newHeldVersion("bump-hold-b-skill", testBranchSHATwo)
		Expect(k8sClient.Create(ctx, second)).To(Succeed())
		DeferCleanup(func() {
			for _, name := range []string{first.Name, second.Name} {
				current := &opendepotv1alpha1.Version{}
				key := types.NamespacedName{Name: name, Namespace: "default"}
				if err := k8sClient.Get(ctx, key, current); err == nil {
					current.Finalizers = nil
					_ = k8sClient.Update(ctx, current)
					_ = k8sClient.Delete(ctx, current)
				}
			}
		})

		reconciler := &VersionReconciler{
			Client:                k8sClient,
			APIReader:             k8sClient,
			Scheme:                k8sClient.Scheme(),
			Log:                   logr.Discard(),
			BranchSourcingEnabled: true,
			JevEnabled:            true,
			JevEndpoint:           "http://127.0.0.1:1",
		}

		firstKey := types.NamespacedName{Name: first.Name, Namespace: "default"}
		firstCurrent := &opendepotv1alpha1.Version{}
		Expect(k8sClient.Get(ctx, firstKey, firstCurrent)).To(Succeed())
		_, _, err := reconciler.assignBranchVersion(ctx, ctrl.Request{NamespacedName: firstKey}, firstCurrent, nil, nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(firstCurrent.Spec.Version).To(Equal("1.0.0"))

		secondKey := types.NamespacedName{Name: second.Name, Namespace: "default"}
		secondCurrent := &opendepotv1alpha1.Version{}
		Expect(k8sClient.Get(ctx, secondKey, secondCurrent)).To(Succeed())
		assessment, requeue, err := reconciler.assignBranchVersion(ctx, ctrl.Request{NamespacedName: secondKey}, secondCurrent, nil, nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(requeue).To(Equal(time.Minute))
		Expect(assessment).To(BeNil())

		current := &opendepotv1alpha1.Version{}
		Expect(k8sClient.Get(ctx, secondKey, current)).To(Succeed())
		Expect(current.Spec.Version).To(BeEmpty())
		Expect(current.Status.BranchPhase).To(Equal(branchPhaseHeld))
		Expect(current.Status.BumpAssessment).NotTo(BeNil())
		Expect(current.Status.BumpAssessment.Attempts).To(Equal(1))

		for attempt, expectedBackoff := range []time.Duration{2 * time.Minute, 4 * time.Minute} {
			backoff := holdAgain(reconciler, ctx, secondKey, secondCurrent)
			Expect(backoff).To(Equal(expectedBackoff))

			current = &opendepotv1alpha1.Version{}
			Expect(k8sClient.Get(ctx, secondKey, current)).To(Succeed())
			Expect(current.Status.BumpAssessment.Attempts).To(Equal(attempt + 2))
		}
	})
})

// holdAgain records another failed classification against the Version and returns the resulting backoff.
func holdAgain(reconciler *VersionReconciler, ctx context.Context, key types.NamespacedName, version *opendepotv1alpha1.Version) time.Duration {
	assessment := &opendepotv1alpha1.BumpAssessment{Commit: version.Spec.SourceCommit}
	backoff, err := reconciler.holdBranchBump(ctx, ctrl.Request{NamespacedName: key}, version, assessment, errBumpTest)
	Expect(err).NotTo(HaveOccurred())

	return backoff
}

var errBumpTest = bumpTestError("jev unavailable")

type bumpTestError string

func (e bumpTestError) Error() string { return string(e) }
