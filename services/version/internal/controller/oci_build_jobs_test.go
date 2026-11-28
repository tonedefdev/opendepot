package controller

import (
	"context"
	"errors"
	"strings"
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	"github.com/tonedefdev/opendepot/pkg/ocibuild"
)

func TestSanitizeOCIMessageRemovesControlCharacters(t *testing.T) {
	got := sanitizeOCIMessage("line one\x1b[31m\nline two\x00\ttab")
	if strings.ContainsAny(got, "\x1b\n\x00\t") {
		t.Fatalf("control characters survived: %q", got)
	}

	if got != "line one[31mline twotab" {
		t.Fatalf("sanitized = %q", got)
	}
}

func TestTruncateOCIMessageKeepsTheTailOnARuneBoundary(t *testing.T) {
	short := "short message"
	if got := truncateOCIMessage(short); got != short {
		t.Fatalf("short message changed: %q", got)
	}

	// Every character is three bytes, so the cut must not start inside a rune.
	long := strings.Repeat("界", 1000) + "END"
	got := truncateOCIMessage(long)
	if len(got) > ociBuildMaxMessageBytes {
		t.Fatalf("truncated length = %d, want at most %d", len(got), ociBuildMaxMessageBytes)
	}

	if !strings.HasSuffix(got, "END") {
		t.Fatalf("tail was not kept: %q", got[len(got)-10:])
	}

	for _, r := range got {
		if r == '\uFFFD' {
			t.Fatal("truncation split a rune")
		}
	}
}

func TestNewOCIBuildIDIsTwentyLowercaseAlphanumerics(t *testing.T) {
	seen := map[string]bool{}
	for range 50 {
		id, err := newOCIBuildID()
		if err != nil {
			t.Fatal(err)
		}

		if len(id) != ociBuildIDLength {
			t.Fatalf("build ID %q has length %d", id, len(id))
		}

		for _, c := range id {
			if !strings.ContainsRune(ociBuildIDAlphabet, c) {
				t.Fatalf("build ID %q has character %q outside the alphabet", id, c)
			}
		}

		seen[id] = true
	}

	if len(seen) < 50 {
		t.Fatalf("expected 50 distinct build IDs, got %d", len(seen))
	}
}

func TestOCIBuildTimeoutIsClampedToTheMaximum(t *testing.T) {
	r := &VersionReconciler{OCIBuilds: OCIBuildOptions{DefaultTimeoutSeconds: 900, MaxTimeoutSeconds: 3600}}
	if got := r.ociBuildTimeout(nil); got != 900 {
		t.Fatalf("default timeout = %d, want 900", got)
	}

	long := int32(7200)
	if got := r.ociBuildTimeout(&opendepotv1alpha1.OCIBuildSource{TimeoutSeconds: &long}); got != 3600 {
		t.Fatalf("clamped timeout = %d, want 3600", got)
	}

	short := int32(120)
	if got := r.ociBuildTimeout(&opendepotv1alpha1.OCIBuildSource{TimeoutSeconds: &short}); got != 120 {
		t.Fatalf("timeout = %d, want 120", got)
	}

	unset := &VersionReconciler{}
	if got := unset.ociBuildTimeout(nil); got != ociBuildDefaultTimeoutSeconds {
		t.Fatalf("unset timeout = %d, want %d", got, ociBuildDefaultTimeoutSeconds)
	}
}

func TestOCIBuildResourcesAreClampedAndRequestsEqualLimits(t *testing.T) {
	r := &VersionReconciler{OCIBuilds: OCIBuildOptions{
		DefaultResources: corev1.ResourceList{
			corev1.ResourceCPU:              resource.MustParse("1"),
			corev1.ResourceMemory:           resource.MustParse("2Gi"),
			corev1.ResourceEphemeralStorage: resource.MustParse("10Gi"),
		},
		MaxResources: corev1.ResourceList{
			corev1.ResourceCPU:              resource.MustParse("4"),
			corev1.ResourceMemory:           resource.MustParse("8Gi"),
			corev1.ResourceEphemeralStorage: resource.MustParse("20Gi"),
		},
	}}

	cpu := resource.MustParse("16")
	memory := resource.MustParse("1Gi")
	src := &opendepotv1alpha1.OCIBuildSource{Resources: &opendepotv1alpha1.OCIBuildResources{CPU: &cpu, Memory: &memory}}
	rr := r.ociBuildResources(src)

	if got := rr.Limits[corev1.ResourceCPU]; got.Cmp(resource.MustParse("4")) != 0 {
		t.Fatalf("cpu limit = %s, want the maximum 4", got.String())
	}

	if got := rr.Limits[corev1.ResourceMemory]; got.Cmp(resource.MustParse("1Gi")) != 0 {
		t.Fatalf("memory limit = %s, want the requested 1Gi", got.String())
	}

	if got := rr.Limits[corev1.ResourceEphemeralStorage]; got.Cmp(resource.MustParse("10Gi")) != 0 {
		t.Fatalf("ephemeral storage = %s, want the default 10Gi", got.String())
	}

	for _, name := range []corev1.ResourceName{corev1.ResourceCPU, corev1.ResourceMemory, corev1.ResourceEphemeralStorage} {
		request, limit := rr.Requests[name], rr.Limits[name]
		if !request.Equal(limit) {
			t.Fatalf("%s request %s differs from its limit %s", name, request.String(), limit.String())
		}
	}
}

func TestOCIBuildRepoRequiresAGitHubURLForTheOwner(t *testing.T) {
	url := "https://github.com/acme/app.git"
	name, repo, err := ociBuildRepo(&opendepotv1alpha1.OCIBuildSource{RepoOwner: "acme", RepoUrl: &url})
	if err != nil {
		t.Fatalf("valid repository rejected: %v", err)
	}

	if name != "https://github.com/acme/app" || repo != "app" {
		t.Fatalf("ociBuildRepo = %q, %q", name, repo)
	}

	if _, _, err := ociBuildRepo(&opendepotv1alpha1.OCIBuildSource{RepoOwner: "acme"}); err == nil {
		t.Fatal("a build source without repoUrl was accepted")
	}

	other := "https://github.com/someone-else/app"
	if _, _, err := ociBuildRepo(&opendepotv1alpha1.OCIBuildSource{RepoOwner: "acme", RepoUrl: &other}); err == nil {
		t.Fatal("a repository of another owner was accepted")
	}
}

func TestOCIBuildPlatformsRejectDuplicatesAndTooManyPlatforms(t *testing.T) {
	r := &VersionReconciler{OCIBuilds: OCIBuildOptions{DefaultPlatforms: []string{"linux/arm64"}}}

	platforms, err := r.ociBuildPlatforms(&opendepotv1alpha1.OCIArtifactConfig{})
	if err != nil || len(platforms) != 1 || platforms[0].String() != "linux/arm64" {
		t.Fatalf("default platforms = %v, %v", platforms, err)
	}

	if _, err := r.ociBuildPlatforms(&opendepotv1alpha1.OCIArtifactConfig{Platforms: []string{"linux/amd64", "linux/amd64"}}); err == nil {
		t.Fatal("duplicate platforms were accepted")
	}

	if _, err := r.ociBuildPlatforms(&opendepotv1alpha1.OCIArtifactConfig{Platforms: []string{"windows/amd64"}}); err == nil {
		t.Fatal("a non-linux platform was accepted")
	}

	many := make([]string, 0, maxOCIPlatforms+1)
	for range maxOCIPlatforms + 1 {
		many = append(many, "linux/amd64")
	}

	if _, err := r.ociBuildPlatforms(&opendepotv1alpha1.OCIArtifactConfig{Platforms: many}); err == nil {
		t.Fatal("more platforms than the limit were accepted")
	}

	empty := &VersionReconciler{}
	platforms, err = empty.ociBuildPlatforms(&opendepotv1alpha1.OCIArtifactConfig{})
	if err != nil || len(platforms) != 1 || platforms[0].String() != ociBuildDefaultPlatform {
		t.Fatalf("built-in default platforms = %v, %v", platforms, err)
	}
}

func TestReadOCIBuildTokensRequiresTwoDistinctTokens(t *testing.T) {
	secret := &corev1.Secret{Data: map[string][]byte{
		ocibuild.FetchTokenKey:  []byte("fetch"),
		ocibuild.UploadTokenKey: []byte("upload"),
	}}

	tokens, err := readOCIBuildTokens(secret)
	if err != nil || tokens.fetch != "fetch" || tokens.upload != "upload" {
		t.Fatalf("tokens = %+v, %v", tokens, err)
	}

	secret.Data[ocibuild.UploadTokenKey] = []byte("fetch")
	if _, err := readOCIBuildTokens(secret); err == nil {
		t.Fatal("identical tokens were accepted")
	}

	if _, err := readOCIBuildTokens(&corev1.Secret{}); err == nil {
		t.Fatal("a Secret with no tokens was accepted")
	}
}

func TestSameOwnerUIDsComparesSets(t *testing.T) {
	a := metav1.OwnerReference{UID: "a"}
	b := metav1.OwnerReference{UID: "b"}

	if !sameOwnerUIDs([]metav1.OwnerReference{a, b}, []metav1.OwnerReference{b, a}) {
		t.Fatal("the same set in another order did not match")
	}

	if sameOwnerUIDs([]metav1.OwnerReference{a}, []metav1.OwnerReference{a, b}) {
		t.Fatal("a subset matched")
	}

	if !sameOwnerUIDs(nil, nil) {
		t.Fatal("two empty sets did not match")
	}
}

func TestVersionForOCIBuildObjectReadsTheAnnotations(t *testing.T) {
	job := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{
		Annotations: map[string]string{
			ocibuild.AnnotationVersionNamespace: "opendepot",
			ocibuild.AnnotationVersionName:      "app-1.0.0",
		},
	}}

	requests := versionForOCIBuildObject(context.TODO(), job)
	if len(requests) != 1 || requests[0].NamespacedName != (types.NamespacedName{Namespace: "opendepot", Name: "app-1.0.0"}) {
		t.Fatalf("requests = %v", requests)
	}

	if got := versionForOCIBuildObject(context.TODO(), &batchv1.Job{}); got != nil {
		t.Fatalf("an unannotated Job mapped to %v", got)
	}
}

func TestIsInvalidBuildContextClassifiesSourceErrors(t *testing.T) {
	if isInvalidBuildContext(nil) {
		t.Fatal("nil error classified as invalid")
	}

	if !isInvalidBuildContext(ocibuild.ErrUnsafeEntry) {
		t.Fatal("an unsafe entry was not classified as invalid")
	}

	if isInvalidBuildContext(errors.New("connection reset")) {
		t.Fatal("a transient error was classified as invalid")
	}
}

func TestJobHasConditionMatchesTrueStatusOnly(t *testing.T) {
	job := &batchv1.Job{Status: batchv1.JobStatus{Conditions: []batchv1.JobCondition{
		{Type: batchv1.JobComplete, Status: corev1.ConditionFalse},
		{Type: batchv1.JobFailed, Status: corev1.ConditionTrue, Reason: ociBuildTimeoutReason},
	}}}

	if jobHasCondition(job, batchv1.JobComplete) {
		t.Fatal("a False condition matched")
	}

	if !jobHasCondition(job, batchv1.JobFailed) {
		t.Fatal("a True condition did not match")
	}

	if reason, _ := jobFailure(job); reason != ociBuildTimeoutReason {
		t.Fatalf("failure reason = %q", reason)
	}
}

func TestNewOCIBuildTokensAreDistinctHexTokens(t *testing.T) {
	tokens, err := newOCIBuildTokens()
	if err != nil {
		t.Fatal(err)
	}

	if tokens.fetch == tokens.upload || len(tokens.fetch) != ocibuild.TokenBytes*2 || len(tokens.upload) != ocibuild.TokenBytes*2 {
		t.Fatalf("tokens = %+v", tokens)
	}
}
