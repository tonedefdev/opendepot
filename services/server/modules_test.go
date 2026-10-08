package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"testing"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
)

func TestModuleArchiveType(t *testing.T) {
	for fileName, want := range map[string]string{
		"module.zip":    "zip",
		"module.tar":    "tar.gz",
		"module.tar.gz": "tar.gz",
	} {
		if got := moduleArchiveType(fileName); got != want {
			t.Errorf("moduleArchiveType(%q) = %q, want %q", fileName, got, want)
		}
	}
}

func TestModuleArchiveWrapper(t *testing.T) {
	t.Run("zip", func(t *testing.T) {
		var archive bytes.Buffer
		writer := zip.NewWriter(&archive)
		entry, err := writer.Create("owner-repo-sha/")
		if err != nil {
			t.Fatalf("zip.Create() error = %v", err)
		}
		if _, err := entry.Write(nil); err != nil {
			t.Fatalf("zip.Write() error = %v", err)
		}
		if err := writer.Close(); err != nil {
			t.Fatalf("zip.Close() error = %v", err)
		}

		wrapper, err := moduleArchiveWrapper(bytes.NewReader(archive.Bytes()), "zip")
		if err != nil {
			t.Fatalf("moduleArchiveWrapper() error = %v", err)
		}
		if wrapper != "owner-repo-sha" {
			t.Fatalf("moduleArchiveWrapper() = %q, want owner-repo-sha", wrapper)
		}
	})

	t.Run("tar.gz", func(t *testing.T) {
		var archive bytes.Buffer
		gzipWriter := gzip.NewWriter(&archive)
		tarWriter := tar.NewWriter(gzipWriter)
		if err := tarWriter.WriteHeader(&tar.Header{Name: "owner-repo-sha/", Typeflag: tar.TypeDir}); err != nil {
			t.Fatalf("tar.WriteHeader() error = %v", err)
		}
		if err := tarWriter.Close(); err != nil {
			t.Fatalf("tar.Close() error = %v", err)
		}
		if err := gzipWriter.Close(); err != nil {
			t.Fatalf("gzip.Close() error = %v", err)
		}

		wrapper, err := moduleArchiveWrapper(bytes.NewReader(archive.Bytes()), "tar.gz")
		if err != nil {
			t.Fatalf("moduleArchiveWrapper() error = %v", err)
		}
		if wrapper != "owner-repo-sha" {
			t.Fatalf("moduleArchiveWrapper() = %q, want owner-repo-sha", wrapper)
		}
	})
}

func TestResolveModuleArchiveWrapperFromFileSystemVersion(t *testing.T) {
	directory := t.TempDir()
	moduleName := "terraform-aws-kms"
	fileName := "module.zip"
	moduleDirectory := filepath.Join(directory, moduleName)
	if err := os.Mkdir(moduleDirectory, 0700); err != nil {
		t.Fatalf("os.Mkdir() error = %v", err)
	}

	archiveFile, err := os.Create(filepath.Join(moduleDirectory, fileName))
	if err != nil {
		t.Fatalf("os.Create() error = %v", err)
	}
	writer := zip.NewWriter(archiveFile)
	if _, err := writer.Create("terraform-aws-kms-abc123/"); err != nil {
		t.Fatalf("zip.Create() error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("zip.Close() error = %v", err)
	}
	if err := archiveFile.Close(); err != nil {
		t.Fatalf("archive.Close() error = %v", err)
	}

	version := &opendepotv1alpha1.Version{
		Spec: opendepotv1alpha1.VersionSpec{
			FileName: &fileName,
			ModuleConfigRef: &opendepotv1alpha1.ModuleConfig{
				Name: &moduleName,
				StorageConfig: &opendepotv1alpha1.StorageConfig{
					FileSystem: &opendepotv1alpha1.FileSystemConfig{DirectoryPath: &directory},
				},
			},
		},
	}

	wrapper, err := resolveModuleArchiveWrapper(context.Background(), version)
	if err != nil {
		t.Fatalf("resolveModuleArchiveWrapper() error = %v", err)
	}
	if wrapper != "terraform-aws-kms-abc123" {
		t.Fatalf("resolveModuleArchiveWrapper() = %q, want terraform-aws-kms-abc123", wrapper)
	}
}
