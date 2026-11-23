package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	"github.com/tonedefdev/opendepot/pkg/storage"
	storageTypes "github.com/tonedefdev/opendepot/pkg/storage/types"
)

func TestCovGBuildDownloadPathFromVersion(t *testing.T) {
	fileName := "vpc-1.2.0.tar.gz"
	moduleName := "vpc"
	providerName := "aws"
	agentSourceName := "reviewer"
	directory := "/data/modules"
	bucket := "modules-bucket"

	tests := []struct {
		name    string
		version opendepotv1alpha1.Version
		want    string
		wantErr bool
	}{
		{
			name: "filesystem module encodes directory path",
			version: opendepotv1alpha1.Version{Spec: opendepotv1alpha1.VersionSpec{
				FileName: &fileName,
				ModuleConfigRef: &opendepotv1alpha1.ModuleConfig{
					Name:          &moduleName,
					StorageConfig: &opendepotv1alpha1.StorageConfig{FileSystem: &opendepotv1alpha1.FileSystemConfig{DirectoryPath: &directory}},
				},
			}},
			want: "fileSystem/" + base64.RawURLEncoding.EncodeToString([]byte(directory)) + "/vpc/vpc-1.2.0.tar.gz",
		},
		{
			name: "gcs provider uses bucket",
			version: opendepotv1alpha1.Version{Spec: opendepotv1alpha1.VersionSpec{
				FileName: &fileName,
				ProviderConfigRef: &opendepotv1alpha1.ProviderConfig{
					Name:          &providerName,
					StorageConfig: &opendepotv1alpha1.StorageConfig{GCS: &opendepotv1alpha1.GoogleCloudStorageConfig{Bucket: bucket}},
				},
			}},
			want: "gcs/modules-bucket/aws/vpc-1.2.0.tar.gz",
		},
		{
			name: "s3 uses bucket region and key",
			version: opendepotv1alpha1.Version{Spec: opendepotv1alpha1.VersionSpec{
				FileName: &fileName,
				ModuleConfigRef: &opendepotv1alpha1.ModuleConfig{
					Name: &moduleName,
					StorageConfig: &opendepotv1alpha1.StorageConfig{S3: &opendepotv1alpha1.AmazonS3Config{
						Bucket: bucket,
						Region: "us-east-1",
						Key:    &fileName,
					}},
				},
			}},
			want: "s3/modules-bucket/us-east-1/vpc-1.2.0.tar.gz",
		},
		{
			name: "agent source prefixes the agent type",
			version: opendepotv1alpha1.Version{Spec: opendepotv1alpha1.VersionSpec{
				Type:           "Skill",
				FileName:       &fileName,
				AgentSourceRef: &opendepotv1alpha1.AgentSourceConfig{Name: &agentSourceName, StorageConfig: &opendepotv1alpha1.StorageConfig{GCS: &opendepotv1alpha1.GoogleCloudStorageConfig{Bucket: bucket}}},
			}},
			want: "gcs/modules-bucket/skill-reviewer/vpc-1.2.0.tar.gz",
		},
		{
			name:    "missing storage config is an error",
			version: opendepotv1alpha1.Version{Spec: opendepotv1alpha1.VersionSpec{FileName: &fileName}},
			wantErr: true,
		},
		{
			name: "missing file name is an error",
			version: opendepotv1alpha1.Version{Spec: opendepotv1alpha1.VersionSpec{
				ModuleConfigRef: &opendepotv1alpha1.ModuleConfig{
					Name:          &moduleName,
					StorageConfig: &opendepotv1alpha1.StorageConfig{GCS: &opendepotv1alpha1.GoogleCloudStorageConfig{Bucket: bucket}},
				},
			}},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildDownloadPathFromVersion(&tt.version)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("buildDownloadPathFromVersion() error = nil, want error (got path %q)", got)
				}

				return
			}

			if err != nil {
				t.Fatalf("buildDownloadPathFromVersion() unexpected error: %v", err)
			}

			if got != tt.want {
				t.Fatalf("buildDownloadPathFromVersion() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCovGInitStorageBackend(t *testing.T) {
	backend, err := initStorageBackend(context.Background(), &opendepotv1alpha1.StorageConfig{
		FileSystem: &opendepotv1alpha1.FileSystemConfig{DirectoryPath: new(string)},
	})
	if err != nil {
		t.Fatalf("initStorageBackend(filesystem) unexpected error: %v", err)
	}

	if _, ok := backend.(*storage.FileSystem); !ok {
		t.Fatalf("initStorageBackend(filesystem) = %T, want *storage.FileSystem", backend)
	}

	if _, err := initStorageBackend(context.Background(), &opendepotv1alpha1.StorageConfig{}); err == nil {
		t.Fatal("initStorageBackend(empty) error = nil, want unsupported storage configuration error")
	}
}

func TestCovGGetObjectFromStorageSystem(t *testing.T) {
	directory := t.TempDir()
	payload := []byte("module archive bytes")
	archive := filepath.Join(directory, "vpc.tar.gz")
	if err := os.WriteFile(archive, payload, 0o600); err != nil {
		t.Fatalf("write archive: %v", err)
	}

	sum := sha256.Sum256(payload)
	checksum := base64.StdEncoding.EncodeToString(sum[:])

	t.Run("streams object when checksum matches", func(t *testing.T) {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		filePath := archive
		soi := &storageTypes.StorageObjectInput{FilePath: &filePath, Method: storageTypes.Get}

		getObjectFromStorageSystem(response, request, &storage.FileSystem{}, soi, checksum)

		if response.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
		}

		if response.Body.String() != string(payload) {
			t.Fatalf("body = %q, want %q", response.Body.String(), string(payload))
		}

		if got := response.Header().Get("Content-Type"); got != "application/x-tar" {
			t.Fatalf("Content-Type = %q, want application/x-tar", got)
		}
	})

	t.Run("zip objects use zip content type", func(t *testing.T) {
		zipPath := filepath.Join(directory, "vpc.zip")
		if err := os.WriteFile(zipPath, payload, 0o600); err != nil {
			t.Fatalf("write zip: %v", err)
		}

		response := httptest.NewRecorder()
		soi := &storageTypes.StorageObjectInput{FilePath: &zipPath, Method: storageTypes.Get}

		getObjectFromStorageSystem(response, httptest.NewRequest(http.MethodGet, "/", nil), &storage.FileSystem{}, soi, checksum)

		if got := response.Header().Get("Content-Type"); got != "application/zip" {
			t.Fatalf("Content-Type = %q, want application/zip", got)
		}
	})

	t.Run("checksum mismatch is refused", func(t *testing.T) {
		response := httptest.NewRecorder()
		filePath := archive
		soi := &storageTypes.StorageObjectInput{FilePath: &filePath, Method: storageTypes.Get}

		getObjectFromStorageSystem(response, httptest.NewRequest(http.MethodGet, "/", nil), &storage.FileSystem{}, soi, "bogus")

		if response.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
		}

		if strings.Contains(response.Body.String(), "module archive bytes") {
			t.Fatal("body leaked object contents after checksum mismatch")
		}
	})

	t.Run("missing object fails checksum lookup", func(t *testing.T) {
		response := httptest.NewRecorder()
		missing := filepath.Join(directory, "missing.tar.gz")
		soi := &storageTypes.StorageObjectInput{FilePath: &missing, Method: storageTypes.Get}

		getObjectFromStorageSystem(response, httptest.NewRequest(http.MethodGet, "/", nil), &storage.FileSystem{}, soi, checksum)

		if response.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
		}
	})
}

func TestCovGServeFileSystemObjectRejectsEscapingPath(t *testing.T) {
	root := t.TempDir()
	original := opendepotFilesystemMountPath
	opendepotFilesystemMountPath = &root
	t.Cleanup(func() { opendepotFilesystemMountPath = original })

	response := httptest.NewRecorder()
	serveFileSystemObject(response, httptest.NewRequest(http.MethodGet, "/", nil), root, "..", "etc-passwd", "checksum")

	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
	}
}
