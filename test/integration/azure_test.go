//go:build integration

package integration

/*
Copyright 2026 Tony Owens.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/gruntwork-io/terratest/modules/terraform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	versionv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	storage "github.com/tonedefdev/opendepot/pkg/storage"
	storagetypes "github.com/tonedefdev/opendepot/pkg/storage/types"
)

func TestAzureBlobStorageIntegration(t *testing.T) {
	subscriptionID := os.Getenv("AZURE_SUBSCRIPTION_ID")
	if subscriptionID == "" {
		t.Skip("AZURE_SUBSCRIPTION_ID must be set to run Azure Blob Storage integration tests")
	}

	location := os.Getenv("AZURE_LOCATION")
	if location == "" {
		location = "West US 2"
	}

	// Unique suffix per test run — prevents resource name collisions across parallel CI runs.
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())

	tfOptions := &terraform.Options{
		TerraformDir:    "../infra/azure",
		TerraformBinary: "tofu",
		Vars: map[string]interface{}{
			"suffix":          suffix,
			"location":        location,
			"subscription_id": subscriptionID,
		},
		NoColor: true,
	}

	// Destroy is always deferred — runs even if InitAndApply or any assertion fails.
	defer terraform.Destroy(t, tfOptions)
	terraform.InitAndApply(t, tfOptions)

	accountName := terraform.Output(t, tfOptions, "account_name")
	accountURL := terraform.Output(t, tfOptions, "account_url")
	resourceGroupName := terraform.Output(t, tfOptions, "resource_group_name")

	ctx := context.Background()

	var az storage.AzureBlobStorage
	require.NoError(t, az.NewClients(subscriptionID, accountURL))

	// Small deterministic payload — simulates a null provider zip.
	testContent := []byte("opendepot-azure-integration-test-payload")
	sum := sha256.Sum256(testContent)
	checksum := base64.StdEncoding.EncodeToString(sum[:])

	// Unique container name per run — 3–63 chars, lowercase alphanumeric and hyphens.
	containerName := fmt.Sprintf("opendepot-intg-%d", time.Now().UnixNano())

	// Unique key within the container.
	key := fmt.Sprintf("opendepot-integration-tests/%d/null_3.0.0_linux_amd64.zip", time.Now().UnixNano())

	storageConfig := &versionv1alpha1.StorageConfig{
		AzureStorage: &versionv1alpha1.AzureStorageConfig{
			AccountName:    accountName,
			AccountUrl:     accountURL,
			SubscriptionID: subscriptionID,
			ResourceGroup:  resourceGroupName,
		},
	}

	putSOI := &storagetypes.StorageObjectInput{
		ContainerName:   &containerName,
		FilePath:        &key,
		StorageConfig:   storageConfig,
		ArchiveChecksum: &checksum,
		FileBytes:       testContent,
	}

	// Belt-and-suspenders cleanup of the test blob. The storage account and resource
	// group are destroyed by tofu destroy; this keeps the test self-contained if a
	// sub-test failure leaves the blob behind.
	t.Cleanup(func() {
		_ = az.DeleteObject(ctx, &storagetypes.StorageObjectInput{
			ContainerName: &containerName,
			FilePath:      &key,
			StorageConfig: storageConfig,
		})
	})

	t.Run("PutObject", func(t *testing.T) {
		require.NoError(t, az.PutObject(ctx, putSOI))
	})

	t.Run("GetObjectChecksum", func(t *testing.T) {
		soi := &storagetypes.StorageObjectInput{
			ContainerName: &containerName,
			FilePath:      &key,
			StorageConfig: storageConfig,
		}

		require.NoError(t, az.GetObjectChecksum(ctx, soi))
		assert.True(t, soi.FileExists)
		require.NotNil(t, soi.ObjectChecksum)
		assert.NotEmpty(t, *soi.ObjectChecksum)
	})

	t.Run("GetObject", func(t *testing.T) {
		soi := &storagetypes.StorageObjectInput{
			ContainerName: &containerName,
			FilePath:      &key,
			StorageConfig: storageConfig,
		}

		reader, err := az.GetObject(ctx, soi)
		require.NoError(t, err)
		got, err := io.ReadAll(reader)
		require.NoError(t, err)
		assert.Equal(t, testContent, got)
	})

	t.Run("PresignObject", func(t *testing.T) {
		soi := &storagetypes.StorageObjectInput{
			ContainerName: &containerName,
			FilePath:      &key,
			StorageConfig: storageConfig,
			PresignTTL:    5 * time.Minute,
		}

		require.NoError(t, az.PresignObject(ctx, soi))
		require.NotNil(t, soi.PresignedURL)
		require.NotEmpty(t, *soi.PresignedURL)

		// Actually fetch the URL to verify the signing chain works end-to-end.
		resp, err := http.Get(*soi.PresignedURL)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("DeleteObject", func(t *testing.T) {
		soi := &storagetypes.StorageObjectInput{
			ContainerName: &containerName,
			FilePath:      &key,
			StorageConfig: storageConfig,
		}

		require.NoError(t, az.DeleteObject(ctx, soi))

		// Confirm the blob is gone. Azure's GetObjectChecksum checks container
		// existence (not blob existence), so use GetObject to verify the blob was
		// actually removed — a 404 error from DownloadStream confirms deletion.
		_, confirmErr := az.GetObject(ctx, &storagetypes.StorageObjectInput{
			ContainerName: &containerName,
			FilePath:      &key,
			StorageConfig: storageConfig,
		})
		assert.Error(t, confirmErr)
	})
}
