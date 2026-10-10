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
	"errors"
	"fmt"
	"strings"
	"time"

	goversion "github.com/hashicorp/go-version"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	opendepotGithub "github.com/tonedefdev/opendepot/pkg/github"
)

const (
	branchPhasePending  = "Pending"
	branchPhaseHeld     = "Held"
	branchPhaseAssigned = "Assigned"
	branchPhaseRejected = "Rejected"

	bumpOverrideAnnotation = "opendepot.defdev.io/bump-override"
	branchWaitRequeue      = 30 * time.Second
	branchCollisionRequeue = 5 * time.Second
)

// branchParentLabel returns the label that ties a Version to the Skill or Agent that owns it.
func branchParentLabel(versionType string) string {
	if versionType == opendepotv1alpha1.OpenDepotAgent {
		return "opendepot.defdev.io/agent"
	}

	return "opendepot.defdev.io/skill"
}

// branchParentName returns the name of the Skill or Agent that owns a branch Version.
func branchParentName(version *opendepotv1alpha1.Version) string {
	if name := version.Labels[branchParentLabel(version.Spec.Type)]; name != "" {
		return name
	}

	if sourceRef := version.Spec.AgentSourceRef; sourceRef != nil && sourceRef.Name != nil {
		return *sourceRef.Name
	}

	return ""
}

// branchSiblings lists the other branch Versions of the same type that belong to the same parent.
func (r *VersionReconciler) branchSiblings(ctx context.Context, version *opendepotv1alpha1.Version) ([]opendepotv1alpha1.Version, error) {
	parent := branchParentName(version)
	if parent == "" {
		return nil, nil
	}

	reader := r.APIReader
	if reader == nil {
		reader = r.Client
	}

	list := &opendepotv1alpha1.VersionList{}
	if err := reader.List(ctx, list,
		client.InNamespace(version.Namespace),
		client.MatchingLabels{branchParentLabel(version.Spec.Type): parent},
	); err != nil {
		return nil, fmt.Errorf("failed to list sibling versions: %w", err)
	}

	var siblings []opendepotv1alpha1.Version
	for _, item := range list.Items {
		if item.Name == version.Name || item.Spec.Type != version.Spec.Type || item.Spec.SourceCommit == "" {
			continue
		}

		siblings = append(siblings, item)
	}

	return siblings, nil
}

// versionCreatedBefore reports whether a was created before b. Equal timestamps are ordered by name.
func versionCreatedBefore(a, b *opendepotv1alpha1.Version) bool {
	if !a.CreationTimestamp.Equal(&b.CreationTimestamp) {
		return a.CreationTimestamp.Before(&b.CreationTimestamp)
	}

	return a.Name < b.Name
}

// branchAssignmentPending reports whether a branch Version still needs its semver assigned. A Version with a
// semver is never assessed again, so a forceSync re-run does not reclassify it.
func branchAssignmentPending(version *opendepotv1alpha1.Version) bool {
	return version.Spec.SourceCommit != "" && version.Spec.Version == ""
}

// setBranchStatus writes the branch phase and sync status of a Version. It does nothing when both are unchanged.
func (r *VersionReconciler) setBranchStatus(ctx context.Context, req ctrl.Request, phase, syncStatus string) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		currentVersion := &opendepotv1alpha1.Version{}
		if err := r.Get(ctx, req.NamespacedName, currentVersion); err != nil {
			return err
		}

		if currentVersion.Status.BranchPhase == phase && currentVersion.Status.SyncStatus == syncStatus {
			return nil
		}

		currentVersion.Status.Synced = false
		currentVersion.Status.SyncStatus = syncStatus
		currentVersion.Status.BranchPhase = phase
		return r.Status().Update(ctx, currentVersion, &client.SubResourceUpdateOptions{
			UpdateOptions: client.UpdateOptions{FieldManager: opendepotControllerName},
		})
	})
}

// reconcileBranchPreflight runs the checks that decide whether a branch Version may be downloaded. It returns
// done when the Version is terminal or waiting, in which case Reconcile returns the result without downloading.
func (r *VersionReconciler) reconcileBranchPreflight(ctx context.Context, req ctrl.Request, version *opendepotv1alpha1.Version) (bool, ctrl.Result, error) {
	if version.Status.BranchPhase == branchPhaseRejected && version.Spec.Version == "" {
		return true, ctrl.Result{}, nil
	}

	if !r.BranchSourcingEnabled {
		return true, ctrl.Result{}, r.setBranchStatus(ctx, req, branchPhaseRejected,
			"branch sourcing is disabled on the version controller")
	}

	if err := opendepotGithub.ValidateCommitSHA(version.Spec.SourceCommit); err != nil {
		return true, ctrl.Result{}, r.setBranchStatus(ctx, req, branchPhaseRejected,
			fmt.Sprintf("Invalid sourceCommit: %v", err))
	}

	if version.Spec.Version != "" {
		return false, ctrl.Result{}, nil
	}

	siblings, err := r.branchSiblings(ctx, version)
	if err != nil {
		return true, ctrl.Result{}, err
	}

	for i := range siblings {
		sibling := &siblings[i]
		if !versionCreatedBefore(sibling, version) || sibling.Spec.Version != "" || sibling.Status.BranchPhase == branchPhaseRejected {
			continue
		}

		waitStatus := fmt.Sprintf("Waiting for %s", sibling.Name)
		if err := r.setBranchStatus(ctx, req, branchPhasePending, waitStatus); err != nil {
			return true, ctrl.Result{}, err
		}

		r.Log.V(5).Info("branch version waits for an older sibling", "version", version.Name, "sibling", sibling.Name)
		return true, ctrl.Result{RequeueAfter: branchWaitRequeue}, nil
	}

	for i := range siblings {
		sibling := &siblings[i]
		if !versionCreatedBefore(version, sibling) {
			continue
		}

		supersededStatus := fmt.Sprintf("Superseded: a newer commit exists as Version '%s'", sibling.Name)
		return true, ctrl.Result{}, r.setBranchStatus(ctx, req, branchPhaseRejected, supersededStatus)
	}

	if branchAssignmentPending(version) && r.branchJevRefMissing(version) {
		requeueAfter, err := r.holdBranchBump(ctx, req, version, &opendepotv1alpha1.BumpAssessment{Commit: version.Spec.SourceCommit}, errors.New(jevRefRequiredError))
		return true, ctrl.Result{RequeueAfter: requeueAfter}, err
	}

	return false, ctrl.Result{}, nil
}

// highestAssignedSibling returns the sibling with the highest semantic version that has been assigned, or nil.
func highestAssignedSibling(siblings []opendepotv1alpha1.Version) (*opendepotv1alpha1.Version, error) {
	var highest *opendepotv1alpha1.Version
	var highestSemver *goversion.Version
	for i := range siblings {
		sibling := &siblings[i]
		if sibling.Spec.Version == "" {
			continue
		}

		semver, err := goversion.NewVersion(sibling.Spec.Version)
		if err != nil {
			return nil, fmt.Errorf("sibling version '%s' has an invalid semver %q: %w", sibling.Name, sibling.Spec.Version, err)
		}

		if highestSemver == nil || semver.GreaterThan(highestSemver) {
			highest = sibling
			highestSemver = semver
		}
	}

	return highest, nil
}

// nextBranchVersion applies a bump category to a semantic version.
func nextBranchVersion(previous, category string) (string, error) {
	semver, err := goversion.NewVersion(previous)
	if err != nil {
		return "", fmt.Errorf("invalid previous semver %q: %w", previous, err)
	}

	segments := semver.Segments()
	if len(segments) < 3 {
		return "", fmt.Errorf("semver %q must have major, minor, and patch segments", previous)
	}

	major, minor, patch := segments[0], segments[1], segments[2]
	switch category {
	case "Major":
		return fmt.Sprintf("%d.0.0", major+1), nil
	case "Minor":
		return fmt.Sprintf("%d.%d.0", major, minor+1), nil
	case "Patch":
		return fmt.Sprintf("%d.%d.%d", major, minor, patch+1), nil
	default:
		return "", fmt.Errorf("unknown bump category %q", category)
	}
}

// manualBumpCategory returns the category set by the bump-override annotation, or an empty string when unset.
func manualBumpCategory(version *opendepotv1alpha1.Version) (string, error) {
	value, ok := version.Annotations[bumpOverrideAnnotation]
	if !ok {
		return "", nil
	}

	switch strings.ToLower(value) {
	case "major":
		return "Major", nil
	case "minor":
		return "Minor", nil
	case "patch":
		return "Patch", nil
	default:
		return "", fmt.Errorf("invalid %s annotation %q: must be major, minor, or patch", bumpOverrideAnnotation, value)
	}
}

// assignBranchVersion picks the semantic version of a branch Version whose checks have passed, writes spec.version
// once, and returns the bump assessment. A requeue duration is returned when the chosen version is already taken.
func (r *VersionReconciler) assignBranchVersion(
	ctx context.Context,
	req ctrl.Request,
	version *opendepotv1alpha1.Version,
	tree *agentContent,
	archive []byte,
) (*opendepotv1alpha1.BumpAssessment, time.Duration, error) {
	siblings, err := r.branchSiblings(ctx, version)
	if err != nil {
		return nil, 0, err
	}

	previous, err := highestAssignedSibling(siblings)
	if err != nil {
		return nil, 0, err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	assessment := &opendepotv1alpha1.BumpAssessment{
		Commit:      version.Spec.SourceCommit,
		EvaluatedAt: now,
	}

	var next string
	if previous == nil {
		if _, overrideErr := manualBumpCategory(version); overrideErr != nil {
			return nil, 0, overrideErr
		}

		next = "1.0.0"
		assessment.Source = "Initial"
		assessment.Reason = "First commit of this source; the first version is always 1.0.0"
	} else {
		category, overrideErr := manualBumpCategory(version)
		if overrideErr != nil {
			return nil, 0, overrideErr
		}

		assessment.PreviousCommit = previous.Spec.SourceCommit
		assessment.PreviousVersion = previous.Spec.Version
		switch {
		case r.branchJevRefMissing(version):
			requeueAfter, holdErr := r.holdBranchBump(ctx, req, version, assessment, errors.New(jevRefRequiredError))
			return nil, requeueAfter, holdErr
		case category != "":
			assessment.Source = "Manual"
			assessment.Reason = fmt.Sprintf("Operator set %s to %s", bumpOverrideAnnotation, strings.ToLower(category))
		case r.branchJevEnabled(version):
			classified, classifyErr := r.classifyBranchBump(ctx, version, previous, tree, archive, assessment)
			if classifyErr != nil {
				requeueAfter, holdErr := r.holdBranchBump(ctx, req, version, assessment, classifyErr)
				return nil, requeueAfter, holdErr
			}

			category = classified
		default:
			category = "Patch"
			assessment.Source = "Default"
			assessment.Reason = "No Jev classifier is configured; PATCH applied"
		}

		assessment.Category = category
		next, err = nextBranchVersion(previous.Spec.Version, category)
		if err != nil {
			return nil, 0, err
		}
	}

	for i := range siblings {
		if siblings[i].Spec.Version == next {
			r.Log.V(5).Info("semver already taken by a sibling; retrying", "version", version.Name, "semver", next)
			return nil, branchCollisionRequeue, nil
		}
	}

	if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		currentVersion := &opendepotv1alpha1.Version{}
		if err := r.Get(ctx, req.NamespacedName, currentVersion); err != nil {
			return err
		}

		if currentVersion.Spec.Version != "" {
			return fmt.Errorf("version '%s' already has semver %q", currentVersion.Name, currentVersion.Spec.Version)
		}

		currentVersion.Spec.Version = next
		return r.Update(ctx, currentVersion, &client.UpdateOptions{FieldManager: opendepotControllerName})
	}); err != nil {
		return nil, 0, fmt.Errorf("failed to assign semver %q: %w", next, err)
	}

	version.Spec.Version = next
	r.Log.Info("assigned semver to branch version", "version", version.Name, "semver", next, "source", assessment.Source)
	return assessment, 0, nil
}
