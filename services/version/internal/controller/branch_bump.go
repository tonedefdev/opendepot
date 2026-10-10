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
	"errors"
	"fmt"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pmezard/go-difflib/difflib"
	"k8s.io/client-go/util/retry"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	"github.com/tonedefdev/opendepot/pkg/jev"
)

const (
	// bumpBaseBackoff is the delay after the first failed classification.
	bumpBaseBackoff = time.Minute
	// bumpMaxBackoff caps the delay between classification attempts.
	bumpMaxBackoff = 15 * time.Minute
)

// branchJevEnabled reports whether the semver bump of a branch Version is classified by Jev. Without Jev the bump is PATCH.
func (r *VersionReconciler) branchJevEnabled(version *opendepotv1alpha1.Version) bool {
	return r.JevEnabled && version.Spec.AgentSourceRef != nil && version.Spec.AgentSourceRef.JevSecretRef != nil
}

// branchJevRefMissing reports whether Jev is enabled on the controller but the branch source omits jevSecretRef.
// Such a bump is held instead of falling back to PATCH, so removing the ref cannot skip the Jev assessment.
func (r *VersionReconciler) branchJevRefMissing(version *opendepotv1alpha1.Version) bool {
	return r.JevEnabled && (version.Spec.AgentSourceRef == nil || version.Spec.AgentSourceRef.JevSecretRef == nil)
}

// bumpBackoff returns the delay before the next classification attempt: 1, 2, 4, and 8 minutes, then 15 minutes.
func bumpBackoff(attempts int) time.Duration {
	attempts = max(attempts, 1)
	if attempts > 4 {
		return bumpMaxBackoff
	}

	return bumpBaseBackoff << (attempts - 1)
}

// bumpCategory applies the policy thresholds to the Jev probabilities. MAJOR wins over MINOR, and PATCH is the default.
func bumpCategory(major, minor float64, policy *opendepotv1alpha1.BranchPolicy) string {
	switch {
	case major >= policy.EffectiveMajorThreshold():
		return "Major"
	case major+minor >= policy.EffectiveMinorThreshold():
		return "Minor"
	default:
		return "Patch"
	}
}

// verifyPreviousChecksum checks that the previous tree, downloaded again at its commit, has the checksum recorded
// on its Version. A mismatch means the commit no longer produces the content that was assessed.
func verifyPreviousChecksum(recorded, downloaded *string) error {
	if recorded == nil || downloaded == nil || *recorded != *downloaded {
		return errors.New("previous tree checksum does not match the checksum recorded on the previous version")
	}

	return nil
}

// fetchAgentArchiveAt downloads the Skill or Agent tree of version at the given commit.
func (r *VersionReconciler) fetchAgentArchiveAt(
	ctx context.Context,
	version *opendepotv1alpha1.Version,
	commit string,
) ([]byte, *string, error) {
	atCommit := version.DeepCopy()
	atCommit.Spec.SourceCommit = commit
	packed, checksum, _, _, err := r.fetchAgentArchive(ctx, atCommit)
	return packed, checksum, err
}

// scanSecretFiles runs a secret-only Trivy scan on dir and returns the base names of the files it flagged.
func (r *VersionReconciler) scanSecretFiles(ctx context.Context, dir string) (map[string]struct{}, error) {
	args := []string{"fs", "--format", "json", "--scanners", "secret"}
	if r.ScanOffline {
		args = append(args, "--offline-scan", "--skip-db-update")
	}

	if r.TrivyCacheDir != "" {
		args = append(args, "--cache-dir", r.TrivyCacheDir)
	}

	args = append(args, "--quiet", dir)

	select {
	case r.scanSem <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	output, err := runTrivy(ctx, args...)
	<-r.scanSem
	if err != nil {
		return nil, fmt.Errorf("previous tree secret scan failed: %w", err)
	}

	if len(output) == 0 {
		return nil, errors.New("previous tree secret scan failed: trivy produced no output")
	}

	_, flagged, err := parseTrivySecrets(output)
	if err != nil {
		return nil, err
	}

	names := make(map[string]struct{}, len(flagged))
	for target := range flagged {
		names[filepath.Base(target)] = struct{}{}
	}

	return names, nil
}

// classifyBranchBump decides the semver bump of a branch Version by comparing it with its previous Version. An
// entry file flagged in either tree gives MAJOR without a Jev call. Otherwise the change is classified by Jev. The
// assessment is filled only when the classification succeeds.
func (r *VersionReconciler) classifyBranchBump(
	ctx context.Context,
	version *opendepotv1alpha1.Version,
	previous *opendepotv1alpha1.Version,
	tree *agentContent,
	archive []byte,
	assessment *opendepotv1alpha1.BumpAssessment,
) (string, error) {
	if tree == nil {
		return "", errors.New("agent content is unavailable for bump classification")
	}

	previousArchive, previousChecksum, err := r.fetchAgentArchiveAt(ctx, version, previous.Spec.SourceCommit)
	if err != nil {
		return "", fmt.Errorf("download previous tree at %s: %w", previous.Spec.SourceCommit, err)
	}

	if err := verifyPreviousChecksum(previous.Status.Checksum, previousChecksum); err != nil {
		return "", err
	}

	return r.classifyPreparedBump(ctx, version, previousArchive, tree, archive, assessment)
}

// classifyPreparedBump classifies the bump once the previous tree is downloaded and verified. It scans the previous
// tree for secrets, withholds MAJOR when an entry file is flagged, and otherwise asks Jev for the change.
func (r *VersionReconciler) classifyPreparedBump(
	ctx context.Context,
	version *opendepotv1alpha1.Version,
	previousArchive []byte,
	tree *agentContent,
	archive []byte,
	assessment *opendepotv1alpha1.BumpAssessment,
) (string, error) {
	previousDir, cleanupPrevious, err := extractArchiveToTempDir(previousArchive, "opendepot-bump-previous-*")
	if err != nil {
		return "", err
	}
	defer cleanupPrevious()

	previousFlagged, err := r.scanSecretFiles(ctx, previousDir)
	if err != nil {
		return "", err
	}

	previousTree, err := loadAgentContent(previousDir, version)
	if err != nil {
		return "", fmt.Errorf("parse previous tree: %w", err)
	}

	_, previousEntryFlagged := previousFlagged[tree.entryName]
	if tree.entryFlagged || previousEntryFlagged {
		assessment.Category = "Major"
		assessment.Source = "Withheld"
		assessment.Reason = "A secret was flagged in the entry file of either version; MAJOR applied without a Jev classification"
		return "Major", nil
	}

	nextDir, cleanupNext, err := extractArchiveToTempDir(archive, "opendepot-bump-next-*")
	if err != nil {
		return "", err
	}
	defer cleanupNext()

	withheld := make(map[string]struct{}, len(tree.flaggedFiles)+len(previousFlagged))
	maps.Copy(withheld, tree.flaggedFiles)
	maps.Copy(withheld, previousFlagged)

	state, err := buildBumpState(previousTree, tree, previousDir, nextDir, withheld)
	if err != nil {
		return "", err
	}

	token, err := r.readJevToken(ctx, version.Namespace, version.Spec.AgentSourceRef.JevSecretRef)
	if err != nil {
		return "", err
	}

	var opts []jev.Option
	if r.JevEndpoint != "" {
		opts = append(opts, jev.WithEndpoint(r.JevEndpoint))
	}

	result, err := jev.NewClient(opts...).ClassifyBump(ctx, token, strings.ToLower(version.Spec.Type), state)
	if err != nil {
		return "", fmt.Errorf("jev bump classification: %w", err)
	}

	policy := version.Spec.AgentSourceRef.BranchPolicy
	category := bumpCategory(result.MajorProbability, result.MinorProbability, policy)
	assessment.Category = category
	assessment.Source = "Jev"
	assessment.Model = result.Model
	assessment.MajorProbability = ptr.To(result.MajorProbability)
	assessment.MinorProbability = ptr.To(result.MinorProbability)
	assessment.PatchProbability = ptr.To(result.PatchProbability)
	assessment.Confidence = ptr.To(result.CategoryConfidence)
	assessment.NeedsReview = result.CategoryConfidence < policy.EffectiveMinConfidence()
	assessment.EvaluatedAt = time.Now().UTC().Format(time.RFC3339)
	assessment.Reason = fmt.Sprintf("Jev classified the change as %s (major %.2f, minor %.2f, patch %.2f)",
		strings.ToLower(category), result.MajorProbability, result.MinorProbability, result.PatchProbability)
	return category, nil
}

// buildBumpState builds the Jev input for a bump. The facts header comes first so that it survives truncation.
// It is followed by a unified diff of every file that is not flagged, and the content of a flagged file is never included.
func buildBumpState(
	previous, next *agentContent,
	previousDir, nextDir string,
	withheld map[string]struct{},
) (string, error) {
	var b strings.Builder
	addedTools := missingItems(next.definition.Tools, previous.definition.Tools)
	removedTools := missingItems(previous.definition.Tools, next.definition.Tools)
	fmt.Fprintf(&b, "Tools added (%d): %s\n", len(addedTools), listOrNone(addedTools))
	fmt.Fprintf(&b, "Tools removed (%d): %s\n", len(removedTools), listOrNone(removedTools))

	if previous.definition.Model != next.definition.Model {
		fmt.Fprintf(&b, "Model changed from %q to %q\n", previous.definition.Model, next.definition.Model)
	} else {
		b.WriteString("Model unchanged\n")
	}

	if previous.definition.Description != next.definition.Description {
		fmt.Fprintf(&b, "Description changed from %q to %q\n", previous.definition.Description, next.definition.Description)
	} else {
		b.WriteString("Description unchanged\n")
	}

	previousFiles := make(map[string]struct{}, len(previous.files))
	for _, name := range previous.files {
		previousFiles[name] = struct{}{}
	}

	nextFiles := make(map[string]struct{}, len(next.files))
	for _, name := range next.files {
		nextFiles[name] = struct{}{}
	}

	names := slices.Concat(previous.files, next.files)
	slices.Sort(names)
	names = slices.Compact(names)

	var added, removed, modified []string
	var diffs strings.Builder
	for _, name := range names {
		_, inPrevious := previousFiles[name]
		_, inNext := nextFiles[name]

		var before, after []byte
		var err error
		if inPrevious {
			before, err = os.ReadFile(filepath.Join(previousDir, filepath.FromSlash(name)))
			if err != nil {
				return "", fmt.Errorf("read previous file %s: %w", name, err)
			}
		}

		if inNext {
			after, err = os.ReadFile(filepath.Join(nextDir, filepath.FromSlash(name)))
			if err != nil {
				return "", fmt.Errorf("read file %s: %w", name, err)
			}
		}

		switch {
		case !inPrevious:
			added = append(added, name)
		case !inNext:
			removed = append(removed, name)
		case bytes.Equal(before, after):
			continue
		default:
			modified = append(modified, name)
		}

		if _, flagged := withheld[path.Base(name)]; flagged {
			fmt.Fprintf(&diffs, "Content of %s withheld: flagged for a secret\n\n", name)
			continue
		}

		diff, err := unifiedFileDiff(name, before, after)
		if err != nil {
			return "", err
		}

		diffs.WriteString(diff)
	}

	fmt.Fprintf(&b, "Files added (%d): %s\n", len(added), listOrNone(added))
	fmt.Fprintf(&b, "Files removed (%d): %s\n", len(removed), listOrNone(removed))
	fmt.Fprintf(&b, "Files modified (%d): %s\n\n", len(modified), listOrNone(modified))
	b.WriteString("Diff:\n")
	b.WriteString(diffs.String())
	return truncateUTF8(b.String(), jev.MaxStateBytes), nil
}

// unifiedFileDiff returns the unified diff of one file. A binary file is described without its content.
func unifiedFileDiff(name string, before, after []byte) (string, error) {
	if bytes.IndexByte(before, 0) >= 0 || bytes.IndexByte(after, 0) >= 0 {
		return fmt.Sprintf("Binary file %s differs; content omitted\n\n", name), nil
	}

	diff, err := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		A:        difflib.SplitLines(string(before)),
		B:        difflib.SplitLines(string(after)),
		FromFile: "previous/" + name,
		ToFile:   "new/" + name,
		Context:  3,
	})
	if err != nil {
		return "", fmt.Errorf("diff file %s: %w", name, err)
	}

	return diff + "\n", nil
}

// missingItems returns the sorted, unique items of list that are not in other.
func missingItems(list, other []string) []string {
	present := make(map[string]struct{}, len(other))
	for _, item := range other {
		present[item] = struct{}{}
	}

	var missing []string
	for _, item := range list {
		if _, ok := present[item]; !ok {
			missing = append(missing, item)
		}
	}

	slices.Sort(missing)
	return slices.Compact(missing)
}

// listOrNone joins the items with commas, or returns none when there are no items.
func listOrNone(items []string) string {
	if len(items) == 0 {
		return "none"
	}

	return strings.Join(items, ", ")
}

// truncateUTF8 cuts s to at most limit bytes without splitting a UTF-8 character.
func truncateUTF8(s string, limit int) string {
	if len(s) <= limit {
		return s
	}

	cut := limit
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}

	return s[:cut]
}

// holdBranchBump records a failed classification on the Version and returns the delay before the next attempt.
// The bump is never guessed. Until Jev succeeds, or an operator sets the bump override or forceSync, the Version stays Held.
func (r *VersionReconciler) holdBranchBump(
	ctx context.Context,
	req ctrl.Request,
	version *opendepotv1alpha1.Version,
	assessment *opendepotv1alpha1.BumpAssessment,
	cause error,
) (time.Duration, error) {
	attempts := 1
	if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		currentVersion := &opendepotv1alpha1.Version{}
		if err := r.Get(ctx, req.NamespacedName, currentVersion); err != nil {
			return err
		}

		attempts = 1
		if prior := currentVersion.Status.BumpAssessment; prior != nil && prior.Commit == version.Spec.SourceCommit {
			attempts = prior.Attempts + 1
		}

		held := *assessment
		held.Attempts = attempts
		held.Error = cause.Error()
		held.EvaluatedAt = time.Now().UTC().Format(time.RFC3339)
		currentVersion.Status.Synced = false
		currentVersion.Status.SyncStatus = fmt.Sprintf("Failed to classify bump: %v", cause)
		currentVersion.Status.BranchPhase = branchPhaseHeld
		currentVersion.Status.BumpAssessment = &held
		return r.Status().Update(ctx, currentVersion, &client.SubResourceUpdateOptions{
			UpdateOptions: client.UpdateOptions{FieldManager: opendepotControllerName},
		})
	}); err != nil {
		return 0, fmt.Errorf("failed to persist held bump for '%s': %w", version.Name, err)
	}

	r.Log.Error(cause, "branch version held until bump is classified", "version", version.Name, "attempts", attempts)
	return bumpBackoff(attempts), nil
}
