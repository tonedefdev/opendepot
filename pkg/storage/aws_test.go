package storage

import (
	"context"
	"os"
	"testing"
)

const s3UsePathStyleEnvVar = "OPENDEPOT_S3_USE_PATH_STYLE"

func TestAmazonS3NewClientPathStyleDefaultsOff(t *testing.T) {
	t.Setenv(s3UsePathStyleEnvVar, "")
	os.Unsetenv(s3UsePathStyleEnvVar)

	storage := &AmazonS3Storage{}
	if err := storage.NewClient(context.Background(), "us-east-1"); err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}

	if storage.client.Options().UsePathStyle {
		t.Errorf("expected UsePathStyle to be false when %s is unset", s3UsePathStyleEnvVar)
	}
}

func TestAmazonS3NewClientPathStyleEnabledWhenTrue(t *testing.T) {
	t.Setenv(s3UsePathStyleEnvVar, "true")

	storage := &AmazonS3Storage{}
	if err := storage.NewClient(context.Background(), "us-east-1"); err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}

	if !storage.client.Options().UsePathStyle {
		t.Errorf("expected UsePathStyle to be true when %s is true", s3UsePathStyleEnvVar)
	}
}

func TestAmazonS3NewClientPathStyleDisabledWhenFalse(t *testing.T) {
	t.Setenv(s3UsePathStyleEnvVar, "false")

	storage := &AmazonS3Storage{}
	if err := storage.NewClient(context.Background(), "us-east-1"); err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}

	if storage.client.Options().UsePathStyle {
		t.Errorf("expected UsePathStyle to be false when %s is false", s3UsePathStyleEnvVar)
	}
}
