package controller

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	"github.com/tonedefdev/opendepot/pkg/hclschema"
	"github.com/tonedefdev/opendepot/pkg/jev"
)

func helpersCovScheme(t *testing.T) *k8sruntime.Scheme {
	t.Helper()

	scheme := k8sruntime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add corev1 to scheme: %v", err)
	}

	if err := opendepotv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add opendepot to scheme: %v", err)
	}

	return scheme
}

func helpersCovReconciler(t *testing.T, objects ...client.Object) *VersionReconciler {
	t.Helper()

	scheme := helpersCovScheme(t)

	return &VersionReconciler{
		Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build(),
		Scheme: scheme,
	}
}

func helpersCovDecodeContract(t *testing.T, encoded string) hclschema.Contract {
	t.Helper()

	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("base64 decode: %v", err)
	}

	gr, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}

	var out hclschema.Contract
	if err := json.NewDecoder(gr).Decode(&out); err != nil {
		t.Fatalf("decode contract json: %v", err)
	}

	return out
}

func helpersCovZipEntry(t *testing.T, name string, mode os.FileMode, content []byte) []byte {
	t.Helper()

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	hdr := &zip.FileHeader{Name: name, Method: zip.Deflate}
	hdr.SetMode(mode)
	w, err := zw.CreateHeader(hdr)
	if err != nil {
		t.Fatalf("create zip header: %v", err)
	}

	if _, err := w.Write(content); err != nil {
		t.Fatalf("write zip entry: %v", err)
	}

	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}

	path := filepath.Join(t.TempDir(), "provider.zip")
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatalf("write zip file: %v", err)
	}

	return buf.Bytes()
}

func TestVersionCovEncodeContractRoundTrip(t *testing.T) {
	in := &hclschema.Contract{
		SchemaVersion: hclschema.ContractSchemaVersion,
		Module:        hclschema.ContractModule{Namespace: "ns", Name: "vpc", Version: "1.2.3"},
		Compatibility: hclschema.Compatibility{Grade: hclschema.GradePartial},
	}

	encoded, err := encodeContract(in)
	if err != nil {
		t.Fatalf("encodeContract returned error: %v", err)
	}

	got := helpersCovDecodeContract(t, encoded)
	if got.Module.Name != "vpc" || got.Module.Version != "1.2.3" {
		t.Fatalf("round trip lost module identity: %+v", got.Module)
	}

	if got.Compatibility.Grade != hclschema.GradePartial {
		t.Fatalf("round trip lost grade: %q", got.Compatibility.Grade)
	}
}

func TestVersionCovContractMetaFor(t *testing.T) {
	name := "vpc"
	cases := []struct {
		name       string
		ref        *opendepotv1alpha1.ModuleConfig
		wantName   string
		wantSource string
		wantProv   string
	}{
		{name: "no module ref", ref: nil},
		{name: "ref without name", ref: &opendepotv1alpha1.ModuleConfig{Provider: "aws"}, wantProv: "aws"},
		{name: "ref with name", ref: &opendepotv1alpha1.ModuleConfig{Name: &name, Provider: "aws"}, wantName: "vpc", wantSource: "ns/vpc/aws", wantProv: "aws"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			version := &opendepotv1alpha1.Version{
				ObjectMeta: metav1.ObjectMeta{Name: "v", Namespace: "ns"},
				Spec:       opendepotv1alpha1.VersionSpec{Version: "1.2.3", ModuleConfigRef: tc.ref},
			}

			meta := contractMetaFor(version)
			if meta.Namespace != "ns" || meta.Version != "1.2.3" {
				t.Fatalf("namespace/version not carried: %+v", meta)
			}

			if meta.Name != tc.wantName || meta.Provider != tc.wantProv || meta.Source != tc.wantSource {
				t.Fatalf("got name=%q provider=%q source=%q, want name=%q provider=%q source=%q",
					meta.Name, meta.Provider, meta.Source, tc.wantName, tc.wantProv, tc.wantSource)
			}
		})
	}
}

func TestVersionCovContractNeedsDerivation(t *testing.T) {
	ctx := context.Background()
	recent := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	old := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)

	extracted := &opendepotv1alpha1.Version{
		ObjectMeta: metav1.ObjectMeta{Name: "provider-v", Namespace: "ns"},
		Spec:       opendepotv1alpha1.VersionSpec{Type: "provider"},
		Status: opendepotv1alpha1.VersionStatus{
			ProviderSchemaRef: &opendepotv1alpha1.ProviderSchemaRef{Key: "k", ExtractedAt: recent},
		},
	}

	cases := []struct {
		name  string
		ref   *opendepotv1alpha1.ContractConfigMapRef
		extra []client.Object
		want  bool
	}{
		{name: "no contract yet", ref: nil, want: true},
		{name: "full grade is kept", ref: &opendepotv1alpha1.ContractConfigMapRef{Grade: hclschema.GradeFull, DerivedAt: old}, want: false},
		{name: "unparseable derivedAt", ref: &opendepotv1alpha1.ContractConfigMapRef{Grade: hclschema.GradePartial, DerivedAt: "not-a-time"}, want: true},
		{name: "partial with no newer schema", ref: &opendepotv1alpha1.ContractConfigMapRef{Grade: hclschema.GradePartial, DerivedAt: recent}, want: false},
		{name: "partial with newer schema", ref: &opendepotv1alpha1.ContractConfigMapRef{Grade: hclschema.GradePartial, DerivedAt: old}, extra: []client.Object{extracted}, want: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := helpersCovReconciler(t, tc.extra...)
			version := &opendepotv1alpha1.Version{
				ObjectMeta: metav1.ObjectMeta{Name: "module-v", Namespace: "ns"},
				Status:     opendepotv1alpha1.VersionStatus{ContractConfigMapRef: tc.ref},
			}

			if got := r.contractNeedsDerivation(ctx, version); got != tc.want {
				t.Fatalf("contractNeedsDerivation = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestVersionCovProviderSchemaNeedsExtraction(t *testing.T) {
	r := &VersionReconciler{}
	matching := &opendepotv1alpha1.Version{
		Spec: opendepotv1alpha1.VersionSpec{OperatingSystem: runtime.GOOS, Architecture: runtime.GOARCH},
	}

	if !r.providerSchemaNeedsExtraction(matching) {
		t.Fatal("a matching platform without a schema must need extraction")
	}

	matching.Status.ProviderSchemaRef = &opendepotv1alpha1.ProviderSchemaRef{Key: "k"}
	if r.providerSchemaNeedsExtraction(matching) {
		t.Fatal("a matching platform with a schema must not need extraction")
	}

	other := &opendepotv1alpha1.Version{
		Spec: opendepotv1alpha1.VersionSpec{OperatingSystem: "plan9", Architecture: "mips"},
	}
	if r.providerSchemaNeedsExtraction(other) {
		t.Fatal("a foreign platform must never be treated as pending")
	}
}

func TestVersionCovDeriveModuleContractWithoutArchive(t *testing.T) {
	ctx := context.Background()
	name := "vpc"
	version := &opendepotv1alpha1.Version{
		ObjectMeta: metav1.ObjectMeta{Name: "vpc-1-2-3", Namespace: "ns", UID: "uid-1"},
		Spec: opendepotv1alpha1.VersionSpec{
			Type:            "module",
			Version:         "1.2.3",
			ModuleConfigRef: &opendepotv1alpha1.ModuleConfig{Name: &name, Provider: "aws"},
		},
	}

	r := helpersCovReconciler(t, version)
	r.Log = logr.Discard()

	ref, err := r.deriveModuleContract(ctx, version, nil)
	if err != nil {
		t.Fatalf("deriveModuleContract must not fail on a missing archive: %v", err)
	}

	if ref.Name != "vpc-1-2-3-contract" || ref.Key != contractConfigMapKey {
		t.Fatalf("unexpected contract ref: %+v", ref)
	}

	if ref.Grade != hclschema.GradeUnsupported {
		t.Fatalf("grade = %q, want %q", ref.Grade, hclschema.GradeUnsupported)
	}

	cm := &corev1.ConfigMap{}
	if err := r.Get(ctx, client.ObjectKey{Name: ref.Name, Namespace: "ns"}, cm); err != nil {
		t.Fatalf("contract ConfigMap was not written: %v", err)
	}

	if cm.Labels["opendepot.defdev.io/module"] != "vpc" || cm.Labels["opendepot.defdev.io/version"] != "vpc-1-2-3" {
		t.Fatalf("unexpected labels: %v", cm.Labels)
	}

	got := helpersCovDecodeContract(t, cm.Data[contractConfigMapKey])
	if got.Compatibility.Grade != hclschema.GradeUnsupported || len(got.Compatibility.Warnings) == 0 {
		t.Fatalf("stored contract should explain the failure: %+v", got.Compatibility)
	}
}

func TestVersionCovUpsertContractConfigMapUpdatesExisting(t *testing.T) {
	ctx := context.Background()
	version := &opendepotv1alpha1.Version{
		ObjectMeta: metav1.ObjectMeta{Name: "v1", Namespace: "ns", UID: "uid-2"},
	}

	r := helpersCovReconciler(t, version)
	first := &hclschema.Contract{Compatibility: hclschema.Compatibility{Grade: hclschema.GradePartial}}
	if _, err := r.upsertContractConfigMap(ctx, version, first); err != nil {
		t.Fatalf("first upsert: %v", err)
	}

	second := &hclschema.Contract{Compatibility: hclschema.Compatibility{Grade: hclschema.GradeFull}}
	ref, err := r.upsertContractConfigMap(ctx, version, second)
	if err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	if ref.Grade != hclschema.GradeFull {
		t.Fatalf("returned grade = %q, want %q", ref.Grade, hclschema.GradeFull)
	}

	cm := &corev1.ConfigMap{}
	if err := r.Get(ctx, client.ObjectKey{Name: "v1-contract", Namespace: "ns"}, cm); err != nil {
		t.Fatalf("get ConfigMap: %v", err)
	}

	if got := helpersCovDecodeContract(t, cm.Data[contractConfigMapKey]); got.Compatibility.Grade != hclschema.GradeFull {
		t.Fatalf("ConfigMap was not updated, grade = %q", got.Compatibility.Grade)
	}
}

func TestVersionCovParseTrivyReportFiltersAndDedupes(t *testing.T) {
	report := []byte(`{"SchemaVersion":2,"Results":[
		{"Target":"go.mod","Class":"lang-pkgs","Type":"gomod",
		 "Vulnerabilities":[
		   {"VulnerabilityID":"CVE-1","PkgName":"a","InstalledVersion":"1","FixedVersion":"2","Severity":"HIGH","Title":"t1"},
		   {"VulnerabilityID":"CVE-1","PkgName":"a","InstalledVersion":"1","Severity":"HIGH","Title":"dup"}]},
		{"Target":"other/main.tf","Class":"config","Type":"terraform",
		 "Misconfigurations":[{"ID":"AVD-1","Severity":"LOW","Title":"m1","CauseMetadata":{"Resource":"res"}},
		                      {"ID":"AVD-1","Severity":"LOW","Title":"dup","CauseMetadata":{"Resource":"res"}}]}
	]}`)

	all, err := parseTrivyReport(report, nil)
	if err != nil {
		t.Fatalf("parseTrivyReport: %v", err)
	}

	if len(all) != 2 {
		t.Fatalf("expected 2 deduplicated findings, got %d: %+v", len(all), all)
	}

	if all[0].VulnerabilityID != "CVE-1" || all[0].FixedVersion != "2" || all[0].Title != "t1" {
		t.Fatalf("vulnerability mapped wrongly: %+v", all[0])
	}

	if all[1].VulnerabilityID != "AVD-1" || all[1].PkgName != "res" {
		t.Fatalf("misconfiguration mapped wrongly: %+v", all[1])
	}

	gomodOnly, err := parseTrivyReport(report, func(res trivyResult) bool {
		return strings.HasSuffix(res.Target, "go.mod")
	})
	if err != nil {
		t.Fatalf("filtered parseTrivyReport: %v", err)
	}

	if len(gomodOnly) != 1 || gomodOnly[0].VulnerabilityID != "CVE-1" {
		t.Fatalf("filter should keep only go.mod findings, got %+v", gomodOnly)
	}

	empty, err := parseTrivyReport([]byte(`{"SchemaVersion":2,"Results":[]}`), nil)
	if err != nil {
		t.Fatalf("empty report: %v", err)
	}

	if empty == nil || len(empty) != 0 {
		t.Fatalf("empty report must yield a non-nil empty slice, got %#v", empty)
	}

	if _, err := parseTrivyReport([]byte("not json"), nil); err == nil {
		t.Fatal("invalid JSON must return an error")
	}
}

func TestVersionCovParseTrivySecretsFlagsTargets(t *testing.T) {
	report := []byte(`{"SchemaVersion":2,"Results":[
		{"Target":"main.tf","Secrets":[{"RuleID":"aws-key","Severity":"CRITICAL","Title":"AWS key"}]},
		{"Target":"clean.tf"}
	]}`)

	findings, flagged, err := parseTrivySecrets(report)
	if err != nil {
		t.Fatalf("parseTrivySecrets: %v", err)
	}

	if len(findings) != 1 || findings[0].VulnerabilityID != "aws-key" || findings[0].PkgName != "main.tf" {
		t.Fatalf("unexpected secret findings: %+v", findings)
	}

	if _, ok := flagged["main.tf"]; !ok {
		t.Fatalf("main.tf must be flagged, got %v", flagged)
	}

	if _, ok := flagged["clean.tf"]; ok {
		t.Fatal("clean.tf must not be flagged")
	}

	if _, _, err := parseTrivySecrets([]byte("{")); err == nil {
		t.Fatal("invalid JSON must return an error")
	}
}

func TestVersionCovExtractBinaryFromZip(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, data []byte) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, data, 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}

		return p
	}

	binPath := write("bin.zip", helpersCovZipEntry(t, "terraform-provider-x_v1", 0o755, []byte("ELF-BYTES")))
	got, err := extractBinaryFromZip(binPath)
	if err != nil {
		t.Fatalf("extractBinaryFromZip: %v", err)
	}

	if string(got) != "ELF-BYTES" {
		t.Fatalf("extracted %q, want the binary content", got)
	}

	noExec := write("noexec.zip", helpersCovZipEntry(t, "readme", 0o644, []byte("text")))
	if _, err := extractBinaryFromZip(noExec); err == nil {
		t.Fatal("a zip without an executable entry must fail")
	}

	if _, err := extractBinaryFromZip(filepath.Join(dir, "missing.zip")); err == nil {
		t.Fatal("a missing archive must fail to open")
	}
}

func TestVersionCovExtractArchiveToTempDir(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	content := []byte("module \"x\" {}")
	if err := tw.WriteHeader(&tar.Header{Name: "main.tf", Mode: 0o644, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatalf("tar header: %v", err)
	}

	if _, err := tw.Write(content); err != nil {
		t.Fatalf("tar write: %v", err)
	}

	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}

	if err := gz.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}

	dir, cleanup, err := extractArchiveToTempDir(buf.Bytes(), "helpers-cov-*")
	if err != nil {
		t.Fatalf("extractArchiveToTempDir: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "main.tf"))
	if err != nil || string(data) != string(content) {
		t.Fatalf("extracted file mismatch: %v %q", err, data)
	}

	cleanup()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("cleanup must remove the temp dir, stat err = %v", err)
	}

	if _, _, err := extractArchiveToTempDir([]byte("not an archive"), "helpers-cov-*"); err == nil {
		t.Fatal("corrupt archive bytes must fail")
	}
}

func TestVersionCovStorageNamesAndPaths(t *testing.T) {
	name := "vpc"
	fileName := "vpc-1.2.3.zip"
	dirPath := "/data/store/"

	noStorage := &opendepotv1alpha1.Version{
		Spec: opendepotv1alpha1.VersionSpec{ModuleConfigRef: &opendepotv1alpha1.ModuleConfig{Name: &name}},
	}
	if _, err := getVersionStorageConfig(noStorage); err == nil {
		t.Fatal("a version without storage config must fail")
	}

	if _, err := getVersionName(&opendepotv1alpha1.Version{}); err == nil {
		t.Fatal("a version with no ref must have no name")
	}

	traversal := "../escape"
	bad := &opendepotv1alpha1.Version{
		Spec: opendepotv1alpha1.VersionSpec{ModuleConfigRef: &opendepotv1alpha1.ModuleConfig{Name: &traversal}},
	}
	if _, err := getVersionName(bad); err == nil {
		t.Fatal("a traversal name must be rejected")
	}

	agent := &opendepotv1alpha1.Version{
		Spec: opendepotv1alpha1.VersionSpec{
			Type:           "Skill",
			AgentSourceRef: &opendepotv1alpha1.AgentSourceConfig{Name: &name},
		},
	}
	gotName, err := getVersionName(agent)
	if err != nil || *gotName != "skill-vpc" {
		t.Fatalf("agent name = %v, %v; want skill-vpc", gotName, err)
	}

	noFile := &opendepotv1alpha1.Version{
		ObjectMeta: metav1.ObjectMeta{Name: "v"},
		Spec: opendepotv1alpha1.VersionSpec{
			ModuleConfigRef: &opendepotv1alpha1.ModuleConfig{
				Name:          &name,
				StorageConfig: &opendepotv1alpha1.StorageConfig{FileSystem: &opendepotv1alpha1.FileSystemConfig{DirectoryPath: &dirPath}},
			},
		},
	}
	if _, err := getVersionFilePath(noFile); err == nil {
		t.Fatal("a nil fileName must fail")
	}

	noFile.Spec.FileName = &fileName
	filePath, err := getVersionFilePath(noFile)
	if err != nil {
		t.Fatalf("getVersionFilePath: %v", err)
	}

	if *filePath != "/data/store/vpc/vpc-1.2.3.zip" {
		t.Fatalf("file path = %q", *filePath)
	}

	schemaPath, err := getProviderSchemaFilePath(&opendepotv1alpha1.Version{
		ObjectMeta: metav1.ObjectMeta{Name: "p"},
		Spec: opendepotv1alpha1.VersionSpec{
			Version: "v1.0.0",
			ProviderConfigRef: &opendepotv1alpha1.ProviderConfig{
				Name:          &name,
				StorageConfig: &opendepotv1alpha1.StorageConfig{FileSystem: &opendepotv1alpha1.FileSystemConfig{DirectoryPath: &dirPath}},
			},
		},
	})
	if err != nil {
		t.Fatalf("getProviderSchemaFilePath: %v", err)
	}

	if *schemaPath != "/data/store/vpc/schemas/1.0.0/schema.json.gz" {
		t.Fatalf("schema path = %q", *schemaPath)
	}

	if _, err := getProviderSchemaFilePath(noStorage); err == nil {
		t.Fatal("a schema path without storage config must fail")
	}
}

func TestVersionCovGeneratedFileNames(t *testing.T) {
	zipFormat := "zip"
	zipName, err := generateModuleFileName(&zipFormat)
	if err != nil || !strings.HasSuffix(*zipName, ".zip") {
		t.Fatalf("zip module name = %v, %v", zipName, err)
	}

	tarName, err := generateModuleFileName(nil)
	if err != nil || !strings.HasSuffix(*tarName, ".tar.gz") {
		t.Fatalf("default module name = %v, %v", tarName, err)
	}

	provName, err := generateProviderFileName("terraform-provider-x_1.0.0.zip")
	if err != nil || !strings.HasSuffix(*provName, ".zip") {
		t.Fatalf("provider name = %v, %v", provName, err)
	}
}

func TestVersionCovSchemaResolverEmpty(t *testing.T) {
	s := &schemaResolver{}

	if _, ok := s.Lookup("aws_vpc", "id", false); ok {
		t.Fatal("an empty resolver must not resolve attributes")
	}

	if _, ok := s.ResourceAttributes("aws_vpc", false); ok {
		t.Fatal("an empty resolver must not resolve resource attributes")
	}

	if got := s.Provenance(); got != nil {
		t.Fatalf("empty resolver provenance = %v, want nil", got)
	}
}

func TestVersionCovBuildSchemaResolverNoRequiredProviders(t *testing.T) {
	r := helpersCovReconciler(t)
	resolver, warnings := r.buildSchemaResolver(context.Background(), &opendepotv1alpha1.Version{}, nil)

	if resolver != nil || warnings != nil {
		t.Fatalf("no required providers must yield no resolver, got %v %v", resolver, warnings)
	}
}

func TestVersionCovBuildSchemaResolverWarnsForMissingProvider(t *testing.T) {
	ctx := context.Background()
	r := helpersCovReconciler(t)
	version := &opendepotv1alpha1.Version{ObjectMeta: metav1.ObjectMeta{Name: "m", Namespace: "ns"}}
	required := []hclschema.RequiredProvider{{LocalName: "aws", Source: "hashicorp/aws"}}

	resolver, warnings := r.buildSchemaResolver(ctx, version, required)
	if resolver != nil {
		t.Fatal("no onboarded provider must yield a nil resolver")
	}

	if len(warnings) != 1 || !strings.Contains(warnings[0], "hashicorp/aws") {
		t.Fatalf("expected one warning naming the provider, got %v", warnings)
	}
}

func TestVersionCovIncompleteRuneTail(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
		want int
	}{
		{name: "empty", in: nil, want: 0},
		{name: "ascii", in: []byte("abc"), want: 0},
		{name: "lead of two byte rune", in: []byte{'a', 0xC3}, want: 1},
		{name: "lead of three byte rune", in: []byte{'a', 0xE2}, want: 1},
		{name: "partial three byte rune", in: []byte{0xE2, 0x82}, want: 2},
		{name: "complete three byte rune", in: []byte{0xE2, 0x82, 0xAC}, want: 0},
		{name: "lead of four byte rune", in: []byte{0xF0, 0x9F, 0x98}, want: 3},
		{name: "stray continuation", in: []byte{0x80}, want: 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := incompleteRuneTail(tc.in); got != tc.want {
				t.Fatalf("incompleteRuneTail = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestVersionCovStreamEnded(t *testing.T) {
	if ended, err := streamEnded(nil); ended || err != nil {
		t.Fatalf("nil error: ended=%v err=%v", ended, err)
	}

	if ended, err := streamEnded(errors.New("EOF")); ended || err == nil {
		t.Fatalf("a plain error must be returned: ended=%v err=%v", ended, err)
	}
}

func TestVersionCovTruncateAndBoundState(t *testing.T) {
	if got := truncateUTF8("abc", 10); got != "abc" {
		t.Fatalf("short string changed: %q", got)
	}

	// "é" is two bytes, so a cut at byte 1 must back off to the rune start.
	if got := truncateUTF8("éa", 1); got != "" {
		t.Fatalf("cut inside a rune must back off, got %q", got)
	}

	if got := boundBumpState("short", false); got != "short" {
		t.Fatalf("short state changed: %q", got)
	}

	long := strings.Repeat("x", jev.MaxStateBytes+100)
	bounded := boundBumpState(long, false)
	if len(bounded) > jev.MaxStateBytes || !strings.HasSuffix(bounded, bumpStateMarker) {
		t.Fatalf("long state must be cut and marked, len=%d", len(bounded))
	}

	if got := boundBumpState("short", true); !strings.HasSuffix(got, bumpStateMarker) {
		t.Fatalf("a truncated state must carry the marker, got %q", got)
	}
}

func TestVersionCovMissingItemsAndListOrNone(t *testing.T) {
	got := missingItems([]string{"c", "a", "b", "a"}, []string{"b"})
	if strings.Join(got, ",") != "a,c" {
		t.Fatalf("missingItems = %v", got)
	}

	if got := missingItems([]string{"a"}, []string{"a"}); len(got) != 0 {
		t.Fatalf("fully present list must yield nothing, got %v", got)
	}

	if listOrNone(nil) != "none" {
		t.Fatal("empty list must render as none")
	}

	if listOrNone([]string{"a", "b"}) != "a, b" {
		t.Fatal("list must render comma separated")
	}
}

func TestVersionCovFileIsBinaryAndWindow(t *testing.T) {
	dir := t.TempDir()
	text := filepath.Join(dir, "text.tf")
	bin := filepath.Join(dir, "bin.dat")
	bad := filepath.Join(dir, "bad.dat")
	if err := os.WriteFile(text, []byte("resource \"x\" {}\n"), 0o600); err != nil {
		t.Fatalf("write text: %v", err)
	}

	if err := os.WriteFile(bin, []byte{'a', 0, 'b'}, 0o600); err != nil {
		t.Fatalf("write bin: %v", err)
	}

	if err := os.WriteFile(bad, []byte{0xFF, 0xFE, 'x'}, 0o600); err != nil {
		t.Fatalf("write bad: %v", err)
	}

	if binary, err := fileIsBinary(text); err != nil || binary {
		t.Fatalf("text file: binary=%v err=%v", binary, err)
	}

	if binary, err := fileIsBinary(bin); err != nil || !binary {
		t.Fatalf("NUL byte file must be binary: binary=%v err=%v", binary, err)
	}

	if binary, err := fileIsBinary(bad); err != nil || !binary {
		t.Fatalf("invalid UTF-8 file must be binary: binary=%v err=%v", binary, err)
	}

	if binary, err := fileIsBinary(""); err != nil || binary {
		t.Fatalf("empty path is an absent file: binary=%v err=%v", binary, err)
	}

	if _, err := fileIsBinary(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("a missing file must return an error")
	}

	data, cut, err := readWindow(text, 4)
	if err != nil || !cut || string(data) != "reso" {
		t.Fatalf("readWindow cut = %q cut=%v err=%v", data, cut, err)
	}

	data, cut, err = readWindow(text, 1000)
	if err != nil || cut || string(data) != "resource \"x\" {}\n" {
		t.Fatalf("readWindow full = %q cut=%v err=%v", data, cut, err)
	}

	data, cut, err = readWindow("", 4)
	if err != nil || cut || data != nil {
		t.Fatalf("empty path must be absent: %q cut=%v err=%v", data, cut, err)
	}
}

func TestVersionCovUnifiedFileDiff(t *testing.T) {
	dir := t.TempDir()
	before := filepath.Join(dir, "before.tf")
	after := filepath.Join(dir, "after.tf")
	binary := filepath.Join(dir, "after.bin")
	if err := os.WriteFile(before, []byte("a = 1\n"), 0o600); err != nil {
		t.Fatalf("write before: %v", err)
	}

	if err := os.WriteFile(after, []byte("a = 2\n"), 0o600); err != nil {
		t.Fatalf("write after: %v", err)
	}

	if err := os.WriteFile(binary, []byte{0, 1}, 0o600); err != nil {
		t.Fatalf("write binary: %v", err)
	}

	diff, cut, err := unifiedFileDiff("main.tf", before, after, 1024)
	if err != nil || cut {
		t.Fatalf("unifiedFileDiff err=%v cut=%v", err, cut)
	}

	if !strings.Contains(diff, "-a = 1") || !strings.Contains(diff, "+a = 2") {
		t.Fatalf("diff does not show the change:\n%s", diff)
	}

	diff, _, err = unifiedFileDiff("bin.dat", "", binary, 1024)
	if err != nil {
		t.Fatalf("binary diff: %v", err)
	}

	if !strings.Contains(diff, "Binary file bin.dat differs") {
		t.Fatalf("binary files must be described without content, got %q", diff)
	}

	if _, _, err := unifiedFileDiff("gone", filepath.Join(dir, "gone"), after, 1024); err == nil {
		t.Fatal("a missing side must return an error")
	}
}

func TestVersionCovJevChunkHelpers(t *testing.T) {
	cases := map[byte]int{'a': 1, 0xC3: 2, 0xE2: 3, 0xF0: 4, 0x80: 0}
	for c, want := range cases {
		if got := jevChunkRuneLen(c); got != want {
			t.Fatalf("jevChunkRuneLen(%#x) = %d, want %d", c, got, want)
		}
	}

	if got := jevChunkMarker(jevChunkPiece{seq: 2, of: 3, start: 0, end: 10, fileSize: 40}); got != "[part 2 of 3, bytes 0-9 of 40]\n" {
		t.Fatalf("first segment marker = %q", got)
	}

	if got := jevChunkMarker(jevChunkPiece{start: 10, end: 20, fileSize: 40}); got != "[continued, bytes 10-19 of 40]\n" {
		t.Fatalf("continued segment marker = %q", got)
	}

	if jevChunkReason(jevChunkWithheld) != "Withheld" {
		t.Fatal("withheld reason mismatch")
	}

	if jevChunkReason(jevChunkBinary) != "Binary" {
		t.Fatal("binary reason mismatch")
	}
}

func TestVersionCovJevChunkCopyRange(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("0123456789"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	var out bytes.Buffer
	if err := jevChunkCopy(&out, dir, jevChunkPiece{file: "f.txt", start: 2, size: 4}); err != nil {
		t.Fatalf("jevChunkCopy: %v", err)
	}

	if out.String() != "2345" {
		t.Fatalf("copied %q, want 2345", out.String())
	}

	if err := jevChunkCopy(&out, dir, jevChunkPiece{file: "f.txt", start: 8, size: 5}); err == nil {
		t.Fatal("a range past the end must report a short read")
	}

	if err := jevChunkCopy(&out, dir, jevChunkPiece{file: "absent.txt", size: 1}); !errors.Is(err, errJevChunkIO) {
		t.Fatalf("a missing file must wrap errJevChunkIO, got %v", err)
	}
}
