package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
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

func TestServeFileSystemObject(t *testing.T) {
	root := t.TempDir()
	moduleName := "terraform-aws-kms"
	fileName := "module.zip"
	content := []byte("module archive bytes")
	moduleDirectory := filepath.Join(root, moduleName)
	if err := os.Mkdir(moduleDirectory, 0700); err != nil {
		t.Fatalf("os.Mkdir() error = %v", err)
	}

	if err := os.WriteFile(filepath.Join(moduleDirectory, fileName), content, 0600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}

	sum := sha256.Sum256(content)
	checksum := base64.StdEncoding.EncodeToString(sum[:])

	previousLogger := logger
	previousMountPath := opendepotFilesystemMountPath
	logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	opendepotFilesystemMountPath = &root
	t.Cleanup(func() {
		logger = previousLogger
		opendepotFilesystemMountPath = previousMountPath
	})

	tests := []struct {
		name       string
		dir        string
		checksum   string
		wantStatus int
		wantBody   string
	}{
		{
			name:       "serves verified object under root",
			dir:        root,
			checksum:   checksum,
			wantStatus: http.StatusOK,
			wantBody:   string(content),
		},
		{
			name:       "rejects path that escapes root",
			dir:        filepath.Join(root, ".."),
			checksum:   checksum,
			wantStatus: http.StatusForbidden,
			wantBody:   "forbidden\n",
		},
		{
			name:       "rejects checksum mismatch",
			dir:        root,
			checksum:   "bogus",
			wantStatus: http.StatusInternalServerError,
			wantBody:   "internal server error\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			serveFileSystemObject(rec, req, tt.dir, moduleName, fileName, tt.checksum)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}

			if rec.Body.String() != tt.wantBody {
				t.Fatalf("body = %q, want %q", rec.Body.String(), tt.wantBody)
			}
		})
	}
}
