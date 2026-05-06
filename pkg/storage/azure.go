package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	storagetypes "github.com/tonedefdev/opendepot/pkg/storage/types"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/storage/armstorage/v3"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/sas"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/service"
)

type AzureBlobStorage struct {
	blobClient    *azblob.Client
	storageClient *armstorage.BlobContainersClient
}

// NewClients creates a new azblob.Client and armstorage.BlobContainersClient to interact with the
// Azure storage systems.
func (storage *AzureBlobStorage) NewClients(subscriptionID string, storageAccountUrl string) error {
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return err
	}

	storageFactory, err := armstorage.NewClientFactory(subscriptionID, cred, nil)
	if err != nil {
		return err
	}

	blobClient, err := azblob.NewClient(storageAccountUrl, cred, nil)
	if err != nil {
		return err
	}

	storage.blobClient = blobClient
	storage.storageClient = storageFactory.NewBlobContainersClient()
	return nil
}

// GetObject retrieves the object from the Azure Blob and returns an io.Reader to stream the file from the server
func (storage *AzureBlobStorage) GetObject(ctx context.Context, soi *storagetypes.StorageObjectInput) (io.Reader, error) {
	blob, err := storage.blobClient.DownloadStream(ctx,
		*soi.ContainerName,
		*soi.FilePath,
		&azblob.DownloadStreamOptions{},
	)
	if err != nil {
		return nil, err
	}

	return blob.Body, err
}

// GetObjectChecksum retrieves the sha256 checksum from the container's metadata and sets it on the soi receiver's field `ObjectChecksum`.
// If the container can be found the function sets the soi receiver's field for `FileExists` to `true`.
func (storage *AzureBlobStorage) GetObjectChecksum(ctx context.Context, soi *storagetypes.StorageObjectInput) error {
	ctr, err := storage.storageClient.Get(ctx,
		soi.StorageConfig.AzureStorage.ResourceGroup,
		soi.StorageConfig.AzureStorage.AccountName,
		*soi.ContainerName,
		nil,
	)
	if err != nil {
		var respErr *azcore.ResponseError
		if errors.As(err, &respErr) {
			if respErr.StatusCode == http.StatusNotFound {
				return err
			}
		}
	}

	soi.ObjectChecksum = ctr.ContainerProperties.Metadata["Checksum"]
	soi.FileExists = true
	return nil
}

// DeleteObject deletes the Version file from the specified container.
func (storage *AzureBlobStorage) DeleteObject(ctx context.Context, soi *storagetypes.StorageObjectInput) error {
	_, err := storage.blobClient.DeleteBlob(ctx,
		*soi.ContainerName,
		*soi.FilePath,
		&azblob.DeleteBlobOptions{},
	)
	return err
}

// PresignObject generates a time-limited SAS URL for the Azure blob and sets it on soi.PresignedURL.
func (storage *AzureBlobStorage) PresignObject(ctx context.Context, soi *storagetypes.StorageObjectInput) error {
	ttl := soi.PresignTTL
	if ttl == 0 {
		ttl = 15 * time.Minute
	}

	containerName := *soi.ContainerName
	now := time.Now().UTC()
	expiry := now.Add(ttl)

	svcClient := storage.blobClient.ServiceClient()
	startStr := now.Format(sas.TimeFormat)
	expiryStr := expiry.Format(sas.TimeFormat)
	udKey, err := svcClient.GetUserDelegationCredential(ctx, service.KeyInfo{
		Start:  &startStr,
		Expiry: &expiryStr,
	}, nil)
	if err != nil {
		return fmt.Errorf("azure: failed to get user delegation credential: %w", err)
	}

	perms := sas.BlobPermissions{Read: true}
	sasValues := sas.BlobSignatureValues{
		Protocol:      sas.ProtocolHTTPS,
		StartTime:     now,
		ExpiryTime:    expiry,
		Permissions:   perms.String(),
		ContainerName: containerName,
		BlobName:      *soi.FilePath,
	}

	queryParams, err := sasValues.SignWithUserDelegation(udKey)
	if err != nil {
		return fmt.Errorf("azure: failed to sign SAS values: %w", err)
	}

	blobURL := storage.blobClient.ServiceClient().NewContainerClient(containerName).NewBlobClient(*soi.FilePath).URL()
	sasURL := blobURL + "?" + queryParams.Encode()
	soi.PresignedURL = &sasURL
	return nil
}

// PutObject puts the Version file in the specified bucket with its computed base64 encoded SHA256 checksum.
func (storage *AzureBlobStorage) PutObject(ctx context.Context, soi *storagetypes.StorageObjectInput) error {
	ctr, err := storage.storageClient.Create(ctx,
		soi.StorageConfig.AzureStorage.ResourceGroup,
		soi.StorageConfig.AzureStorage.AccountName,
		*soi.ContainerName,
		armstorage.BlobContainer{
			ContainerProperties: &armstorage.ContainerProperties{
				Metadata: map[string]*string{
					"Checksum": soi.ArchiveChecksum,
				},
			},
		}, nil)

	if err != nil {
		return err
	}

	if soi.FileReader != nil {
		streamOptions := &azblob.UploadStreamOptions{
			Concurrency: 10,
		}
		_, err = storage.blobClient.UploadStream(ctx, *ctr.Name, *soi.FilePath, soi.FileReader, streamOptions)
	} else {
		bufferOptions := &azblob.UploadBufferOptions{
			Concurrency: 10,
		}
		_, err = storage.blobClient.UploadBuffer(ctx, *ctr.Name, *soi.FilePath, soi.FileBytes, bufferOptions)
	}

	if err != nil {
		return err
	}

	return nil
}
