package ocibuild

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/opencontainers/go-digest"
	"github.com/tonedefdev/opendepot/api/v1alpha1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Names and keys that the build Job, its Secret, and the broker share.
const (
	// ServiceAccountName is the service account that build Pods run as. It has no RBAC bindings and no token.
	ServiceAccountName = "opendepot-build"

	// LabelBuild marks every build Pod, so that the NetworkPolicy can select them.
	LabelBuild = "opendepot.defdev.io/build"
	// BuildIDKey labels and annotates the build's Jobs, Pods, and Secret with the build ID.
	BuildIDKey = "opendepot.defdev.io/build-id"
	// AnnotationVersionNamespace and AnnotationVersionName let the Job and Pod event watches find the Version. Label
	// values are too short to hold a namespace and name.
	AnnotationVersionNamespace = "opendepot.defdev.io/version-namespace"
	AnnotationVersionName      = "opendepot.defdev.io/version-name"

	// FetchTokenKey and UploadTokenKey are the keys of the build Secret. Each key is mounted only in its own container.
	FetchTokenKey  = "fetch-token"
	UploadTokenKey = "upload-token"

	// FetchTokenMountPath and UploadTokenMountPath are where the helper containers read their token file.
	FetchTokenMountPath  = "/var/run/secrets/opendepot-build/fetch"
	UploadTokenMountPath = "/var/run/secrets/opendepot-build/upload"

	// WorkspaceMountPath holds the extracted build context, written by the fetch helper.
	WorkspaceMountPath = "/workspace"
	// OutputMountPath holds the OCI layout that BuildKit writes.
	OutputMountPath = "/output"
	// LayoutPath is the directory that BuildKit writes the OCI layout to, read by the upload helper.
	LayoutPath = OutputMountPath + "/layout"

	// SnapshotterAuto and SnapshotterNative are the accepted values of JobParams.Snapshotter.
	SnapshotterAuto   = "auto"
	SnapshotterNative = "native"

	// BuildKitUID is the UID and GID of the rootless BuildKit container.
	BuildKitUID int64 = 1000
	// HelperUID is the UID and GID of the fetch and upload helper containers.
	HelperUID int64 = 65532

	// MaxBuildArgs is the largest number of build arguments that one build accepts.
	MaxBuildArgs = 32
	// MaxTimeoutSeconds is the longest activeDeadlineSeconds that a build Job may have.
	MaxTimeoutSeconds int64 = 7200
)

const (
	secretNamePrefix            = "odb-"
	buildKitCommand             = "buildctl-daemonless.sh"
	buildKitDataDir             = "/home/user/.local/share/buildkit"
	buildKitStateDir            = "/home/user/.local/tmp"
	buildKitRuntimeDir          = "/run/user/1000"
	buildKitDockerConfig        = "/tmp/docker-config"
	maxBuildIDLength            = 36
	maxBuildArgValueBytes       = 4096
	maxTargetLength             = 128
	secretVolumeMode      int32 = 0o440
)

var (
	buildIDPattern       = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]{0,34}[a-z0-9])?$`)
	buildArgNamePattern  = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	buildArgValuePattern = regexp.MustCompile(`^[^\x00-\x08\x0a-\x1f\x7f]*$`)
	targetPattern        = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
)

// scratchSizeLimit caps the emptyDirs that hold small runtime state rather than build data.
var scratchSizeLimit = resource.MustParse("256Mi")

// JobParams describes one platform's build Job. Call Validate before RenderJob.
type JobParams struct {
	// Namespace is the builds namespace, where the Job, its Pod, and the Secret are created.
	Namespace string
	// BuildID identifies the build. It must be a DNS-1123 label of at most 36 characters.
	BuildID string
	// Platform is the platform that this Job builds.
	Platform Platform
	// VersionNamespace and VersionName identify the Version that owns the build.
	VersionNamespace string
	VersionName      string
	// BrokerURL is the base URL of the build broker, such as http://version-controller-build-broker.<ns>:8090.
	BrokerURL string
	// ContextDigest is the digest of the staged context archive. The fetch helper verifies it.
	ContextDigest digest.Digest
	// Target is an optional build stage, as in docker build --target.
	Target string
	// BuildArgs are non-secret build arguments.
	BuildArgs []v1alpha1.OCIBuildArg
	// SourceDateEpoch is the commit time in seconds since the epoch. It is passed as SOURCE_DATE_EPOCH.
	SourceDateEpoch int64
	// BuildKitImage is the pinned moby/buildkit rootless image, with a digest.
	BuildKitImage string
	// HelperImage is the version-controller image, which runs the fetch and upload helper modes.
	HelperImage string
	// ActiveDeadlineSeconds is the clamped build timeout.
	ActiveDeadlineSeconds int64
	// TTLSecondsAfterFinished is how long the finished Job is kept before the TTL controller removes it.
	TTLSecondsAfterFinished int32
	// Resources are the clamped compute resources of the buildkit container.
	Resources corev1.ResourceRequirements
	// VolumeSizeLimit caps each emptyDir that holds build data: the workspace, the output, and the BuildKit state.
	VolumeSizeLimit resource.Quantity
	// NodeSelector is merged with the platform selector. The platform keys take precedence.
	NodeSelector map[string]string
	// Tolerations, PriorityClassName, and RuntimeClassName come from the chart.
	Tolerations       []corev1.Toleration
	PriorityClassName string
	RuntimeClassName  *string
	// UserNamespaces sets hostUsers: false. The controller must verify the UID map at runtime before it relies on this.
	UserNamespaces bool
	// Snapshotter is SnapshotterAuto (the default when empty) or SnapshotterNative.
	Snapshotter string
	// FetchToken and UploadToken are the raw build tokens. Only their hashes are stored in status.
	FetchToken  string
	UploadToken string
}

// JobName returns the Job name for one platform of a build: odb-<buildID>-<arch>[-<variant>].
func JobName(buildID string, platform Platform) string {
	return secretNamePrefix + buildID + "-" + platform.JobSuffix()
}

// SecretName returns the name of the Secret that holds a build's two tokens: odb-<buildID>.
func SecretName(buildID string) string {
	return secretNamePrefix + buildID
}

// JobLabels returns the labels of a build's Jobs, Pods, and Secret.
func JobLabels(buildID string) map[string]string {
	return map[string]string{
		LabelBuild: "true",
		BuildIDKey: buildID,
	}
}

// JobAnnotations returns the annotations that identify the Version that owns a build.
func JobAnnotations(versionNamespace, versionName, buildID string) map[string]string {
	return map[string]string{
		AnnotationVersionNamespace: versionNamespace,
		AnnotationVersionName:      versionName,
		BuildIDKey:                 buildID,
	}
}

// Validate checks the parameters that RenderJob relies on. The controller calls it before it creates anything.
func (p JobParams) Validate() error {
	if p.Namespace == "" {
		return errors.New("build namespace is required")
	}

	if len(p.BuildID) > maxBuildIDLength || !buildIDPattern.MatchString(p.BuildID) {
		return fmt.Errorf("build ID %q must be a DNS-1123 label of at most %d characters", p.BuildID, maxBuildIDLength)
	}

	if _, err := ParsePlatform(p.Platform.String()); err != nil {
		return err
	}

	if p.VersionNamespace == "" || p.VersionName == "" {
		return errors.New("version namespace and name are required")
	}

	if p.BrokerURL == "" {
		return errors.New("build broker URL is required")
	}

	if err := p.ContextDigest.Validate(); err != nil || p.ContextDigest.Algorithm() != digest.SHA256 {
		return fmt.Errorf("context digest %q must be sha256", p.ContextDigest)
	}

	if p.Target != "" && (len(p.Target) > maxTargetLength || !targetPattern.MatchString(p.Target)) {
		return fmt.Errorf("target %q is not a valid build stage name", p.Target)
	}

	if err := validateBuildArgs(p.BuildArgs); err != nil {
		return err
	}

	return p.validateRuntime()
}

func (p JobParams) validateRuntime() error {
	if p.SourceDateEpoch < 0 {
		return errors.New("source date epoch must not be negative")
	}

	if p.BuildKitImage == "" || p.HelperImage == "" {
		return errors.New("BuildKit and helper images are required")
	}

	if p.ActiveDeadlineSeconds <= 0 || p.ActiveDeadlineSeconds > MaxTimeoutSeconds {
		return fmt.Errorf("active deadline must be between 1 and %d seconds", MaxTimeoutSeconds)
	}

	if p.TTLSecondsAfterFinished <= 0 {
		return errors.New("TTL after finished must be positive")
	}

	if p.FetchToken == "" || p.UploadToken == "" || p.FetchToken == p.UploadToken {
		return errors.New("fetch and upload tokens must be set and distinct")
	}

	switch p.Snapshotter {
	case "", SnapshotterAuto, SnapshotterNative:
	default:
		return fmt.Errorf("snapshotter %q must be %s or %s", p.Snapshotter, SnapshotterAuto, SnapshotterNative)
	}

	if p.VolumeSizeLimit.IsZero() {
		return errors.New("volume size limit is required")
	}

	return requireComputeResources(p.Resources)
}

func validateBuildArgs(args []v1alpha1.OCIBuildArg) error {
	if len(args) > MaxBuildArgs {
		return fmt.Errorf("at most %d build arguments are allowed", MaxBuildArgs)
	}

	for _, arg := range args {
		if !buildArgNamePattern.MatchString(arg.Name) || len(arg.Name) > 128 {
			return fmt.Errorf("build argument name %q is invalid", arg.Name)
		}

		if strings.HasPrefix(arg.Name, "BUILDKIT_") || arg.Name == "SOURCE_DATE_EPOCH" {
			return fmt.Errorf("build argument %q is reserved", arg.Name)
		}

		if len(arg.Value) > maxBuildArgValueBytes || !buildArgValuePattern.MatchString(arg.Value) {
			return fmt.Errorf("build argument %q has an invalid value", arg.Name)
		}
	}

	return nil
}

// requireComputeResources checks that cpu, memory, and ephemeral storage are set, as both requests and limits.
func requireComputeResources(rr corev1.ResourceRequirements) error {
	for _, name := range []corev1.ResourceName{
		corev1.ResourceCPU,
		corev1.ResourceMemory,
		corev1.ResourceEphemeralStorage,
	} {
		request, hasRequest := rr.Requests[name]
		limit, hasLimit := rr.Limits[name]
		if !hasRequest || !hasLimit || request.IsZero() || limit.IsZero() {
			return fmt.Errorf("resource %s needs a request and a limit", name)
		}
	}

	return nil
}

// RenderJob returns the build Job for one platform and the Secret that holds the build's two tokens. It renders the
// rootless BuildKit posture that the B0 spike verified. The Job has three containers, and each token is mounted in
// one container only:
//
//   - fetch-context (init, restricted): mounts the fetch token and writes the context to the workspace.
//   - buildkit (init, rootless UID 1000): mounts no token and no credentials. It reads the workspace and writes the
//     OCI layout to the output volume, with tar=false and rewrite-timestamp=true.
//   - upload (main, restricted): mounts the upload token and reads the output. It runs only after BuildKit exits.
//
// The helper containers receive their arguments as a list: --build-helper, --build-url, --token-file, and either
// --context-digest (fetch) or --layout and --platform (upload). The Secret has no owner references. The controller
// adds them after it creates the platform Jobs. RenderJob does not validate its input, so call Validate first.
func RenderJob(p JobParams) (*batchv1.Job, *corev1.Secret) {
	labels := JobLabels(p.BuildID)
	annotations := JobAnnotations(p.VersionNamespace, p.VersionName, p.BuildID)
	secretName := SecretName(p.BuildID)

	podSpec := corev1.PodSpec{
		RestartPolicy:                corev1.RestartPolicyNever,
		ServiceAccountName:           ServiceAccountName,
		AutomountServiceAccountToken: ptrTo(false),
		EnableServiceLinks:           ptrTo(false),
		ShareProcessNamespace:        ptrTo(false),
		NodeSelector:                 platformNodeSelector(p),
		Tolerations:                  p.Tolerations,
		PriorityClassName:            p.PriorityClassName,
		RuntimeClassName:             p.RuntimeClassName,
		SecurityContext: &corev1.PodSecurityContext{
			RunAsNonRoot:   ptrTo(true),
			RunAsUser:      ptrTo(BuildKitUID),
			RunAsGroup:     ptrTo(BuildKitUID),
			FSGroup:        ptrTo(BuildKitUID),
			SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
		},
		InitContainers: []corev1.Container{
			fetchContainer(p),
			buildKitContainer(p),
		},
		Containers: []corev1.Container{
			uploadContainer(p),
		},
		Volumes: buildVolumes(p, secretName),
	}

	if p.UserNamespaces {
		podSpec.HostUsers = ptrTo(false)
	}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:        JobName(p.BuildID, p.Platform),
			Namespace:   p.Namespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: batchv1.JobSpec{
			Completions:             ptrTo(int32(1)),
			Parallelism:             ptrTo(int32(1)),
			BackoffLimit:            ptrTo(int32(0)),
			ActiveDeadlineSeconds:   &p.ActiveDeadlineSeconds,
			TTLSecondsAfterFinished: &p.TTLSecondsAfterFinished,
			PodFailurePolicy:        podFailurePolicy(),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels, Annotations: annotations},
				Spec:       podSpec,
			},
		},
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: p.Namespace,
			Labels:    labels,
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			FetchTokenKey:  []byte(p.FetchToken),
			UploadTokenKey: []byte(p.UploadToken),
		},
	}

	return job, secret
}

func platformNodeSelector(p JobParams) map[string]string {
	selector := make(map[string]string, len(p.NodeSelector)+2)
	for k, v := range p.NodeSelector {
		selector[k] = v
	}

	selector["kubernetes.io/os"] = p.Platform.OS
	selector["kubernetes.io/arch"] = p.Platform.Architecture
	return selector
}

// podFailurePolicy ignores disruptions, such as node drains, and fails the Job on any non-zero exit code.
func podFailurePolicy() *batchv1.PodFailurePolicy {
	return &batchv1.PodFailurePolicy{
		Rules: []batchv1.PodFailurePolicyRule{
			{
				Action: batchv1.PodFailurePolicyActionIgnore,
				OnPodConditions: []batchv1.PodFailurePolicyOnPodConditionsPattern{{
					Type:   corev1.DisruptionTarget,
					Status: corev1.ConditionTrue,
				}},
			},
			{
				Action: batchv1.PodFailurePolicyActionFailJob,
				OnExitCodes: &batchv1.PodFailurePolicyOnExitCodesRequirement{
					Operator: batchv1.PodFailurePolicyOnExitCodesOpNotIn,
					Values:   []int32{0},
				},
			},
		},
	}
}

// fetchContainer is the restricted init container that writes the verified context to the workspace.
func fetchContainer(p JobParams) corev1.Container {
	return corev1.Container{
		Name:                     "fetch-context",
		Image:                    p.HelperImage,
		Args:                     helperArgs("fetch", p, FetchTokenMountPath+"/"+FetchTokenKey),
		Resources:                helperResources(),
		SecurityContext:          restrictedSecurityContext(HelperUID),
		TerminationMessagePolicy: corev1.TerminationMessageReadFile,
		VolumeMounts: []corev1.VolumeMount{
			{Name: FetchTokenKey, MountPath: FetchTokenMountPath, ReadOnly: true},
			{Name: "workspace", MountPath: WorkspaceMountPath},
		},
	}
}

// buildKitContainer is the rootless BuildKit init container. It holds no token, so nothing it runs can read one.
func buildKitContainer(p JobParams) corev1.Container {
	return corev1.Container{
		Name:                     "buildkit",
		Image:                    p.BuildKitImage,
		Command:                  []string{buildKitCommand},
		Args:                     buildKitArgs(p),
		Env:                      buildKitEnv(p),
		Resources:                p.Resources,
		SecurityContext:          buildKitSecurityContext(),
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
		VolumeMounts: []corev1.VolumeMount{
			{Name: "workspace", MountPath: WorkspaceMountPath, ReadOnly: true},
			{Name: "output", MountPath: OutputMountPath},
			{Name: "buildkitd", MountPath: buildKitDataDir},
			{Name: "rootlesskit", MountPath: buildKitStateDir},
			{Name: "run", MountPath: buildKitRuntimeDir},
			{Name: "tmp", MountPath: "/tmp"},
		},
	}
}

// uploadContainer is the restricted main container. It runs after BuildKit has exited, and it holds the upload token.
func uploadContainer(p JobParams) corev1.Container {
	return corev1.Container{
		Name:            "upload",
		Image:           p.HelperImage,
		Args:            helperArgs("upload", p, UploadTokenMountPath+"/"+UploadTokenKey),
		Resources:       helperResources(),
		SecurityContext: restrictedSecurityContext(HelperUID),
		VolumeMounts: []corev1.VolumeMount{
			{Name: UploadTokenKey, MountPath: UploadTokenMountPath, ReadOnly: true},
			{Name: "output", MountPath: OutputMountPath, ReadOnly: true},
		},
	}
}

// buildKitArgs returns the argv for buildctl-daemonless.sh. No shell is involved.
func buildKitArgs(p JobParams) []string {
	args := []string{
		"build",
		"--frontend", "dockerfile.v0",
		"--local", "context=" + WorkspaceMountPath + "/context",
		"--local", "dockerfile=" + WorkspaceMountPath + "/dockerfile",
		"--opt", "filename=" + dockerfileName,
		"--opt", "platform=" + p.Platform.String(),
	}

	if p.Target != "" {
		args = append(args, "--opt", "target="+p.Target)
	}

	args = append(args, "--opt", "build-arg:SOURCE_DATE_EPOCH="+strconv.FormatInt(p.SourceDateEpoch, 10))
	for _, arg := range p.BuildArgs {
		args = append(args, "--opt", "build-arg:"+arg.Name+"="+arg.Value)
	}

	return append(args, "--output", "type=oci,dest="+LayoutPath+",tar=false,rewrite-timestamp=true")
}

func buildKitEnv(p JobParams) []corev1.EnvVar {
	flags := []string{"--oci-worker-no-process-sandbox"}
	if p.Snapshotter == SnapshotterNative {
		flags = append(flags, "--oci-worker-snapshotter=native")
	}

	return []corev1.EnvVar{
		{Name: "BUILDKITD_FLAGS", Value: strings.Join(flags, " ")},
		{Name: "DOCKER_CONFIG", Value: buildKitDockerConfig},
	}
}

// helperArgs returns the arguments for a helper mode of the version-controller image.
func helperArgs(mode string, p JobParams, tokenFile string) []string {
	args := []string{
		"--build-helper=" + mode,
		"--build-url=" + buildURL(p),
		"--token-file=" + tokenFile,
	}

	if mode == "fetch" {
		return append(args, "--context-digest="+p.ContextDigest.String())
	}

	return append(args, "--layout="+LayoutPath, "--platform="+p.Platform.String())
}

// buildURL is the broker route for this build: /internal/builds/v1/{namespace}/{version}/{buildID}.
func buildURL(p JobParams) string {
	return strings.TrimSuffix(p.BrokerURL, "/") + "/internal/builds/v1/" +
		p.VersionNamespace + "/" + p.VersionName + "/" + p.BuildID
}

// buildKitSecurityContext is the posture that rootless BuildKit needs on the B0 spike cluster. Seccomp and AppArmor
// are unconfined, privilege escalation is allowed for the setuid newuidmap and newgidmap, and only SETUID and SETGID
// are added. The namespace is labelled privileged, and the ValidatingAdmissionPolicy keeps the pod to this shape.
func buildKitSecurityContext() *corev1.SecurityContext {
	return &corev1.SecurityContext{
		RunAsNonRoot:             ptrTo(true),
		RunAsUser:                ptrTo(BuildKitUID),
		RunAsGroup:               ptrTo(BuildKitUID),
		AllowPrivilegeEscalation: ptrTo(true),
		ReadOnlyRootFilesystem:   ptrTo(true),
		ProcMount:                ptrTo(corev1.DefaultProcMount),
		Capabilities: &corev1.Capabilities{
			Drop: []corev1.Capability{"ALL"},
			Add:  []corev1.Capability{"SETUID", "SETGID"},
		},
		SeccompProfile:  &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeUnconfined},
		AppArmorProfile: &corev1.AppArmorProfile{Type: corev1.AppArmorProfileTypeUnconfined},
	}
}

// restrictedSecurityContext satisfies the Restricted Pod Security Standard.
func restrictedSecurityContext(uid int64) *corev1.SecurityContext {
	return &corev1.SecurityContext{
		RunAsNonRoot:             ptrTo(true),
		RunAsUser:                ptrTo(uid),
		RunAsGroup:               ptrTo(uid),
		AllowPrivilegeEscalation: ptrTo(false),
		ReadOnlyRootFilesystem:   ptrTo(true),
		Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
		SeccompProfile:           &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
	}
}

// helperResources are the fixed compute resources of the fetch and upload containers.
func helperResources() corev1.ResourceRequirements {
	return corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:              resource.MustParse("100m"),
			corev1.ResourceMemory:           resource.MustParse("128Mi"),
			corev1.ResourceEphemeralStorage: resource.MustParse("256Mi"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:              resource.MustParse("500m"),
			corev1.ResourceMemory:           resource.MustParse("256Mi"),
			corev1.ResourceEphemeralStorage: resource.MustParse("1Gi"),
		},
	}
}

func buildVolumes(p JobParams, secretName string) []corev1.Volume {
	return []corev1.Volume{
		emptyDirVolume("workspace", p.VolumeSizeLimit),
		emptyDirVolume("output", p.VolumeSizeLimit),
		emptyDirVolume("buildkitd", p.VolumeSizeLimit),
		emptyDirVolume("rootlesskit", scratchSizeLimit),
		emptyDirVolume("run", scratchSizeLimit),
		emptyDirVolume("tmp", scratchSizeLimit),
		secretVolume(FetchTokenKey, secretName, FetchTokenKey),
		secretVolume(UploadTokenKey, secretName, UploadTokenKey),
	}
}

func emptyDirVolume(name string, sizeLimit resource.Quantity) corev1.Volume {
	return corev1.Volume{
		Name: name,
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{SizeLimit: &sizeLimit},
		},
	}
}

// secretVolume projects one key of the build Secret. The volume name is the key, so each container sees one token.
func secretVolume(name, secretName, key string) corev1.Volume {
	return corev1.Volume{
		Name: name,
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName:  secretName,
				Items:       []corev1.KeyToPath{{Key: key, Path: key}},
				DefaultMode: ptrTo(secretVolumeMode),
			},
		},
	}
}

func ptrTo[T any](v T) *T {
	return &v
}
