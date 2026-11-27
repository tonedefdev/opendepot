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
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"github.com/google/go-github/v81/github"
	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	corev1 "k8s.io/api/core/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	opendepotGithub "github.com/tonedefdev/opendepot/pkg/github"
	"github.com/tonedefdev/opendepot/pkg/oci"
	"github.com/tonedefdev/opendepot/pkg/storage"
	"github.com/tonedefdev/opendepot/pkg/storage/types"
	"github.com/tonedefdev/opendepot/services/version/internal/policy"
)

const (
	// ociArtifactImage, ociArtifactHelm, and ociArtifactGeneric are the artifactType values of an OCIArtifact.
	ociArtifactImage   = "Image"
	ociArtifactHelm    = "Helm"
	ociArtifactGeneric = "Generic"
	// ociDriftRequeueInterval is how often a synced mirror Version checks its upstream tag for drift.
	ociDriftRequeueInterval = time.Hour
	// maxOCIReferrers bounds the number of OCI 1.1 referrers that are mirrored for one Version.
	maxOCIReferrers = 64
	// maxOCIPlatforms bounds the platforms that one Image Version serves. It matches the CRD limit on
	// status.ociManifest.platforms.
	maxOCIPlatforms = 16
	// maxOCIAssetBytes bounds a GitHub release asset before it is packaged.
	maxOCIAssetBytes int64 = 1 << 30
	// maxHelmChartBytes bounds the decompressed size of a Helm chart archive.
	maxHelmChartBytes int64 = 64 << 20
)

var (
	// errOCIInvalid is returned when upstream content or the spec cannot produce a valid artifact. Retrying does not help.
	errOCIInvalid = errors.New("invalid OCI artifact")
	// errHelmChartTooLarge is returned when a chart archive decompresses to more than the allowed size.
	errHelmChartTooLarge = errors.New("helm chart archive exceeds the maximum decompressed size")
	// errOCIAssetTooLarge is returned when a GitHub release asset is larger than maxOCIAssetBytes.
	errOCIAssetTooLarge = errors.New("release asset exceeds the maximum size")
)

// ociRegistry is the upstream surface that the OCI sync reads. *oci.Upstream implements it. Tests substitute a fake.
type ociRegistry interface {
	Resolve(ctx context.Context, repository, reference string) (ocispec.Descriptor, error)
	FetchManifest(ctx context.Context, repository, reference string, platforms []string) (*oci.ResolvedManifest, error)
	FetchBlob(ctx context.Context, repository string, desc ocispec.Descriptor) (io.ReadCloser, error)
	Referrers(ctx context.Context, repository string, desc ocispec.Descriptor, artifactType string) ([]ocispec.Descriptor, error)
}

var _ ociRegistry = (*oci.Upstream)(nil)

// ociBlobStore reads and writes the blobs and manifests of one OCIArtifact Version through a pkg/oci.Store.
type ociBlobStore struct {
	store     *oci.Store
	namespace string
	repo      string
}

func (s ociBlobStore) putBlob(ctx context.Context, desc ocispec.Descriptor, content io.ReadSeeker) error {
	return s.store.PutBlob(ctx, s.namespace, s.repo, desc.Digest, content)
}

func (s ociBlobStore) putManifest(ctx context.Context, content []byte) error {
	_, err := s.store.PutManifest(ctx, s.namespace, s.repo, content)
	return err
}

func (s ociBlobStore) getBlob(ctx context.Context, d digest.Digest) (io.ReadCloser, error) {
	return s.store.GetBlob(ctx, s.namespace, s.repo, d)
}

// syncOCIVersion reconciles an OCIArtifact Version. A mirror is checked against its upstream tag on every reconcile and
// is synced once per digest it serves. A GitHub artifact is synced once.
func (r *VersionReconciler) syncOCIVersion(
	ctx context.Context,
	req ctrl.Request,
	version *opendepotv1alpha1.Version,
) (ctrl.Result, error) {
	config := version.Spec.OCIArtifactConfigRef
	if config == nil {
		return r.terminalOCI(ctx, req, "OCI artifact config is not set on the version")
	}

	if config.StorageConfig == nil {
		return r.terminalOCI(ctx, req, "storageConfig is not set on the OCI artifact config")
	}

	switch {
	case config.Mirror != nil && config.Github == nil:
		return r.syncOCIMirror(ctx, req, version, config)
	case config.Github != nil && config.Mirror == nil:
		return r.syncOCIGithub(ctx, req, version, config)
	default:
		return r.terminalOCI(ctx, req, "exactly one of mirror or github is required")
	}
}

// syncOCIMirror resolves the upstream tag. An unchanged digest is recorded as in sync. A moved tag is recorded as drift
// when immutable is set, and it is re-synced only when immutable is false. ForceSync re-syncs an unchanged digest.
func (r *VersionReconciler) syncOCIMirror(
	ctx context.Context,
	req ctrl.Request,
	version *opendepotv1alpha1.Version,
	config *opendepotv1alpha1.OCIArtifactConfig,
) (ctrl.Result, error) {
	src := config.Mirror
	repository := src.Registry + "/" + src.Repository

	registry, err := r.newOCIRegistry(ctx, version.Namespace, src.CredentialsSecretRef)
	if err != nil {
		return r.failOCISync(ctx, req, version, "configure upstream", err)
	}

	upstream, err := registry.Resolve(ctx, repository, version.Spec.Version)
	if err != nil {
		return r.failOCISync(ctx, req, version, "resolve tag", err)
	}

	if ociSynced(version) {
		served := version.Status.OCIManifest.Digest
		if upstream.Digest.String() == served && !version.Spec.ForceSync {
			return r.recordOCIUpstream(ctx, req, version, upstream.Digest.String())
		}

		if upstream.Digest.String() != served && ociImmutable(config) {
			return r.recordOCIUpstream(ctx, req, version, upstream.Digest.String())
		}
	}

	return r.mirrorOCIArtifact(ctx, req, version, config, registry, repository, upstream.Digest)
}

// recordOCIUpstream records the digest that the upstream tag points to and whether it differs from the served digest.
// The served digest is never changed here.
func (r *VersionReconciler) recordOCIUpstream(
	ctx context.Context,
	req ctrl.Request,
	version *opendepotv1alpha1.Version,
	upstream string,
) (ctrl.Result, error) {
	drift := upstream != version.Status.OCIManifest.Digest
	if version.Status.OCIManifest.UpstreamDigest != upstream || version.Status.OCIManifest.TagDrift != drift {
		syncStatus := "Synced"
		if drift {
			syncStatus = "Synced; the upstream tag has moved and is not re-synced because immutable is set"
		}

		err := r.persistOCIStatus(ctx, req, true, syncStatus, func(current *opendepotv1alpha1.Version) {
			if current.Status.OCIManifest == nil {
				return
			}

			current.Status.OCIManifest.UpstreamDigest = upstream
			current.Status.OCIManifest.TagDrift = drift
		})
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{RequeueAfter: ociDriftRequeueInterval}, nil
}

// mirrorOCIArtifact pulls the upstream digest into storage, verifies its signature when configured, scans it, and
// records it as synced.
func (r *VersionReconciler) mirrorOCIArtifact(
	ctx context.Context,
	req ctrl.Request,
	version *opendepotv1alpha1.Version,
	config *opendepotv1alpha1.OCIArtifactConfig,
	registry ociRegistry,
	repository string,
	upstream digest.Digest,
) (ctrl.Result, error) {
	src := config.Mirror

	var signatureVerified *bool
	if src.Verify != nil {
		key, err := r.cosignPublicKey(ctx, version.Namespace, src.Verify.CosignPublicKeySecretRef)
		if err != nil {
			return r.failOCISync(ctx, req, version, "read cosign public key", err)
		}

		verifyErr := verifyCosignSignature(ctx, registry, repository, upstream, key)
		verified := verifyErr == nil
		signatureVerified = &verified
		if verifyErr != nil && src.Verify.Required {
			return r.blockOCISync(ctx, req, fmt.Sprintf("Signature verification failed: %v", verifyErr))
		}
	}

	root, err := registry.FetchManifest(ctx, repository, upstream.String(), config.Platforms)
	if err != nil {
		return r.failOCISync(ctx, req, version, "fetch manifest", err)
	}

	platforms := ociPlatforms(root.Manifests)
	if len(platforms) > maxOCIPlatforms {
		return r.terminalOCI(ctx, req, fmt.Sprintf("Failed to sync OCI artifact: the index serves %d platforms and the limit is %d; set platforms", len(platforms), maxOCIPlatforms))
	}

	blobs, err := r.ociBlobStoreFor(ctx, version)
	if err != nil {
		return r.failOCISync(ctx, req, version, "open storage", err)
	}

	if err := mirrorOCIContent(ctx, registry, repository, blobs, root); err != nil {
		return r.failOCISync(ctx, req, version, "mirror content", err)
	}

	artifactType := config.ArtifactType
	input := ociScanInput{root: root, blobs: blobs}

	var helm *opendepotv1alpha1.HelmChartMetadata
	if artifactType != ociArtifactImage {
		primary, err := spoolOCIPrimary(ctx, artifactType, root, blobs)
		if errors.Is(err, errOCIInvalid) {
			return r.terminalOCI(ctx, req, fmt.Sprintf("Failed to sync OCI artifact: %v", err))
		}

		if err != nil {
			return r.failOCISync(ctx, req, version, "read artifact", err)
		}
		defer removeTempFile(primary)

		input.filePath = primary.Name()
		if artifactType == ociArtifactHelm {
			helm, err = chartMetadataFromFile(primary)
			if err != nil {
				return r.terminalOCI(ctx, req, fmt.Sprintf("Invalid Helm chart: %v", err))
			}
		}
	}

	manifest := &opendepotv1alpha1.OCIManifestStatus{
		Digest:            root.Descriptor.Digest.String(),
		MediaType:         root.Descriptor.MediaType,
		ArtifactType:      ociRootArtifactType(root),
		UpstreamDigest:    upstream.String(),
		Platforms:         platforms,
		TotalSizeBytes:    ociBlobSize(root.Manifests),
		SignatureVerified: signatureVerified,
	}

	if err := r.finishOCISync(ctx, req, version, artifactType, manifest, helm, input); err != nil {
		return ctrl.Result{}, err
	}

	if version.Spec.ForceSync {
		if err := r.clearOCIForceSync(ctx, req); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{RequeueAfter: ociDriftRequeueInterval}, nil
}

// syncOCIGithub downloads the release asset of a GitHub source, packages it, and records it as synced. The asset is
// read once per tag, so the Version is not re-synced unless ForceSync is set.
func (r *VersionReconciler) syncOCIGithub(
	ctx context.Context,
	req ctrl.Request,
	version *opendepotv1alpha1.Version,
	config *opendepotv1alpha1.OCIArtifactConfig,
) (ctrl.Result, error) {
	if ociSynced(version) && !version.Spec.ForceSync {
		return ctrl.Result{}, nil
	}

	if config.ArtifactType == ociArtifactImage {
		return r.terminalOCI(ctx, req, "images can only be mirrored")
	}

	src := config.Github
	repoName, err := ociGithubRepoName(src)
	if err != nil {
		return r.terminalOCI(ctx, req, fmt.Sprintf("Invalid GitHub source: %v", err))
	}

	tag := version.Spec.Version
	if src.TagPrefix != nil {
		tag = *src.TagPrefix + tag
	}

	githubClient, err := r.githubClientForOCI(ctx, version, config)
	if err != nil {
		return r.failOCISync(ctx, req, version, "create GitHub client", err)
	}

	release, _, err := githubClient.Repositories.GetReleaseByTag(ctx, src.RepoOwner, repoName, tag)
	if err != nil {
		return r.failOCISync(ctx, req, version, "read release", err)
	}

	matches, err := matchReleaseAssets(release, src.AssetPattern)
	if errors.Is(err, errOCIInvalid) {
		return r.terminalOCI(ctx, req, fmt.Sprintf("Failed to sync OCI artifact: %v", err))
	}

	if err != nil {
		return r.failOCISync(ctx, req, version, "match release asset", err)
	}

	if len(matches) != 1 {
		return r.terminalOCI(ctx, req, fmt.Sprintf("Failed to sync OCI artifact: expected one release asset matching %q in tag %s, found %d", src.AssetPattern, tag, len(matches)))
	}

	asset, err := downloadReleaseAsset(ctx, githubClient, src.RepoOwner, repoName, matches[0])
	if errors.Is(err, errOCIAssetTooLarge) {
		return r.terminalOCI(ctx, req, fmt.Sprintf("Failed to sync OCI artifact: %v", err))
	}

	if err != nil {
		return r.failOCISync(ctx, req, version, "download release asset", err)
	}
	defer removeTempFile(asset)

	blobs, err := r.ociBlobStoreFor(ctx, version)
	if err != nil {
		return r.failOCISync(ctx, req, version, "open storage", err)
	}

	var helm *opendepotv1alpha1.HelmChartMetadata
	var packaged *oci.Packaged
	switch config.ArtifactType {
	case ociArtifactHelm:
		var prov *os.File
		if provAsset := findReleaseAsset(release, matches[0].GetName()+".prov"); provAsset != nil {
			prov, err = downloadReleaseAsset(ctx, githubClient, src.RepoOwner, repoName, provAsset)
			if err != nil {
				return r.failOCISync(ctx, req, version, "download provenance", err)
			}
			defer removeTempFile(prov)
		}

		helm, err = chartMetadataFromFile(asset)
		if err != nil {
			return r.terminalOCI(ctx, req, fmt.Sprintf("Invalid Helm chart: %v", err))
		}

		var provReader io.ReadSeeker
		if prov != nil {
			provReader = prov
		}

		packaged, err = oci.PackageHelmChart(asset, provReader)
	case ociArtifactGeneric:
		if src.MediaType == nil || src.ArtifactMediaType == nil {
			return r.terminalOCI(ctx, req, "mediaType and artifactMediaType are required for a Generic GitHub source")
		}

		packaged, err = oci.PackageGeneric(asset, oci.GenericOptions{
			MediaType:    *src.MediaType,
			ArtifactType: *src.ArtifactMediaType,
			Title:        matches[0].GetName(),
		})
	default:
		return r.terminalOCI(ctx, req, fmt.Sprintf("artifactType %q cannot be built from a GitHub source", config.ArtifactType))
	}

	if err != nil {
		return r.terminalOCI(ctx, req, fmt.Sprintf("Failed to package OCI artifact: %v", err))
	}

	manifest, err := storePackagedOCI(ctx, blobs, packaged)
	if err != nil {
		return r.failOCISync(ctx, req, version, "store artifact", err)
	}

	input := ociScanInput{blobs: blobs, filePath: asset.Name()}
	if err := r.finishOCISync(ctx, req, version, config.ArtifactType, manifest, helm, input); err != nil {
		return ctrl.Result{}, err
	}

	if version.Spec.ForceSync {
		if err := r.clearOCIForceSync(ctx, req); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

// finishOCISync runs the Trivy scan and the ScanPolicy gate when scanning is enabled, then records the synced state. A
// blocking finding leaves the Version unsynced and returns an error, so the Version is retried with backoff.
func (r *VersionReconciler) finishOCISync(
	ctx context.Context,
	req ctrl.Request,
	version *opendepotv1alpha1.Version,
	artifactType string,
	manifest *opendepotv1alpha1.OCIManifestStatus,
	helm *opendepotv1alpha1.HelmChartMetadata,
	input ociScanInput,
) error {
	var scan *opendepotv1alpha1.SourceScan
	if r.ScanningEnabled {
		resolved, _, err := r.resolveScanPolicy(ctx, version)
		if err != nil {
			return fmt.Errorf("resolve scan policy: %w", err)
		}

		findings, err := r.scanOCIArtifact(ctx, artifactType, input)
		if err != nil {
			msg := fmt.Sprintf("OCI scan failed: %v", err)
			if perr := r.persistOCIStatus(ctx, req, false, msg, func(current *opendepotv1alpha1.Version) {
				current.Status.OCIManifest = manifest
			}); perr != nil {
				return perr
			}

			return err
		}

		annotated, blocking := policy.Apply(resolved, findings, ociScanType)
		scan = &opendepotv1alpha1.SourceScan{
			ScannedAt: time.Now().UTC().Format(time.RFC3339),
			Findings:  annotated,
		}

		if blocking != nil {
			msg := fmt.Sprintf("Scan policy violation: blocking: %s finding %s in OCI artifact (%s)", blocking.Severity, blocking.VulnerabilityID, blocking.PkgName)
			if err := r.persistOCIStatus(ctx, req, false, msg, func(current *opendepotv1alpha1.Version) {
				current.Status.OCIManifest = manifest
				current.Status.OCIScan = scan
			}); err != nil {
				return err
			}

			return errors.New(msg)
		}
	}

	size := manifest.TotalSizeBytes
	return r.persistOCIStatus(ctx, req, true, "Synced", func(current *opendepotv1alpha1.Version) {
		current.Status.OCIManifest = manifest
		current.Status.HelmChart = helm
		current.Status.OCIScan = scan
		current.Status.ArchiveSizeBytes = &size
	})
}

// persistOCIStatus writes Synced and SyncStatus on the latest copy of the Version, applies update, and writes the status.
func (r *VersionReconciler) persistOCIStatus(
	ctx context.Context,
	req ctrl.Request,
	synced bool,
	syncStatus string,
	update func(current *opendepotv1alpha1.Version),
) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		current := &opendepotv1alpha1.Version{}
		if err := r.Get(ctx, req.NamespacedName, current); err != nil {
			return err
		}

		current.Status.Synced = synced
		current.Status.SyncStatus = syncStatus
		if update != nil {
			update(current)
		}

		return r.Status().Update(ctx, current, &client.SubResourceUpdateOptions{
			UpdateOptions: client.UpdateOptions{FieldManager: opendepotControllerName},
		})
	})
}

// terminalOCI records a configuration error that retrying cannot fix. The Version stays unsynced and is not requeued.
func (r *VersionReconciler) terminalOCI(ctx context.Context, req ctrl.Request, msg string) (ctrl.Result, error) {
	if err := r.persistOCIStatus(ctx, req, false, msg, nil); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// blockOCISync records a block that is retried with backoff, such as a missing required signature. The error is returned.
func (r *VersionReconciler) blockOCISync(ctx context.Context, req ctrl.Request, msg string) (ctrl.Result, error) {
	if err := r.persistOCIStatus(ctx, req, false, msg, nil); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, errors.New(msg)
}

// failOCISync records a sync failure. An allow-list denial is terminal. Other failures keep the synced state and return
// the error, so the Version is retried with backoff.
func (r *VersionReconciler) failOCISync(
	ctx context.Context,
	req ctrl.Request,
	version *opendepotv1alpha1.Version,
	action string,
	err error,
) (ctrl.Result, error) {
	msg := fmt.Sprintf("Failed to sync OCI artifact (%s): %v", action, err)
	if errors.Is(err, oci.ErrUpstreamDenied) {
		return r.terminalOCI(ctx, req, msg)
	}

	if perr := r.persistOCIStatus(ctx, req, version.Status.Synced, msg, nil); perr != nil {
		r.Log.Error(perr, "Failed to persist OCI sync status", "version", version.Name)
	}

	return ctrl.Result{}, fmt.Errorf("oci sync %s: %w", action, err)
}

// clearOCIForceSync resets forceSync once a sync has completed.
func (r *VersionReconciler) clearOCIForceSync(ctx context.Context, req ctrl.Request) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		current := &opendepotv1alpha1.Version{}
		if err := r.Get(ctx, req.NamespacedName, current); err != nil {
			return err
		}

		if !current.Spec.ForceSync {
			return nil
		}

		current.Spec.ForceSync = false
		return r.Update(ctx, current, &client.UpdateOptions{FieldManager: opendepotControllerName})
	})
}

// newOCIRegistry builds the upstream client for a mirror. pkg/oci enforces the allow-list on every request and parses the
// dockerconfigjson credentials in memory.
func (r *VersionReconciler) newOCIRegistry(
	ctx context.Context,
	namespace string,
	ref *corev1.LocalObjectReference,
) (ociRegistry, error) {
	var creds []byte
	if ref != nil {
		value, err := r.readOCISecretValue(ctx, namespace, ref.Name, corev1.DockerConfigJsonKey, corev1.SecretTypeDockerConfigJson)
		if err != nil {
			return nil, err
		}

		creds = value
	}

	if r.ociRegistryFor != nil {
		return r.ociRegistryFor(r.OCIAllowedRegistries, creds)
	}

	upstream, err := oci.NewUpstream(r.OCIAllowedRegistries, creds)
	if err != nil {
		return nil, err
	}

	return upstream, nil
}

// readOCISecretValue returns one key of a Secret in namespace. When secretType is set, the Secret must have that type.
// The value is returned to the caller only and is never logged.
func (r *VersionReconciler) readOCISecretValue(
	ctx context.Context,
	namespace, name, key string,
	secretType corev1.SecretType,
) ([]byte, error) {
	var reader client.Reader = r.Client
	if r.APIReader != nil {
		reader = r.APIReader
	}

	secret := &corev1.Secret{}
	if err := reader.Get(ctx, k8stypes.NamespacedName{Namespace: namespace, Name: name}, secret); err != nil {
		return nil, fmt.Errorf("read secret %s: %w", name, err)
	}

	if secretType != "" && secret.Type != secretType {
		return nil, fmt.Errorf("secret %s must have type %s", name, secretType)
	}

	value, ok := secret.Data[key]
	if !ok || len(value) == 0 {
		return nil, fmt.Errorf("secret %s has no value for key %s", name, key)
	}

	return value, nil
}

// githubClientForOCI returns the GitHub client for a GitHub source. The default is the controller's GitHub client helper.
func (r *VersionReconciler) githubClientForOCI(
	ctx context.Context,
	version *opendepotv1alpha1.Version,
	config *opendepotv1alpha1.OCIArtifactConfig,
) (*github.Client, error) {
	if r.ociGithubClientFor != nil {
		return r.ociGithubClientFor(ctx, version, config)
	}

	useAuth := config.GithubClientConfig != nil && config.GithubClientConfig.UseAuthenticatedClient
	var githubConfig *opendepotGithub.GithubClientConfig
	if useAuth {
		cfg, err := opendepotGithub.GetGithubApplicationSecret(ctx, r.APIReader, version.Namespace)
		if err != nil {
			return nil, err
		}

		githubConfig = cfg
	}

	return opendepotGithub.CreateGithubClient(ctx, useAuth, githubConfig)
}

// ociBlobStoreFor returns the blob store of a Version. It uses the storage backend of the OCI config and keeps the blobs
// under the Version's namespace and name.
func (r *VersionReconciler) ociBlobStoreFor(ctx context.Context, version *opendepotv1alpha1.Version) (ociBlobStore, error) {
	soi := &types.StorageObjectInput{Version: version}
	backend, err := r.resolveStorageInterface(ctx, soi)
	if err != nil {
		return ociBlobStore{}, err
	}

	name := version.Name
	store, err := oci.NewStore(ociStorageBackend{
		Storage:       backend,
		storageConfig: soi.StorageConfig,
		containerName: &name,
		version:       version,
	}, ociStorageRoot(soi.StorageConfig))
	if err != nil {
		return ociBlobStore{}, err
	}

	return ociBlobStore{store: store, namespace: version.Namespace, repo: version.Name}, nil
}

// ociStorageRoot returns the key prefix under which the OCI store writes. Object storage uses its key prefix, and the
// filesystem uses its directory path. The other backends have no prefix.
func ociStorageRoot(cfg *opendepotv1alpha1.StorageConfig) string {
	switch {
	case cfg.S3 != nil && cfg.S3.Key != nil:
		return strings.TrimRight(*cfg.S3.Key, "/")
	case cfg.FileSystem != nil && cfg.FileSystem.DirectoryPath != nil:
		return strings.TrimRight(*cfg.FileSystem.DirectoryPath, "/\\")
	default:
		return ""
	}
}

// ociStorageBackend adds the storage settings of a Version to every call that the OCI store makes to a storage backend.
type ociStorageBackend struct {
	storage.Storage
	storageConfig *opendepotv1alpha1.StorageConfig
	containerName *string
	version       *opendepotv1alpha1.Version
}

func (b ociStorageBackend) configure(soi *types.StorageObjectInput) {
	soi.StorageConfig = b.storageConfig
	soi.ContainerName = b.containerName
	soi.Version = b.version
}

func (b ociStorageBackend) GetObject(ctx context.Context, soi *types.StorageObjectInput) (io.Reader, error) {
	b.configure(soi)
	return b.Storage.GetObject(ctx, soi)
}

func (b ociStorageBackend) GetObjectChecksum(ctx context.Context, soi *types.StorageObjectInput) error {
	b.configure(soi)
	return b.Storage.GetObjectChecksum(ctx, soi)
}

func (b ociStorageBackend) PutObject(ctx context.Context, soi *types.StorageObjectInput) error {
	b.configure(soi)
	return b.Storage.PutObject(ctx, soi)
}

func (b ociStorageBackend) DeleteObject(ctx context.Context, soi *types.StorageObjectInput) error {
	b.configure(soi)
	return b.Storage.DeleteObject(ctx, soi)
}

// ociSynced reports whether an OCIArtifact Version has synced a digest that it still serves.
func ociSynced(version *opendepotv1alpha1.Version) bool {
	return version.Status.Synced && version.Status.OCIManifest != nil && version.Status.OCIManifest.Digest != ""
}

// ociImmutable reports whether an OCIArtifact keeps the digest it synced. It is true when immutable is unset.
func ociImmutable(config *opendepotv1alpha1.OCIArtifactConfig) bool {
	return config.Immutable == nil || *config.Immutable
}

// manifestBlobs returns the config and layer descriptors of an image manifest.
func manifestBlobs(m ocispec.Manifest) []ocispec.Descriptor {
	return append([]ocispec.Descriptor{m.Config}, m.Layers...)
}

// ociRootArtifactType returns the artifactType declared by the root index or manifest.
func ociRootArtifactType(root *oci.ResolvedManifest) string {
	if root.Descriptor.MediaType == ocispec.MediaTypeImageIndex {
		var index ocispec.Index
		if err := json.Unmarshal(root.Content, &index); err != nil {
			return ""
		}

		return index.ArtifactType
	}

	if len(root.Manifests) == 1 {
		return root.Manifests[0].Manifest.ArtifactType
	}

	return ""
}

// ociPlatforms returns the os/arch[/variant] platform of each selected index entry.
func ociPlatforms(manifests []oci.PlatformManifest) []string {
	var platforms []string
	for _, pm := range manifests {
		p := pm.Descriptor.Platform
		if p == nil {
			continue
		}

		value := p.OS + "/" + p.Architecture
		if p.Variant != "" {
			value += "/" + p.Variant
		}

		platforms = append(platforms, value)
	}

	return platforms
}

// ociBlobSize returns the total size of the distinct config and layer blobs of the selected platform manifests.
func ociBlobSize(manifests []oci.PlatformManifest) int64 {
	seen := map[digest.Digest]bool{}
	var total int64
	for _, pm := range manifests {
		for _, desc := range manifestBlobs(pm.Manifest) {
			if seen[desc.Digest] {
				continue
			}

			seen[desc.Digest] = true
			total += desc.Size
		}
	}

	return total
}

// mirrorOCIContent copies the root manifest or index, the platform manifests it selects, their blobs, and the referrers
// of the root and of each platform manifest into blobs.
func mirrorOCIContent(
	ctx context.Context,
	registry ociRegistry,
	repository string,
	blobs ociBlobStore,
	root *oci.ResolvedManifest,
) error {
	seen := map[digest.Digest]bool{}
	if err := mirrorOCIManifest(ctx, registry, repository, blobs, root, seen); err != nil {
		return err
	}

	subjects := []ocispec.Descriptor{root.Descriptor}
	for _, pm := range root.Manifests {
		subjects = append(subjects, pm.Descriptor)
	}

	referrers := 0
	for _, subject := range subjects {
		if err := mirrorOCIReferrers(ctx, registry, repository, blobs, subject, seen, &referrers); err != nil {
			return err
		}
	}

	return nil
}

// mirrorOCIManifest stores a fetched manifest or index, the platform manifests it selects, and their blobs. Digests that
// are already in seen are skipped.
func mirrorOCIManifest(
	ctx context.Context,
	registry ociRegistry,
	repository string,
	blobs ociBlobStore,
	rm *oci.ResolvedManifest,
	seen map[digest.Digest]bool,
) error {
	if !seen[rm.Descriptor.Digest] {
		if err := blobs.putManifest(ctx, rm.Content); err != nil {
			return err
		}

		seen[rm.Descriptor.Digest] = true
	}

	for _, pm := range rm.Manifests {
		if !seen[pm.Descriptor.Digest] {
			if err := blobs.putManifest(ctx, pm.Content); err != nil {
				return err
			}

			seen[pm.Descriptor.Digest] = true
		}

		for _, desc := range manifestBlobs(pm.Manifest) {
			if seen[desc.Digest] {
				continue
			}

			if err := mirrorOCIBlob(ctx, registry, repository, blobs, desc); err != nil {
				return err
			}

			seen[desc.Digest] = true
		}
	}

	return nil
}

// mirrorOCIReferrers mirrors the OCI 1.1 referrers of subject. The count is shared across subjects and is capped.
func mirrorOCIReferrers(
	ctx context.Context,
	registry ociRegistry,
	repository string,
	blobs ociBlobStore,
	subject ocispec.Descriptor,
	seen map[digest.Digest]bool,
	count *int,
) error {
	referrers, err := registry.Referrers(ctx, repository, subject, "")
	if err != nil {
		return err
	}

	for _, ref := range referrers {
		if seen[ref.Digest] {
			continue
		}

		*count++
		if *count > maxOCIReferrers {
			return fmt.Errorf("%w: more than %d referrers", errOCIInvalid, maxOCIReferrers)
		}

		rm, err := registry.FetchManifest(ctx, repository, ref.Digest.String(), nil)
		if err != nil {
			return err
		}

		if err := mirrorOCIManifest(ctx, registry, repository, blobs, rm, seen); err != nil {
			return err
		}
	}

	return nil
}

// mirrorOCIBlob fetches a blob into a temp file, which pkg/oci verifies against desc, and stores it.
func mirrorOCIBlob(
	ctx context.Context,
	registry ociRegistry,
	repository string,
	blobs ociBlobStore,
	desc ocispec.Descriptor,
) error {
	rc, err := registry.FetchBlob(ctx, repository, desc)
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()

	spool, err := spoolToTemp(rc)
	if err != nil {
		return err
	}
	defer removeTempFile(spool)

	return blobs.putBlob(ctx, desc, spool)
}

// spoolOCIPrimary returns a temp file that holds the chart archive of a Helm artifact or the content of a Generic
// artifact. The caller removes it with removeTempFile.
func spoolOCIPrimary(
	ctx context.Context,
	artifactType string,
	root *oci.ResolvedManifest,
	blobs ociBlobStore,
) (*os.File, error) {
	if root.Descriptor.MediaType == ocispec.MediaTypeImageIndex || len(root.Manifests) != 1 {
		return nil, fmt.Errorf("%w: %s artifacts must be a single manifest", errOCIInvalid, artifactType)
	}

	layers := root.Manifests[0].Manifest.Layers
	var layer ocispec.Descriptor
	switch artifactType {
	case ociArtifactHelm:
		found := false
		for _, l := range layers {
			if l.MediaType == oci.HelmChartLayerMediaType {
				layer = l
				found = true
				break
			}
		}

		if !found {
			return nil, fmt.Errorf("%w: helm artifact has no chart layer", errOCIInvalid)
		}
	case ociArtifactGeneric:
		if len(layers) != 1 {
			return nil, fmt.Errorf("%w: generic artifact must have exactly one layer", errOCIInvalid)
		}

		layer = layers[0]
	default:
		return nil, fmt.Errorf("%w: unsupported artifactType %q", errOCIInvalid, artifactType)
	}

	rc, err := blobs.getBlob(ctx, layer.Digest)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()

	return spoolToTemp(rc)
}

// storePackagedOCI stores a packaged GitHub artifact and returns its manifest status.
func storePackagedOCI(ctx context.Context, blobs ociBlobStore, packaged *oci.Packaged) (*opendepotv1alpha1.OCIManifestStatus, error) {
	for _, b := range append([]oci.Blob{packaged.Config}, packaged.Layers...) {
		if err := blobs.putBlob(ctx, b.Descriptor, b.Content); err != nil {
			return nil, err
		}
	}

	if err := blobs.putManifest(ctx, packaged.Manifest); err != nil {
		return nil, err
	}

	var manifest ocispec.Manifest
	if err := json.Unmarshal(packaged.Manifest, &manifest); err != nil {
		return nil, fmt.Errorf("decode packaged manifest: %w", err)
	}

	size := packaged.Config.Descriptor.Size
	for _, layer := range packaged.Layers {
		size += layer.Descriptor.Size
	}

	return &opendepotv1alpha1.OCIManifestStatus{
		Digest:         packaged.Descriptor.Digest.String(),
		MediaType:      packaged.Descriptor.MediaType,
		ArtifactType:   manifest.ArtifactType,
		TotalSizeBytes: size,
	}, nil
}

// ociGithubRepoName returns the repository name of a GitHub source, taken from its repoUrl, and validates it with the owner.
func ociGithubRepoName(src *opendepotv1alpha1.OCIGithubSource) (string, error) {
	if src.RepoUrl == nil {
		return "", errors.New("repoUrl is required for a GitHub source")
	}

	parsed, err := url.Parse(*src.RepoUrl)
	if err != nil {
		return "", fmt.Errorf("repoUrl is not a valid URL: %w", err)
	}

	name := strings.TrimSuffix(path.Base(strings.TrimSuffix(parsed.Path, "/")), ".git")
	if err := opendepotGithub.ValidateRepoName(src.RepoOwner, name); err != nil {
		return "", err
	}

	return name, nil
}

// matchReleaseAssets returns the release assets whose names match the glob pattern.
func matchReleaseAssets(release *github.RepositoryRelease, pattern string) ([]*github.ReleaseAsset, error) {
	var matches []*github.ReleaseAsset
	for _, asset := range release.Assets {
		ok, err := path.Match(pattern, asset.GetName())
		if err != nil {
			return nil, fmt.Errorf("%w: assetPattern %q is not a valid glob: %w", errOCIInvalid, pattern, err)
		}

		if ok {
			matches = append(matches, asset)
		}
	}

	return matches, nil
}

// findReleaseAsset returns the release asset with the exact name, or nil when there is none.
func findReleaseAsset(release *github.RepositoryRelease, name string) *github.ReleaseAsset {
	for _, asset := range release.Assets {
		if asset.GetName() == name {
			return asset
		}
	}

	return nil
}

// downloadReleaseAsset streams a release asset into a temp file. The download is refused beyond maxOCIAssetBytes. The
// caller removes the file with removeTempFile.
func downloadReleaseAsset(
	ctx context.Context,
	githubClient *github.Client,
	owner, repo string,
	asset *github.ReleaseAsset,
) (*os.File, error) {
	rc, _, err := githubClient.Repositories.DownloadReleaseAsset(ctx, owner, repo, asset.GetID(), githubClient.Client())
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", asset.GetName(), err)
	}

	if rc == nil {
		return nil, fmt.Errorf("download %s: no content", asset.GetName())
	}
	defer func() { _ = rc.Close() }()

	f, err := spoolToTemp(io.LimitReader(rc, maxOCIAssetBytes+1))
	if err != nil {
		return nil, err
	}

	info, err := f.Stat()
	if err != nil {
		removeTempFile(f)
		return nil, err
	}

	if info.Size() > maxOCIAssetBytes {
		removeTempFile(f)
		return nil, errOCIAssetTooLarge
	}

	return f, nil
}

// spoolToTemp copies r into a new temp file, rewinds it, and returns it. The caller removes it with removeTempFile.
func spoolToTemp(r io.Reader) (*os.File, error) {
	f, err := os.CreateTemp("", "opendepot-oci-*")
	if err != nil {
		return nil, fmt.Errorf("create temp file: %w", err)
	}

	if _, err := io.Copy(f, r); err != nil {
		removeTempFile(f)
		return nil, err
	}

	if _, err := f.Seek(0, io.SeekStart); err != nil {
		removeTempFile(f)
		return nil, err
	}

	return f, nil
}

// removeTempFile closes and deletes a temp file created by spoolToTemp.
func removeTempFile(f *os.File) {
	_ = f.Close()
	_ = os.Remove(f.Name())
}

// rewindFile seeks f back to its start.
func rewindFile(f *os.File) error {
	_, err := f.Seek(0, io.SeekStart)
	return err
}

// chartMetadataFromFile bounds the decompressed size of a chart archive and parses its Chart.yaml. It leaves f at its start.
func chartMetadataFromFile(f *os.File) (*opendepotv1alpha1.HelmChartMetadata, error) {
	if err := checkChartArchiveBounds(f, maxHelmChartBytes); err != nil {
		return nil, err
	}

	md, err := oci.ParseChartMetadata(f)
	if err != nil {
		return nil, err
	}

	if err := rewindFile(f); err != nil {
		return nil, err
	}

	return md, nil
}

// checkChartArchiveBounds decompresses a chart archive to its end and fails when the decompressed size exceeds limit. It
// rewinds f before and after the pass, so the parsers that run next read the whole archive.
func checkChartArchiveBounds(f io.ReadSeeker, limit int64) error {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}

	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("chart archive is not gzip compressed: %w", err)
	}
	defer func() { _ = gz.Close() }()

	if _, err := io.Copy(io.Discard, &boundedReader{r: gz, limit: limit}); err != nil {
		return fmt.Errorf("read chart archive: %w", err)
	}

	_, err = f.Seek(0, io.SeekStart)
	return err
}

// boundedReader fails with errHelmChartTooLarge once more than limit bytes have been read through it.
type boundedReader struct {
	r     io.Reader
	limit int64
	read  int64
}

func (b *boundedReader) Read(p []byte) (int, error) {
	n, err := b.r.Read(p)
	b.read += int64(n)
	if b.read > b.limit {
		return n, errHelmChartTooLarge
	}

	return n, err
}
