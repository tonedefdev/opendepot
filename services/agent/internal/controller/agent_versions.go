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
	"fmt"
	"slices"
	"strings"

	"github.com/go-logr/logr"
	"github.com/google/uuid"
	"golang.org/x/mod/semver"
	"k8s.io/apimachinery/pkg/api/equality"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
)

// agentKind describes how a Skill or an Agent maps onto Version resources.
type agentKind struct {
	controllerName string
	versionType    string
	labelKey       string
	namePrefix     string
}

var (
	skillKind = agentKind{
		controllerName: skillsControllerName,
		versionType:    opendepotv1alpha1.OpenDepotSkill,
		labelKey:       "opendepot.defdev.io/skill",
		namePrefix:     "skill",
	}

	agentKindDef = agentKind{
		controllerName: agentsControllerName,
		versionType:    opendepotv1alpha1.OpenDepotAgent,
		labelKey:       "opendepot.defdev.io/agent",
		namePrefix:     "agent",
	}
)

const (
	branchStatePending  = "Pending"
	branchStateAssigned = "Assigned"
	branchStateRejected = "Rejected"
	branchPhaseRejected = "Rejected"
	commitLabelKey      = "opendepot.defdev.io/commit"

	defaultBranchVersionHistoryLimit = 10
)

// reconcileAgentVersions creates a Version for each entry that does not already exist. Tag entries are named
// after their version and branch entries after their commit. Entries that branch mode does not manage are skipped.
// Existing Versions are never updated because their agentSourceRef is immutable and their spec carries user state
// such as Yanked. The returned map is keyed by the Version resource name.
func reconcileAgentVersions(
	ctx context.Context,
	c client.Client,
	scheme *runtime.Scheme,
	log logr.Logger,
	owner metav1.Object,
	kind agentKind,
	sourceConfig opendepotv1alpha1.AgentSourceConfig,
	entries []opendepotv1alpha1.AgentVersion,
) (map[string]*opendepotv1alpha1.Version, error) {
	configName := agentConfigName(owner, sourceConfig)
	branch := sourceConfig.Ref != nil

	versionObjects := make(map[string]*opendepotv1alpha1.Version, len(entries))
	for _, entry := range entries {
		versionName := agentEntryVersionName(kind, configName, entry, branch)
		if versionName == "" {
			continue
		}

		object := client.ObjectKey{
			Name:      versionName,
			Namespace: owner.GetNamespace(),
		}

		existing := &opendepotv1alpha1.Version{}
		err := c.Get(ctx, object, existing)
		if err == nil {
			versionObjects[versionName] = existing
			continue
		}

		if !k8serr.IsNotFound(err) {
			return nil, fmt.Errorf("get %s version %s: %w", kind.namePrefix, versionName, err)
		}

		fileName, err := generateAgentFileName()
		if err != nil {
			return nil, fmt.Errorf("generate filename for %s version %s: %w", kind.namePrefix, versionName, err)
		}

		sourceRef := sourceConfig
		sourceRef.Name = &configName
		agentVersion := &opendepotv1alpha1.Version{
			ObjectMeta: metav1.ObjectMeta{
				Name:      versionName,
				Namespace: owner.GetNamespace(),
				Labels: map[string]string{
					kind.labelKey:                   owner.GetName(),
					"opendepot.defdev.io/namespace": owner.GetNamespace(),
				},
			},
			Spec: opendepotv1alpha1.VersionSpec{
				FileName:       fileName,
				Type:           kind.versionType,
				Version:        entry.Version,
				AgentSourceRef: &sourceRef,
			},
		}

		// A branch Version starts without a semantic version. The Version controller assigns it after
		// the checks pass, and the Skill or Agent copies it back to the entry.
		if branch {
			agentVersion.Labels[commitLabelKey] = entry.Commit
			agentVersion.Spec.SourceCommit = entry.Commit
			agentVersion.Spec.Version = ""
		}

		// Set the ownerRef for the Version, ensuring that the Version
		// is garbage collected when its Skill or Agent is deleted.
		if err = controllerutil.SetControllerReference(owner, agentVersion, scheme); err != nil {
			return nil, err
		}

		if err = c.Create(ctx, agentVersion, &client.CreateOptions{
			FieldManager: kind.controllerName,
		}); err != nil {
			return nil, fmt.Errorf("create %s version %s: %w", kind.namePrefix, versionName, err)
		}

		versionObjects[versionName] = agentVersion
		log.V(5).Info("Successfully created version",
			"kind", kind.versionType,
			"version", entry.Version,
			"commit", entry.Commit,
			"name", owner.GetName(),
		)
	}

	return versionObjects, nil
}

// reconcileAgentVersionRemovals deletes Versions owned by the Skill or Agent whose name is not in keep.
func reconcileAgentVersionRemovals(ctx context.Context, c client.Client, log logr.Logger, owner metav1.Object, kind agentKind, keep map[string]struct{}) error {
	versionList := opendepotv1alpha1.VersionList{}
	labelsMap := map[string]string{
		kind.labelKey:                   owner.GetName(),
		"opendepot.defdev.io/namespace": owner.GetNamespace(),
	}

	labelSelector, err := labels.Parse(labels.FormatLabels(labelsMap))
	if err != nil {
		return err
	}

	if err = c.List(ctx, &versionList, &client.ListOptions{
		LabelSelector: labelSelector,
	}); err != nil {
		return err
	}

	for _, version := range versionList.Items {
		if _, ok := keep[version.Name]; ok {
			continue
		}

		log.Info("Deleting version", "kind", kind.versionType, "name", owner.GetName(), "version", version.Name)
		if err = c.Delete(ctx, &version); err != nil {
			return err
		}
	}

	return nil
}

// agentVersionNameSet returns the Version names in versionObjects as a set.
func agentVersionNameSet(versionObjects map[string]*opendepotv1alpha1.Version) map[string]struct{} {
	names := make(map[string]struct{}, len(versionObjects))
	for name := range versionObjects {
		names[name] = struct{}{}
	}

	return names
}

// agentEntriesEqual reports whether two sets of entries are identical in order and content.
func agentEntriesEqual(a, b []opendepotv1alpha1.AgentVersion) bool {
	return slices.EqualFunc(a, b, func(x, y opendepotv1alpha1.AgentVersion) bool {
		return equality.Semantic.DeepEqual(x, y)
	})
}

// agentConfigName returns the name that the Versions of a Skill or Agent are created under.
func agentConfigName(owner metav1.Object, sourceConfig opendepotv1alpha1.AgentSourceConfig) string {
	if sourceConfig.Name != nil {
		return *sourceConfig.Name
	}

	return owner.GetName()
}

// agentEntryVersionName returns the Version resource name for an entry. A tag entry is named after its version and
// a branch entry after its commit. An empty name means that branch mode does not manage the entry.
func agentEntryVersionName(kind agentKind, configName string, entry opendepotv1alpha1.AgentVersion, branch bool) string {
	if !branch {
		return agentVersionName(kind, configName, entry.Version)
	}

	if entry.Commit == "" {
		return ""
	}

	return agentVersionName(kind, configName, entry.Commit)
}

// agentVersionHistoryLimit returns the version history limit for a source. A branch source keeps
// defaultBranchVersionHistoryLimit versions when the limit is unset.
func agentVersionHistoryLimit(sourceConfig opendepotv1alpha1.AgentSourceConfig) *int {
	if sourceConfig.VersionHistoryLimit == nil && sourceConfig.Ref != nil {
		limit := defaultBranchVersionHistoryLimit
		return &limit
	}

	return sourceConfig.VersionHistoryLimit
}

// trimAgentEntries returns the entries to keep, in their original order. Tag mode keeps the newest versions allowed
// by the limit. Branch mode keeps the newest assigned versions allowed by the limit, every entry that has no version
// yet, the newest entry when it is rejected, and entries that branch mode does not manage.
func trimAgentEntries(entries []opendepotv1alpha1.AgentVersion, limit *int, branch bool) []opendepotv1alpha1.AgentVersion {
	if !branch {
		versions := make([]string, 0, len(entries))
		for _, entry := range entries {
			versions = append(versions, entry.Version)
		}

		keep := trimAgentVersions(versions, limit)
		if len(keep) == len(entries) {
			return entries
		}

		kept := make([]opendepotv1alpha1.AgentVersion, 0, len(keep))
		for _, entry := range entries {
			if slices.Contains(keep, entry.Version) {
				kept = append(kept, entry)
			}
		}

		return kept
	}

	var assigned []string
	newest := -1
	for i, entry := range entries {
		if entry.Commit == "" {
			continue
		}

		if entry.Version != "" {
			assigned = append(assigned, entry.Version)
		}

		if newest < 0 || notBefore(entry.DiscoveredAt, entries[newest].DiscoveredAt) {
			newest = i
		}
	}

	keep := trimAgentVersions(assigned, limit)
	kept := make([]opendepotv1alpha1.AgentVersion, 0, len(entries))
	for i, entry := range entries {
		switch {
		case entry.Commit == "":
			// Not managed by branch mode, so the entry is left in place.
		case entry.Version != "":
			if !slices.Contains(keep, entry.Version) {
				continue
			}
		case entry.State == branchStateRejected && i != newest:
			continue
		}

		kept = append(kept, entry)
	}

	return kept
}

// notBefore reports whether a was discovered at or after b. A missing time is treated as the oldest.
func notBefore(a, b *metav1.Time) bool {
	if b == nil {
		return true
	}

	if a == nil {
		return false
	}

	return !a.Time.Before(b.Time)
}

// getBranchVersions returns the existing Version for each branch entry, keyed by the Version name.
func getBranchVersions(
	ctx context.Context,
	c client.Client,
	owner metav1.Object,
	kind agentKind,
	configName string,
	entries []opendepotv1alpha1.AgentVersion,
) (map[string]*opendepotv1alpha1.Version, error) {
	versions := make(map[string]*opendepotv1alpha1.Version)
	for _, entry := range entries {
		versionName := agentEntryVersionName(kind, configName, entry, true)
		if versionName == "" {
			continue
		}

		version := &opendepotv1alpha1.Version{}
		err := c.Get(ctx, client.ObjectKey{Name: versionName, Namespace: owner.GetNamespace()}, version)
		if k8serr.IsNotFound(err) {
			continue
		}

		if err != nil {
			return nil, fmt.Errorf("get %s version %s: %w", kind.namePrefix, versionName, err)
		}

		versions[versionName] = version
	}

	return versions, nil
}

// syncBranchEntries copies the semantic version and the rejection that the Version controller recorded on each
// branch Version back to its entry.
func syncBranchEntries(kind agentKind, configName string, entries []opendepotv1alpha1.AgentVersion, versions map[string]*opendepotv1alpha1.Version) {
	for i := range entries {
		version, ok := versions[agentEntryVersionName(kind, configName, entries[i], true)]
		if !ok {
			continue
		}

		if version.Spec.Version != "" {
			entries[i].Version = version.Spec.Version
			entries[i].State = branchStateAssigned
			continue
		}

		if version.Status.BranchPhase == branchPhaseRejected {
			entries[i].State = branchStateRejected
		}
	}
}

// branchVersionChanged reports whether a branch Version update needs its Skill or Agent to copy back an assigned
// semantic version or a rejection. Tag Versions never trigger a reconcile.
func branchVersionChanged(e event.UpdateEvent) bool {
	oldVersion, oldOK := e.ObjectOld.(*opendepotv1alpha1.Version)
	newVersion, newOK := e.ObjectNew.(*opendepotv1alpha1.Version)
	if !oldOK || !newOK || newVersion.Spec.SourceCommit == "" {
		return false
	}

	return oldVersion.Spec.Version != newVersion.Spec.Version || oldVersion.Status.BranchPhase != newVersion.Status.BranchPhase
}

// agentVersionName returns the Version resource name for a Skill or Agent, for example
// skill-my-skill-1-0-0 or agent-my-agent-1-0-0.
func agentVersionName(kind agentKind, configName string, version string) string {
	return fmt.Sprintf("%s-%s-%s", kind.namePrefix, configName, sanitizeAgentVersion(version))
}

// sanitizeAgentVersion removes the leading 'v' and replaces '.' and '_' with '-' to produce
// a Kubernetes-safe resource name component.
func sanitizeAgentVersion(version string) string {
	version = strings.TrimPrefix(version, "v")
	version = strings.ToLower(version)
	version = strings.ReplaceAll(version, ".", "-")
	version = strings.ReplaceAll(version, "_", "-")
	return version
}

// canonicalAgentVersion returns the canonical semantic version with a leading 'v'.
func canonicalAgentVersion(version string) string {
	if !strings.HasPrefix(version, "v") {
		version = "v" + version
	}
	return semver.Canonical(version)
}

// sortAgentVersions returns the versions sorted in ascending semantic version order.
func sortAgentVersions(versions []string) []string {
	sorted := slices.Clone(versions)
	slices.SortFunc(sorted, func(a, b string) int {
		return semver.Compare(canonicalAgentVersion(a), canonicalAgentVersion(b))
	})
	return sorted
}

// latestAgentVersion returns the highest canonical semantic version or nil when there are no versions.
func latestAgentVersion(versions []string) *string {
	if len(versions) == 0 {
		return nil
	}

	sorted := sortAgentVersions(versions)
	latest := canonicalAgentVersion(sorted[len(sorted)-1])
	return &latest
}

// trimAgentVersions returns the most recent versions allowed by the version history limit.
// A nil or non-positive limit keeps every version.
func trimAgentVersions(versions []string, limit *int) []string {
	if limit == nil || *limit <= 0 || len(versions) <= *limit {
		return versions
	}

	sorted := sortAgentVersions(versions)
	return sorted[len(sorted)-*limit:]
}

// generateAgentFileName returns a randomly generated UUID7 filename for an archive.
func generateAgentFileName() (*string, error) {
	fileUUID, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}

	fileName := fmt.Sprintf("%s.tar.gz", fileUUID)
	return &fileName, nil
}
