package storage

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/tonedefdev/opendepot/pkg/storage/types"
)

// ErrNotFound is returned (wrapped, via fmt.Errorf's %w) by GetObject and GetObjectChecksum
// when the requested object does not exist in the backend. Callers that need to
// distinguish "object is absent" (often a recoverable, expected condition — e.g. deciding
// whether to re-upload an artifact) from a genuine backend failure should check for it with
// errors.Is rather than treating any non-nil error as fatal.
var ErrNotFound = errors.New("object not found")

// Storage interface implements methods to store a specific Version in an external storage system.
type Storage interface {
	// Deletes a file from the configured storage system.
	DeleteObject(ctx context.Context, soi *types.StorageObjectInput) error
	// GetObject returns an io.Reader to stream the file from the underlying storage system.
	// Returns an error wrapping ErrNotFound when the object does not exist.
	GetObject(ctx context.Context, soi *types.StorageObjectInput) (io.Reader, error)
	// Gets the checksum of the file from the configured storage system. When it exists the function should store the value
	// in the soi receiver's field `ObjectChecksum` and set its `FileExists` field to `true`.
	// Returns an error wrapping ErrNotFound when the object does not exist.
	GetObjectChecksum(ctx context.Context, soi *types.StorageObjectInput) error
	// PresignObject generates a time-limited pre-signed URL for direct client download
	// and sets it on soi.PresignedURL. Backends that do not support pre-signed URLs
	// (e.g. filesystem) must return a non-nil error.
	PresignObject(ctx context.Context, soi *types.StorageObjectInput) error
	// Puts a new file into the configured storage system.
	PutObject(ctx context.Context, soi *types.StorageObjectInput) error
}

// RemoveTrailingSlash removes trailing slash characters from the string received by s.
func RemoveTrailingSlash(s *string) (*string, error) {
	if s == nil {
		return nil, fmt.Errorf("the provided string was nil")
	}

	cs := *s
	if cs[len(cs)-1] == '/' || cs[len(cs)-1] == '\\' {
		ps := *s
		ps = ps[:len(ps)-1]
		s = &ps
		return s, nil
	}

	return s, nil
}
