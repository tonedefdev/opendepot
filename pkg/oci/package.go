package oci

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/opencontainers/go-digest"
	specs "github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// Blob is one blob of a packaged artifact. Content is rewound to its start.
type Blob struct {
	Descriptor ocispec.Descriptor
	Content    io.ReadSeeker
}

// Packaged is an OCI artifact built in memory: its manifest and the blobs the manifest references. Store each blob
// with Store.PutBlob, the manifest with Store.PutManifest, and point a tag at it with Store.PutTag.
type Packaged struct {
	// Descriptor describes Manifest.
	Descriptor ocispec.Descriptor
	// Manifest is the JSON-encoded image manifest.
	Manifest []byte
	// Config is the manifest's config blob.
	Config Blob
	// Layers are the manifest's layer blobs, in manifest order.
	Layers []Blob
}

// newBlob hashes content and returns it as a Blob with the given media type. content is rewound afterwards.
func newBlob(mediaType string, content io.ReadSeeker) (Blob, error) {
	d, _, err := hashContent(content)
	if err != nil {
		return Blob{}, fmt.Errorf("oci: hash %s blob: %w", mediaType, err)
	}

	size, err := content.Seek(0, io.SeekEnd)
	if err != nil {
		return Blob{}, fmt.Errorf("oci: size %s blob: %w", mediaType, err)
	}

	if _, err := content.Seek(0, io.SeekStart); err != nil {
		return Blob{}, fmt.Errorf("oci: rewind %s blob: %w", mediaType, err)
	}

	return Blob{
		Descriptor: ocispec.Descriptor{MediaType: mediaType, Digest: d, Size: size},
		Content:    content,
	}, nil
}

// buildPackage encodes the image manifest that refers to config and layers.
func buildPackage(config Blob, layers []Blob, artifactType string, annotations map[string]string) (*Packaged, error) {
	descriptors := make([]ocispec.Descriptor, 0, len(layers))
	for _, layer := range layers {
		descriptors = append(descriptors, layer.Descriptor)
	}

	manifest := ocispec.Manifest{
		Versioned:    specs.Versioned{SchemaVersion: 2},
		MediaType:    ocispec.MediaTypeImageManifest,
		ArtifactType: artifactType,
		Config:       config.Descriptor,
		Layers:       descriptors,
		Annotations:  annotations,
	}

	raw, err := json.Marshal(manifest)
	if err != nil {
		return nil, fmt.Errorf("oci: encode manifest: %w", err)
	}

	return &Packaged{
		Descriptor: ocispec.Descriptor{
			MediaType: ocispec.MediaTypeImageManifest,
			Digest:    digest.FromBytes(raw),
			Size:      int64(len(raw)),
		},
		Manifest: raw,
		Config:   config,
		Layers:   layers,
	}, nil
}

// emptyConfigBlob is the OCI empty descriptor used as the config of artifacts that have no config of their own.
func emptyConfigBlob() Blob {
	desc := ocispec.DescriptorEmptyJSON
	desc.Data = nil

	return Blob{Descriptor: desc, Content: bytes.NewReader([]byte("{}"))}
}
