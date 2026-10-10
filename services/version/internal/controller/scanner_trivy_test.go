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
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func installFakeTrivy(t *testing.T, script string) {
	t.Helper()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "trivy"), []byte("#!/bin/sh\n"+script+"\n"), 0755); err != nil {
		t.Fatalf("write fake trivy: %v", err)
	}

	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestRunTrivyExitOneWithoutReportIsError(t *testing.T) {
	installFakeTrivy(t, `echo "FATAL DB download failed" >&2; exit 1`)

	output, err := runTrivy(context.Background(), "fs", "--format", "json", ".")
	if err == nil {
		t.Fatal("runTrivy() error = nil, want error for exit 1 without a report")
	}

	if !strings.Contains(err.Error(), "FATAL DB download failed") {
		t.Fatalf("runTrivy() error = %v, want trivy stderr included", err)
	}

	if output != nil {
		t.Fatalf("runTrivy() output = %q, want nil", output)
	}
}

func TestRunTrivyExitOneWithReportReturnsOutput(t *testing.T) {
	installFakeTrivy(t, `printf '{"SchemaVersion":2,"Results":[]}'; exit 1`)

	output, err := runTrivy(context.Background(), "fs", "--format", "json", ".")
	if err != nil {
		t.Fatalf("runTrivy() error = %v, want nil", err)
	}

	if string(output) != `{"SchemaVersion":2,"Results":[]}` {
		t.Fatalf("runTrivy() output = %q, want report", output)
	}
}
