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
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math/big"
	"net/url"
	"os"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/google/go-github/v81/github"
	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	opendepotGithub "github.com/tonedefdev/opendepot/pkg/github"
	"github.com/tonedefdev/opendepot/pkg/oci"
	"github.com/tonedefdev/opendepot/pkg/ocibuild"
)

const (
	ociBuildPhasePending   = "Pending"
	ociBuildPhaseRunning   = "Running"
	ociBuildPhaseSucceeded = "Succeeded"
	ociBuildPhaseFailed    = "Failed"

	// ociBuildSlotRequeue is how long a build waits for a free build slot before it checks again.
	ociBuildSlotRequeue = 30 * time.Second
	// ociBuildPollInterval is how often a running build checks its Jobs, in addition to the Job and Pod watches.
	ociBuildPollInterval = 15 * time.Second
	// ociBuildDeadlineGrace is how long a build may run past its timeout before the controller fails it itself.
	ociBuildDeadlineGrace = 2 * time.Minute
	// ociBuildIDLength matches the CRD pattern of status.ociBuild.buildID.
	ociBuildIDLength = 20
	// ociBuildIDAlphabet is the character set of a build ID.
	ociBuildIDAlphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	// ociBuildDefaultTimeoutSeconds is the timeout of a build that does not set one.
	ociBuildDefaultTimeoutSeconds int64 = 1800
	// ociBuildDefaultMaxConcurrentJobs is the slot cap when MaxConcurrentJobs is unset.
	ociBuildDefaultMaxConcurrentJobs = 2
	// ociBuildTTLSecondsAfterFinished is how long a finished build Job is kept when TTLSecondsAfterFinished is unset.
	ociBuildTTLSecondsAfterFinished int32 = 600
	// ociBuildDefaultPlatform is built when no platform is set and no default is configured.
	ociBuildDefaultPlatform = "linux/amd64"
	// ociBuildMaxConfigBytes bounds the image config that a built manifest is checked against.
	ociBuildMaxConfigBytes int64 = 1 << 20
	// ociSyncedStatus is the SyncStatus of a Version that is synced and up to date.
	ociSyncedStatus = "Synced"
)

// ociNow returns the current time. Tests replace it to exercise build deadlines.
var ociNow = func() time.Time {
	return time.Now().UTC()
}

// errOCIBuildInvalid is returned when a build source cannot produce a build. Retrying does not help.
var errOCIBuildInvalid = errors.New("invalid build source")

// OCIBuildOptions configures in-cluster builds of OCIArtifacts that set build. The zero value disables builds.
type OCIBuildOptions struct {
	// Enabled turns on builds. When it is false, every build source is refused.
	Enabled bool
	// Namespace is the builds namespace, where build Jobs, Pods, and Secrets are created.
	Namespace string
	// BrokerURL is the base URL of the build broker that the Jobs reach.
	BrokerURL string
	// BuildKitImage is the pinned rootless BuildKit image.
	BuildKitImage string
	// HelperImage is the version-controller image, which runs the fetch and upload helpers.
	HelperImage string
	// MaxConcurrentJobs is the number of build Jobs that may run at once. Zero uses the default of 2.
	MaxConcurrentJobs int
	// AllowedRepoOwners lists the GitHub owners whose repositories may be built. An empty list denies every build.
	AllowedRepoOwners []string
	// DefaultPlatforms are built when an OCIArtifact does not set platforms. An empty list uses linux/amd64.
	DefaultPlatforms []string
	// DefaultTimeoutSeconds is the timeout of a build that does not set one. Zero uses 1800 seconds.
	DefaultTimeoutSeconds int64
	// MaxTimeoutSeconds clamps the build timeout. Zero uses ocibuild.MaxTimeoutSeconds.
	MaxTimeoutSeconds int64
	// DefaultResources are the cpu, memory, and ephemeral-storage of a build that does not set them.
	DefaultResources corev1.ResourceList
	// MaxResources clamp the cpu, memory, and ephemeral-storage of each build.
	MaxResources corev1.ResourceList
	// MaxContextBytes bounds the compressed build context. Zero uses ocibuild.DefaultMaxCompressedBytes.
	MaxContextBytes int64
	// MaxImageBytes bounds the total size of a built image. Zero uses ocibuild.DefaultMaxImageBytes.
	MaxImageBytes int64
	// RequireScanning refuses a build unless scanning is enabled.
	RequireScanning bool
	// UserNamespaces sets hostUsers: false on build Pods.
	UserNamespaces bool
	// RuntimeClassName is the runtime class of build Pods. Nil uses the default runtime.
	RuntimeClassName *string
	// Snapshotter is ocibuild.SnapshotterAuto or ocibuild.SnapshotterNative.
	Snapshotter string
	// TTLSecondsAfterFinished is how long a finished build Job is kept. Zero uses 600 seconds.
	TTLSecondsAfterFinished int32
	// NodeSelector is merged with the platform selector of build Pods.
	NodeSelector map[string]string
	// Tolerations are applied to build Pods.
	Tolerations []corev1.Toleration
	// PriorityClassName is the priority class of build Pods. Empty uses the default priority.
	PriorityClassName string
}

// ociBuildRun holds the settings that one build's Jobs are rendered from.
type ociBuildRun struct {
	tag         string
	sha         string
	committedAt time.Time
	repoURL     string
	repoName    string
	platforms   []ocibuild.Platform
	timeout     int64
	resources   corev1.ResourceRequirements
}

// ociBuildTokens are the two one-time tokens of a build. They are stored only in the build Secret. Status keeps their
// hashes.
type ociBuildTokens struct {
	fetch  string
	upload string
}

// OCIBlobSink is the blob and manifest store of one OCIArtifact Version. The build broker writes build output through it.
type OCIBlobSink interface {
	// GetBlob opens the blob addressed by d. It fails when the blob is missing.
	GetBlob(ctx context.Context, d digest.Digest) (io.ReadCloser, error)
	// PutBlob stores content as the blob addressed by d. The content must hash to d.
	PutBlob(ctx context.Context, d digest.Digest, content io.ReadSeeker) error
	// PutManifest stores a manifest and returns its digest.
	PutManifest(ctx context.Context, content []byte) (digest.Digest, error)
}

// ociBuildSink adapts an ociBlobStore to OCIBlobSink.
type ociBuildSink struct {
	blobs ociBlobStore
}

func (s ociBuildSink) GetBlob(ctx context.Context, d digest.Digest) (io.ReadCloser, error) {
	return s.blobs.getBlob(ctx, d)
}

func (s ociBuildSink) PutBlob(ctx context.Context, d digest.Digest, content io.ReadSeeker) error {
	return s.blobs.store.PutBlob(ctx, s.blobs.namespace, s.blobs.repo, d, content)
}

func (s ociBuildSink) PutManifest(ctx context.Context, content []byte) (digest.Digest, error) {
	return s.blobs.store.PutManifest(ctx, s.blobs.namespace, s.blobs.repo, content)
}

// OCIBlobSinkFor returns the blob store of the Version namespace/name. The build broker uses it to store the build
// context and the output of each platform.
func (r *VersionReconciler) OCIBlobSinkFor(ctx context.Context, namespace, name string) (OCIBlobSink, error) {
	version := &opendepotv1alpha1.Version{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, version); err != nil {
		return nil, err
	}

	blobs, err := r.ociBlobStoreFor(ctx, version)
	if err != nil {
		return nil, err
	}

	return ociBuildSink{blobs: blobs}, nil
}

// deleteBlob removes one blob of the Version. A blob that is already gone is not an error.
func (s ociBlobStore) deleteBlob(ctx context.Context, d digest.Digest) error {
	return s.store.DeleteBlob(ctx, s.namespace, s.repo, d)
}

// syncOCIBuild builds an Image OCIArtifact Version from its GitHub source in the cluster. It checks the gates, pins the
// tag to a commit, and starts a build when none is in progress. A build in progress is observed until every platform
// finishes. A finished build is assembled into an index and scanned.
func (r *VersionReconciler) syncOCIBuild(
	ctx context.Context,
	req ctrl.Request,
	version *opendepotv1alpha1.Version,
	config *opendepotv1alpha1.OCIArtifactConfig,
) (ctrl.Result, error) {
	src := config.Build
	if config.ArtifactType != ociArtifactImage {
		return r.terminalOCI(ctx, req, "build is only valid for Image")
	}

	if !r.OCIBuilds.Enabled {
		return r.terminalOCI(ctx, req, "In-cluster builds are disabled")
	}

	if !slices.Contains(r.OCIBuilds.AllowedRepoOwners, src.RepoOwner) {
		return r.terminalOCI(ctx, req, fmt.Sprintf("Repository owner %s is not on the build allow-list", src.RepoOwner))
	}

	if r.OCIBuilds.RequireScanning && !r.ScanningEnabled {
		return r.terminalOCI(ctx, req, "builds require scanning: enable scanning or set oci.builds.requireScanning to false")
	}

	// The status is read without the cache, so that a stale cache cannot start a second build of the same Version.
	current, err := r.currentVersion(ctx, version)
	if err != nil {
		return ctrl.Result{}, err
	}

	version = current
	build := version.Status.OCIBuild
	if build != nil && (build.Phase == ociBuildPhasePending || build.Phase == ociBuildPhaseRunning) {
		return r.advanceOCIBuild(ctx, req, version, config)
	}

	if build != nil && build.Phase == ociBuildPhaseSucceeded && !version.Status.Synced && !version.Spec.ForceSync {
		return r.finishOCIBuild(ctx, req, version, config, build.DeepCopy())
	}

	if build != nil && build.Phase == ociBuildPhaseFailed && !version.Spec.ForceSync {
		return ctrl.Result{}, nil
	}

	repoURL, repoName, err := ociBuildRepo(src)
	if err != nil {
		return r.terminalOCI(ctx, req, fmt.Sprintf("Invalid build source: %v", err))
	}

	gh, err := r.githubClientForOCI(ctx, version, config)
	if err != nil {
		return r.failOCISync(ctx, req, version, "create GitHub client", err)
	}

	tag, sha, committedAt, err := resolveBuildTag(ctx, gh, src, repoName, version.Spec.Version)
	if err != nil {
		return r.failOCISync(ctx, req, version, "resolve tag", err)
	}

	if ociSynced(version) && build != nil && build.Phase == ociBuildPhaseSucceeded {
		// A synced build is never rebuilt while immutable is unset or true. Only a moved tag is recorded as drift.
		if ociImmutable(config) {
			return r.recordOCIBuildDrift(ctx, req, version, sha, true)
		}

		if build.SourceCommit == sha && !version.Spec.ForceSync {
			return r.recordOCIBuildDrift(ctx, req, version, sha, false)
		}
	}

	run, err := r.buildRunFor(config, repoURL, repoName, tag, sha, committedAt)
	if err != nil {
		return r.terminalOCI(ctx, req, fmt.Sprintf("Invalid build settings: %v", err))
	}

	return r.startOCIBuild(ctx, req, version, config, gh, run)
}

// currentVersion returns the latest copy of version. It reads the API server when an API reader is set.
func (r *VersionReconciler) currentVersion(ctx context.Context, version *opendepotv1alpha1.Version) (*opendepotv1alpha1.Version, error) {
	if r.APIReader == nil {
		return version, nil
	}

	latest := &opendepotv1alpha1.Version{}
	if err := r.APIReader.Get(ctx, client.ObjectKeyFromObject(version), latest); err != nil {
		return nil, fmt.Errorf("read version: %w", err)
	}

	return latest, nil
}

// startOCIBuild reserves a build slot, stages the build context, and launches the build. The slot is checked before the
// context is staged, so a waiting build does not download the source on every check.
func (r *VersionReconciler) startOCIBuild(
	ctx context.Context,
	req ctrl.Request,
	version *opendepotv1alpha1.Version,
	config *opendepotv1alpha1.OCIArtifactConfig,
	gh *github.Client,
	run ociBuildRun,
) (ctrl.Result, error) {
	full, err := r.ociBuildSlotsFull(ctx)
	if err != nil {
		return ctrl.Result{}, err
	}

	if full {
		return r.waitForOCIBuildSlot(ctx, req)
	}

	contextDigest, err := r.stageOCIBuildContext(ctx, gh, version, config, run)
	if errors.Is(err, errOCIBuildInvalid) {
		return r.terminalOCI(ctx, req, fmt.Sprintf("Failed to stage build context: %v", err))
	}

	if err != nil {
		return r.failOCISync(ctx, req, version, "stage build context", err)
	}

	// The check and the launch are serialized in this process, so two Versions cannot both take the last slot.
	r.ociBuildMu.Lock()
	defer r.ociBuildMu.Unlock()

	full, err = r.ociBuildSlotsFull(ctx)
	if err != nil {
		return ctrl.Result{}, err
	}

	if full {
		return r.waitForOCIBuildSlot(ctx, req)
	}

	return r.launchOCIBuild(ctx, req, version, config, run, contextDigest)
}

// waitForOCIBuildSlot records that the build is waiting for a slot and requeues it.
func (r *VersionReconciler) waitForOCIBuildSlot(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	if err := r.persistOCIStatus(ctx, req, false, "Waiting for a build slot", nil); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: ociBuildSlotRequeue}, nil
}

// ociBuildSlotsFull reports whether the build Jobs in the builds namespace have reached MaxConcurrentJobs.
func (r *VersionReconciler) ociBuildSlotsFull(ctx context.Context) (bool, error) {
	active, err := r.activeOCIBuildJobs(ctx)
	if err != nil {
		return false, fmt.Errorf("count build Jobs: %w", err)
	}

	limit := r.OCIBuilds.MaxConcurrentJobs
	if limit <= 0 {
		limit = ociBuildDefaultMaxConcurrentJobs
	}

	return active >= limit, nil
}

// stageOCIBuildContext downloads the source archive at the pinned commit, stages the build context, and stores it as a
// blob of the Version. It returns the digest of the staged context.
func (r *VersionReconciler) stageOCIBuildContext(
	ctx context.Context,
	gh *github.Client,
	version *opendepotv1alpha1.Version,
	config *opendepotv1alpha1.OCIArtifactConfig,
	run ociBuildRun,
) (digest.Digest, error) {
	src := config.Build
	resp, err := opendepotGithub.GetArchiveRequest(ctx, gh, src.RepoOwner, run.repoName, github.Tarball, run.sha)
	if err != nil {
		return "", fmt.Errorf("download source: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("download source: status code %d", resp.StatusCode)
	}

	workDir, err := os.MkdirTemp("", "opendepot-ocibuild-src-*")
	if err != nil {
		return "", fmt.Errorf("create source dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(workDir) }()

	spool, err := os.CreateTemp("", "opendepot-ocibuild-context-*.tar.gz")
	if err != nil {
		return "", fmt.Errorf("create context spool: %w", err)
	}
	defer func() {
		_ = spool.Close()
		_ = os.Remove(spool.Name())
	}()

	hasher := digest.SHA256.Digester()
	err = ocibuild.StageContext(io.MultiWriter(spool, hasher.Hash()), resp.Body, workDir, ocibuild.ContextSpec{
		ContextPath:    src.ContextPath,
		DockerfilePath: src.DockerfilePath,
	}, ocibuild.Limits{MaxCompressedBytes: r.OCIBuilds.MaxContextBytes})
	if isInvalidBuildContext(err) {
		return "", fmt.Errorf("%w: %v", errOCIBuildInvalid, err)
	}

	if err != nil {
		return "", fmt.Errorf("stage build context: %w", err)
	}

	info, err := spool.Stat()
	if err != nil {
		return "", fmt.Errorf("stat context: %w", err)
	}

	if _, err := spool.Seek(0, io.SeekStart); err != nil {
		return "", fmt.Errorf("rewind context: %w", err)
	}

	blobs, err := r.ociBlobStoreFor(ctx, version)
	if err != nil {
		return "", fmt.Errorf("open storage: %w", err)
	}

	d := hasher.Digest()
	if err := blobs.putBlob(ctx, ocispec.Descriptor{Digest: d, Size: info.Size()}, spool); err != nil {
		return "", fmt.Errorf("store build context: %w", err)
	}

	return d, nil
}

// isInvalidBuildContext reports whether a staging error comes from the source itself, so that retrying cannot fix it.
func isInvalidBuildContext(err error) bool {
	return errors.Is(err, ocibuild.ErrLimitExceeded) ||
		errors.Is(err, ocibuild.ErrUnsafeEntry) ||
		errors.Is(err, ocibuild.ErrUnsafePath) ||
		errors.Is(err, ocibuild.ErrInvalidDockerfile) ||
		errors.Is(err, fs.ErrNotExist)
}

// launchOCIBuild writes the build state to status before it creates any object, so that a restart can resume the
// build. It then creates the Secret with the two tokens, one Job per platform, and the owner references that tie the
// Secret to the Jobs.
func (r *VersionReconciler) launchOCIBuild(
	ctx context.Context,
	req ctrl.Request,
	version *opendepotv1alpha1.Version,
	config *opendepotv1alpha1.OCIArtifactConfig,
	run ociBuildRun,
	contextDigest digest.Digest,
) (ctrl.Result, error) {
	buildID, err := newOCIBuildID()
	if err != nil {
		return ctrl.Result{}, err
	}

	tokens, err := newOCIBuildTokens()
	if err != nil {
		return ctrl.Result{}, err
	}

	now := metav1.NewTime(ociNow())
	platforms := make([]opendepotv1alpha1.OCIBuildPlatformStatus, 0, len(run.platforms))
	for _, p := range run.platforms {
		platforms = append(platforms, opendepotv1alpha1.OCIBuildPlatformStatus{
			Platform: p.String(),
			JobName:  ocibuild.JobName(buildID, p),
			Phase:    ociBuildPhasePending,
		})
	}

	build := &opendepotv1alpha1.OCIBuildStatus{
		BuildID:         buildID,
		SourceCommit:    run.sha,
		SourceTag:       run.tag,
		ContextDigest:   contextDigest.String(),
		FetchTokenHash:  "sha256:" + ocibuild.HashToken(tokens.fetch),
		UploadTokenHash: "sha256:" + ocibuild.HashToken(tokens.upload),
		Phase:           ociBuildPhaseRunning,
		StartedAt:       &now,
		Message:         fmt.Sprintf("Building %d platform(s)", len(platforms)),
		Platforms:       platforms,
	}

	if err := r.persistOCIStatus(ctx, req, false, fmt.Sprintf("Building %s from %s", run.tag, run.sha), func(current *opendepotv1alpha1.Version) {
		current.Status.OCIBuild = build
	}); err != nil {
		return ctrl.Result{}, err
	}

	if version.Spec.ForceSync {
		if err := r.clearOCIForceSync(ctx, req); err != nil {
			return ctrl.Result{}, err
		}
	}

	if err := r.createOCIBuildObjects(ctx, version, config, run, build, tokens); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: ociBuildPollInterval}, nil
}

// createOCIBuildObjects renders the Jobs and the Secret of a build with pkg/ocibuild. Objects that already exist are
// kept, so that a restart can finish a launch. The Secret is created before the Jobs, because a Job's volumes need it.
func (r *VersionReconciler) createOCIBuildObjects(
	ctx context.Context,
	version *opendepotv1alpha1.Version,
	config *opendepotv1alpha1.OCIArtifactConfig,
	run ociBuildRun,
	build *opendepotv1alpha1.OCIBuildStatus,
	tokens ociBuildTokens,
) error {
	contextDigest := digest.Digest(build.ContextDigest)
	var secret *corev1.Secret
	jobs := make([]*batchv1.Job, 0, len(run.platforms))
	for _, p := range run.platforms {
		params := r.ociBuildJobParams(version, config, run, build.BuildID, p, contextDigest, tokens)
		if err := params.Validate(); err != nil {
			return fmt.Errorf("invalid build settings: %w", err)
		}

		job, s := ocibuild.RenderJob(params)
		if secret == nil {
			secret = s
		}

		jobs = append(jobs, job)
	}

	if err := r.Create(ctx, secret); err != nil && !k8serr.IsAlreadyExists(err) {
		return fmt.Errorf("create build secret: %w", err)
	}

	for _, job := range jobs {
		if err := r.Create(ctx, job); err != nil && !k8serr.IsAlreadyExists(err) {
			return fmt.Errorf("create build Job %s: %w", job.Name, err)
		}
	}

	existing, err := r.listOCIBuildJobs(ctx, build.BuildID)
	if err != nil {
		return err
	}

	return r.ensureOCIBuildSecretOwners(ctx, build.BuildID, existing)
}

// ociBuildJobParams returns the parameters of one platform's Job.
func (r *VersionReconciler) ociBuildJobParams(
	version *opendepotv1alpha1.Version,
	config *opendepotv1alpha1.OCIArtifactConfig,
	run ociBuildRun,
	buildID string,
	platform ocibuild.Platform,
	contextDigest digest.Digest,
	tokens ociBuildTokens,
) ocibuild.JobParams {
	src := config.Build
	ttl := r.OCIBuilds.TTLSecondsAfterFinished
	if ttl == 0 {
		ttl = ociBuildTTLSecondsAfterFinished
	}

	params := ocibuild.JobParams{
		Namespace:               r.OCIBuilds.Namespace,
		BuildID:                 buildID,
		Platform:                platform,
		VersionNamespace:        version.Namespace,
		VersionName:             version.Name,
		BrokerURL:               r.OCIBuilds.BrokerURL,
		ContextDigest:           contextDigest,
		BuildArgs:               src.BuildArgs,
		SourceDateEpoch:         run.committedAt.Unix(),
		BuildKitImage:           r.OCIBuilds.BuildKitImage,
		HelperImage:             r.OCIBuilds.HelperImage,
		ActiveDeadlineSeconds:   run.timeout,
		TTLSecondsAfterFinished: ttl,
		Resources:               run.resources,
		VolumeSizeLimit:         run.resources.Limits[corev1.ResourceEphemeralStorage],
		RuntimeClassName:        r.OCIBuilds.RuntimeClassName,
		UserNamespaces:          r.OCIBuilds.UserNamespaces,
		Snapshotter:             r.OCIBuilds.Snapshotter,
		NodeSelector:            r.OCIBuilds.NodeSelector,
		Tolerations:             r.OCIBuilds.Tolerations,
		PriorityClassName:       r.OCIBuilds.PriorityClassName,
		FetchToken:              tokens.fetch,
		UploadToken:             tokens.upload,
	}

	if src.Target != nil {
		params.Target = *src.Target
	}

	return params
}

// advanceOCIBuild observes a build that is in progress. A failed platform fails the build. When every platform has
// succeeded, the build is assembled and scanned.
func (r *VersionReconciler) advanceOCIBuild(
	ctx context.Context,
	req ctrl.Request,
	version *opendepotv1alpha1.Version,
	config *opendepotv1alpha1.OCIArtifactConfig,
) (ctrl.Result, error) {
	build := version.Status.OCIBuild.DeepCopy()
	timeout := r.ociBuildTimeout(config.Build)
	if build.StartedAt != nil && ociNow().After(build.StartedAt.Add(time.Duration(timeout)*time.Second+ociBuildDeadlineGrace)) {
		return r.failOCIBuild(ctx, req, version, build, fmt.Sprintf("build exceeded its timeout of %d seconds", timeout))
	}

	secret, err := r.getOCIBuildSecret(ctx, build.BuildID)
	if err != nil {
		if !k8serr.IsNotFound(err) {
			return ctrl.Result{}, fmt.Errorf("read build secret: %w", err)
		}

		jobs, err := r.listOCIBuildJobs(ctx, build.BuildID)
		if err != nil {
			return ctrl.Result{}, err
		}

		if len(jobs) > 0 {
			return r.failOCIBuild(ctx, req, version, build, "the build Secret was deleted")
		}

		// Nothing was created before the restart, so the build starts again with new tokens and the same context.
		return r.relaunchOCIBuild(ctx, req, version, config, build)
	}

	tokens, err := readOCIBuildTokens(secret)
	if err != nil {
		return r.failOCIBuild(ctx, req, version, build, err.Error())
	}

	jobs, err := r.listOCIBuildJobs(ctx, build.BuildID)
	if err != nil {
		return ctrl.Result{}, err
	}

	if err := r.ensureOCIBuildSecretOwners(ctx, build.BuildID, jobs); err != nil {
		return ctrl.Result{}, err
	}

	changed := false
	for i := range build.Platforms {
		p := &build.Platforms[i]
		if p.Phase == ociBuildPhaseSucceeded {
			continue
		}

		platform, err := ocibuild.ParsePlatform(p.Platform)
		if err != nil {
			return r.failOCIBuild(ctx, req, version, build, fmt.Sprintf("invalid platform %s", p.Platform))
		}

		job, ok := jobs[p.JobName]
		if !ok {
			if p.Phase != ociBuildPhasePending {
				p.Phase = ociBuildPhaseFailed
				p.Message = "the build Job is missing"
				return r.failOCIBuild(ctx, req, version, build, fmt.Sprintf("build for %s: the build Job is missing", platform))
			}

			// The Secret exists but this Job was never created, so the launch stopped part way. Create it now.
			if err := r.createMissingOCIBuildJob(ctx, version, config, build, platform, tokens); err != nil {
				return ctrl.Result{}, err
			}

			changed = true
			continue
		}

		switch {
		case jobHasCondition(job, batchv1.JobFailed):
			msg, err := r.platformFailure(ctx, job, platform)
			if err != nil {
				return ctrl.Result{}, err
			}

			p.Phase = ociBuildPhaseFailed
			p.Message = msg
			return r.failOCIBuild(ctx, req, version, build, msg)
		case jobHasCondition(job, batchv1.JobComplete):
			manifestDigest, err := r.uploadedManifestDigest(ctx, job)
			if err != nil {
				p.Phase = ociBuildPhaseFailed
				p.Message = err.Error()
				return r.failOCIBuild(ctx, req, version, build, fmt.Sprintf("build for %s: %v", platform, err))
			}

			p.Phase = ociBuildPhaseSucceeded
			p.ManifestDigest = manifestDigest
			changed = true
		default:
			unschedulable, err := r.jobUnschedulable(ctx, job)
			if err != nil {
				return ctrl.Result{}, err
			}

			if unschedulable {
				p.Phase = ociBuildPhaseFailed
				p.Message = "no schedulable node for this platform"
				return r.failOCIBuild(ctx, req, version, build, fmt.Sprintf("build for %s: no schedulable node", platform))
			}

			if p.Phase != ociBuildPhaseRunning {
				p.Phase = ociBuildPhaseRunning
				changed = true
			}
		}
	}

	if !allOCIBuildPlatformsSucceeded(build.Platforms) {
		if changed {
			if err := r.persistOCIBuildPlatforms(ctx, req, build); err != nil {
				return ctrl.Result{}, err
			}
		}

		return ctrl.Result{RequeueAfter: ociBuildPollInterval}, nil
	}

	now := metav1.NewTime(ociNow())
	build.Phase = ociBuildPhaseSucceeded
	build.CompletedAt = &now
	build.Message = "All platforms built; assembling the index"
	if err := r.persistOCIBuild(ctx, req, build); err != nil {
		return ctrl.Result{}, err
	}

	return r.finishOCIBuild(ctx, req, version, config, build)
}

// createMissingOCIBuildJob renders and creates the Job of one platform from the build Secret.
func (r *VersionReconciler) createMissingOCIBuildJob(
	ctx context.Context,
	version *opendepotv1alpha1.Version,
	config *opendepotv1alpha1.OCIArtifactConfig,
	build *opendepotv1alpha1.OCIBuildStatus,
	platform ocibuild.Platform,
	tokens ociBuildTokens,
) error {
	run, err := r.resolveOCIBuildRun(ctx, version, config, build)
	if err != nil {
		return err
	}

	params := r.ociBuildJobParams(version, config, run, build.BuildID, platform, digest.Digest(build.ContextDigest), tokens)
	if err := params.Validate(); err != nil {
		return fmt.Errorf("invalid build settings: %w", err)
	}

	job, _ := ocibuild.RenderJob(params)
	if err := r.Create(ctx, job); err != nil && !k8serr.IsAlreadyExists(err) {
		return fmt.Errorf("create build Job %s: %w", job.Name, err)
	}

	return nil
}

// relaunchOCIBuild starts a build again after a restart that created nothing. It keeps the staged context and the
// pinned commit, and it generates a new build ID and new tokens.
func (r *VersionReconciler) relaunchOCIBuild(
	ctx context.Context,
	req ctrl.Request,
	version *opendepotv1alpha1.Version,
	config *opendepotv1alpha1.OCIArtifactConfig,
	build *opendepotv1alpha1.OCIBuildStatus,
) (ctrl.Result, error) {
	if build.ContextDigest == "" {
		return r.failOCIBuild(ctx, req, version, build, "the build context is missing")
	}

	run, err := r.resolveOCIBuildRun(ctx, version, config, build)
	if err != nil {
		return ctrl.Result{}, err
	}

	return r.launchOCIBuild(ctx, req, version, config, run, digest.Digest(build.ContextDigest))
}

// resolveOCIBuildRun returns the settings of a build from its pinned commit. The commit time is read again from GitHub,
// because the Job needs it.
func (r *VersionReconciler) resolveOCIBuildRun(
	ctx context.Context,
	version *opendepotv1alpha1.Version,
	config *opendepotv1alpha1.OCIArtifactConfig,
	build *opendepotv1alpha1.OCIBuildStatus,
) (ociBuildRun, error) {
	repoURL, repoName, err := ociBuildRepo(config.Build)
	if err != nil {
		return ociBuildRun{}, err
	}

	gh, err := r.githubClientForOCI(ctx, version, config)
	if err != nil {
		return ociBuildRun{}, err
	}

	_, committedAt, err := opendepotGithub.ResolveTagCommit(ctx, gh, config.Build.RepoOwner, repoName, build.SourceCommit)
	if err != nil {
		return ociBuildRun{}, fmt.Errorf("resolve pinned commit: %w", err)
	}

	return r.buildRunFor(config, repoURL, repoName, build.SourceTag, build.SourceCommit, committedAt)
}

// finishOCIBuild assembles the platform manifests into an image index, runs the scan and the ScanPolicy gate through
// the shared finishOCISync path, and cleans up the build once the Version is synced. A blocking scan returns an error,
// so the assembly is retried with backoff.
func (r *VersionReconciler) finishOCIBuild(
	ctx context.Context,
	req ctrl.Request,
	version *opendepotv1alpha1.Version,
	config *opendepotv1alpha1.OCIArtifactConfig,
	build *opendepotv1alpha1.OCIBuildStatus,
) (ctrl.Result, error) {
	blobs, err := r.ociBlobStoreFor(ctx, version)
	if errors.Is(err, errOCIInvalid) {
		return r.terminalOCI(ctx, req, fmt.Sprintf("Failed to build OCI artifact: %v", err))
	}

	if err != nil {
		return r.failOCISync(ctx, req, version, "open storage", err)
	}

	repoURL, _, err := ociBuildRepo(config.Build)
	if err != nil {
		return r.terminalOCI(ctx, req, fmt.Sprintf("Invalid build source: %v", err))
	}

	var manifests []oci.PlatformManifest
	var entries []ocibuild.PlatformManifest
	for _, p := range build.Platforms {
		platform, err := ocibuild.ParsePlatform(p.Platform)
		if err != nil {
			return r.terminalOCI(ctx, req, fmt.Sprintf("Failed to build OCI artifact: %v", err))
		}

		d, err := digest.Parse(p.ManifestDigest)
		if err != nil || d.Algorithm() != digest.SHA256 {
			return r.terminalOCI(ctx, req, fmt.Sprintf("Failed to build OCI artifact: build for %s produced no manifest", platform))
		}

		content, err := blobs.store.GetManifest(ctx, blobs.namespace, blobs.repo, d)
		if err != nil {
			return r.failOCISync(ctx, req, version, "read platform manifest", err)
		}

		var manifest ocispec.Manifest
		if err := json.Unmarshal(content, &manifest); err != nil {
			return r.terminalOCI(ctx, req, fmt.Sprintf("Failed to build OCI artifact: manifest for %s is not valid JSON", platform))
		}

		if err := r.checkBuiltManifest(ctx, blobs, platform, manifest); err != nil {
			return r.terminalOCI(ctx, req, fmt.Sprintf("Failed to build OCI artifact: %v", err))
		}

		desc := ocispec.Descriptor{
			MediaType: ocispec.MediaTypeImageManifest,
			Digest:    d,
			Size:      int64(len(content)),
			Platform:  platform.OCI(),
		}
		manifests = append(manifests, oci.PlatformManifest{Descriptor: desc, Content: content, Manifest: manifest})
		entries = append(entries, ocibuild.PlatformManifest{
			Platform:  platform,
			MediaType: ocispec.MediaTypeImageManifest,
			Digest:    d,
			Size:      desc.Size,
		})
	}

	totalBytes := ociBlobSize(manifests)
	if limit := r.ociBuildMaxImageBytes(); totalBytes > limit {
		return r.terminalOCI(ctx, req, fmt.Sprintf("Failed to build OCI artifact: the image is %d bytes and the limit is %d", totalBytes, limit))
	}

	indexBytes, err := ocibuild.BuildIndex(entries, ocibuild.IndexMetadata{
		Source:   repoURL,
		Revision: build.SourceCommit,
		Version:  version.Spec.Version,
		BuildID:  build.BuildID,
	})
	if err != nil {
		return r.terminalOCI(ctx, req, fmt.Sprintf("Failed to build OCI artifact: %v", err))
	}

	indexDigest, err := blobs.store.PutManifest(ctx, blobs.namespace, blobs.repo, indexBytes)
	if err != nil {
		return r.failOCISync(ctx, req, version, "store image index", err)
	}

	root := &oci.ResolvedManifest{
		Descriptor: ocispec.Descriptor{MediaType: ocispec.MediaTypeImageIndex, Digest: indexDigest, Size: int64(len(indexBytes))},
		Content:    indexBytes,
		Manifests:  manifests,
	}

	manifest := &opendepotv1alpha1.OCIManifestStatus{
		Digest:         indexDigest.String(),
		MediaType:      ocispec.MediaTypeImageIndex,
		Platforms:      ociPlatforms(manifests),
		TotalSizeBytes: totalBytes,
	}

	if err := r.finishOCISync(ctx, req, version, ociArtifactImage, manifest, nil, ociScanInput{root: root, blobs: blobs}); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.cleanupOCIBuild(ctx, version, build); err != nil {
		r.Log.Error(err, "Failed to clean up the build", "version", version.Name, "buildID", build.BuildID)
	}

	return ctrl.Result{RequeueAfter: ociDriftRequeueInterval}, nil
}

// checkBuiltManifest checks a platform manifest before it is assembled. It must be an image manifest with no
// attestations, at most the allowed number of layers, blobs within the blob limit, and a config that names the platform.
func (r *VersionReconciler) checkBuiltManifest(
	ctx context.Context,
	blobs ociBlobStore,
	platform ocibuild.Platform,
	manifest ocispec.Manifest,
) error {
	if manifest.MediaType != "" && manifest.MediaType != ocispec.MediaTypeImageManifest {
		return fmt.Errorf("manifest media type %q must be %s", manifest.MediaType, ocispec.MediaTypeImageManifest)
	}

	if manifest.Subject != nil || manifest.Annotations["vnd.docker.reference.type"] == "attestation-manifest" {
		return errors.New("attestation and referrer manifests are not allowed")
	}

	if len(manifest.Layers) > ocibuild.DefaultMaxLayers {
		return fmt.Errorf("manifest has %d layers, more than %d", len(manifest.Layers), ocibuild.DefaultMaxLayers)
	}

	for _, layer := range manifest.Layers {
		if strings.HasPrefix(layer.MediaType, "application/vnd.in-toto") {
			return errors.New("attestation layers are not allowed")
		}
	}

	for _, desc := range manifestBlobs(manifest) {
		if desc.Size > maxImageBlobBytes {
			return errOCIBlobSize(desc, maxImageBlobBytes)
		}
	}

	rc, err := blobs.getBlob(ctx, manifest.Config.Digest)
	if err != nil {
		return fmt.Errorf("read image config: %w", err)
	}
	defer func() { _ = rc.Close() }()

	var config ocispec.Image
	if err := json.NewDecoder(io.LimitReader(rc, ociBuildMaxConfigBytes)).Decode(&config); err != nil {
		return fmt.Errorf("image config is not valid JSON: %w", err)
	}

	if config.OS != platform.OS || config.Architecture != platform.Architecture || config.Variant != platform.Variant {
		return fmt.Errorf("image config platform %s/%s does not match %s", config.OS, config.Architecture, platform)
	}

	return nil
}

// failOCIBuild marks the build failed, then deletes its Jobs and Secret and removes the staged context. A failed build is
// terminal until forceSync is set, so it is not requeued.
func (r *VersionReconciler) failOCIBuild(
	ctx context.Context,
	req ctrl.Request,
	version *opendepotv1alpha1.Version,
	build *opendepotv1alpha1.OCIBuildStatus,
	msg string,
) (ctrl.Result, error) {
	msg = truncateOCIMessage(sanitizeOCIMessage(msg))
	now := metav1.NewTime(ociNow())
	build.Phase = ociBuildPhaseFailed
	build.CompletedAt = &now
	build.Message = msg
	if err := r.persistOCIStatus(ctx, req, false, "Build failed: "+msg, func(current *opendepotv1alpha1.Version) {
		current.Status.OCIBuild = build
	}); err != nil {
		return ctrl.Result{}, err
	}

	r.Log.Info("OCI build failed", "version", version.Name, "buildID", build.BuildID, "reason", msg)
	if err := r.cleanupOCIBuild(ctx, version, build); err != nil {
		r.Log.Error(err, "Failed to clean up the failed build", "version", version.Name, "buildID", build.BuildID)
	}

	return ctrl.Result{}, nil
}

// recordOCIBuildDrift records whether the tag has moved since the synced build. A moved tag is recorded as drift. It is
// never rebuilt while immutable is set, and forceSync is cleared because it has been considered.
func (r *VersionReconciler) recordOCIBuildDrift(
	ctx context.Context,
	req ctrl.Request,
	version *opendepotv1alpha1.Version,
	sha string,
	immutable bool,
) (ctrl.Result, error) {
	drift := version.Status.OCIBuild.SourceCommit != sha
	if version.Status.OCIManifest.TagDrift != drift {
		syncStatus := ociSyncedStatus
		if drift && immutable {
			syncStatus = "Synced; the tag has moved and is not rebuilt because immutable is set"
		}

		err := r.persistOCIStatus(ctx, req, true, syncStatus, func(current *opendepotv1alpha1.Version) {
			if current.Status.OCIManifest == nil {
				return
			}

			current.Status.OCIManifest.TagDrift = drift
		})
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	if version.Spec.ForceSync {
		if err := r.clearOCIForceSync(ctx, req); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{RequeueAfter: ociDriftRequeueInterval}, nil
}

// cleanupOCIBuild deletes the Jobs and the Secret of a build, then the staged context blob. The platform manifests stay,
// because the index refers to them.
func (r *VersionReconciler) cleanupOCIBuild(ctx context.Context, version *opendepotv1alpha1.Version, build *opendepotv1alpha1.OCIBuildStatus) error {
	if err := r.deleteOCIBuildObjects(ctx, build.BuildID); err != nil {
		return err
	}

	if build.ContextDigest == "" {
		return nil
	}

	blobs, err := r.ociBlobStoreFor(ctx, version)
	if err != nil {
		return err
	}

	return blobs.deleteBlob(ctx, digest.Digest(build.ContextDigest))
}

// persistOCIBuild writes the build status of the Version.
func (r *VersionReconciler) persistOCIBuild(ctx context.Context, req ctrl.Request, build *opendepotv1alpha1.OCIBuildStatus) error {
	return r.persistOCIStatus(ctx, req, false, build.Message, func(current *opendepotv1alpha1.Version) {
		current.Status.OCIBuild = build
	})
}

// persistOCIBuildPlatforms writes the platform states of the build.
func (r *VersionReconciler) persistOCIBuildPlatforms(ctx context.Context, req ctrl.Request, build *opendepotv1alpha1.OCIBuildStatus) error {
	done := 0
	for _, p := range build.Platforms {
		if p.Phase == ociBuildPhaseSucceeded {
			done++
		}
	}

	syncStatus := fmt.Sprintf("Building: %d of %d platform(s) complete", done, len(build.Platforms))
	return r.persistOCIStatus(ctx, req, false, syncStatus, func(current *opendepotv1alpha1.Version) {
		if current.Status.OCIBuild == nil || current.Status.OCIBuild.BuildID != build.BuildID {
			return
		}

		current.Status.OCIBuild.Platforms = build.Platforms
	})
}

// buildRunFor returns the settings of one build from its pinned commit and the OCIArtifact config.
func (r *VersionReconciler) buildRunFor(
	config *opendepotv1alpha1.OCIArtifactConfig,
	repoURL, repoName, tag, sha string,
	committedAt time.Time,
) (ociBuildRun, error) {
	platforms, err := r.ociBuildPlatforms(config)
	if err != nil {
		return ociBuildRun{}, err
	}

	return ociBuildRun{
		tag:         tag,
		sha:         sha,
		committedAt: committedAt,
		repoURL:     repoURL,
		repoName:    repoName,
		platforms:   platforms,
		timeout:     r.ociBuildTimeout(config.Build),
		resources:   r.ociBuildResources(config.Build),
	}, nil
}

// ociBuildPlatforms returns the platforms of a build. It uses the platforms of the OCIArtifact, then the default
// platforms, and it rejects duplicates and more platforms than a Version may serve.
func (r *VersionReconciler) ociBuildPlatforms(config *opendepotv1alpha1.OCIArtifactConfig) ([]ocibuild.Platform, error) {
	values := config.Platforms
	if len(values) == 0 {
		values = r.OCIBuilds.DefaultPlatforms
	}

	if len(values) == 0 {
		values = []string{ociBuildDefaultPlatform}
	}

	if len(values) > maxOCIPlatforms {
		return nil, fmt.Errorf("the build serves %d platforms and the limit is %d", len(values), maxOCIPlatforms)
	}

	seen := map[string]bool{}
	platforms := make([]ocibuild.Platform, 0, len(values))
	for _, value := range values {
		p, err := ocibuild.ParsePlatform(value)
		if err != nil {
			return nil, err
		}

		if seen[p.String()] {
			return nil, fmt.Errorf("platform %s is listed more than once", p)
		}

		seen[p.String()] = true
		platforms = append(platforms, p)
	}

	return platforms, nil
}

// ociBuildTimeout returns the timeout of a build in seconds, clamped to the maximum.
func (r *VersionReconciler) ociBuildTimeout(src *opendepotv1alpha1.OCIBuildSource) int64 {
	timeout := r.OCIBuilds.DefaultTimeoutSeconds
	if timeout <= 0 {
		timeout = ociBuildDefaultTimeoutSeconds
	}

	if src != nil && src.TimeoutSeconds != nil {
		timeout = int64(*src.TimeoutSeconds)
	}

	limit := r.OCIBuilds.MaxTimeoutSeconds
	if limit <= 0 {
		limit = ocibuild.MaxTimeoutSeconds
	}

	return min(timeout, limit)
}

// ociBuildResources returns the compute resources of a build. Each value is the source's value or the default, clamped
// to the maximum. Requests and limits are equal, so the scheduler reserves exactly what the Job may use.
func (r *VersionReconciler) ociBuildResources(src *opendepotv1alpha1.OCIBuildSource) corev1.ResourceRequirements {
	requested := map[corev1.ResourceName]*resource.Quantity{}
	if src != nil && src.Resources != nil {
		requested[corev1.ResourceCPU] = src.Resources.CPU
		requested[corev1.ResourceMemory] = src.Resources.Memory
		requested[corev1.ResourceEphemeralStorage] = src.Resources.EphemeralStorage
	}

	values := corev1.ResourceList{}
	for _, name := range []corev1.ResourceName{corev1.ResourceCPU, corev1.ResourceMemory, corev1.ResourceEphemeralStorage} {
		value, ok := r.OCIBuilds.DefaultResources[name]
		if q := requested[name]; q != nil {
			value, ok = *q, true
		}

		if !ok {
			continue
		}

		if limit, ok := r.OCIBuilds.MaxResources[name]; ok && value.Cmp(limit) > 0 {
			value = limit
		}

		values[name] = value.DeepCopy()
	}

	return corev1.ResourceRequirements{Requests: values, Limits: values.DeepCopy()}
}

// ociBuildMaxImageBytes returns the largest image that a build may assemble.
func (r *VersionReconciler) ociBuildMaxImageBytes() int64 {
	if r.OCIBuilds.MaxImageBytes > 0 {
		return r.OCIBuilds.MaxImageBytes
	}

	return ocibuild.DefaultMaxImageBytes
}

// ociBuildRepo returns the canonical repository URL and the repository name of a build source. The URL must be a GitHub
// URL for the owner in the source.
func ociBuildRepo(src *opendepotv1alpha1.OCIBuildSource) (string, string, error) {
	if src.RepoUrl == nil {
		return "", "", errors.New("repoUrl is required for a build source")
	}

	if err := opendepotGithub.ValidateRepoURL(src.RepoOwner, *src.RepoUrl); err != nil {
		return "", "", err
	}

	parsed, err := url.Parse(*src.RepoUrl)
	if err != nil {
		return "", "", fmt.Errorf("repoUrl is not a valid URL: %w", err)
	}

	name := strings.TrimSuffix(path.Base(strings.TrimSuffix(parsed.Path, "/")), ".git")
	if err := opendepotGithub.ValidateRepoName(src.RepoOwner, name); err != nil {
		return "", "", err
	}

	return "https://github.com/" + src.RepoOwner + "/" + name, name, nil
}

// resolveBuildTag returns the first candidate tag of version that exists, the commit it points to, and the commit time.
// The candidates are tried in the order of opendepotGithub.TagCandidates.
func resolveBuildTag(
	ctx context.Context,
	gh *github.Client,
	src *opendepotv1alpha1.OCIBuildSource,
	repoName, version string,
) (string, string, time.Time, error) {
	prefix := ""
	if src.TagPrefix != nil {
		prefix = *src.TagPrefix
	}

	var lastErr error
	for _, tag := range opendepotGithub.TagCandidates(prefix, version) {
		sha, committedAt, err := opendepotGithub.ResolveTagCommit(ctx, gh, src.RepoOwner, repoName, tag)
		if err == nil {
			return tag, sha, committedAt, nil
		}

		lastErr = err
		if !errors.Is(err, opendepotGithub.ErrRefNotFound) {
			return "", "", time.Time{}, err
		}
	}

	return "", "", time.Time{}, lastErr
}

// newOCIBuildID returns a random build ID of ociBuildIDLength lowercase letters and digits.
func newOCIBuildID() (string, error) {
	alphabet := big.NewInt(int64(len(ociBuildIDAlphabet)))
	id := make([]byte, ociBuildIDLength)
	for i := range id {
		n, err := rand.Int(rand.Reader, alphabet)
		if err != nil {
			return "", fmt.Errorf("generate build ID: %w", err)
		}

		id[i] = ociBuildIDAlphabet[n.Int64()]
	}

	return string(id), nil
}

// newOCIBuildTokens returns the fetch and upload tokens of a new build.
func newOCIBuildTokens() (ociBuildTokens, error) {
	fetch, err := ocibuild.NewToken()
	if err != nil {
		return ociBuildTokens{}, err
	}

	upload, err := ocibuild.NewToken()
	if err != nil {
		return ociBuildTokens{}, err
	}

	return ociBuildTokens{fetch: fetch, upload: upload}, nil
}

// allOCIBuildPlatformsSucceeded reports whether every platform of a build has succeeded.
func allOCIBuildPlatformsSucceeded(platforms []opendepotv1alpha1.OCIBuildPlatformStatus) bool {
	if len(platforms) == 0 {
		return false
	}

	for _, p := range platforms {
		if p.Phase != ociBuildPhaseSucceeded {
			return false
		}
	}

	return true
}
