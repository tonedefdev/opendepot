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
	"net/http"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8stypes "k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	"github.com/tonedefdev/opendepot/services/version/internal/policy"
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

func TestReadMCPMetadataFallbacks(t *testing.T) {
	long := strings.Repeat("n", mcpMaxNameRunes+10)
	tests := []struct {
		name        string
		files       map[string]string
		cfgName     *string
		version     string
		wantName    string
		wantDesc    string
		wantFinding string
	}{
		{
			name:     "package.json supplies name and description",
			files:    map[string]string{"package.json": `{"name":"pkg-mcp","description":"from package"}`},
			wantName: "pkg-mcp",
			wantDesc: "from package",
		},
		{
			name:     "spec name overrides server.json",
			files:    map[string]string{"server.json": testServerJSON},
			cfgName:  &long,
			wantName: long[:mcpMaxNameRunes],
			wantDesc: "An example MCP server",
		},
		{
			name:     "no metadata falls back to the Version name",
			files:    map[string]string{},
			version:  "fallback-version",
			wantName: "fallback-version",
		},
		{
			name:        "invalid server.json is terminal",
			files:       map[string]string{"server.json": `{"name":`},
			wantFinding: mcpFindingInvalid,
		},
		{
			name:        "invalid package.json is terminal",
			files:       map[string]string{"package.json": `{"name":`},
			wantFinding: mcpFindingInvalid,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pkgDir := t.TempDir()
			writeMCPFiles(t, pkgDir, tc.files)

			cfg := &opendepotv1alpha1.MCPServerSourceConfig{
				Name:   tc.cfgName,
				Source: opendepotv1alpha1.MCPServerSource{Type: "Unknown"},
			}
			version := &opendepotv1alpha1.Version{ObjectMeta: metav1.ObjectMeta{Name: tc.version}}

			meta, err := readMCPMetadata(pkgDir, cfg, version)
			if got := mcpFindingCode(err); got != tc.wantFinding {
				t.Fatalf("finding = %q, want %q (err=%v)", got, tc.wantFinding, err)
			}

			if tc.wantFinding != "" {
				return
			}

			if err != nil {
				t.Fatalf("readMCPMetadata: %v", err)
			}

			if meta.name != tc.wantName || meta.description != tc.wantDesc {
				t.Fatalf("meta = %+v, want name %q and description %q", meta, tc.wantName, tc.wantDesc)
			}
		})
	}
}

func TestReadMCPMetadataRequiresName(t *testing.T) {
	cfg := &opendepotv1alpha1.MCPServerSourceConfig{Source: opendepotv1alpha1.MCPServerSource{Type: "Unknown"}}
	version := &opendepotv1alpha1.Version{}

	if _, err := readMCPMetadata(t.TempDir(), cfg, version); mcpFindingCode(err) != mcpFindingInvalid {
		t.Fatalf("nameless package finding = %v, want invalid", err)
	}
}

func TestMCPSourceNameByType(t *testing.T) {
	version := &opendepotv1alpha1.Version{ObjectMeta: metav1.ObjectMeta{Name: "version-name"}}

	tests := []struct {
		name string
		cfg  *opendepotv1alpha1.MCPServerSourceConfig
		want string
	}{
		{
			name: "Npm package",
			cfg:  &opendepotv1alpha1.MCPServerSourceConfig{Source: opendepotv1alpha1.MCPServerSource{Type: mcpSourceNpm, Npm: &opendepotv1alpha1.MCPNpmSource{Package: "npm-pkg"}}},
			want: "npm-pkg",
		},
		{
			name: "PyPI package",
			cfg:  &opendepotv1alpha1.MCPServerSourceConfig{Source: opendepotv1alpha1.MCPServerSource{Type: mcpSourcePyPI, PyPI: &opendepotv1alpha1.MCPPyPISource{Package: "py-pkg"}}},
			want: "py-pkg",
		},
		{
			name: "Source repository path base",
			cfg:  &opendepotv1alpha1.MCPServerSourceConfig{Source: opendepotv1alpha1.MCPServerSource{Type: mcpSourceRepo, Source: &opendepotv1alpha1.MCPRepoSource{Path: "servers/tools/"}}},
			want: "tools",
		},
		{
			name: "Npm without a package block uses the Version name",
			cfg:  &opendepotv1alpha1.MCPServerSourceConfig{Source: opendepotv1alpha1.MCPServerSource{Type: mcpSourceNpm}},
			want: "version-name",
		},
		{
			name: "Source without a repository block uses the Version name",
			cfg:  &opendepotv1alpha1.MCPServerSourceConfig{Source: opendepotv1alpha1.MCPServerSource{Type: mcpSourceRepo}},
			want: "version-name",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := mcpSourceName(tc.cfg, version); got != tc.want {
				t.Fatalf("mcpSourceName = %q, want %q", got, tc.want)
			}
		})
	}
}

func newMCPJevGateReconciler(t *testing.T, endpoint string, version *opendepotv1alpha1.Version) *VersionReconciler {
	t.Helper()

	reconciler := newJevTestReconciler(t, endpoint)
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}

	if err := opendepotv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "jev-token", Namespace: "default"},
		Data:       map[string][]byte{"jevToken": []byte(jevSentinelToken)},
	}

	reconciler.Client = fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).WithRuntimeObjects(version).WithStatusSubresource(&opendepotv1alpha1.Version{}).Build()
	reconciler.Scheme = scheme
	reconciler.Log = logr.Discard()

	return reconciler
}

func testMCPJevGateContent(t *testing.T) *agentContent {
	t.Helper()

	pkgDir := t.TempDir()
	writeMCPFiles(t, pkgDir, map[string]string{"server.json": testServerJSON, "package.json": `{"name":"example-mcp"}`})
	content, err := stageMCPJev(&mcpVendored{tree: pkgDir, pkgDir: pkgDir}, mcpMeta{name: "example-mcp"}, "abc123")
	if err != nil {
		t.Fatalf("stageMCPJev: %v", err)
	}

	return content
}

func TestGateMCPJevAllowsCleanAssessment(t *testing.T) {
	endpoint, calls := newJevEndpoint(t, jevOK)
	version := newMCPJevTestVersion(nil)
	reconciler := newMCPJevGateReconciler(t, endpoint, version)
	req := ctrl.Request{NamespacedName: k8stypes.NamespacedName{Name: version.Name, Namespace: "default"}}

	gate, _, stop, err := reconciler.gateMCPJev(context.Background(), req, version, policy.Resolved{}, nil, nil, testMCPJevGateContent(t))
	if err != nil || stop {
		t.Fatalf("gateMCPJev stop=%v err=%v, want a clean pass", stop, err)
	}

	if gate.assessment == nil || gate.assessment.Blocked || calls.Load() != 1 {
		t.Fatalf("gate assessment = %+v after %d calls, want an unblocked assessment", gate.assessment, calls.Load())
	}
}

func TestGateMCPJevStopsWhenDisabled(t *testing.T) {
	version := newMCPJevTestVersion(nil)
	reconciler := newMCPJevGateReconciler(t, "", version)
	reconciler.JevEnabled = false
	req := ctrl.Request{NamespacedName: k8stypes.NamespacedName{Name: version.Name, Namespace: "default"}}

	gate, _, stop, _ := reconciler.gateMCPJev(context.Background(), req, version, policy.Resolved{}, nil, nil, testMCPJevGateContent(t))
	if !stop {
		t.Fatal("disabled Jev did not stop the Version from publishing")
	}

	if gate.assessment == nil || !gate.assessment.Blocked {
		t.Fatalf("disabled Jev assessment = %+v, want a blocked assessment", gate.assessment)
	}

	current := &opendepotv1alpha1.Version{}
	if err := reconciler.Get(context.Background(), req.NamespacedName, current); err != nil {
		t.Fatalf("get version: %v", err)
	}

	if current.Status.JevAssessment == nil || !current.Status.JevAssessment.Blocked {
		t.Fatalf("persisted assessment = %+v, want blocked", current.Status.JevAssessment)
	}
}

func TestGateMCPJevHoldsWhenServiceUnavailable(t *testing.T) {
	endpoint, _ := newJevEndpoint(t, jevStatus(http.StatusServiceUnavailable, `{"error":"down"}`))
	version := newMCPJevTestVersion(nil)
	reconciler := newMCPJevGateReconciler(t, endpoint, version)
	req := ctrl.Request{NamespacedName: k8stypes.NamespacedName{Name: version.Name, Namespace: "default"}}

	_, result, stop, _ := reconciler.gateMCPJev(context.Background(), req, version, policy.Resolved{}, nil, nil, testMCPJevGateContent(t))
	if !stop {
		t.Fatal("unavailable Jev service did not hold the Version")
	}

	if result.RequeueAfter == 0 && !result.IsZero() {
		t.Fatalf("result = %+v, want a requeue or a terminal hold", result)
	}
}
