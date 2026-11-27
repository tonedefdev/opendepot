package ocibuild

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/opencontainers/go-digest"
	specs "github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// Annotations written on the image index that BuildIndex assembles.
const (
	AnnotationSource   = ocispec.AnnotationSource
	AnnotationRevision = ocispec.AnnotationRevision
	AnnotationVersion  = ocispec.AnnotationVersion
	AnnotationBuildID  = BuildIDKey
)

var commitSHAPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

// PlatformManifest is one platform image manifest that the index references. MediaType must be the OCI image manifest
// media type. Nested indexes are rejected.
type PlatformManifest struct {
	Platform  Platform
	MediaType string
	Digest    digest.Digest
	Size      int64
}

// IndexMetadata holds the values that BuildIndex records as annotations on the index.
type IndexMetadata struct {
	// Source is the repository URL, such as https://github.com/acme/modules.
	Source string
	// Revision is the commit SHA that the build was made from.
	Revision string
	// Version is the Version tag that the build produced.
	Version string
	// BuildID identifies the build that produced the index.
	BuildID string
}

// BuildIndex assembles an OCI image index that references each platform manifest. The index is always an index, even
// for a single platform. Entries are sorted by platform, so the same inputs always produce the same bytes. The caller
// stores the result with Store.PutManifest.
func BuildIndex(manifests []PlatformManifest, meta IndexMetadata) ([]byte, error) {
	if len(manifests) == 0 {
		return nil, errors.New("image index requires at least one platform manifest")
	}

	if err := validateIndexMetadata(meta); err != nil {
		return nil, err
	}

	sorted := slices.Clone(manifests)
	slices.SortFunc(sorted, func(a, b PlatformManifest) int {
		return strings.Compare(a.Platform.key(), b.Platform.key())
	})

	descriptors := make([]ocispec.Descriptor, 0, len(sorted))
	for i, m := range sorted {
		if err := validatePlatformManifest(m); err != nil {
			return nil, err
		}

		if i > 0 && sorted[i-1].Platform.key() == m.Platform.key() {
			return nil, fmt.Errorf("duplicate manifest for platform %s", m.Platform)
		}

		descriptors = append(descriptors, ocispec.Descriptor{
			MediaType: m.MediaType,
			Digest:    m.Digest,
			Size:      m.Size,
			Platform:  m.Platform.OCI(),
		})
	}

	index := ocispec.Index{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ocispec.MediaTypeImageIndex,
		Manifests: descriptors,
		Annotations: map[string]string{
			AnnotationSource:   meta.Source,
			AnnotationRevision: meta.Revision,
			AnnotationVersion:  meta.Version,
			AnnotationBuildID:  meta.BuildID,
		},
	}

	data, err := json.Marshal(index)
	if err != nil {
		return nil, fmt.Errorf("marshal image index: %w", err)
	}

	return data, nil
}

func validateIndexMetadata(meta IndexMetadata) error {
	if strings.TrimSpace(meta.Source) == "" {
		return errors.New("image index requires a source annotation")
	}

	if !commitSHAPattern.MatchString(meta.Revision) {
		return fmt.Errorf("image index revision %q must be a full commit SHA", meta.Revision)
	}

	if strings.TrimSpace(meta.Version) == "" {
		return errors.New("image index requires a version annotation")
	}

	if strings.TrimSpace(meta.BuildID) == "" {
		return errors.New("image index requires a build ID annotation")
	}

	return nil
}

func validatePlatformManifest(m PlatformManifest) error {
	if m.MediaType != ocispec.MediaTypeImageManifest {
		return fmt.Errorf("platform %s: media type %q must be %s (nested indexes are not allowed)",
			m.Platform, m.MediaType, ocispec.MediaTypeImageManifest)
	}

	if err := m.Digest.Validate(); err != nil || m.Digest.Algorithm() != digest.SHA256 {
		return fmt.Errorf("platform %s: digest %q must be sha256", m.Platform, m.Digest)
	}

	if m.Size <= 0 {
		return fmt.Errorf("platform %s: manifest size must be positive", m.Platform)
	}

	return nil
}
