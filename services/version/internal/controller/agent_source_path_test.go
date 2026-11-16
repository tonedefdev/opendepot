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
	"os"
	"path/filepath"
	"testing"

	"github.com/tonedefdev/opendepot/pkg/agentspec"
)

// Tests
func TestStageAgentFileIsolatesDefinitionFile(t *testing.T) {
	tmpDir := t.TempDir()
	sourceDir := filepath.Join(tmpDir, "repo", "agents")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatal(err)
	}

	filePath := filepath.Join(sourceDir, "accessibility.agent.md")
	if err := os.WriteFile(filePath, []byte("---\nname: accessibility\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	otherPath := filepath.Join(sourceDir, "other.agent.md")
	if err := os.WriteFile(otherPath, []byte("---\nname: other\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	stageDir, err := stageAgentFile(tmpDir, filePath)
	if err != nil {
		t.Fatalf("stageAgentFile returned error: %v", err)
	}

	entries, err := os.ReadDir(stageDir)
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 1 || entries[0].Name() != "accessibility.agent.md" {
		t.Fatalf("staged directory holds %d entries, want only accessibility.agent.md", len(entries))
	}

	entryPath, err := agentspec.FindAgentEntry(stageDir, "accessibility")
	if err != nil {
		t.Fatalf("FindAgentEntry returned error: %v", err)
	}

	if entryPath != filepath.Join(stageDir, "accessibility.agent.md") {
		t.Fatalf("entry = %s, want the staged definition file", entryPath)
	}

	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Fatalf("original file still present after staging: %v", err)
	}
}
