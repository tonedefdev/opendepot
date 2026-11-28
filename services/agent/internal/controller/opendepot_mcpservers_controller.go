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
	mcpServersControllerName = "opendepot-mcpservers-controller"
	mcpServerLabelKey        = "opendepot.defdev.io/mcpserver"
	mcpNamespaceLabelKey     = "opendepot.defdev.io/namespace"
	// mcpSourceTypeImage is the MCPServer source type that runs a container image mirrored by an OCIArtifact.
	mcpSourceTypeImage = "Image"
	// mcpImageArtifactType is the OCIArtifact artifactType that an Image source must reference.
	mcpImageArtifactType = "Image"
	// mcpVersionHashLength is the number of hex characters of the SHA-256 digest appended to a Version name.
	mcpVersionHashLength = 8
	// mcpVersionNameMaxLength is the maximum length of a Version name, which must be a DNS-1123 label.
	mcpVersionNameMaxLength = 63
	// mcpRecheckInterval is how often an MCPServer with an invalid Image source is reconciled again, so that it
	// resumes syncing once the referenced OCIArtifact is created or corrected.
	mcpRecheckInterval = time.Minute
)

// errMCPVersionCollision reports that a Version with the same name exists but was not created by the MCPServer.
var errMCPVersionCollision = errors.New("version name collides with a Version not owned by this MCPServer")

// MCPServerReconciler reconciles an MCPServer object
type MCPServerReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=opendepot.defdev.io,resources=mcpservers,verbs=get;list;watch;update
// +kubebuilder:rbac:groups=opendepot.defdev.io,resources=mcpservers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=opendepot.defdev.io,resources=ociartifacts,verbs=get;list;watch
// +kubebuilder:rbac:groups=opendepot.defdev.io,resources=versions,verbs=get;list;watch;create;update;patch;delete

// Reconcile creates a Version for each MCPServer version and removes the Versions that are no longer declared.
// An Image source must reference an OCIArtifact with artifactType Image in the same namespace, otherwise no Versions
// are created.
func (r *MCPServerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	mcpServer := &opendepotv1alpha1.MCPServer{}
	err := r.Get(ctx, req.NamespacedName, mcpServer)
	if err != nil {
		if k8serr.IsNotFound(err) {
			r.Log.V(5).Info("MCPServer resource not found. Ignoring since object must be deleted", "mcpServer", req.Name)
			return ctrl.Result{}, nil
		}

		r.Log.Error(err, "Failed to get MCPServer", "mcpServer", req.Name)
		return ctrl.Result{}, err
	}

	if mcpServer.Spec.MCPServerSourceConfig.Source.Type == mcpSourceTypeImage {
		msg, err := r.validateMCPImageSource(ctx, mcpServer)
		if err != nil {
			r.Log.Error(err, "Failed to validate MCPServer image source", "mcpServer", mcpServer.Name)
			return ctrl.Result{}, err
		}

		if msg != "" {
			status := mcpServer.Status
			status.Synced = false
			status.SyncStatus = msg
			if err = r.updateMCPServerStatus(ctx, req.NamespacedName, status); err != nil {
				r.Log.Error(err, "Failed to update MCPServer status", "mcpServer", mcpServer.Name)
				return ctrl.Result{}, err
			}

			r.Log.Info(msg, "mcpServer", mcpServer.Name)
			return ctrl.Result{RequeueAfter: mcpRecheckInterval}, nil
		}
	}

	config := mcpSourceRef(mcpServer)
	configName := mcpConfigName(mcpServer)
	limit := mcpServer.Spec.MCPServerSourceConfig.VersionHistoryLimit
	original := slices.Clone(mcpServer.Spec.Versions)
	keep := trimMCPVersions(mcpServer.Spec.Versions, limit)
	if !slices.Equal(original, keep) {
		if err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
			if err = r.Get(ctx, req.NamespacedName, mcpServer); err != nil {
				return err
			}

			// Recompute from the latest spec so entries added while reconciling are not overwritten.
			keep = trimMCPVersions(mcpServer.Spec.Versions, limit)
			mcpServer.Spec.Versions = append(make([]opendepotv1alpha1.MCPServerVersion, 0, len(keep)), keep...)

			return r.Update(ctx, mcpServer, &client.UpdateOptions{FieldManager: mcpServersControllerName})
		}); err != nil {
			r.Log.Error(err, "Failed to update MCPServer versions", "mcpServer", mcpServer.Name)
			return ctrl.Result{}, err
		}
	}

	forceSync := mcpServer.Spec.ForceSync
	versionObjects, collisions, err := reconcileMCPVersions(ctx, r.Client, r.Scheme, r.Log, mcpServer, config, keep, forceSync)
	if err != nil {
		r.Log.Error(err, "Failed to reconcile MCPServer versions", "mcpServer", mcpServer.Name)
		return ctrl.Result{}, err
	}

	// If ForceSync is true set it to false now that the Versions have been asked to synchronize
	if forceSync {
		if err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
			if err = r.Get(ctx, req.NamespacedName, mcpServer); err != nil {
				return err
			}

			mcpServer.Spec.ForceSync = false
			return r.Update(ctx, mcpServer, &client.UpdateOptions{FieldManager: mcpServersControllerName})
		}); err != nil {
			r.Log.Error(err, "Failed to update MCPServer", "mcpServer", mcpServer.Name)
			return ctrl.Result{}, err
		}
	}

	refs := make(map[string]*opendepotv1alpha1.MCPServerVersion, len(keep))
	assigned := make([]string, 0, len(keep))
	syncedCount := 0
	for _, entry := range keep {
		versionObject, ok := versionObjects[mcpVersionName(configName, entry.Version)]
		if !ok {
			continue
		}

		assigned = append(assigned, entry.Version)
		refs[entry.Version] = &opendepotv1alpha1.MCPServerVersion{
			Name:     versionObject.Name,
			FileName: versionObject.Spec.FileName,
			Synced:   versionObject.Status.Synced,
			Version:  entry.Version,
		}

		if versionObject.Status.Synced {
			syncedCount++
		}
	}

	status := opendepotv1alpha1.MCPServerStatus{
		LatestVersion: latestAgentVersion(assigned),
		Synced:        syncedCount == len(keep),
		SyncStatus:    mcpSyncStatus(syncedCount, len(keep)),
		VersionRefs:   refs,
	}

	// Colliding names are kept out of removal so that a Version this MCPServer does not own is never deleted.
	keepNames := mcpVersionNameSet(versionObjects)
	for _, name := range collisions {
		keepNames[name] = struct{}{}
	}

	if len(collisions) > 0 {
		status.Synced = false
		status.SyncStatus = fmt.Sprintf("Version name collision with a Version not owned by this MCPServer: %s",
			strings.Join(collisions, ", "))
	}

	if err = r.updateMCPServerStatus(ctx, req.NamespacedName, status); err != nil {
		r.Log.Error(err, "Failed to update MCPServer status", "mcpServer", mcpServer.Name)
		return ctrl.Result{}, err
	}

	if err = reconcileMCPVersionRemovals(ctx, r.Client, r.Log, mcpServer, keepNames); err != nil {
		return ctrl.Result{}, err
	}

	r.Log.V(5).Info("Successfully reconciled MCPServer", "mcpServer", mcpServer.Name)
	return ctrl.Result{}, nil
}

// validateMCPImageSource returns a message describing why the Image source of the MCPServer is invalid, or an empty
// string when the source is valid. The referenced OCIArtifact must exist in the MCPServer namespace and must have
// artifactType Image.
func (r *MCPServerReconciler) validateMCPImageSource(ctx context.Context, mcpServer *opendepotv1alpha1.MCPServer) (string, error) {
	image := mcpServer.Spec.MCPServerSourceConfig.Source.Image
	if image == nil {
		return "Image source requires an ociArtifactRef", nil
	}

	name := image.OCIArtifactRef.Name
	ociArtifact := &opendepotv1alpha1.OCIArtifact{}
	err := r.Get(ctx, client.ObjectKey{Name: name, Namespace: mcpServer.GetNamespace()}, ociArtifact)
	if err != nil {
		if k8serr.IsNotFound(err) {
			return fmt.Sprintf("referenced OCIArtifact %s not found", name), nil
		}

		return "", fmt.Errorf("get oci artifact %s: %w", name, err)
	}

	if ociArtifact.Spec.OCIArtifactConfig.ArtifactType != mcpImageArtifactType {
		return fmt.Sprintf("referenced OCIArtifact %s must have artifactType %s", name, mcpImageArtifactType), nil
	}

	return "", nil
}

// updateMCPServerStatus writes the desired status only when it differs from the stored status, so that an unchanged
// reconcile does not produce an update event.
func (r *MCPServerReconciler) updateMCPServerStatus(
	ctx context.Context,
	key client.ObjectKey,
	desired opendepotv1alpha1.MCPServerStatus,
) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		mcpServer := &opendepotv1alpha1.MCPServer{}
		if err := r.Get(ctx, key, mcpServer); err != nil {
			return err
		}

		if equality.Semantic.DeepEqual(mcpServer.Status, desired) {
			return nil
		}

		mcpServer.Status = desired
		return r.Status().Update(ctx, mcpServer, &client.SubResourceUpdateOptions{
			UpdateOptions: client.UpdateOptions{FieldManager: mcpServersControllerName},
		})
	})
}

// SetupWithManager sets up the controller with the Manager.
func (r *MCPServerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&opendepotv1alpha1.MCPServer{}).
		Owns(&opendepotv1alpha1.Version{}).
		Named(mcpServersControllerName).
		Complete(r)
}

// reconcileMCPVersions creates a Version for each entry that does not already exist and brings existing Versions in
// line with the config and ForceSync. The returned map is keyed by the Version resource name. The returned collisions
// are the Version names of entries whose name is held by a Version that the MCPServer does not own. Those entries are
// left untouched.
func reconcileMCPVersions(
	ctx context.Context,
	c client.Client,
	scheme *runtime.Scheme,
	log logr.Logger,
	owner *opendepotv1alpha1.MCPServer,
	config *opendepotv1alpha1.MCPServerSourceConfig,
	entries []opendepotv1alpha1.MCPServerVersion,
	forceSync bool,
) (map[string]*opendepotv1alpha1.Version, []string, error) {
	configName := mcpConfigName(owner)

	versionObjects := make(map[string]*opendepotv1alpha1.Version, len(entries))
	var collisions []string
	for _, entry := range entries {
		versionName := mcpVersionName(configName, entry.Version)
		existing := &opendepotv1alpha1.Version{}
		err := c.Get(ctx, client.ObjectKey{Name: versionName, Namespace: owner.GetNamespace()}, existing)
		if err == nil {
			if err = syncMCPVersion(ctx, c, log, owner, existing, config, forceSync); err != nil {
				if errors.Is(err, errMCPVersionCollision) {
					log.Info("Skipping version that collides with an existing Version",
						"name", owner.GetName(), "version", entry.Version, "versionName", versionName)
					collisions = append(collisions, versionName)
					continue
				}

				return nil, nil, fmt.Errorf("sync mcp version %s: %w", versionName, err)
			}

			versionObjects[versionName] = existing
			continue
		}

		if !k8serr.IsNotFound(err) {
			return nil, nil, fmt.Errorf("get mcp version %s: %w", versionName, err)
		}

		mcpVersion := &opendepotv1alpha1.Version{
			ObjectMeta: metav1.ObjectMeta{
				Name:      versionName,
				Namespace: owner.GetNamespace(),
				Labels: map[string]string{
					mcpServerLabelKey:    owner.GetName(),
					mcpNamespaceLabelKey: owner.GetNamespace(),
				},
			},
			Spec: opendepotv1alpha1.VersionSpec{
				Type:               opendepotv1alpha1.OpenDepotMCPServer,
				Version:            entry.Version,
				MCPServerSourceRef: config.DeepCopy(),
			},
		}

		// Set the ownerRef for the Version, ensuring that the Version
		// is garbage collected when its MCPServer is deleted.
		if err = controllerutil.SetControllerReference(owner, mcpVersion, scheme); err != nil {
			return nil, nil, err
		}

		if err = c.Create(ctx, mcpVersion, &client.CreateOptions{
			FieldManager: mcpServersControllerName,
		}); err != nil {
			return nil, nil, fmt.Errorf("create mcp version %s: %w", versionName, err)
		}

		versionObjects[versionName] = mcpVersion
		log.V(5).Info("Successfully created version",
			"kind", opendepotv1alpha1.OpenDepotMCPServer,
			"version", entry.Version,
			"name", owner.GetName(),
		)
	}

	return versionObjects, collisions, nil
}

// mcpVersionOwnedBy reports whether a Version was created by the MCPServer owner. Only such a Version may be patched
// or removed by the owner.
func mcpVersionOwnedBy(version *opendepotv1alpha1.Version, owner *opendepotv1alpha1.MCPServer) bool {
	return version.Spec.Type == opendepotv1alpha1.OpenDepotMCPServer &&
		version.Labels[mcpServerLabelKey] == owner.GetName() &&
		version.Labels[mcpNamespaceLabelKey] == owner.GetNamespace() &&
		metav1.IsControlledBy(version, owner)
}

// mcpVersionOutdated reports whether an existing Version differs from its MCPServer. A Version needs a config refresh
// when its mcpServerSourceRef differs, and it needs ForceSync when the MCPServer requests one.
func mcpVersionOutdated(version *opendepotv1alpha1.Version, config *opendepotv1alpha1.MCPServerSourceConfig, forceSync bool) bool {
	return !equality.Semantic.DeepEqual(version.Spec.MCPServerSourceRef, config) || (forceSync && !version.Spec.ForceSync)
}

// syncMCPVersion patches an existing Version so that its mcpServerSourceRef matches the MCPServer and so that it is
// asked to synchronize when ForceSync is set. Nothing is written when the Version is already in line. A Version that
// the owner did not create is never patched and errMCPVersionCollision is returned instead.
func syncMCPVersion(
	ctx context.Context,
	c client.Client,
	log logr.Logger,
	owner *opendepotv1alpha1.MCPServer,
	version *opendepotv1alpha1.Version,
	config *opendepotv1alpha1.MCPServerSourceConfig,
	forceSync bool,
) error {
	if !mcpVersionOwnedBy(version, owner) {
		return fmt.Errorf("%w: %s", errMCPVersionCollision, version.Name)
	}

	if !mcpVersionOutdated(version, config, forceSync) {
		return nil
	}

	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		if err := c.Get(ctx, client.ObjectKeyFromObject(version), version); err != nil {
			return err
		}

		if !mcpVersionOwnedBy(version, owner) {
			return fmt.Errorf("%w: %s", errMCPVersionCollision, version.Name)
		}

		if !mcpVersionOutdated(version, config, forceSync) {
			return nil
		}

		patch := client.MergeFrom(version.DeepCopy())
		version.Spec.MCPServerSourceRef = config.DeepCopy()
		if forceSync {
			version.Spec.ForceSync = true
		}

		if err := c.Patch(ctx, version, patch, client.FieldOwner(mcpServersControllerName)); err != nil {
			return err
		}

		log.V(5).Info("Successfully synced config to version",
			"kind", opendepotv1alpha1.OpenDepotMCPServer,
			"name", version.Name,
		)

		return nil
	})
}

// reconcileMCPVersionRemovals deletes the Versions owned by the MCPServer whose name is not in keep. A labeled Version
// that the owner did not create is left alone.
func reconcileMCPVersionRemovals(
	ctx context.Context,
	c client.Client,
	log logr.Logger,
	owner *opendepotv1alpha1.MCPServer,
	keep map[string]struct{},
) error {
	versionList := opendepotv1alpha1.VersionList{}
	if err := c.List(ctx, &versionList,
		client.InNamespace(owner.GetNamespace()),
		client.MatchingLabels{
			mcpServerLabelKey:    owner.GetName(),
			mcpNamespaceLabelKey: owner.GetNamespace(),
		},
	); err != nil {
		return err
	}

	for _, version := range versionList.Items {
		if _, ok := keep[version.Name]; ok {
			continue
		}

		if !mcpVersionOwnedBy(&version, owner) {
			log.Info("Skipping removal of version not owned by MCPServer", "name", owner.GetName(), "version", version.Name)
			continue
		}

		log.Info("Deleting version", "kind", opendepotv1alpha1.OpenDepotMCPServer, "name", owner.GetName(), "version", version.Name)
		if err := c.Delete(ctx, &version); err != nil {
			return err
		}
	}

	return nil
}

// mcpVersionNameSet returns the Version names in versionObjects as a set.
func mcpVersionNameSet(versionObjects map[string]*opendepotv1alpha1.Version) map[string]struct{} {
	names := make(map[string]struct{}, len(versionObjects))
	for name := range versionObjects {
		names[name] = struct{}{}
	}

	return names
}

// mcpConfigName returns the name that the Versions of an MCPServer are created under.
func mcpConfigName(mcpServer *opendepotv1alpha1.MCPServer) string {
	if mcpServer.Spec.MCPServerSourceConfig.Name != nil {
		return *mcpServer.Spec.MCPServerSourceConfig.Name
	}

	return mcpServer.Name
}

// mcpSourceRef returns a copy of the MCPServer source config that is stored on each Version, with the config name set.
func mcpSourceRef(mcpServer *opendepotv1alpha1.MCPServer) *opendepotv1alpha1.MCPServerSourceConfig {
	config := mcpServer.Spec.MCPServerSourceConfig.DeepCopy()
	name := mcpConfigName(mcpServer)
	config.Name = &name

	return config
}

// mcpNameSegment lowercases a value and maps every character that is not a letter or digit to '-' so that the value is
// a valid part of a Kubernetes resource name.
func mcpNameSegment(value string) string {
	return strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			return r
		}

		return '-'
	}, strings.ToLower(value))
}

// mcpVersionName returns the Version resource name for a version, for example mcp-my-server-1-0-0-1a2b3c4d. The
// name and version are normalized to a Kubernetes-safe form, which is lossy, so a SHA-256 suffix of the unnormalized
// config name and version keeps names distinct. The base is truncated so the name stays within the DNS-1123 label
// length.
func mcpVersionName(configName string, version string) string {
	base := strings.Trim(fmt.Sprintf("mcp-%s-%s", mcpNameSegment(configName), mcpNameSegment(version)), "-")

	maxBaseLength := mcpVersionNameMaxLength - mcpVersionHashLength - 1
	if len(base) > maxBaseLength {
		base = strings.TrimRight(base[:maxBaseLength], "-")
	}

	digest := sha256.Sum256([]byte(configName + "\x00" + version))
	return base + "-" + hex.EncodeToString(digest[:mcpVersionHashLength/2])
}

// trimMCPVersions returns the entries to keep, in their original order. A nil or non-positive limit keeps every entry.
// Otherwise the highest semantic versions are kept.
func trimMCPVersions(entries []opendepotv1alpha1.MCPServerVersion, limit *int) []opendepotv1alpha1.MCPServerVersion {
	if limit == nil || *limit <= 0 || len(entries) <= *limit {
		return entries
	}

	versions := make([]string, 0, len(entries))
	for _, entry := range entries {
		versions = append(versions, entry.Version)
	}

	newest := trimAgentVersions(versions, limit)
	kept := make([]opendepotv1alpha1.MCPServerVersion, 0, *limit)
	for _, entry := range entries {
		if slices.Contains(newest, entry.Version) {
			kept = append(kept, entry)
		}
	}

	return kept
}

// mcpSyncStatus describes how many of the kept versions have synced.
func mcpSyncStatus(synced int, total int) string {
	switch {
	case total == 0:
		return "No versions declared"
	case synced == total:
		return "Successfully synced MCPServer"
	default:
		return fmt.Sprintf("Syncing %d of %d versions", synced, total)
	}
}
