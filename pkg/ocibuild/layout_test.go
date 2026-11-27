package ocibuild

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/opencontainers/go-digest"
	specs "github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// layoutFixture describes an OCI directory layout to write. The default is one valid linux/amd64 image.
type layoutFixture struct {
	version          string
	configPlatform   ocispec.Platform
	indexPlatform    *ocispec.Platform
	manifestMedia    string
	entryAnnotations map[string]string
	entryCount       int
	manifestSubject  bool
	layerMedia       string
	layers           [][]byte
	corruptLayer     bool
	missingLayer     bool
}

func defaultLayoutFixture() layoutFixture {
	return layoutFixture{
		version:        LayoutVersion,
		configPlatform: ocispec.Platform{OS: testLinux, Architecture: testAmd64},
		indexPlatform:  &ocispec.Platform{OS: testLinux, Architecture: testAmd64},
		manifestMedia:  ocispec.MediaTypeImageManifest,
		entryCount:     1,
		layerMedia:     ocispec.MediaTypeImageLayerGzip,
	}
}

// writeLayout writes the fixture as an OCI directory layout, the way BuildKit writes it with tar=false. It also
// writes an ingest/ directory, which the reader must ignore.
func writeLayout(t *testing.T, f layoutFixture) string {
	t.Helper()

	root := t.TempDir()
	blobs := filepath.Join(root, "blobs", "sha256")
	if err := os.MkdirAll(blobs, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(filepath.Join(root, "ingest"), 0o755); err != nil {
		t.Fatal(err)
	}

	writeFile(t, filepath.Join(root, "ingest", "leftover"), "not referenced")
	writeFile(t, filepath.Join(root, "oci-layout"), `{"imageLayoutVersion":"`+f.version+`"}`)

	cfgBytes := mustJSON(t, ocispec.Image{Platform: f.configPlatform})
	cfgDigest := putBlob(t, root, cfgBytes)

	layers := f.layers
	if layers == nil {
		layers = [][]byte{[]byte("layer-bytes")}
	}

	layerDescs := make([]ocispec.Descriptor, 0, len(layers))
	for _, layer := range layers {
		layerDescs = append(layerDescs, ocispec.Descriptor{
			MediaType: f.layerMedia,
			Digest:    putBlob(t, root, layer),
			Size:      int64(len(layer)),
		})
	}

	if f.corruptLayer {
		first := layerDescs[0]
		writeFile(t, filepath.Join(blobs, first.Digest.Encoded()), string(make([]byte, first.Size)))
	}

	if f.missingLayer {
		if err := os.Remove(filepath.Join(blobs, layerDescs[0].Digest.Encoded())); err != nil {
			t.Fatal(err)
		}
	}

	manifest := ocispec.Manifest{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ocispec.MediaTypeImageManifest,
		Config: ocispec.Descriptor{
			MediaType: ocispec.MediaTypeImageConfig,
			Digest:    cfgDigest,
			Size:      int64(len(cfgBytes)),
		},
		Layers: layerDescs,
	}

	if f.manifestSubject {
		manifest.Subject = &ocispec.Descriptor{MediaType: ocispec.MediaTypeImageManifest, Digest: cfgDigest, Size: 1}
	}

	manBytes := mustJSON(t, manifest)
	entry := ocispec.Descriptor{
		MediaType:   f.manifestMedia,
		Digest:      putBlob(t, root, manBytes),
		Size:        int64(len(manBytes)),
		Platform:    f.indexPlatform,
		Annotations: f.entryAnnotations,
	}

	index := ocispec.Index{Versioned: specs.Versioned{SchemaVersion: 2}}
	for range f.entryCount {
		index.Manifests = append(index.Manifests, entry)
	}

	writeFile(t, filepath.Join(root, "index.json"), string(mustJSON(t, index)))
	return root
}

func putBlob(t *testing.T, root string, content []byte) digest.Digest {
	t.Helper()

	d := digest.FromBytes(content)
	writeFile(t, filepath.Join(root, "blobs", "sha256", d.Encoded()), string(content))
	return d
}

func writeFile(t *testing.T, p, content string) {
	t.Helper()

	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()

	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	return data
}

// platformManifest returns a PlatformManifest for platform with a digest derived from seed.
func platformManifest(t *testing.T, platform, seed string) PlatformManifest {
	t.Helper()

	p, err := ParsePlatform(platform)
	if err != nil {
		t.Fatalf("ParsePlatform(%q): %v", platform, err)
	}

	return PlatformManifest{
		Platform:  p,
		MediaType: ocispec.MediaTypeImageManifest,
		Digest:    digest.FromString("manifest-" + seed + "-" + platform),
		Size:      512,
	}
}

func TestReadLayoutAcceptsValidLayout(t *testing.T) {
	root := writeLayout(t, defaultLayoutFixture())

	p, err := ParsePlatform(testPlatformAmd64)
	if err != nil {
		t.Fatal(err)
	}

	layout, err := ReadLayout(root, p, LayoutLimits{})
	if err != nil {
		t.Fatalf("ReadLayout: %v", err)
	}

	if len(layout.Blobs) != 2 {
		t.Fatalf("blobs = %d, want config and one layer", len(layout.Blobs))
	}

	wantTotal := int64(len(layout.ManifestBytes))
	for _, b := range layout.Blobs {
		wantTotal += b.Size
	}

	if layout.TotalBytes != wantTotal {
		t.Fatalf("TotalBytes = %d, want %d", layout.TotalBytes, wantTotal)
	}

	if layout.Manifest.Digest != digest.FromBytes(layout.ManifestBytes) {
		t.Fatal("manifest descriptor digest does not match the manifest bytes")
	}
}

func TestReadLayoutValidation(t *testing.T) {
	armV7 := &ocispec.Platform{OS: testLinux, Architecture: testArm, Variant: "v7"}
	arm64 := &ocispec.Platform{OS: testLinux, Architecture: testArm64}
	unknown := &ocispec.Platform{OS: "unknown", Architecture: "unknown"}

	tests := []struct {
		name     string
		platform string
		limits   LayoutLimits
		mutate   func(*layoutFixture)
		want     error
	}{
		{
			name:     "index reference annotation is ignored",
			platform: testPlatformAmd64,
			mutate: func(f *layoutFixture) {
				f.entryAnnotations = map[string]string{"org.opencontainers.image.ref.name": "latest"}
			},
		},
		{
			name:     "arm v7 platform matches",
			platform: testPlatformArmV7,
			mutate: func(f *layoutFixture) {
				f.configPlatform = *armV7
				f.indexPlatform = armV7
			},
		},
		{
			name:     "nested index is rejected",
			platform: testPlatformAmd64,
			mutate:   func(f *layoutFixture) { f.manifestMedia = ocispec.MediaTypeImageIndex },
			want:     ErrInvalidLayout,
		},
		{
			name:     "attestation annotation is rejected",
			platform: testPlatformAmd64,
			mutate: func(f *layoutFixture) {
				f.entryAnnotations = map[string]string{"vnd.docker.reference.type": "attestation-manifest"}
			},
			want: ErrInvalidLayout,
		},
		{
			name:     "unknown platform is an attestation",
			platform: testPlatformAmd64,
			mutate:   func(f *layoutFixture) { f.indexPlatform = unknown },
			want:     ErrInvalidLayout,
		},
		{
			name:     "two manifests are rejected",
			platform: testPlatformAmd64,
			mutate:   func(f *layoutFixture) { f.entryCount = 2 },
			want:     ErrInvalidLayout,
		},
		{
			name:     "no manifests are rejected",
			platform: testPlatformAmd64,
			mutate:   func(f *layoutFixture) { f.entryCount = 0 },
			want:     ErrInvalidLayout,
		},
		{
			name:     "index platform mismatch",
			platform: testPlatformAmd64,
			mutate:   func(f *layoutFixture) { f.indexPlatform = arm64 },
			want:     ErrInvalidLayout,
		},
		{
			name:     "config platform mismatch",
			platform: testPlatformAmd64,
			mutate: func(f *layoutFixture) {
				f.configPlatform = ocispec.Platform{OS: testLinux, Architecture: testArm64}
			},
			want: ErrInvalidLayout,
		},
		{
			name:     "subject marks a referrer",
			platform: testPlatformAmd64,
			mutate:   func(f *layoutFixture) { f.manifestSubject = true },
			want:     ErrInvalidLayout,
		},
		{
			name:     "in-toto attestation layer is rejected",
			platform: testPlatformAmd64,
			mutate:   func(f *layoutFixture) { f.layerMedia = "application/vnd.in-toto+json" },
			want:     ErrInvalidLayout,
		},
		{
			name:     "too many layers",
			platform: testPlatformAmd64,
			limits:   LayoutLimits{MaxLayers: 1},
			mutate:   func(f *layoutFixture) { f.layers = [][]byte{[]byte("a"), []byte("b")} },
			want:     ErrLimitExceeded,
		},
		{
			name:     "oversize blob",
			platform: testPlatformAmd64,
			limits:   LayoutLimits{MaxBlobBytes: 4},
			mutate:   func(*layoutFixture) {},
			want:     ErrLimitExceeded,
		},
		{
			name:     "image larger than the total limit",
			platform: testPlatformAmd64,
			limits:   LayoutLimits{MaxImageBytes: 64},
			mutate:   func(*layoutFixture) {},
			want:     ErrLimitExceeded,
		},
		{
			name:     "corrupt layer fails its digest",
			platform: testPlatformAmd64,
			mutate:   func(f *layoutFixture) { f.corruptLayer = true },
			want:     ErrInvalidLayout,
		},
		{
			name:     "missing layer",
			platform: testPlatformAmd64,
			mutate:   func(f *layoutFixture) { f.missingLayer = true },
			want:     ErrInvalidLayout,
		},
		{
			name:     "unsupported layout version",
			platform: testPlatformAmd64,
			mutate:   func(f *layoutFixture) { f.version = "2.0.0" },
			want:     ErrInvalidLayout,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := defaultLayoutFixture()
			tt.mutate(&f)
			root := writeLayout(t, f)

			p, err := ParsePlatform(tt.platform)
			if err != nil {
				t.Fatal(err)
			}

			_, err = ReadLayout(root, p, tt.limits)
			if tt.want == nil {
				if err != nil {
					t.Fatalf("ReadLayout: %v", err)
				}

				return
			}

			if err == nil {
				t.Fatal("expected an error")
			}

			if !errors.Is(err, tt.want) {
				t.Fatalf("ReadLayout error = %v, want %v", err, tt.want)
			}
		})
	}
}
