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
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/go-github/v81/github"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	"github.com/tonedefdev/opendepot/pkg/ocibuild"
)

// ociBuildsTestNamespace is the builds namespace of the build specs.
const ociBuildsTestNamespace = "oci-builds-test"

// fakeBuildGitHub serves the GitHub commits API and the source archive of the build specs.
type fakeBuildGitHub struct {
	mu       sync.Mutex
	refs     map[string]string
	archives int
	commits  int
	base     string
	client   *github.Client
}

// newFakeBuildGitHub starts the fake and returns a GitHub client that points at it.
func newFakeBuildGitHub() *fakeBuildGitHub {
	f := &fakeBuildGitHub{refs: map[string]string{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/acme/app/commits/", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()

		f.commits++
		sha, ok := f.refs[strings.TrimPrefix(r.URL.Path, "/repos/acme/app/commits/")]
		if !ok {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"sha":%q,"commit":{"committer":{"date":"2026-01-02T03:04:05Z"}}}`, sha)
	})
	mux.HandleFunc("/repos/acme/app/tarball/", func(w http.ResponseWriter, r *http.Request) {
		ref := strings.TrimPrefix(r.URL.Path, "/repos/acme/app/tarball/")
		http.Redirect(w, r, f.base+"/archive/"+ref, http.StatusFound)
	})
	mux.HandleFunc("/archive/", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.archives++
		sha, ok := f.refs[strings.TrimPrefix(r.URL.Path, "/archive/")]
		f.mu.Unlock()
		if !ok {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Content-Type", "application/gzip")
		_, _ = w.Write(buildGzipTar([]tarEntry{
			{header: tar.Header{Name: "acme-app-" + sha + "/Dockerfile", Typeflag: tar.TypeReg}, body: "FROM scratch\nLABEL commit=" + sha + "\n"},
			{header: tar.Header{Name: "acme-app-" + sha + "/app/main.txt", Typeflag: tar.TypeReg}, body: "payload " + sha},
		}))
	})

	server := httptest.NewServer(mux)
	DeferCleanup(server.Close)
	f.base = server.URL

	base, err := url.Parse(server.URL + "/")
	Expect(err).NotTo(HaveOccurred())

	f.client = github.NewClient(nil)
	f.client.BaseURL = base

	return f
}

// setRef points a tag, and the commit itself, at sha.
func (f *fakeBuildGitHub) setRef(tag, sha string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.refs[tag] = sha
	f.refs[sha] = sha
}

// counts returns how many archives and commit lookups the fake has served.
func (f *fakeBuildGitHub) counts() (archives, commits int) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.archives, f.commits
}

var _ = Describe("OCIArtifact in-cluster builds", func() {
	const (
		buildName  = "build-app"
		tag        = "v1.0.0"
		firstSHA   = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		secondSHA  = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		repoURLSrc = "https://github.com/acme/app"
	)

	ctx := context.Background()

	var (
		gh         *fakeBuildGitHub
		storageDir string
	)

	BeforeEach(func() {
		ensureOCITestNamespace()
		ensureBuildsNamespace()
		gh = newFakeBuildGitHub()
		gh.setRef(tag, firstSHA)
		storageDir = GinkgoT().TempDir()
	})

	AfterEach(func() {
		selector := client.MatchingLabels{ocibuild.LabelBuild: "true"}
		background := client.PropagationPolicy(metav1.DeletePropagationBackground)
		Expect(k8sClient.DeleteAllOf(ctx, &batchv1.Job{}, client.InNamespace(ociBuildsTestNamespace), selector, background)).To(Succeed())
		Expect(k8sClient.DeleteAllOf(ctx, &corev1.Pod{}, client.InNamespace(ociBuildsTestNamespace), selector)).To(Succeed())
		Expect(k8sClient.DeleteAllOf(ctx, &corev1.Secret{}, client.InNamespace(ociBuildsTestNamespace), selector)).To(Succeed())
	})

	// buildConfig returns an Image build of acme/app for linux/amd64 unless platforms are given.
	buildConfig := func(platforms ...string) *opendepotv1alpha1.OCIArtifactConfig {
		repoURL := repoURLSrc
		if len(platforms) == 0 {
			platforms = []string{"linux/amd64"}
		}

		return &opendepotv1alpha1.OCIArtifactConfig{
			ArtifactType:  "Image",
			Build:         &opendepotv1alpha1.OCIBuildSource{RepoOwner: "acme", RepoUrl: &repoURL},
			Platforms:     platforms,
			StorageConfig: ociStorageConfig(storageDir),
		}
	}

	// newBuildReconciler returns a reconciler with builds enabled for the acme owner. opts may change the options.
	newBuildReconciler := func(opts ...func(*VersionReconciler)) *VersionReconciler {
		r := newOCIReconciler(newFakeOCIRegistry())
		r.ociGithubClientFor = func(_ context.Context, _ *opendepotv1alpha1.Version, _ *opendepotv1alpha1.OCIArtifactConfig) (*github.Client, error) {
			return gh.client, nil
		}

		r.OCIBuilds = OCIBuildOptions{
			Enabled:           true,
			Namespace:         ociBuildsTestNamespace,
			BrokerURL:         "http://version-controller-build-broker.opendepot:8090",
			BuildKitImage:     "moby/buildkit:v0.35.1-rootless@sha256:7d02292556e66feea6d2d89b7ec143503b57e4f59a4efc6b7ece8547e4d3e5be",
			HelperImage:       "opendepot/version:test",
			MaxConcurrentJobs: 2,
			AllowedRepoOwners: []string{"acme"},
			DefaultResources: corev1.ResourceList{
				corev1.ResourceCPU:              resourceQuantity("1"),
				corev1.ResourceMemory:           resourceQuantity("2Gi"),
				corev1.ResourceEphemeralStorage: resourceQuantity("10Gi"),
			},
			MaxResources: corev1.ResourceList{
				corev1.ResourceCPU:              resourceQuantity("4"),
				corev1.ResourceMemory:           resourceQuantity("8Gi"),
				corev1.ResourceEphemeralStorage: resourceQuantity("20Gi"),
			},
		}

		for _, opt := range opts {
			opt(r)
		}

		return r
	}

	getBuild := func() *opendepotv1alpha1.OCIBuildStatus {
		return getOCIVersion(buildName).Status.OCIBuild
	}

	buildJobs := func() []batchv1.Job {
		jobs := &batchv1.JobList{}
		Expect(k8sClient.List(ctx, jobs, client.InNamespace(ociBuildsTestNamespace), client.MatchingLabels{ocibuild.LabelBuild: "true"})).To(Succeed())
		return jobs.Items
	}

	getJob := func(name string) *batchv1.Job {
		job := &batchv1.Job{}
		Expect(k8sClient.Get(ctx, k8stypes.NamespacedName{Namespace: ociBuildsTestNamespace, Name: name}, job)).To(Succeed())
		return job
	}

	secretFor := func(buildID string) (*corev1.Secret, error) {
		secret := &corev1.Secret{}
		err := k8sClient.Get(ctx, k8stypes.NamespacedName{Namespace: ociBuildsTestNamespace, Name: ocibuild.SecretName(buildID)}, secret)
		return secret, err
	}

	// seedPlatformManifest stores a single-layer image for arch in the Version store and returns its manifest digest.
	seedPlatformManifest := func(r *VersionReconciler, arch string) digest.Digest {
		version := getOCIVersion(buildName)
		blobs, err := r.ociBlobStoreFor(ctx, version)
		Expect(err).NotTo(HaveOccurred())

		configJSON, err := json.Marshal(ocispec.Image{
			Platform: ocispec.Platform{OS: "linux", Architecture: arch},
			RootFS:   ocispec.RootFS{Type: "layers"},
		})
		Expect(err).NotTo(HaveOccurred())

		layer := []byte("layer for " + arch)
		configDesc := ocispec.Descriptor{MediaType: ocispec.MediaTypeImageConfig, Digest: digest.FromBytes(configJSON), Size: int64(len(configJSON))}
		layerDesc := ocispec.Descriptor{MediaType: ocispec.MediaTypeImageLayerGzip, Digest: digest.FromBytes(layer), Size: int64(len(layer))}
		Expect(blobs.putBlob(ctx, configDesc, bytes.NewReader(configJSON))).To(Succeed())
		Expect(blobs.putBlob(ctx, layerDesc, bytes.NewReader(layer))).To(Succeed())

		manifestJSON, err := json.Marshal(ocispec.Manifest{
			MediaType: ocispec.MediaTypeImageManifest,
			Config:    configDesc,
			Layers:    []ocispec.Descriptor{layerDesc},
		})
		Expect(err).NotTo(HaveOccurred())

		d, err := blobs.store.PutManifest(ctx, blobs.namespace, blobs.repo, manifestJSON)
		Expect(err).NotTo(HaveOccurred())

		return d
	}

	// createPodFor creates a Pod that the Job controls, with the given phase, conditions, and terminated containers.
	createPodFor := func(job *batchv1.Job, phase corev1.PodPhase, conditions []corev1.PodCondition, terminated map[string]corev1.ContainerStateTerminated) {
		controller := true
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      job.Name + "-pod",
				Namespace: job.Namespace,
				Labels:    job.Spec.Template.Labels,
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion: "batch/v1",
					Kind:       "Job",
					Name:       job.Name,
					UID:        job.UID,
					Controller: &controller,
				}},
			},
			Spec: job.Spec.Template.Spec,
		}
		Expect(k8sClient.Create(ctx, pod)).To(Succeed())

		statusFor := func(name string) corev1.ContainerStatus {
			status := corev1.ContainerStatus{Name: name, Image: "test", ImageID: "test"}
			if state, ok := terminated[name]; ok {
				status.State = corev1.ContainerState{Terminated: &state}
			}

			return status
		}

		initStatuses := make([]corev1.ContainerStatus, 0, len(job.Spec.Template.Spec.InitContainers))
		statuses := make([]corev1.ContainerStatus, 0, len(job.Spec.Template.Spec.Containers))
		for _, c := range job.Spec.Template.Spec.InitContainers {
			initStatuses = append(initStatuses, statusFor(c.Name))
		}

		for _, c := range job.Spec.Template.Spec.Containers {
			statuses = append(statuses, statusFor(c.Name))
		}

		pod.Status = corev1.PodStatus{
			Phase:                 phase,
			Conditions:            conditions,
			InitContainerStatuses: initStatuses,
			ContainerStatuses:     statuses,
		}
		Expect(k8sClient.Status().Update(ctx, pod)).To(Succeed())
	}

	// setJobCondition marks the Job complete or failed. Kubernetes requires the criteria condition before the terminal
	// one, and it sets completionTime only with Complete.
	setJobCondition := func(job *batchv1.Job, condType batchv1.JobConditionType, reason, message string) {
		now := metav1.NewTime(time.Now())
		job.Status.StartTime = &now
		criteria := batchv1.JobSuccessCriteriaMet
		if condType == batchv1.JobFailed {
			criteria = batchv1.JobFailureTarget
		}

		job.Status.Conditions = append(job.Status.Conditions,
			batchv1.JobCondition{Type: criteria, Status: corev1.ConditionTrue, LastTransitionTime: now},
			batchv1.JobCondition{Type: condType, Status: corev1.ConditionTrue, Reason: reason, Message: message, LastTransitionTime: now},
		)
		if condType == batchv1.JobComplete {
			job.Status.CompletionTime = &now
		}

		Expect(k8sClient.Status().Update(ctx, job)).To(Succeed())
	}

	// markUploaded completes a platform Job whose upload reported manifestDigest.
	markUploaded := func(job *batchv1.Job, manifestDigest digest.Digest) {
		message := fmt.Sprintf(`{"manifestDigest":%q}`, manifestDigest.String())
		createPodFor(job, corev1.PodSucceeded, nil, map[string]corev1.ContainerStateTerminated{
			ociBuildKitContainerName:    {ExitCode: 0},
			ociBuildUploadContainerName: {ExitCode: 0, Message: message},
		})
		setJobCondition(job, batchv1.JobComplete, "", "")
	}

	// createLaunchedBuild reconciles the Version once, which pins the tag, stages the context, and launches the Jobs.
	launchBuild := func(r *VersionReconciler) {
		result, err := reconcileOCI(r, buildName)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(ociBuildPollInterval))
	}

	It("launches one Job per platform and a Secret of tokens, and records only their hashes", func() {
		createOCIVersion(buildName, buildConfig())
		r := newBuildReconciler()
		launchBuild(r)

		build := getBuild()
		Expect(build).NotTo(BeNil())
		Expect(build.Phase).To(Equal(ociBuildPhaseRunning))
		Expect(build.SourceCommit).To(Equal(firstSHA))
		Expect(build.SourceTag).To(Equal(tag))
		Expect(build.ContextDigest).To(HavePrefix("sha256:"))
		Expect(build.Platforms).To(HaveLen(1))
		Expect(build.Platforms[0].JobName).To(Equal(ocibuild.JobName(build.BuildID, ocibuildPlatform("linux/amd64"))))

		jobs := buildJobs()
		Expect(jobs).To(HaveLen(1))
		Expect(jobs[0].Name).To(Equal(build.Platforms[0].JobName))
		Expect(jobs[0].Annotations).To(HaveKeyWithValue(ocibuild.AnnotationVersionName, buildName))

		secret, err := secretFor(build.BuildID)
		Expect(err).NotTo(HaveOccurred())
		Expect(secret.OwnerReferences).To(HaveLen(1))
		Expect(secret.OwnerReferences[0].UID).To(Equal(jobs[0].UID))

		fetch := string(secret.Data[ocibuild.FetchTokenKey])
		upload := string(secret.Data[ocibuild.UploadTokenKey])
		Expect(build.FetchTokenHash).To(Equal("sha256:" + ocibuild.HashToken(fetch)))
		Expect(build.UploadTokenHash).To(Equal("sha256:" + ocibuild.HashToken(upload)))
		Expect(fetch).NotTo(Equal(upload))

		raw, err := json.Marshal(getOCIVersion(buildName).Status)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(raw)).NotTo(ContainSubstring(fetch))
		Expect(string(raw)).NotTo(ContainSubstring(upload))

		archives, _ := gh.counts()
		Expect(archives).To(Equal(1))
	})

	It("assembles the index, cleans up, and marks the Version synced once every platform uploads", func() {
		createOCIVersion(buildName, buildConfig())
		r := newBuildReconciler()
		launchBuild(r)

		build := getBuild()
		manifestDigest := seedPlatformManifest(r, "amd64")
		markUploaded(getJob(build.Platforms[0].JobName), manifestDigest)

		_, err := reconcileOCI(r, buildName)
		Expect(err).NotTo(HaveOccurred())

		version := getOCIVersion(buildName)
		Expect(version.Status.Synced).To(BeTrue())
		Expect(version.Status.OCIBuild.Phase).To(Equal(ociBuildPhaseSucceeded))
		Expect(version.Status.OCIManifest).NotTo(BeNil())
		Expect(version.Status.OCIManifest.MediaType).To(Equal(ocispec.MediaTypeImageIndex))
		Expect(version.Status.OCIManifest.Platforms).To(ConsistOf("linux/amd64"))

		blobs, err := r.ociBlobStoreFor(ctx, version)
		Expect(err).NotTo(HaveOccurred())
		indexBytes, err := blobs.store.GetManifest(ctx, blobs.namespace, blobs.repo, digest.Digest(version.Status.OCIManifest.Digest))
		Expect(err).NotTo(HaveOccurred())

		var index ocispec.Index
		Expect(json.Unmarshal(indexBytes, &index)).To(Succeed())
		Expect(index.Manifests).To(HaveLen(1))
		Expect(index.Manifests[0].Digest).To(Equal(manifestDigest))
		Expect(index.Annotations).To(HaveKeyWithValue(ocispec.AnnotationRevision, firstSHA))
		Expect(index.Annotations).To(HaveKeyWithValue(ocispec.AnnotationVersion, "1.0.0"))
		Expect(index.Annotations).To(HaveKeyWithValue(ocibuild.AnnotationBuildID, build.BuildID))

		Expect(buildJobs()).To(BeEmpty())
		_, err = secretFor(build.BuildID)
		Expect(k8serr.IsNotFound(err)).To(BeTrue())

		_, err = blobs.store.GetBlob(ctx, blobs.namespace, blobs.repo, digest.Digest(build.ContextDigest))
		Expect(err).To(HaveOccurred())
	})

	It("resumes a launch after a restart without creating duplicate objects or calling GitHub", func() {
		createOCIVersion(buildName, buildConfig())
		r := newBuildReconciler()

		current := getOCIVersion(buildName)
		tokens, err := newOCIBuildTokens()
		Expect(err).NotTo(HaveOccurred())

		build := &opendepotv1alpha1.OCIBuildStatus{
			BuildID:         "abcdefghijklmnopqrst",
			SourceCommit:    firstSHA,
			SourceTag:       tag,
			ContextDigest:   digest.FromString("context").String(),
			FetchTokenHash:  "sha256:" + ocibuild.HashToken(tokens.fetch),
			UploadTokenHash: "sha256:" + ocibuild.HashToken(tokens.upload),
			Phase:           ociBuildPhaseRunning,
			StartedAt:       &metav1.Time{Time: time.Now()},
			Platforms: []opendepotv1alpha1.OCIBuildPlatformStatus{
				{Platform: "linux/amd64", JobName: ocibuild.JobName("abcdefghijklmnopqrst", ocibuildPlatform("linux/amd64")), Phase: ociBuildPhasePending},
			},
		}

		run := ociBuildRun{committedAt: time.Unix(1767323045, 0), timeout: 1800, resources: r.ociBuildResources(current.Spec.OCIArtifactConfigRef.Build)}
		run.platforms = []ocibuild.Platform{ocibuildPlatform("linux/amd64")}
		Expect(r.createOCIBuildObjects(ctx, current, current.Spec.OCIArtifactConfigRef, run, build, tokens)).To(Succeed())

		current.Status.OCIBuild = build
		Expect(k8sClient.Status().Update(ctx, current)).To(Succeed())

		_, err = reconcileOCI(r, buildName)
		Expect(err).NotTo(HaveOccurred())

		Expect(buildJobs()).To(HaveLen(1))
		Expect(getBuild().BuildID).To(Equal(build.BuildID))
		Expect(getBuild().Phase).To(Equal(ociBuildPhaseRunning))

		archives, commits := gh.counts()
		Expect(archives).To(BeZero())
		Expect(commits).To(BeZero())
	})

	It("waits for a slot without staging the context when MaxConcurrentJobs are active", func() {
		createOCIVersion(buildName, buildConfig())
		r := newBuildReconciler(func(r *VersionReconciler) { r.OCIBuilds.MaxConcurrentJobs = 1 })

		other := &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "odb-otherbuild0000000000-amd64",
				Namespace: ociBuildsTestNamespace,
				Labels:    ocibuild.JobLabels("otherbuild0000000000"),
			},
			Spec: batchv1.JobSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
				RestartPolicy: corev1.RestartPolicyNever,
				Containers:    []corev1.Container{{Name: "c", Image: "busybox"}},
			}}},
		}
		Expect(k8sClient.Create(ctx, other)).To(Succeed())

		result, err := reconcileOCI(r, buildName)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(ociBuildSlotRequeue))

		version := getOCIVersion(buildName)
		Expect(version.Status.SyncStatus).To(Equal("Waiting for a build slot"))
		Expect(version.Status.OCIBuild).To(BeNil())

		archives, _ := gh.counts()
		Expect(archives).To(BeZero())
	})

	It("fails the whole build and deletes the sibling Job when one platform fails", func() {
		createOCIVersion(buildName, buildConfig("linux/amd64", "linux/arm64"))
		r := newBuildReconciler()
		launchBuild(r)

		build := getBuild()
		Expect(build.Platforms).To(HaveLen(2))
		var failed *batchv1.Job
		for _, p := range build.Platforms {
			if p.Platform == "linux/arm64" {
				failed = getJob(p.JobName)
			}
		}

		Expect(failed).NotTo(BeNil())
		createPodFor(failed, corev1.PodFailed, nil, map[string]corev1.ContainerStateTerminated{
			ociBuildKitContainerName: {ExitCode: 1, Message: "ERROR: process did not complete\x1b[0m\nstep 4/5"},
		})
		setJobCondition(failed, batchv1.JobFailed, "BackoffLimitExceeded", "job has reached the specified backoff limit")

		result, err := reconcileOCI(r, buildName)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal(ctrl.Result{}))

		build = getBuild()
		Expect(build.Phase).To(Equal(ociBuildPhaseFailed))
		Expect(build.Message).To(ContainSubstring("linux/arm64"))
		Expect(build.Message).To(ContainSubstring("did not complete"))
		Expect(build.Message).NotTo(ContainSubstring("\x1b"))
		Expect(buildJobs()).To(BeEmpty())

		version := getOCIVersion(buildName)
		Expect(version.Status.Synced).To(BeFalse())
	})

	It("fails a build whose timeout has passed", func() {
		createOCIVersion(buildName, buildConfig())
		r := newBuildReconciler()

		current := getOCIVersion(buildName)
		started := metav1.NewTime(time.Now().Add(-3 * time.Hour))
		current.Status.OCIBuild = &opendepotv1alpha1.OCIBuildStatus{
			BuildID:         "abcdefghijklmnopqrst",
			SourceCommit:    firstSHA,
			SourceTag:       tag,
			ContextDigest:   digest.FromString("context").String(),
			FetchTokenHash:  "sha256:" + strings.Repeat("a", 64),
			UploadTokenHash: "sha256:" + strings.Repeat("b", 64),
			Phase:           ociBuildPhaseRunning,
			StartedAt:       &started,
			Platforms: []opendepotv1alpha1.OCIBuildPlatformStatus{
				{Platform: "linux/amd64", JobName: "odb-abcdefghijklmnopqrst-amd64", Phase: ociBuildPhaseRunning},
			},
		}
		Expect(k8sClient.Status().Update(ctx, current)).To(Succeed())

		_, err := reconcileOCI(r, buildName)
		Expect(err).NotTo(HaveOccurred())

		build := getBuild()
		Expect(build.Phase).To(Equal(ociBuildPhaseFailed))
		Expect(build.Message).To(ContainSubstring("timeout"))
	})

	It("fails a platform that no node can schedule", func() {
		createOCIVersion(buildName, buildConfig())
		r := newBuildReconciler()
		launchBuild(r)

		job := getJob(getBuild().Platforms[0].JobName)
		createPodFor(job, corev1.PodPending, []corev1.PodCondition{{
			Type:   corev1.PodScheduled,
			Status: corev1.ConditionFalse,
			Reason: ociBuildUnschedulableReason,
		}}, nil)

		_, err := reconcileOCI(r, buildName)
		Expect(err).NotTo(HaveOccurred())

		build := getBuild()
		Expect(build.Phase).To(Equal(ociBuildPhaseFailed))
		Expect(build.Message).To(ContainSubstring("no schedulable node"))
	})

	Context("when the tag moves after a build is synced", func() {
		// syncedBuild stores a synced Version whose build was made from firstSHA.
		syncedBuild := func(immutable *bool) {
			config := buildConfig()
			config.Immutable = immutable
			createOCIVersion(buildName, config)

			current := getOCIVersion(buildName)
			current.Status.Synced = true
			current.Status.SyncStatus = ociSyncedStatus
			current.Status.OCIManifest = &opendepotv1alpha1.OCIManifestStatus{
				Digest:    "sha256:" + strings.Repeat("c", 64),
				MediaType: ocispec.MediaTypeImageIndex,
			}
			current.Status.OCIBuild = &opendepotv1alpha1.OCIBuildStatus{
				BuildID:         "abcdefghijklmnopqrst",
				SourceCommit:    firstSHA,
				SourceTag:       tag,
				ContextDigest:   digest.FromString("context").String(),
				FetchTokenHash:  "sha256:" + strings.Repeat("a", 64),
				UploadTokenHash: "sha256:" + strings.Repeat("b", 64),
				Phase:           ociBuildPhaseSucceeded,
			}
			Expect(k8sClient.Status().Update(ctx, current)).To(Succeed())
		}

		It("records drift and never rebuilds while immutable is unset", func() {
			syncedBuild(nil)
			gh.setRef(tag, secondSHA)
			r := newBuildReconciler()

			result, err := reconcileOCI(r, buildName)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(ociDriftRequeueInterval))

			version := getOCIVersion(buildName)
			Expect(version.Status.Synced).To(BeTrue())
			Expect(version.Status.OCIManifest.TagDrift).To(BeTrue())
			Expect(version.Status.SyncStatus).To(ContainSubstring("immutable is set"))
			Expect(version.Status.OCIBuild.BuildID).To(Equal("abcdefghijklmnopqrst"))
			Expect(buildJobs()).To(BeEmpty())
		})

		It("rebuilds the moved tag when immutable is false", func() {
			immutable := false
			syncedBuild(&immutable)
			gh.setRef(tag, secondSHA)
			r := newBuildReconciler()

			_, err := reconcileOCI(r, buildName)
			Expect(err).NotTo(HaveOccurred())

			build := getBuild()
			Expect(build.BuildID).NotTo(Equal("abcdefghijklmnopqrst"))
			Expect(build.SourceCommit).To(Equal(secondSHA))
			Expect(build.Phase).To(Equal(ociBuildPhaseRunning))
			Expect(buildJobs()).To(HaveLen(1))
		})

		It("ignores forceSync for an immutable synced build and clears it", func() {
			immutable := true
			syncedBuild(&immutable)
			current := getOCIVersion(buildName)
			current.Spec.ForceSync = true
			Expect(k8sClient.Update(ctx, current)).To(Succeed())
			r := newBuildReconciler()

			_, err := reconcileOCI(r, buildName)
			Expect(err).NotTo(HaveOccurred())

			version := getOCIVersion(buildName)
			Expect(version.Spec.ForceSync).To(BeFalse())
			Expect(version.Status.OCIBuild.BuildID).To(Equal("abcdefghijklmnopqrst"))
			Expect(version.Status.OCIManifest.TagDrift).To(BeFalse())
			Expect(buildJobs()).To(BeEmpty())
		})
	})

	It("does not retry a failed build until forceSync is set", func() {
		createOCIVersion(buildName, buildConfig())
		current := getOCIVersion(buildName)
		current.Status.OCIBuild = &opendepotv1alpha1.OCIBuildStatus{
			BuildID:         "abcdefghijklmnopqrst",
			SourceCommit:    firstSHA,
			SourceTag:       tag,
			FetchTokenHash:  "sha256:" + strings.Repeat("a", 64),
			UploadTokenHash: "sha256:" + strings.Repeat("b", 64),
			Phase:           ociBuildPhaseFailed,
			Message:         "earlier failure",
		}
		Expect(k8sClient.Status().Update(ctx, current)).To(Succeed())
		r := newBuildReconciler()

		_, err := reconcileOCI(r, buildName)
		Expect(err).NotTo(HaveOccurred())
		Expect(getBuild().BuildID).To(Equal("abcdefghijklmnopqrst"))
		Expect(buildJobs()).To(BeEmpty())

		current = getOCIVersion(buildName)
		current.Spec.ForceSync = true
		Expect(k8sClient.Update(ctx, current)).To(Succeed())

		_, err = reconcileOCI(r, buildName)
		Expect(err).NotTo(HaveOccurred())
		Expect(getBuild().BuildID).NotTo(Equal("abcdefghijklmnopqrst"))
		Expect(getBuild().Phase).To(Equal(ociBuildPhaseRunning))
		Expect(getOCIVersion(buildName).Spec.ForceSync).To(BeFalse())
	})

	It("refuses a build while in-cluster builds are disabled", func() {
		createOCIVersion(buildName, buildConfig())
		r := newBuildReconciler(func(r *VersionReconciler) { r.OCIBuilds.Enabled = false })

		_, err := reconcileOCI(r, buildName)
		Expect(err).NotTo(HaveOccurred())

		version := getOCIVersion(buildName)
		Expect(version.Status.Synced).To(BeFalse())
		Expect(version.Status.SyncStatus).To(Equal("In-cluster builds are disabled"))
		Expect(version.Status.OCIBuild).To(BeNil())
		Expect(buildJobs()).To(BeEmpty())
	})

	It("refuses a repository owner that is not on the allow-list", func() {
		createOCIVersion(buildName, buildConfig())
		r := newBuildReconciler(func(r *VersionReconciler) { r.OCIBuilds.AllowedRepoOwners = []string{"someone-else"} })

		_, err := reconcileOCI(r, buildName)
		Expect(err).NotTo(HaveOccurred())

		version := getOCIVersion(buildName)
		Expect(version.Status.Synced).To(BeFalse())
		Expect(version.Status.SyncStatus).To(Equal("Repository owner acme is not on the build allow-list"))
		Expect(buildJobs()).To(BeEmpty())
	})

	It("refuses a build when scanning is required and disabled", func() {
		createOCIVersion(buildName, buildConfig())
		r := newBuildReconciler(func(r *VersionReconciler) { r.OCIBuilds.RequireScanning = true })

		_, err := reconcileOCI(r, buildName)
		Expect(err).NotTo(HaveOccurred())

		version := getOCIVersion(buildName)
		Expect(version.Status.Synced).To(BeFalse())
		Expect(version.Status.SyncStatus).To(ContainSubstring("require scanning"))
		Expect(buildJobs()).To(BeEmpty())
	})

	It("scans the assembled index through the shared scan path before the Version is synced", func() {
		createOCIVersion(buildName, buildConfig())
		r := newBuildReconciler(func(r *VersionReconciler) {
			r.ScanningEnabled = true
			r.TrivyCacheDir = "/cache/trivy"
		})
		launchBuild(r)

		build := getBuild()
		manifestDigest := seedPlatformManifest(r, "amd64")
		markUploaded(getJob(build.Platforms[0].JobName), manifestDigest)

		var scannedArgs []string
		var layoutSeenIndex bool
		stubTrivy(ociTrivyReport, func(a []string) {
			scannedArgs = a
			layoutDir := argValue(a, "--input")
			_, statErr := os.Stat(layoutDir + "/index.json")
			layoutSeenIndex = statErr == nil
		})

		_, err := reconcileOCI(r, buildName)
		Expect(err).NotTo(HaveOccurred())

		Expect(argValue(scannedArgs, "--input")).NotTo(BeEmpty())
		Expect(layoutSeenIndex).To(BeTrue())

		version := getOCIVersion(buildName)
		Expect(version.Status.Synced).To(BeTrue())
		Expect(version.Status.OCIScan).NotTo(BeNil())
		Expect(version.Status.OCIScan.Findings).To(HaveLen(1))
		Expect(version.Status.OCIScan.Findings[0].VulnerabilityID).To(Equal(ociTestCVE))
	})

	It("deletes the Jobs and Secret of a build when its Version is deleted", func() {
		createOCIVersion(buildName, buildConfig())
		r := newBuildReconciler()
		launchBuild(r)
		buildID := getBuild().BuildID

		current := getOCIVersion(buildName)
		Expect(k8sClient.Delete(ctx, current)).To(Succeed())

		_, err := reconcileOCI(r, buildName)
		Expect(err).NotTo(HaveOccurred())

		Expect(buildJobs()).To(BeEmpty())
		_, err = secretFor(buildID)
		Expect(k8serr.IsNotFound(err)).To(BeTrue())
	})
})

// ensureBuildsNamespace creates the builds namespace that the build specs use. It is safe to call more than once.
func ensureBuildsNamespace() {
	err := k8sClient.Create(context.Background(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ociBuildsTestNamespace}})
	if err != nil && !k8serr.IsAlreadyExists(err) {
		Expect(err).NotTo(HaveOccurred())
	}
}

// resourceQuantity parses a quantity for a test ResourceList.
func resourceQuantity(s string) resource.Quantity {
	return resource.MustParse(s)
}

// ocibuildPlatform parses a platform that a test names.
func ocibuildPlatform(s string) ocibuild.Platform {
	p, err := ocibuild.ParsePlatform(s)
	Expect(err).NotTo(HaveOccurred())

	return p
}
