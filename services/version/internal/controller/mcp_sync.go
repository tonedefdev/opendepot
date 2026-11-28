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
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	"github.com/tonedefdev/opendepot/pkg/archive"
	opendepotGithub "github.com/tonedefdev/opendepot/pkg/github"
	"github.com/tonedefdev/opendepot/pkg/mcp"
	"github.com/tonedefdev/opendepot/pkg/storage/types"
	"github.com/tonedefdev/opendepot/services/version/internal/policy"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// mcpStorageNamePrefix prefixes the storage name of an MCPServer Version so its archives do not collide with other kinds.
	mcpStorageNamePrefix = "mcpserver-"
	// mcpOCIArtifactLabelKey and mcpNamespaceLabelKey are the labels the OCIArtifact controller sets on its Versions.
	mcpOCIArtifactLabelKey = "opendepot.defdev.io/ociartifact"
	mcpNamespaceLabelKey   = "opendepot.defdev.io/namespace"
	// mcpRequeueAfter is how long an Image MCPServer waits for its OCIArtifact Version to sync.
	mcpRequeueAfter = 30 * time.Second
	// mcpMaxNameRunes and mcpMaxDescriptionRunes bound the metadata written to status.mcpMetadata.
	mcpMaxNameRunes        = 255
	mcpMaxDescriptionRunes = 1024

	mcpSourceImage = "Image"
	mcpSourceNpm   = "Npm"
	mcpSourcePyPI  = "PyPI"
	mcpSourceRepo  = "Source"
)

// reconcileMCPVersion syncs an MCPServer Version. Image Versions reference a synced OCIArtifact and store no archive.
// Npm, PyPI, and Source Versions vendor a server package, scan it, sign it, and store it as an archive.
func (r *VersionReconciler) reconcileMCPVersion(ctx context.Context, req ctrl.Request, version *opendepotv1alpha1.Version) (ctrl.Result, error) {
	cfg := version.Spec.MCPServerSourceRef
	if cfg == nil {
		return ctrl.Result{}, r.persistSyncStatus(ctx, req, false, "MCPServer source reference is missing")
	}

	if version.Status.Synced && !version.Spec.ForceSync {
		return ctrl.Result{}, nil
	}

	if cfg.Source.Type == mcpSourceImage {
		return r.reconcileMCPImage(ctx, req, version, cfg)
	}

	return r.reconcileMCPArchive(ctx, req, version, cfg)
}

// reconcileMCPImage publishes an Image MCPServer Version that points at the OCIArtifact Version matching its tag.
// The OCIArtifact owns the image, so no archive is written and nothing is scanned here.
func (r *VersionReconciler) reconcileMCPImage(ctx context.Context, req ctrl.Request, version *opendepotv1alpha1.Version, cfg *opendepotv1alpha1.MCPServerSourceConfig) (ctrl.Result, error) {
	if cfg.Source.Image == nil || cfg.Source.Image.OCIArtifactRef.Name == "" {
		return r.mcpFailure(ctx, req, newMCPFinding(mcpFindingInvalid, "source.image.ociArtifactRef is required"))
	}

	artifactName := cfg.Source.Image.OCIArtifactRef.Name
	artifact := &opendepotv1alpha1.OCIArtifact{}
	if err := r.Get(ctx, k8stypes.NamespacedName{Namespace: version.Namespace, Name: artifactName}, artifact); err != nil {
		if k8serr.IsNotFound(err) {
			return ctrl.Result{RequeueAfter: mcpRequeueAfter}, r.persistSyncStatus(ctx, req, false, fmt.Sprintf("Waiting for OCIArtifact %s", artifactName))
		}

		return ctrl.Result{}, err
	}

	if artifact.Spec.OCIArtifactConfig.ArtifactType != ociArtifactImage {
		return r.mcpFailure(ctx, req, newMCPFinding(mcpFindingInvalid, "OCIArtifact %s has artifactType %s, expected %s", artifactName, artifact.Spec.OCIArtifactConfig.ArtifactType, ociArtifactImage))
	}

	digest, err := r.mcpSyncedImageDigest(ctx, version, artifactName)
	if err != nil {
		return ctrl.Result{}, err
	}

	if digest == "" {
		return ctrl.Result{RequeueAfter: mcpRequeueAfter}, r.persistSyncStatus(ctx, req, false, fmt.Sprintf("Waiting for OCIArtifact %s version %s to sync", artifactName, version.Spec.Version))
	}

	metadata := &opendepotv1alpha1.MCPMetadata{
		ServerName:    artifactName,
		Transport:     mcpTransport(cfg),
		Runnable:      true,
		PackageDigest: digest,
	}
	if cfg.Name != nil && *cfg.Name != "" {
		metadata.ServerName = *cfg.Name
	}

	if err := r.publishMCPStatus(ctx, req, func(current *opendepotv1alpha1.Version) {
		current.Status.Synced = true
		current.Status.SyncStatus = jevPublishedStatus(nil, false)
		current.Status.MCPMetadata = metadata
	}); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, r.resetMCPForceSync(ctx, req, version)
}

// mcpSyncedImageDigest returns the manifest digest of the synced OCIArtifact Version whose tag matches the
// MCPServer Version. It returns an empty string when no such Version has synced yet.
func (r *VersionReconciler) mcpSyncedImageDigest(ctx context.Context, version *opendepotv1alpha1.Version, artifactName string) (string, error) {
	versions := &opendepotv1alpha1.VersionList{}
	if err := r.List(ctx, versions,
		client.InNamespace(version.Namespace),
		client.MatchingLabels{mcpOCIArtifactLabelKey: artifactName, mcpNamespaceLabelKey: version.Namespace},
	); err != nil {
		return "", err
	}

	for i := range versions.Items {
		candidate := &versions.Items[i]
		if candidate.Spec.Type == opendepotv1alpha1.OpenDepotOCIArtifact && candidate.Spec.Version == version.Spec.Version && ociSynced(candidate) {
			return candidate.Status.OCIManifest.Digest, nil
		}
	}

	return "", nil
}

// reconcileMCPArchive vendors an Npm, PyPI, or Source MCPServer, packages it with its launch descriptor, scans and
// Jev-assesses it, signs its SHA256SUMS, and stores the archive.
func (r *VersionReconciler) reconcileMCPArchive(ctx context.Context, req ctrl.Request, version *opendepotv1alpha1.Version, cfg *opendepotv1alpha1.MCPServerSourceConfig) (ctrl.Result, error) {
	resolvedPolicy, policies, err := r.resolveScanPolicy(ctx, version)
	if err != nil {
		return ctrl.Result{}, err
	}

	scratch, err := os.MkdirTemp("", "mcp-sync-")
	if err != nil {
		return ctrl.Result{}, err
	}
	defer os.RemoveAll(scratch)

	transport := mcpTransport(cfg)
	vendored, err := r.vendorMCP(ctx, version, cfg, transport, scratch)
	if err != nil {
		return r.mcpFailure(ctx, req, err)
	}

	metadata, err := readMCPMetadata(vendored.pkgDir, cfg, version)
	if err != nil {
		return r.mcpFailure(ctx, req, err)
	}

	if err := writeMCPDescriptor(vendored); err != nil {
		return r.mcpFailure(ctx, req, err)
	}

	sourceScan, blocking, scanErr := r.runMCPScan(ctx, vendored.tree, resolvedPolicy)
	if scanErr != nil {
		if err := r.persistAgentBlock(ctx, req, nil, nil, nil, nil, fmt.Sprintf("MCP scan failed: %v", scanErr)); err != nil {
			r.Log.Error(err, "Failed to persist MCP scan failure", "version", version.Name)
		}

		return ctrl.Result{}, scanErr
	}

	if blocking != nil {
		blockErr := fmt.Errorf("blocking: %s finding %s in %s: %s", blocking.Severity, blocking.VulnerabilityID, blocking.PkgName, blocking.Title)
		if err := r.persistScanViolation(ctx, req, nil, sourceScan, blockErr); err != nil {
			r.Log.Error(err, "Failed to persist scan findings for blocked MCPServer", "version", version.Name)
		}

		if version.Spec.SourceCommit != "" {
			return ctrl.Result{}, nil
		}

		return ctrl.Result{}, blockErr
	}

	var buf bytes.Buffer
	if err := archive.WriteDeterministicTarGz(vendored.tree, &buf); err != nil {
		return ctrl.Result{}, fmt.Errorf("mcp: package vendored tree: %w", err)
	}

	archiveBytes := buf.Bytes()
	sum := sha256.Sum256(archiveBytes)
	checksumHex := hex.EncodeToString(sum[:])
	checksumBase64 := base64.StdEncoding.EncodeToString(sum[:])

	content, err := stageMCPJev(vendored, metadata, checksumHex)
	if err != nil {
		return ctrl.Result{}, err
	}

	gate, result, stop, err := r.gateMCPJev(ctx, req, version, resolvedPolicy, policies, sourceScan, content)
	if stop {
		return result, err
	}

	if version.Spec.FileName == nil {
		fileName, err := generateModuleFileName(nil)
		if err != nil {
			return ctrl.Result{}, err
		}

		version.Spec.FileName = fileName
	}

	storedPath, err := getVersionFilePath(version)
	if err != nil {
		return ctrl.Result{}, err
	}

	var prior *signedAgentArchive
	if version.Status.ShaSums != "" && version.Status.ShaSumsSignature != "" {
		prior = &signedAgentArchive{sums: version.Status.ShaSums, signature: version.Status.ShaSumsSignature, fingerprint: version.Status.SigningKeyFingerprint}
	}

	signed, signErr := signAgentArchive(archiveBytes, filepath.Base(*storedPath), prior)
	if signErr != nil {
		if err := r.persistAgentBlock(ctx, req, sourceScan, gate.assessment, gate.governance, gate.unblock, fmt.Sprintf("SigningUnavailable: %v", signErr)); err != nil {
			r.Log.Error(err, "Failed to persist signing unavailable status", "version", version.Name)
		}

		return ctrl.Result{}, signErr
	}

	soi := &types.StorageObjectInput{
		ArchiveChecksum: &checksumBase64,
		Method:          types.Put,
		FileBytes:       archiveBytes,
		FilePath:        storedPath,
		Version:         version,
	}

	storageInterface, err := r.resolveStorageInterface(ctx, soi)
	if err != nil {
		return ctrl.Result{}, err
	}

	if err := checkOCIFilesystemRoot(soi.StorageConfig, r.FilesystemMountPath); err != nil {
		return ctrl.Result{}, err
	}

	if err := RunStorageFactory(ctx, storageInterface, soi); err != nil {
		return ctrl.Result{}, fmt.Errorf("mcp: store archive: %w", err)
	}

	size := soi.BytesWritten
	if size == 0 {
		size = int64(len(archiveBytes))
	}

	if err := r.persistMCPFileName(ctx, req, version.Spec.FileName); err != nil {
		return ctrl.Result{}, err
	}

	runnable := cfg.Source.Type != mcpSourceRepo
	packageDigest := "sha256:" + checksumHex
	if err := r.publishMCPStatus(ctx, req, func(current *opendepotv1alpha1.Version) {
		current.Status.Synced = true
		current.Status.Checksum = &checksumBase64
		current.Status.ArchiveSizeBytes = &size
		current.Status.SyncStatus = jevPublishedStatus(gate.assessment, false)
		current.Status.SourceScan = sourceScan
		current.Status.ShaSums = signed.sums
		current.Status.ShaSumsSignature = signed.signature
		current.Status.SigningKeyFingerprint = signed.fingerprint
		current.Status.JevAssessment = gate.assessment
		current.Status.JevExemptionGovernance = gate.governance
		current.Status.JevUnblock = gate.unblock
		current.Status.MCPMetadata = &opendepotv1alpha1.MCPMetadata{
			ServerName:    metadata.name,
			Description:   metadata.description,
			Transport:     transport,
			Runnable:      runnable,
			Platforms:     vendored.platforms,
			PackageDigest: packageDigest,
		}
	}); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, r.resetMCPForceSync(ctx, req, version)
}

// vendorMCP vendors the server package for the source type into scratch and returns the tree to package.
func (r *VersionReconciler) vendorMCP(ctx context.Context, version *opendepotv1alpha1.Version, cfg *opendepotv1alpha1.MCPServerSourceConfig, transport, scratch string) (*mcpVendored, error) {
	switch cfg.Source.Type {
	case mcpSourceNpm:
		if cfg.Source.Npm == nil {
			return nil, newMCPFinding(mcpFindingInvalid, "source.npm is required for Npm sources")
		}

		return r.vendorNPM(ctx, cfg.Source.Npm, version, transport, cfg.Entrypoint, scratch)
	case mcpSourcePyPI:
		if cfg.Source.PyPI == nil {
			return nil, newMCPFinding(mcpFindingInvalid, "source.pypi is required for PyPI sources")
		}

		return r.vendorPyPI(ctx, cfg.Source.PyPI, version, transport, cfg.Entrypoint, scratch)
	case mcpSourceRepo:
		return r.vendorMCPSource(ctx, version, cfg, transport, scratch)
	}

	return nil, newMCPFinding(mcpFindingInvalid, "unsupported source type %q", cfg.Source.Type)
}

// vendorMCPSource archives the repository path of a Source MCPServer. The path is confined to the repository, so
// a traversal or absolute path is rejected before anything is downloaded.
func (r *VersionReconciler) vendorMCPSource(ctx context.Context, version *opendepotv1alpha1.Version, cfg *opendepotv1alpha1.MCPServerSourceConfig, transport, scratch string) (*mcpVendored, error) {
	repo := cfg.Source.Source
	if repo == nil {
		return nil, newMCPFinding(mcpFindingInvalid, "source.source is required for Source sources")
	}

	if err := validateMCPSourcePath(repo.Path); err != nil {
		return nil, newMCPFinding(mcpFindingInvalid, "%v", err)
	}

	shadow := mcpAgentShadow(version)
	var refs []string
	if version.Spec.SourceCommit != "" {
		if err := opendepotGithub.ValidateCommitSHA(version.Spec.SourceCommit); err != nil {
			return nil, err
		}

		refs = []string{version.Spec.SourceCommit}
	} else {
		tagPrefix := ""
		if repo.TagPrefix != nil {
			tagPrefix = *repo.TagPrefix
		}

		refs = opendepotGithub.TagCandidates(tagPrefix, version.Spec.Version)
	}

	tarball, _, err := r.downloadAgentArchive(ctx, shadow, refs)
	if err != nil {
		return nil, err
	}

	extracted := filepath.Join(scratch, "extract")
	if err := os.MkdirAll(extracted, 0o755); err != nil {
		return nil, err
	}

	subtree := strings.Trim(filepath.ToSlash(filepath.Clean("/"+repo.Path)), "/")
	if err := archive.ExtractSubtreeToDir(tarball, extracted, subtree); err != nil {
		return nil, fmt.Errorf("mcp: extract source path: %w", err)
	}

	// GitHub tarballs contain a single top-level directory named after the repository and commit.
	rootDir := extracted
	entries, err := os.ReadDir(extracted)
	if err != nil {
		return nil, err
	}

	if len(entries) == 1 && entries[0].IsDir() {
		rootDir = filepath.Join(extracted, entries[0].Name())
	}

	// Cleaning the path relative to a rooted path prevents traversal outside of the repository.
	tree := filepath.Join(rootDir, filepath.Clean("/"+repo.Path))
	info, err := os.Stat(tree)
	if err != nil || !info.IsDir() {
		return nil, newMCPFinding(mcpFindingInvalid, "source path %q is not a directory in the repository", repo.Path)
	}

	if err := rejectMCPSymlinks(tree); err != nil {
		return nil, err
	}

	vendored := &mcpVendored{tree: tree, pkgDir: tree}
	if cfg.Entrypoint == nil {
		return vendored, nil
	}

	runtime, err := mcpSourceRuntime(cfg.Entrypoint.Command)
	if err != nil {
		return nil, err
	}

	vendored.descriptor = &mcp.LaunchDescriptor{
		Command:   cfg.Entrypoint.Command,
		Args:      cfg.Entrypoint.Args,
		Transport: transport,
		Runtime:   runtime,
	}

	return vendored, nil
}

// mcpSourceRuntime maps an entrypoint command to the runtime that launches it. Only node and python entrypoints
// can run from a vendored Source tree.
func mcpSourceRuntime(command string) (string, error) {
	switch filepath.Base(command) {
	case "node":
		return mcp.RuntimeNode, nil
	case "python", "python3":
		return mcp.RuntimePython, nil
	}

	return "", newMCPFinding(mcpFindingInvalid, "source entrypoint command %q must be node or python", command)
}

// writeMCPDescriptor validates the launch descriptor and writes it to the root of the vendored tree. A tree
// without a descriptor is not runnable and writes nothing.
func writeMCPDescriptor(vendored *mcpVendored) error {
	if vendored.descriptor == nil {
		return nil
	}

	if err := vendored.descriptor.Validate(); err != nil {
		return newMCPFinding(mcpFindingInvalid, "launch descriptor: %v", err)
	}

	data, err := vendored.descriptor.Marshal()
	if err != nil {
		return fmt.Errorf("mcp: marshal launch descriptor: %w", err)
	}

	return os.WriteFile(filepath.Join(vendored.tree, mcp.LaunchDescriptorFile), data, 0o644)
}

// runMCPScan runs an offline Trivy filesystem scan over the vendored tree and applies the ScanPolicy. It returns
// the scan record and the finding that blocks the Version, if any.
func (r *VersionReconciler) runMCPScan(ctx context.Context, tree string, resolved policy.Resolved) (*opendepotv1alpha1.SourceScan, *opendepotv1alpha1.SecurityFinding, error) {
	args := []string{"fs", "--format", "json", "--scanners", "vuln,secret"}
	if r.ScanOffline {
		args = append(args, "--offline-scan", "--skip-db-update")
	}

	if r.TrivyCacheDir != "" {
		args = append(args, "--cache-dir", r.TrivyCacheDir)
	}

	args = append(args, "--quiet", tree)

	// Serialise Trivy invocations: each process loads the full vulnerability DB.
	if r.scanSem != nil {
		select {
		case r.scanSem <- struct{}{}:
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		}
	}

	output, err := runTrivy(ctx, args...)
	if r.scanSem != nil {
		<-r.scanSem
	}

	if err != nil {
		return nil, nil, fmt.Errorf("mcp scan failed: %w", err)
	}

	if len(output) == 0 {
		return nil, nil, errors.New("mcp scan failed: trivy produced no output; verify the Trivy vulnerability DB is populated in the offline cache")
	}

	findings, err := parseTrivyReport(output, nil)
	if err != nil {
		return nil, nil, err
	}

	secrets, _, err := parseTrivySecrets(output)
	if err != nil {
		return nil, nil, err
	}

	findings = append(findings, secrets...)
	annotated, blocking := policy.Apply(resolved, findings, policy.ScanTypeMCP)
	sourceScan := &opendepotv1alpha1.SourceScan{
		ScannedAt: time.Now().UTC().Format(time.RFC3339),
		Findings:  annotated,
	}

	return sourceScan, blocking, nil
}

// mcpFailure records a sync failure. A terminal finding blocks the Version with its finding code and is not retried.
// Any other error leaves the Version unsynced and returns the error for a backoff retry.
func (r *VersionReconciler) mcpFailure(ctx context.Context, req ctrl.Request, err error) (ctrl.Result, error) {
	var finding *mcpFindingError
	if errors.As(err, &finding) {
		if persistErr := r.persistAgentBlock(ctx, req, nil, nil, nil, nil, err.Error()); persistErr != nil {
			return ctrl.Result{}, persistErr
		}

		return ctrl.Result{}, nil
	}

	if persistErr := r.persistSyncStatus(ctx, req, false, fmt.Sprintf("MCP sync failed: %v", err)); persistErr != nil {
		r.Log.Error(persistErr, "Failed to persist MCP sync failure", "name", req.Name)
	}

	return ctrl.Result{}, err
}

// persistMCPFileName stores the archive file name on the spec before the status is published. The spec write
// triggers a reconcile, which must find the Version already synced by the time it runs.
func (r *VersionReconciler) persistMCPFileName(ctx context.Context, req ctrl.Request, fileName *string) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		current := &opendepotv1alpha1.Version{}
		if err := r.Get(ctx, req.NamespacedName, current); err != nil {
			return err
		}

		current.Spec.FileName = fileName
		return r.Update(ctx, current, &client.UpdateOptions{FieldManager: opendepotControllerName})
	})
}

// publishMCPStatus applies mutate to the latest Version and writes its status.
func (r *VersionReconciler) publishMCPStatus(ctx context.Context, req ctrl.Request, mutate func(*opendepotv1alpha1.Version)) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		current := &opendepotv1alpha1.Version{}
		if err := r.Get(ctx, req.NamespacedName, current); err != nil {
			return err
		}

		mutate(current)
		return r.Status().Update(ctx, current, &client.SubResourceUpdateOptions{
			UpdateOptions: client.UpdateOptions{FieldManager: opendepotControllerName},
		})
	})
}

// resetMCPForceSync clears spec.forceSync once a sync has published.
func (r *VersionReconciler) resetMCPForceSync(ctx context.Context, req ctrl.Request, version *opendepotv1alpha1.Version) error {
	if !version.Spec.ForceSync {
		return nil
	}

	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		current := &opendepotv1alpha1.Version{}
		if err := r.Get(ctx, req.NamespacedName, current); err != nil {
			return err
		}

		current.Spec.ForceSync = false
		return r.Update(ctx, current, &client.UpdateOptions{FieldManager: opendepotControllerName})
	})
}

// mcpTransport returns the configured transport, defaulting to stdio.
func mcpTransport(cfg *opendepotv1alpha1.MCPServerSourceConfig) string {
	if cfg.Transport != "" {
		return cfg.Transport
	}

	return mcp.TransportStdio
}

// truncateMCPRunes shortens value to at most limit runes.
func truncateMCPRunes(value string, limit int) string {
	if utf8.RuneCountInString(value) <= limit {
		return value
	}

	return string([]rune(value)[:limit])
}
