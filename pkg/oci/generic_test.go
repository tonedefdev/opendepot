package oci

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

const testAssetMediaType = "application/vnd.example.release.asset.v1"
const testAssetArtifactType = "application/vnd.example.release.v1"

func TestEmptyConfigMatchesOCIEmptyDescriptor(t *testing.T) {
	if digest.FromString("{}") != ocispec.DescriptorEmptyJSON.Digest {
		t.Fatal("OCI empty descriptor digest no longer matches sha256 of {}")
	}

	cfg := emptyConfigBlob()
	if cfg.Descriptor.MediaType != ocispec.MediaTypeEmptyJSON || cfg.Descriptor.Size != 2 {
		t.Fatalf("empty config descriptor = %+v", cfg.Descriptor)
	}

	if cfg.Descriptor.Data != nil {
		t.Fatal("empty config descriptor must not embed data in the manifest")
	}
}

func TestPackageGenericWrapsAssetAsSingleLayer(t *testing.T) {
	asset := []byte("#!/bin/sh\necho release\n")

	p, err := PackageGeneric(bytes.NewReader(asset), GenericOptions{
		MediaType:    testAssetMediaType,
		ArtifactType: testAssetArtifactType,
		Title:        "tool-linux-amd64.sh",
	})
	if err != nil {
		t.Fatalf("PackageGeneric: %v", err)
	}

	m := decodeManifest(t, p)
	if m.ArtifactType != testAssetArtifactType {
		t.Fatalf("artifactType = %q", m.ArtifactType)
	}

	if m.Config.MediaType != ocispec.MediaTypeEmptyJSON {
		t.Fatalf("config mediaType = %q, want the OCI empty type", m.Config.MediaType)
	}

	if len(m.Layers) != 1 {
		t.Fatalf("layers = %d, want 1", len(m.Layers))
	}

	layer := m.Layers[0]
	if layer.MediaType != testAssetMediaType || layer.Digest != digest.FromBytes(asset) ||
		layer.Size != int64(len(asset)) {
		t.Fatalf("layer descriptor = %+v", layer)
	}

	if layer.Annotations[ocispec.AnnotationTitle] != "tool-linux-amd64.sh" {
		t.Fatalf("layer title annotation = %v", layer.Annotations)
	}

	if !bytes.Equal(readBlobContent(t, p.Layers[0]), asset) {
		t.Fatal("layer content does not match the asset")
	}
}

func TestPackageGenericRequiresTypes(t *testing.T) {
	asset := bytes.NewReader([]byte("x"))

	if _, err := PackageGeneric(asset, GenericOptions{ArtifactType: testAssetArtifactType}); err == nil {
		t.Error("expected error without a media type")
	}

	if _, err := PackageGeneric(asset, GenericOptions{MediaType: testAssetMediaType}); err == nil {
		t.Error("expected error without an artifact type")
	}
}

func TestPackageGenericWithoutTitleHasNoLayerAnnotations(t *testing.T) {
	p, err := PackageGeneric(bytes.NewReader([]byte("data")), GenericOptions{
		MediaType:    testAssetMediaType,
		ArtifactType: testAssetArtifactType,
	})
	if err != nil {
		t.Fatal(err)
	}

	m := decodeManifest(t, p)
	if m.Layers[0].Annotations != nil {
		t.Fatalf("layer annotations = %v, want none", m.Layers[0].Annotations)
	}
}

func TestPackageGenericRoundTripsThroughStore(t *testing.T) {
	ctx := context.Background()
	store, _ := newTestStore(t)
	asset := []byte("binary release bytes")

	p, err := PackageGeneric(bytes.NewReader(asset), GenericOptions{
		MediaType:    testAssetMediaType,
		ArtifactType: testAssetArtifactType,
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, b := range append([]Blob{p.Config}, p.Layers...) {
		if err := store.PutBlob(ctx, "acme", "tool", b.Descriptor.Digest, b.Content); err != nil {
			t.Fatalf("PutBlob: %v", err)
		}
	}

	if _, err := store.PutManifest(ctx, "acme", "tool", p.Manifest); err != nil {
		t.Fatalf("PutManifest: %v", err)
	}

	stored, err := store.GetManifest(ctx, "acme", "tool", p.Descriptor.Digest)
	if err != nil {
		t.Fatalf("GetManifest: %v", err)
	}

	var m ocispec.Manifest
	if err := json.Unmarshal(stored, &m); err != nil {
		t.Fatal(err)
	}

	rc, err := store.GetBlob(ctx, "acme", "tool", m.Layers[0].Digest)
	if err != nil {
		t.Fatalf("GetBlob: %v", err)
	}

	if got := readAllClose(t, rc); !bytes.Equal(got, asset) {
		t.Fatal("stored asset does not match")
	}
}
