package storage

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tonedefdev/opendepot/pkg/storage/types"
)

func TestFileSystemPutObjectFromBytesCreatesDirectory(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "nested", "module.zip")
	soi := &types.StorageObjectInput{FilePath: &filePath, FileBytes: []byte("archive")}

	fs := &FileSystem{}
	if err := fs.PutObject(context.Background(), soi); err != nil {
		t.Fatalf("PutObject returned error: %v", err)
	}

	got, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("reading written file: %v", err)
	}

	if string(got) != "archive" {
		t.Errorf("file contents = %q, want %q", got, "archive")
	}

	if soi.BytesWritten != int64(len("archive")) {
		t.Errorf("BytesWritten = %d, want %d", soi.BytesWritten, len("archive"))
	}
}

func TestFileSystemPutObjectFromReaderStreamsContent(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "module.zip")
	soi := &types.StorageObjectInput{
		FilePath:   &filePath,
		FileReader: strings.NewReader("streamed-archive"),
	}

	fs := &FileSystem{}
	if err := fs.PutObject(context.Background(), soi); err != nil {
		t.Fatalf("PutObject returned error: %v", err)
	}

	got, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("reading written file: %v", err)
	}

	if string(got) != "streamed-archive" {
		t.Errorf("file contents = %q, want %q", got, "streamed-archive")
	}

	if soi.BytesWritten != int64(len("streamed-archive")) {
		t.Errorf("BytesWritten = %d, want %d", soi.BytesWritten, len("streamed-archive"))
	}
}

func TestFileSystemPutObjectTruncatesExistingFile(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "module.zip")
	if err := os.WriteFile(filePath, []byte("a much longer previous archive"), 0644); err != nil {
		t.Fatalf("seeding file: %v", err)
	}

	soi := &types.StorageObjectInput{FilePath: &filePath, FileBytes: []byte("new")}
	fs := &FileSystem{}
	if err := fs.PutObject(context.Background(), soi); err != nil {
		t.Fatalf("PutObject returned error: %v", err)
	}

	got, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("reading written file: %v", err)
	}

	if string(got) != "new" {
		t.Errorf("file contents = %q, want %q", got, "new")
	}
}

func TestFileSystemPutObjectParentIsFile(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0644); err != nil {
		t.Fatalf("creating blocker file: %v", err)
	}

	filePath := filepath.Join(blocker, "module.zip")
	soi := &types.StorageObjectInput{FilePath: &filePath, FileBytes: []byte("archive")}

	fs := &FileSystem{}
	if err := fs.PutObject(context.Background(), soi); err == nil {
		t.Error("expected error when the parent path is a regular file")
	}
}

func TestFileSystemGetObjectChecksumComputesSha256(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "module.zip")
	content := []byte("checksum-me")
	if err := os.WriteFile(filePath, content, 0644); err != nil {
		t.Fatalf("seeding file: %v", err)
	}

	sum := sha256.Sum256(content)
	want := base64.StdEncoding.EncodeToString(sum[:])

	soi := &types.StorageObjectInput{FilePath: &filePath}
	fs := &FileSystem{}
	if err := fs.GetObjectChecksum(context.Background(), soi); err != nil {
		t.Fatalf("GetObjectChecksum returned error: %v", err)
	}

	if soi.ObjectChecksum == nil || *soi.ObjectChecksum != want {
		t.Errorf("ObjectChecksum = %v, want %q", soi.ObjectChecksum, want)
	}

	if !soi.FileExists {
		t.Error("expected FileExists to be true")
	}
}

func TestFileSystemGetObjectReturnsContents(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "module.zip")
	if err := os.WriteFile(filePath, []byte("readable"), 0644); err != nil {
		t.Fatalf("seeding file: %v", err)
	}

	soi := &types.StorageObjectInput{FilePath: &filePath}
	fs := &FileSystem{}
	reader, err := fs.GetObject(context.Background(), soi)
	if err != nil {
		t.Fatalf("GetObject returned error: %v", err)
	}

	if closer, ok := reader.(io.Closer); ok {
		defer closer.Close()
	}

	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("reading object: %v", err)
	}

	if string(got) != "readable" {
		t.Errorf("object contents = %q, want %q", got, "readable")
	}
}

func TestFileSystemGetObjectStatErrorPropagates(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0644); err != nil {
		t.Fatalf("creating blocker file: %v", err)
	}

	filePath := filepath.Join(blocker, "module.zip")
	soi := &types.StorageObjectInput{FilePath: &filePath}

	fs := &FileSystem{}
	_, err := fs.GetObject(context.Background(), soi)
	if err == nil {
		t.Fatal("expected error when stat fails")
	}

	if errors.Is(err, ErrNotFound) {
		t.Errorf("stat failure should not be reported as ErrNotFound: %v", err)
	}
}

func TestFileSystemGetObjectChecksumStatErrorPropagates(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0644); err != nil {
		t.Fatalf("creating blocker file: %v", err)
	}

	filePath := filepath.Join(blocker, "module.zip")
	soi := &types.StorageObjectInput{FilePath: &filePath}

	fs := &FileSystem{}
	if err := fs.GetObjectChecksum(context.Background(), soi); err == nil {
		t.Fatal("expected error when stat fails")
	}
}

func TestFileSystemDeleteObject(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "module.zip")
	if err := os.WriteFile(filePath, []byte("x"), 0644); err != nil {
		t.Fatalf("seeding file: %v", err)
	}

	soi := &types.StorageObjectInput{FilePath: &filePath}
	fs := &FileSystem{}
	if err := fs.DeleteObject(context.Background(), soi); err != nil {
		t.Fatalf("DeleteObject returned error: %v", err)
	}

	if _, err := os.Stat(filePath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected file to be removed, stat err = %v", err)
	}
}

func TestFileSystemDeleteObjectMissingIsNoop(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "missing.zip")

	soi := &types.StorageObjectInput{FilePath: &filePath}
	fs := &FileSystem{}
	if err := fs.DeleteObject(context.Background(), soi); err != nil {
		t.Errorf("DeleteObject on missing file returned error: %v", err)
	}
}

func TestFileSystemDeleteObjectRemoveErrorPropagates(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "not-empty")
	if err := os.MkdirAll(filepath.Join(filePath, "child"), 0755); err != nil {
		t.Fatalf("creating directory: %v", err)
	}

	soi := &types.StorageObjectInput{FilePath: &filePath}
	fs := &FileSystem{}
	if err := fs.DeleteObject(context.Background(), soi); err == nil {
		t.Error("expected error when removing a non-empty directory")
	}
}

func TestFileSystemPresignObjectUnsupported(t *testing.T) {
	filePath := "module.zip"
	soi := &types.StorageObjectInput{FilePath: &filePath}

	fs := &FileSystem{}
	if err := fs.PresignObject(context.Background(), soi); err == nil {
		t.Error("expected error because filesystem storage does not support presigned URLs")
	}

	if soi.PresignedURL != nil {
		t.Errorf("PresignedURL = %v, want nil", *soi.PresignedURL)
	}
}
