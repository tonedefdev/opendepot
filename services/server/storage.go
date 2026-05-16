package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	"github.com/tonedefdev/opendepot/pkg/storage"
	storageTypes "github.com/tonedefdev/opendepot/pkg/storage/types"
)

// buildDownloadPathFromVersion constructs the storage-backend path segment appended to
// /opendepot/modules/v1/download/ for a given Version resource. It inspects both
// ModuleConfigRef and ProviderConfigRef storage configs and returns an error if
// neither is populated or the storage backend is not recognised.
func buildDownloadPathFromVersion(versionResource *opendepotv1alpha1.Version) (string, error) {
	var storageConfig *opendepotv1alpha1.StorageConfig
	var name *string

	if versionResource.Spec.ModuleConfigRef != nil && versionResource.Spec.ModuleConfigRef.StorageConfig != nil {
		storageConfig = versionResource.Spec.ModuleConfigRef.StorageConfig
		name = versionResource.Spec.ModuleConfigRef.Name
	} else if versionResource.Spec.ProviderConfigRef != nil && versionResource.Spec.ProviderConfigRef.StorageConfig != nil {
		storageConfig = versionResource.Spec.ProviderConfigRef.StorageConfig
		name = versionResource.Spec.ProviderConfigRef.Name
	}

	if storageConfig == nil || name == nil || versionResource.Spec.FileName == nil {
		return "", fmt.Errorf("storage configuration not available for version '%s'", versionResource.Name)
	}

	if storageConfig.AzureStorage != nil {
		return fmt.Sprintf("azure/%s/%s/%s/%s/%s/%s",
			storageConfig.AzureStorage.SubscriptionID,
			storageConfig.AzureStorage.ResourceGroup,
			storageConfig.AzureStorage.AccountName,
			url.PathEscape(storageConfig.AzureStorage.AccountUrl),
			*name,
			*versionResource.Spec.FileName,
		), nil
	}

	if storageConfig.FileSystem != nil && storageConfig.FileSystem.DirectoryPath != nil {
		return fmt.Sprintf("fileSystem/%s/%s/%s",
			base64.RawURLEncoding.EncodeToString([]byte(*storageConfig.FileSystem.DirectoryPath)),
			*name,
			*versionResource.Spec.FileName,
		), nil
	}

	if storageConfig.GCS != nil {
		return fmt.Sprintf("gcs/%s/%s/%s",
			storageConfig.GCS.Bucket,
			*name,
			*versionResource.Spec.FileName,
		), nil
	}

	if storageConfig.S3 != nil {
		return fmt.Sprintf("s3/%s/%s/%s",
			storageConfig.S3.Bucket,
			storageConfig.S3.Region,
			*storageConfig.S3.Key,
		), nil
	}

	return "", fmt.Errorf("unsupported storage configuration for version '%s'", versionResource.Name)
}

// getObjectFromStorageSystem validates the object's sha256 checksum and when valid copies from
// the storage system src to the download stream dst provided by http.ResponseWriter.
func getObjectFromStorageSystem(w http.ResponseWriter, r *http.Request, s storage.Storage, soi *storageTypes.StorageObjectInput, checksum string) {
	if err := s.GetObjectChecksum(r.Context(), soi); err != nil {
		logger.Error("failed to get checksum from storage system", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if soi.ObjectChecksum != nil && *soi.ObjectChecksum != checksum {
		logger.Error("checksum mismatch from storage system", "want", checksum, "received", *soi.ObjectChecksum)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	reader, err := s.GetObject(r.Context(), soi)
	if err != nil {
		logger.Error("failed to get module from storage system", "error", err)
		http.Error(w, "failed to get module", http.StatusInternalServerError)
		return
	}

	if strings.HasSuffix(*soi.FilePath, ".zip") {
		w.Header().Set("Content-Type", "application/zip")
	} else {
		w.Header().Set("Content-Type", "application/x-tar")
	}

	if _, err := io.Copy(w, reader); err != nil {
		http.Error(w, fmt.Sprintf("failed to stream file: %v", err), http.StatusInternalServerError)
		return
	}
}

// initStorageBackend creates and initialises a storage backend from the provided StorageConfig.
// It returns an error if the backend cannot be initialised or if no supported backend is configured.
func initStorageBackend(ctx context.Context, storageConfig *opendepotv1alpha1.StorageConfig) (storage.Storage, error) {
	if storageConfig.S3 != nil {
		s3Storage := &storage.AmazonS3Storage{}
		if err := s3Storage.NewClient(ctx, storageConfig.S3.Region); err != nil {
			return nil, fmt.Errorf("failed to init s3 client: %w", err)
		}
		return s3Storage, nil
	}

	if storageConfig.GCS != nil {
		gcsStorage := &storage.GoogleCloudStorage{}
		if err := gcsStorage.NewClient(ctx); err != nil {
			return nil, fmt.Errorf("failed to init gcs client: %w", err)
		}
		return gcsStorage, nil
	}

	if storageConfig.AzureStorage != nil {
		azStorage := &storage.AzureBlobStorage{}
		if err := azStorage.NewClients(storageConfig.AzureStorage.SubscriptionID, storageConfig.AzureStorage.AccountUrl); err != nil {
			return nil, fmt.Errorf("failed to init azure client: %w", err)
		}
		return azStorage, nil
	}

	if storageConfig.FileSystem != nil {
		return &storage.FileSystem{}, nil
	}

	return nil, fmt.Errorf("unsupported storage configuration")
}
