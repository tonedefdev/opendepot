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
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/tonedefdev/opendepot/pkg/mcp"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
)

// fakeMCPResolver returns canned registry answers so vendoring tests never reach a registry.
type fakeMCPResolver struct {
	npm      mcp.NPMVersion
	pypi     mcp.PyPIRelease
	tarballs map[string][]byte
	err      error
}

func (f *fakeMCPResolver) ResolveNPM(_ context.Context, _, _, _ string) (mcp.NPMVersion, error) {
	return f.npm, f.err
}

func (f *fakeMCPResolver) ResolvePyPI(_ context.Context, _, _, _ string, _ mcp.PyPIFilter) (mcp.PyPIRelease, error) {
	return f.pypi, f.err
}

func (f *fakeMCPResolver) FetchTarball(_ context.Context, rawURL string) ([]byte, error) {
	body, ok := f.tarballs[rawURL]
	if !ok {
		return nil, errors.New("unexpected tarball request")
	}

	return body, nil
}

// testAllowList returns an allow-list of the given entries.
func testAllowList(t *testing.T, entries ...string) *mcp.AllowList {
	t.Helper()

	al, err := mcp.NewAllowList(entries)
	if err != nil {
		t.Fatalf("NewAllowList(%v) error = %v", entries, err)
	}

	return al
}

// testTarball returns a gzip-compressed tar archive that holds the given files.
func testTarball(t *testing.T, files map[string]string) []byte {
	t.Helper()

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, content := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatalf("write tar header %s: %v", name, err)
		}

		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatalf("write tar entry %s: %v", name, err)
		}
	}

	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}

	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}

	return buf.Bytes()
}

// recordMCPCommands replaces runMCPCommand. Each call's arguments are recorded, and onRun runs in place of the tool so
// a test can write the files that the next vendoring step expects.
func recordMCPCommands(t *testing.T, onRun func(dir string, args []string) error) *[][]string {
	t.Helper()

	var calls [][]string
	original := runMCPCommand
	t.Cleanup(func() { runMCPCommand = original })
	runMCPCommand = func(_ context.Context, dir string, _ []string, _ string, args ...string) ([]byte, error) {
		calls = append(calls, append([]string(nil), args...))
		if err := onRun(dir, args); err != nil {
			return nil, err
		}

		return nil, nil
	}

	return &calls
}

// mcpFindingCode returns the finding code of err, or an empty string when err is not a terminal finding.
func mcpFindingCode(err error) string {
	var finding *mcpFindingError
	if errors.As(err, &finding) {
		return finding.code
	}

	return ""
}

// writeMCPFiles writes the given relative path to content values under root.
func writeMCPFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()

	for name, content := range files {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatalf("mkdir: %v", err)
		}

		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
}

const testNPMIntegrity = "sha512-AAAA"

func testNPMLock(extra map[string]npmLockEntry) string {
	entries := map[string]npmLockEntry{
		"node_modules/example-mcp": {Version: "1.0.0", Resolved: "https://registry.npmjs.org/example-mcp/-/example-mcp-1.0.0.tgz", Integrity: testNPMIntegrity},
	}

	for k, v := range extra {
		entries[k] = v
	}

	var b strings.Builder
	b.WriteString(`{"packages":{`)
	first := true
	for k, v := range entries {
		if !first {
			b.WriteString(",")
		}

		first = false
		b.WriteString(`"` + k + `":{"version":"` + v.Version + `","resolved":"` + v.Resolved + `","integrity":"` + v.Integrity + `"`)
		if v.HasInstallScript {
			b.WriteString(`,"hasInstallScript":true`)
		}

		if v.Optional {
			b.WriteString(`,"optional":true`)
		}

		if len(v.OS) > 0 {
			b.WriteString(`,"os":["` + v.OS[0] + `"]`)
		}

		if len(v.Dependencies) > 0 {
			b.WriteString(`,"dependencies":{`)
			i := 0
			for dep, ver := range v.Dependencies {
				if i > 0 {
					b.WriteString(",")
				}

				b.WriteString(`"` + dep + `":"` + ver + `"`)
				i++
			}

			b.WriteString("}")
		}

		b.WriteString("}")
	}

	b.WriteString("}}")

	return b.String()
}

func TestCheckNPMLockfileClean(t *testing.T) {
	dir := t.TempDir()
	lock := filepath.Join(dir, "package-lock.json")
	writeMCPFiles(t, dir, map[string]string{"package-lock.json": testNPMLock(nil)})

	allow := testAllowList(t, "registry.npmjs.org")
	err := checkNPMLockfile(lock, "example-mcp", mcp.NPMVersion{Version: "1.0.0", Integrity: testNPMIntegrity}, allow)
	if err != nil {
		t.Fatalf("clean lockfile rejected: %v", err)
	}
}

func TestCheckNPMLockfileNativeAddonDetection(t *testing.T) {
	tests := []struct {
		name  string
		extra map[string]npmLockEntry
		want  string
	}{
		{
			name: "install script with node-gyp dependency",
			extra: map[string]npmLockEntry{
				"node_modules/native-thing": {Version: "2.0.0", Resolved: "https://registry.npmjs.org/n.tgz", Integrity: "sha512-BBBB", HasInstallScript: true, Dependencies: map[string]string{"node-gyp": "^9"}},
			},
			want: mcpFindingNativeAddon,
		},
		{
			name: "install script that is itself a native build tool",
			extra: map[string]npmLockEntry{
				"node_modules/prebuild-install": {Version: "7.0.0", Resolved: "https://registry.npmjs.org/p.tgz", Integrity: "sha512-CCCC", HasInstallScript: true},
			},
			want: mcpFindingNativeAddon,
		},
		{
			name: "platform-constrained optional package",
			extra: map[string]npmLockEntry{
				"node_modules/@scope/linux-x64": {Version: "1.0.0", Resolved: "https://registry.npmjs.org/l.tgz", Integrity: "sha512-DDDD", Optional: true, OS: []string{"linux"}},
			},
			want: mcpFindingNativeAddon,
		},
		{
			name: "install script without native deps is allowed",
			extra: map[string]npmLockEntry{
				"node_modules/husky": {Version: "9.0.0", Resolved: "https://registry.npmjs.org/h.tgz", Integrity: "sha512-EEEE", HasInstallScript: true},
			},
		},
		{
			name: "entry without integrity",
			extra: map[string]npmLockEntry{
				"node_modules/no-sri": {Version: "1.0.0", Resolved: "https://registry.npmjs.org/x.tgz"},
			},
			want: mcpFindingInvalid,
		},
		{
			name: "entry fetched over http",
			extra: map[string]npmLockEntry{
				"node_modules/plain": {Version: "1.0.0", Resolved: "http://registry.npmjs.org/x.tgz", Integrity: "sha512-FFFF"},
			},
			want: mcpFindingInvalid,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			lock := filepath.Join(dir, "package-lock.json")
			writeMCPFiles(t, dir, map[string]string{"package-lock.json": testNPMLock(tc.extra)})

			allow := testAllowList(t, "registry.npmjs.org")
			err := checkNPMLockfile(lock, "example-mcp", mcp.NPMVersion{Version: "1.0.0", Integrity: testNPMIntegrity}, allow)
			if got := mcpFindingCode(err); got != tc.want {
				t.Fatalf("finding code = %q, want %q (err=%v)", got, tc.want, err)
			}
		})
	}
}

func TestCheckNPMLockfileRootMismatch(t *testing.T) {
	dir := t.TempDir()
	lock := filepath.Join(dir, "package-lock.json")
	writeMCPFiles(t, dir, map[string]string{"package-lock.json": testNPMLock(nil)})

	allow := testAllowList(t, "registry.npmjs.org")
	err := checkNPMLockfile(lock, "example-mcp", mcp.NPMVersion{Version: "2.0.0", Integrity: testNPMIntegrity}, allow)
	if got := mcpFindingCode(err); got != mcpFindingInvalid {
		t.Fatalf("finding code = %q, want %q (err=%v)", got, mcpFindingInvalid, err)
	}
}

func TestRejectNPMNativeOnDisk(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{name: "binding.gyp present", files: map[string]string{"binding.gyp": "{}"}, want: mcpFindingNativeAddon},
		{name: "compiled .node addon present", files: map[string]string{"build/addon.node": "ELF"}, want: mcpFindingNativeAddon},
		{name: "gypfile manifest", files: map[string]string{"node_modules/x/package.json": `{"gypfile":true}`}, want: mcpFindingNativeAddon},
		{name: "plain JavaScript tree", files: map[string]string{"dist/index.js": "export {}", "package.json": `{"main":"dist/index.js"}`}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeMCPFiles(t, dir, tc.files)

			err := rejectNPMNativeOnDisk(dir)
			if got := mcpFindingCode(err); got != tc.want {
				t.Fatalf("finding code = %q, want %q (err=%v)", got, tc.want, err)
			}
		})
	}
}

func TestRejectMCPSymlinks(t *testing.T) {
	dir := t.TempDir()
	writeMCPFiles(t, dir, map[string]string{"index.js": "export {}"})

	if err := rejectMCPSymlinks(dir); err != nil {
		t.Fatalf("tree without links rejected: %v", err)
	}

	if err := os.Symlink("/etc/passwd", filepath.Join(dir, "escape")); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	err := rejectMCPSymlinks(dir)
	if got := mcpFindingCode(err); got != mcpFindingInvalid {
		t.Fatalf("finding code = %q, want %q (err=%v)", got, mcpFindingInvalid, err)
	}
}

func TestValidateMCPSourcePath(t *testing.T) {
	tests := []struct {
		path    string
		wantErr bool
	}{
		{path: ""},
		{path: "servers/weather"},
		{path: "servers/../../etc", wantErr: true},
		{path: "..", wantErr: true},
		{path: "/etc", wantErr: true},
		{path: `\windows`, wantErr: true},
	}

	for _, tc := range tests {
		err := validateMCPSourcePath(tc.path)
		if (err != nil) != tc.wantErr {
			t.Fatalf("validateMCPSourcePath(%q) error = %v, wantErr %t", tc.path, err, tc.wantErr)
		}
	}
}

func TestVerifyPyPIWheels(t *testing.T) {
	dir := t.TempDir()
	wheel := "example_mcp-1.0.0-py3-none-any.whl"
	writeMCPFiles(t, dir, map[string]string{wheel: "wheel-bytes"})

	sum, err := fileSHA256(filepath.Join(dir, wheel))
	if err != nil {
		t.Fatalf("hash: %v", err)
	}

	release := mcp.PyPIRelease{Version: "1.0.0", Wheels: []mcp.PyPIWheel{{Filename: wheel, SHA256: sum}}}
	if err := verifyPyPIWheels(dir, "example-mcp", release); err != nil {
		t.Fatalf("matching wheel rejected: %v", err)
	}

	release.Wheels[0].SHA256 = strings.Repeat("0", 64)
	if got := mcpFindingCode(verifyPyPIWheels(dir, "example-mcp", release)); got != mcpFindingInvalid {
		t.Fatalf("digest mismatch finding = %q, want %q", got, mcpFindingInvalid)
	}

	empty := t.TempDir()
	if got := mcpFindingCode(verifyPyPIWheels(empty, "example-mcp", release)); got != mcpFindingInvalid {
		t.Fatalf("missing wheel finding = %q, want %q", got, mcpFindingInvalid)
	}
}

func TestMCPSdistOnlyOutput(t *testing.T) {
	if !mcpSdistOnlyOutput([]byte("ERROR: Could not find a version that satisfies the requirement x (from versions: none)")) {
		t.Fatal("'from versions: none' not detected as sdist-only")
	}

	if mcpSdistOnlyOutput([]byte("Successfully downloaded example_mcp-1.0.0-py3-none-any.whl")) {
		t.Fatal("successful download detected as sdist-only")
	}
}

func TestVendorPyPISdistOnlyFails(t *testing.T) {
	index := "https://pypi.org/simple"
	reconciler := &VersionReconciler{
		MCPAllowedPyPIIndexes:   []string{"pypi.org", "files.pythonhosted.org"},
		MCPPyPIDefaultPlatforms: []string{"manylinux_2_28_x86_64"},
		mcpClient: &fakeMCPResolver{pypi: mcp.PyPIRelease{
			Version: "1.0.0",
			Wheels:  []mcp.PyPIWheel{{Filename: "example_mcp-1.0.0-py3-none-any.whl", URL: "https://files.pythonhosted.org/packages/example_mcp-1.0.0-py3-none-any.whl"}},
		}},
	}

	original := runMCPCommand
	t.Cleanup(func() { runMCPCommand = original })
	runMCPCommand = func(_ context.Context, _ string, _ []string, _ string, _ ...string) ([]byte, error) {
		return []byte("ERROR: Could not find a version that satisfies the requirement example-mcp==1.0.0 (from versions: none)"), errors.New("exit status 1")
	}

	version := &opendepotv1alpha1.Version{Spec: opendepotv1alpha1.VersionSpec{Version: "1.0.0"}}
	spec := &opendepotv1alpha1.MCPPyPISource{Package: "example-mcp", Index: &index}
	entry := &opendepotv1alpha1.MCPEntrypoint{Command: "python3", Args: []string{"-m", "example_mcp"}}

	_, err := reconciler.vendorPyPI(context.Background(), spec, version, mcp.TransportStdio, entry, t.TempDir())
	if got := mcpFindingCode(err); got != mcpFindingSdistOnly {
		t.Fatalf("finding code = %q, want %q (err=%v)", got, mcpFindingSdistOnly, err)
	}
}

func TestVendorPyPIRejectsOffListIndex(t *testing.T) {
	index := "https://evil.example.com/simple"
	reconciler := &VersionReconciler{
		MCPAllowedPyPIIndexes: []string{"pypi.org"},
		mcpClient:             &fakeMCPResolver{},
	}

	version := &opendepotv1alpha1.Version{Spec: opendepotv1alpha1.VersionSpec{Version: "1.0.0"}}
	spec := &opendepotv1alpha1.MCPPyPISource{Package: "example-mcp", Index: &index}

	_, err := reconciler.vendorPyPI(context.Background(), spec, version, mcp.TransportStdio, nil, t.TempDir())
	if got := mcpFindingCode(err); got != mcpFindingInvalid {
		t.Fatalf("finding code = %q, want %q (err=%v)", got, mcpFindingInvalid, err)
	}
}

func TestVendorNPMRejectsOffListRegistry(t *testing.T) {
	registry := "https://evil.example.com"
	reconciler := &VersionReconciler{
		MCPAllowedNpmRegistries: []string{"registry.npmjs.org"},
		mcpClient:               &fakeMCPResolver{},
	}

	version := &opendepotv1alpha1.Version{Spec: opendepotv1alpha1.VersionSpec{Version: "1.0.0"}}
	spec := &opendepotv1alpha1.MCPNpmSource{Package: "example-mcp", Registry: &registry}

	_, err := reconciler.vendorNPM(context.Background(), spec, version, mcp.TransportStdio, nil, t.TempDir())
	if got := mcpFindingCode(err); got != mcpFindingInvalid {
		t.Fatalf("finding code = %q, want %q (err=%v)", got, mcpFindingInvalid, err)
	}
}

func TestCheckNPMLockfileRejectsDisallowedHost(t *testing.T) {
	allow := testAllowList(t, "registry.npmjs.org")
	dir := t.TempDir()
	lock := filepath.Join(dir, "package-lock.json")
	writeMCPFiles(t, dir, map[string]string{"package-lock.json": testNPMLock(map[string]npmLockEntry{
		"node_modules/transitive": {Version: "1.0.0", Resolved: "https://evil.example.com/t.tgz", Integrity: "sha512-GGGG"},
	})})

	err := checkNPMLockfile(lock, "example-mcp", mcp.NPMVersion{Version: "1.0.0", Integrity: testNPMIntegrity}, allow)
	if got := mcpFindingCode(err); got != mcpFindingInvalid {
		t.Fatalf("disallowed host finding = %q, want %q (err=%v)", got, mcpFindingInvalid, err)
	}
}

func TestCheckNPMLockfileAcceptsAllowedHost(t *testing.T) {
	allow := testAllowList(t, "registry.npmjs.org", "mirror.example.com")
	dir := t.TempDir()
	lock := filepath.Join(dir, "package-lock.json")
	writeMCPFiles(t, dir, map[string]string{"package-lock.json": testNPMLock(map[string]npmLockEntry{
		"node_modules/transitive": {Version: "1.0.0", Resolved: "https://mirror.example.com/t.tgz", Integrity: "sha512-HHHH"},
	})})

	if err := checkNPMLockfile(lock, "example-mcp", mcp.NPMVersion{Version: "1.0.0", Integrity: testNPMIntegrity}, allow); err != nil {
		t.Fatalf("allow-listed host rejected: %v", err)
	}
}

func TestNPMArgsReplaceRegistryHost(t *testing.T) {
	calls := recordMCPCommands(t, func(dir string, args []string) error {
		if args[0] == "install" {
			writeMCPFiles(t, dir, map[string]string{"package-lock.json": testNPMLock(nil)})
		}

		return nil
	})

	reconciler := &VersionReconciler{
		MCPAllowedNpmRegistries: []string{"registry.npmjs.org"},
		mcpClient: &fakeMCPResolver{
			npm:      mcp.NPMVersion{Version: "1.0.0", Integrity: testNPMIntegrity, Tarball: "https://registry.npmjs.org/example-mcp/-/example-mcp-1.0.0.tgz"},
			tarballs: map[string][]byte{"https://registry.npmjs.org/example-mcp/-/example-mcp-1.0.0.tgz": testTarball(t, map[string]string{"index.js": "export {}"})},
		},
	}

	version := &opendepotv1alpha1.Version{Spec: opendepotv1alpha1.VersionSpec{Version: "1.0.0"}}
	spec := &opendepotv1alpha1.MCPNpmSource{Package: "example-mcp"}

	_, _ = reconciler.vendorNPM(context.Background(), spec, version, mcp.TransportStdio, nil, t.TempDir())

	var sawInstall, sawCI bool
	for _, args := range *calls {
		if !slices.Contains(args, "--replace-registry-host=always") {
			continue
		}

		switch args[0] {
		case "install":
			sawInstall = true
		case "ci":
			sawCI = true
		}
	}

	if !sawInstall || !sawCI {
		t.Fatalf("--replace-registry-host=always missing (install=%t ci=%t) calls=%v", sawInstall, sawCI, *calls)
	}
}

func TestPyPIArgsIncludeNoDeps(t *testing.T) {
	index := "https://pypi.org/simple"
	wheel := "example_mcp-1.0.0-py3-none-any.whl"
	sum := sha256.Sum256([]byte("wheel-bytes"))
	release := mcp.PyPIRelease{Version: "1.0.0", Wheels: []mcp.PyPIWheel{{
		Filename: wheel,
		URL:      "https://files.pythonhosted.org/packages/" + wheel,
		SHA256:   hex.EncodeToString(sum[:]),
	}}}

	calls := recordMCPCommands(t, func(_ string, args []string) error {
		if args[0] == "download" {
			writeMCPFiles(t, argValue(args, "--dest"), map[string]string{wheel: "wheel-bytes"})
		}

		return nil
	})

	reconciler := &VersionReconciler{
		MCPAllowedPyPIIndexes:   []string{"pypi.org", "files.pythonhosted.org"},
		MCPPyPIDefaultPlatforms: []string{"manylinux_2_28_x86_64"},
		mcpClient:               &fakeMCPResolver{pypi: release},
	}

	version := &opendepotv1alpha1.Version{Spec: opendepotv1alpha1.VersionSpec{Version: "1.0.0"}}
	spec := &opendepotv1alpha1.MCPPyPISource{Package: "example-mcp", Index: &index}
	entry := &opendepotv1alpha1.MCPEntrypoint{Command: "python3", Args: []string{"-m", "example_mcp"}}

	_, _ = reconciler.vendorPyPI(context.Background(), spec, version, mcp.TransportStdio, entry, t.TempDir())

	var sawDownload, sawInstall bool
	for _, args := range *calls {
		if !slices.Contains(args, "--no-deps") {
			continue
		}

		switch args[0] {
		case "download":
			sawDownload = true
		case "install":
			sawInstall = true
		}
	}

	if !sawDownload || !sawInstall {
		t.Fatalf("--no-deps missing (download=%t install=%t) calls=%v", sawDownload, sawInstall, *calls)
	}
}

func TestVerifyPyPIWheelsRejectsUnverifiedFile(t *testing.T) {
	dir := t.TempDir()
	wheel := "example_mcp-1.0.0-py3-none-any.whl"
	writeMCPFiles(t, dir, map[string]string{wheel: "wheel-bytes", "extra_dep-2.0.0-py3-none-any.whl": "smuggled"})

	sum, err := fileSHA256(filepath.Join(dir, wheel))
	if err != nil {
		t.Fatalf("hash: %v", err)
	}

	release := mcp.PyPIRelease{Version: "1.0.0", Wheels: []mcp.PyPIWheel{{Filename: wheel, SHA256: sum}}}
	if got := mcpFindingCode(verifyPyPIWheels(dir, "example-mcp", release)); got != mcpFindingInvalid {
		t.Fatalf("unverified wheel finding = %q, want %q", got, mcpFindingInvalid)
	}
}

func TestCheckNPMTarballsRejectsNativeAddon(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{name: "compiled .node entry", files: map[string]string{"package/build/Release/addon.node": "ELF"}, want: mcpFindingNativeAddon},
		{name: "binding.gyp entry", files: map[string]string{"package/binding.gyp": "{}"}, want: mcpFindingNativeAddon},
		{name: "plain JavaScript", files: map[string]string{"package/index.js": "export {}"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tarURL := "https://registry.npmjs.org/transitive/-/transitive-1.0.0.tgz"
			rootURL := "https://registry.npmjs.org/example-mcp/-/example-mcp-1.0.0.tgz"
			dir := t.TempDir()
			lock := filepath.Join(dir, "package-lock.json")
			writeMCPFiles(t, dir, map[string]string{"package-lock.json": testNPMLock(map[string]npmLockEntry{
				"node_modules/transitive": {Version: "1.0.0", Resolved: tarURL, Integrity: "sha512-JJJJ"},
			})})

			resolver := &fakeMCPResolver{tarballs: map[string][]byte{
				rootURL: testTarball(t, map[string]string{"package/index.js": "export {}"}),
				tarURL:  testTarball(t, tc.files),
			}}
			err := checkNPMTarballs(context.Background(), resolver, lock)
			if got := mcpFindingCode(err); got != tc.want {
				t.Fatalf("finding code = %q, want %q (err=%v)", got, tc.want, err)
			}
		})
	}
}
