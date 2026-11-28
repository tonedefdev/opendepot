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
	"time"

	"github.com/go-logr/logr"
	"github.com/tonedefdev/opendepot/pkg/mcp"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	ctrl "sigs.k8s.io/controller-runtime/pkg/reconcile"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8stypes "k8s.io/apimachinery/pkg/types"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	"github.com/tonedefdev/opendepot/services/version/internal/policy"
)

const testTrivyCritical = `{"SchemaVersion":2,"Results":[{"Target":"node_modules/x","Vulnerabilities":[{"VulnerabilityID":"CVE-2099-0001","PkgName":"x","InstalledVersion":"1.0.0","Severity":"CRITICAL","Title":"test critical"}]}]}`

func TestMCPSourceRuntime(t *testing.T) {
	for command, want := range map[string]string{
		"node":          mcp.RuntimeNode,
		"/usr/bin/node": mcp.RuntimeNode,
		"python":        mcp.RuntimePython,
		"python3":       mcp.RuntimePython,
	} {
		got, err := mcpSourceRuntime(command)
		if err != nil || got != want {
			t.Fatalf("mcpSourceRuntime(%q) = %q, %v; want %q", command, got, err, want)
		}
	}

	_, err := mcpSourceRuntime("bash")
	if got := mcpFindingCode(err); got != mcpFindingInvalid {
		t.Fatalf("bash finding = %q, want %q", got, mcpFindingInvalid)
	}
}

func TestMCPTransportDefaultsToStdio(t *testing.T) {
	cfg := &opendepotv1alpha1.MCPServerSourceConfig{}
	if got := mcpTransport(cfg); got != mcp.TransportStdio {
		t.Fatalf("default transport = %q, want %q", got, mcp.TransportStdio)
	}

	cfg.Transport = mcp.TransportStreamableHTTP
	if got := mcpTransport(cfg); got != mcp.TransportStreamableHTTP {
		t.Fatalf("transport = %q, want %q", got, mcp.TransportStreamableHTTP)
	}
}

func TestWriteMCPDescriptorWritesValidDescriptor(t *testing.T) {
	tree := t.TempDir()
	writeMCPFiles(t, tree, map[string]string{"dist/index.js": "export {}"})

	vendored := &mcpVendored{
		tree: tree,
		descriptor: &mcp.LaunchDescriptor{
			Command:   "node",
			Args:      []string{"dist/index.js"},
			Transport: mcp.TransportStdio,
			Runtime:   mcp.RuntimeNode,
		},
	}

	if err := writeMCPDescriptor(vendored); err != nil {
		t.Fatalf("writeMCPDescriptor: %v", err)
	}

	if _, err := os.Stat(filepath.Join(tree, mcp.LaunchDescriptorFile)); err != nil {
		t.Fatalf("descriptor not written: %v", err)
	}
}

func TestWriteMCPDescriptorSkipsWithoutDescriptor(t *testing.T) {
	tree := t.TempDir()
	if err := writeMCPDescriptor(&mcpVendored{tree: tree}); err != nil {
		t.Fatalf("writeMCPDescriptor: %v", err)
	}

	if _, err := os.Stat(filepath.Join(tree, mcp.LaunchDescriptorFile)); !os.IsNotExist(err) {
		t.Fatalf("descriptor written for a non-runnable tree: %v", err)
	}
}

func TestRunMCPScanBlocksCriticalFinding(t *testing.T) {
	var gotArgs []string
	original := runTrivy
	runTrivy = func(_ context.Context, args ...string) ([]byte, error) {
		gotArgs = args
		return []byte(testTrivyCritical), nil
	}
	t.Cleanup(func() { runTrivy = original })

	reconciler := &VersionReconciler{}
	resolved := policy.Resolve(nil, true, false, time.Now())

	sourceScan, blocking, err := reconciler.runMCPScan(context.Background(), t.TempDir(), resolved)
	if err != nil {
		t.Fatalf("runMCPScan: %v", err)
	}

	if blocking == nil || blocking.VulnerabilityID != "CVE-2099-0001" {
		t.Fatalf("blocking = %+v, want CVE-2099-0001", blocking)
	}

	if sourceScan == nil || len(sourceScan.Findings) == 0 {
		t.Fatal("scan record has no findings")
	}

	if !strings.Contains(strings.Join(gotArgs, " "), "--scanners vuln,secret") {
		t.Fatalf("trivy args = %v, want vuln and secret scanners", gotArgs)
	}
}

func TestRunMCPScanEmptyOutputFails(t *testing.T) {
	original := runTrivy
	runTrivy = func(_ context.Context, _ ...string) ([]byte, error) {
		return nil, nil
	}
	t.Cleanup(func() { runTrivy = original })

	reconciler := &VersionReconciler{}
	_, _, err := reconciler.runMCPScan(context.Background(), t.TempDir(), policy.Resolved{})
	if err == nil {
		t.Fatal("empty trivy output accepted")
	}
}

func TestSignMCPArchiveSignsSHA256SUMS(t *testing.T) {
	setJevSigningKey(t)

	signed, err := signAgentArchive([]byte("archive-bytes"), "example-mcp-1.0.0.tar.gz", nil)
	if err != nil {
		t.Fatalf("signAgentArchive: %v", err)
	}

	if signed.sums == "" || signed.signature == "" || signed.fingerprint == "" {
		t.Fatalf("signed archive missing SHA256SUMS, signature, or fingerprint: %+v", signed)
	}

	if !strings.Contains(signed.sums, "example-mcp-1.0.0.tar.gz") {
		t.Fatalf("SHA256SUMS does not list the archive: %q", signed.sums)
	}
}

func TestVendorMCPSourceRejectsTraversal(t *testing.T) {
	reconciler := &VersionReconciler{}
	version := &opendepotv1alpha1.Version{Spec: opendepotv1alpha1.VersionSpec{Version: "1.0.0"}}
	cfg := &opendepotv1alpha1.MCPServerSourceConfig{
		Source: opendepotv1alpha1.MCPServerSource{
			Type: mcpSourceRepo,
			Source: &opendepotv1alpha1.MCPRepoSource{
				RepoOwner: "owner",
				Path:      "../../etc",
			},
		},
	}

	_, err := reconciler.vendorMCPSource(context.Background(), version, cfg, mcp.TransportStdio, t.TempDir())
	if got := mcpFindingCode(err); got != mcpFindingInvalid {
		t.Fatalf("traversal finding = %q, want %q (err=%v)", got, mcpFindingInvalid, err)
	}
}

func newMCPSyncTestClient(t *testing.T, objects ...runtime.Object) *VersionReconciler {
	t.Helper()

	scheme := runtime.NewScheme()
	if err := opendepotv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}

	return &VersionReconciler{
		Client: fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(objects...).WithStatusSubresource(&opendepotv1alpha1.Version{}).Build(),
		Scheme: scheme,
		Log:    logr.Discard(),
	}
}

func newMCPImageVersion(artifact string) *opendepotv1alpha1.Version {
	return &opendepotv1alpha1.Version{
		ObjectMeta: metav1.ObjectMeta{Name: "example-mcp-1-0-0", Namespace: "default"},
		Spec: opendepotv1alpha1.VersionSpec{
			Type:    opendepotv1alpha1.OpenDepotMCPServer,
			Version: "1.0.0",
			MCPServerSourceRef: &opendepotv1alpha1.MCPServerSourceConfig{
				Source: opendepotv1alpha1.MCPServerSource{
					Type:  mcpSourceImage,
					Image: &opendepotv1alpha1.MCPImageSource{OCIArtifactRef: corev1.LocalObjectReference{Name: artifact}},
				},
			},
		},
	}
}

func TestReconcileMCPImagePublishesSyncedDigest(t *testing.T) {
	artifact := &opendepotv1alpha1.OCIArtifact{
		ObjectMeta: metav1.ObjectMeta{Name: "example-image", Namespace: "default"},
		Spec: opendepotv1alpha1.OCIArtifactSpec{
			OCIArtifactConfig: opendepotv1alpha1.OCIArtifactConfig{ArtifactType: ociArtifactImage},
		},
	}
	ociVersion := &opendepotv1alpha1.Version{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "example-image-1-0-0",
			Namespace: "default",
			Labels: map[string]string{
				mcpOCIArtifactLabelKey: "example-image",
				mcpNamespaceLabelKey:   "default",
			},
		},
		Spec: opendepotv1alpha1.VersionSpec{Type: opendepotv1alpha1.OpenDepotOCIArtifact, Version: "1.0.0"},
		Status: opendepotv1alpha1.VersionStatus{
			Synced:      true,
			OCIManifest: &opendepotv1alpha1.OCIManifestStatus{Digest: "sha256:abc123"},
		},
	}
	version := newMCPImageVersion("example-image")
	version.Name = "example-mcp-image-1-0-0"

	reconciler := newMCPSyncTestClient(t, artifact, ociVersion, version)
	req := ctrl.Request{NamespacedName: k8stypes.NamespacedName{Name: version.Name, Namespace: "default"}}

	if _, err := reconciler.reconcileMCPVersion(context.Background(), req, version); err != nil {
		t.Fatalf("reconcileMCPVersion: %v", err)
	}

	current := &opendepotv1alpha1.Version{}
	if err := reconciler.Get(context.Background(), req.NamespacedName, current); err != nil {
		t.Fatalf("get version: %v", err)
	}

	if !current.Status.Synced || current.Status.MCPMetadata == nil {
		t.Fatalf("status = %+v, want synced with MCP metadata", current.Status)
	}

	if current.Status.MCPMetadata.PackageDigest != "sha256:abc123" || !current.Status.MCPMetadata.Runnable {
		t.Fatalf("MCP metadata = %+v, want digest sha256:abc123 and runnable", current.Status.MCPMetadata)
	}
}

func TestReconcileMCPImageRejectsWrongArtifactType(t *testing.T) {
	artifact := &opendepotv1alpha1.OCIArtifact{
		ObjectMeta: metav1.ObjectMeta{Name: "not-image", Namespace: "default"},
		Spec: opendepotv1alpha1.OCIArtifactSpec{
			OCIArtifactConfig: opendepotv1alpha1.OCIArtifactConfig{ArtifactType: "Skill"},
		},
	}
	version := newMCPImageVersion("not-image")

	reconciler := newMCPSyncTestClient(t, artifact, version)
	req := ctrl.Request{NamespacedName: k8stypes.NamespacedName{Name: version.Name, Namespace: "default"}}

	if _, err := reconciler.reconcileMCPVersion(context.Background(), req, version); err != nil {
		t.Fatalf("terminal finding returned an error: %v", err)
	}

	current := &opendepotv1alpha1.Version{}
	if err := reconciler.Get(context.Background(), req.NamespacedName, current); err != nil {
		t.Fatalf("get version: %v", err)
	}

	if current.Status.Synced {
		t.Fatal("Version with the wrong artifact type was synced")
	}
}

func TestReconcileMCPMissingSourceRefFails(t *testing.T) {
	version := &opendepotv1alpha1.Version{
		ObjectMeta: metav1.ObjectMeta{Name: "no-source-1-0-0", Namespace: "default"},
		Spec:       opendepotv1alpha1.VersionSpec{Type: opendepotv1alpha1.OpenDepotMCPServer, Version: "1.0.0"},
	}

	reconciler := newMCPSyncTestClient(t, version)
	req := ctrl.Request{NamespacedName: k8stypes.NamespacedName{Name: version.Name, Namespace: "default"}}

	if _, err := reconciler.reconcileMCPVersion(context.Background(), req, version); err != nil {
		t.Fatalf("reconcileMCPVersion: %v", err)
	}

	current := &opendepotv1alpha1.Version{}
	if err := reconciler.Get(context.Background(), req.NamespacedName, current); err != nil {
		t.Fatalf("get version: %v", err)
	}

	if current.Status.Synced {
		t.Fatal("Version without a source reference was synced")
	}
}
