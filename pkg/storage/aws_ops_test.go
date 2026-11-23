package storage

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	versionv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	storagetypes "github.com/tonedefdev/opendepot/pkg/storage/types"
)

const (
	testS3Bucket          = "bucket"
	testS3Key             = "module.zip"
	testS3NoSuchKeyXML    = `<?xml version="1.0" encoding="UTF-8"?><Error><Code>NoSuchKey</Code><Message>The specified key does not exist.</Message></Error>`
	testS3AccessDeniedXML = `<?xml version="1.0" encoding="UTF-8"?><Error><Code>AccessDenied</Code><Message>Access Denied</Message></Error>`
)

// newTestS3Storage builds an AmazonS3Storage through the production NewClient path, pointed at a fake
// S3 endpoint served by handler. AWS_ENDPOINT_URL redirects the SDK and OPENDEPOT_S3_USE_PATH_STYLE
// keeps bucket addressing path-based so it resolves against the local server.
func newTestS3Storage(t *testing.T, handler http.HandlerFunc) *AmazonS3Storage {
	t.Helper()

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	t.Setenv("AWS_ENDPOINT_URL", srv.URL)
	t.Setenv("AWS_ACCESS_KEY_ID", "test-access-key")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test-secret-key")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv(s3UsePathStyleEnvVar, "true")

	s3Storage := &AmazonS3Storage{}
	if err := s3Storage.NewClient(context.Background(), "us-east-1"); err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}

	return s3Storage
}

func testS3Input(key string) *storagetypes.StorageObjectInput {
	filePath := key
	return &storagetypes.StorageObjectInput{
		FilePath: &filePath,
		StorageConfig: &versionv1alpha1.StorageConfig{
			S3: &versionv1alpha1.AmazonS3Config{Bucket: testS3Bucket},
		},
	}
}

func TestAmazonS3GetObjectReturnsBody(t *testing.T) {
	const payload = "archive-bytes"
	sum := sha256.Sum256([]byte(payload))

	s3Storage := newTestS3Storage(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/"+testS3Bucket+"/"+testS3Key {
			http.Error(w, "unexpected request", http.StatusTeapot)
			return
		}

		w.Header().Set("x-amz-checksum-sha256", base64.StdEncoding.EncodeToString(sum[:]))
		_, _ = io.WriteString(w, payload)
	})

	reader, err := s3Storage.GetObject(context.Background(), testS3Input(testS3Key))
	if err != nil {
		t.Fatalf("GetObject returned error: %v", err)
	}

	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("reading object body: %v", err)
	}

	if string(got) != payload {
		t.Errorf("object body = %q, want %q", got, payload)
	}
}

func TestAmazonS3GetObjectNotFoundWrapsErrNotFound(t *testing.T) {
	s3Storage := newTestS3Storage(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, testS3NoSuchKeyXML)
	})

	_, err := s3Storage.GetObject(context.Background(), testS3Input(testS3Key))
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("GetObject error = %v, want wrapped ErrNotFound", err)
	}
}

func TestAmazonS3GetObjectBackendErrorIsNotNotFound(t *testing.T) {
	s3Storage := newTestS3Storage(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, testS3AccessDeniedXML)
	})

	_, err := s3Storage.GetObject(context.Background(), testS3Input(testS3Key))
	if err == nil {
		t.Fatal("expected error for access denied")
	}

	if errors.Is(err, ErrNotFound) {
		t.Errorf("access denied should not be reported as ErrNotFound: %v", err)
	}
}

func TestAmazonS3GetObjectChecksumFound(t *testing.T) {
	const payload = "checksum-body"
	sum := sha256.Sum256([]byte(payload))
	want := base64.StdEncoding.EncodeToString(sum[:])

	s3Storage := newTestS3Storage(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("x-amz-checksum-sha256", want)
		_, _ = io.WriteString(w, payload)
	})

	soi := testS3Input(testS3Key)
	if err := s3Storage.GetObjectChecksum(context.Background(), soi); err != nil {
		t.Fatalf("GetObjectChecksum returned error: %v", err)
	}

	if soi.ObjectChecksum == nil || *soi.ObjectChecksum != want {
		t.Errorf("ObjectChecksum = %v, want %q", soi.ObjectChecksum, want)
	}

	if !soi.FileExists {
		t.Error("expected FileExists to be true")
	}
}

func TestAmazonS3GetObjectChecksumWithoutHeaderLeavesFileMissing(t *testing.T) {
	s3Storage := newTestS3Storage(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "no-checksum")
	})

	soi := testS3Input(testS3Key)
	if err := s3Storage.GetObjectChecksum(context.Background(), soi); err != nil {
		t.Fatalf("GetObjectChecksum returned error: %v", err)
	}

	if soi.FileExists {
		t.Error("expected FileExists to remain false when no checksum is returned")
	}

	if soi.ObjectChecksum != nil {
		t.Errorf("ObjectChecksum = %q, want nil", *soi.ObjectChecksum)
	}
}

func TestAmazonS3GetObjectChecksumNotFoundWrapsErrNotFound(t *testing.T) {
	s3Storage := newTestS3Storage(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, testS3NoSuchKeyXML)
	})

	err := s3Storage.GetObjectChecksum(context.Background(), testS3Input(testS3Key))
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("GetObjectChecksum error = %v, want wrapped ErrNotFound", err)
	}
}

func TestAmazonS3GetObjectChecksumBackendError(t *testing.T) {
	s3Storage := newTestS3Storage(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, testS3AccessDeniedXML)
	})

	err := s3Storage.GetObjectChecksum(context.Background(), testS3Input(testS3Key))
	if err == nil {
		t.Fatal("expected error for access denied")
	}

	if errors.Is(err, ErrNotFound) {
		t.Errorf("access denied should not be reported as ErrNotFound: %v", err)
	}
}

func TestAmazonS3DeleteObject(t *testing.T) {
	var gotMethod, gotPath string
	s3Storage := newTestS3Storage(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	})

	if err := s3Storage.DeleteObject(context.Background(), testS3Input(testS3Key)); err != nil {
		t.Fatalf("DeleteObject returned error: %v", err)
	}

	if gotMethod != http.MethodDelete {
		t.Errorf("method = %q, want DELETE", gotMethod)
	}

	if gotPath != "/"+testS3Bucket+"/"+testS3Key {
		t.Errorf("path = %q, want /%s/%s", gotPath, testS3Bucket, testS3Key)
	}
}

func TestAmazonS3DeleteObjectBackendError(t *testing.T) {
	s3Storage := newTestS3Storage(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, testS3AccessDeniedXML)
	})

	if err := s3Storage.DeleteObject(context.Background(), testS3Input(testS3Key)); err == nil {
		t.Error("expected error for access denied")
	}
}

func TestAmazonS3PutObjectFromBytes(t *testing.T) {
	const payload = "bytes-payload"
	var gotMethod string
	var gotBody string
	s3Storage := newTestS3Storage(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.Header().Set("ETag", `"etag"`)
		w.WriteHeader(http.StatusOK)
	})

	soi := testS3Input(testS3Key)
	soi.FileBytes = []byte(payload)
	if err := s3Storage.PutObject(context.Background(), soi); err != nil {
		t.Fatalf("PutObject returned error: %v", err)
	}

	if gotMethod != http.MethodPut {
		t.Errorf("method = %q, want PUT", gotMethod)
	}

	if !strings.Contains(gotBody, payload) {
		t.Errorf("uploaded body does not contain payload %q", payload)
	}

	if soi.BytesWritten != int64(len(payload)) {
		t.Errorf("BytesWritten = %d, want %d", soi.BytesWritten, len(payload))
	}
}

func TestAmazonS3PutObjectFromReader(t *testing.T) {
	const payload = "streamed-payload"
	var gotBody string
	s3Storage := newTestS3Storage(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.WriteHeader(http.StatusOK)
	})

	soi := testS3Input(testS3Key)
	soi.FileReader = strings.NewReader(payload)
	if err := s3Storage.PutObject(context.Background(), soi); err != nil {
		t.Fatalf("PutObject returned error: %v", err)
	}

	if !strings.Contains(gotBody, payload) {
		t.Errorf("uploaded body does not contain payload %q", payload)
	}

	if soi.BytesWritten != int64(len(payload)) {
		t.Errorf("BytesWritten = %d, want %d", soi.BytesWritten, len(payload))
	}
}

func TestAmazonS3PutObjectBackendErrorResetsBytesWritten(t *testing.T) {
	s3Storage := newTestS3Storage(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, testS3AccessDeniedXML)
	})

	soi := testS3Input(testS3Key)
	soi.FileBytes = []byte("bytes-payload")
	if err := s3Storage.PutObject(context.Background(), soi); err == nil {
		t.Fatal("expected error for access denied")
	}

	if soi.BytesWritten != 0 {
		t.Errorf("BytesWritten = %d, want 0 after failed upload", soi.BytesWritten)
	}
}

func TestAmazonS3PresignObjectDefaultTTL(t *testing.T) {
	s3Storage := newTestS3Storage(t, func(w http.ResponseWriter, _ *http.Request) {
		t.Errorf("presigning should not contact the endpoint")
		w.WriteHeader(http.StatusInternalServerError)
	})

	soi := testS3Input(testS3Key)
	if err := s3Storage.PresignObject(context.Background(), soi); err != nil {
		t.Fatalf("PresignObject returned error: %v", err)
	}

	if soi.PresignedURL == nil {
		t.Fatal("expected PresignedURL to be set")
	}

	if !strings.Contains(*soi.PresignedURL, "/"+testS3Bucket+"/"+testS3Key) {
		t.Errorf("PresignedURL %q does not reference the object", *soi.PresignedURL)
	}

	if !strings.Contains(*soi.PresignedURL, "X-Amz-Expires=900") {
		t.Errorf("PresignedURL %q does not use the 15 minute default TTL", *soi.PresignedURL)
	}
}

func TestAmazonS3PresignObjectCustomTTL(t *testing.T) {
	s3Storage := newTestS3Storage(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	soi := testS3Input(testS3Key)
	soi.PresignTTL = 2 * time.Minute
	if err := s3Storage.PresignObject(context.Background(), soi); err != nil {
		t.Fatalf("PresignObject returned error: %v", err)
	}

	if soi.PresignedURL == nil || !strings.Contains(*soi.PresignedURL, "X-Amz-Expires=120") {
		t.Errorf("PresignedURL = %v, want X-Amz-Expires=120", soi.PresignedURL)
	}
}
