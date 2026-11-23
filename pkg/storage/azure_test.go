package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/cloud"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/storage/armstorage/v3"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"

	versionv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	storagetypes "github.com/tonedefdev/opendepot/pkg/storage/types"
)

const (
	testAzureSubscription    = "00000000-0000-0000-0000-000000000000"
	testAzureResourceGroup   = "rg"
	testAzureAccount         = "acct"
	testAzureContainer       = "module-container"
	testAzureBlob            = "module.zip"
	testAzureBlobNotFoundXML = `<?xml version="1.0" encoding="utf-8"?><Error><Code>BlobNotFound</Code><Message>The specified blob does not exist.</Message></Error>`
	testAzureAccessDenied    = `<?xml version="1.0" encoding="utf-8"?><Error><Code>AuthorizationFailure</Code><Message>denied</Message></Error>`
	testAzureUserDelegation  = `<?xml version="1.0" encoding="utf-8"?><UserDelegationKey><SignedOid>00000000-0000-0000-0000-000000000001</SignedOid><SignedTid>00000000-0000-0000-0000-000000000002</SignedTid><SignedStart>%s</SignedStart><SignedExpiry>%s</SignedExpiry><SignedService>b</SignedService><SignedVersion>2020-02-10</SignedVersion><Value>dGVzdC1rZXk=</Value></UserDelegationKey>`
)

// fakeAzureCredential satisfies azcore.TokenCredential for the fake ARM endpoint.
type fakeAzureCredential struct{}

func (fakeAzureCredential) GetToken(_ context.Context, _ policy.TokenRequestOptions) (azcore.AccessToken, error) {
	return azcore.AccessToken{Token: "test-token", ExpiresOn: time.Now().Add(time.Hour)}, nil
}

// newTestAzureStorage wires an AzureBlobStorage to a fake TLS endpoint that serves both the blob data
// plane and the ARM blob container control plane.
func newTestAzureStorage(t *testing.T, handler http.HandlerFunc) *AzureBlobStorage {
	t.Helper()

	srv := httptest.NewTLSServer(handler)
	t.Cleanup(srv.Close)

	blobClient, err := azblob.NewClientWithNoCredential(srv.URL, &azblob.ClientOptions{
		ClientOptions: policy.ClientOptions{Transport: srv.Client()},
	})
	if err != nil {
		t.Fatalf("creating blob client: %v", err)
	}

	containerClient, err := armstorage.NewBlobContainersClient(testAzureSubscription, fakeAzureCredential{}, &arm.ClientOptions{
		ClientOptions: policy.ClientOptions{
			Transport: srv.Client(),
			Cloud: cloud.Configuration{
				Services: map[cloud.ServiceName]cloud.ServiceConfiguration{
					cloud.ResourceManager: {Endpoint: srv.URL, Audience: srv.URL},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("creating container client: %v", err)
	}

	return &AzureBlobStorage{blobClient: blobClient, storageClient: containerClient}
}

func testAzureInput(blob string) *storagetypes.StorageObjectInput {
	container := testAzureContainer
	filePath := blob
	return &storagetypes.StorageObjectInput{
		ContainerName: &container,
		FilePath:      &filePath,
		StorageConfig: &versionv1alpha1.StorageConfig{
			AzureStorage: &versionv1alpha1.AzureStorageConfig{
				AccountName:    testAzureAccount,
				ResourceGroup:  testAzureResourceGroup,
				SubscriptionID: testAzureSubscription,
			},
		},
	}
}

func isAzureContainerPath(path string) bool {
	return strings.HasSuffix(path, "/containers/"+testAzureContainer)
}

func writeAzureXML(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

func TestAzureBlobStorageNewClientsCreatesClients(t *testing.T) {
	azureStorage := &AzureBlobStorage{}
	if err := azureStorage.NewClients(testAzureSubscription, "https://acct.blob.core.windows.net/"); err != nil {
		t.Fatalf("NewClients returned error: %v", err)
	}

	if azureStorage.blobClient == nil {
		t.Error("expected NewClients to set the blob client")
	}

	if azureStorage.storageClient == nil {
		t.Error("expected NewClients to set the container client")
	}
}

func TestAzureBlobStorageGetObjectReturnsBody(t *testing.T) {
	const payload = "azure-archive"
	azureStorage := newTestAzureStorage(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/"+testAzureContainer+"/"+testAzureBlob {
			writeAzureXML(w, http.StatusNotFound, testAzureBlobNotFoundXML)
			return
		}

		_, _ = io.WriteString(w, payload)
	})

	reader, err := azureStorage.GetObject(context.Background(), testAzureInput(testAzureBlob))
	if err != nil {
		t.Fatalf("GetObject returned error: %v", err)
	}

	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("reading blob: %v", err)
	}

	if string(got) != payload {
		t.Errorf("blob contents = %q, want %q", got, payload)
	}
}

func TestAzureBlobStorageGetObjectNotFoundWrapsErrNotFound(t *testing.T) {
	azureStorage := newTestAzureStorage(t, func(w http.ResponseWriter, _ *http.Request) {
		writeAzureXML(w, http.StatusNotFound, testAzureBlobNotFoundXML)
	})

	_, err := azureStorage.GetObject(context.Background(), testAzureInput(testAzureBlob))
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("GetObject error = %v, want wrapped ErrNotFound", err)
	}
}

func TestAzureBlobStorageGetObjectBackendError(t *testing.T) {
	azureStorage := newTestAzureStorage(t, func(w http.ResponseWriter, _ *http.Request) {
		writeAzureXML(w, http.StatusForbidden, testAzureAccessDenied)
	})

	_, err := azureStorage.GetObject(context.Background(), testAzureInput(testAzureBlob))
	if err == nil {
		t.Fatal("expected error for access denied")
	}

	if errors.Is(err, ErrNotFound) {
		t.Errorf("access denied should not be reported as ErrNotFound: %v", err)
	}
}

func TestAzureBlobStorageGetObjectChecksumFound(t *testing.T) {
	azureStorage := newTestAzureStorage(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || !isAzureContainerPath(r.URL.Path) {
			http.Error(w, "unexpected request", http.StatusTeapot)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"name":"`+testAzureContainer+`","properties":{"metadata":{"Checksum":"azure-checksum"}}}`)
	})

	soi := testAzureInput(testAzureBlob)
	if err := azureStorage.GetObjectChecksum(context.Background(), soi); err != nil {
		t.Fatalf("GetObjectChecksum returned error: %v", err)
	}

	if soi.ObjectChecksum == nil || *soi.ObjectChecksum != "azure-checksum" {
		t.Errorf("ObjectChecksum = %v, want azure-checksum", soi.ObjectChecksum)
	}

	if !soi.FileExists {
		t.Error("expected FileExists to be true")
	}
}

func TestAzureBlobStorageGetObjectChecksumNotFoundWrapsErrNotFound(t *testing.T) {
	azureStorage := newTestAzureStorage(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":{"code":"ContainerNotFound","message":"missing"}}`)
	})

	err := azureStorage.GetObjectChecksum(context.Background(), testAzureInput(testAzureBlob))
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("GetObjectChecksum error = %v, want wrapped ErrNotFound", err)
	}
}

func TestAzureBlobStorageGetObjectChecksumBackendError(t *testing.T) {
	azureStorage := newTestAzureStorage(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"error":{"code":"AuthorizationFailed","message":"denied"}}`)
	})

	err := azureStorage.GetObjectChecksum(context.Background(), testAzureInput(testAzureBlob))
	if err == nil {
		t.Fatal("expected error for access denied")
	}

	if errors.Is(err, ErrNotFound) {
		t.Errorf("access denied should not be reported as ErrNotFound: %v", err)
	}
}

func TestAzureBlobStorageDeleteObject(t *testing.T) {
	var gotMethod, gotPath string
	azureStorage := newTestAzureStorage(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusAccepted)
	})

	if err := azureStorage.DeleteObject(context.Background(), testAzureInput(testAzureBlob)); err != nil {
		t.Fatalf("DeleteObject returned error: %v", err)
	}

	if gotMethod != http.MethodDelete {
		t.Errorf("method = %q, want DELETE", gotMethod)
	}

	if gotPath != "/"+testAzureContainer+"/"+testAzureBlob {
		t.Errorf("path = %q, want /%s/%s", gotPath, testAzureContainer, testAzureBlob)
	}
}

func TestAzureBlobStorageDeleteObjectBackendError(t *testing.T) {
	azureStorage := newTestAzureStorage(t, func(w http.ResponseWriter, _ *http.Request) {
		writeAzureXML(w, http.StatusForbidden, testAzureAccessDenied)
	})

	if err := azureStorage.DeleteObject(context.Background(), testAzureInput(testAzureBlob)); err == nil {
		t.Error("expected error for access denied")
	}
}

func TestAzureBlobStoragePutObjectFromBytes(t *testing.T) {
	const payload = "azure-bytes"
	var blobPuts int
	azureStorage := newTestAzureStorage(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut && isAzureContainerPath(r.URL.Path) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"name":"`+testAzureContainer+`"}`)
			return
		}

		if r.Method == http.MethodPut && r.URL.Path == "/"+testAzureContainer+"/"+testAzureBlob {
			blobPuts++
			w.WriteHeader(http.StatusCreated)
			return
		}

		http.Error(w, "unexpected request", http.StatusTeapot)
	})

	checksum := "bytes-checksum"
	soi := testAzureInput(testAzureBlob)
	soi.ArchiveChecksum = &checksum
	soi.FileBytes = []byte(payload)
	if err := azureStorage.PutObject(context.Background(), soi); err != nil {
		t.Fatalf("PutObject returned error: %v", err)
	}

	if blobPuts == 0 {
		t.Error("expected the blob to be uploaded")
	}

	if soi.BytesWritten != int64(len(payload)) {
		t.Errorf("BytesWritten = %d, want %d", soi.BytesWritten, len(payload))
	}
}

func TestAzureBlobStoragePutObjectFromReader(t *testing.T) {
	const payload = "azure-stream"
	azureStorage := newTestAzureStorage(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut && isAzureContainerPath(r.URL.Path) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"name":"`+testAzureContainer+`"}`)
			return
		}

		_, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusCreated)
	})

	checksum := "stream-checksum"
	soi := testAzureInput(testAzureBlob)
	soi.ArchiveChecksum = &checksum
	soi.FileReader = strings.NewReader(payload)
	if err := azureStorage.PutObject(context.Background(), soi); err != nil {
		t.Fatalf("PutObject returned error: %v", err)
	}

	if soi.BytesWritten != int64(len(payload)) {
		t.Errorf("BytesWritten = %d, want %d", soi.BytesWritten, len(payload))
	}
}

func TestAzureBlobStoragePutObjectContainerError(t *testing.T) {
	azureStorage := newTestAzureStorage(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"error":{"code":"AuthorizationFailed","message":"denied"}}`)
	})

	checksum := "bytes-checksum"
	soi := testAzureInput(testAzureBlob)
	soi.ArchiveChecksum = &checksum
	soi.FileBytes = []byte("azure-bytes")
	if err := azureStorage.PutObject(context.Background(), soi); err == nil {
		t.Error("expected error when the container cannot be created")
	}
}

func TestAzureBlobStoragePutObjectUploadErrorResetsBytesWritten(t *testing.T) {
	azureStorage := newTestAzureStorage(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut && isAzureContainerPath(r.URL.Path) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"name":"`+testAzureContainer+`"}`)
			return
		}

		writeAzureXML(w, http.StatusForbidden, testAzureAccessDenied)
	})

	checksum := "bytes-checksum"
	soi := testAzureInput(testAzureBlob)
	soi.ArchiveChecksum = &checksum
	soi.FileBytes = []byte("azure-bytes")
	if err := azureStorage.PutObject(context.Background(), soi); err == nil {
		t.Fatal("expected error when the blob upload is denied")
	}

	if soi.BytesWritten != 0 {
		t.Errorf("BytesWritten = %d, want 0 after failed upload", soi.BytesWritten)
	}
}

func TestAzureBlobStoragePresignObject(t *testing.T) {
	now := time.Now().UTC()
	udk := fmt.Sprintf(testAzureUserDelegation,
		now.Add(-time.Minute).Format(time.RFC3339),
		now.Add(time.Hour).Format(time.RFC3339),
	)

	azureStorage := newTestAzureStorage(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Query().Get("comp") == "userdelegationkey" {
			writeAzureXML(w, http.StatusOK, udk)
			return
		}

		http.Error(w, "unexpected request", http.StatusTeapot)
	})

	soi := testAzureInput(testAzureBlob)
	if err := azureStorage.PresignObject(context.Background(), soi); err != nil {
		t.Fatalf("PresignObject returned error: %v", err)
	}

	if soi.PresignedURL == nil {
		t.Fatal("expected PresignedURL to be set")
	}

	wantPrefix := "/" + testAzureContainer + "/" + testAzureBlob + "?"
	if !strings.Contains(*soi.PresignedURL, wantPrefix) {
		t.Errorf("PresignedURL %q does not reference the blob", *soi.PresignedURL)
	}

	if !strings.Contains(*soi.PresignedURL, "sp=r") || !strings.Contains(*soi.PresignedURL, "sig=") {
		t.Errorf("PresignedURL %q is missing read permission or signature", *soi.PresignedURL)
	}

	if !strings.Contains(*soi.PresignedURL, "sv=") {
		t.Errorf("PresignedURL %q is missing the signed version", *soi.PresignedURL)
	}

}

func TestAzureBlobStoragePresignObjectDelegationError(t *testing.T) {
	azureStorage := newTestAzureStorage(t, func(w http.ResponseWriter, _ *http.Request) {
		writeAzureXML(w, http.StatusForbidden, testAzureAccessDenied)
	})

	soi := testAzureInput(testAzureBlob)
	if err := azureStorage.PresignObject(context.Background(), soi); err == nil {
		t.Error("expected error when the user delegation key cannot be obtained")
	}

	if soi.PresignedURL != nil {
		t.Errorf("PresignedURL = %q, want nil", *soi.PresignedURL)
	}
}
