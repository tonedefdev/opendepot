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

package controller

import (
	"archive/zip"
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestReadBoundedEntry(t *testing.T) {
	const limit int64 = 16

	t.Run("under limit", func(t *testing.T) {
		data, err := readBoundedEntry(bytes.NewReader(bytes.Repeat([]byte("a"), 15)), limit)
		if err != nil {
			t.Fatalf("readBoundedEntry() error = %v, want nil", err)
		}

		if len(data) != 15 {
			t.Fatalf("readBoundedEntry() length = %d, want 15", len(data))
		}
	})

	t.Run("exactly at limit", func(t *testing.T) {
		data, err := readBoundedEntry(bytes.NewReader(bytes.Repeat([]byte("a"), 16)), limit)
		if err != nil {
			t.Fatalf("readBoundedEntry() error = %v, want nil", err)
		}

		if len(data) != 16 {
			t.Fatalf("readBoundedEntry() length = %d, want 16", len(data))
		}
	})

	t.Run("one byte over limit", func(t *testing.T) {
		_, err := readBoundedEntry(bytes.NewReader(bytes.Repeat([]byte("a"), 17)), limit)
		if !errors.Is(err, ErrProviderBinaryTooLarge) {
			t.Fatalf("readBoundedEntry() error = %v, want ErrProviderBinaryTooLarge", err)
		}
	})
}

func TestExtractBinaryFromZipRejectsDeclaredOversizeEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "provider.zip")
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("os.Create() error = %v", err)
	}

	zw := zip.NewWriter(file)
	header := &zip.FileHeader{
		Name:               "terraform-provider-example",
		Method:             zip.Store,
		UncompressedSize64: uint64(maxProviderBinaryBytes) + 1,
		CompressedSize64:   4,
	}
	header.SetMode(0755)
	entry, err := zw.CreateRaw(header)
	if err != nil {
		t.Fatalf("CreateRaw() error = %v", err)
	}

	if _, err := entry.Write([]byte("abcd")); err != nil {
		t.Fatalf("entry.Write() error = %v", err)
	}

	if err := zw.Close(); err != nil {
		t.Fatalf("zw.Close() error = %v", err)
	}

	if err := file.Close(); err != nil {
		t.Fatalf("file.Close() error = %v", err)
	}

	_, err = extractBinaryFromZip(path)
	if !errors.Is(err, ErrProviderBinaryTooLarge) {
		t.Fatalf("extractBinaryFromZip() error = %v, want ErrProviderBinaryTooLarge", err)
	}
}
