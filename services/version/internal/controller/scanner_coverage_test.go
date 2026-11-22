package controller

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	"github.com/tonedefdev/opendepot/services/version/internal/policy"
)

// wave6KScanner returns a reconciler with the scan semaphore initialised, as the controller's SetupWithManager does.
func wave6KScanner() *VersionReconciler {
	return &VersionReconciler{
		Log:     logr.Discard(),
		scanSem: make(chan struct{}, 1),
	}
}

// wave6KMockTrivy replaces runTrivy for the duration of a test and records every invocation.
func wave6KMockTrivy(t *testing.T, output []byte, err error) *[][]string {
	t.Helper()

	var calls [][]string
	original := runTrivy
	runTrivy = func(_ context.Context, args ...string) ([]byte, error) {
		calls = append(calls, append([]string(nil), args...))

		return output, err
	}
	t.Cleanup(func() {
		runTrivy = original
	})

	return &calls
}

func wave6KWriteZip(t *testing.T, name string, mode os.FileMode, content []byte) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "provider.zip")
	if err := os.WriteFile(path, helpersCovZipEntry(t, name, mode, content), 0o600); err != nil {
		t.Fatalf("write zip: %v", err)
	}

	return path
}

func TestWave6KScanProviderBinaryKeepsPackageFindingsOnly(t *testing.T) {
	report := []byte(`{"SchemaVersion":2,"Results":[
		{"Target":"provider-binary","Class":"lang-pkgs","Type":"gobinary","Vulnerabilities":[
			{"VulnerabilityID":"CVE-2024-0001","PkgName":"stdlib","InstalledVersion":"1.21.0","Severity":"HIGH"}]},
		{"Target":"provider-binary","Class":"secret","Type":"","Secrets":[{"RuleID":"aws","Severity":"CRITICAL"}]}
	]}`)
	calls := wave6KMockTrivy(t, report, nil)
	r := wave6KScanner()
	archive := wave6KWriteZip(t, "terraform-provider-null_v3.2.4", 0o755, []byte("ELF"))

	findings, err := r.scanProviderBinary(context.Background(), archive, t.TempDir(), true)
	if err != nil {
		t.Fatalf("scanProviderBinary: %v", err)
	}

	if len(findings) != 1 || findings[0].VulnerabilityID != "CVE-2024-0001" {
		t.Fatalf("expected only the package finding, got %+v", findings)
	}

	if len(*calls) != 1 {
		t.Fatalf("expected one trivy invocation, got %d", len(*calls))
	}

	args := strings.Join((*calls)[0], " ")
	if !strings.HasPrefix(args, "rootfs --format json") || !strings.Contains(args, "--offline-scan --skip-db-update") {
		t.Fatalf("unexpected trivy args: %s", args)
	}
}

func TestWave6KScanProviderBinaryFailures(t *testing.T) {
	ctx := context.Background()
	r := wave6KScanner()

	missing := filepath.Join(t.TempDir(), "missing.zip")
	if _, err := r.scanProviderBinary(ctx, missing, t.TempDir(), false); err == nil || !strings.Contains(err.Error(), "failed to extract binary") {
		t.Fatalf("expected extraction error, got %v", err)
	}

	wave6KMockTrivy(t, nil, errors.New("exit status 1"))
	archive := wave6KWriteZip(t, "terraform-provider-null_v3.2.4", 0o755, []byte("ELF"))
	if _, err := r.scanProviderBinary(ctx, archive, t.TempDir(), false); err == nil || !strings.Contains(err.Error(), "binary scan failed") {
		t.Fatalf("expected trivy failure, got %v", err)
	}

	wave6KMockTrivy(t, nil, nil)
	findings, err := r.scanProviderBinary(ctx, archive, t.TempDir(), false)
	if err != nil || findings != nil {
		t.Fatalf("empty trivy output must be a clean scan, got findings=%v err=%v", findings, err)
	}
}

func TestWave6KScanProviderSourceSkipsUnavailableGoMod(t *testing.T) {
	calls := wave6KMockTrivy(t, nil, nil)
	r := wave6KScanner()

	findings, err := r.scanProviderSource(context.Background(), "https://github.com/onlyowner", "1.0.0", t.TempDir(), false, nil)
	if err != nil {
		t.Fatalf("an unavailable go.mod must be non-fatal, got %v", err)
	}

	if findings != nil {
		t.Fatalf("expected nil findings so the caller does not persist a result, got %v", findings)
	}

	if len(*calls) != 0 {
		t.Fatalf("trivy must not run without a go.mod, ran %d times", len(*calls))
	}
}

func TestWave6KScanModuleArchiveReportHandling(t *testing.T) {
	ctx := context.Background()
	archive := helpersCovZipEntry(t, "main.tf", 0o644, []byte(`resource "x" "y" {}`))

	wave6KMockTrivy(t, nil, errors.New("exit status 1"))
	r := wave6KScanner()
	if _, err := r.scanModuleArchive(ctx, archive, t.TempDir(), false); err == nil || !strings.Contains(err.Error(), "module source scan failed") {
		t.Fatalf("expected trivy failure, got %v", err)
	}

	wave6KMockTrivy(t, []byte("fatal: not a report"), nil)
	if _, err := r.scanModuleArchive(ctx, archive, t.TempDir(), false); err == nil || !strings.Contains(err.Error(), "produced no valid report") {
		t.Fatalf("a non-JSON report must not pass as clean, got %v", err)
	}

	report := []byte(`{"SchemaVersion":2,"Results":[
		{"Target":"main.tf","Class":"config","Type":"terraform","Misconfigurations":[
			{"ID":"AVD-AWS-0001","Title":"Open bucket","Severity":"HIGH"}]},
		{"Target":"go.sum","Class":"lang-pkgs","Type":"gomod","Vulnerabilities":[
			{"VulnerabilityID":"CVE-2024-0002","PkgName":"x","Severity":"LOW"}]}
	]}`)
	calls := wave6KMockTrivy(t, report, nil)
	findings, err := r.scanModuleArchive(ctx, archive, t.TempDir(), true)
	if err != nil {
		t.Fatalf("scanModuleArchive: %v", err)
	}

	if len(findings) != 1 || findings[0].VulnerabilityID != "AVD-AWS-0001" {
		t.Fatalf("expected only the config finding, got %+v", findings)
	}

	if !strings.Contains(strings.Join((*calls)[0], " "), "--scanners misconfig") {
		t.Fatalf("module scan must use misconfig scanners, args %v", (*calls)[0])
	}
}

func TestWave6KRunModuleScanBlocksOnThreshold(t *testing.T) {
	ctx := context.Background()
	archive := helpersCovZipEntry(t, "main.tf", 0o644, []byte(`resource "x" "y" {}`))
	report := []byte(`{"SchemaVersion":2,"Results":[
		{"Target":"main.tf","Class":"config","Type":"terraform","Misconfigurations":[
			{"ID":"AVD-AWS-0009","Title":"Public ACL","Severity":"CRITICAL"}]}]}`)
	version := &opendepotv1alpha1.Version{ObjectMeta: metav1.ObjectMeta{Name: "vpc-1-0-0", Namespace: "default"}}

	wave6KMockTrivy(t, report, nil)
	r := wave6KScanner()

	scan, err := r.runModuleScan(ctx, version, archive, t.TempDir(), false, policy.Resolved{Threshold: 5})
	if err == nil || !strings.Contains(err.Error(), "blocking: CRITICAL finding AVD-AWS-0009") {
		t.Fatalf("expected a blocking error, got %v", err)
	}

	if scan == nil || len(scan.Findings) != 1 || scan.ScannedAt == "" {
		t.Fatalf("a blocked module must still return its scan result, got %+v", scan)
	}

	scan, err = r.runModuleScan(ctx, version, archive, t.TempDir(), false, policy.Resolved{Threshold: 0})
	if err != nil {
		t.Fatalf("a disabled threshold must not block: %v", err)
	}

	if scan == nil || len(scan.Findings) != 1 {
		t.Fatalf("expected the finding to be recorded without blocking, got %+v", scan)
	}
}

func TestWave6KRunProviderScanReusesStoredSourceScan(t *testing.T) {
	ctx := context.Background()
	providerName := "null"
	repo := "https://github.com/hashicorp/terraform-provider-null"
	scannedAt := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	version := &opendepotv1alpha1.Version{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "null-3-2-4",
			Namespace: "default",
			Labels:    map[string]string{"opendepot.defdev.io/provider": providerName},
		},
		Spec: opendepotv1alpha1.VersionSpec{
			Type:    opendepotv1alpha1.OpenDepotProvider,
			Version: "3.2.4",
			ProviderConfigRef: &opendepotv1alpha1.ProviderConfig{
				Name:             &providerName,
				SourceRepository: &repo,
			},
		},
		Status: opendepotv1alpha1.VersionStatus{
			SourceScan: &opendepotv1alpha1.SourceScan{
				ScannedAt: scannedAt,
				Findings: []opendepotv1alpha1.SecurityFinding{
					{VulnerabilityID: "CVE-2024-0003", PkgName: "golang.org/x/net", Severity: "CRITICAL"},
				},
			},
		},
	}
	r := wave6KScanner()
	missingArchive := filepath.Join(t.TempDir(), "missing.zip")

	gotRepo, binaryScan, sourceScan, err := r.runProviderScan(ctx, version, missingArchive, t.TempDir(), false, policy.Resolved{Threshold: 5})
	if gotRepo != repo {
		t.Fatalf("expected repo %q, got %q", repo, gotRepo)
	}

	if binaryScan != nil {
		t.Fatalf("a failed binary scan must not produce a result, got %+v", binaryScan)
	}

	if err == nil || !strings.Contains(err.Error(), "blocking: CRITICAL vulnerability CVE-2024-0003 in source") {
		t.Fatalf("expected the stored critical finding to block, got %v", err)
	}

	if sourceScan == nil || sourceScan.ScannedAt != scannedAt {
		t.Fatalf("expected the stored scan timestamp to be kept, got %+v", sourceScan)
	}

	_, _, sourceScan, err = r.runProviderScan(ctx, version, missingArchive, t.TempDir(), false, policy.Resolved{Threshold: 0})
	if err != nil {
		t.Fatalf("a disabled threshold must not block: %v", err)
	}

	if sourceScan == nil || len(sourceScan.Findings) != 1 {
		t.Fatalf("expected stored findings to be re-evaluated, got %+v", sourceScan)
	}
}

func TestWave6KRunProviderScanWithoutProviderLabelIsNoop(t *testing.T) {
	version := &opendepotv1alpha1.Version{ObjectMeta: metav1.ObjectMeta{Name: "null-3-2-4", Namespace: "default"}}
	r := wave6KScanner()

	repo, binaryScan, sourceScan, err := r.runProviderScan(context.Background(), version, "", t.TempDir(), false, policy.Resolved{})
	if err != nil || repo != "" || binaryScan != nil || sourceScan != nil {
		t.Fatalf("expected an empty result, got repo=%q binary=%v source=%v err=%v", repo, binaryScan, sourceScan, err)
	}
}

func TestWave6KResolveProviderSourceRepositoryBranches(t *testing.T) {
	ctx := context.Background()
	original := lookupProviderRepo
	t.Cleanup(func() {
		lookupProviderRepo = original
	})

	override := "  https://github.com/acme/terraform-provider-acme  "
	if got := resolveProviderSourceRepository(ctx, "acme", "acme", &opendepotv1alpha1.ProviderConfig{SourceRepository: &override}); got != "https://github.com/acme/terraform-provider-acme" {
		t.Fatalf("expected trimmed override, got %q", got)
	}

	lookupProviderRepo = func(context.Context, string, string) (string, error) {
		return "", errors.New("docs API unavailable")
	}
	openTofu := opendepotv1alpha1.OpenTofuRegistryHost
	if got := resolveProviderSourceRepository(ctx, "", "null", &opendepotv1alpha1.ProviderConfig{UpstreamRegistry: &openTofu}); got != "https://github.com/hashicorp/terraform-provider-null" {
		t.Fatalf("expected heuristic fallback after lookup error, got %q", got)
	}

	lookupProviderRepo = func(context.Context, string, string) (string, error) {
		return "https://github.com/opentofu/terraform-provider-null", nil
	}
	if got := resolveProviderSourceRepository(ctx, "hashicorp", "null", &opendepotv1alpha1.ProviderConfig{UpstreamRegistry: &openTofu}); got != "https://github.com/opentofu/terraform-provider-null" {
		t.Fatalf("expected looked-up repository, got %q", got)
	}
}

func TestWave6KReadBoundedEntryLimit(t *testing.T) {
	got, err := readBoundedEntry(bytes.NewReader([]byte("abcd")), 4)
	if err != nil || string(got) != "abcd" {
		t.Fatalf("a read at the limit must succeed, got %q err=%v", got, err)
	}

	if _, err := readBoundedEntry(bytes.NewReader([]byte("abcde")), 4); !errors.Is(err, ErrProviderBinaryTooLarge) {
		t.Fatalf("an oversize stream must be rejected, got %v", err)
	}
}

func TestWave6KScanProviderBinaryHonoursCancelledContext(t *testing.T) {
	calls := wave6KMockTrivy(t, nil, nil)
	r := wave6KScanner()
	r.scanSem <- struct{}{}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	archive := wave6KWriteZip(t, "terraform-provider-null_v3.2.4", 0o755, []byte("ELF"))
	if _, err := r.scanProviderBinary(ctx, archive, t.TempDir(), false); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation while waiting for the scan slot, got %v", err)
	}

	if len(*calls) != 0 {
		t.Fatalf("trivy must not run after cancellation, ran %d times", len(*calls))
	}
}
