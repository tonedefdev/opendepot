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
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
)

const testServerJSON = `{
  "$schema": "https://static.modelcontextprotocol.io/schemas/2025-12-11/server.schema.json",
  "name": "io.example/example-mcp",
  "description": "An example MCP server",
  "version": "1.0.0"
}`

func newMCPJevTestVersion(cached *opendepotv1alpha1.JevAssessment) *opendepotv1alpha1.Version {
	name := "example-mcp"
	version := &opendepotv1alpha1.Version{
		ObjectMeta: metav1.ObjectMeta{Name: "example-mcp-1-0-0", Namespace: "default"},
		Spec: opendepotv1alpha1.VersionSpec{
			Type:    opendepotv1alpha1.OpenDepotMCPServer,
			Version: "1.0.0",
			MCPServerSourceRef: &opendepotv1alpha1.MCPServerSourceConfig{
				Name: &name,
				Source: opendepotv1alpha1.MCPServerSource{
					Type: mcpSourceNpm,
					Npm:  &opendepotv1alpha1.MCPNpmSource{Package: "example-mcp"},
				},
				JevSecretRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "jev-token"},
				},
			},
		},
	}
	version.Status.JevAssessment = cached

	return version
}

func TestStageMCPJevRequiresMetadataFiles(t *testing.T) {
	vendored := &mcpVendored{tree: t.TempDir(), pkgDir: t.TempDir()}

	_, err := stageMCPJev(vendored, mcpMeta{name: "example-mcp"}, "abc")
	if got := mcpFindingCode(err); got != mcpFindingInvalid {
		t.Fatalf("finding = %q, want %q (err=%v)", got, mcpFindingInvalid, err)
	}
}

func TestStageMCPJevAssessesOnlyMetadataFiles(t *testing.T) {
	pkgDir := t.TempDir()
	writeMCPFiles(t, pkgDir, map[string]string{
		"server.json":          testServerJSON,
		"package.json":         `{"name":"example-mcp"}`,
		"README.md":            "# Example",
		"node_modules/x/a.js":  "module.exports = {}",
		"node_modules/x/b.txt": "dependency",
	})

	vendored := &mcpVendored{tree: pkgDir, pkgDir: pkgDir}
	content, err := stageMCPJev(vendored, mcpMeta{name: "example-mcp", description: "An example"}, "abc")
	if err != nil {
		t.Fatalf("stageMCPJev: %v", err)
	}

	want := map[string]bool{"server.json": true, "package.json": true, "README.md": true}
	if len(content.files) != len(want) {
		t.Fatalf("staged files = %v, want only metadata files", content.files)
	}

	for _, f := range content.files {
		if !want[f] {
			t.Fatalf("staged file %q is not a metadata file", f)
		}
	}

	if content.checksum != "abc" || content.definition == nil || content.definition.Name != "example-mcp" {
		t.Fatalf("staged content = %+v, want checksum and definition set", content)
	}
}

func TestReadMCPMetadataPrefersServerJSON(t *testing.T) {
	pkgDir := t.TempDir()
	writeMCPFiles(t, pkgDir, map[string]string{
		"server.json":  testServerJSON,
		"package.json": `{"name":"fallback","description":"fallback description"}`,
	})

	version := newMCPJevTestVersion(nil)
	meta, err := readMCPMetadata(pkgDir, version.Spec.MCPServerSourceRef, version)
	if err != nil {
		t.Fatalf("readMCPMetadata: %v", err)
	}

	if meta.name == "" || meta.description != "An example MCP server" {
		t.Fatalf("metadata = %+v, want the server.json description", meta)
	}
}

func TestRunMCPJevAssessesMetadataWithoutThresholds(t *testing.T) {
	endpoint, calls := newJevEndpoint(t, jevOK)
	reconciler := newJevTestReconciler(t, endpoint)

	pkgDir := t.TempDir()
	writeMCPFiles(t, pkgDir, map[string]string{
		"server.json":  testServerJSON,
		"package.json": `{"name":"example-mcp"}`,
	})

	version := newMCPJevTestVersion(nil)
	vendored := &mcpVendored{tree: pkgDir, pkgDir: pkgDir}
	content, err := stageMCPJev(vendored, mcpMeta{name: "example-mcp"}, "abc123")
	if err != nil {
		t.Fatalf("stageMCPJev: %v", err)
	}

	assessment, err := reconciler.runAgentJev(context.Background(), mcpAgentShadow(version), content)
	if err != nil {
		t.Fatalf("runAgentJev: %v", err)
	}

	if assessment.Blocked || assessment.Error != "" || assessment.ChunkCount != 1 {
		t.Fatalf("assessment = %+v, want a clean single-part assessment", assessment)
	}

	if calls.Load() != 1 {
		t.Fatalf("Jev calls = %d, want 1", calls.Load())
	}
}

func TestRunMCPJevChunksLargeMetadata(t *testing.T) {
	endpoint, calls := newJevEndpoint(t, jevOK)
	reconciler := newJevTestReconciler(t, endpoint)

	pkgDir := t.TempDir()
	large := strings.Repeat("Documentation line for the MCP server.\n", 3*jevChunkBodyBudget/38+1)
	writeMCPFiles(t, pkgDir, map[string]string{
		"server.json": testServerJSON,
		"README.md":   large,
	})

	version := newMCPJevTestVersion(nil)
	vendored := &mcpVendored{tree: pkgDir, pkgDir: pkgDir}
	content, err := stageMCPJev(vendored, mcpMeta{name: "example-mcp"}, "def456")
	if err != nil {
		t.Fatalf("stageMCPJev: %v", err)
	}

	assessment, err := reconciler.runAgentJev(context.Background(), mcpAgentShadow(version), content)
	if err != nil {
		t.Fatalf("runAgentJev: %v", err)
	}

	if assessment.ChunkCount < 2 {
		t.Fatalf("ChunkCount = %d, want the metadata split into multiple parts", assessment.ChunkCount)
	}

	if calls.Load() != int32(assessment.ChunkCount) {
		t.Fatalf("Jev calls = %d, want one per chunk (%d)", calls.Load(), assessment.ChunkCount)
	}
}

func TestMCPAgentShadowCarriesJevConfig(t *testing.T) {
	version := newMCPJevTestVersion(nil)
	shadow := mcpAgentShadow(version)

	if shadow.Spec.Type != opendepotv1alpha1.OpenDepotAgent {
		t.Fatalf("shadow type = %q, want %q", shadow.Spec.Type, opendepotv1alpha1.OpenDepotAgent)
	}

	if shadow.Spec.AgentSourceRef == nil || shadow.Spec.AgentSourceRef.JevSecretRef == nil {
		t.Fatal("shadow Version lost the Jev secret reference")
	}

	if version.Spec.Type != opendepotv1alpha1.OpenDepotMCPServer {
		t.Fatal("mcpAgentShadow mutated the MCPServer Version")
	}
}
