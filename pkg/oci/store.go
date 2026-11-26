package oci

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/opencontainers/go-digest"
	"github.com/tonedefdev/opendepot/pkg/storage"
	"github.com/tonedefdev/opendepot/pkg/storage/types"
)

// ErrDigestMismatch is returned when content does not hash to the digest it is stored or requested under.
var ErrDigestMismatch = errors.New("digest mismatch")

// ErrInvalidName is returned when a namespace, repository, tag, or digest would produce an unsafe storage path.
var ErrInvalidName = errors.New("invalid name")

const (
	blobsDir     = "blobs/sha256"
	manifestsDir = "manifests/sha256"
	tagsDir      = "tags"
	maxTagBytes  = 64 << 10
)

var (
	namePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,254}$`)
	tagPattern  = regexp.MustCompile(`^[a-zA-Z0-9_][a-zA-Z0-9._-]{0,127}$`)
	hexPattern  = regexp.MustCompile(`^[a-f0-9]{64}$`)
)

// TagPointer is the JSON document stored under tags/<tag>. It points a tag at a manifest digest.
type TagPointer struct {
	// Digest is the sha256 digest of the manifest or index the tag refers to.
	Digest string `json:"digest"`
	// MediaType is the media type of the manifest or index the tag refers to.
	MediaType string `json:"mediaType"`
	// ArtifactType is the artifactType of the manifest, when one is set.
	ArtifactType string `json:"artifactType,omitempty"`
	// UpdatedAt is when the tag was last pointed at a digest.
	UpdatedAt time.Time `json:"updatedAt"`
}

// Store keeps OCI blobs, manifests, and tag pointers in a storage.Storage backend using a
// content-addressed layout:
//
//	<root>/oci/<namespace>/<repo>/blobs/sha256/<hex>
//	<root>/oci/<namespace>/<repo>/manifests/sha256/<hex>
//	<root>/oci/<namespace>/<repo>/tags/<tag>
type Store struct {
	backend storage.Storage
	root    string
}

// NewStore returns a Store that writes through backend beneath root. For the filesystem backend root is a directory
// path. For object storage it is a key prefix, and it may be empty.
func NewStore(backend storage.Storage, root string) (*Store, error) {
	if backend == nil {
		return nil, errors.New("oci: store requires a storage backend")
	}

	return &Store{backend: backend, root: root}, nil
}

// PutBlob writes content as the blob addressed by expected. The content is hashed and compared to expected before
// anything is written, so a mismatch never reaches the backend. Writing a blob that is already stored with the same
// content is a no-op.
func (s *Store) PutBlob(
	ctx context.Context,
	namespace, repo string,
	expected digest.Digest,
	content io.ReadSeeker,
) error {
	return s.put(ctx, blobsDir, namespace, repo, expected, content)
}

// GetBlob opens the blob addressed by d. The stored content is hashed in full before the reader is returned, so a
// corrupt blob fails with ErrDigestMismatch and no bytes are returned to the caller.
func (s *Store) GetBlob(ctx context.Context, namespace, repo string, d digest.Digest) (io.ReadCloser, error) {
	return s.get(ctx, blobsDir, namespace, repo, d)
}

// PutManifest stores a manifest or index and returns its sha256 digest.
func (s *Store) PutManifest(ctx context.Context, namespace, repo string, content []byte) (digest.Digest, error) {
	d := digest.FromBytes(content)
	if err := s.put(ctx, manifestsDir, namespace, repo, d, bytes.NewReader(content)); err != nil {
		return "", err
	}

	return d, nil
}

// GetManifest returns the manifest or index addressed by d after verifying its digest.
func (s *Store) GetManifest(ctx context.Context, namespace, repo string, d digest.Digest) ([]byte, error) {
	rc, err := s.get(ctx, manifestsDir, namespace, repo, d)
	if err != nil {
		return nil, err
	}
	defer closeReader(rc)

	return io.ReadAll(rc)
}

// PutTag points tag at the digest in pointer.
func (s *Store) PutTag(ctx context.Context, namespace, repo, tag string, pointer TagPointer) error {
	key, err := s.tagPath(namespace, repo, tag)
	if err != nil {
		return err
	}

	if err := digest.Digest(pointer.Digest).Validate(); err != nil {
		return fmt.Errorf("%w: tag digest: %w", ErrInvalidName, err)
	}

	if pointer.MediaType == "" {
		return fmt.Errorf("%w: tag mediaType is required", ErrInvalidName)
	}

	data, err := json.Marshal(pointer)
	if err != nil {
		return fmt.Errorf("oci: marshal tag pointer: %w", err)
	}

	soi := &types.StorageObjectInput{FilePath: &key, Method: types.Put, FileBytes: data}
	if err := s.backend.PutObject(ctx, soi); err != nil {
		return fmt.Errorf("oci: put tag %q: %w", tag, err)
	}

	return nil
}

// GetTag returns the pointer stored for tag. It returns an error wrapping storage.ErrNotFound when the tag is absent.
func (s *Store) GetTag(ctx context.Context, namespace, repo, tag string) (*TagPointer, error) {
	key, err := s.tagPath(namespace, repo, tag)
	if err != nil {
		return nil, err
	}

	rc, err := s.backend.GetObject(ctx, &types.StorageObjectInput{FilePath: &key, Method: types.Get})
	if err != nil {
		return nil, fmt.Errorf("oci: get tag %q: %w", tag, err)
	}
	defer closeReader(rc)

	data, err := io.ReadAll(io.LimitReader(rc, maxTagBytes+1))
	if err != nil {
		return nil, fmt.Errorf("oci: read tag %q: %w", tag, err)
	}

	if len(data) > maxTagBytes {
		return nil, fmt.Errorf("oci: tag %q pointer exceeds %d bytes", tag, maxTagBytes)
	}

	var pointer TagPointer
	if err := json.Unmarshal(data, &pointer); err != nil {
		return nil, fmt.Errorf("oci: decode tag %q: %w", tag, err)
	}

	if err := digest.Digest(pointer.Digest).Validate(); err != nil {
		return nil, fmt.Errorf("oci: tag %q has an invalid digest: %w", tag, err)
	}

	return &pointer, nil
}

func (s *Store) put(
	ctx context.Context,
	kind, namespace, repo string,
	expected digest.Digest,
	content io.ReadSeeker,
) error {
	key, err := s.objectPath(kind, namespace, repo, expected)
	if err != nil {
		return err
	}

	actual, sum, err := hashContent(content)
	if err != nil {
		return fmt.Errorf("oci: hash content for %s: %w", expected, err)
	}

	if actual != expected {
		return fmt.Errorf("%w: expected %s, computed %s", ErrDigestMismatch, expected, actual)
	}

	existing := &types.StorageObjectInput{FilePath: &key}
	err = s.backend.GetObjectChecksum(ctx, existing)
	switch {
	case err == nil && existing.FileExists && existing.ObjectChecksum != nil &&
		*existing.ObjectChecksum == base64.StdEncoding.EncodeToString(sum):
		return nil
	case err != nil && !errors.Is(err, storage.ErrNotFound):
		return fmt.Errorf("oci: check %s: %w", expected, err)
	}

	if _, err := content.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("oci: rewind content for %s: %w", expected, err)
	}

	soi := &types.StorageObjectInput{FilePath: &key, Method: types.Put, FileReader: content}
	if err := s.backend.PutObject(ctx, soi); err != nil {
		return fmt.Errorf("oci: put %s: %w", expected, err)
	}

	return nil
}

func (s *Store) get(ctx context.Context, kind, namespace, repo string, d digest.Digest) (io.ReadCloser, error) {
	key, err := s.objectPath(kind, namespace, repo, d)
	if err != nil {
		return nil, err
	}

	verify, err := s.backend.GetObject(ctx, &types.StorageObjectInput{FilePath: &key, Method: types.Get})
	if err != nil {
		return nil, fmt.Errorf("oci: get %s: %w", d, err)
	}

	stored, err := digestReader(verify)
	closeReader(verify)
	if err != nil {
		return nil, fmt.Errorf("oci: read %s: %w", d, err)
	}

	if stored != d {
		return nil, fmt.Errorf("%w: stored %s has digest %s", ErrDigestMismatch, d, stored)
	}

	rc, err := s.backend.GetObject(ctx, &types.StorageObjectInput{FilePath: &key, Method: types.Get})
	if err != nil {
		return nil, fmt.Errorf("oci: reopen %s: %w", d, err)
	}

	return readCloser(rc), nil
}

func (s *Store) objectPath(kind, namespace, repo string, d digest.Digest) (string, error) {
	if err := validateName("namespace", namespace, namePattern); err != nil {
		return "", err
	}

	if err := validateName("repository", repo, namePattern); err != nil {
		return "", err
	}

	if d.Algorithm() != digest.SHA256 || !hexPattern.MatchString(d.Encoded()) {
		return "", fmt.Errorf("%w: digest %q must be sha256 with 64 lowercase hex characters", ErrInvalidName, d)
	}

	return path.Join(s.root, "oci", namespace, repo, kind, d.Encoded()), nil
}

func (s *Store) tagPath(namespace, repo, tag string) (string, error) {
	if err := validateName("namespace", namespace, namePattern); err != nil {
		return "", err
	}

	if err := validateName("repository", repo, namePattern); err != nil {
		return "", err
	}

	if err := validateName("tag", tag, tagPattern); err != nil {
		return "", err
	}

	return path.Join(s.root, "oci", namespace, repo, tagsDir, tag), nil
}

func validateName(kind, value string, pattern *regexp.Regexp) error {
	if strings.Contains(value, "..") || !pattern.MatchString(value) {
		return fmt.Errorf("%w: %s %q", ErrInvalidName, kind, value)
	}

	return nil
}

// digestReader returns the sha256 digest of everything in r.
func digestReader(r io.Reader) (digest.Digest, error) {
	h := sha256.New()
	if _, err := io.Copy(h, r); err != nil {
		return "", err
	}

	return digest.NewDigestFromBytes(digest.SHA256, h.Sum(nil)), nil
}

// hashContent returns the sha256 digest and raw sum of everything in r, reading from the start. r is left
// rewound to the start so the caller can read it again.
func hashContent(r io.ReadSeeker) (digest.Digest, []byte, error) {
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return "", nil, err
	}

	h := sha256.New()
	if _, err := io.Copy(h, r); err != nil {
		return "", nil, err
	}

	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return "", nil, err
	}

	sum := h.Sum(nil)
	return digest.NewDigestFromBytes(digest.SHA256, sum), sum, nil
}

func readCloser(r io.Reader) io.ReadCloser {
	if rc, ok := r.(io.ReadCloser); ok {
		return rc
	}

	return io.NopCloser(r)
}

func closeReader(r io.Reader) {
	if c, ok := r.(io.Closer); ok {
		_ = c.Close()
	}
}
