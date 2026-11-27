package main

import (
	"flag"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/tonedefdev/opendepot/pkg/ocibuild"
)

// parseOCIBuildArgs parses args with the build flags and returns them.
func parseOCIBuildArgs(t *testing.T, args ...string) ociBuildFlags {
	t.Helper()
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	var f ociBuildFlags
	registerOCIBuildFlags(fs, &f)
	if err := fs.Parse(args); err != nil {
		t.Fatalf("parse %v: %v", args, err)
	}

	return f
}

// validOCIBuildArgs enables builds with every required setting.
func validOCIBuildArgs() []string {
	return []string{
		"--oci-builds-enabled",
		"--oci-builds-namespace=opendepot-builds",
		"--oci-builds-helper-image=ghcr.io/tonedefdev/version:0.13.0",
		"--oci-build-broker-url=http://version-controller-build-broker.opendepot:8090",
	}
}

func TestOCIBuildsDisabledNeedsNoOtherSettings(t *testing.T) {
	opts, err := parseOCIBuildArgs(t).options()
	if err != nil {
		t.Fatalf("disabled builds rejected: %v", err)
	}

	if opts.Enabled {
		t.Fatal("builds are enabled by default")
	}
}

func TestOCIBuildsEnabledFailsFastOnMissingRequiredSettings(t *testing.T) {
	cases := map[string]string{
		"buildkit image":   "--oci-builds-buildkit-image=",
		"builds namespace": "--oci-builds-namespace=",
		"helper image":     "--oci-builds-helper-image=",
		"broker URL":       "--oci-build-broker-url=",
	}

	for name, override := range cases {
		t.Run(name, func(t *testing.T) {
			args := append(validOCIBuildArgs(), override)
			if _, err := parseOCIBuildArgs(t, args...).options(); err == nil {
				t.Fatalf("builds were enabled without a %s", name)
			}
		})
	}
}

func TestOCIBuildsOptionsFromFlags(t *testing.T) {
	args := append(validOCIBuildArgs(),
		"--oci-builds-allowed-repo-owners=acme, other",
		"--oci-builds-default-platforms=linux/amd64,linux/arm64/v8",
		"--oci-builds-default-timeout=30m",
		"--oci-builds-max-timeout=2h",
		"--oci-builds-max-concurrent=3",
		"--oci-builds-max-context-bytes=512Mi",
		"--oci-builds-max-image-bytes=16Gi",
		"--oci-builds-resources-default=cpu=500m,memory=1Gi,ephemeral-storage=5Gi",
		"--oci-builds-resources-max=cpu=2,memory=4Gi,ephemeral-storage=10Gi",
		"--oci-builds-runtime-class=kata",
		"--oci-builds-snapshotter=native",
	)

	opts, err := parseOCIBuildArgs(t, args...).options()
	if err != nil {
		t.Fatalf("valid settings rejected: %v", err)
	}

	if !opts.Enabled || opts.Namespace != "opendepot-builds" || opts.MaxConcurrentJobs != 3 {
		t.Fatalf("options = %+v", opts)
	}

	if opts.BrokerURL != "http://version-controller-build-broker.opendepot:8090" {
		t.Fatalf("BrokerURL = %q", opts.BrokerURL)
	}

	if strings.Join(opts.AllowedRepoOwners, ",") != "acme,other" {
		t.Fatalf("AllowedRepoOwners = %v", opts.AllowedRepoOwners)
	}

	if strings.Join(opts.DefaultPlatforms, ",") != "linux/amd64,linux/arm64/v8" {
		t.Fatalf("DefaultPlatforms = %v", opts.DefaultPlatforms)
	}

	if opts.DefaultTimeoutSeconds != 1800 || opts.MaxTimeoutSeconds != 7200 {
		t.Fatalf("timeouts = %d / %d", opts.DefaultTimeoutSeconds, opts.MaxTimeoutSeconds)
	}

	if got := opts.DefaultResources[corev1.ResourceMemory]; got.Cmp(resource.MustParse("1Gi")) != 0 {
		t.Fatalf("default memory = %s", got.String())
	}

	if got := opts.MaxResources[corev1.ResourceCPU]; got.Cmp(resource.MustParse("2")) != 0 {
		t.Fatalf("max cpu = %s", got.String())
	}

	if opts.MaxContextBytes != 512<<20 || opts.MaxImageBytes != 16<<30 {
		t.Fatalf("size limits = %d / %d", opts.MaxContextBytes, opts.MaxImageBytes)
	}

	if opts.RuntimeClassName == nil || *opts.RuntimeClassName != "kata" {
		t.Fatalf("RuntimeClassName = %v", opts.RuntimeClassName)
	}

	if opts.Snapshotter != ocibuild.SnapshotterNative {
		t.Fatalf("Snapshotter = %q", opts.Snapshotter)
	}

	if !opts.RequireScanning {
		t.Fatal("requireScanning should default to true")
	}
}

func TestOCIBuildsRejectInvalidSettings(t *testing.T) {
	cases := map[string]string{
		"zero concurrency":           "--oci-builds-max-concurrent=0",
		"default timeout over max":   "--oci-builds-default-timeout=3h",
		"max timeout over 2 hours":   "--oci-builds-max-timeout=3h",
		"unknown snapshotter":        "--oci-builds-snapshotter=overlay",
		"non-linux platform":         "--oci-builds-default-platforms=windows/amd64",
		"context size is zero":       "--oci-builds-max-context-bytes=0",
		"resource name is unknown":   "--oci-builds-resources-default=cpu=1,memory=2Gi,gpu=1",
		"resource is missing":        "--oci-builds-resources-max=cpu=4,memory=8Gi",
		"resource is listed twice":   "--oci-builds-resources-default=cpu=1,cpu=2,memory=2Gi,ephemeral-storage=10Gi",
		"resource quantity is bad":   "--oci-builds-resources-default=cpu=lots,memory=2Gi,ephemeral-storage=10Gi",
		"resource has no quantity":   "--oci-builds-resources-max=cpu,memory=8Gi,ephemeral-storage=20Gi",
		"image size is not a number": "--oci-builds-max-image-bytes=big",
		"ttl is negative":            "--oci-builds-ttl-seconds-after-finished=-1",
		"ttl overflows int32":        "--oci-builds-ttl-seconds-after-finished=2147483648",
		"node selector is not JSON":  "--oci-builds-node-selector={pool:builds",
		"node selector is an array":  "--oci-builds-node-selector=[\"builds\"]",
		"tolerations are not JSON":   "--oci-builds-tolerations=[{key:builds",
		"tolerations are an object":  "--oci-builds-tolerations={\"key\":\"builds\"}",
	}

	for name, arg := range cases {
		t.Run(name, func(t *testing.T) {
			args := append(validOCIBuildArgs(), arg)
			if _, err := parseOCIBuildArgs(t, args...).options(); err == nil {
				t.Fatalf("%s was accepted", arg)
			}
		})
	}
}

func TestOCIBuildsDefaults(t *testing.T) {
	opts, err := parseOCIBuildArgs(t, validOCIBuildArgs()...).options()
	if err != nil {
		t.Fatalf("defaults rejected: %v", err)
	}

	if opts.RuntimeClassName != nil {
		t.Fatalf("RuntimeClassName = %q, want unset", *opts.RuntimeClassName)
	}

	if opts.Snapshotter != ocibuild.SnapshotterAuto {
		t.Fatalf("Snapshotter = %q, want %s", opts.Snapshotter, ocibuild.SnapshotterAuto)
	}

	if opts.DefaultTimeoutSeconds != int64((30 * time.Minute).Seconds()) {
		t.Fatalf("DefaultTimeoutSeconds = %d", opts.DefaultTimeoutSeconds)
	}

	if opts.TTLSecondsAfterFinished != 600 {
		t.Fatalf("TTLSecondsAfterFinished = %d, want 600", opts.TTLSecondsAfterFinished)
	}

	if opts.NodeSelector != nil || opts.Tolerations != nil || opts.PriorityClassName != "" {
		t.Fatalf("scheduling = %v / %v / %q, want unset", opts.NodeSelector, opts.Tolerations, opts.PriorityClassName)
	}
}

func TestOCIBuildsSchedulingFromFlags(t *testing.T) {
	args := append(validOCIBuildArgs(),
		"--oci-builds-ttl-seconds-after-finished=900",
		`--oci-builds-node-selector={"pool":"builds","zone":"a"}`,
		`--oci-builds-tolerations=[{"key":"builds","operator":"Exists","effect":"NoSchedule"}]`,
		"--oci-builds-priority-class-name= opendepot-builds ",
	)

	opts, err := parseOCIBuildArgs(t, args...).options()
	if err != nil {
		t.Fatalf("valid scheduling settings rejected: %v", err)
	}

	if opts.TTLSecondsAfterFinished != 900 {
		t.Fatalf("TTLSecondsAfterFinished = %d, want 900", opts.TTLSecondsAfterFinished)
	}

	if len(opts.NodeSelector) != 2 || opts.NodeSelector["pool"] != "builds" || opts.NodeSelector["zone"] != "a" {
		t.Fatalf("NodeSelector = %v", opts.NodeSelector)
	}

	if len(opts.Tolerations) != 1 || opts.Tolerations[0].Key != "builds" ||
		opts.Tolerations[0].Operator != corev1.TolerationOpExists || opts.Tolerations[0].Effect != corev1.TaintEffectNoSchedule {
		t.Fatalf("Tolerations = %+v", opts.Tolerations)
	}

	if opts.PriorityClassName != "opendepot-builds" {
		t.Fatalf("PriorityClassName = %q", opts.PriorityClassName)
	}
}

func TestOCIBuildsTTLZeroIsAccepted(t *testing.T) {
	opts, err := parseOCIBuildArgs(t, append(validOCIBuildArgs(), "--oci-builds-ttl-seconds-after-finished=0")...).options()
	if err != nil {
		t.Fatalf("zero TTL rejected: %v", err)
	}

	if opts.TTLSecondsAfterFinished != 0 {
		t.Fatalf("TTLSecondsAfterFinished = %d, want 0 so the controller applies its default", opts.TTLSecondsAfterFinished)
	}
}

func TestOCIBuildCacheOptionsScopeJobsAndPodsToTheBuildsNamespace(t *testing.T) {
	options := ociBuildCacheOptions("opendepot-builds")
	if len(options) != 2 {
		t.Fatalf("cache options cover %d types, want Job and Pod", len(options))
	}

	for object, scope := range options {
		if len(scope.Namespaces) != 1 {
			t.Fatalf("%T is cached in %d namespaces, want 1", object, len(scope.Namespaces))
		}

		if _, ok := scope.Namespaces["opendepot-builds"]; !ok {
			t.Fatalf("%T is not cached in the builds namespace", object)
		}
	}
}

func TestBuildHelperFlagsDefaultToTheImageLayout(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	var f buildHelperFlags
	registerBuildHelperFlags(fs, &f)
	args := []string{
		"--build-helper=upload", "--build-url=http://broker:8090/x", "--token-file=/t", "--platform=linux/amd64",
	}
	if err := fs.Parse(args); err != nil {
		t.Fatal(err)
	}

	if f.mode != "upload" || f.buildURL != "http://broker:8090/x" || f.platform != "linux/amd64" {
		t.Fatalf("helper flags = %+v", f)
	}

	if f.layout != ocibuild.LayoutPath || f.workspace != ocibuild.WorkspaceMountPath {
		t.Fatalf("layout = %q, workspace = %q", f.layout, f.workspace)
	}

	if f.terminationLog != "/dev/termination-log" {
		t.Fatalf("termination log = %q", f.terminationLog)
	}
}
