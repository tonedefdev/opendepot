/*
Copyright 2026 Tony Owens.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/opencontainers/go-digest"
	specs "github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	"github.com/tonedefdev/opendepot/pkg/oci"
)

// ociScanType is the ScanPolicy scan type that OCIArtifact findings are evaluated under.
const ociScanType = "oci"

// trivyFormatFlag is the Trivy flag that selects the output format.
const trivyFormatFlag = "--format"

// ociScanInput is the content that the Trivy scan of an OCIArtifact Version reads.
type ociScanInput struct {
	// root is the mirrored Image manifest or index. It is read through blobs and written as an OCI layout.
	root *oci.ResolvedManifest
	// blobs reads the stored blobs of root.
	blobs ociBlobStore
	// filePath is the local chart archive of a Helm artifact or the content of a Generic artifact.
	filePath string
}

// scanOCIArtifact runs the offline Trivy scan that matches artifactType and returns its findings. A scan error or an empty
// report is returned as an error, so the scan fails closed.
func (r *VersionReconciler) scanOCIArtifact(
	ctx context.Context,
	artifactType string,
	input ociScanInput,
) ([]opendepotv1alpha1.SecurityFinding, error) {
	dir, err := os.MkdirTemp("", "opendepot-ociscan-*")
	if err != nil {
		return nil, fmt.Errorf("create oci scan dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	var args []string
	var target string
	switch artifactType {
	case ociArtifactImage:
		if input.root == nil {
			return nil, fmt.Errorf("%w: image scan has no manifest", errOCIInvalid)
		}

		if err := writeOCILayout(ctx, input.blobs, input.root, dir); err != nil {
			return nil, fmt.Errorf("write oci layout: %w", err)
		}

		args = []string{"image", "--input", dir, trivyFormatFlag, "json", "--skip-db-update", "--skip-java-db-update", "--offline-scan"}
	case ociArtifactHelm:
		chartDir := filepath.Join(dir, "chart")
		if err := extractChartFile(input.filePath, chartDir); err != nil {
			return nil, fmt.Errorf("unpack chart: %w", err)
		}

		args = []string{"config", "--skip-check-update", trivyFormatFlag, "json"}
		target = chartDir
	case ociArtifactGeneric:
		if err := copyOCIFile(input.filePath, filepath.Join(dir, "artifact")); err != nil {
			return nil, fmt.Errorf("stage artifact: %w", err)
		}

		args = []string{"fs", "--skip-db-update", "--offline-scan", trivyFormatFlag, "json"}
		target = dir
	default:
		return nil, fmt.Errorf("%w: unsupported artifactType %q", errOCIInvalid, artifactType)
	}

	if r.TrivyCacheDir != "" {
		args = append(args, "--cache-dir", r.TrivyCacheDir)
	}

	args = append(args, "--quiet")
	if target != "" {
		args = append(args, target)
	}

	// Serialise Trivy invocations: each process loads the full vulnerability DB.
	select {
	case r.scanSem <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	output, err := runTrivy(ctx, args...)
	<-r.scanSem
	if err != nil {
		return nil, fmt.Errorf("oci scan failed: %w", err)
	}

	if len(output) == 0 {
		return nil, errors.New("oci scan failed: trivy produced no output; verify the Trivy vulnerability DB is populated in the offline cache")
	}

	findings, err := parseTrivyReport(output, nil)
	if err != nil {
		return nil, err
	}

	secrets, _, err := parseTrivySecrets(output)
	if err != nil {
		return nil, err
	}

	return append(findings, secrets...), nil
}

// writeOCILayout writes the Image as an unpacked OCI image layout in dir. The layout is a directory, because Trivy rejects
// a tar of the layout.
func writeOCILayout(ctx context.Context, blobs ociBlobStore, root *oci.ResolvedManifest, dir string) error {
	blobDir := filepath.Join(dir, "blobs", "sha256")
	if err := os.MkdirAll(blobDir, 0o755); err != nil {
		return err
	}

	if err := os.WriteFile(filepath.Join(dir, "oci-layout"), []byte(`{"imageLayoutVersion":"1.0.0"}`), 0o644); err != nil {
		return err
	}

	index := ocispec.Index{
		Versioned: specs.Versioned{SchemaVersion: 2},
		Manifests: []ocispec.Descriptor{root.Descriptor},
	}

	indexJSON, err := json.Marshal(index)
	if err != nil {
		return fmt.Errorf("encode index.json: %w", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "index.json"), indexJSON, 0o644); err != nil {
		return err
	}

	rootPath, err := layoutBlobPath(blobDir, root.Descriptor.Digest)
	if err != nil {
		return err
	}

	if err := os.WriteFile(rootPath, root.Content, 0o644); err != nil {
		return err
	}

	for _, pm := range root.Manifests {
		pmPath, err := layoutBlobPath(blobDir, pm.Descriptor.Digest)
		if err != nil {
			return err
		}

		if err := os.WriteFile(pmPath, pm.Content, 0o644); err != nil {
			return err
		}

		for _, desc := range manifestBlobs(pm.Manifest) {
			if err := copyStoredBlob(ctx, blobs, desc.Digest, blobDir); err != nil {
				return err
			}
		}
	}

	return nil
}

// layoutBlobPath returns the path of a blob inside an OCI layout. The digest must be a valid sha256 digest, so that its
// encoded form is a plain file name that cannot leave blobDir.
func layoutBlobPath(blobDir string, d digest.Digest) (string, error) {
	if d.Algorithm() != digest.SHA256 || d.Validate() != nil {
		return "", fmt.Errorf("%w: digest %q is not a sha256 digest", errOCIInvalid, d)
	}

	return filepath.Join(blobDir, d.Encoded()), nil
}

// copyStoredBlob copies a stored blob into blobDir. The store verifies the blob's digest while it is read.
func copyStoredBlob(ctx context.Context, blobs ociBlobStore, d digest.Digest, blobDir string) error {
	rc, err := blobs.getBlob(ctx, d)
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()

	blobPath, err := layoutBlobPath(blobDir, d)
	if err != nil {
		return err
	}

	f, err := os.Create(blobPath)
	if err != nil {
		return err
	}

	if _, err := io.Copy(f, rc); err != nil {
		_ = f.Close()
		return err
	}

	return f.Close()
}

// extractChartFile unpacks the chart archive at filePath into dir.
func extractChartFile(filePath, dir string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer f.Close()

	return extractChartArchive(f, dir, maxHelmChartBytes)
}

// extractChartArchive unpacks a gzip-compressed chart archive into dir. It refuses entries that would leave dir, links, and
// special files, and it fails when the decompressed size exceeds limit.
func extractChartArchive(archive io.Reader, dir string, limit int64) error {
	gz, err := gzip.NewReader(archive)
	if err != nil {
		return fmt.Errorf("chart archive is not gzip compressed: %w", err)
	}
	defer func() { _ = gz.Close() }()

	tr := tar.NewReader(&boundedReader{r: gz, limit: limit})
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}

		if err != nil {
			return fmt.Errorf("read chart archive: %w", err)
		}

		target, err := chartEntryPath(dir, hdr.Name)
		if err != nil {
			return err
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}

			if err := writeExclusiveFile(target, tr); err != nil {
				return err
			}
		default:
			return fmt.Errorf("chart archive entry %q has an unsupported type", hdr.Name)
		}
	}
}

// writeExclusiveFile writes a regular file from a chart archive or a staged artifact. It never overwrites an existing file.
func writeExclusiveFile(target string, content io.Reader) error {
	f, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}

	if _, err := io.Copy(f, content); err != nil {
		_ = f.Close()
		return err
	}

	return f.Close()
}

// chartEntryPath returns the path under dir that a chart archive entry name maps to. It rejects names that are absolute
// or that climb out of dir.
func chartEntryPath(dir, name string) (string, error) {
	clean := path.Clean(name)
	if path.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("chart archive entry %q escapes the chart directory", name)
	}

	root := filepath.Clean(dir)
	target := filepath.Join(root, filepath.FromSlash(clean))
	if target != root && !strings.HasPrefix(target, root+string(filepath.Separator)) {
		return "", fmt.Errorf("chart archive entry %q escapes the chart directory", name)
	}

	return target, nil
}

// copyOCIFile copies the file at src to dst.
func copyOCIFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	return writeExclusiveFile(dst, in)
}
