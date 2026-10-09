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
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/google/go-github/v81/github"
	"github.com/google/uuid"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlbuilder "sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	"github.com/tonedefdev/opendepot/pkg/agentspec"
	"github.com/tonedefdev/opendepot/pkg/archive"
	opendepotGithub "github.com/tonedefdev/opendepot/pkg/github"
	"github.com/tonedefdev/opendepot/pkg/hclschema"
	"github.com/tonedefdev/opendepot/pkg/registry"
	"github.com/tonedefdev/opendepot/pkg/storage"
	"github.com/tonedefdev/opendepot/pkg/storage/types"
	"github.com/tonedefdev/opendepot/services/version/internal/policy"
)

const (
	opendepotControllerName = "opendepot-versions-controller"
)

var lookupProviderDownload = registry.LookupProviderDownload

// VersionReconciler reconciles a Version object.
type VersionReconciler struct {
	client.Client
	Log             logr.Logger
	Scheme          *runtime.Scheme
	ScanningEnabled bool
	ScanModules     bool
	TrivyCacheDir   string
	ScanOffline     bool
	BlockOnCritical bool
	BlockOnHigh     bool
	// ScanAgents controls whether Skill and Agent Versions are synced. Agent scanning is always on and fails closed.
	ScanAgents bool
	// AgentAllowedDomains lists the domains an agent may reference. An empty list disables the domain rule.
	AgentAllowedDomains []string
	// JevEnabled turns on the TypeSafe Jev assessment of Skill and Agent Versions.
	JevEnabled bool
	// JevEndpoint overrides the default TypeSafe Jev API endpoint.
	JevEndpoint string
	// AssemblyEnabled turns on Assembly Line contract derivation for module Versions
	// and reduced provider schema extraction for provider Versions.
	AssemblyEnabled bool
	// TofuBinPath is the path to the tofu binary used to extract provider schemas.
	TofuBinPath string
	// SchemaExtractionTimeout bounds how long a single `tofu providers schema` run may take.
	SchemaExtractionTimeout time.Duration
	// scanSem limits the number of concurrent Trivy processes. Each Trivy
	// invocation loads the full vulnerability DB (~2 GiB) so running more than
	// one at a time risks OOMKill even with a generous container memory limit.
	scanSem chan struct{}
	// downloadSem limits the number of concurrent provider archive downloads.
	// Each download streams a ~700 MB zip to disk; allowing all four workers to
	// download simultaneously risks exhausting memory and disk I/O.
	downloadSem chan struct{}
}

// +kubebuilder:rbac:groups=opendepot.defdev.io,resources=versions,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=opendepot.defdev.io,resources=versions/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=opendepot.defdev.io,resources=versions/finalizers,verbs=update
// +kubebuilder:rbac:groups=opendepot.defdev.io,resources=modules,verbs=get
// +kubebuilder:rbac:groups=opendepot.defdev.io,resources=modules/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=opendepot.defdev.io,resources=providers,verbs=get
// +kubebuilder:rbac:groups=opendepot.defdev.io,resources=providers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=opendepot.defdev.io,resources=skills,verbs=get
// +kubebuilder:rbac:groups=opendepot.defdev.io,resources=agents,verbs=get
// +kubebuilder:rbac:groups=opendepot.defdev.io,resources=scanpolicies,verbs=get;list;watch
// +kubebuilder:rbac:groups=opendepot.defdev.io,resources=scanpolicies/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch

func (r *VersionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	version := &opendepotv1alpha1.Version{}
	if err := r.Get(ctx, req.NamespacedName, version); err != nil {
		if k8serr.IsNotFound(err) {
			r.Log.V(5).Info("version resource not found. Ignoring since object must be deleted", "version", req.Name)
			return ctrl.Result{}, nil
		}
		r.Log.Error(err, "Failed to get version", "version", req.Name)
		return ctrl.Result{}, err
	}

	r.Log.V(5).Info(
		"Version found: starting reconciliation",
		"type", version.Spec.Type,
		"version", version.Spec.Version,
		"versionName", version.Name,
	)

	if version.ObjectMeta.DeletionTimestamp.IsZero() {
		if !controllerutil.ContainsFinalizer(version, opendepotv1alpha1.OpenDepotFinalizer) {
			controllerutil.AddFinalizer(version, opendepotv1alpha1.OpenDepotFinalizer)
			if err := r.Update(ctx, version); err != nil {
				return ctrl.Result{}, err
			}

			return ctrl.Result{RequeueAfter: 1 * time.Second}, nil
		}
	} else {
		return r.reconcileDeletion(ctx, version)
	}

	if strings.ContainsAny(version.Name, ".") {
		terminalMsg := fmt.Sprintf("Version name '%s' is invalid: name must not contain '.' characters: rename the resource and reapply", version.Name)
		if !version.Status.Synced && version.Status.SyncStatus == terminalMsg {
			return ctrl.Result{}, nil
		}

		r.Log.V(5).Info("name guard: Version name contains '.' characters; writing terminal status",
			"version", version.Name,
		)

		version.Status.Synced = false
		version.Status.SyncStatus = terminalMsg
		if err := r.Status().Update(ctx, version); err != nil {
			return ctrl.Result{}, err
		}

		return ctrl.Result{}, nil
	}

	var prepareResult ctrl.Result
	var prepareErr error

	switch version.Spec.Type {
	case opendepotv1alpha1.OpenDepotModule:
		prepareResult, prepareErr = r.prepareModuleVersion(ctx, req, version)
	case opendepotv1alpha1.OpenDepotProvider:
		prepareResult, prepareErr = r.prepareProviderVersion(version)
	case opendepotv1alpha1.OpenDepotSkill, opendepotv1alpha1.OpenDepotAgent:
		prepareResult, prepareErr = r.prepareAgentVersion(version)
	default:
		return ctrl.Result{}, fmt.Errorf("no usable type provided on Version '%s'", version.Name)
	}

	if prepareErr != nil {
		return prepareResult, prepareErr
	}

	if prepareResult.RequeueAfter > 0 {
		return prepareResult, nil
	}

	if conflict := configConflictMessage(version); conflict != "" {
		r.Log.V(5).Info("dual-config guard: conflicting source references are set; writing terminal status",
			"version", version.Name,
		)

		version.Status.Synced = false
		version.Status.SyncStatus = conflict
		if err := r.Status().Update(ctx, version); err != nil {
			return ctrl.Result{}, err
		}

		// This is a permanent user configuration error that cannot be resolved by
		// requeuing. Return without an error so controller-runtime does not log a
		// "Reconciler error" and does not schedule unnecessary backoff retries.
		return ctrl.Result{}, nil
	}

	var fileBytes []byte
	isAgent := version.Spec.Type == opendepotv1alpha1.OpenDepotSkill || version.Spec.Type == opendepotv1alpha1.OpenDepotAgent
	var archiveChecksum *string
	var providerTmpPath string
	var readmeConfigMapRef *opendepotv1alpha1.ReadmeConfigMapRef
	var agentMetadata *opendepotv1alpha1.AgentMetadata
	var agentReadme []byte
	var contractConfigMapRef *opendepotv1alpha1.ContractConfigMapRef
	var providerSchemaRef *opendepotv1alpha1.ProviderSchemaRef
	var providerSchemaStatus *opendepotv1alpha1.ProviderSchemaStatus

	switch version.Spec.Type {
	case opendepotv1alpha1.OpenDepotModule, opendepotv1alpha1.OpenDepotSkill, opendepotv1alpha1.OpenDepotAgent:
		// Fast path: if the Version has already been synced and the artifact exists in
		// storage with a matching checksum, skip the GitHub download and any re-scan.
		// Without this, the controller re-downloads the archive from GitHub and re-runs
		// Trivy on every reconcile (e.g. after every controller restart), which hammers
		// the GitHub API and wastes scan CPU even when nothing has changed.
		if isAgent && !r.ScanAgents {
			if version.Status.Synced {
				return ctrl.Result{}, nil
			}

			if err := r.persistAgentBlock(ctx, req, nil, nil, "Agent scanning is disabled on the version controller (--scan-agents=false)"); err != nil {
				return ctrl.Result{}, err
			}

			return ctrl.Result{}, nil
		}

		if version.Status.Checksum != nil && version.Status.Synced && version.Spec.FileName != nil {
			existingFilePath, pathErr := getVersionFilePath(version)
			if pathErr == nil {
				earlySoi := &types.StorageObjectInput{
					Method:   types.Get,
					FilePath: existingFilePath,
					Version:  version,
				}

				if checkErr := r.InitStorageFactory(ctx, earlySoi); checkErr == nil &&
					earlySoi.FileExists &&
					earlySoi.ObjectChecksum != nil &&
					*earlySoi.ObjectChecksum == *version.Status.Checksum {
					bypassFastPath := version.Spec.ForceSync ||
						(version.Spec.Type == opendepotv1alpha1.OpenDepotModule && r.AssemblyEnabled && r.contractNeedsDerivation(ctx, version)) ||
						(isAgent && version.Status.ShaSumsSignature == "")

					if !bypassFastPath {
						r.Log.V(5).Info("module fast-path hit: artifact exists with matching checksum; skipping download", "version", version.Name)

						if result, err := r.reconcileStoredScanPolicy(ctx, req, version); err != nil {
							return ctrl.Result{}, err
						} else if result.RequeueAfter > 0 {
							return result, nil
						}

						return ctrl.Result{}, nil
					}

					r.Log.V(5).Info("module fast-path bypassed; re-downloading the archive",
						"version", version.Name, "forceSync", version.Spec.ForceSync)
				}
			}
		}

		r.Log.V(5).Info("fetching archive", "version", version.Name, "type", version.Spec.Type, "versionStr", version.Spec.Version)
		var moduleBytes []byte
		var checksum *string
		var fetchErr error
		if version.Spec.Type == opendepotv1alpha1.OpenDepotModule {
			moduleBytes, checksum, fetchErr = r.fetchModuleArchive(ctx, version)
		} else {
			moduleBytes, checksum, agentMetadata, agentReadme, fetchErr = r.fetchAgentArchive(ctx, version)
		}

		if fetchErr != nil {
			version.Status.SyncStatus = fmt.Sprintf("Failed to retrieve %s archive: %v", strings.ToLower(version.Spec.Type), fetchErr)
			_ = r.Status().Update(ctx, version)
			return ctrl.Result{}, fetchErr
		}

		r.Log.V(5).Info("archive fetched", "version", version.Name, "bytes", len(moduleBytes))
		fileBytes = moduleBytes
		archiveChecksum = checksum

		if versionImmutable(version) &&
			version.Status.Checksum != nil &&
			archiveChecksum != nil &&
			*version.Status.Checksum != *archiveChecksum {

			statusMsg := fmt.Errorf("version is marked immutable: archive checksum doesn't match existing checksum: got '%s'", *archiveChecksum)
			version.Status.SyncStatus = statusMsg.Error()
			version.Status.Synced = false
			_ = r.Status().Update(ctx, version)
			return ctrl.Result{}, statusMsg
		}

		// Only (re)fetch the README on the module's first sync or when ForceSync is set.
		// This avoids hammering the GitHub API on every reconcile once a README has been
		// resolved. Any failure to resolve or store a README is non-fatal: the Version can
		// still sync successfully without one.
		if version.Status.ReadmeConfigMapRef == nil || version.Spec.ForceSync {
			var readmeBytes []byte
			var readmeErr error
			if version.Spec.Type == opendepotv1alpha1.OpenDepotModule {
				readmeBytes, readmeErr = r.fetchModuleReadme(ctx, version, fileBytes)
			} else {
				readmeBytes = agentReadme
			}
			if readmeErr != nil {
				r.Log.V(5).Info("failed to resolve module readme; skipping", "version", version.Name, "error", readmeErr.Error())
			} else if len(readmeBytes) > 0 {
				ref, upsertErr := r.upsertReadmeConfigMap(ctx, version, readmeBytes)
				if upsertErr != nil {
					r.Log.V(5).Info("failed to upsert readme configmap; skipping", "version", version.Name, "error", upsertErr.Error())
				} else {
					readmeConfigMapRef = ref
				}
			}
		}

		// Derive the Assembly Line contract on the module's first sync, when ForceSync is
		// set, or when newly onboarded provider schemas can improve a degraded contract.
		// Contract derivation is entirely non-fatal: a module that cannot be parsed still
		// syncs, it just records an "unsupported" contract.
		if version.Spec.Type == opendepotv1alpha1.OpenDepotModule && r.AssemblyEnabled && (version.Spec.ForceSync || r.contractNeedsDerivation(ctx, version)) {
			ref, contractErr := r.deriveModuleContract(ctx, version, fileBytes)
			if contractErr != nil {
				r.Log.V(5).Info("failed to derive module contract; skipping", "version", version.Name, "error", contractErr.Error())
			} else {
				contractConfigMapRef = ref
			}
		}
	case opendepotv1alpha1.OpenDepotProvider:
		r.Log.V(5).Info("checking provider fast-path", "version", version.Name, "synced", version.Status.Synced, "checksumSet", version.Status.Checksum != nil)
		// Fast path: if the Version has already been synced and the artifact exists in
		// storage with a matching checksum, there is nothing to download or upload.
		// Skipping the download is critical — /tmp is tmpfs (RAM-backed) in Linux
		// containers, so downloading 700MB per worker on every reconcile exhausts memory.
		if version.Status.Checksum != nil && version.Status.Synced && version.Spec.FileName != nil {
			existingFilePath, pathErr := getVersionFilePath(version)
			if pathErr == nil {
				earlySoi := &types.StorageObjectInput{
					Method:   types.Get,
					FilePath: existingFilePath,
					Version:  version,
				}

				if checkErr := r.InitStorageFactory(ctx, earlySoi); checkErr == nil &&
					earlySoi.FileExists &&
					earlySoi.ObjectChecksum != nil &&
					*earlySoi.ObjectChecksum == *version.Status.Checksum {

					bypassFastPath := version.Spec.ForceSync ||
						(r.AssemblyEnabled && r.providerSchemaNeedsExtraction(version))

					if !bypassFastPath {
						r.Log.V(5).Info("provider fast-path hit: artifact exists with matching checksum; skipping download", "version", version.Name)

						if result, err := r.reconcileStoredScanPolicy(ctx, req, version); err != nil {
							return ctrl.Result{}, err
						} else if result.RequeueAfter > 0 {
							return result, nil
						}

						return ctrl.Result{}, nil
					}

					r.Log.V(5).Info("provider fast-path bypassed; re-downloading the archive",
						"version", version.Name, "forceSync", version.Spec.ForceSync)
				}
			}
		}

		// Serialize provider downloads: each is ~700 MB and concurrent downloads
		// exhaust memory. The semaphore ensures only one download runs at a time.
		r.Log.V(5).Info("waiting for download semaphore", "version", version.Name)
		select {
		case r.downloadSem <- struct{}{}:
		case <-ctx.Done():
			return ctrl.Result{}, ctx.Err()
		}

		r.Log.V(5).Info("download semaphore acquired; fetching provider archive", "version", version.Name)
		tmpPath, cleanupArchive, checksum, fileName, err := r.fetchProviderArchive(ctx, version)

		<-r.downloadSem
		r.Log.V(5).Info("download semaphore released", "version", version.Name)

		if err != nil {
			version.Status.SyncStatus = fmt.Sprintf("Failed to retrieve provider archive from OpenTofu releases API: %v", err)
			_ = r.Status().Update(ctx, version)
			return ctrl.Result{}, err
		}

		r.Log.V(5).Info("provider archive fetched", "version", version.Name, "tmpPath", tmpPath)
		defer cleanupArchive()

		if version.Spec.FileName == nil {
			uuidFileName, err := generateProviderFileName(*fileName)
			if err != nil {
				version.Status.SyncStatus = fmt.Sprintf("Failed to generate UUID filename for provider archive: %v", err)
				_ = r.Status().Update(ctx, version)
				return ctrl.Result{}, err
			}

			version.Spec.FileName = uuidFileName
		}

		archiveChecksum = checksum
		providerTmpPath = tmpPath

		// Extract the reduced provider schema once per provider version. Failures are
		// non-fatal: modules that depend on this provider simply fall back to best-effort
		// output type inference. The outcome is still recorded in ProviderSchemaStatus so
		// a repeatedly failing extraction is visible instead of silently absent.
		if r.AssemblyEnabled && (version.Spec.ForceSync || r.providerSchemaNeedsExtraction(version)) {
			ref, schemaErr := r.extractProviderSchema(ctx, version, providerTmpPath)
			switch {
			case schemaErr != nil:
				r.Log.V(5).Info("failed to extract provider schema; skipping", "version", version.Name, "error", schemaErr.Error())
				providerSchemaStatus = &opendepotv1alpha1.ProviderSchemaStatus{
					State:       "Failed",
					Message:     schemaErr.Error(),
					AttemptedAt: time.Now().UTC().Format(time.RFC3339),
				}
			case ref != nil:
				providerSchemaRef = ref
				providerSchemaStatus = &opendepotv1alpha1.ProviderSchemaStatus{
					State:       "Succeeded",
					AttemptedAt: ref.ExtractedAt,
				}
			}
		}
	}

	filePath, err := getVersionFilePath(version)
	if err != nil {
		// FileName is nil — the spec update that persists it from a previous
		// reconcile has not propagated yet (e.g. the update was lost to a
		// conflict). Write a status message and requeue; do not return an error
		// or controller-runtime will log "Reconciler error" and backoff-requeue
		// indefinitely for what is a transient state.
		r.Log.V(5).Info("cannot compute file path; requeueing", "version", version.Name, "reason", err.Error())
		version.Status.Synced = false
		version.Status.SyncStatus = fmt.Sprintf("Waiting for file path to be available: %v", err)
		_ = r.Status().Update(ctx, version)
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	// For providers the archive is on disk (providerTmpPath); open it as a ReadSeeker so
	// storage backends can stream it without ever loading the full zip into the Go heap.
	var providerFile *os.File
	if providerTmpPath != "" {
		pf, openErr := os.Open(providerTmpPath)
		if openErr != nil {
			version.Status.SyncStatus = fmt.Sprintf("Failed to open provider archive temp file: %v", openErr)
			_ = r.Status().Update(ctx, version)
			return ctrl.Result{}, openErr
		}

		defer pf.Close()
		providerFile = pf
	}

	// hasArtifact indicates whether we have bytes (module) or a temp file (provider) to upload.
	hasArtifact := len(fileBytes) > 0 || providerFile != nil

	soi := &types.StorageObjectInput{
		ArchiveChecksum: archiveChecksum,
		FileBytes:       fileBytes,
		FilePath:        filePath,
		Version:         version,
	}

	if providerFile != nil {
		soi.FileReader = providerFile
	}

	if version.Status.Checksum != nil {
		r.Log.V(5).Info("status checksum set; performing storage get to verify artifact", "version", version.Name)
		soi.Method = types.Get
		if err = r.InitStorageFactory(ctx, soi); err != nil && !errors.Is(err, storage.ErrNotFound) {
			return ctrl.Result{}, err
		}

		// A not-found error just means the artifact is missing from storage (e.g. an
		// evicted local volume, or a bucket object deleted out-of-band) — soi.FileExists
		// stays false and the re-upload check below handles it. It is not fatal.
		r.Log.V(5).Info("storage get complete", "version", version.Name, "fileExists", soi.FileExists)
	} else {
		if !hasArtifact {
			version.Status.Synced = false
			version.Status.SyncStatus = "No artifact bytes available for upload yet"
			_ = r.Status().Update(ctx, version)
			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}

		r.Log.V(5).Info("no status checksum; uploading artifact", "version", version.Name)
		soi.Method = types.Put
		if err = r.InitStorageFactory(ctx, soi); err != nil {
			return ctrl.Result{}, err
		}
		r.Log.V(5).Info("initial storage put complete", "version", version.Name)
	}

	if !soi.FileExists || (soi.ObjectChecksum != nil && version.Status.Checksum != nil && *soi.ObjectChecksum != *version.Status.Checksum) {
		if !hasArtifact {
			version.Status.Synced = false
			version.Status.SyncStatus = "Artifact missing in storage and no bytes available to reconcile"
			_ = r.Status().Update(ctx, version)
			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}

		// Seek back to the start so the storage backend can re-read the provider archive.
		if providerFile != nil {
			if _, seekErr := providerFile.Seek(0, io.SeekStart); seekErr != nil {
				return ctrl.Result{}, fmt.Errorf("failed to seek provider archive: %w", seekErr)
			}
		}

		r.Log.V(5).Info("artifact missing or checksum mismatch; re-uploading", "version", version.Name, "fileExists", soi.FileExists)
		soi.Method = types.Put
		if err = r.InitStorageFactory(ctx, soi); err != nil {
			return ctrl.Result{}, err
		}
		r.Log.V(5).Info("re-upload storage put complete", "version", version.Name)
	}

	if err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		currentVersion := &opendepotv1alpha1.Version{}
		if err := r.Get(ctx, req.NamespacedName, currentVersion); err != nil {
			return err
		}

		currentVersion.Spec.FileName = version.Spec.FileName
		currentVersion.Spec.ModuleConfigRef = version.Spec.ModuleConfigRef
		currentVersion.Spec.ProviderConfigRef = version.Spec.ProviderConfigRef

		if err := r.Update(ctx, currentVersion); err != nil {
			return err
		}
		return nil
	}); err != nil {
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}

	// Run Trivy security scan for provider artifacts when scanning is enabled.
	// The binary scan result is returned here and written in the final status
	// update below so that all required fields (checksum, synced, syncStatus)
	// are set atomically and satisfy CRD required-field validation.
	//
	// Free the in-memory zip bytes before invoking Trivy so that the ~700 MB
	// provider archive and the ~2 GB Trivy vulnerability DB are never resident
	// in the Go heap simultaneously. The scan reads the zip directly from the
	// temp file on disk instead.
	var binaryScan *opendepotv1alpha1.BinaryScan
	var sourceScan *opendepotv1alpha1.SourceScan
	var resolvedRepo string
	var resolvedPolicy policy.Resolved
	var agentSigned *signedAgentArchive
	var agentJevAssessment *opendepotv1alpha1.JevAssessment

	if r.ScanningEnabled || isAgent {
		resolvedPolicy = r.resolveScanPolicy(ctx, version)
	}

	if r.ScanningEnabled && version.Spec.Type == opendepotv1alpha1.OpenDepotProvider && providerTmpPath != "" {
		var scanErr error
		resolvedRepo, binaryScan, sourceScan, scanErr = r.runProviderScan(ctx, version, providerTmpPath, r.TrivyCacheDir, r.ScanOffline, resolvedPolicy)
		r.Log.V(5).Info("provider binary scan complete", "version", version.Name, "findingsPresent", binaryScan != nil)

		if scanErr != nil {
			if err := r.persistScanViolation(ctx, req, binaryScan, sourceScan, scanErr); err != nil {
				r.Log.Error(err, "Failed to persist scan findings for blocked version", "version", version.Name)
			}

			return ctrl.Result{}, scanErr
		}
	}

	// Run Trivy IaC scan for module archives when scanning and module scanning are enabled.
	// The scan result is returned here and written atomically in the final status update below.
	if r.ScanningEnabled && r.ScanModules && version.Spec.Type == opendepotv1alpha1.OpenDepotModule && len(fileBytes) > 0 {
		var scanErr error
		sourceScan, scanErr = r.runModuleScan(ctx, version, fileBytes, r.TrivyCacheDir, r.ScanOffline, resolvedPolicy)
		r.Log.V(5).Info("module source scan complete", "version", version.Name, "findingsPresent", sourceScan != nil)

		if scanErr != nil {
			if err := r.persistScanViolation(ctx, req, binaryScan, sourceScan, scanErr); err != nil {
				r.Log.Error(err, "Failed to persist scan findings for blocked version", "version", version.Name)
			}

			return ctrl.Result{}, scanErr
		}
	}

	// Skill and Agent Versions pass the gates in order: Trivy, agentspec and Expr, Jev, and then signing.
	// Signing is the last gate, so a Version is never published without a signature.
	if isAgent {
		agentSourceScan, content, blocking, scanErr := r.runAgentScan(ctx, version, fileBytes, r.TrivyCacheDir, r.ScanOffline, resolvedPolicy)
		if scanErr != nil {
			if err := r.persistAgentBlock(ctx, req, nil, nil, fmt.Sprintf("Agent scan failed: %v", scanErr)); err != nil {
				r.Log.Error(err, "Failed to persist agent scan failure", "version", version.Name)
			}

			return ctrl.Result{}, scanErr
		}

		sourceScan = agentSourceScan
		if blocking != nil {
			blockErr := fmt.Errorf("blocking: %s finding %s in %s: %s", blocking.Severity, blocking.VulnerabilityID, blocking.PkgName, blocking.Title)
			if err := r.persistScanViolation(ctx, req, nil, sourceScan, blockErr); err != nil {
				r.Log.Error(err, "Failed to persist scan findings for blocked version", "version", version.Name)
			}

			return ctrl.Result{}, blockErr
		}

		jevAssessment, jevErr := r.runAgentJev(ctx, version, content)
		if jevErr != nil {
			if err := r.persistAgentBlock(ctx, req, sourceScan, jevAssessment, fmt.Sprintf("JevUnavailable: %v", jevErr)); err != nil {
				r.Log.Error(err, "Failed to persist jev unavailable status", "version", version.Name)
			}

			return ctrl.Result{}, jevErr
		}

		if jevAssessment != nil && jevAssessment.Blocked {
			if err := r.persistAgentBlock(ctx, req, sourceScan, jevAssessment, "JevBlocked: "+strings.Join(jevAssessment.BlockReasons, "; ")); err != nil {
				r.Log.Error(err, "Failed to persist jev block", "version", version.Name)
			}

			return ctrl.Result{}, nil
		}

		storedPath, err := getVersionFilePath(version)
		if err != nil {
			return ctrl.Result{}, err
		}

		signed, signErr := signAgentArchive(fileBytes, filepath.Base(*storedPath))
		if signErr != nil {
			if err := r.persistAgentBlock(ctx, req, sourceScan, jevAssessment, fmt.Sprintf("SigningUnavailable: %v", signErr)); err != nil {
				r.Log.Error(err, "Failed to persist signing unavailable status", "version", version.Name)
			}

			return ctrl.Result{}, signErr
		}

		agentSigned = signed
		agentJevAssessment = jevAssessment
	}

	if err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		currentVersion := &opendepotv1alpha1.Version{}
		if err := r.Get(ctx, req.NamespacedName, currentVersion); err != nil {
			return err
		}

		currentVersion.Status.Synced = true
		if archiveChecksum != nil {
			currentVersion.Status.Checksum = archiveChecksum
		}

		if soi.BytesWritten > 0 {
			currentVersion.Status.ArchiveSizeBytes = &soi.BytesWritten
		} else if len(fileBytes) > 0 {
			size := int64(len(fileBytes))
			currentVersion.Status.ArchiveSizeBytes = &size
		} else if providerTmpPath != "" {
			if info, statErr := os.Stat(providerTmpPath); statErr == nil {
				size := info.Size()
				currentVersion.Status.ArchiveSizeBytes = &size
			}
		}

		currentVersion.Status.SyncStatus = "Successfully synced version"
		if binaryScan != nil {
			currentVersion.Status.BinaryScan = binaryScan
		}

		if sourceScan != nil {
			currentVersion.Status.SourceScan = sourceScan
		}

		if readmeConfigMapRef != nil {
			currentVersion.Status.ReadmeConfigMapRef = readmeConfigMapRef
		}

		if agentMetadata != nil {
			currentVersion.Status.AgentMetadata = agentMetadata
		}

		if contractConfigMapRef != nil {
			currentVersion.Status.ContractConfigMapRef = contractConfigMapRef
		}

		if providerSchemaRef != nil {
			currentVersion.Status.ProviderSchemaRef = providerSchemaRef
		}

		if providerSchemaStatus != nil {
			currentVersion.Status.ProviderSchemaStatus = providerSchemaStatus
		}

		if agentSigned != nil {
			currentVersion.Status.ShaSums = agentSigned.sums
			currentVersion.Status.ShaSumsSignature = agentSigned.signature
			currentVersion.Status.SigningKeyFingerprint = agentSigned.fingerprint
		}

		if agentJevAssessment != nil {
			currentVersion.Status.JevAssessment = agentJevAssessment
		}

		if err := r.Status().Update(ctx, currentVersion, &client.SubResourceUpdateOptions{
			UpdateOptions: client.UpdateOptions{FieldManager: opendepotControllerName},
		}); err != nil {
			return err
		}

		return nil
	}); err != nil {
		return ctrl.Result{}, err
	}

	// Reset forceSync to false now that reconciliation has completed successfully.
	if version.Spec.ForceSync {
		if err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
			currentVersion := &opendepotv1alpha1.Version{}
			if err := r.Get(ctx, req.NamespacedName, currentVersion); err != nil {
				return err
			}
			currentVersion.Spec.ForceSync = false
			return r.Update(ctx, currentVersion, &client.UpdateOptions{
				FieldManager: opendepotControllerName,
			})
		}); err != nil {
			r.Log.Error(err, "Failed to reset forceSync on Version", "version", version.Name)
			return ctrl.Result{}, err
		}
	}

	if err := r.patchProviderResolvedRepo(ctx, version, resolvedRepo); err != nil {
		r.Log.V(5).Info("patchProviderResolvedRepo failed — non-fatal", "error", err)
	}

	// Schedule a requeue for the earliest exemption expiry so that an expired exemption
	// re-enforces on its own rather than waiting for an unrelated event on this Version.
	if resolvedPolicy.NextExpiry != nil {
		requeueAfter := time.Until(*resolvedPolicy.NextExpiry)
		if requeueAfter > 0 {
			r.Log.V(5).Info("Requeueing for scan exemption expiry",
				"version", version.Name, "scanPolicy", resolvedPolicy.PolicyName, "expiresAt", *resolvedPolicy.NextExpiry)

			return ctrl.Result{RequeueAfter: requeueAfter}, nil
		}
	}

	return ctrl.Result{}, nil
}

// persistScanViolation records a blocking scan result on the Version status. The findings
// are written alongside the failure message so that a blocked Version still reports which
// findings caused the block, which is what allows a ScanPolicy exemption to be authored
// for them.
func (r *VersionReconciler) persistScanViolation(
	ctx context.Context,
	req ctrl.Request,
	binaryScan *opendepotv1alpha1.BinaryScan,
	sourceScan *opendepotv1alpha1.SourceScan,
	scanErr error,
) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		currentVersion := &opendepotv1alpha1.Version{}
		if err := r.Get(ctx, req.NamespacedName, currentVersion); err != nil {
			return err
		}

		currentVersion.Status.Synced = false
		currentVersion.Status.SyncStatus = fmt.Sprintf("Scan policy violation: %v", scanErr)
		if binaryScan != nil {
			currentVersion.Status.BinaryScan = binaryScan
		}

		if sourceScan != nil {
			currentVersion.Status.SourceScan = sourceScan
		}

		return r.Status().Update(ctx, currentVersion, &client.SubResourceUpdateOptions{
			UpdateOptions: client.UpdateOptions{FieldManager: opendepotControllerName},
		})
	})
}

// persistAgentBlock records a Skill or Agent Version that did not pass a gate. The Version stays unsynced,
// and any scan or Jev result gathered so far is kept for review.
func (r *VersionReconciler) persistAgentBlock(
	ctx context.Context,
	req ctrl.Request,
	sourceScan *opendepotv1alpha1.SourceScan,
	jevAssessment *opendepotv1alpha1.JevAssessment,
	syncStatus string,
) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		currentVersion := &opendepotv1alpha1.Version{}
		if err := r.Get(ctx, req.NamespacedName, currentVersion); err != nil {
			return err
		}

		currentVersion.Status.Synced = false
		currentVersion.Status.SyncStatus = syncStatus
		if sourceScan != nil {
			currentVersion.Status.SourceScan = sourceScan
		}

		if jevAssessment != nil {
			currentVersion.Status.JevAssessment = jevAssessment
		}

		return r.Status().Update(ctx, currentVersion, &client.SubResourceUpdateOptions{
			UpdateOptions: client.UpdateOptions{FieldManager: opendepotControllerName},
		})
	})
}

// reconcileStoredScanPolicy re-evaluates findings already recorded on a synced Version.
// ScanPolicy events intentionally use the normal Version reconcile queue, but the storage
// fast path would otherwise return before the scanner can observe a newly written policy.
func (r *VersionReconciler) reconcileStoredScanPolicy(
	ctx context.Context,
	req ctrl.Request,
	version *opendepotv1alpha1.Version,
) (ctrl.Result, error) {
	isAgent := version.Spec.Type == opendepotv1alpha1.OpenDepotSkill || version.Spec.Type == opendepotv1alpha1.OpenDepotAgent
	if (!r.ScanningEnabled && !isAgent) || (version.Status.BinaryScan == nil && version.Status.SourceScan == nil) {
		return ctrl.Result{}, nil
	}

	resolved := r.resolveScanPolicy(ctx, version)
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		current := &opendepotv1alpha1.Version{}
		if err := r.Get(ctx, req.NamespacedName, current); err != nil {
			return err
		}

		changed := false
		var blocking *opendepotv1alpha1.SecurityFinding
		blockingType := ""

		if current.Status.BinaryScan != nil {
			annotated, finding := policy.Apply(resolved, current.Status.BinaryScan.Findings, policy.ScanTypeBinary)
			if !reflect.DeepEqual(annotated, current.Status.BinaryScan.Findings) {
				changed = true
			}
			current.Status.BinaryScan = &opendepotv1alpha1.BinaryScan{
				ScannedAt: current.Status.BinaryScan.ScannedAt,
				Findings:  annotated,
			}
			if finding != nil {
				blocking = finding
				blockingType = "binary"
			}
		}

		if current.Status.SourceScan != nil {
			scanType := policy.ScanTypeSource
			switch current.Spec.Type {
			case opendepotv1alpha1.OpenDepotModule:
				scanType = policy.ScanTypeModule
			case opendepotv1alpha1.OpenDepotSkill, opendepotv1alpha1.OpenDepotAgent:
				scanType = policy.ScanTypeAgent
			}

			annotated, finding := policy.Apply(resolved, current.Status.SourceScan.Findings, scanType)
			if !reflect.DeepEqual(annotated, current.Status.SourceScan.Findings) {
				changed = true
			}
			current.Status.SourceScan = &opendepotv1alpha1.SourceScan{
				ScannedAt: current.Status.SourceScan.ScannedAt,
				Findings:  annotated,
			}
			if blocking == nil && finding != nil {
				blocking = finding
				blockingType = scanType
			}
		}

		if blocking != nil {
			message := fmt.Sprintf("Scan policy violation: blocking: %s vulnerability %s in %s (%s %s)",
				blocking.Severity, blocking.VulnerabilityID, blockingType, blocking.PkgName, blocking.InstalledVersion)
			if current.Status.Synced || current.Status.SyncStatus != message {
				changed = true
			}
			current.Status.Synced = false
			current.Status.SyncStatus = message
		} else if strings.HasPrefix(current.Status.SyncStatus, "Scan policy violation:") {
			changed = true
			current.Status.Synced = true
			current.Status.SyncStatus = "Synced"
		}

		if !changed {
			return nil
		}

		return r.Status().Update(ctx, current, &client.SubResourceUpdateOptions{
			UpdateOptions: client.UpdateOptions{FieldManager: opendepotControllerName},
		})
	})
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("reconcile stored scan policy: %w", err)
	}

	if resolved.NextExpiry != nil {
		if requeueAfter := time.Until(*resolved.NextExpiry); requeueAfter > 0 {
			return ctrl.Result{RequeueAfter: requeueAfter}, nil
		}
	}

	return ctrl.Result{}, nil
}

// patchProviderResolvedRepo writes repoURL to the Provider's status.resolvedSourceRepository
// field. It is idempotent: if the field is already set the update is skipped. Errors are
// logged at verbosity 5 and are non-fatal so that a transient API-server hiccup does not
// prevent the Version from completing its own reconciliation.
func (r *VersionReconciler) patchProviderResolvedRepo(ctx context.Context, version *opendepotv1alpha1.Version, repoURL string) error {
	if repoURL == "" || version.Spec.ProviderConfigRef == nil || version.Spec.ProviderConfigRef.Name == nil {
		r.Log.V(5).Info("repo url was empty", "version", version.Name, "repoURL", repoURL)
		return nil
	}

	provider := &opendepotv1alpha1.Provider{}
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		if err := r.Get(ctx, k8stypes.NamespacedName{Name: *version.Spec.ProviderConfigRef.Name, Namespace: version.Namespace}, provider); err != nil {
			if k8serr.IsNotFound(err) {
				return nil
			}

			return fmt.Errorf("patchProviderResolvedRepo: failed to get provider: %w", err)
		}

		if provider.Status.ResolvedSourceRepository != "" {
			return nil
		}

		provider.Status.ResolvedSourceRepository = repoURL
		if err := r.Status().Update(ctx, provider, &client.SubResourceUpdateOptions{
			UpdateOptions: client.UpdateOptions{FieldManager: opendepotControllerName},
		}); err != nil {
			return fmt.Errorf("patchProviderResolvedRepo: failed to update provider status: %w", err)
		}

		r.Log.V(5).Info("patched provider with resolved source repository", "provider", provider.Name, "repoURL", repoURL)
		return nil
	})
}

// reconcileDeletion removes the stored artifact and finalizer when a Version is being deleted.
func (r *VersionReconciler) reconcileDeletion(ctx context.Context, version *opendepotv1alpha1.Version) (ctrl.Result, error) {
	r.Log.V(5).Info("reconciling deletion", "version", version.Name)
	if !controllerutil.ContainsFinalizer(version, opendepotv1alpha1.OpenDepotFinalizer) {
		r.Log.V(5).Info("no finalizer present; skipping deletion reconciliation", "version", version.Name)
		return ctrl.Result{}, nil
	}

	filePath, err := getVersionFilePath(version)
	if err != nil {
		// If the file path cannot be resolved (e.g. FileName was never persisted
		// because the Version was deleted before its first successful sync), there
		// is no stored artifact to remove. Skip storage deletion and proceed
		// directly to finalizer removal so the object is not stuck terminating.
		r.Log.V(5).Info("skipping storage deletion: cannot resolve file path; removing finalizer directly",
			"version", version.Name, "reason", err.Error(),
		)
		controllerutil.RemoveFinalizer(version, opendepotv1alpha1.OpenDepotFinalizer)
		if err := r.Update(ctx, version); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	soi := &types.StorageObjectInput{
		Method:   types.Delete,
		FilePath: filePath,
		Version:  version,
	}

	r.Log.V(5).Info("deleting stored artifact", "version", version.Name, "filePath", filePath)
	if err := r.InitStorageFactory(ctx, soi); err != nil {
		return ctrl.Result{}, err
	}

	r.Log.V(5).Info("artifact deleted; removing finalizer", "version", version.Name)
	controllerutil.RemoveFinalizer(version, opendepotv1alpha1.OpenDepotFinalizer)
	if err := r.Update(ctx, version); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// prepareModuleVersion resolves the backing Module metadata required to reconcile a module Version.
// When moduleConfigRef.name is absent, or when no Module CR with that name exists in the namespace,
// the config is treated as fully inline: a UUID filename is generated so getVersionFilePath has a
// stable storage key, and the GitHub download proceeds using the fields already on moduleConfigRef.
func (r *VersionReconciler) prepareModuleVersion(ctx context.Context, req ctrl.Request, version *opendepotv1alpha1.Version) (ctrl.Result, error) {
	if version.Spec.ModuleConfigRef == nil {
		return ctrl.Result{}, fmt.Errorf("moduleConfigRef is required for module version '%s'", version.Name)
	}

	// No Module CR name provided — the caller supplied all config inline.
	if version.Spec.ModuleConfigRef.Name == nil {
		if version.Spec.FileName == nil {
			uuidFileName, err := generateModuleFileName(version.Spec.ModuleConfigRef.FileFormat)
			if err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to generate UUID filename for module archive: %w", err)
			}
			version.Spec.FileName = uuidFileName
		}
		return ctrl.Result{}, nil
	}

	moduleObject := client.ObjectKey{Name: *version.Spec.ModuleConfigRef.Name, Namespace: req.Namespace}
	module := &opendepotv1alpha1.Module{}
	if err := r.Get(ctx, moduleObject, module); err != nil {
		if k8serr.IsNotFound(err) {
			// No backing Module CR — treat as inline config, using Name as the GitHub repo name.
			if version.Spec.FileName == nil {
				uuidFileName, err := generateModuleFileName(version.Spec.ModuleConfigRef.FileFormat)
				if err != nil {
					return ctrl.Result{}, fmt.Errorf("failed to generate UUID filename for module archive: %w", err)
				}
				version.Spec.FileName = uuidFileName
			}
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if module.Status.ModuleVersionRefs == nil {
		// Module CR exists but the module controller has not populated any refs yet
		// (e.g. the module controller is not deployed in this environment). Fall back
		// to inline mode so the version controller can operate independently.
		if version.Spec.FileName == nil {
			uuidFileName, err := generateModuleFileName(version.Spec.ModuleConfigRef.FileFormat)
			if err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to generate UUID filename for module archive: %w", err)
			}
			version.Spec.FileName = uuidFileName
		}
		return ctrl.Result{}, nil
	}

	moduleRef, exists := module.Status.ModuleVersionRefs[version.Spec.Version]
	if !exists || moduleRef == nil || moduleRef.FileName == nil {
		// The Module CR exists but has no ref for this specific version — the module
		// controller may not be deployed or has not reconciled this version yet. Fall
		// back to inline mode so this Version can be synced independently.
		if version.Spec.FileName == nil {
			uuidFileName, err := generateModuleFileName(version.Spec.ModuleConfigRef.FileFormat)
			if err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to generate UUID filename for module archive: %w", err)
			}
			version.Spec.FileName = uuidFileName
		}
		return ctrl.Result{}, nil
	}

	version.Spec.ModuleConfigRef = &module.Spec.ModuleConfig
	version.Spec.FileName = moduleRef.FileName

	if version.Spec.ModuleConfigRef.Name == nil {
		version.Spec.ModuleConfigRef.Name = &module.ObjectMeta.Name
	}

	return ctrl.Result{}, nil
}

// prepareProviderVersion validates provider references and ensures required provider fields are present.
func (r *VersionReconciler) prepareProviderVersion(version *opendepotv1alpha1.Version) (ctrl.Result, error) {
	if version.Spec.ProviderConfigRef == nil {
		return ctrl.Result{}, fmt.Errorf("providerConfigRef is required for provider version '%s'", version.Name)
	}

	if version.Spec.ProviderConfigRef.Name == nil {
		providerName := version.Labels["opendepot.defdev.io/provider"]
		if providerName == "" {
			return ctrl.Result{}, fmt.Errorf("providerConfigRef.name is required for provider version '%s'", version.Name)
		}
		version.Spec.ProviderConfigRef.Name = &providerName
	}

	return ctrl.Result{}, nil
}

// configConflictMessage returns a terminal status message when more than one of moduleConfigRef,
// providerConfigRef, or agentSourceRef is set, or an empty string when the references are compatible.
func configConflictMessage(version *opendepotv1alpha1.Version) string {
	if version.Spec.ModuleConfigRef != nil && version.Spec.ProviderConfigRef != nil {
		return "Only one of 'ModuleConfigRef' or 'ProviderConfigRef' can be provided: both are defined"
	}

	if version.Spec.AgentSourceRef != nil && (version.Spec.ModuleConfigRef != nil || version.Spec.ProviderConfigRef != nil) {
		return "Only one of 'AgentSourceRef', 'ModuleConfigRef', or 'ProviderConfigRef' can be provided: multiple are defined"
	}

	return ""
}

// versionImmutable reports whether the Version's source config requires its archive checksum to remain unchanged.
func versionImmutable(version *opendepotv1alpha1.Version) bool {
	if version.Spec.ModuleConfigRef != nil && version.Spec.ModuleConfigRef.Immutable != nil {
		return *version.Spec.ModuleConfigRef.Immutable
	}

	if version.Spec.AgentSourceRef != nil && version.Spec.AgentSourceRef.Immutable != nil {
		return *version.Spec.AgentSourceRef.Immutable
	}

	return false
}

// prepareAgentVersion validates the agentSourceRef of a Skill or Agent Version and ensures its required fields are present.
func (r *VersionReconciler) prepareAgentVersion(version *opendepotv1alpha1.Version) (ctrl.Result, error) {
	if version.Spec.AgentSourceRef == nil {
		return ctrl.Result{}, fmt.Errorf("agentSourceRef is required for %s version '%s'", strings.ToLower(version.Spec.Type), version.Name)
	}

	if version.Spec.AgentSourceRef.Name == nil {
		agentName := version.Labels["opendepot.defdev.io/"+strings.ToLower(version.Spec.Type)]
		if agentName == "" {
			return ctrl.Result{}, fmt.Errorf("agentSourceRef.name is required for %s version '%s'", strings.ToLower(version.Spec.Type), version.Name)
		}
		version.Spec.AgentSourceRef.Name = &agentName
	}

	if version.Spec.FileName == nil {
		uuidFileName, err := generateModuleFileName(nil)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to generate UUID filename for %s archive: %w", strings.ToLower(version.Spec.Type), err)
		}
		version.Spec.FileName = uuidFileName
	}

	return ctrl.Result{}, nil
}

// fetchAgentArchive downloads the Skill or Agent source directory from GitHub, validates its definition file,
// and returns a deterministic tar.gz of only that directory along with its checksum, parsed metadata, and README body.
func (r *VersionReconciler) fetchAgentArchive(ctx context.Context, version *opendepotv1alpha1.Version) ([]byte, *string, *opendepotv1alpha1.AgentMetadata, []byte, error) {
	sourceRef := version.Spec.AgentSourceRef

	useAuthClient := false
	if sourceRef.GithubClientConfig != nil {
		useAuthClient = sourceRef.GithubClientConfig.UseAuthenticatedClient
	}

	var githubClientConfig *opendepotGithub.GithubClientConfig
	var err error
	if useAuthClient {
		githubClientConfig, err = opendepotGithub.GetGithubApplicationSecret(ctx, r.Client, version.Namespace)
		if err != nil {
			return nil, nil, nil, nil, err
		}
	}

	githubClient, err := opendepotGithub.CreateGithubClient(ctx, useAuthClient, githubClientConfig)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	tagPrefix := ""
	if sourceRef.TagPrefix != nil {
		tagPrefix = *sourceRef.TagPrefix
	}

	repoName := opendepotGithub.AgentSourceRepoName(sourceRef)
	var tarball []byte
	var lastErr error
	for _, tag := range opendepotGithub.TagCandidates(tagPrefix, version.Spec.Version) {
		resp, reqErr := opendepotGithub.GetArchiveRequest(ctx, githubClient, sourceRef.RepoOwner, repoName, github.Tarball, tag)
		if reqErr != nil {
			lastErr = reqErr
			continue
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			lastErr = fmt.Errorf("failed to get archive for tag '%s': status code %d", tag, resp.StatusCode)
			continue
		}

		tarball, err = io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, nil, nil, nil, fmt.Errorf("failed to read archive data: %w", err)
		}

		lastErr = nil
		break
	}

	if lastErr != nil {
		return nil, nil, nil, nil, lastErr
	}

	tmpDir, err := os.MkdirTemp("", "agent-archive-")
	if err != nil {
		return nil, nil, nil, nil, err
	}
	defer os.RemoveAll(tmpDir)

	if err = archive.ExtractToDir(tarball, tmpDir); err != nil {
		return nil, nil, nil, nil, fmt.Errorf("failed to extract archive: %w", err)
	}

	// GitHub tarballs contain a single top-level directory named after the repository and commit.
	rootDir := tmpDir
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	if len(entries) == 1 && entries[0].IsDir() {
		rootDir = filepath.Join(tmpDir, entries[0].Name())
	}

	// Cleaning the path relative to a rooted path prevents traversal outside of the repository.
	agentDir := filepath.Join(rootDir, filepath.Clean("/"+sourceRef.Path))
	info, err := os.Stat(agentDir)
	if err != nil || !info.IsDir() {
		return nil, nil, nil, nil, fmt.Errorf("path '%s' is not a directory in repository '%s'", sourceRef.Path, repoName)
	}

	entryPath := filepath.Join(agentDir, "SKILL.md")
	if version.Spec.Type == opendepotv1alpha1.OpenDepotAgent {
		if sourceRef.Name == nil {
			return nil, nil, nil, nil, fmt.Errorf("agentSourceRef.name is required for agent version '%s'", version.Name)
		}

		entryPath, err = agentspec.FindAgentEntry(agentDir, *sourceRef.Name)
		if err != nil {
			return nil, nil, nil, nil, err
		}
	}

	content, err := os.ReadFile(entryPath)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("failed to read %s: %w", filepath.Base(entryPath), err)
	}

	parsed, err := agentspec.ParseAgent(entryPath, content)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("failed to validate %s: %w", filepath.Base(entryPath), err)
	}

	var packed bytes.Buffer
	if err = archive.WriteDeterministicTarGz(agentDir, &packed); err != nil {
		return nil, nil, nil, nil, fmt.Errorf("failed to package archive: %w", err)
	}

	sum := sha256.Sum256(packed.Bytes())
	checksum := base64.StdEncoding.EncodeToString(sum[:])
	metadata := &opendepotv1alpha1.AgentMetadata{
		Name:        parsed.Name,
		Description: parsed.Description,
		Tools:       parsed.Tools,
		Model:       parsed.Model,
	}

	return packed.Bytes(), &checksum, metadata, []byte(parsed.Body), nil
}

// fetchModuleArchive downloads module source from GitHub and returns bytes with a checksum.
func (r *VersionReconciler) fetchModuleArchive(ctx context.Context, version *opendepotv1alpha1.Version) ([]byte, *string, error) {
	var githubClientConfig *opendepotGithub.GithubClientConfig
	var githubClient *github.Client

	useAuthClient := false
	if version.Spec.ModuleConfigRef.GithubClientConfig != nil {
		useAuthClient = version.Spec.ModuleConfigRef.GithubClientConfig.UseAuthenticatedClient
	}

	var err error
	if useAuthClient {
		githubClientConfig, err = opendepotGithub.GetGithubApplicationSecret(ctx, r.Client, version.Namespace)
		if err != nil {
			return nil, nil, err
		}
	}

	githubClient, err = opendepotGithub.CreateGithubClient(ctx, useAuthClient, githubClientConfig)
	if err != nil {
		return nil, nil, err
	}

	var fileFormat github.ArchiveFormat
	if version.Spec.FileName != nil && strings.Contains(*version.Spec.FileName, "zip") {
		fileFormat = github.Zipball
	} else {
		fileFormat = github.Tarball
	}

	return opendepotGithub.GetModuleArchiveFromRef(ctx, r.Log, githubClient, version, fileFormat)
}

// generateModuleFileName returns a randomly generated UUID7 filename for a module archive.
// The default extension is .tar.gz; pass fileFormat = "zip" to get a .zip extension.
func generateModuleFileName(fileFormat *string) (*string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	ext := ".tar.gz"
	if fileFormat != nil && *fileFormat == "zip" {
		ext = ".zip"
	}
	name := fmt.Sprintf("%s%s", id, ext)
	return &name, nil
}

// generateProviderFileName returns a randomly generated UUID7 filename, preserving the original file extension.
func generateProviderFileName(originalFileName string) (*string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}

	ext := path.Ext(originalFileName)
	name := fmt.Sprintf("%s%s", id, ext)
	return &name, nil
}

// fetchProviderArchive resolves a provider binary download from the configured upstream registry
// and streams the artifact to a temporary file on disk to avoid buffering the
// full provider zip (~700 MB) in the Go heap. The caller must invoke the returned
// cleanup function (typically via defer) to remove the temp file.
func (r *VersionReconciler) fetchProviderArchive(ctx context.Context, version *opendepotv1alpha1.Version) (archivePath string, cleanup func(), checksum *string, fileName *string, err error) {
	r.Log.V(5).Info("looking up provider download URL", "version", version.Name, "versionStr", version.Spec.Version, "os", version.Spec.OperatingSystem, "arch", version.Spec.Architecture)
	if version.Spec.ProviderConfigRef == nil || version.Spec.ProviderConfigRef.Name == nil {
		return "", func() {}, nil, nil, fmt.Errorf("providerConfigRef.name is required")
	}

	if strings.TrimSpace(version.Spec.OperatingSystem) == "" || strings.TrimSpace(version.Spec.Architecture) == "" {
		return "", func() {}, nil, nil, fmt.Errorf("provider operatingSystem and architecture are required")
	}

	providerName := strings.TrimSpace(*version.Spec.ProviderConfigRef.Name)
	providerVersion := strings.TrimPrefix(strings.TrimSpace(version.Spec.Version), "v")
	if providerVersion == "" {
		return "", func() {}, nil, nil, fmt.Errorf("provider version is empty")
	}

	providerNamespace := "hashicorp"
	if version.Spec.ProviderConfigRef.Namespace != nil {
		if ns := strings.TrimSpace(*version.Spec.ProviderConfigRef.Namespace); ns != "" {
			providerNamespace = ns
		}
	}

	upstreamRegistry := opendepotv1alpha1.ProviderUpstreamRegistry(version.Spec.ProviderConfigRef)
	download, err := lookupProviderDownload(ctx, upstreamRegistry, providerNamespace, providerName, providerVersion,
		version.Spec.OperatingSystem, version.Spec.Architecture)
	if err != nil {
		return "", func() {}, nil, nil, err
	}

	r.Log.V(5).Info("provider download resolved; streaming archive", "version", version.Name, "upstreamRegistry", upstreamRegistry, "filename", download.Filename)

	tmpPath, checksumHex, cleanupFn, err := httpStreamToFile(ctx, download.DownloadURL)
	if err != nil {
		return "", func() {}, nil, nil, err
	}

	// Validate the downloaded archive against the registry-provided SHA256.
	if download.Shasum != "" {
		if checksumHex != strings.ToLower(download.Shasum) {
			cleanupFn()
			return "", func() {}, nil, nil, fmt.Errorf("checksum mismatch for provider archive %s: registry expected %s, got %s",
				download.Filename, download.Shasum, checksumHex)
		}
		r.Log.V(5).Info("provider archive checksum verified", "version", version.Name, "sha256", checksumHex)
	}

	// Re-encode the hex SHA-256 as base64 for storage (matches the existing format).
	checksumBytes, _ := hex.DecodeString(checksumHex)
	checksumB64 := base64.StdEncoding.EncodeToString(checksumBytes)

	fn := download.Filename
	if fn == "" {
		parsedDownloadURL, err := url.Parse(download.DownloadURL)
		if err == nil {
			fn = path.Base(parsedDownloadURL.Path)
		}
	}

	if fn == "." || fn == "/" || fn == "" {
		cleanupFn()
		return "", func() {}, nil, nil, fmt.Errorf("unable to determine filename from provider download URL '%s'", redactedURL(download.DownloadURL))
	}

	return tmpPath, cleanupFn, &checksumB64, &fn, nil
}

// httpStreamToFile streams an HTTP GET response body to a temporary file on disk
// while computing its SHA-256 checksum via an io.TeeReader. It returns the temp
// file path, the hex-encoded checksum, a cleanup function that removes the file,
// and any error. This avoids buffering large provider binaries (~700 MB) in the
// Go heap, which is critical when multiple reconcilers run concurrently.
func httpStreamToFile(ctx context.Context, requestURL string) (filePath string, checksumHex string, cleanup func(), err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return "", "", func() {}, fmt.Errorf("invalid provider download URL '%s'", redactedURL(requestURL))
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", func() {}, fmt.Errorf("request failed for '%s'", redactedURL(requestURL))
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", func() {}, fmt.Errorf("request to '%s' failed with status %d", redactedURL(requestURL), resp.StatusCode)
	}

	f, err := os.CreateTemp("", "opendepot-provider-*.zip")
	if err != nil {
		return "", "", func() {}, fmt.Errorf("failed to create temp file for provider download: %w", err)
	}

	cleanupFn := func() {
		f.Close()
		os.Remove(f.Name())
	}

	h := sha256.New()
	if _, err = io.Copy(f, io.TeeReader(resp.Body, h)); err != nil {
		cleanupFn()
		return "", "", func() {}, fmt.Errorf("failed to stream provider archive from '%s': %w", redactedURL(requestURL), err)
	}

	if err = f.Sync(); err != nil {
		cleanupFn()
		return "", "", func() {}, fmt.Errorf("failed to sync provider archive temp file: %w", err)
	}

	return f.Name(), fmt.Sprintf("%x", h.Sum(nil)), cleanupFn, nil
}

func redactedURL(rawURL string) string {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return "<invalid URL>"
	}

	parsedURL.User = nil
	parsedURL.RawQuery = ""
	parsedURL.ForceQuery = false
	parsedURL.Fragment = ""
	parsedURL.RawFragment = ""

	return parsedURL.String()
}

// SetupWithManager sets up the controller with the Manager.
func (r *VersionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Allow at most one Trivy process at a time to prevent concurrent DB loads
	// from exhausting container memory when multiple reconcilers run in parallel.
	r.scanSem = make(chan struct{}, 1)
	// Allow at most one provider archive download at a time. Each download is
	// ~700 MB; concurrent downloads exhaust memory and storage I/O.
	r.downloadSem = make(chan struct{}, 1)
	// GenerationChangedPredicate only overrides Update; its Create/Delete/Generic all
	// default to true. client-go informers synthesize a Create event for every
	// pre-existing object during the initial cache sync on every controller restart, so
	// without this additional filter, every already-synced Version in the cluster is
	// re-enqueued on every restart, and the in-Reconcile fast path still has to make a
	// storage existence/checksum call per object before it can bail out - hammering
	// storage and backing up the work queue for large fleets even though nothing changed.
	// Genuinely new Versions have never synced, so they always pass through untouched;
	// Versions being deleted must also pass through so finalizer cleanup still runs.
	skipResyncedCreate := predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			version, ok := e.Object.(*opendepotv1alpha1.Version)
			if !ok {
				return true
			}

			if !version.ObjectMeta.DeletionTimestamp.IsZero() {
				return true
			}

			return version.Spec.ForceSync || !version.Status.Synced || version.Status.Checksum == nil
		},
	}

	builder := ctrl.NewControllerManagedBy(mgr)
	builder = builder.For(&opendepotv1alpha1.Version{}, ctrlbuilder.WithPredicates(predicate.And(predicate.GenerationChangedPredicate{}, skipResyncedCreate)))
	// Editing a ScanPolicy must re-evaluate the Versions it governs. Only spec changes
	// matter here: without this predicate the ScanPolicy status writes made by
	// ScanPolicyReconciler would bounce straight back into this controller.
	builder = builder.Watches(
		&opendepotv1alpha1.ScanPolicy{},
		handler.EnqueueRequestsFromMapFunc(r.versionsInNamespace),
		ctrlbuilder.WithPredicates(predicate.GenerationChangedPredicate{}),
	).Named(opendepotControllerName).
		WithOptions(controller.Options{MaxConcurrentReconciles: 4})

	// For(&Version{}) only enqueues the object that changed. When a provider Version
	// publishes a new schema, module Versions in the same namespace must be re-enqueued
	// so their degraded contracts can be upgraded with the newly available types.
	//
	// providerSchemaExtracted must gate this watch: without it, every update to every
	// provider Version (including no-op status touches) fans out to every degraded module
	// Version in the namespace, re-triggering contract derivation and repeatedly re-queuing
	// the same keys forever for modules that can never reach GradeFull.
	if r.AssemblyEnabled {
		providerSchemaExtracted := predicate.Funcs{
			// CreateFunc must always return false: client-go informers synthesize a
			// Create event for every pre-existing object during the initial cache sync
			// on every controller restart, not just for genuinely new objects. Since
			// ProviderSchemaRef is only ever populated by a later status Update (never
			// set at true object creation time), a genuine new extraction is always
			// observed via UpdateFunc below — treating Create as a signal here would
			// re-trigger a full fan-out for every already-extracted provider Version on
			// every restart.
			CreateFunc: func(e event.CreateEvent) bool {
				return false
			},

			UpdateFunc: func(e event.UpdateEvent) bool {
				newVersion, ok := e.ObjectNew.(*opendepotv1alpha1.Version)
				if !ok || newVersion.Spec.Type != opendepotv1alpha1.OpenDepotProvider || newVersion.Status.ProviderSchemaRef == nil {
					return false
				}

				oldVersion, ok := e.ObjectOld.(*opendepotv1alpha1.Version)
				if !ok || oldVersion.Status.ProviderSchemaRef == nil {
					return true
				}

				return oldVersion.Status.ProviderSchemaRef.ExtractedAt != newVersion.Status.ProviderSchemaRef.ExtractedAt
			},

			DeleteFunc: func(e event.DeleteEvent) bool {
				return false
			},

			GenericFunc: func(e event.GenericEvent) bool {
				return false
			},
		}

		builder = builder.Watches(
			&opendepotv1alpha1.Version{},
			handler.EnqueueRequestsFromMapFunc(r.moduleVersionsForProviderSchema),
			ctrlbuilder.WithPredicates(providerSchemaExtracted),
		)
	}

	return builder.Complete(r)
}

// moduleVersionsForProviderSchema maps a provider Version that has an extracted schema to
// every module Version in the same namespace whose contract is not already fully typed.
func (r *VersionReconciler) moduleVersionsForProviderSchema(ctx context.Context, obj client.Object) []reconcile.Request {
	providerVersion, ok := obj.(*opendepotv1alpha1.Version)
	if !ok {
		return nil
	}

	if providerVersion.Spec.Type != opendepotv1alpha1.OpenDepotProvider || providerVersion.Status.ProviderSchemaRef == nil {
		return nil
	}

	versions := &opendepotv1alpha1.VersionList{}
	if err := r.List(ctx, versions, client.InNamespace(providerVersion.Namespace)); err != nil {
		r.Log.Error(err, "failed to list versions for provider schema fan-out", "namespace", providerVersion.Namespace)
		return nil
	}

	var requests []reconcile.Request
	for i := range versions.Items {
		moduleVersion := &versions.Items[i]
		if moduleVersion.Spec.Type != opendepotv1alpha1.OpenDepotModule {
			continue
		}

		if moduleVersion.Status.ContractConfigMapRef != nil && moduleVersion.Status.ContractConfigMapRef.Grade == hclschema.GradeFull {
			continue
		}

		requests = append(requests, reconcile.Request{
			NamespacedName: k8stypes.NamespacedName{
				Namespace: moduleVersion.Namespace,
				Name:      moduleVersion.Name,
			},
		})
	}

	return requests
}

// versionsInNamespace maps a ScanPolicy event onto every Version in the same namespace.
// ScanPolicy is namespaced, so its blast radius is bounded by the namespace and a full
// list is cheap enough to avoid maintaining a selector index.
func (r *VersionReconciler) versionsInNamespace(ctx context.Context, obj client.Object) []reconcile.Request {
	versions := &opendepotv1alpha1.VersionList{}
	if err := r.List(ctx, versions, client.InNamespace(obj.GetNamespace())); err != nil {
		r.Log.Error(err, "Failed to list Versions for ScanPolicy event",
			"scanPolicy", obj.GetName(), "namespace", obj.GetNamespace())

		return nil
	}

	requests := make([]reconcile.Request, 0, len(versions.Items))
	for _, version := range versions.Items {
		requests = append(requests, reconcile.Request{
			NamespacedName: k8stypes.NamespacedName{Name: version.Name, Namespace: version.Namespace},
		})
	}

	return requests
}

// RunStorageFactory is the runtime handler for managing storage objects received by 'soi'.
func RunStorageFactory(ctx context.Context, storageInterface storage.Storage, soi *types.StorageObjectInput) error {
	switch soi.Method {
	case types.Delete:
		if err := storageInterface.DeleteObject(ctx, soi); err != nil {
			return err
		}
	case types.Get:
		if err := storageInterface.GetObjectChecksum(ctx, soi); err != nil {
			return err
		}
	case types.Put:
		if err := storageInterface.PutObject(ctx, soi); err != nil {
			return err
		}
	default:
		return fmt.Errorf("no usable method provided")
	}

	return nil
}

// InitStorageFactory prepares and initializes storage using the version's storage config.
func (r *VersionReconciler) InitStorageFactory(ctx context.Context, soi *types.StorageObjectInput) error {
	storageInterface, err := r.resolveStorageInterface(ctx, soi)
	if err != nil {
		return err
	}

	return RunStorageFactory(ctx, storageInterface, soi)
}

// resolveStorageInterface populates soi with the Version's resolved storage configuration
// and returns the backend implementation for it. Callers that need direct access to the
// backend (e.g. to stream an object's bytes rather than just its checksum) use this
// instead of InitStorageFactory.
func (r *VersionReconciler) resolveStorageInterface(ctx context.Context, soi *types.StorageObjectInput) (storage.Storage, error) {
	storageConfig, err := getVersionStorageConfig(soi.Version)
	if err != nil {
		return nil, err
	}

	soi.StorageConfig = storageConfig

	if name, nameErr := getVersionName(soi.Version); nameErr == nil {
		soi.ContainerName = name
	}

	if storageConfig.FileSystem != nil {
		return &storage.FileSystem{}, nil
	}

	if storageConfig.S3 != nil {
		amazonS3Storage := &storage.AmazonS3Storage{}
		if err := amazonS3Storage.NewClient(ctx, storageConfig.S3.Region); err != nil {
			return nil, err
		}

		return amazonS3Storage, nil
	}

	if storageConfig.AzureStorage != nil {
		azureBlobStorage := &storage.AzureBlobStorage{}
		if err := azureBlobStorage.NewClients(storageConfig.AzureStorage.SubscriptionID, storageConfig.AzureStorage.AccountUrl); err != nil {
			return nil, err
		}

		return azureBlobStorage, nil
	}

	if storageConfig.GCS != nil {
		gcsStorage := &storage.GoogleCloudStorage{}
		if err := gcsStorage.NewClient(ctx); err != nil {
			return nil, err
		}

		return gcsStorage, nil
	}

	return nil, fmt.Errorf("at least one StorageConfig backend must be configured")
}

// getVersionStorageConfig resolves storage configuration from module or provider config references.
func getVersionStorageConfig(version *opendepotv1alpha1.Version) (*opendepotv1alpha1.StorageConfig, error) {
	if version.Spec.AgentSourceRef != nil && version.Spec.AgentSourceRef.StorageConfig != nil {
		return version.Spec.AgentSourceRef.StorageConfig, nil
	}

	if version.Spec.ModuleConfigRef != nil && version.Spec.ModuleConfigRef.StorageConfig != nil {
		return version.Spec.ModuleConfigRef.StorageConfig, nil
	}

	if version.Spec.ProviderConfigRef != nil && version.Spec.ProviderConfigRef.StorageConfig != nil {
		return version.Spec.ProviderConfigRef.StorageConfig, nil
	}

	return nil, fmt.Errorf("storage config is not configured on moduleConfigRef or providerConfigRef")
}

// getVersionName resolves the logical resource name used as the storage prefix for a Version.
func getVersionName(version *opendepotv1alpha1.Version) (*string, error) {
	var storageName *string

	switch {
	case version.Spec.AgentSourceRef != nil && version.Spec.AgentSourceRef.Name != nil:
		name := strings.ToLower(version.Spec.Type) + "-" + *version.Spec.AgentSourceRef.Name
		storageName = &name
	case version.Spec.ModuleConfigRef != nil && version.Spec.ModuleConfigRef.Name != nil:
		storageName = version.Spec.ModuleConfigRef.Name
	case version.Spec.ProviderConfigRef != nil && version.Spec.ProviderConfigRef.Name != nil:
		storageName = version.Spec.ProviderConfigRef.Name
	default:
		return nil, fmt.Errorf("unable to resolve version name from moduleConfigRef or providerConfigRef")
	}

	if err := validatePathSegment("version name", *storageName); err != nil {
		return nil, err
	}

	return storageName, nil
}

// versionPathSegmentPattern matches a single path segment made of letters, digits, '.', '_' and '-'.
// Generated file names (a UUID7 followed by an extension such as .zip or .tar.gz) and Kubernetes
// object names always match.
var versionPathSegmentPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// validatePathSegment rejects a value that is not a plain path segment, so it cannot inject path
// separators or '..' segments when it is joined into a storage key.
func validatePathSegment(field, value string) error {
	if !versionPathSegmentPattern.MatchString(value) || strings.Contains(value, "..") {
		return fmt.Errorf("%s '%s' must be a plain name without path separators or '..'", field, value)
	}

	return nil
}

// validateVersionFileName rejects a fileName that is not a plain file name.
func validateVersionFileName(fileName string) error {
	return validatePathSegment("fileName", fileName)
}

// getVersionFilePath computes the object key for module/provider artifacts.
func getVersionFilePath(version *opendepotv1alpha1.Version) (*string, error) {
	storageConfig, err := getVersionStorageConfig(version)
	if err != nil {
		return nil, err
	}

	name, err := getVersionName(version)
	if err != nil {
		return nil, err
	}

	if version.Spec.FileName == nil {
		return nil, fmt.Errorf("fileName is nil for version '%s'", version.Name)
	}

	if err := validateVersionFileName(*version.Spec.FileName); err != nil {
		return nil, fmt.Errorf("invalid version '%s': %w", version.Name, err)
	}

	if storageConfig.S3 != nil && storageConfig.S3.Key != nil {
		sanitized, err := storage.RemoveTrailingSlash(storageConfig.S3.Key)
		if err != nil {
			return nil, err
		}
		filePath := fmt.Sprintf("%s/%s/%s", *sanitized, *name, *version.Spec.FileName)
		return &filePath, nil
	}

	if storageConfig.FileSystem != nil && storageConfig.FileSystem.DirectoryPath != nil {
		sanitized, err := storage.RemoveTrailingSlash(storageConfig.FileSystem.DirectoryPath)
		if err != nil {
			return nil, err
		}
		filePath := fmt.Sprintf("%s/%s/%s", *sanitized, *name, *version.Spec.FileName)
		return &filePath, nil
	}

	filePath := fmt.Sprintf("%s/%s", *name, *version.Spec.FileName)
	return &filePath, nil
}
