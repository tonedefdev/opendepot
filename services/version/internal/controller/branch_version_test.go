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
	"context"
	"strings"
	"testing"
	"time"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	"github.com/tonedefdev/opendepot/pkg/github"
)

const (
	testBranchSHAOne   = "1111111111111111111111111111111111111111"
	testBranchSHATwo   = "2222222222222222222222222222222222222222"
	testBranchSHAThree = "3333333333333333333333333333333333333333"
)

func TestDecodeTrivyReportRejectsMalformedOutput(t *testing.T) {
	tests := []struct {
		name    string
		data    string
		wantErr bool
	}{
		{name: "valid report", data: `{"SchemaVersion":2,"Results":[]}`, wantErr: false},
		{name: "null", data: `null`, wantErr: true},
		{name: "array", data: `[{"SchemaVersion":2}]`, wantErr: true},
		{name: "invalid JSON", data: `{"SchemaVersion":2,`, wantErr: true},
		{name: "empty output", data: ``, wantErr: true},
		{name: "missing SchemaVersion", data: `{"Results":[]}`, wantErr: true},
		{name: "zero SchemaVersion", data: `{"SchemaVersion":0,"Results":[]}`, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := decodeTrivyReport([]byte(tt.data)); (err != nil) != tt.wantErr {
				t.Errorf("decodeTrivyReport() error = %v, wantErr %v", err, tt.wantErr)
			}

			if _, _, err := parseTrivySecrets([]byte(tt.data)); (err != nil) != tt.wantErr {
				t.Errorf("parseTrivySecrets() error = %v, wantErr %v", err, tt.wantErr)
			}

			if _, err := parseTrivyReport([]byte(tt.data), nil); (err != nil) != tt.wantErr {
				t.Errorf("parseTrivyReport() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateCommitSHA(t *testing.T) {
	if err := github.ValidateCommitSHA(testBranchSHAOne); err != nil {
		t.Errorf("ValidateCommitSHA(full SHA) error = %v", err)
	}

	for _, bad := range []string{"main", "1111", "ABCDEF0123456789ABCDEF0123456789ABCDEF01", testBranchSHAOne + "0"} {
		if err := github.ValidateCommitSHA(bad); err == nil {
			t.Errorf("ValidateCommitSHA(%q) should fail", bad)
		}
	}
}

func TestNextBranchVersion(t *testing.T) {
	tests := []struct {
		previous string
		category string
		want     string
	}{
		{previous: "1.0.0", category: "Patch", want: "1.0.1"},
		{previous: "1.0.9", category: "Patch", want: "1.0.10"},
		{previous: "1.2.3", category: "Minor", want: "1.3.0"},
		{previous: "1.2.3", category: "Major", want: "2.0.0"},
	}

	for _, tt := range tests {
		got, err := nextBranchVersion(tt.previous, tt.category)
		if err != nil {
			t.Fatalf("nextBranchVersion(%q, %q) error = %v", tt.previous, tt.category, err)
		}

		if got != tt.want {
			t.Errorf("nextBranchVersion(%q, %q) = %q, want %q", tt.previous, tt.category, got, tt.want)
		}
	}

	if _, err := nextBranchVersion("1.0.0", "Unknown"); err == nil {
		t.Error("nextBranchVersion() with an unknown category should fail")
	}
}

func TestManualBumpCategory(t *testing.T) {
	tests := []struct {
		name       string
		annotation *string
		want       string
		wantErr    bool
	}{
		{name: "unset", annotation: nil, want: ""},
		{name: "major", annotation: ptr.To("major"), want: "Major"},
		{name: "minor", annotation: ptr.To("Minor"), want: "Minor"},
		{name: "patch", annotation: ptr.To("patch"), want: "Patch"},
		{name: "invalid", annotation: ptr.To("huge"), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			version := &opendepotv1alpha1.Version{}
			if tt.annotation != nil {
				version.Annotations = map[string]string{bumpOverrideAnnotation: *tt.annotation}
			}

			got, err := manualBumpCategory(version)
			if (err != nil) != tt.wantErr {
				t.Fatalf("manualBumpCategory() error = %v, wantErr %v", err, tt.wantErr)
			}

			if got != tt.want {
				t.Errorf("manualBumpCategory() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestHighestAssignedSibling(t *testing.T) {
	siblings := []opendepotv1alpha1.Version{
		{ObjectMeta: metav1.ObjectMeta{Name: "a"}, Spec: opendepotv1alpha1.VersionSpec{Version: ""}},
		{ObjectMeta: metav1.ObjectMeta{Name: "b"}, Spec: opendepotv1alpha1.VersionSpec{Version: "1.10.0"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "c"}, Spec: opendepotv1alpha1.VersionSpec{Version: "1.9.0"}},
	}

	highest, err := highestAssignedSibling(siblings)
	if err != nil {
		t.Fatalf("highestAssignedSibling() error = %v", err)
	}

	if highest == nil || highest.Name != "b" {
		t.Fatalf("highestAssignedSibling() = %v, want sibling b", highest)
	}

	if none, err := highestAssignedSibling(siblings[:1]); err != nil || none != nil {
		t.Errorf("highestAssignedSibling() with no assigned sibling = %v, %v; want nil, nil", none, err)
	}
}

var _ = Describe("Branch Version sourcing", func() {
	ctx := context.Background()

	newBranchVersion := func(name, parent, sha string) *opendepotv1alpha1.Version {
		return &opendepotv1alpha1.Version{
			ObjectMeta: metav1.ObjectMeta{
				Name:       name,
				Namespace:  "default",
				Finalizers: []string{opendepotv1alpha1.OpenDepotFinalizer},
				Labels: map[string]string{
					"opendepot.defdev.io/skill": parent,
				},
			},
			Spec: opendepotv1alpha1.VersionSpec{
				Type:         opendepotv1alpha1.OpenDepotSkill,
				SourceCommit: sha,
				AgentSourceRef: &opendepotv1alpha1.AgentSourceConfig{
					Name:      ptr.To(parent),
					RepoOwner: "example",
					Path:      "skills/" + parent,
					Ref:       ptr.To("main"),
				},
			},
		}
	}

	cleanupVersion := func(name string) {
		DeferCleanup(func() {
			current := &opendepotv1alpha1.Version{}
			key := types.NamespacedName{Name: name, Namespace: "default"}
			if err := k8sClient.Get(ctx, key, current); err == nil {
				current.Finalizers = nil
				_ = k8sClient.Update(ctx, current)
				_ = k8sClient.Delete(ctx, current)
			}
		})
	}

	createBranchVersion := func(resource *opendepotv1alpha1.Version) {
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		cleanupVersion(resource.Name)
	}

	reconcilerFor := func(enabled bool) *VersionReconciler {
		return &VersionReconciler{
			Client:                k8sClient,
			APIReader:             k8sClient,
			Scheme:                k8sClient.Scheme(),
			Log:                   logr.Discard(),
			BranchSourcingEnabled: enabled,
		}
	}

	It("rejects a branch Version when branch sourcing is disabled", func() {
		resource := newBranchVersion("bs-flag-off-skill-"+testBranchSHAOne[:8], "bs-flag-off", testBranchSHAOne)
		createBranchVersion(resource)

		key := types.NamespacedName{Name: resource.Name, Namespace: "default"}
		_, err := reconcilerFor(false).Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())

		current := &opendepotv1alpha1.Version{}
		Expect(k8sClient.Get(ctx, key, current)).To(Succeed())
		Expect(current.Status.BranchPhase).To(Equal(branchPhaseRejected))
		Expect(current.Status.SyncStatus).To(ContainSubstring("branch sourcing is disabled"))
		Expect(current.Spec.Version).To(BeEmpty())
	})

	It("waits for an older unassigned sibling", func() {
		older := newBranchVersion("bs-wait-a-skill", "bs-wait", testBranchSHAOne)
		createBranchVersion(older)
		newer := newBranchVersion("bs-wait-b-skill", "bs-wait", testBranchSHATwo)
		createBranchVersion(newer)

		key := types.NamespacedName{Name: newer.Name, Namespace: "default"}
		result, err := reconcilerFor(true).Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(branchWaitRequeue))

		current := &opendepotv1alpha1.Version{}
		Expect(k8sClient.Get(ctx, key, current)).To(Succeed())
		Expect(current.Status.BranchPhase).To(Equal(branchPhasePending))
		Expect(current.Status.SyncStatus).To(Equal("Waiting for bs-wait-a-skill"))
		Expect(current.Spec.Version).To(BeEmpty())
	})

	It("supersedes an older Version when a newer commit exists", func() {
		older := newBranchVersion("bs-super-a-skill", "bs-super", testBranchSHAOne)
		createBranchVersion(older)
		newer := newBranchVersion("bs-super-b-skill", "bs-super", testBranchSHATwo)
		createBranchVersion(newer)

		key := types.NamespacedName{Name: older.Name, Namespace: "default"}
		_, err := reconcilerFor(true).Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())

		current := &opendepotv1alpha1.Version{}
		Expect(k8sClient.Get(ctx, key, current)).To(Succeed())
		Expect(current.Status.BranchPhase).To(Equal(branchPhaseRejected))
		Expect(current.Status.SyncStatus).To(ContainSubstring("Superseded"))
		Expect(current.Spec.Version).To(BeEmpty())
	})

	It("assigns 1.0.0 to the first commit, then increasing unique semvers", func() {
		first := newBranchVersion("bs-assign-a-skill", "bs-assign", testBranchSHAOne)
		createBranchVersion(first)
		second := newBranchVersion("bs-assign-b-skill", "bs-assign", testBranchSHATwo)
		createBranchVersion(second)
		third := newBranchVersion("bs-assign-c-skill", "bs-assign", testBranchSHAThree)
		createBranchVersion(third)

		reconciler := reconcilerFor(true)
		firstKey := types.NamespacedName{Name: first.Name, Namespace: "default"}
		firstCurrent := &opendepotv1alpha1.Version{}
		Expect(k8sClient.Get(ctx, firstKey, firstCurrent)).To(Succeed())
		assessment, requeue, err := reconciler.assignBranchVersion(ctx, ctrl.Request{NamespacedName: firstKey}, firstCurrent, nil, nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(requeue).To(BeZero())
		Expect(assessment.Source).To(Equal("Initial"))
		Expect(firstCurrent.Spec.Version).To(Equal("1.0.0"))

		secondKey := types.NamespacedName{Name: second.Name, Namespace: "default"}
		secondCurrent := &opendepotv1alpha1.Version{}
		Expect(k8sClient.Get(ctx, secondKey, secondCurrent)).To(Succeed())
		assessment, _, err = reconciler.assignBranchVersion(ctx, ctrl.Request{NamespacedName: secondKey}, secondCurrent, nil, nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(assessment.Source).To(Equal("Default"))
		Expect(assessment.Category).To(Equal("Patch"))
		Expect(assessment.PreviousVersion).To(Equal("1.0.0"))
		Expect(secondCurrent.Spec.Version).To(Equal("1.0.1"))

		thirdKey := types.NamespacedName{Name: third.Name, Namespace: "default"}
		thirdCurrent := &opendepotv1alpha1.Version{}
		Expect(k8sClient.Get(ctx, thirdKey, thirdCurrent)).To(Succeed())
		thirdCurrent.Annotations = map[string]string{bumpOverrideAnnotation: "minor"}
		Expect(k8sClient.Update(ctx, thirdCurrent)).To(Succeed())
		Expect(k8sClient.Get(ctx, thirdKey, thirdCurrent)).To(Succeed())
		assessment, _, err = reconciler.assignBranchVersion(ctx, ctrl.Request{NamespacedName: thirdKey}, thirdCurrent, nil, nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(assessment.Source).To(Equal("Manual"))
		Expect(assessment.Category).To(Equal("Minor"))
		Expect(thirdCurrent.Spec.Version).To(Equal("1.1.0"))
	})
})

// TestBranchPreflightJevExemptionReopens covers Step 7: a Rejected branch Version re-opens only for a live Jev
// exemption on its stored digest, and a newer sibling still supersedes it.
func TestBranchPreflightJevExemptionReopens(t *testing.T) {
	const parent = "jev-branch"
	now := time.Now()
	digest := jevExemptTestDigest
	criticalReason := "part skills/agent is rated Critical risk, which always blocks"
	thresholdReason := "destructive probability 0.90 exceeds the policy limit of 0.01"
	pastExpiry := metav1.NewTime(now.Add(-time.Hour))
	futureExpiry := metav1.NewTime(now.Add(time.Hour))

	newBranch := func(name, sha string, created time.Time) *opendepotv1alpha1.Version {
		return &opendepotv1alpha1.Version{
			ObjectMeta: metav1.ObjectMeta{
				Name:              name,
				Namespace:         "default",
				CreationTimestamp: metav1.NewTime(created),
				Labels:            map[string]string{"opendepot.defdev.io/skill": parent},
			},
			Spec: opendepotv1alpha1.VersionSpec{
				Type:         opendepotv1alpha1.OpenDepotSkill,
				SourceCommit: sha,
				AgentSourceRef: &opendepotv1alpha1.AgentSourceConfig{
					Name:      ptr.To(parent),
					RepoOwner: "example",
					Path:      "skills/" + parent,
					Ref:       ptr.To("main"),
				},
			},
		}
	}

	jevRejected := func(reasons ...string) *opendepotv1alpha1.JevAssessment {
		return &opendepotv1alpha1.JevAssessment{Blocked: true, BlockReasons: reasons, ContentDigest: digest}
	}

	tests := []struct {
		name       string
		syncStatus string
		assessment *opendepotv1alpha1.JevAssessment
		policies   []*opendepotv1alpha1.ScanPolicy
		newerSib   bool
		wantDone   bool
		wantPhase  string
	}{
		{
			name:       "scan violation stays terminal",
			syncStatus: "Scan policy violation: blocking: CRITICAL vulnerability",
			policies:   []*opendepotv1alpha1.ScanPolicy{jevExemptPolicy("release", 0, digest, nil)},
			wantDone:   true,
			wantPhase:  branchPhaseRejected,
		},
		{
			name:       "Jev block with no exemption stays terminal",
			syncStatus: "JevBlocked: " + criticalReason,
			assessment: jevRejected(criticalReason),
			wantDone:   true,
			wantPhase:  branchPhaseRejected,
		},
		{
			name:       "Jev block with an exemption for another digest stays terminal",
			syncStatus: "JevBlocked: " + criticalReason,
			assessment: jevRejected(criticalReason),
			policies:   []*opendepotv1alpha1.ScanPolicy{jevExemptPolicy("release", 0, jevExemptTestOther, nil)},
			wantDone:   true,
			wantPhase:  branchPhaseRejected,
		},
		{
			name:       "Critical with a matching exemption proceeds",
			syncStatus: "JevBlocked: " + criticalReason,
			assessment: jevRejected(criticalReason),
			policies:   []*opendepotv1alpha1.ScanPolicy{jevExemptPolicy("release", 0, digest, nil)},
			wantDone:   false,
		},
		{
			name:       "Critical with an expired exemption stays terminal",
			syncStatus: "JevBlocked: " + criticalReason,
			assessment: jevRejected(criticalReason),
			policies:   []*opendepotv1alpha1.ScanPolicy{jevExemptPolicy("release", 0, digest, &pastExpiry)},
			wantDone:   true,
			wantPhase:  branchPhaseRejected,
		},
		{
			name:       "Critical plus a threshold reason with a matching exemption proceeds",
			syncStatus: "JevBlocked: " + criticalReason + "; " + thresholdReason,
			assessment: jevRejected(criticalReason, thresholdReason),
			policies:   []*opendepotv1alpha1.ScanPolicy{jevExemptPolicy("release", 0, digest, &futureExpiry)},
			wantDone:   false,
		},
		{
			name:       "superseded still wins over a matching exemption",
			syncStatus: "JevBlocked: " + criticalReason,
			assessment: jevRejected(criticalReason),
			policies:   []*opendepotv1alpha1.ScanPolicy{jevExemptPolicy("release", 0, digest, nil)},
			newerSib:   true,
			wantDone:   true,
			wantPhase:  branchPhaseRejected,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			if err := opendepotv1alpha1.AddToScheme(scheme); err != nil {
				t.Fatalf("AddToScheme: %v", err)
			}

			version := newBranch("jev-branch-skill-a", testBranchSHAOne, now.Add(-2*time.Hour))
			version.Spec.Version = ""
			version.Status.BranchPhase = branchPhaseRejected
			version.Status.SyncStatus = tt.syncStatus
			version.Status.JevAssessment = tt.assessment

			objects := []client.Object{version}
			for _, policy := range tt.policies {
				objects = append(objects, policy)
			}

			if tt.newerSib {
				objects = append(objects, newBranch("jev-branch-skill-b", testBranchSHATwo, now))
			}

			reconciler := &VersionReconciler{
				Client:                fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).WithStatusSubresource(&opendepotv1alpha1.Version{}).Build(),
				Log:                   logr.Discard(),
				BranchSourcingEnabled: true,
			}

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: version.Name, Namespace: "default"}}
			done, _, err := reconciler.reconcileBranchPreflight(context.Background(), req, version)
			if err != nil {
				t.Fatalf("reconcileBranchPreflight: %v", err)
			}

			if done != tt.wantDone {
				t.Fatalf("done = %t; want %t", done, tt.wantDone)
			}

			if tt.wantPhase == "" {
				return
			}

			stored := &opendepotv1alpha1.Version{}
			if err := reconciler.Get(context.Background(), req.NamespacedName, stored); err != nil {
				t.Fatalf("Get: %v", err)
			}

			if stored.Status.BranchPhase != tt.wantPhase {
				t.Fatalf("BranchPhase = %q; want %q", stored.Status.BranchPhase, tt.wantPhase)
			}

			if tt.newerSib && !strings.HasPrefix(stored.Status.SyncStatus, "Superseded:") {
				t.Fatalf("SyncStatus = %q; want the superseded message", stored.Status.SyncStatus)
			}
		})
	}
}
