package oci

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	helmchart "helm.sh/helm/v4/pkg/chart/v2"
)

var updateGolden = flag.Bool("update", false, "rewrite golden files in testdata")

func TestHelmMediaTypes(t *testing.T) {
	if HelmConfigMediaType != "application/vnd.cncf.helm.config.v1+json" {
		t.Errorf("config media type = %q", HelmConfigMediaType)
	}

	if HelmChartLayerMediaType != "application/vnd.cncf.helm.chart.content.v1.tar+gzip" {
		t.Errorf("chart layer media type = %q", HelmChartLayerMediaType)
	}

	if HelmProvenanceLayerMediaType != "application/vnd.cncf.helm.chart.provenance.v1.prov" {
		t.Errorf("provenance layer media type = %q", HelmProvenanceLayerMediaType)
	}
}

func TestParseChartMetadata(t *testing.T) {
	tgz := buildChartTgz(t, testChart, testVersion, nil)

	md, err := ParseChartMetadata(bytes.NewReader(tgz))
	if err != nil {
		t.Fatalf("ParseChartMetadata: %v", err)
	}

	if md.Name != testChart || md.Version != testVersion || md.AppVersion != "2.0" {
		t.Fatalf("name/version/appVersion = %q/%q/%q", md.Name, md.Version, md.AppVersion)
	}

	if md.Description != "Demo chart demo" || md.KubeVersion != ">=1.25.0" {
		t.Fatalf("description/kubeVersion = %q/%q", md.Description, md.KubeVersion)
	}

	if len(md.Keywords) != 2 || md.Keywords[0] != testChart {
		t.Fatalf("keywords = %v", md.Keywords)
	}

	if len(md.Dependencies) != 1 || md.Dependencies[0].Name != "redis" ||
		md.Dependencies[0].Version != "17.x" || md.Dependencies[0].Repository != "https://charts.example.com" {
		t.Fatalf("dependencies = %+v", md.Dependencies)
	}
}

func TestParseChartMetadataRejectsBadArchives(t *testing.T) {
	if _, err := ParseChartMetadata(strings.NewReader("not a gzip stream")); err == nil {
		t.Error("expected error for a non-gzip archive")
	}

	noChart := makeTgz(t, map[string]string{"demo/values.yaml": "a: 1\n"})
	if _, err := ParseChartMetadata(bytes.NewReader(noChart)); err == nil {
		t.Error("expected error for an archive without Chart.yaml")
	}

	noName := makeTgz(t, map[string]string{"demo/Chart.yaml": "apiVersion: v2\nversion: 1.0.0\n"})
	if _, err := ParseChartMetadata(bytes.NewReader(noName)); err == nil {
		t.Error("expected error for Chart.yaml without a name")
	}
}

// decodeManifest parses a packaged manifest and checks its digest.
func decodeManifest(t *testing.T, p *Packaged) ocispec.Manifest {
	t.Helper()

	if p.Descriptor.Digest != digest.FromBytes(p.Manifest) {
		t.Fatalf("manifest descriptor digest %s does not match its bytes", p.Descriptor.Digest)
	}

	if p.Descriptor.Size != int64(len(p.Manifest)) {
		t.Fatalf("manifest descriptor size %d, want %d", p.Descriptor.Size, len(p.Manifest))
	}

	var m ocispec.Manifest
	if err := json.Unmarshal(p.Manifest, &m); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}

	return m
}

func readBlobContent(t *testing.T, b Blob) []byte {
	t.Helper()

	if _, err := b.Content.Seek(0, 0); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(b.Content); err != nil {
		t.Fatal(err)
	}

	return buf.Bytes()
}

func TestPackageHelmChartWithProvenance(t *testing.T) {
	tgz := buildChartTgz(t, testChart, testVersion, nil)
	prov := []byte("-----BEGIN PGP SIGNED MESSAGE-----\nprovenance\n")

	p, err := PackageHelmChart(bytes.NewReader(tgz), bytes.NewReader(prov))
	if err != nil {
		t.Fatalf("PackageHelmChart: %v", err)
	}

	m := decodeManifest(t, p)
	if m.SchemaVersion != 2 || m.MediaType != ocispec.MediaTypeImageManifest {
		t.Fatalf("manifest schema/mediaType = %d/%s", m.SchemaVersion, m.MediaType)
	}

	if m.ArtifactType != "" {
		t.Errorf("helm manifest artifactType = %q, want unset (matches helm push)", m.ArtifactType)
	}

	if m.Config.MediaType != HelmConfigMediaType {
		t.Fatalf("config mediaType = %q", m.Config.MediaType)
	}

	if len(m.Layers) != 2 {
		t.Fatalf("layers = %d, want 2 (provenance then chart)", len(m.Layers))
	}

	if m.Layers[0].MediaType != HelmProvenanceLayerMediaType || m.Layers[1].MediaType != HelmChartLayerMediaType {
		t.Fatalf("layer media types = %q, %q", m.Layers[0].MediaType, m.Layers[1].MediaType)
	}

	if m.Layers[0].Digest != digest.FromBytes(prov) || m.Layers[0].Size != int64(len(prov)) {
		t.Fatalf("provenance layer descriptor = %+v", m.Layers[0])
	}

	if m.Layers[1].Digest != digest.FromBytes(tgz) || m.Layers[1].Size != int64(len(tgz)) {
		t.Fatalf("chart layer descriptor = %+v", m.Layers[1])
	}

	if m.Annotations[ocispec.AnnotationTitle] != testChart || m.Annotations[ocispec.AnnotationVersion] != testVersion {
		t.Fatalf("annotations = %v", m.Annotations)
	}

	var cfg helmchart.Metadata
	if err := json.Unmarshal(readBlobContent(t, p.Config), &cfg); err != nil {
		t.Fatalf("config is not chart metadata JSON: %v", err)
	}

	if cfg.Name != testChart || cfg.Version != testVersion {
		t.Fatalf("config metadata = %s %s", cfg.Name, cfg.Version)
	}

	if !bytes.Equal(readBlobContent(t, p.Layers[1]), tgz) {
		t.Fatal("chart layer content does not match the input archive")
	}
}

func TestPackageHelmChartWithoutProvenance(t *testing.T) {
	tgz := buildChartTgz(t, testChart, "0.1.0", nil)

	p, err := PackageHelmChart(bytes.NewReader(tgz), nil)
	if err != nil {
		t.Fatalf("PackageHelmChart: %v", err)
	}

	m := decodeManifest(t, p)
	if len(m.Layers) != 1 || m.Layers[0].MediaType != HelmChartLayerMediaType {
		t.Fatalf("layers = %+v, want a single chart layer", m.Layers)
	}
}

func TestPackageHelmChartIsDeterministic(t *testing.T) {
	tgz := buildChartTgz(t, testChart, "0.1.0", nil)

	a, err := PackageHelmChart(bytes.NewReader(tgz), nil)
	if err != nil {
		t.Fatal(err)
	}

	b, err := PackageHelmChart(bytes.NewReader(tgz), nil)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(a.Manifest, b.Manifest) {
		t.Fatal("packaging the same chart twice produced different manifests")
	}
}

func TestPackageHelmChartRejectsInvalidArchive(t *testing.T) {
	if _, err := PackageHelmChart(bytes.NewReader([]byte("garbage")), nil); err == nil {
		t.Fatal("expected error for a non-chart archive")
	}
}

func TestHelmPackageRoundTripsThroughStore(t *testing.T) {
	ctx := context.Background()
	store, _ := newTestStore(t)
	tgz := buildChartTgz(t, testChart, "1.0.0", nil)
	prov := []byte("prov")

	p, err := PackageHelmChart(bytes.NewReader(tgz), bytes.NewReader(prov))
	if err != nil {
		t.Fatal(err)
	}

	for _, b := range append([]Blob{p.Config}, p.Layers...) {
		if err := store.PutBlob(ctx, "acme", testChart, b.Descriptor.Digest, b.Content); err != nil {
			t.Fatalf("PutBlob %s: %v", b.Descriptor.MediaType, err)
		}
	}

	d, err := store.PutManifest(ctx, "acme", testChart, p.Manifest)
	if err != nil {
		t.Fatalf("PutManifest: %v", err)
	}

	if d != p.Descriptor.Digest {
		t.Fatalf("stored manifest digest %s, want %s", d, p.Descriptor.Digest)
	}

	rc, err := store.GetBlob(ctx, "acme", testChart, p.Layers[1].Descriptor.Digest)
	if err != nil {
		t.Fatalf("GetBlob chart layer: %v", err)
	}

	got := readAllClose(t, rc)
	if !bytes.Equal(got, tgz) {
		t.Fatal("chart layer read back from the store does not match the archive")
	}
}

func indexCharts(t *testing.T) []IndexChart {
	t.Helper()

	jan15 := time.Date(2026, 1, 15, 9, 0, 0, 0, time.UTC)
	jan10 := time.Date(2026, 1, 10, 9, 0, 0, 0, time.UTC)
	feb1 := time.Date(2026, 2, 1, 9, 0, 0, 0, time.UTC)

	return []IndexChart{
		{Archive: bytes.NewReader(buildChartTgz(t, "other", "0.1.0", nil)), Created: jan15},
		{Archive: bytes.NewReader(buildChartTgz(t, testChart, "1.0.0", nil)), Created: jan10},
		{Archive: bytes.NewReader(buildChartTgz(t, testChart, "1.1.0", nil)), Created: feb1},
	}
}

func TestRenderIndexGolden(t *testing.T) {
	generated := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)

	got, err := RenderIndex(indexCharts(t), generated)
	if err != nil {
		t.Fatalf("RenderIndex: %v", err)
	}

	const golden = "testdata/index.golden.yaml"
	if *updateGolden {
		if err := os.WriteFile(golden, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden file (run with -update to create it): %v", err)
	}

	if !bytes.Equal(got, want) {
		t.Fatalf("index.yaml differs from %s:\n%s", golden, got)
	}
}

func TestRenderIndexIsDeterministicAndRelative(t *testing.T) {
	generated := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)

	first, err := RenderIndex(indexCharts(t), generated)
	if err != nil {
		t.Fatal(err)
	}

	reversed := indexCharts(t)
	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}

	second, err := RenderIndex(reversed, generated)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(first, second) {
		t.Fatal("index.yaml depends on input order")
	}

	wantEntries := []string{
		"- charts/demo-1.1.0.tgz",
		"- charts/demo-1.0.0.tgz",
		"- charts/other-0.1.0.tgz",
		"digest: " + digest.FromBytes(buildChartTgz(t, testChart, "1.0.0", nil)).Encoded(),
	}
	for _, want := range wantEntries {
		if !strings.Contains(string(first), want) {
			t.Errorf("index.yaml missing %q", want)
		}
	}

	if strings.Contains(string(first), "- http://") || strings.Contains(string(first), "- https://") {
		t.Error("index.yaml URLs must be server-relative")
	}
}

func TestRenderIndexRejectsDuplicateVersion(t *testing.T) {
	charts := []IndexChart{
		{Archive: bytes.NewReader(buildChartTgz(t, testChart, "1.0.0", nil))},
		{Archive: bytes.NewReader(buildChartTgz(t, testChart, "1.0.0", map[string]string{"extra.txt": "x"}))},
	}

	if _, err := RenderIndex(charts, time.Now()); err == nil {
		t.Fatal("duplicate name and version must be rejected")
	}
}

func TestRenderIndexRejectsInvalidArchive(t *testing.T) {
	charts := []IndexChart{{Archive: bytes.NewReader([]byte("nope"))}}
	if _, err := RenderIndex(charts, time.Now()); err == nil {
		t.Fatal("expected a chart archive error")
	}
}
