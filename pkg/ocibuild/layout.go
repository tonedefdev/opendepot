package ocibuild

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// LayoutVersion is the imageLayoutVersion that BuildKit writes to oci-layout.
const LayoutVersion = "1.0.0"

// Defaults for LayoutLimits.
const (
	DefaultMaxLayers     = 128
	DefaultMaxBlobBytes  = int64(2) << 30
	DefaultMaxImageBytes = int64(8) << 30
)

const (
	maxLayoutOCILayoutBytes = int64(1) << 10
	maxLayoutIndexBytes     = int64(1) << 20
	maxLayoutConfigBytes    = int64(1) << 20
	maxLayoutManifestBytes  = int64(4) << 20
)

// attestationReferenceTypeKey and attestationManifestType mark BuildKit attestation manifests, which are rejected.
const (
	attestationReferenceTypeKey = "vnd.docker.reference.type"
	attestationManifestType     = "attestation-manifest"
	inTotoMediaTypePrefix       = "application/vnd.in-toto"
)

// ErrInvalidLayout is returned when an OCI layout does not hold exactly one valid image manifest for the expected
// platform.
var ErrInvalidLayout = errors.New("invalid OCI layout")

var sha256HexPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

// LayoutLimits bounds what ReadLayout accepts. A zero field takes its default.
type LayoutLimits struct {
	// MaxLayers is the largest number of layers that one platform manifest may reference.
	MaxLayers int
	// MaxBlobBytes is the largest size of any single blob: the config or a layer.
	MaxBlobBytes int64
	// MaxImageBytes is the largest total size of the manifest and every blob it references.
	MaxImageBytes int64
}

func (l LayoutLimits) withDefaults() LayoutLimits {
	if l.MaxLayers <= 0 {
		l.MaxLayers = DefaultMaxLayers
	}

	if l.MaxBlobBytes <= 0 {
		l.MaxBlobBytes = DefaultMaxBlobBytes
	}

	if l.MaxImageBytes <= 0 {
		l.MaxImageBytes = DefaultMaxImageBytes
	}

	return l
}

// LayoutBlob is a config or layer blob that the manifest references and that the uploader must store.
type LayoutBlob struct {
	Digest digest.Digest
	Size   int64
	// Path is the file that holds the blob.
	Path string
}

// Layout is one validated platform image manifest read from an OCI directory layout.
type Layout struct {
	// Manifest describes the image manifest, with its platform.
	Manifest ocispec.Descriptor
	// ManifestBytes is the manifest content. The caller stores it with Store.PutManifest.
	ManifestBytes []byte
	// Blobs are the config and layer blobs in manifest order.
	Blobs []LayoutBlob
	// TotalBytes is the size of the manifest plus every blob.
	TotalBytes int64
}

// ReadLayout reads and validates the OCI directory layout that BuildKit writes with tar=false, and returns the single
// image manifest for platform. Based on the B0 spike, it ignores the ingest/ directory and the
// org.opencontainers.image.ref.name annotation, and it reads only the blobs that index.json references.
//
// It rejects a layout that has anything other than one image manifest, a nested index, an attestation manifest or
// attestation layer, a config platform that does not match, a blob that is oversize or whose digest does not match,
// and a manifest with more layers than allowed.
func ReadLayout(root string, platform Platform, limits LayoutLimits) (*Layout, error) {
	limits = limits.withDefaults()

	if err := checkLayoutVersion(root); err != nil {
		return nil, err
	}

	entry, err := readSingleManifestEntry(root, platform)
	if err != nil {
		return nil, err
	}

	manifestBytes, err := readSmallBlob(root, entry.Digest, maxLayoutManifestBytes)
	if err != nil {
		return nil, fmt.Errorf("%w: manifest: %w", ErrInvalidLayout, err)
	}

	if int64(len(manifestBytes)) != entry.Size {
		return nil, fmt.Errorf("%w: manifest is %d bytes, index says %d", ErrInvalidLayout, len(manifestBytes), entry.Size)
	}

	manifest, err := parseImageManifest(manifestBytes, limits)
	if err != nil {
		return nil, err
	}

	if err := checkConfigPlatform(root, manifest.Config, platform, limits); err != nil {
		return nil, err
	}

	layout := &Layout{Manifest: entry, ManifestBytes: manifestBytes, TotalBytes: int64(len(manifestBytes))}
	for _, desc := range append([]ocispec.Descriptor{manifest.Config}, manifest.Layers...) {
		blob, err := verifyLayoutBlob(root, desc, limits)
		if err != nil {
			return nil, err
		}

		layout.Blobs = append(layout.Blobs, blob)
		layout.TotalBytes += blob.Size
	}

	if layout.TotalBytes > limits.MaxImageBytes {
		return nil, fmt.Errorf("%w: image is %d bytes, more than %d",
			ErrLimitExceeded, layout.TotalBytes, limits.MaxImageBytes)
	}

	return layout, nil
}

// checkLayoutVersion requires oci-layout to declare LayoutVersion.
func checkLayoutVersion(root string) error {
	data, err := readSmallFile(filepath.Join(root, "oci-layout"), maxLayoutOCILayoutBytes)
	if err != nil {
		return fmt.Errorf("%w: oci-layout: %w", ErrInvalidLayout, err)
	}

	var marker struct {
		ImageLayoutVersion string `json:"imageLayoutVersion"`
	}

	if err := json.Unmarshal(data, &marker); err != nil {
		return fmt.Errorf("%w: oci-layout: %w", ErrInvalidLayout, err)
	}

	if marker.ImageLayoutVersion != LayoutVersion {
		return fmt.Errorf("%w: imageLayoutVersion %q, want %q", ErrInvalidLayout, marker.ImageLayoutVersion, LayoutVersion)
	}

	return nil
}

// readSingleManifestEntry parses index.json and returns its one entry after checking it against platform. The entry
// must be an image manifest and not a nested index or an attestation, and its platform must match when one is set.
func readSingleManifestEntry(root string, platform Platform) (ocispec.Descriptor, error) {
	data, err := readSmallFile(filepath.Join(root, "index.json"), maxLayoutIndexBytes)
	if err != nil {
		return ocispec.Descriptor{}, fmt.Errorf("%w: index.json: %w", ErrInvalidLayout, err)
	}

	var index ocispec.Index
	if err := json.Unmarshal(data, &index); err != nil {
		return ocispec.Descriptor{}, fmt.Errorf("%w: index.json: %w", ErrInvalidLayout, err)
	}

	if len(index.Manifests) != 1 {
		return ocispec.Descriptor{}, fmt.Errorf("%w: expected exactly one manifest, found %d",
			ErrInvalidLayout, len(index.Manifests))
	}

	entry := index.Manifests[0]
	if entry.MediaType == ocispec.MediaTypeImageIndex {
		return ocispec.Descriptor{}, fmt.Errorf("%w: nested indexes are not allowed", ErrInvalidLayout)
	}

	if entry.MediaType != ocispec.MediaTypeImageManifest {
		return ocispec.Descriptor{}, fmt.Errorf("%w: media type %q must be %s",
			ErrInvalidLayout, entry.MediaType, ocispec.MediaTypeImageManifest)
	}

	if isAttestationDescriptor(entry) {
		return ocispec.Descriptor{}, fmt.Errorf("%w: attestation manifests are not allowed", ErrInvalidLayout)
	}

	if entry.Platform != nil && !platform.matches(entry.Platform.OS, entry.Platform.Architecture, entry.Platform.Variant) {
		return ocispec.Descriptor{}, fmt.Errorf("%w: index platform %s/%s does not match %s",
			ErrInvalidLayout, entry.Platform.OS, entry.Platform.Architecture, platform)
	}

	if err := entry.Digest.Validate(); err != nil || entry.Digest.Algorithm() != digest.SHA256 {
		return ocispec.Descriptor{}, fmt.Errorf("%w: manifest digest %q must be sha256", ErrInvalidLayout, entry.Digest)
	}

	return entry, nil
}

func isAttestationDescriptor(desc ocispec.Descriptor) bool {
	if desc.Annotations[attestationReferenceTypeKey] == attestationManifestType {
		return true
	}

	return desc.Platform != nil && desc.Platform.Architecture == "unknown"
}

// parseImageManifest decodes the manifest and rejects referrers, attestation layers, and manifests with too many
// layers.
func parseImageManifest(data []byte, limits LayoutLimits) (*ocispec.Manifest, error) {
	var manifest ocispec.Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("%w: manifest: %w", ErrInvalidLayout, err)
	}

	if manifest.MediaType != "" && manifest.MediaType != ocispec.MediaTypeImageManifest {
		return nil, fmt.Errorf("%w: manifest media type %q must be %s",
			ErrInvalidLayout, manifest.MediaType, ocispec.MediaTypeImageManifest)
	}

	if manifest.Subject != nil || manifest.Annotations[attestationReferenceTypeKey] == attestationManifestType {
		return nil, fmt.Errorf("%w: attestation and referrer manifests are not allowed", ErrInvalidLayout)
	}

	if manifest.Config.MediaType != ocispec.MediaTypeImageConfig {
		return nil, fmt.Errorf("%w: config media type %q must be %s",
			ErrInvalidLayout, manifest.Config.MediaType, ocispec.MediaTypeImageConfig)
	}

	if len(manifest.Layers) > limits.MaxLayers {
		return nil, fmt.Errorf("%w: %d layers, more than %d", ErrLimitExceeded, len(manifest.Layers), limits.MaxLayers)
	}

	for _, layer := range manifest.Layers {
		if strings.HasPrefix(layer.MediaType, inTotoMediaTypePrefix) {
			return nil, fmt.Errorf("%w: attestation layers are not allowed", ErrInvalidLayout)
		}
	}

	return &manifest, nil
}

// checkConfigPlatform requires the image config to name the platform that the index selected.
func checkConfigPlatform(root string, desc ocispec.Descriptor, platform Platform, limits LayoutLimits) error {
	if desc.Size > limits.MaxBlobBytes {
		return fmt.Errorf("%w: config is %d bytes", ErrLimitExceeded, desc.Size)
	}

	data, err := readSmallBlob(root, desc.Digest, min(maxLayoutConfigBytes, limits.MaxBlobBytes))
	if err != nil {
		return fmt.Errorf("%w: config: %w", ErrInvalidLayout, err)
	}

	var config ocispec.Image
	if err := json.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("%w: config: %w", ErrInvalidLayout, err)
	}

	if !platform.matches(config.OS, config.Architecture, config.Variant) {
		return fmt.Errorf("%w: config platform %s/%s/%s does not match %s",
			ErrInvalidLayout, config.OS, config.Architecture, config.Variant, platform)
	}

	return nil
}

// verifyLayoutBlob checks that the blob for desc is a regular file of the declared size, within the size limit, that
// hashes to its digest. It streams the file, so a large layer is never held in memory.
func verifyLayoutBlob(root string, desc ocispec.Descriptor, limits LayoutLimits) (LayoutBlob, error) {
	if desc.Size > limits.MaxBlobBytes {
		return LayoutBlob{}, fmt.Errorf("%w: blob %s is %d bytes", ErrLimitExceeded, desc.Digest, desc.Size)
	}

	p, err := blobPath(root, desc.Digest)
	if err != nil {
		return LayoutBlob{}, err
	}

	info, err := os.Lstat(p)
	if err != nil {
		return LayoutBlob{}, fmt.Errorf("%w: blob %s: %w", ErrInvalidLayout, desc.Digest, err)
	}

	if !info.Mode().IsRegular() || info.Size() != desc.Size {
		return LayoutBlob{}, fmt.Errorf("%w: blob %s must be a regular file of %d bytes",
			ErrInvalidLayout, desc.Digest, desc.Size)
	}

	if err := hashFileMatches(p, desc.Digest); err != nil {
		return LayoutBlob{}, err
	}

	return LayoutBlob{Digest: desc.Digest, Size: desc.Size, Path: p}, nil
}

// readSmallBlob returns a blob's content after checking its size and digest. It is used for the manifest and config.
func readSmallBlob(root string, d digest.Digest, limit int64) ([]byte, error) {
	p, err := blobPath(root, d)
	if err != nil {
		return nil, err
	}

	data, err := readSmallFile(p, limit)
	if err != nil {
		return nil, err
	}

	if digest.FromBytes(data) != d {
		return nil, fmt.Errorf("%w: blob %s has a different digest", ErrInvalidLayout, d)
	}

	return data, nil
}

// readSmallFile reads a regular file of at most limit bytes. A larger file is rejected before it is read.
func readSmallFile(p string, limit int64) ([]byte, error) {
	info, err := os.Lstat(p)
	if err != nil {
		return nil, err
	}

	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%q is not a regular file", p)
	}

	if info.Size() > limit {
		return nil, fmt.Errorf("%w: %q is %d bytes, more than %d", ErrLimitExceeded, p, info.Size(), limit)
	}

	return os.ReadFile(p)
}

func hashFileMatches(p string, want digest.Digest) error {
	f, err := os.Open(p)
	if err != nil {
		return fmt.Errorf("%w: open blob %s: %w", ErrInvalidLayout, want, err)
	}

	defer func() { _ = f.Close() }()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("read blob %s: %w", want, err)
	}

	if got := digest.NewDigestFromEncoded(digest.SHA256, hex.EncodeToString(h.Sum(nil))); got != want {
		return fmt.Errorf("%w: blob %s has digest %s", ErrInvalidLayout, want, got)
	}

	return nil
}

// blobPath returns the path of a sha256 blob in the layout, after checking the digest.
func blobPath(root string, d digest.Digest) (string, error) {
	if d.Algorithm() != digest.SHA256 || !sha256HexPattern.MatchString(d.Encoded()) {
		return "", fmt.Errorf("%w: digest %q must be sha256", ErrInvalidLayout, d)
	}

	return filepath.Join(root, "blobs", "sha256", d.Encoded()), nil
}
