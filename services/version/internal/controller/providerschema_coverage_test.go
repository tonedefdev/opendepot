package controller

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/zclconf/go-cty/cty"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	"github.com/tonedefdev/opendepot/pkg/hclschema"
)

func wave6KSchemaProvider(name, version string, storage *opendepotv1alpha1.StorageConfig) *opendepotv1alpha1.Version {
	providerName := name

	return &opendepotv1alpha1.Version{
		ObjectMeta: metav1.ObjectMeta{Name: name + "-" + strings.ReplaceAll(version, ".", "-"), Namespace: "default"},
		Spec: opendepotv1alpha1.VersionSpec{
			Type:    opendepotv1alpha1.OpenDepotProvider,
			Version: version,
			ProviderConfigRef: &opendepotv1alpha1.ProviderConfig{
				Name:          &providerName,
				StorageConfig: storage,
			},
		},
	}
}

func wave6KGzip(t *testing.T, data []byte) []byte {
	t.Helper()

	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	if _, err := gw.Write(data); err != nil {
		t.Fatalf("gzip write: %v", err)
	}

	if err := gw.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}

	return buf.Bytes()
}

func TestWave6KExtractProviderSchemaPlatformMismatchIsSkipped(t *testing.T) {
	version := wave6KSchemaProvider("null", "3.2.4", nil)
	version.Spec.OperatingSystem = "plan9"
	version.Spec.Architecture = "mips"
	r := &VersionReconciler{Log: logr.Discard()}

	ref, err := r.extractProviderSchema(context.Background(), version, "")
	if err != nil || ref != nil {
		t.Fatalf("a platform the controller does not run must be skipped, got ref=%v err=%v", ref, err)
	}
}

func TestWave6KExtractProviderSchemaRequiresProviderName(t *testing.T) {
	version := wave6KSchemaProvider("null", "3.2.4", nil)
	version.Spec.OperatingSystem = runtime.GOOS
	version.Spec.Architecture = runtime.GOARCH
	version.Spec.ProviderConfigRef.Name = nil
	r := &VersionReconciler{Log: logr.Discard()}

	_, err := r.extractProviderSchema(context.Background(), version, "")
	if err == nil || !strings.Contains(err.Error(), "providerConfigRef.name is required") {
		t.Fatalf("expected providerConfigRef.name error, got %v", err)
	}
}

func TestWave6KExtractProviderSchemaFailsWithoutTofu(t *testing.T) {
	version := wave6KSchemaProvider("null", "3.2.4", nil)
	version.Spec.OperatingSystem = runtime.GOOS
	version.Spec.Architecture = runtime.GOARCH
	r := &VersionReconciler{
		Log:                     logr.Discard(),
		TofuBinPath:             filepath.Join(t.TempDir(), "no-such-tofu"),
		SchemaExtractionTimeout: 10 * time.Second,
	}

	ref, err := r.extractProviderSchema(context.Background(), version, filepath.Join(t.TempDir(), "missing.zip"))
	if err == nil {
		t.Fatalf("expected extraction to fail without a provider archive or tofu binary, got ref=%+v", ref)
	}

	if ref != nil {
		t.Fatalf("a failed extraction must not return a schema reference")
	}
}

func TestWave6KGetProviderSchemaFilePathByStorageBackend(t *testing.T) {
	dir := t.TempDir()

	fileSystem := wave6KSchemaProvider("null", "v3.2.4", &opendepotv1alpha1.StorageConfig{
		FileSystem: &opendepotv1alpha1.FileSystemConfig{DirectoryPath: &dir},
	})
	got, err := getProviderSchemaFilePath(fileSystem)
	if err != nil {
		t.Fatalf("filesystem key: %v", err)
	}

	want := dir + "/null/schemas/3.2.4/schema.json.gz"
	if *got != want {
		t.Fatalf("filesystem key = %q, want %q", *got, want)
	}

	prefix := "opendepot/providers"
	s3 := wave6KSchemaProvider("null", "3.2.4", &opendepotv1alpha1.StorageConfig{
		S3: &opendepotv1alpha1.AmazonS3Config{Key: &prefix},
	})
	got, err = getProviderSchemaFilePath(s3)
	if err != nil {
		t.Fatalf("s3 key: %v", err)
	}

	if *got != "opendepot/providers/null/schemas/3.2.4/schema.json.gz" {
		t.Fatalf("s3 key = %q", *got)
	}

	bare := wave6KSchemaProvider("null", "3.2.4", &opendepotv1alpha1.StorageConfig{})
	got, err = getProviderSchemaFilePath(bare)
	if err != nil {
		t.Fatalf("bare key: %v", err)
	}

	if *got != "null/schemas/3.2.4/schema.json.gz" {
		t.Fatalf("bare key = %q", *got)
	}

	noStorage := wave6KSchemaProvider("null", "3.2.4", nil)
	if _, err := getProviderSchemaFilePath(noStorage); err == nil {
		t.Fatalf("a provider without storage config must fail")
	}
}

func TestWave6KSchemaResolverWithoutSchemasResolvesNothing(t *testing.T) {
	resolver := &schemaResolver{}

	if got, ok := resolver.Lookup("null_resource", "id", false); ok || got != cty.DynamicPseudoType {
		t.Fatalf("expected no attribute, got %v ok=%v", got, ok)
	}

	if _, ok := resolver.ResourceAttributes("null_resource", false); ok {
		t.Fatalf("expected no resource attributes")
	}

	if provenance := resolver.Provenance(); len(provenance) != 0 {
		t.Fatalf("expected empty provenance, got %+v", provenance)
	}
}

func TestWave6KBuildSchemaResolverReportsWarnings(t *testing.T) {
	ctx := context.Background()
	if resolver, warnings := (&VersionReconciler{}).buildSchemaResolver(ctx, &opendepotv1alpha1.Version{}, nil); resolver != nil || warnings != nil {
		t.Fatalf("no required providers must yield no resolver, got %v %v", resolver, warnings)
	}

	dir := t.TempDir()
	onboarded := wave6KSchemaProvider("null", "3.2.4", &opendepotv1alpha1.StorageConfig{
		FileSystem: &opendepotv1alpha1.FileSystemConfig{DirectoryPath: &dir},
	})
	onboarded.Namespace = "default"
	onboarded.Status.ProviderSchemaRef = &opendepotv1alpha1.ProviderSchemaRef{
		Key:    filepath.Join(dir, "null", "schemas", "3.2.4", "schema.json.gz"),
		Digest: "deadbeef",
	}

	scheme := helpersCovScheme(t)
	r := helpersCovReconciler(t, onboarded)
	r.Scheme = scheme
	r.Log = logr.Discard()

	consumer := &opendepotv1alpha1.Version{ObjectMeta: metav1.ObjectMeta{Name: "vpc-1-0-0", Namespace: "default"}}
	required := []hclschema.RequiredProvider{
		{Source: "hashicorp/aws", VersionConstraint: ">= 5.0"},
		{Source: "hashicorp/null", VersionConstraint: ">= 3.0"},
	}

	resolver, warnings := r.buildSchemaResolver(ctx, consumer, required)
	if resolver != nil {
		t.Fatalf("no schema could be loaded, expected a nil resolver")
	}

	if len(warnings) != 2 {
		t.Fatalf("expected two warnings, got %v", warnings)
	}

	if !strings.Contains(warnings[0], "provider hashicorp/aws is not onboarded") {
		t.Fatalf("unexpected first warning: %q", warnings[0])
	}

	if !strings.Contains(warnings[1], "failed to load the schema for provider hashicorp/null") {
		t.Fatalf("unexpected second warning: %q", warnings[1])
	}
}

func TestWave6KLoadReducedSchemaVerifiesDigest(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	storage := &opendepotv1alpha1.StorageConfig{
		FileSystem: &opendepotv1alpha1.FileSystemConfig{DirectoryPath: &dir},
	}

	payload := []byte(`{}`)
	sum := sha256.Sum256(payload)
	key := filepath.Join(dir, "null", "schemas", "3.2.4", "schema.json.gz")
	if err := os.MkdirAll(filepath.Dir(key), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if err := os.WriteFile(key, wave6KGzip(t, payload), 0o600); err != nil {
		t.Fatalf("write schema: %v", err)
	}

	version := wave6KSchemaProvider("null", "3.2.4", storage)
	version.Status.ProviderSchemaRef = &opendepotv1alpha1.ProviderSchemaRef{Key: key, Digest: hex.EncodeToString(sum[:])}
	r := helpersCovReconciler(t)

	schema, err := r.loadReducedSchema(ctx, version)
	if err != nil {
		t.Fatalf("loadReducedSchema: %v", err)
	}

	if schema == nil {
		t.Fatalf("expected a decoded schema")
	}

	version.Status.ProviderSchemaRef.Digest = strings.Repeat("0", 64)
	if _, err := r.loadReducedSchema(ctx, version); err == nil || !strings.Contains(err.Error(), "digest does not match") {
		t.Fatalf("a tampered digest must be rejected, got %v", err)
	}

	missing := filepath.Join(dir, "missing", "schema.json.gz")
	version.Status.ProviderSchemaRef = &opendepotv1alpha1.ProviderSchemaRef{Key: missing, Digest: hex.EncodeToString(sum[:])}
	if _, err := r.loadReducedSchema(ctx, version); err == nil || !strings.Contains(err.Error(), "failed to read provider schema") {
		t.Fatalf("a missing schema object must fail to read, got %v", err)
	}

	notGzip := filepath.Join(dir, "raw.json.gz")
	if err := os.WriteFile(notGzip, []byte(`{}`), 0o600); err != nil {
		t.Fatalf("write raw schema: %v", err)
	}

	version.Status.ProviderSchemaRef = &opendepotv1alpha1.ProviderSchemaRef{Key: notGzip, Digest: hex.EncodeToString(sum[:])}
	if _, err := r.loadReducedSchema(ctx, version); err == nil || !strings.Contains(err.Error(), "failed to gunzip provider schema") {
		t.Fatalf("an uncompressed schema must be rejected, got %v", err)
	}
}

func TestWave6KSplitProviderSource(t *testing.T) {
	ns, name, ok := splitProviderSource(" hashicorp/null ")
	if !ok || ns != "hashicorp" || name != "null" {
		t.Fatalf("split = %q %q %v", ns, name, ok)
	}

	for _, bad := range []string{"null", "a/b/c", "/null", "hashicorp/"} {
		if _, _, ok := splitProviderSource(bad); ok {
			t.Fatalf("%q must not split", bad)
		}
	}
}

func TestWave6KSelectProviderSchemaVersionPicksHighestMatch(t *testing.T) {
	dir := t.TempDir()
	storage := &opendepotv1alpha1.StorageConfig{
		FileSystem: &opendepotv1alpha1.FileSystemConfig{DirectoryPath: &dir},
	}

	older := wave6KSchemaProvider("null", "3.1.0", storage)
	older.Status.ProviderSchemaRef = &opendepotv1alpha1.ProviderSchemaRef{Key: "a"}
	newest := wave6KSchemaProvider("null", "3.2.4", storage)
	newest.Status.ProviderSchemaRef = &opendepotv1alpha1.ProviderSchemaRef{Key: "b"}
	noSchema := wave6KSchemaProvider("null", "9.9.9", storage)
	outside := wave6KSchemaProvider("null", "8.0.0", storage)
	outside.Status.ProviderSchemaRef = &opendepotv1alpha1.ProviderSchemaRef{Key: "c"}
	outside.Spec.ProviderConfigRef.Namespace = &dir
	module := wave6KSchemaProvider("null", "7.0.0", storage)
	module.Spec.Type = opendepotv1alpha1.OpenDepotModule
	module.Status.ProviderSchemaRef = &opendepotv1alpha1.ProviderSchemaRef{Key: "d"}

	candidates := []opendepotv1alpha1.Version{*older, *newest, *noSchema, *outside, *module}

	got := selectProviderSchemaVersion(candidates, hclschema.RequiredProvider{Source: "hashicorp/null", VersionConstraint: "< 3.2.4"})
	if got == nil || got.Spec.Version != "3.1.0" {
		t.Fatalf("constraint < 3.2.4 must select 3.1.0, got %+v", got)
	}

	got = selectProviderSchemaVersion(candidates, hclschema.RequiredProvider{Source: "hashicorp/null"})
	if got == nil || got.Spec.Version != "3.2.4" {
		t.Fatalf("unconstrained must select the highest schema-bearing version, got %+v", got)
	}

	got = selectProviderSchemaVersion(candidates, hclschema.RequiredProvider{Source: "hashicorp/null", VersionConstraint: ">= 10.0"})
	if got != nil {
		t.Fatalf("no version satisfies the constraint, got %+v", got)
	}

	got = selectProviderSchemaVersion(candidates, hclschema.RequiredProvider{Source: "not-a-source"})
	if got != nil {
		t.Fatalf("an invalid source must match nothing, got %+v", got)
	}
}
