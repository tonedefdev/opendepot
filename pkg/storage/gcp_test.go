package storage

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	versionv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	storagetypes "github.com/tonedefdev/opendepot/pkg/storage/types"
)

const (
	testGCSBucket = "bucket"
	testGCSObject = "module.zip"
)

// newTestGCSStorage builds a GoogleCloudStorage through the production NewClient path. The
// STORAGE_EMULATOR_HOST variable makes the Google client talk to the fake server without credentials.
func newTestGCSStorage(t *testing.T, handler http.HandlerFunc) *GoogleCloudStorage {
	t.Helper()

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	t.Setenv("STORAGE_EMULATOR_HOST", strings.TrimPrefix(srv.URL, "http://"))

	gcsStorage := &GoogleCloudStorage{}
	if err := gcsStorage.NewClient(context.Background()); err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}

	return gcsStorage
}

func testGCSInput(object string) *storagetypes.StorageObjectInput {
	filePath := object
	return &storagetypes.StorageObjectInput{
		FilePath: &filePath,
		StorageConfig: &versionv1alpha1.StorageConfig{
			GCS: &versionv1alpha1.GoogleCloudStorageConfig{Bucket: testGCSBucket},
		},
	}
}

func writeGCSNotFound(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	_, _ = io.WriteString(w, `{"error":{"code":404,"message":"No such object"}}`)
}

func writeGCSForbidden(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	_, _ = io.WriteString(w, `{"error":{"code":403,"message":"Access denied"}}`)
}

func TestGoogleCloudStorageNewClientUsesEmulator(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("STORAGE_EMULATOR_HOST", strings.TrimPrefix(srv.URL, "http://"))

	gcsStorage := &GoogleCloudStorage{}
	if err := gcsStorage.NewClient(context.Background()); err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}

	if gcsStorage.client == nil {
		t.Error("expected NewClient to set the storage client")
	}
}

func TestGoogleCloudStorageGetObjectChecksumFound(t *testing.T) {
	gcsStorage := newTestGCSStorage(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/b/"+testGCSBucket+"/o/"+testGCSObject) {
			writeGCSNotFound(w)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"name":"`+testGCSObject+`","metadata":{"sha256-checksum":"abc123"}}`)
	})

	soi := testGCSInput(testGCSObject)
	if err := gcsStorage.GetObjectChecksum(context.Background(), soi); err != nil {
		t.Fatalf("GetObjectChecksum returned error: %v", err)
	}

	if soi.ObjectChecksum == nil || *soi.ObjectChecksum != "abc123" {
		t.Errorf("ObjectChecksum = %v, want abc123", soi.ObjectChecksum)
	}

	if !soi.FileExists {
		t.Error("expected FileExists to be true")
	}
}

func TestGoogleCloudStorageGetObjectChecksumWithoutMetadata(t *testing.T) {
	gcsStorage := newTestGCSStorage(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"name":"`+testGCSObject+`"}`)
	})

	soi := testGCSInput(testGCSObject)
	if err := gcsStorage.GetObjectChecksum(context.Background(), soi); err != nil {
		t.Fatalf("GetObjectChecksum returned error: %v", err)
	}

	if soi.ObjectChecksum != nil {
		t.Errorf("ObjectChecksum = %q, want nil", *soi.ObjectChecksum)
	}

	if !soi.FileExists {
		t.Error("expected FileExists to be true when the object exists")
	}
}

func TestGoogleCloudStorageGetObjectChecksumNotFoundWrapsErrNotFound(t *testing.T) {
	gcsStorage := newTestGCSStorage(t, func(w http.ResponseWriter, _ *http.Request) {
		writeGCSNotFound(w)
	})

	err := gcsStorage.GetObjectChecksum(context.Background(), testGCSInput(testGCSObject))
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("GetObjectChecksum error = %v, want wrapped ErrNotFound", err)
	}
}

func TestGoogleCloudStorageGetObjectChecksumBackendError(t *testing.T) {
	gcsStorage := newTestGCSStorage(t, func(w http.ResponseWriter, _ *http.Request) {
		writeGCSForbidden(w)
	})

	err := gcsStorage.GetObjectChecksum(context.Background(), testGCSInput(testGCSObject))
	if err == nil {
		t.Fatal("expected error for access denied")
	}

	if errors.Is(err, ErrNotFound) {
		t.Errorf("access denied should not be reported as ErrNotFound: %v", err)
	}
}

func TestGoogleCloudStorageGetObjectStreamsMedia(t *testing.T) {
	const payload = "gcs-archive"
	gcsStorage := newTestGCSStorage(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/"+testGCSBucket+"/"+testGCSObject {
			writeGCSNotFound(w)
			return
		}

		_, _ = io.WriteString(w, payload)
	})

	reader, err := gcsStorage.GetObject(context.Background(), testGCSInput(testGCSObject))
	if err != nil {
		t.Fatalf("GetObject returned error: %v", err)
	}

	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("reading object: %v", err)
	}

	if string(got) != payload {
		t.Errorf("object contents = %q, want %q", got, payload)
	}
}

func TestGoogleCloudStorageGetObjectNotFoundWrapsErrNotFound(t *testing.T) {
	gcsStorage := newTestGCSStorage(t, func(w http.ResponseWriter, _ *http.Request) {
		writeGCSNotFound(w)
	})

	_, err := gcsStorage.GetObject(context.Background(), testGCSInput(testGCSObject))
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("GetObject error = %v, want wrapped ErrNotFound", err)
	}
}

func TestGoogleCloudStorageGetObjectBackendError(t *testing.T) {
	gcsStorage := newTestGCSStorage(t, func(w http.ResponseWriter, _ *http.Request) {
		writeGCSForbidden(w)
	})

	_, err := gcsStorage.GetObject(context.Background(), testGCSInput(testGCSObject))
	if err == nil {
		t.Fatal("expected error for access denied")
	}

	if errors.Is(err, ErrNotFound) {
		t.Errorf("access denied should not be reported as ErrNotFound: %v", err)
	}
}

func TestGoogleCloudStorageDeleteObject(t *testing.T) {
	var gotMethod string
	gcsStorage := newTestGCSStorage(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		w.WriteHeader(http.StatusNoContent)
	})

	if err := gcsStorage.DeleteObject(context.Background(), testGCSInput(testGCSObject)); err != nil {
		t.Fatalf("DeleteObject returned error: %v", err)
	}

	if gotMethod != http.MethodDelete {
		t.Errorf("method = %q, want DELETE", gotMethod)
	}
}

func TestGoogleCloudStorageDeleteObjectMissingIsNoop(t *testing.T) {
	gcsStorage := newTestGCSStorage(t, func(w http.ResponseWriter, _ *http.Request) {
		writeGCSNotFound(w)
	})

	if err := gcsStorage.DeleteObject(context.Background(), testGCSInput(testGCSObject)); err != nil {
		t.Errorf("DeleteObject on missing object returned error: %v", err)
	}
}

func TestGoogleCloudStorageDeleteObjectBackendError(t *testing.T) {
	gcsStorage := newTestGCSStorage(t, func(w http.ResponseWriter, _ *http.Request) {
		writeGCSForbidden(w)
	})

	if err := gcsStorage.DeleteObject(context.Background(), testGCSInput(testGCSObject)); err == nil {
		t.Error("expected error for access denied")
	}
}

func TestGoogleCloudStoragePutObjectSetsChecksumMetadata(t *testing.T) {
	tests := []struct {
		name        string
		object      string
		contentType string
	}{
		{name: "zip archive", object: "module.zip", contentType: "application/zip"},
		{name: "tar archive", object: "module.tar", contentType: "application/x-tar"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotBody string
			gcsStorage := newTestGCSStorage(t, func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				gotBody = string(body)
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"name":"`+tt.object+`"}`)
			})

			checksum := "checksum-value"
			soi := testGCSInput(tt.object)
			soi.ArchiveChecksum = &checksum
			soi.FileBytes = []byte("payload-bytes")
			if err := gcsStorage.PutObject(context.Background(), soi); err != nil {
				t.Fatalf("PutObject returned error: %v", err)
			}

			if !strings.Contains(gotBody, `"sha256-checksum":"checksum-value"`) {
				t.Errorf("upload is missing the sha256-checksum metadata")
			}

			if !strings.Contains(gotBody, tt.contentType) {
				t.Errorf("upload is missing content type %q", tt.contentType)
			}

			if soi.BytesWritten != int64(len("payload-bytes")) {
				t.Errorf("BytesWritten = %d, want %d", soi.BytesWritten, len("payload-bytes"))
			}
		})
	}
}

func TestGoogleCloudStoragePutObjectFromReader(t *testing.T) {
	gcsStorage := newTestGCSStorage(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"name":"`+testGCSObject+`"}`)
	})

	checksum := "checksum-value"
	soi := testGCSInput(testGCSObject)
	soi.ArchiveChecksum = &checksum
	soi.FileReader = strings.NewReader("streamed-payload")
	if err := gcsStorage.PutObject(context.Background(), soi); err != nil {
		t.Fatalf("PutObject returned error: %v", err)
	}

	if soi.BytesWritten != int64(len("streamed-payload")) {
		t.Errorf("BytesWritten = %d, want %d", soi.BytesWritten, len("streamed-payload"))
	}
}

func TestGoogleCloudStoragePutObjectBackendErrorResetsBytesWritten(t *testing.T) {
	gcsStorage := newTestGCSStorage(t, func(w http.ResponseWriter, _ *http.Request) {
		writeGCSForbidden(w)
	})

	checksum := "checksum-value"
	soi := testGCSInput(testGCSObject)
	soi.ArchiveChecksum = &checksum
	soi.FileBytes = []byte("payload-bytes")
	if err := gcsStorage.PutObject(context.Background(), soi); err == nil {
		t.Fatal("expected error for access denied")
	}

	if soi.BytesWritten != 0 {
		t.Errorf("BytesWritten = %d, want 0 after failed upload", soi.BytesWritten)
	}
}

func TestGoogleCloudStoragePresignObjectWithoutSigningCredentials(t *testing.T) {
	gcsStorage := newTestGCSStorage(t, func(w http.ResponseWriter, _ *http.Request) {
		writeGCSNotFound(w)
	})

	soi := testGCSInput(testGCSObject)
	if err := gcsStorage.PresignObject(context.Background(), soi); err == nil {
		t.Error("expected error when no signing credentials are configured")
	}
}
