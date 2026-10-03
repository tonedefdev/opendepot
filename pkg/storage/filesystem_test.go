package storage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/tonedefdev/opendepot/pkg/storage/types"
)

// TestFileSystemGetObjectNotFound guards against GetObject returning (nil, nil)
// for a missing file — that shape violates the io.Reader/error contract every
// caller relies on (err == nil implies a usable reader) and previously caused
// a nil pointer panic downstream in gzip.NewReader.
func TestFileSystemGetObjectNotFound(t *testing.T) {
	fs := &FileSystem{}
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	soi := &types.StorageObjectInput{FilePath: &missing}

	reader, err := fs.GetObject(context.Background(), soi)
	if err == nil {
		t.Fatal("GetObject should return an error for a missing file, got nil")
	}

	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("GetObject error = %v, want wrapped os.ErrNotExist", err)
	}

	if !errors.Is(err, ErrNotFound) {
		t.Errorf("GetObject error = %v, want wrapped ErrNotFound", err)
	}

	if reader != nil {
		t.Errorf("GetObject reader = %v, want nil alongside the error", reader)
	}
}

// TestFileSystemGetObjectChecksumNotFound mirrors TestFileSystemGetObjectNotFound
// for GetObjectChecksum, which had the same (nil, nil) not-found bug.
func TestFileSystemGetObjectChecksumNotFound(t *testing.T) {
	fs := &FileSystem{}
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	soi := &types.StorageObjectInput{FilePath: &missing}

	err := fs.GetObjectChecksum(context.Background(), soi)
	if err == nil {
		t.Fatal("GetObjectChecksum should return an error for a missing file, got nil")
	}

	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("GetObjectChecksum error = %v, want wrapped os.ErrNotExist", err)
	}

	if !errors.Is(err, ErrNotFound) {
		t.Errorf("GetObjectChecksum error = %v, want wrapped ErrNotFound", err)
	}

	if soi.FileExists {
		t.Error("soi.FileExists should remain false for a missing file")
	}
}

// TestFileSystemGetObjectFound is a basic sanity check that the happy path
// still works after the not-found fix.
func TestFileSystemGetObjectFound(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "object.txt")
	if err := os.WriteFile(path, []byte("hello"), 0600); err != nil {
		t.Fatalf("failed to write fixture file: %v", err)
	}

	fs := &FileSystem{}
	soi := &types.StorageObjectInput{FilePath: &path}

	reader, err := fs.GetObject(context.Background(), soi)
	if err != nil {
		t.Fatalf("GetObject failed: %v", err)
	}

	if reader == nil {
		t.Fatal("GetObject reader should not be nil for an existing file")
	}
}
