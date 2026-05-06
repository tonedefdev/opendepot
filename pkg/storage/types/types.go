package types

import (
	"io"
	"time"

	versionv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
)

//go:generate stringer -type=StorageMethod
type StorageMethod int

const (
	Get StorageMethod = iota
	Delete
	Put
)

// StorageObjectInput is common configuration for various storage systems.
type StorageObjectInput struct {
	// The sha256 checksum of the Github archive as a base64 encoded string.
	ArchiveChecksum *string
	// The storage method to use. One of 'Get', 'Delete', or 'Put'
	Method StorageMethod
	// The archive file as a byte slice. For large files prefer FileReader to avoid
	// loading the full artifact into the Go heap.
	FileBytes []byte
	// FileReader is an optional streaming source for Put operations. When set, storage
	// backends use it instead of FileBytes, keeping large artifacts off the heap.
	FileReader io.ReadSeeker
	// A flag set to true when the storage system determines the file exists.
	FileExists bool
	// The file path of the storage object. This may be a reference to a cloud storage path such as an `AWS S3 Bucket` key or an `Azure Storage Blob`.
	// It may also refer to a filesystem path like `/foo/bar` on *nix based systems or `C:\foo\bar` for Windows.
	FilePath *string
	// The sha256 checksum of the object from the storage system as a base64 encoded string.
	ObjectChecksum *string
	// PresignedURL is the time-limited pre-signed URL set by PresignObject.
	// Nil when the backend does not support pre-signed URLs.
	PresignedURL *string
	// PresignTTL is how long the pre-signed URL should be valid.
	// If zero, backends use a 15-minute default.
	PresignTTL time.Duration
	// StorageConfig is the resolved storage backend configuration.
	// Backends read bucket names, account details, and other settings from this field.
	// Populated by InitStorageFactory from the Version's module or provider config ref,
	// or set directly by callers that manage backend instantiation themselves.
	StorageConfig *versionv1alpha1.StorageConfig
	// ContainerName is the logical container name used by Azure Blob Storage.
	// For Azure backends this is the blob container name. Populated by InitStorageFactory
	// from the Version's name field, or set directly by callers.
	ContainerName *string
	// The Version spec of the object Version.
	Version *versionv1alpha1.Version
}
