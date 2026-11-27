package ocibuild

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/tonedefdev/opendepot/api/v1alpha1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	sigsyaml "sigs.k8s.io/yaml"
)

const (
	testFetchContainer  = "fetch-context"
	testBuildKitName    = "buildkit"
	testUploadName      = "upload"
	testSecretName      = "odb-" + testBuildID
	testBuildKitDataDir = "/home/user/.local/share/buildkit"
)

func mustPlatform(t *testing.T, s string) Platform {
	t.Helper()

	p, err := ParsePlatform(s)
	if err != nil {
		t.Fatalf("ParsePlatform(%q): %v", s, err)
	}

	return p
}

func goldenParams(t *testing.T) JobParams {
	t.Helper()

	return JobParams{
		Namespace:        testNamespace,
		BuildID:          testBuildID,
		Platform:         mustPlatform(t, testPlatformArmV7),
		VersionNamespace: "opendepot",
		VersionName:      "acme-modules-1.2.3",
		BrokerURL:        testBrokerURL,
		ContextDigest:    digest.Digest("sha256:" + strings.Repeat("a", 64)),
		Target:           "release",
		BuildArgs:        []v1alpha1.OCIBuildArg{{Name: "APP_VERSION", Value: "a=b,c"}},
		SourceDateEpoch:  1767225600,
		BuildKitImage:    testBuildKitImage,
		HelperImage:      testHelperImage,

		ActiveDeadlineSeconds:   900,
		TTLSecondsAfterFinished: 600,
		Resources:               buildKitResources("2", "4Gi", "10Gi"),
		VolumeSizeLimit:         resource.MustParse("10Gi"),
		NodeSelector:            map[string]string{"opendepot.defdev.io/pool": "builders"},
		PriorityClassName:       "opendepot-builds",
		FetchToken:              testFetchToken,
		UploadToken:             testUploadToken,
	}
}

func buildKitResources(cpu, memory, eph string) corev1.ResourceRequirements {
	list := corev1.ResourceList{
		corev1.ResourceCPU:              resource.MustParse(cpu),
		corev1.ResourceMemory:           resource.MustParse(memory),
		corev1.ResourceEphemeralStorage: resource.MustParse(eph),
	}

	return corev1.ResourceRequirements{Requests: list, Limits: list}
}

func allContainers(job *batchv1.Job) []corev1.Container {
	return append(slices.Clone(job.Spec.Template.Spec.InitContainers), job.Spec.Template.Spec.Containers...)
}

func findContainer(t *testing.T, job *batchv1.Job, name string) corev1.Container {
	t.Helper()

	for _, c := range allContainers(job) {
		if c.Name == name {
			return c
		}
	}

	t.Fatalf("container %q not found", name)
	return corev1.Container{}
}

func mountsOf(c corev1.Container) map[string]corev1.VolumeMount {
	out := map[string]corev1.VolumeMount{}
	for _, m := range c.VolumeMounts {
		out[m.Name] = m
	}

	return out
}

func assertRestricted(t *testing.T, c corev1.Container, uid int64) {
	t.Helper()

	sc := c.SecurityContext
	if sc == nil {
		t.Fatalf("%s: no security context", c.Name)
	}

	if !*sc.RunAsNonRoot || *sc.RunAsUser != uid || *sc.RunAsGroup != uid {
		t.Errorf("%s: run as %d/%d non-root=%v, want uid %d", c.Name, *sc.RunAsUser, *sc.RunAsGroup, *sc.RunAsNonRoot, uid)
	}

	if *sc.AllowPrivilegeEscalation {
		t.Errorf("%s: allowPrivilegeEscalation must be false", c.Name)
	}

	if !*sc.ReadOnlyRootFilesystem {
		t.Errorf("%s: readOnlyRootFilesystem must be true", c.Name)
	}

	if sc.Capabilities == nil || !slices.Equal(sc.Capabilities.Drop, []corev1.Capability{"ALL"}) ||
		len(sc.Capabilities.Add) != 0 {
		t.Errorf("%s: capabilities must drop ALL and add nothing: %+v", c.Name, sc.Capabilities)
	}

	if sc.SeccompProfile == nil || sc.SeccompProfile.Type != corev1.SeccompProfileTypeRuntimeDefault {
		t.Errorf("%s: seccomp must be RuntimeDefault", c.Name)
	}

	if sc.Privileged != nil && *sc.Privileged {
		t.Errorf("%s: must not be privileged", c.Name)
	}
}

func TestRenderJobGolden(t *testing.T) {
	job, secret := RenderJob(goldenParams(t))

	got, err := sigsyaml.Marshal(job)
	if err != nil {
		t.Fatalf("marshal job: %v", err)
	}

	golden := filepath.Join("testdata", "job.golden.yaml")
	if os.Getenv("OCIBUILD_UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(golden, got, 0o644); err != nil {
			t.Fatalf("update golden: %v", err)
		}
	}

	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}

	if !bytes.Equal(got, want) {
		t.Fatalf("rendered Job differs from %s; rerun with OCIBUILD_UPDATE_GOLDEN=1 after reviewing the change\n%s",
			golden, got)
	}

	if secret.Name != testSecretName || secret.Type != corev1.SecretTypeOpaque {
		t.Fatalf("secret = %s/%s", secret.Name, secret.Type)
	}

	if string(secret.Data[FetchTokenKey]) != testFetchToken || string(secret.Data[UploadTokenKey]) != testUploadToken {
		t.Fatalf("secret data does not carry both tokens")
	}

	if len(secret.OwnerReferences) != 0 {
		t.Fatal("RenderJob must not set owner references; the controller adds them after creating the Jobs")
	}
}

func TestRenderJobPodSpec(t *testing.T) {
	job, _ := RenderJob(goldenParams(t))
	spec := job.Spec.Template.Spec

	if job.Name != "odb-"+testBuildID+"-arm-v7" || job.Namespace != testNamespace {
		t.Fatalf("job name/namespace = %s/%s", job.Namespace, job.Name)
	}

	if *job.Spec.BackoffLimit != 0 || *job.Spec.Completions != 1 || *job.Spec.Parallelism != 1 {
		t.Fatal("backoffLimit must be 0 and completions and parallelism 1")
	}

	if *job.Spec.ActiveDeadlineSeconds != 900 || *job.Spec.TTLSecondsAfterFinished != 600 {
		t.Fatal("deadline and TTL must come from the params")
	}

	if len(job.Spec.PodFailurePolicy.Rules) != 2 {
		t.Fatalf("pod failure policy rules = %d, want 2", len(job.Spec.PodFailurePolicy.Rules))
	}

	if spec.RestartPolicy != corev1.RestartPolicyNever || spec.ServiceAccountName != ServiceAccountName {
		t.Fatalf("restart policy %s and service account %s", spec.RestartPolicy, spec.ServiceAccountName)
	}

	if spec.AutomountServiceAccountToken == nil || *spec.AutomountServiceAccountToken {
		t.Fatal("automountServiceAccountToken must be false")
	}

	if spec.EnableServiceLinks == nil || *spec.EnableServiceLinks {
		t.Fatal("enableServiceLinks must be false")
	}

	if spec.HostNetwork || spec.HostPID || spec.HostIPC {
		t.Fatal("host namespaces must be off")
	}

	if spec.ShareProcessNamespace == nil || *spec.ShareProcessNamespace {
		t.Fatal("process namespace must not be shared")
	}

	if spec.HostUsers != nil {
		t.Fatal("hostUsers must be unset when user namespaces are disabled")
	}

	if spec.NodeSelector["kubernetes.io/os"] != testLinux || spec.NodeSelector["kubernetes.io/arch"] != testArm ||
		spec.NodeSelector["opendepot.defdev.io/pool"] != "builders" {
		t.Fatalf("node selector = %v", spec.NodeSelector)
	}
}

func TestRenderJobPodSecurityAndOrder(t *testing.T) {
	job, _ := RenderJob(goldenParams(t))
	spec := job.Spec.Template.Spec

	pod := spec.SecurityContext
	if pod == nil || !*pod.RunAsNonRoot || *pod.RunAsUser != BuildKitUID || *pod.FSGroup != BuildKitUID {
		t.Fatalf("pod security context = %+v", pod)
	}

	if pod.SeccompProfile == nil || pod.SeccompProfile.Type != corev1.SeccompProfileTypeRuntimeDefault {
		t.Fatal("pod seccomp must be RuntimeDefault")
	}

	if len(spec.Containers) != 1 || spec.Containers[0].Name != testUploadName || len(spec.InitContainers) != 2 {
		t.Fatal("expect init containers fetch-context and buildkit, then the upload main container")
	}

	if spec.InitContainers[0].Name != testFetchContainer || spec.InitContainers[1].Name != testBuildKitName {
		t.Fatalf("init container order = %s, %s", spec.InitContainers[0].Name, spec.InitContainers[1].Name)
	}
}

func TestRenderJobContainerSecurity(t *testing.T) {
	job, _ := RenderJob(goldenParams(t))

	assertRestricted(t, findContainer(t, job, testFetchContainer), HelperUID)
	assertRestricted(t, findContainer(t, job, testUploadName), HelperUID)

	buildkit := findContainer(t, job, testBuildKitName)
	sc := buildkit.SecurityContext
	if sc == nil || *sc.RunAsUser != BuildKitUID || *sc.RunAsGroup != BuildKitUID || !*sc.RunAsNonRoot {
		t.Fatalf("buildkit must run as UID and GID 1000, non-root: %+v", sc)
	}

	if !*sc.AllowPrivilegeEscalation {
		t.Error("buildkit needs allowPrivilegeEscalation: true for newuidmap and newgidmap")
	}

	if !*sc.ReadOnlyRootFilesystem {
		t.Error("buildkit must run with a read-only root filesystem")
	}

	if sc.SeccompProfile == nil || sc.SeccompProfile.Type != corev1.SeccompProfileTypeUnconfined {
		t.Error("buildkit seccomp must be Unconfined")
	}

	if sc.AppArmorProfile == nil || sc.AppArmorProfile.Type != corev1.AppArmorProfileTypeUnconfined {
		t.Error("buildkit AppArmor must be Unconfined")
	}

	if sc.ProcMount == nil || *sc.ProcMount != corev1.DefaultProcMount {
		t.Error("buildkit procMount must be Default")
	}

	if sc.Capabilities == nil || !slices.Equal(sc.Capabilities.Drop, []corev1.Capability{"ALL"}) ||
		!slices.Equal(sc.Capabilities.Add, []corev1.Capability{"SETUID", "SETGID"}) {
		t.Errorf("buildkit capabilities = %+v, want drop ALL and add SETUID, SETGID", sc.Capabilities)
	}

	if sc.Privileged != nil && *sc.Privileged {
		t.Error("buildkit must not be privileged")
	}

	if buildkit.Image != testBuildKitImage || buildkit.Command[0] != "buildctl-daemonless.sh" {
		t.Errorf("buildkit image %q command %v", buildkit.Image, buildkit.Command)
	}

	if buildkit.TerminationMessagePolicy != corev1.TerminationMessageFallbackToLogsOnError {
		t.Error("buildkit terminationMessagePolicy must be FallbackToLogsOnError")
	}
}

func TestRenderJobMountsIsolateTokens(t *testing.T) {
	job, _ := RenderJob(goldenParams(t))

	fetchMounts := mountsOf(findContainer(t, job, testFetchContainer))
	if len(fetchMounts) != 2 || fetchMounts[FetchTokenKey].MountPath != FetchTokenMountPath ||
		!fetchMounts[FetchTokenKey].ReadOnly || fetchMounts["workspace"].MountPath != WorkspaceMountPath {
		t.Fatalf("fetch-context mounts = %+v", fetchMounts)
	}

	uploadMounts := mountsOf(findContainer(t, job, testUploadName))
	if len(uploadMounts) != 2 || uploadMounts[UploadTokenKey].MountPath != UploadTokenMountPath ||
		!uploadMounts[UploadTokenKey].ReadOnly || !uploadMounts["output"].ReadOnly {
		t.Fatalf("upload mounts = %+v", uploadMounts)
	}

	buildkit := findContainer(t, job, testBuildKitName)
	wantMounts := map[string]string{
		"workspace":   WorkspaceMountPath,
		"output":      OutputMountPath,
		"buildkitd":   testBuildKitDataDir,
		"rootlesskit": "/home/user/.local/tmp",
		"run":         "/run/user/1000",
		"tmp":         "/tmp",
	}

	got := mountsOf(buildkit)
	if len(got) != len(wantMounts) {
		t.Fatalf("buildkit mounts = %+v", got)
	}

	for name, path := range wantMounts {
		if got[name].MountPath != path {
			t.Errorf("buildkit mount %s at %q, want %q", name, got[name].MountPath, path)
		}
	}

	for _, m := range buildkit.VolumeMounts {
		if m.Name == FetchTokenKey || m.Name == UploadTokenKey {
			t.Fatalf("buildkit must not mount a token, found %s", m.Name)
		}
	}

	wantEnv := map[string]string{
		"DOCKER_CONFIG":   "/tmp/docker-config",
		"BUILDKITD_FLAGS": "--oci-worker-no-process-sandbox",
	}

	for _, env := range buildkit.Env {
		if strings.Contains(env.Value, testFetchToken) || strings.Contains(env.Value, testUploadToken) {
			t.Fatalf("buildkit env leaks a token: %s", env.Name)
		}

		if wantEnv[env.Name] != env.Value {
			t.Errorf("buildkit env %s = %q", env.Name, env.Value)
		}
	}
}

func TestRenderJobVolumesProjectOneKeyEach(t *testing.T) {
	job, _ := RenderJob(goldenParams(t))

	volumes := map[string]corev1.Volume{}
	for _, v := range job.Spec.Template.Spec.Volumes {
		volumes[v.Name] = v
	}

	for _, name := range []string{FetchTokenKey, UploadTokenKey} {
		v, ok := volumes[name]
		if !ok || v.Secret == nil {
			t.Fatalf("volume %s is not a secret volume", name)
		}

		if v.Secret.SecretName != testSecretName || len(v.Secret.Items) != 1 || v.Secret.Items[0].Key != name {
			t.Fatalf("volume %s projects %+v", name, v.Secret.Items)
		}
	}

	for _, name := range []string{"workspace", "output", "buildkitd", "rootlesskit", "run", "tmp"} {
		v, ok := volumes[name]
		if !ok || v.EmptyDir == nil || v.EmptyDir.SizeLimit == nil {
			t.Fatalf("volume %s must be an emptyDir with a sizeLimit", name)
		}
	}
}

func TestRenderJobEveryContainerHasComputeResources(t *testing.T) {
	job, _ := RenderJob(goldenParams(t))

	for _, c := range allContainers(job) {
		if err := requireComputeResources(c.Resources); err != nil {
			t.Errorf("%s: %v", c.Name, err)
		}
	}
}

func TestRenderJobArgs(t *testing.T) {
	job, _ := RenderJob(goldenParams(t))
	args := findContainer(t, job, testBuildKitName).Args

	for _, want := range []string{
		"build",
		"dockerfile.v0",
		"context=/workspace/context",
		"dockerfile=/workspace/dockerfile",
		"filename=Dockerfile",
		"platform=" + testPlatformArmV7,
		"target=release",
		"build-arg:SOURCE_DATE_EPOCH=1767225600",
		"build-arg:APP_VERSION=a=b,c",
		"type=oci,dest=/output/layout,tar=false,rewrite-timestamp=true",
	} {
		if !slices.Contains(args, want) {
			t.Errorf("buildkit args missing %q: %v", want, args)
		}
	}

	upload := findContainer(t, job, testUploadName).Args
	if !slices.Contains(upload, "--build-helper=upload") ||
		!slices.Contains(upload, "--token-file="+UploadTokenMountPath+"/"+UploadTokenKey) {
		t.Fatalf("upload args = %v", upload)
	}

	fetch := findContainer(t, job, testFetchContainer).Args
	wantURL := "--build-url=" + testBrokerURL + "/internal/builds/v1/opendepot/acme-modules-1.2.3/" + testBuildID
	if !slices.Contains(fetch, wantURL) || !slices.Contains(fetch, "--context-digest=sha256:"+strings.Repeat("a", 64)) {
		t.Fatalf("fetch args = %v", fetch)
	}
}

func TestRenderJobUserNamespacesAndNativeSnapshotter(t *testing.T) {
	params := goldenParams(t)
	params.UserNamespaces = true
	params.Snapshotter = SnapshotterNative
	job, _ := RenderJob(params)

	if hu := job.Spec.Template.Spec.HostUsers; hu == nil || *hu {
		t.Fatal("hostUsers must be false when user namespaces are enabled")
	}

	env := findContainer(t, job, testBuildKitName).Env
	if env[0].Value != "--oci-worker-no-process-sandbox --oci-worker-snapshotter=native" {
		t.Fatalf("BUILDKITD_FLAGS = %q", env[0].Value)
	}
}

func TestJobNamesAreDNSLabels(t *testing.T) {
	longest := strings.Repeat("a", maxBuildIDLength)
	name := JobName(longest, mustPlatform(t, testPlatformArmV7))
	if len(name) > 63 {
		t.Fatalf("job name %q is %d characters", name, len(name))
	}

	if got := JobName("b1", mustPlatform(t, testPlatformAmd64)); got != "odb-b1-amd64" {
		t.Fatalf("JobName = %q", got)
	}

	if got := SecretName("b1"); got != "odb-b1" {
		t.Fatalf("SecretName = %q", got)
	}
}

func TestJobParamsValidate(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*JobParams)
	}{
		{name: "valid"},
		{name: "uppercase build ID", mutate: func(p *JobParams) { p.BuildID = "ABC" }},
		{name: "build ID too long", mutate: func(p *JobParams) { p.BuildID = strings.Repeat("a", maxBuildIDLength+1) }},
		{name: "missing namespace", mutate: func(p *JobParams) { p.Namespace = "" }},
		{
			name:   "non-linux platform",
			mutate: func(p *JobParams) { p.Platform = Platform{OS: "windows", Architecture: testAmd64} },
		},
		{name: "missing broker", mutate: func(p *JobParams) { p.BrokerURL = "" }},
		{name: "bad target", mutate: func(p *JobParams) { p.Target = "../x" }},
		{name: "reserved build arg", mutate: func(p *JobParams) {
			p.BuildArgs = []v1alpha1.OCIBuildArg{{Name: "BUILDKIT_INLINE_CACHE", Value: "1"}}
		}},
		{name: "source date epoch reserved", mutate: func(p *JobParams) {
			p.BuildArgs = []v1alpha1.OCIBuildArg{{Name: "SOURCE_DATE_EPOCH", Value: "1"}}
		}},
		{name: "control character in build arg", mutate: func(p *JobParams) {
			p.BuildArgs = []v1alpha1.OCIBuildArg{{Name: "A", Value: "x\ny"}}
		}},
		{name: "zero deadline", mutate: func(p *JobParams) { p.ActiveDeadlineSeconds = 0 }},
		{name: "deadline above maximum", mutate: func(p *JobParams) { p.ActiveDeadlineSeconds = MaxTimeoutSeconds + 1 }},
		{name: "same token twice", mutate: func(p *JobParams) { p.UploadToken = p.FetchToken }},
		{name: "unknown snapshotter", mutate: func(p *JobParams) { p.Snapshotter = "btrfs" }},
		{name: "missing buildkit resources", mutate: func(p *JobParams) { p.Resources = corev1.ResourceRequirements{} }},
		{name: "missing volume size limit", mutate: func(p *JobParams) { p.VolumeSizeLimit = resource.Quantity{} }},
		{name: "bad context digest", mutate: func(p *JobParams) { p.ContextDigest = "md5:abc" }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := goldenParams(t)
			if tt.mutate != nil {
				tt.mutate(&p)
			}

			err := p.Validate()
			if tt.name == "valid" {
				if err != nil {
					t.Fatalf("Validate: %v", err)
				}

				return
			}

			if err == nil {
				t.Fatal("expected a validation error")
			}
		})
	}
}
