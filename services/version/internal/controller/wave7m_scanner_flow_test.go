package controller

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	"github.com/tonedefdev/opendepot/services/version/internal/policy"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const wave7mGoModBody = "module github.com/acme/terraform-provider-aws\n\ngo 1.22\n"

const wave7mSourceReport = `{"SchemaVersion":2,"Results":[
	{"Target":"go.mod","Class":"lang-pkgs","Type":"gomod","Vulnerabilities":[
		{"VulnerabilityID":"CVE-2024-7001","PkgName":"golang.org/x/net","InstalledVersion":"v0.1.0","Severity":"CRITICAL"}]}
]}`

func wave7mGoModTransport(t *testing.T) {
	t.Helper()

	original := http.DefaultTransport
	http.DefaultTransport = jevRoundTripper(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host == "raw.githubusercontent.com" && strings.HasSuffix(req.URL.Path, "/go.mod") {
			return jevHTTPResponse(req, http.StatusOK, nil, []byte(wave7mGoModBody)), nil
		}

		return jevHTTPResponse(req, http.StatusNotFound, nil, nil), nil
	})
	t.Cleanup(func() { http.DefaultTransport = original })
}

func wave7mTrivyByCommand(t *testing.T, handler func(args []string) ([]byte, error)) {
	t.Helper()

	original := runTrivy
	runTrivy = func(_ context.Context, args ...string) ([]byte, error) {
		return handler(args)
	}
	t.Cleanup(func() { runTrivy = original })
}

func wave7mSourceScanVersion(name string, cfg *opendepotv1alpha1.ProviderConfig) *opendepotv1alpha1.Version {
	return &opendepotv1alpha1.Version{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			Labels:    map[string]string{"opendepot.defdev.io/provider": "aws"},
		},
		Spec: opendepotv1alpha1.VersionSpec{
			Type:              opendepotv1alpha1.OpenDepotProvider,
			Version:           "1.0.0",
			ProviderConfigRef: cfg,
		},
	}
}

func TestWave7MScanProviderSourceReturnsGoModFindings(t *testing.T) {
	wave7mGoModTransport(t)
	calls := wave6KMockTrivy(t, []byte(wave7mSourceReport), nil)
	r := wave6KScanner()

	findings, err := r.scanProviderSource(context.Background(), "https://github.com/acme/terraform-provider-aws", "1.0.0", t.TempDir(), true, nil)
	if err != nil {
		t.Fatalf("scanProviderSource: %v", err)
	}

	if len(findings) != 1 || findings[0].VulnerabilityID != "CVE-2024-7001" {
		t.Fatalf("findings = %+v, want the go.mod vulnerability", findings)
	}

	args := strings.Join((*calls)[0], " ")
	if !strings.Contains(args, "--offline-scan") || !strings.Contains(args, "--skip-db-update") {
		t.Fatalf("offline source scan args = %q, want offline flags", args)
	}
}

func TestWave7MScanProviderSourceCleanOutputIsEmptySlice(t *testing.T) {
	wave7mGoModTransport(t)
	wave6KMockTrivy(t, nil, nil)
	r := wave6KScanner()

	findings, err := r.scanProviderSource(context.Background(), "https://github.com/acme/terraform-provider-aws", "1.0.0", t.TempDir(), false, nil)
	if err != nil || findings == nil || len(findings) != 0 {
		t.Fatalf("clean scan = %v, %v; want an empty non-nil slice", findings, err)
	}
}

func TestWave7MScanProviderSourceTrivyFailureIsReported(t *testing.T) {
	wave7mGoModTransport(t)
	wave6KMockTrivy(t, nil, errors.New("exit status 2"))
	r := wave6KScanner()

	_, err := r.scanProviderSource(context.Background(), "https://github.com/acme/terraform-provider-aws", "1.0.0", t.TempDir(), false, nil)
	if err == nil || !strings.Contains(err.Error(), "source scan failed") {
		t.Fatalf("err = %v, want a source scan failure", err)
	}
}

func TestWave7MScanProviderSourceHonoursCancelledWaitForSemaphore(t *testing.T) {
	wave7mGoModTransport(t)
	calls := wave6KMockTrivy(t, []byte(wave7mSourceReport), nil)
	r := wave6KScanner()
	r.scanSem <- struct{}{}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := r.scanProviderSource(ctx, "https://github.com/acme/terraform-provider-aws", "1.0.0", t.TempDir(), false, nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want the context deadline while waiting for the scan slot", err)
	}

	if len(*calls) != 0 {
		t.Fatalf("trivy ran %d times while the scan slot was held", len(*calls))
	}
}

func TestWave7MRunProviderScanSourceFindingsBlockAtThreshold(t *testing.T) {
	wave7mGoModTransport(t)
	wave7mTrivyByCommand(t, func(args []string) ([]byte, error) {
		if args[0] == "rootfs" {
			return nil, nil
		}

		return []byte(wave7mSourceReport), nil
	})
	archive := wave6KWriteZip(t, "terraform-provider-aws_v1.0.0", 0o755, []byte("ELF"))
	repo := "https://github.com/acme/terraform-provider-aws"
	version := wave7mSourceScanVersion("aws-scan-1-0-0", &opendepotv1alpha1.ProviderConfig{SourceRepository: &repo})
	r := wave6KScanner()

	gotRepo, binaryScan, sourceScan, err := r.runProviderScan(context.Background(), version, archive, t.TempDir(), false, policy.Resolved{Threshold: 5})
	if gotRepo != repo || binaryScan == nil {
		t.Fatalf("repo=%q binary=%v, want the repo and a clean binary scan", gotRepo, binaryScan)
	}

	if err == nil || !strings.Contains(err.Error(), "blocking: CRITICAL vulnerability CVE-2024-7001 in source") {
		t.Fatalf("err = %v, want the critical source finding to block", err)
	}

	if sourceScan == nil || len(sourceScan.Findings) != 1 {
		t.Fatalf("sourceScan = %+v, want the fresh findings", sourceScan)
	}
}

func TestWave7MRunProviderScanSourceFindingsRecordedWhenNotBlocking(t *testing.T) {
	wave7mGoModTransport(t)
	wave7mTrivyByCommand(t, func(args []string) ([]byte, error) {
		if args[0] == "rootfs" {
			return nil, nil
		}

		return []byte(wave7mSourceReport), nil
	})
	archive := wave6KWriteZip(t, "terraform-provider-aws_v1.0.0", 0o755, []byte("ELF"))
	repo := "https://github.com/acme/terraform-provider-aws"
	version := wave7mSourceScanVersion("aws-scan-2-0-0", &opendepotv1alpha1.ProviderConfig{SourceRepository: &repo})
	r := wave6KScanner()

	_, _, sourceScan, err := r.runProviderScan(context.Background(), version, archive, t.TempDir(), false, policy.Resolved{Threshold: 0})
	if err != nil {
		t.Fatalf("a disabled threshold must not block: %v", err)
	}

	if sourceScan == nil || len(sourceScan.Findings) != 1 || sourceScan.ScannedAt == "" {
		t.Fatalf("sourceScan = %+v, want a timestamped fresh scan", sourceScan)
	}
}

func TestWave7MRunProviderScanSourceFailureKeepsBinaryResult(t *testing.T) {
	wave7mGoModTransport(t)
	wave7mTrivyByCommand(t, func(args []string) ([]byte, error) {
		if args[0] == "rootfs" {
			return nil, nil
		}

		return nil, errors.New("exit status 2")
	})
	archive := wave6KWriteZip(t, "terraform-provider-aws_v1.0.0", 0o755, []byte("ELF"))
	repo := "https://github.com/acme/terraform-provider-aws"
	version := wave7mSourceScanVersion("aws-scan-3-0-0", &opendepotv1alpha1.ProviderConfig{SourceRepository: &repo})
	r := wave6KScanner()

	_, binaryScan, sourceScan, err := r.runProviderScan(context.Background(), version, archive, t.TempDir(), false, policy.Resolved{Threshold: 5})
	if err != nil || binaryScan == nil || sourceScan != nil {
		t.Fatalf("binary=%v source=%v err=%v, want a binary result and no source result", binaryScan, sourceScan, err)
	}
}

func TestWave7MRunProviderScanAuthenticatedWithoutSecretFallsBack(t *testing.T) {
	wave7mGoModTransport(t)
	wave6KMockTrivy(t, nil, nil)
	archive := wave6KWriteZip(t, "terraform-provider-aws_v1.0.0", 0o755, []byte("ELF"))
	repo := "https://github.com/acme/terraform-provider-aws"
	version := wave7mSourceScanVersion("aws-scan-4-0-0", &opendepotv1alpha1.ProviderConfig{
		SourceRepository: &repo,
		GithubClientConfig: &opendepotv1alpha1.GithubClientConfig{
			UseAuthenticatedClient: true,
		},
	})
	r := wave7mProviderReconciler(t, version)
	r.APIReader = r.Client
	r.scanSem = make(chan struct{}, 1)

	gotRepo, binaryScan, _, err := r.runProviderScan(context.Background(), version, archive, t.TempDir(), false, policy.Resolved{Threshold: 5})
	if err != nil || gotRepo != repo || binaryScan == nil {
		t.Fatalf("repo=%q binary=%v err=%v, want an unauthenticated fallback", gotRepo, binaryScan, err)
	}

}
