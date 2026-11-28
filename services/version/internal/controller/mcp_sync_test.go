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
	"errors"
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
		Client:                  fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(objects...).WithStatusSubresource(&opendepotv1alpha1.Version{}).Build(),
		Scheme:                  scheme,
		Log:                     logr.Discard(),
		MCPAllowedNpmRegistries: []string{"registry.npmjs.org"},
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

func newMCPImageArtifact(name, artifactType string) *opendepotv1alpha1.OCIArtifact {
	return &opendepotv1alpha1.OCIArtifact{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: opendepotv1alpha1.OCIArtifactSpec{
			OCIArtifactConfig: opendepotv1alpha1.OCIArtifactConfig{ArtifactType: artifactType},
		},
	}
}

func TestReconcileMCPImageWaitsForArtifacts(t *testing.T) {
	tests := []struct {
		name    string
		objects []runtime.Object
	}{
		{name: "OCIArtifact not created yet", objects: nil},
		{name: "OCIArtifact Version not synced", objects: []runtime.Object{newMCPImageArtifact("example-image", ociArtifactImage)}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			version := newMCPImageVersion("example-image")
			version.Name = "example-mcp-image-1-0-0"

			reconciler := newMCPSyncTestClient(t, append(tc.objects, version)...)
			req := ctrl.Request{NamespacedName: k8stypes.NamespacedName{Name: version.Name, Namespace: "default"}}

			result, err := reconciler.reconcileMCPVersion(context.Background(), req, version)
			if err != nil {
				t.Fatalf("reconcileMCPVersion: %v", err)
			}

			if result.RequeueAfter == 0 {
				t.Fatal("waiting Version did not requeue")
			}

			current := &opendepotv1alpha1.Version{}
			if err := reconciler.Get(context.Background(), req.NamespacedName, current); err != nil {
				t.Fatalf("get version: %v", err)
			}

			if current.Status.Synced {
				t.Fatal("Version synced before its OCIArtifact was ready")
			}
		})
	}
}

func TestReconcileMCPImageRequiresArtifactRef(t *testing.T) {
	version := newMCPImageVersion("")
	reconciler := newMCPSyncTestClient(t, version)
	req := ctrl.Request{NamespacedName: k8stypes.NamespacedName{Name: version.Name, Namespace: "default"}}

	if _, err := reconciler.reconcileMCPVersion(context.Background(), req, version); err != nil {
		t.Fatalf("terminal finding returned an error: %v", err)
	}

	current := &opendepotv1alpha1.Version{}
	if err := reconciler.Get(context.Background(), req.NamespacedName, current); err != nil {
		t.Fatalf("get version: %v", err)
	}

	if current.Status.Synced {
		t.Fatalf("status = %+v, want an unsynced Version", current.Status)
	}
}

func TestReconcileMCPImageUsesConfiguredNameAndClearsForceSync(t *testing.T) {
	artifact := newMCPImageArtifact("example-image", ociArtifactImage)
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
			OCIManifest: &opendepotv1alpha1.OCIManifestStatus{Digest: "sha256:def456"},
		},
	}
	version := newMCPImageVersion("example-image")
	version.Name = "example-mcp-image-1-0-0"
	version.Spec.ForceSync = true
	name := "Renamed Server"
	version.Spec.MCPServerSourceRef.Name = &name

	reconciler := newMCPSyncTestClient(t, artifact, ociVersion, version)
	req := ctrl.Request{NamespacedName: k8stypes.NamespacedName{Name: version.Name, Namespace: "default"}}

	if _, err := reconciler.reconcileMCPVersion(context.Background(), req, version); err != nil {
		t.Fatalf("reconcileMCPVersion: %v", err)
	}

	current := &opendepotv1alpha1.Version{}
	if err := reconciler.Get(context.Background(), req.NamespacedName, current); err != nil {
		t.Fatalf("get version: %v", err)
	}

	if current.Status.MCPMetadata == nil || current.Status.MCPMetadata.ServerName != name {
		t.Fatalf("MCP metadata = %+v, want server name %q", current.Status.MCPMetadata, name)
	}

	if current.Spec.ForceSync {
		t.Fatal("ForceSync was not cleared after the image published")
	}
}

func TestMCPFailureRecordsTransientError(t *testing.T) {
	version := newMCPImageVersion("example-image")
	version.Name = "example-mcp-image-1-0-0"
	reconciler := newMCPSyncTestClient(t, version)
	req := ctrl.Request{NamespacedName: k8stypes.NamespacedName{Name: version.Name, Namespace: "default"}}

	transient := errors.New("registry timeout")
	if _, err := reconciler.mcpFailure(context.Background(), req, transient); !errors.Is(err, transient) {
		t.Fatalf("mcpFailure err = %v, want the transient error returned for backoff", err)
	}

	current := &opendepotv1alpha1.Version{}
	if err := reconciler.Get(context.Background(), req.NamespacedName, current); err != nil {
		t.Fatalf("get version: %v", err)
	}

	if current.Status.Synced || !strings.Contains(current.Status.SyncStatus, "MCP sync failed") {
		t.Fatalf("status = %+v, want an unsynced Version naming the failure", current.Status)
	}
}

func TestVendorMCPSourceVendorsRepositoryPath(t *testing.T) {
	const commit = "0123456789abcdef0123456789abcdef01234567"
	tarball := testTarball(t, map[string]string{
		"owner-repo-0123456/servers/tool/index.js":     "export {}",
		"owner-repo-0123456/servers/tool/package.json": `{"name":"tool"}`,
	})

	tests := []struct {
		name       string
		repoPath   string
		entrypoint *opendepotv1alpha1.MCPEntrypoint
		finding    string
		descriptor bool
		fails      bool
	}{
		{name: "vendors the path without an entrypoint", repoPath: "servers/tool"},
		{name: "writes a node launch descriptor", repoPath: "servers/tool", entrypoint: &opendepotv1alpha1.MCPEntrypoint{Command: "node", Args: []string{"index.js"}}, descriptor: true},
		{name: "rejects an unsupported runtime", repoPath: "servers/tool", entrypoint: &opendepotv1alpha1.MCPEntrypoint{Command: "ruby"}, finding: mcpFindingInvalid},
		{name: "rejects a missing repository path", repoPath: "servers/missing", fails: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stubJevGitHub(t, tarball)

			repoURL := "https://github.com/owner/repo"
			cfg := &opendepotv1alpha1.MCPServerSourceConfig{
				Entrypoint: tc.entrypoint,
				Source: opendepotv1alpha1.MCPServerSource{
					Type:   mcpSourceRepo,
					Source: &opendepotv1alpha1.MCPRepoSource{RepoOwner: "owner", RepoUrl: &repoURL, Path: tc.repoPath},
				},
			}
			reconciler := &VersionReconciler{}
			version := &opendepotv1alpha1.Version{Spec: opendepotv1alpha1.VersionSpec{Version: "1.0.0", SourceCommit: commit, MCPServerSourceRef: cfg}}
			vendored, err := reconciler.vendorMCPSource(context.Background(), version, cfg, mcp.TransportStdio, t.TempDir())
			if tc.fails {
				if err == nil {
					t.Fatal("vendorMCPSource: want an error for a missing repository path")
				}

				return
			}

			if got := mcpFindingCode(err); got != tc.finding {
				t.Fatalf("finding = %q, want %q (err=%v)", got, tc.finding, err)
			}

			if tc.finding != "" {
				return
			}

			if err != nil {
				t.Fatalf("vendorMCPSource: %v", err)
			}

			if _, statErr := os.Stat(filepath.Join(vendored.tree, "index.js")); statErr != nil {
				t.Fatalf("vendored tree is missing the server entry: %v", statErr)
			}

			if (vendored.descriptor != nil) != tc.descriptor {
				t.Fatalf("descriptor = %+v, want present=%v", vendored.descriptor, tc.descriptor)
			}
		})
	}
}

func TestReconcileMCPVersionSkipsSyncedVersion(t *testing.T) {
	version := newMCPImageVersion("example-image")
	version.Name = "example-mcp-synced-1-0-0"
	version.Status.Synced = true
	reconciler := newMCPSyncTestClient(t, version)

	result, err := reconciler.reconcileMCPVersion(context.Background(), ctrl.Request{}, version)
	if err != nil || result != (ctrl.Result{}) {
		t.Fatalf("reconcileMCPVersion on a synced Version = %+v, %v; want a no-op", result, err)
	}
}

func TestRunMCPScanRequiresTrivyOutput(t *testing.T) {
	stubMCPTrivy(t, "", nil)

	reconciler := &VersionReconciler{ScanOffline: true, TrivyCacheDir: t.TempDir()}
	if _, _, err := reconciler.runMCPScan(context.Background(), t.TempDir(), policy.Resolved{}); err == nil || !strings.Contains(err.Error(), "no output") {
		t.Fatalf("runMCPScan err = %v, want the empty trivy output error", err)
	}
}

func TestWriteMCPDescriptorValidatesBeforeWriting(t *testing.T) {
	if err := writeMCPDescriptor(&mcpVendored{tree: t.TempDir()}); err != nil {
		t.Fatalf("writeMCPDescriptor without a descriptor: %v", err)
	}

	invalid := &mcpVendored{tree: t.TempDir(), descriptor: &mcp.LaunchDescriptor{Transport: mcp.TransportStdio, Runtime: mcp.RuntimeNode}}
	if got := mcpFindingCode(writeMCPDescriptor(invalid)); got != mcpFindingInvalid {
		t.Fatalf("invalid descriptor finding = %q, want %q", got, mcpFindingInvalid)
	}

	if _, err := os.Stat(filepath.Join(invalid.tree, mcp.LaunchDescriptorFile)); !os.IsNotExist(err) {
		t.Fatalf("invalid descriptor was written: %v", err)
	}
}

func TestMCPFailureBlocksOnTerminalFinding(t *testing.T) {
	version := newMCPImageVersion("example-image")
	version.Name = "example-mcp-finding-1-0-0"
	reconciler := newMCPSyncTestClient(t, version)
	req := ctrl.Request{NamespacedName: k8stypes.NamespacedName{Name: version.Name, Namespace: "default"}}

	finding := newMCPFinding(mcpFindingInvalid, "bad source")
	if _, err := reconciler.mcpFailure(context.Background(), req, finding); err != nil {
		t.Fatalf("mcpFailure terminal finding err = %v, want nil so the Version is not retried", err)
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

const testTrivyClean = `{"SchemaVersion":2,"Results":[]}`

// stubMCPTrivy replaces runTrivy with a fixed output, or a failure when err is set.
func stubMCPTrivy(t *testing.T, output string, err error) {
	t.Helper()

	original := runTrivy
	t.Cleanup(func() { runTrivy = original })
	runTrivy = func(_ context.Context, _ ...string) ([]byte, error) {
		if err != nil {
			return nil, err
		}

		return []byte(output), nil
	}
}

// testNPMArchiveRecorder stands in for npm: install writes a lockfile and ci writes the installed package.
func testNPMArchiveRecorder(t *testing.T) {
	t.Helper()

	recordMCPCommands(t, func(dir string, args []string) error {
		switch args[0] {
		case "install":
			writeMCPFiles(t, dir, map[string]string{"package-lock.json": testNPMLock(nil)})
		case "ci":
			writeMCPFiles(t, dir, map[string]string{
				"node_modules/example-mcp/package.json":  `{"name":"example-mcp","version":"1.0.0","bin":{"example-mcp":"dist/index.js"}}`,
				"node_modules/example-mcp/dist/index.js": "export {}",
			})
		}

		return nil
	})
}

func newMCPNpmArchiveVersion(storageDir string) *opendepotv1alpha1.Version {
	name := "example-mcp"

	return &opendepotv1alpha1.Version{
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
				StorageConfig: &opendepotv1alpha1.StorageConfig{
					FileSystem: &opendepotv1alpha1.FileSystemConfig{DirectoryPath: &storageDir},
				},
			},
		},
	}
}

func testNPMResolver(t *testing.T) *fakeMCPResolver {
	t.Helper()

	return &fakeMCPResolver{
		npm: mcp.NPMVersion{Name: "example-mcp", Version: "1.0.0", Integrity: testNPMIntegrity},
		tarballs: map[string][]byte{
			"https://registry.npmjs.org/example-mcp/-/example-mcp-1.0.0.tgz": testTarball(t, map[string]string{"package/index.js": "export {}"}),
		},
	}
}

func TestReconcileMCPArchivePublishesNpmSource(t *testing.T) {
	setJevSigningKey(t)
	stubMCPTrivy(t, testTrivyClean, nil)
	testNPMArchiveRecorder(t)

	storageDir := t.TempDir()
	version := newMCPNpmArchiveVersion(storageDir)
	reconciler := newMCPSyncTestClient(t, version)
	reconciler.mcpClient = testNPMResolver(t)
	req := ctrl.Request{NamespacedName: k8stypes.NamespacedName{Name: version.Name, Namespace: "default"}}

	if _, err := reconciler.reconcileMCPArchive(context.Background(), req, version, version.Spec.MCPServerSourceRef); err != nil {
		t.Fatalf("reconcileMCPArchive: %v", err)
	}

	current := &opendepotv1alpha1.Version{}
	if err := reconciler.Get(context.Background(), req.NamespacedName, current); err != nil {
		t.Fatalf("get version: %v", err)
	}

	if !current.Status.Synced || current.Status.Checksum == nil || current.Status.ArchiveSizeBytes == nil {
		t.Fatalf("status = %+v, want synced with checksum and size", current.Status)
	}

	if current.Status.ShaSums == "" || current.Status.ShaSumsSignature == "" || current.Status.SigningKeyFingerprint == "" {
		t.Fatal("archive was not signed with SHA256SUMS")
	}

	if current.Status.SourceScan == nil {
		t.Fatal("source scan was not recorded")
	}

	meta := current.Status.MCPMetadata
	if meta == nil || !meta.Runnable || meta.Transport != mcp.TransportStdio || !strings.HasPrefix(meta.PackageDigest, "sha256:") {
		t.Fatalf("MCP metadata = %+v, want runnable stdio with a sha256 digest", meta)
	}

	entries, err := os.ReadDir(storageDir)
	if err != nil || len(entries) == 0 {
		t.Fatalf("archive not written to storage: %v", err)
	}
}

func TestReconcileMCPArchiveBlocksCriticalFinding(t *testing.T) {
	setJevSigningKey(t)
	stubMCPTrivy(t, testTrivyCritical, nil)
	testNPMArchiveRecorder(t)

	version := newMCPNpmArchiveVersion(t.TempDir())
	reconciler := newMCPSyncTestClient(t, version)
	reconciler.BlockOnCritical = true
	reconciler.mcpClient = testNPMResolver(t)
	req := ctrl.Request{NamespacedName: k8stypes.NamespacedName{Name: version.Name, Namespace: "default"}}

	_, err := reconciler.reconcileMCPArchive(context.Background(), req, version, version.Spec.MCPServerSourceRef)
	if err == nil || !strings.Contains(err.Error(), "CVE-2099-0001") {
		t.Fatalf("reconcileMCPArchive error = %v, want the blocking CVE", err)
	}

	current := &opendepotv1alpha1.Version{}
	if err := reconciler.Get(context.Background(), req.NamespacedName, current); err != nil {
		t.Fatalf("get version: %v", err)
	}

	if current.Status.Synced {
		t.Fatal("Version with a critical finding was synced")
	}
}

func TestReconcileMCPArchiveSkipsBlockedSourceCommit(t *testing.T) {
	setJevSigningKey(t)
	stubMCPTrivy(t, testTrivyCritical, nil)
	testNPMArchiveRecorder(t)

	version := newMCPNpmArchiveVersion(t.TempDir())
	version.Spec.SourceCommit = "abc123"
	reconciler := newMCPSyncTestClient(t, version)
	reconciler.mcpClient = testNPMResolver(t)
	req := ctrl.Request{NamespacedName: k8stypes.NamespacedName{Name: version.Name, Namespace: "default"}}

	if _, err := reconciler.reconcileMCPArchive(context.Background(), req, version, version.Spec.MCPServerSourceRef); err != nil {
		t.Fatalf("blocked source commit returned an error: %v", err)
	}
}

func TestReconcileMCPArchiveScanFailurePropagates(t *testing.T) {
	setJevSigningKey(t)
	stubMCPTrivy(t, "", errors.New("trivy unavailable"))
	testNPMArchiveRecorder(t)

	version := newMCPNpmArchiveVersion(t.TempDir())
	reconciler := newMCPSyncTestClient(t, version)
	reconciler.mcpClient = testNPMResolver(t)
	req := ctrl.Request{NamespacedName: k8stypes.NamespacedName{Name: version.Name, Namespace: "default"}}

	if _, err := reconciler.reconcileMCPArchive(context.Background(), req, version, version.Spec.MCPServerSourceRef); err == nil {
		t.Fatal("scan failure was not returned")
	}
}

func TestReconcileMCPArchiveRejectsOutOfRootStorage(t *testing.T) {
	setJevSigningKey(t)
	stubMCPTrivy(t, testTrivyClean, nil)
	testNPMArchiveRecorder(t)

	version := newMCPNpmArchiveVersion(t.TempDir())
	reconciler := newMCPSyncTestClient(t, version)
	reconciler.mcpClient = testNPMResolver(t)
	reconciler.FilesystemMountPath = t.TempDir()
	req := ctrl.Request{NamespacedName: k8stypes.NamespacedName{Name: version.Name, Namespace: "default"}}

	if _, err := reconciler.reconcileMCPArchive(context.Background(), req, version, version.Spec.MCPServerSourceRef); err == nil {
		t.Fatal("storage directory outside the mount path was accepted")
	}
}

func TestReconcileMCPArchiveSigningKeyMissingFails(t *testing.T) {
	t.Setenv(agentSigningKeyEnv, "")
	stubMCPTrivy(t, testTrivyClean, nil)
	testNPMArchiveRecorder(t)

	version := newMCPNpmArchiveVersion(t.TempDir())
	reconciler := newMCPSyncTestClient(t, version)
	reconciler.mcpClient = testNPMResolver(t)
	req := ctrl.Request{NamespacedName: k8stypes.NamespacedName{Name: version.Name, Namespace: "default"}}

	_, err := reconciler.reconcileMCPArchive(context.Background(), req, version, version.Spec.MCPServerSourceRef)
	if !errors.Is(err, errAgentSigningKeyNotConfigured) {
		t.Fatalf("reconcileMCPArchive error = %v, want errAgentSigningKeyNotConfigured", err)
	}

	current := &opendepotv1alpha1.Version{}
	if err := reconciler.Get(context.Background(), req.NamespacedName, current); err != nil {
		t.Fatalf("get version: %v", err)
	}

	if current.Status.Synced {
		t.Fatal("unsigned archive was synced")
	}
}

func TestReconcileMCPArchiveInvalidPackageMetadataFails(t *testing.T) {
	setJevSigningKey(t)
	stubMCPTrivy(t, testTrivyClean, nil)
	recordMCPCommands(t, func(dir string, args []string) error {
		switch args[0] {
		case "install":
			writeMCPFiles(t, dir, map[string]string{"package-lock.json": testNPMLock(nil)})
		case "ci":
			writeMCPFiles(t, dir, map[string]string{
				"node_modules/example-mcp/package.json":  "{not json",
				"node_modules/example-mcp/dist/index.js": "export {}",
			})
		}

		return nil
	})

	version := newMCPNpmArchiveVersion(t.TempDir())
	version.Spec.MCPServerSourceRef.Name = nil
	reconciler := newMCPSyncTestClient(t, version)
	reconciler.mcpClient = testNPMResolver(t)
	req := ctrl.Request{NamespacedName: k8stypes.NamespacedName{Name: version.Name, Namespace: "default"}}

	if _, err := reconciler.reconcileMCPArchive(context.Background(), req, version, version.Spec.MCPServerSourceRef); err != nil {
		t.Fatalf("invalid package metadata returned an error instead of a terminal finding: %v", err)
	}

	current := &opendepotv1alpha1.Version{}
	if err := reconciler.Get(context.Background(), req.NamespacedName, current); err != nil {
		t.Fatalf("get version: %v", err)
	}

	if current.Status.Synced {
		t.Fatal("Version with invalid package metadata was synced")
	}
}

func TestVendorMCPRejectsInvalidSources(t *testing.T) {
	version := &opendepotv1alpha1.Version{Spec: opendepotv1alpha1.VersionSpec{Version: "1.0.0"}}
	offListRegistry := "https://evil.example.com"

	tests := []struct {
		name     string
		cfg      *opendepotv1alpha1.MCPServerSourceConfig
		resolver *fakeMCPResolver
		want     string
	}{
		{
			name: "Npm source without npm block",
			cfg:  &opendepotv1alpha1.MCPServerSourceConfig{Source: opendepotv1alpha1.MCPServerSource{Type: mcpSourceNpm}},
			want: mcpFindingInvalid,
		},
		{
			name: "PyPI source without pypi block",
			cfg:  &opendepotv1alpha1.MCPServerSourceConfig{Source: opendepotv1alpha1.MCPServerSource{Type: mcpSourcePyPI}},
			want: mcpFindingInvalid,
		},
		{
			name: "unsupported source type",
			cfg:  &opendepotv1alpha1.MCPServerSourceConfig{Source: opendepotv1alpha1.MCPServerSource{Type: "Docker"}},
			want: mcpFindingInvalid,
		},
		{
			name: "Source without source block",
			cfg:  &opendepotv1alpha1.MCPServerSourceConfig{Source: opendepotv1alpha1.MCPServerSource{Type: mcpSourceRepo}},
			want: mcpFindingInvalid,
		},
		{
			name:     "Npm version not found",
			cfg:      &opendepotv1alpha1.MCPServerSourceConfig{Source: opendepotv1alpha1.MCPServerSource{Type: mcpSourceNpm, Npm: &opendepotv1alpha1.MCPNpmSource{Package: "example-mcp"}}},
			resolver: &fakeMCPResolver{err: mcp.ErrVersionNotFound},
			want:     mcpFindingInvalid,
		},
		{
			name:     "Npm version without integrity",
			cfg:      &opendepotv1alpha1.MCPServerSourceConfig{Source: opendepotv1alpha1.MCPServerSource{Type: mcpSourceNpm, Npm: &opendepotv1alpha1.MCPNpmSource{Package: "example-mcp"}}},
			resolver: &fakeMCPResolver{npm: mcp.NPMVersion{Version: "1.0.0"}},
			want:     mcpFindingInvalid,
		},
		{
			name: "Npm registry off the allow-list",
			cfg: &opendepotv1alpha1.MCPServerSourceConfig{Source: opendepotv1alpha1.MCPServerSource{Type: mcpSourceNpm, Npm: &opendepotv1alpha1.MCPNpmSource{
				Package:  "example-mcp",
				Registry: &offListRegistry,
			}}},
			resolver: &fakeMCPResolver{},
			want:     mcpFindingInvalid,
		},
		{
			name:     "Npm transient resolver error is not terminal",
			cfg:      &opendepotv1alpha1.MCPServerSourceConfig{Source: opendepotv1alpha1.MCPServerSource{Type: mcpSourceNpm, Npm: &opendepotv1alpha1.MCPNpmSource{Package: "example-mcp"}}},
			resolver: &fakeMCPResolver{err: errors.New("connection reset")},
			want:     "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			reconciler := &VersionReconciler{MCPAllowedNpmRegistries: []string{"registry.npmjs.org"}}
			if tc.resolver != nil {
				reconciler.mcpClient = tc.resolver
			}

			_, err := reconciler.vendorMCP(context.Background(), version, tc.cfg, mcp.TransportStdio, t.TempDir())
			if err == nil {
				t.Fatal("invalid source was vendored")
			}

			if got := mcpFindingCode(err); got != tc.want {
				t.Fatalf("finding = %q, want %q (err=%v)", got, tc.want, err)
			}
		})
	}
}
