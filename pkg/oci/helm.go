package oci

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"time"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/tonedefdev/opendepot/api/v1alpha1"
	helmchart "helm.sh/helm/v4/pkg/chart/v2"
	"helm.sh/helm/v4/pkg/chart/v2/loader"
	helmrepo "helm.sh/helm/v4/pkg/repo/v1"
	"sigs.k8s.io/yaml"
)

// Helm OCI media types. They match the values Helm uses for `helm push` and `helm pull oci://` (Helm 3.8.0 or later).
const (
	// HelmConfigMediaType is the media type of a Helm chart's config blob, which holds the chart metadata as JSON.
	HelmConfigMediaType = "application/vnd.cncf.helm.config.v1+json"
	// HelmChartLayerMediaType is the media type of the layer that holds the chart .tgz.
	HelmChartLayerMediaType = "application/vnd.cncf.helm.chart.content.v1.tar+gzip"
	// HelmProvenanceLayerMediaType is the media type of the optional layer that holds the chart .prov file.
	HelmProvenanceLayerMediaType = "application/vnd.cncf.helm.chart.provenance.v1.prov"
)

// ParseChartMetadata reads Chart.yaml from a chart .tgz archive and returns the fields that OpenDepot records on an
// OCIArtifact Version.
func ParseChartMetadata(chartTgz io.Reader) (*v1alpha1.HelmChartMetadata, error) {
	md, err := loadChartMetadata(chartTgz)
	if err != nil {
		return nil, err
	}

	return toHelmChartMetadata(md), nil
}

// PackageHelmChart builds the OCI manifest for a Helm chart. chartTgz is the chart archive and is required. prov is the
// optional provenance file. When it is set, its layer is added before the chart layer, matching `helm push`.
func PackageHelmChart(chartTgz io.ReadSeeker, prov io.ReadSeeker) (*Packaged, error) {
	md, err := loadChartMetadata(chartTgz)
	if err != nil {
		return nil, err
	}

	if _, err := chartTgz.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("oci: rewind chart archive: %w", err)
	}

	cfg, err := json.Marshal(md)
	if err != nil {
		return nil, fmt.Errorf("oci: encode chart config: %w", err)
	}

	config, err := newBlob(HelmConfigMediaType, bytes.NewReader(cfg))
	if err != nil {
		return nil, err
	}

	chartLayer, err := newBlob(HelmChartLayerMediaType, chartTgz)
	if err != nil {
		return nil, err
	}

	var layers []Blob
	if prov != nil {
		provLayer, err := newBlob(HelmProvenanceLayerMediaType, prov)
		if err != nil {
			return nil, err
		}

		layers = append(layers, provLayer)
	}

	layers = append(layers, chartLayer)

	annotations := map[string]string{
		ocispec.AnnotationTitle:       md.Name,
		ocispec.AnnotationVersion:     md.Version,
		ocispec.AnnotationDescription: md.Description,
	}
	for k, v := range annotations {
		if v == "" {
			delete(annotations, k)
		}
	}

	return buildPackage(config, layers, "", annotations)
}

// IndexChart is one packaged chart archive to list in an index.yaml.
type IndexChart struct {
	// Archive is the chart .tgz. Its Chart.yaml supplies the name and version, and its sha256 digest is recorded.
	Archive io.ReadSeeker
	// Created is the creation time recorded for the entry. The caller sets it so the output stays deterministic.
	Created time.Time
}

// RenderIndex renders a classic Helm index.yaml for charts. Each entry's URL is server-relative:
// charts/<name>-<version>.tgz.
// Entries are grouped by chart name and sorted by version, newest first. The output is deterministic: the caller
// supplies generated and every entry's Created, and no clock is read. A duplicate name and version is an error.
func RenderIndex(charts []IndexChart, generated time.Time) ([]byte, error) {
	index := &helmrepo.IndexFile{
		APIVersion: helmrepo.APIVersionV1,
		Generated:  generated,
		Entries:    map[string]helmrepo.ChartVersions{},
	}

	seen := map[string]bool{}
	for _, c := range charts {
		if _, err := c.Archive.Seek(0, io.SeekStart); err != nil {
			return nil, fmt.Errorf("oci: rewind chart archive: %w", err)
		}

		md, err := loadChartMetadata(c.Archive)
		if err != nil {
			return nil, err
		}

		filename := md.Name + "-" + md.Version + ".tgz"
		if seen[filename] {
			return nil, fmt.Errorf("oci: duplicate index entry %s", filename)
		}

		seen[filename] = true
		d, _, err := hashContent(c.Archive)
		if err != nil {
			return nil, fmt.Errorf("oci: hash %s: %w", filename, err)
		}

		index.Entries[md.Name] = append(index.Entries[md.Name], &helmrepo.ChartVersion{
			Metadata: md,
			URLs:     []string{path.Join("charts", filename)},
			Created:  c.Created,
			Digest:   d.Encoded(),
		})
	}

	index.SortEntries()
	out, err := yaml.Marshal(index)
	if err != nil {
		return nil, fmt.Errorf("oci: encode index.yaml: %w", err)
	}

	return out, nil
}

// loadChartMetadata reads and validates Chart.yaml from a chart archive.
func loadChartMetadata(r io.Reader) (*helmchart.Metadata, error) {
	c, err := loader.LoadArchive(r)
	if err != nil {
		return nil, fmt.Errorf("oci: read chart archive: %w", err)
	}

	if c.Metadata == nil {
		return nil, errors.New("oci: chart archive has no Chart.yaml")
	}

	if err := c.Metadata.Validate(); err != nil {
		return nil, fmt.Errorf("oci: invalid Chart.yaml: %w", err)
	}

	return c.Metadata, nil
}

func toHelmChartMetadata(md *helmchart.Metadata) *v1alpha1.HelmChartMetadata {
	out := &v1alpha1.HelmChartMetadata{
		Name:        md.Name,
		Version:     md.Version,
		AppVersion:  md.AppVersion,
		Description: md.Description,
		KubeVersion: md.KubeVersion,
		Deprecated:  md.Deprecated,
		Keywords:    md.Keywords,
	}

	for _, dep := range md.Dependencies {
		if dep == nil {
			continue
		}

		out.Dependencies = append(out.Dependencies, v1alpha1.HelmChartDependency{
			Name:       dep.Name,
			Version:    dep.Version,
			Repository: dep.Repository,
		})
	}

	return out
}
