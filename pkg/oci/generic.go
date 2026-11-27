package oci

import (
	"errors"
	"fmt"
	"io"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// GenericOptions configures PackageGeneric.
type GenericOptions struct {
	// MediaType is the media type of the single layer that holds the asset.
	MediaType string
	// ArtifactType is the artifactType of the manifest.
	ArtifactType string
	// Title is the optional org.opencontainers.image.title annotation of the layer, typically the asset file name.
	Title string
}

// PackageGeneric wraps a release asset as a single-layer OCI artifact. The config is the OCI empty descriptor.
func PackageGeneric(asset io.ReadSeeker, opts GenericOptions) (*Packaged, error) {
	if opts.MediaType == "" {
		return nil, errors.New("oci: generic artifact requires a media type")
	}

	if opts.ArtifactType == "" {
		return nil, errors.New("oci: generic artifact requires an artifact type")
	}

	layer, err := newBlob(opts.MediaType, asset)
	if err != nil {
		return nil, fmt.Errorf("oci: package asset: %w", err)
	}

	if opts.Title != "" {
		layer.Descriptor.Annotations = map[string]string{ocispec.AnnotationTitle: opts.Title}
	}

	return buildPackage(emptyConfigBlob(), []Blob{layer}, opts.ArtifactType, nil)
}
