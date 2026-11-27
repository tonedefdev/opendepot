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
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"golang.org/x/mod/semver"
	"k8s.io/apimachinery/pkg/api/equality"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
)

const (
	ociArtifactsControllerName = "opendepot-ociartifacts-controller"
	ociArtifactLabelKey        = "opendepot.defdev.io/ociartifact"
	ociNamespaceLabelKey       = "opendepot.defdev.io/namespace"
	// ociVersionHashLength is the number of hex characters of the SHA-256 digest appended to a Version name.
	ociVersionHashLength = 8
	// ociVersionNameMaxLength is the maximum length of a Version name, which must be a DNS-1123 label.
	ociVersionNameMaxLength = 63
	// ociDuplicateRecheckInterval is how often an OCIArtifact that claims a duplicated repository name is reconciled
	// again, so that it resumes syncing once the duplicate is removed.
	ociDuplicateRecheckInterval = time.Minute
)

// errOCIVersionCollision reports that a Version with the same name exists but was not created by the OCIArtifact.
var errOCIVersionCollision = errors.New("version name collides with a Version not owned by this OCIArtifact")

// ociNameReplacer maps the characters that are valid in a repository path or a tag but not in a Version name.
var ociNameReplacer = strings.NewReplacer(".", "-", "_", "-", "/", "-")

// OCIArtifactReconciler reconciles an OCIArtifact object
type OCIArtifactReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
	// AllowedRegistries lists the upstream registry hosts that an OCIArtifact may mirror from. An empty list denies
	// every mirror.
	AllowedRegistries []string
	// OCIBuildsEnabled turns on in-cluster builds of OCIArtifacts that set build. When it is false, build sources are
	// not synced.
	OCIBuildsEnabled bool
	// OCIBuildsAllowedRepoOwners lists the GitHub owners whose repositories may be built. An empty list denies every
	// build.
	OCIBuildsAllowedRepoOwners []string
}

// +kubebuilder:rbac:groups=opendepot.defdev.io,resources=ociartifacts,verbs=get;list;watch;update
// +kubebuilder:rbac:groups=opendepot.defdev.io,resources=ociartifacts/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=opendepot.defdev.io,resources=versions,verbs=get;list;watch;create;update;patch;delete

// Reconcile creates a Version for each OCIArtifact version and removes the Versions that are no longer declared.
// Image artifacts get one Version per tag because the OCI index keeps every selected platform.
func (r *OCIArtifactReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	ociArtifact := &opendepotv1alpha1.OCIArtifact{}
	err := r.Get(ctx, req.NamespacedName, ociArtifact)
	if err != nil {
		if k8serr.IsNotFound(err) {
			r.Log.V(5).Info("OCIArtifact resource not found. Ignoring since object must be deleted", "ociArtifact", req.Name)
			return ctrl.Result{}, nil
		}

		r.Log.Error(err, "Failed to get OCIArtifact", "ociArtifact", req.Name)
		return ctrl.Result{}, err
	}

	config := ociArtifact.Spec.OCIArtifactConfig
	if config.Mirror != nil && !slices.Contains(r.AllowedRegistries, config.Mirror.Registry) {
		status := ociArtifact.Status
		status.Synced = false
		status.SyncStatus = fmt.Sprintf("Registry %s is not on the allow-list", config.Mirror.Registry)
		if err = r.updateOCIArtifactStatus(ctx, req.NamespacedName, status); err != nil {
			r.Log.Error(err, "Failed to update OCIArtifact status", "ociArtifact", ociArtifact.Name)
			return ctrl.Result{}, err
		}

		r.Log.Info("Registry is not on the allow-list", "ociArtifact", ociArtifact.Name, "registry", config.Mirror.Registry)
		return ctrl.Result{}, nil
	}

	if config.Build != nil {
		var buildMsg string
		switch {
		case !r.OCIBuildsEnabled:
			buildMsg = "In-cluster builds are disabled"
		case !slices.Contains(r.OCIBuildsAllowedRepoOwners, config.Build.RepoOwner):
			buildMsg = fmt.Sprintf("Repository owner %s is not on the build allow-list", config.Build.RepoOwner)
		}

		if buildMsg != "" {
			status := ociArtifact.Status
			status.Synced = false
			status.SyncStatus = buildMsg
			if err = r.updateOCIArtifactStatus(ctx, req.NamespacedName, status); err != nil {
				r.Log.Error(err, "Failed to update OCIArtifact status", "ociArtifact", ociArtifact.Name)
				return ctrl.Result{}, err
			}

			r.Log.Info(buildMsg, "ociArtifact", ociArtifact.Name, "repoOwner", config.Build.RepoOwner)
			return ctrl.Result{}, nil
		}
	}

	configName := ociConfigName(ociArtifact)
	claims, err := countOCIRepositoryClaims(ctx, r.Client, ociArtifact.GetNamespace(), configName)
	if err != nil {
		r.Log.Error(err, "Failed to list OCIArtifacts", "ociArtifact", ociArtifact.Name)
		return ctrl.Result{}, err
	}

	// Two OCIArtifacts that claim the same repository name would write the same Versions, so neither may sync.
	if claims > 1 {
		status := ociArtifact.Status
		status.Synced = false
		status.SyncStatus = fmt.Sprintf("duplicate OCIArtifact repository name %s: %d OCIArtifacts in namespace %s claim it",
			configName, claims, ociArtifact.GetNamespace())
		if err = r.updateOCIArtifactStatus(ctx, req.NamespacedName, status); err != nil {
			r.Log.Error(err, "Failed to update OCIArtifact status", "ociArtifact", ociArtifact.Name)
			return ctrl.Result{}, err
		}

		r.Log.Info("Duplicate OCIArtifact repository name", "ociArtifact", ociArtifact.Name, "repository", configName)
		return ctrl.Result{RequeueAfter: ociDuplicateRecheckInterval}, nil
	}

	discovered := config.TagPattern != nil
	limit := config.VersionHistoryLimit
	original := slices.Clone(ociArtifact.Spec.Versions)
	keep := trimOCIVersions(ociArtifact.Spec.Versions, limit, discovered)
	if !slices.Equal(original, keep) {
		if err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
			if err = r.Get(ctx, req.NamespacedName, ociArtifact); err != nil {
				return err
			}

			// Recompute from the latest spec so entries added while reconciling are not overwritten.
			keep = trimOCIVersions(ociArtifact.Spec.Versions, limit, discovered)
			ociArtifact.Spec.Versions = slices.Clone(keep)

			return r.Update(ctx, ociArtifact, &client.UpdateOptions{FieldManager: ociArtifactsControllerName})
		}); err != nil {
			r.Log.Error(err, "Failed to update OCIArtifact versions", "ociArtifact", ociArtifact.Name)
			return ctrl.Result{}, err
		}
	}

	forceSync := ociArtifact.Spec.ForceSync
	versionObjects, collisions, err := reconcileOCIVersions(ctx, r.Client, r.Scheme, r.Log, ociArtifact, keep, forceSync)
	if err != nil {
		r.Log.Error(err, "Failed to reconcile OCIArtifact versions", "ociArtifact", ociArtifact.Name)
		return ctrl.Result{}, err
	}

	// If ForceSync is true set it to false now that the Versions have been asked to synchronize
	if forceSync {
		if err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
			if err = r.Get(ctx, req.NamespacedName, ociArtifact); err != nil {
				return err
			}

			ociArtifact.Spec.ForceSync = false
			return r.Update(ctx, ociArtifact, &client.UpdateOptions{FieldManager: ociArtifactsControllerName})
		}); err != nil {
			r.Log.Error(err, "Failed to update OCIArtifact", "ociArtifact", ociArtifact.Name)
			return ctrl.Result{}, err
		}
	}

	refs := make(map[string]*opendepotv1alpha1.OCIArtifactVersion, len(keep))
	syncedCount := 0
	for _, entry := range keep {
		versionObject, ok := versionObjects[ociVersionName(configName, entry.Version)]
		if !ok {
			continue
		}

		ref := &opendepotv1alpha1.OCIArtifactVersion{
			Name:    versionObject.Name,
			Version: entry.Version,
			Synced:  versionObject.Status.Synced,
		}

		if versionObject.Status.OCIManifest != nil {
			ref.Digest = versionObject.Status.OCIManifest.Digest
		}

		refs[entry.Version] = ref
		if versionObject.Status.Synced {
			syncedCount++
		}
	}

	status := opendepotv1alpha1.OCIArtifactStatus{
		LatestVersion: latestOCIVersion(keep, discovered),
		Synced:        syncedCount == len(keep),
		SyncStatus:    ociSyncStatus(syncedCount, len(keep)),
		VersionRefs:   refs,
	}

	// Colliding names are kept out of removal so that a Version this OCIArtifact does not own is never deleted.
	keepNames := ociVersionNameSet(versionObjects)
	for _, name := range collisions {
		keepNames[name] = struct{}{}
	}

	if len(collisions) > 0 {
		status.Synced = false
		status.SyncStatus = fmt.Sprintf("Version name collision with a Version not owned by this OCIArtifact: %s",
			strings.Join(collisions, ", "))
	}

	if err = r.updateOCIArtifactStatus(ctx, req.NamespacedName, status); err != nil {
		r.Log.Error(err, "Failed to update OCIArtifact status", "ociArtifact", ociArtifact.Name)
		return ctrl.Result{}, err
	}

	if err = reconcileOCIVersionRemovals(ctx, r.Client, r.Log, ociArtifact, keepNames); err != nil {
		return ctrl.Result{}, err
	}

	r.Log.V(5).Info("Successfully reconciled OCIArtifact", "ociArtifact", ociArtifact.Name)
	return ctrl.Result{}, nil
}

// updateOCIArtifactStatus writes the desired status only when it differs from the stored status, so that an unchanged
// reconcile does not produce an update event.
func (r *OCIArtifactReconciler) updateOCIArtifactStatus(
	ctx context.Context,
	key client.ObjectKey,
	desired opendepotv1alpha1.OCIArtifactStatus,
) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		ociArtifact := &opendepotv1alpha1.OCIArtifact{}
		if err := r.Get(ctx, key, ociArtifact); err != nil {
			return err
		}

		if equality.Semantic.DeepEqual(ociArtifact.Status, desired) {
			return nil
		}

		ociArtifact.Status = desired
		return r.Status().Update(ctx, ociArtifact, &client.SubResourceUpdateOptions{
			UpdateOptions: client.UpdateOptions{FieldManager: ociArtifactsControllerName},
		})
	})
}

// SetupWithManager sets up the controller with the Manager.
func (r *OCIArtifactReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&opendepotv1alpha1.OCIArtifact{}).
		Owns(&opendepotv1alpha1.Version{}).
		Named(ociArtifactsControllerName).
		Complete(r)
}

// reconcileOCIVersions creates a Version for each entry that does not already exist and brings existing Versions in
// line with the config and ForceSync. The returned map is keyed by the Version resource name. The returned collisions
// are the Version names of entries whose name is held by a Version that the OCIArtifact does not own. Those entries
// are left untouched.
func reconcileOCIVersions(
	ctx context.Context,
	c client.Client,
	scheme *runtime.Scheme,
	log logr.Logger,
	owner *opendepotv1alpha1.OCIArtifact,
	entries []opendepotv1alpha1.OCIArtifactVersion,
	forceSync bool,
) (map[string]*opendepotv1alpha1.Version, []string, error) {
	configName := ociConfigName(owner)
	config := owner.Spec.OCIArtifactConfig

	versionObjects := make(map[string]*opendepotv1alpha1.Version, len(entries))
	var collisions []string
	for _, entry := range entries {
		versionName := ociVersionName(configName, entry.Version)
		existing := &opendepotv1alpha1.Version{}
		err := c.Get(ctx, client.ObjectKey{Name: versionName, Namespace: owner.GetNamespace()}, existing)
		if err == nil {
			if err = syncOCIVersion(ctx, c, log, owner, existing, &config, forceSync); err != nil {
				if errors.Is(err, errOCIVersionCollision) {
					log.Info("Skipping version that collides with an existing Version",
						"name", owner.GetName(), "version", entry.Version, "versionName", versionName)
					collisions = append(collisions, versionName)
					continue
				}

				return nil, nil, fmt.Errorf("sync oci version %s: %w", versionName, err)
			}

			versionObjects[versionName] = existing
			continue
		}

		if !k8serr.IsNotFound(err) {
			return nil, nil, fmt.Errorf("get oci version %s: %w", versionName, err)
		}

		ociVersion := &opendepotv1alpha1.Version{
			ObjectMeta: metav1.ObjectMeta{
				Name:      versionName,
				Namespace: owner.GetNamespace(),
				Labels: map[string]string{
					ociArtifactLabelKey:  owner.GetName(),
					ociNamespaceLabelKey: owner.GetNamespace(),
				},
			},
			Spec: opendepotv1alpha1.VersionSpec{
				Type:                 opendepotv1alpha1.OpenDepotOCIArtifact,
				Version:              entry.Version,
				OCIArtifactConfigRef: config.DeepCopy(),
			},
		}

		// Set the ownerRef for the Version, ensuring that the Version
		// is garbage collected when its OCIArtifact is deleted.
		if err = controllerutil.SetControllerReference(owner, ociVersion, scheme); err != nil {
			return nil, nil, err
		}

		if err = c.Create(ctx, ociVersion, &client.CreateOptions{
			FieldManager: ociArtifactsControllerName,
		}); err != nil {
			return nil, nil, fmt.Errorf("create oci version %s: %w", versionName, err)
		}

		versionObjects[versionName] = ociVersion
		log.V(5).Info("Successfully created version",
			"kind", opendepotv1alpha1.OpenDepotOCIArtifact,
			"version", entry.Version,
			"name", owner.GetName(),
		)
	}

	return versionObjects, collisions, nil
}

// countOCIRepositoryClaims returns how many OCIArtifacts in the namespace claim the repository name.
func countOCIRepositoryClaims(ctx context.Context, c client.Client, namespace string, configName string) (int, error) {
	ociArtifactList := opendepotv1alpha1.OCIArtifactList{}
	if err := c.List(ctx, &ociArtifactList, client.InNamespace(namespace)); err != nil {
		return 0, err
	}

	claims := 0
	for i := range ociArtifactList.Items {
		if ociConfigName(&ociArtifactList.Items[i]) == configName {
			claims++
		}
	}

	return claims, nil
}

// ociVersionOwnedBy reports whether a Version was created by the OCIArtifact owner. Only such a Version may be patched
// or removed by the owner.
func ociVersionOwnedBy(version *opendepotv1alpha1.Version, owner *opendepotv1alpha1.OCIArtifact) bool {
	return version.Spec.Type == opendepotv1alpha1.OpenDepotOCIArtifact &&
		version.Labels[ociArtifactLabelKey] == owner.GetName() &&
		version.Labels[ociNamespaceLabelKey] == owner.GetNamespace() &&
		metav1.IsControlledBy(version, owner)
}

// ociVersionOutdated reports whether an existing Version differs from its OCIArtifact. A Version needs a
// config refresh when its ociArtifactConfigRef differs, and it needs ForceSync when the OCIArtifact requests one.
func ociVersionOutdated(version *opendepotv1alpha1.Version, config *opendepotv1alpha1.OCIArtifactConfig, forceSync bool) bool {
	return !equality.Semantic.DeepEqual(version.Spec.OCIArtifactConfigRef, config) || (forceSync && !version.Spec.ForceSync)
}

// syncOCIVersion patches an existing Version so that its ociArtifactConfigRef matches the OCIArtifact and so that it
// is asked to synchronize when ForceSync is set. Nothing is written when the Version is already in line. A Version
// that the owner did not create is never patched and errOCIVersionCollision is returned instead.
func syncOCIVersion(
	ctx context.Context,
	c client.Client,
	log logr.Logger,
	owner *opendepotv1alpha1.OCIArtifact,
	version *opendepotv1alpha1.Version,
	config *opendepotv1alpha1.OCIArtifactConfig,
	forceSync bool,
) error {
	if !ociVersionOwnedBy(version, owner) {
		return fmt.Errorf("%w: %s", errOCIVersionCollision, version.Name)
	}

	if !ociVersionOutdated(version, config, forceSync) {
		return nil
	}

	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		if err := c.Get(ctx, client.ObjectKeyFromObject(version), version); err != nil {
			return err
		}

		if !ociVersionOwnedBy(version, owner) {
			return fmt.Errorf("%w: %s", errOCIVersionCollision, version.Name)
		}

		if !ociVersionOutdated(version, config, forceSync) {
			return nil
		}

		patch := client.MergeFrom(version.DeepCopy())
		version.Spec.OCIArtifactConfigRef = config.DeepCopy()
		if forceSync {
			version.Spec.ForceSync = true
		}

		if err := c.Patch(ctx, version, patch, client.FieldOwner(ociArtifactsControllerName)); err != nil {
			return err
		}

		log.V(5).Info("Successfully synced config to version",
			"kind", opendepotv1alpha1.OpenDepotOCIArtifact,
			"name", version.Name,
		)

		return nil
	})
}

// reconcileOCIVersionRemovals deletes the Versions owned by the OCIArtifact whose name is not in keep.
func reconcileOCIVersionRemovals(
	ctx context.Context,
	c client.Client,
	log logr.Logger,
	owner *opendepotv1alpha1.OCIArtifact,
	keep map[string]struct{},
) error {
	versionList := opendepotv1alpha1.VersionList{}
	if err := c.List(ctx, &versionList,
		client.InNamespace(owner.GetNamespace()),
		client.MatchingLabels{
			ociArtifactLabelKey:  owner.GetName(),
			ociNamespaceLabelKey: owner.GetNamespace(),
		},
	); err != nil {
		return err
	}

	for _, version := range versionList.Items {
		if _, ok := keep[version.Name]; ok {
			continue
		}

		log.Info("Deleting version", "kind", opendepotv1alpha1.OpenDepotOCIArtifact, "name", owner.GetName(), "version", version.Name)
		if err := c.Delete(ctx, &version); err != nil {
			return err
		}
	}

	return nil
}

// ociVersionNameSet returns the Version names in versionObjects as a set.
func ociVersionNameSet(versionObjects map[string]*opendepotv1alpha1.Version) map[string]struct{} {
	names := make(map[string]struct{}, len(versionObjects))
	for name := range versionObjects {
		names[name] = struct{}{}
	}

	return names
}

// ociConfigName returns the repository name that the Versions of an OCIArtifact are created under.
func ociConfigName(ociArtifact *opendepotv1alpha1.OCIArtifact) string {
	if ociArtifact.Spec.OCIArtifactConfig.Name != nil {
		return *ociArtifact.Spec.OCIArtifactConfig.Name
	}

	return ociArtifact.Name
}

// ociVersionName returns the Version resource name for a tag, for example oci-podinfo-6-5-0-1a2b3c4d. The repository
// name can contain '/' and the tag can contain '.' and '_', so both are mapped to '-' to produce a Kubernetes-safe
// name. Because that mapping is lossy, a SHA-256 suffix of the unnormalized repository name and tag keeps names
// distinct. The base is truncated so the name stays within the DNS-1123 label length.
func ociVersionName(configName string, tag string) string {
	base := strings.Trim(fmt.Sprintf("oci-%s-%s",
		ociNameReplacer.Replace(strings.ToLower(configName)),
		ociNameReplacer.Replace(strings.ToLower(tag)),
	), "-")

	maxBaseLength := ociVersionNameMaxLength - ociVersionHashLength - 1
	if len(base) > maxBaseLength {
		base = strings.TrimRight(base[:maxBaseLength], "-")
	}

	digest := sha256.Sum256([]byte(configName + "\x00" + tag))
	return base + "-" + hex.EncodeToString(digest[:ociVersionHashLength/2])
}

// ociSemver returns the tag as a semantic version. Tags that are not semantic versions sort below the valid ones.
func ociSemver(tag string) string {
	return "v" + strings.TrimPrefix(tag, "v")
}

// sortOCIVersions returns the entries in ascending semantic version order. Entries that compare equal keep their
// original order.
func sortOCIVersions(entries []opendepotv1alpha1.OCIArtifactVersion) []opendepotv1alpha1.OCIArtifactVersion {
	sorted := slices.Clone(entries)
	slices.SortStableFunc(sorted, func(a, b opendepotv1alpha1.OCIArtifactVersion) int {
		return semver.Compare(ociSemver(a.Version), ociSemver(b.Version))
	})

	return sorted
}

// latestOCIVersion returns the latest tag or nil when there are no entries. Semantic versions are ordered by semver
// unless discovered is true. Then the last entry is the most recently discovered tag.
func latestOCIVersion(entries []opendepotv1alpha1.OCIArtifactVersion, discovered bool) *string {
	if len(entries) == 0 {
		return nil
	}

	latest := entries[len(entries)-1].Version
	if !discovered {
		sorted := sortOCIVersions(entries)
		latest = sorted[len(sorted)-1].Version
	}

	return &latest
}

// trimOCIVersions returns the entries to keep, in their original order. A nil or non-positive limit keeps every
// entry. When discovered is true the newest entries are the last ones, since discovery appends them. Otherwise the
// highest semantic versions are kept.
func trimOCIVersions(entries []opendepotv1alpha1.OCIArtifactVersion, limit *int, discovered bool) []opendepotv1alpha1.OCIArtifactVersion {
	if limit == nil || *limit <= 0 || len(entries) <= *limit {
		return entries
	}

	if discovered {
		return entries[len(entries)-*limit:]
	}

	sorted := sortOCIVersions(entries)
	newest := sorted[len(sorted)-*limit:]

	kept := make([]opendepotv1alpha1.OCIArtifactVersion, 0, *limit)
	for _, entry := range entries {
		if slices.ContainsFunc(newest, func(version opendepotv1alpha1.OCIArtifactVersion) bool {
			return version.Version == entry.Version
		}) {
			kept = append(kept, entry)
		}
	}

	return kept
}

// ociSyncStatus describes how many of the kept versions have synced.
func ociSyncStatus(synced int, total int) string {
	switch {
	case total == 0:
		return "No versions declared"
	case synced == total:
		return "Successfully synced OCIArtifact"
	default:
		return fmt.Sprintf("Syncing %d of %d versions", synced, total)
	}
}
