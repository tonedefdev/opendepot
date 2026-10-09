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
	"context"
	"testing"
)

func moduleTestArchive(t *testing.T) []byte {
	t.Helper()

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("main.tf")
	if err != nil {
		t.Fatalf("create zip entry: %v", err)
	}
	if _, err := w.Write([]byte("terraform {}\n")); err != nil {
		t.Fatalf("write zip entry: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}

	return buf.Bytes()
}

func TestScanModuleArchiveFailsClosedOnUnusableTrivyOutput(t *testing.T) {
	original := runTrivy
	t.Cleanup(func() { runTrivy = original })

	tests := []struct {
		name    string
		output  []byte
		wantErr bool
	}{
		{name: "empty output", output: nil, wantErr: true},
		{name: "non-json output", output: []byte("FATAL error"), wantErr: true},
		{name: "valid empty report", output: []byte(`{"Results":[]}`), wantErr: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runTrivy = func(ctx context.Context, args ...string) ([]byte, error) {
				return test.output, nil
			}

			r := &VersionReconciler{scanSem: make(chan struct{}, 1)}
			findings, err := r.scanModuleArchive(context.Background(), moduleTestArchive(t), t.TempDir(), true)

			if test.wantErr {
				if err == nil {
					t.Fatalf("expected error, got findings=%v", findings)
				}

				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
