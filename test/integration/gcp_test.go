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
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gruntwork-io/terratest/modules/terraform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	versionv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	storage "github.com/tonedefdev/opendepot/pkg/storage"
	storagetypes "github.com/tonedefdev/opendepot/pkg/storage/types"
)

// credsSupportSigning reports whether the active Application Default Credentials
// are capable of signing URLs. Signed URLs require a service account, an
// external_account (Workload Identity Federation), or an impersonated service
// account credential. Plain user OAuth2 credentials (authorized_user) cannot
// sign and will always fail PresignObject.
func credsSupportSigning() bool {
	path := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS")
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return false
		}
		path = filepath.Join(home, ".config", "gcloud", "application_default_credentials.json")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}

	var creds struct {
		Type string `json:"type"`
	}

	if err := json.Unmarshal(data, &creds); err != nil {
		return false
	}

	switch creds.Type {
	case "service_account", "external_account", "impersonated_service_account":
		return true
	}

	return false
}

func TestGCSStorageIntegration(t *testing.T) {
	project := os.Getenv("GCP_PROJECT")
	if project == "" {
		t.Skip("GCP_PROJECT must be set to run GCS integration tests")
	}

	location := os.Getenv("GCP_LOCATION")
	if location == "" {
		location = "US"
	}

	// Unique suffix per test run — prevents bucket name collisions across parallel CI runs.
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())

	tfOptions := &terraform.Options{
		TerraformDir:    "../infra/gcp",
		TerraformBinary: "tofu",
		Vars: map[string]interface{}{
			"bucket_suffix": suffix,
			"location":      location,
			"project":       project,
		},
		NoColor: true,
	}

	// Destroy is always deferred — runs even if InitAndApply or any assertion fails.
	defer terraform.Destroy(t, tfOptions)
	terraform.InitAndApply(t, tfOptions)

	bucketName := terraform.Output(t, tfOptions, "bucket_name")

	ctx := context.Background()

	var gcs storage.GoogleCloudStorage
	require.NoError(t, gcs.NewClient(ctx))

	// Small deterministic payload — simulates a null provider zip.
	testContent := []byte("opendepot-gcs-integration-test-payload")
	sum := sha256.Sum256(testContent)
	checksum := base64.StdEncoding.EncodeToString(sum[:])

	// Unique key within the bucket.
	key := fmt.Sprintf("opendepot-integration-tests/%d/null_3.0.0_linux_amd64.zip", time.Now().UnixNano())

	storageConfig := &versionv1alpha1.StorageConfig{
		GCS: &versionv1alpha1.GoogleCloudStorageConfig{
			Bucket: bucketName,
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
		_ = gcs.DeleteObject(ctx, &storagetypes.StorageObjectInput{
			FilePath:      &key,
			StorageConfig: storageConfig,
		})
	})

	t.Run("PutObject", func(t *testing.T) {
		require.NoError(t, gcs.PutObject(ctx, putSOI))
	})

	t.Run("GetObjectChecksum", func(t *testing.T) {
		soi := &storagetypes.StorageObjectInput{
			FilePath:      &key,
			StorageConfig: storageConfig,
		}

		require.NoError(t, gcs.GetObjectChecksum(ctx, soi))
		assert.True(t, soi.FileExists)
		require.NotNil(t, soi.ObjectChecksum)
		assert.NotEmpty(t, *soi.ObjectChecksum)
	})

	t.Run("GetObject", func(t *testing.T) {
		soi := &storagetypes.StorageObjectInput{
			FilePath:      &key,
			StorageConfig: storageConfig,
		}

		reader, err := gcs.GetObject(ctx, soi)
		require.NoError(t, err)
		got, err := io.ReadAll(reader)
		require.NoError(t, err)
		assert.Equal(t, testContent, got)
	})

	t.Run("PresignObject", func(t *testing.T) {
		if !credsSupportSigning() {
			t.Skip("PresignObject requires service account, external_account, or impersonated credentials; " +
				"re-run with a service account key or: gcloud auth application-default login --impersonate-service-account=SA@PROJECT.iam.gserviceaccount.com")
		}
		soi := &storagetypes.StorageObjectInput{
			FilePath:      &key,
			StorageConfig: storageConfig,
			PresignTTL:    5 * time.Minute,
		}

		require.NoError(t, gcs.PresignObject(ctx, soi))
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

		require.NoError(t, gcs.DeleteObject(ctx, soi))
		confirmSOI := &storagetypes.StorageObjectInput{
			FilePath:      &key,
			StorageConfig: storageConfig,
		}

		// GCS DeleteObject treats 404 as success, so an error here means the
		// object genuinely no longer exists. FileExists is the authoritative signal.
		_ = gcs.GetObjectChecksum(ctx, confirmSOI)
		assert.False(t, confirmSOI.FileExists)
	})
}
