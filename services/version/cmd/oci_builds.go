package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/tonedefdev/opendepot/pkg/ocibuild"
	"github.com/tonedefdev/opendepot/services/version/internal/buildhelper"
	"github.com/tonedefdev/opendepot/services/version/internal/controller"
)

// Defaults for the --oci-builds-* and --oci-build-broker-* flags. The BuildKit image is pinned by digest.
const (
	defaultOCIBuildsNamespace = "opendepot-builds"
	defaultOCIBuildKitImage   = "moby/buildkit:v0.35.1-rootless@sha256:" +
		"7d02292556e66feea6d2d89b7ec143503b57e4f59a4efc6b7ece8547e4d3e5be"
	defaultOCIBuildsMaxConcurrent   = 2
	defaultOCIBuildsPlatforms       = "linux/amd64"
	defaultOCIBuildsTimeout         = 30 * time.Minute
	defaultOCIBuildsMaxTimeout      = 2 * time.Hour
	defaultOCIBuildsResourcesDef    = "cpu=1,memory=2Gi,ephemeral-storage=10Gi"
	defaultOCIBuildsResourcesMax    = "cpu=4,memory=8Gi,ephemeral-storage=20Gi"
	defaultOCIBuildsMaxContextBytes = "256Mi"
	defaultOCIBuildsMaxImageBytes   = "8Gi"
	defaultOCIBuildsSnapshotter     = ocibuild.SnapshotterAuto
	defaultOCIBuildBrokerAddr       = ":8090"
)

// ociBuildFlags holds the values of the --oci-builds-* and --oci-build-broker-* flags.
type ociBuildFlags struct {
	enabled           bool
	namespace         string
	buildKitImage     string
	helperImage       string
	maxConcurrent     int
	allowedRepoOwners string
	defaultPlatforms  string
	defaultTimeout    time.Duration
	maxTimeout        time.Duration
	resourcesDefault  string
	resourcesMax      string
	maxContextBytes   string
	maxImageBytes     string
	requireScanning   bool
	userNamespaces    bool
	runtimeClass      string
	snapshotter       string
	brokerAddr        string
	brokerURL         string
}

// registerOCIBuildFlags binds the build flags to f on fs.
func registerOCIBuildFlags(fs *flag.FlagSet, f *ociBuildFlags) {
	fs.BoolVar(&f.enabled, "oci-builds-enabled", false,
		"Allow OCIArtifacts that set build to be built in the cluster with rootless BuildKit.")
	fs.StringVar(&f.namespace, "oci-builds-namespace", defaultOCIBuildsNamespace,
		"The namespace where build Jobs, Pods, and Secrets are created.")
	fs.StringVar(&f.buildKitImage, "oci-builds-buildkit-image", defaultOCIBuildKitImage,
		"The pinned rootless BuildKit image that runs the builds.")
	fs.StringVar(&f.helperImage, "oci-builds-helper-image", "",
		"The version-controller image that runs the fetch and upload helpers of a build.")
	fs.IntVar(&f.maxConcurrent, "oci-builds-max-concurrent", defaultOCIBuildsMaxConcurrent,
		"The number of build Jobs that may run at once.")
	fs.StringVar(&f.allowedRepoOwners, "oci-builds-allowed-repo-owners", "",
		"Comma-separated GitHub owners whose repositories may be built. Empty denies every build.")
	fs.StringVar(&f.defaultPlatforms, "oci-builds-default-platforms", defaultOCIBuildsPlatforms,
		"Comma-separated platforms that a build uses when its OCIArtifact does not set platforms.")
	fs.DurationVar(&f.defaultTimeout, "oci-builds-default-timeout", defaultOCIBuildsTimeout,
		"The build timeout when a source does not set one.")
	fs.DurationVar(&f.maxTimeout, "oci-builds-max-timeout", defaultOCIBuildsMaxTimeout,
		"The longest build timeout that a source may request.")
	fs.StringVar(&f.resourcesDefault, "oci-builds-resources-default", defaultOCIBuildsResourcesDef,
		"Comma-separated cpu, memory, and ephemeral-storage of a build that does not set them.")
	fs.StringVar(&f.resourcesMax, "oci-builds-resources-max", defaultOCIBuildsResourcesMax,
		"Comma-separated cpu, memory, and ephemeral-storage that clamp each build.")
	fs.StringVar(&f.maxContextBytes, "oci-builds-max-context-bytes", defaultOCIBuildsMaxContextBytes,
		"The largest compressed build context, such as 256Mi.")
	fs.StringVar(&f.maxImageBytes, "oci-builds-max-image-bytes", defaultOCIBuildsMaxImageBytes,
		"The largest image that a build may assemble, such as 8Gi.")
	fs.BoolVar(&f.requireScanning, "oci-builds-require-scanning", true,
		"Refuse a build unless scanning is enabled.")
	fs.BoolVar(&f.userNamespaces, "oci-builds-user-namespaces", false,
		"Set hostUsers: false on build Pods. The cluster must support user namespaces.")
	fs.StringVar(&f.runtimeClass, "oci-builds-runtime-class", "",
		"The runtime class of build Pods, such as a sandboxed runtime. Empty uses the default runtime.")
	fs.StringVar(&f.snapshotter, "oci-builds-snapshotter", defaultOCIBuildsSnapshotter,
		"The BuildKit snapshotter: auto or native.")
	fs.StringVar(&f.brokerAddr, "oci-build-broker-addr", defaultOCIBuildBrokerAddr,
		"The address that the build broker listens on, which every version-controller replica serves.")
	fs.StringVar(&f.brokerURL, "oci-build-broker-url", "",
		"The base URL of the build broker that build Pods reach, "+
			"such as http://version-controller-build-broker.opendepot:8090.")
}

// options checks the flags and returns the controller options. A disabled build feature needs no other settings. An
// enabled one fails fast when a required setting is missing, so that a build never starts without it.
func (f ociBuildFlags) options() (controller.OCIBuildOptions, error) {
	if !f.enabled {
		return controller.OCIBuildOptions{}, nil
	}

	if strings.TrimSpace(f.buildKitImage) == "" {
		return controller.OCIBuildOptions{}, errors.New(
			"--oci-builds-buildkit-image is required when --oci-builds-enabled is set")
	}

	if strings.TrimSpace(f.namespace) == "" {
		return controller.OCIBuildOptions{}, errors.New("--oci-builds-namespace is required when --oci-builds-enabled is set")
	}

	if strings.TrimSpace(f.helperImage) == "" {
		return controller.OCIBuildOptions{}, errors.New(
			"--oci-builds-helper-image is required when --oci-builds-enabled is set")
	}

	if strings.TrimSpace(f.brokerURL) == "" {
		return controller.OCIBuildOptions{}, errors.New("--oci-build-broker-url is required when --oci-builds-enabled is set")
	}

	if f.maxConcurrent < 1 {
		return controller.OCIBuildOptions{}, errors.New("--oci-builds-max-concurrent must be at least 1")
	}

	if f.defaultTimeout <= 0 || f.maxTimeout <= 0 || f.defaultTimeout > f.maxTimeout {
		return controller.OCIBuildOptions{}, errors.New(
			"--oci-builds-default-timeout must be positive and at most --oci-builds-max-timeout")
	}

	if f.maxTimeout > time.Duration(ocibuild.MaxTimeoutSeconds)*time.Second {
		return controller.OCIBuildOptions{}, fmt.Errorf("--oci-builds-max-timeout must be at most %s",
			time.Duration(ocibuild.MaxTimeoutSeconds)*time.Second)
	}

	switch f.snapshotter {
	case ocibuild.SnapshotterAuto, ocibuild.SnapshotterNative:
	default:
		return controller.OCIBuildOptions{}, fmt.Errorf("--oci-builds-snapshotter must be %s or %s",
			ocibuild.SnapshotterAuto, ocibuild.SnapshotterNative)
	}

	defaults, err := parseOCIBuildResources("--oci-builds-resources-default", f.resourcesDefault)
	if err != nil {
		return controller.OCIBuildOptions{}, err
	}

	maxima, err := parseOCIBuildResources("--oci-builds-resources-max", f.resourcesMax)
	if err != nil {
		return controller.OCIBuildOptions{}, err
	}

	contextBytes, err := parseOCIBuildSize("--oci-builds-max-context-bytes", f.maxContextBytes)
	if err != nil {
		return controller.OCIBuildOptions{}, err
	}

	imageBytes, err := parseOCIBuildSize("--oci-builds-max-image-bytes", f.maxImageBytes)
	if err != nil {
		return controller.OCIBuildOptions{}, err
	}

	platforms := parseOCIAllowedRegistries(f.defaultPlatforms)
	for _, platform := range platforms {
		if _, err := ocibuild.ParsePlatform(platform); err != nil {
			return controller.OCIBuildOptions{}, fmt.Errorf("--oci-builds-default-platforms: %w", err)
		}
	}

	var runtimeClass *string
	if value := strings.TrimSpace(f.runtimeClass); value != "" {
		runtimeClass = &value
	}

	return controller.OCIBuildOptions{
		Enabled:               true,
		Namespace:             strings.TrimSpace(f.namespace),
		BrokerURL:             strings.TrimRight(strings.TrimSpace(f.brokerURL), "/"),
		BuildKitImage:         strings.TrimSpace(f.buildKitImage),
		HelperImage:           strings.TrimSpace(f.helperImage),
		MaxConcurrentJobs:     f.maxConcurrent,
		AllowedRepoOwners:     parseOCIAllowedRegistries(f.allowedRepoOwners),
		DefaultPlatforms:      platforms,
		DefaultTimeoutSeconds: int64(f.defaultTimeout / time.Second),
		MaxTimeoutSeconds:     int64(f.maxTimeout / time.Second),
		DefaultResources:      defaults,
		MaxResources:          maxima,
		MaxContextBytes:       contextBytes,
		MaxImageBytes:         imageBytes,
		RequireScanning:       f.requireScanning,
		UserNamespaces:        f.userNamespaces,
		RuntimeClassName:      runtimeClass,
		Snapshotter:           f.snapshotter,
	}, nil
}

// parseOCIBuildResources parses cpu, memory, and ephemeral-storage from a comma-separated list such as
// cpu=1,memory=2Gi,ephemeral-storage=10Gi. Each of the three names must be set once and must be positive.
func parseOCIBuildResources(flagName, value string) (corev1.ResourceList, error) {
	list := corev1.ResourceList{}
	for _, part := range parseOCIAllowedRegistries(value) {
		name, raw, found := strings.Cut(part, "=")
		if !found {
			return nil, fmt.Errorf("%s: %q must be name=quantity", flagName, part)
		}

		resourceName := corev1.ResourceName(strings.TrimSpace(name))
		switch resourceName {
		case corev1.ResourceCPU, corev1.ResourceMemory, corev1.ResourceEphemeralStorage:
		default:
			return nil, fmt.Errorf("%s: %q is not cpu, memory, or ephemeral-storage", flagName, name)
		}

		quantity, err := resource.ParseQuantity(strings.TrimSpace(raw))
		if err != nil || quantity.Sign() <= 0 {
			return nil, fmt.Errorf("%s: %q is not a positive quantity", flagName, raw)
		}

		if _, dup := list[resourceName]; dup {
			return nil, fmt.Errorf("%s: %s is set more than once", flagName, resourceName)
		}

		list[resourceName] = quantity
	}

	for _, name := range []corev1.ResourceName{
		corev1.ResourceCPU, corev1.ResourceMemory, corev1.ResourceEphemeralStorage,
	} {
		if _, ok := list[name]; !ok {
			return nil, fmt.Errorf("%s: %s is required", flagName, name)
		}
	}

	return list, nil
}

// parseOCIBuildSize parses a byte size such as 256Mi and returns the number of bytes.
func parseOCIBuildSize(flagName, value string) (int64, error) {
	quantity, err := resource.ParseQuantity(strings.TrimSpace(value))
	if err != nil || quantity.Sign() <= 0 {
		return 0, fmt.Errorf("%s: %q is not a positive size", flagName, value)
	}

	return quantity.Value(), nil
}

// ociBuildCacheOptions scopes the Job and Pod caches to the builds namespace, so that the version controller does not
// watch every Job and Pod in the cluster.
func ociBuildCacheOptions(namespace string) map[client.Object]cache.ByObject {
	scope := cache.ByObject{Namespaces: map[string]cache.Config{namespace: {}}}
	return map[client.Object]cache.ByObject{
		&batchv1.Job{}: scope,
		&corev1.Pod{}:  scope,
	}
}

// buildHelperFlags holds the values of the --build-helper flags, which run the fetch and upload helpers of a build Job.
type buildHelperFlags struct {
	mode           string
	buildURL       string
	tokenFile      string
	contextDigest  string
	workspace      string
	layout         string
	platform       string
	terminationLog string
}

// registerBuildHelperFlags binds the helper flags to f on fs.
func registerBuildHelperFlags(fs *flag.FlagSet, f *buildHelperFlags) {
	fs.StringVar(&f.mode, "build-helper", "", "Run as a build Job helper and exit: fetch or upload.")
	fs.StringVar(&f.buildURL, "build-url", "", "The build broker route for the build.")
	fs.StringVar(&f.tokenFile, "token-file", "", "The file that holds the one-time token for this helper.")
	fs.StringVar(&f.contextDigest, "context-digest", "", "The sha256 digest of the build context. "+
		"The fetch helper verifies it.")
	fs.StringVar(&f.workspace, "workspace", ocibuild.WorkspaceMountPath,
		"The directory that the fetch helper extracts the context into.")
	fs.StringVar(&f.layout, "layout", ocibuild.LayoutPath, "The OCI layout that BuildKit wrote. "+
		"The upload helper reads it.")
	fs.StringVar(&f.platform, "platform", "", "The platform of the layout, such as linux/amd64. "+
		"The upload helper reads it.")
	fs.StringVar(&f.terminationLog, "termination-log", buildhelper.DefaultTerminationLogPath,
		"The file where the upload helper writes the manifest digest.")
}

// runBuildHelper runs one helper mode and exits the process with status 1 when it fails. It runs before the manager
// starts, so a build container never reads the cluster configuration.
func runBuildHelper(f buildHelperFlags) {
	cfg := buildhelper.Config{
		Mode:               f.mode,
		BuildURL:           f.buildURL,
		TokenFile:          f.tokenFile,
		ContextDigest:      f.contextDigest,
		Workspace:          f.workspace,
		Layout:             f.layout,
		Platform:           f.platform,
		TerminationLogPath: f.terminationLog,
	}

	if err := buildhelper.Run(ctrl.SetupSignalHandler(), cfg); err != nil {
		setupLog.Error(err, "build helper failed", "mode", f.mode)
		os.Exit(1)
	}
}
