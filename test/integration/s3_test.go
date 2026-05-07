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

func TestS3StorageIntegration(t *testing.T) {
	region := os.Getenv("AWS_REGION")
	if region == "" {
		t.Skip("AWS_REGION must be set to run S3 integration tests")
	}

	// Unique suffix per test run — prevents bucket name collisions across parallel CI runs.
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())

	tfOptions := &terraform.Options{
		TerraformDir:    "../infra/s3",
		TerraformBinary: "tofu",
		Vars: map[string]interface{}{
			"bucket_suffix": suffix,
			"region":        region,
		},
		NoColor: true,
	}

	// Destroy is always deferred — runs even if InitAndApply or any assertion fails.
	defer terraform.Destroy(t, tfOptions)
	terraform.InitAndApply(t, tfOptions)

	bucketName := terraform.Output(t, tfOptions, "bucket_name")

	ctx := context.Background()

	var s storage.AmazonS3Storage
	require.NoError(t, s.NewClient(ctx, region))

	// Small deterministic payload — simulates a null provider zip.
	testContent := []byte("opendepot-s3-integration-test-payload")
	sum := sha256.Sum256(testContent)
	checksum := base64.StdEncoding.EncodeToString(sum[:])

	// Unique key within the bucket.
	key := fmt.Sprintf("opendepot-integration-tests/%d/null_3.0.0_linux_amd64.zip", time.Now().UnixNano())

	storageConfig := &versionv1alpha1.StorageConfig{
		S3: &versionv1alpha1.AmazonS3Config{
			Bucket: bucketName,
			Region: region,
		},
	}

	putSOI := &storagetypes.StorageObjectInput{
		FilePath:        &key,
		StorageConfig:   storageConfig,
		ArchiveChecksum: &checksum,
		FileBytes:       testContent,
	}

	// Belt-and-suspenders cleanup of the test object inside the bucket.
	// tofu destroy with force_destroy=true handles the bucket itself, but
	// explicit DeleteObject keeps the test self-contained.
	t.Cleanup(func() {
		_ = s.DeleteObject(ctx, &storagetypes.StorageObjectInput{
			FilePath:      &key,
			StorageConfig: storageConfig,
		})
	})

	t.Run("PutObject", func(t *testing.T) {
		require.NoError(t, s.PutObject(ctx, putSOI))
	})

	t.Run("GetObjectChecksum", func(t *testing.T) {
		soi := &storagetypes.StorageObjectInput{
			FilePath:      &key,
			StorageConfig: storageConfig,
		}

		require.NoError(t, s.GetObjectChecksum(ctx, soi))
		assert.True(t, soi.FileExists)
		require.NotNil(t, soi.ObjectChecksum)
		assert.NotEmpty(t, *soi.ObjectChecksum)
	})

	t.Run("GetObject", func(t *testing.T) {
		soi := &storagetypes.StorageObjectInput{
			FilePath:      &key,
			StorageConfig: storageConfig,
		}

		reader, err := s.GetObject(ctx, soi)
		require.NoError(t, err)
		got, err := io.ReadAll(reader)
		require.NoError(t, err)
		assert.Equal(t, testContent, got)
	})

	t.Run("PresignObject", func(t *testing.T) {
		soi := &storagetypes.StorageObjectInput{
			FilePath:      &key,
			StorageConfig: storageConfig,
			PresignTTL:    5 * time.Minute,
		}

		require.NoError(t, s.PresignObject(ctx, soi))
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
			FilePath:      &key,
			StorageConfig: storageConfig,
		}

		require.NoError(t, s.DeleteObject(ctx, soi))
		confirmSOI := &storagetypes.StorageObjectInput{
			FilePath:      &key,
			StorageConfig: storageConfig,
		}

		_ = s.GetObjectChecksum(ctx, confirmSOI)
		assert.False(t, confirmSOI.FileExists)
	})
}
