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
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	"github.com/tonedefdev/opendepot/pkg/mcp"
)

const (
	// mcpDefaultNPMRegistry is the npm registry used when an npm source does not set registry.
	mcpDefaultNPMRegistry = "https://registry.npmjs.org"
	// mcpDefaultPyPIIndex is the PyPI index used when a PyPI source does not set index.
	mcpDefaultPyPIIndex = "https://pypi.org/simple"
	// mcpMaxPlatforms matches the MCPMetadata.Platforms bound in the CRD.
	mcpMaxPlatforms = 8

	mcpFindingNativeAddon = "mcp-native-addon"
	mcpFindingSdistOnly   = "mcp-sdist-only"
	mcpFindingInvalid     = "mcp-invalid"
)

// mcpNativeBuildDeps are packages whose install scripts compile or fetch native code. A package with an install
// script that depends on one of them is a native addon.
var mcpNativeBuildDeps = map[string]struct{}{
	"node-gyp":             {},
	"prebuild-install":     {},
	"bindings":             {},
	"node-addon-api":       {},
	"node-pre-gyp":         {},
	"@mapbox/node-pre-gyp": {},
	"node-gyp-build":       {},
	"nan":                  {},
}

// mcpResolver resolves exact package versions from an allow-listed registry. *mcp.Client implements it.
type mcpResolver interface {
	ResolveNPM(ctx context.Context, registry, pkgName, version string) (mcp.NPMVersion, error)
	ResolvePyPI(ctx context.Context, index, pkgName, version string, filter mcp.PyPIFilter) (mcp.PyPIRelease, error)
}

// mcpFindingError is a terminal finding that fails the MCPServer Version. Its code is one of the mcp-* finding codes.
type mcpFindingError struct {
	code   string
	detail string
}

func (e *mcpFindingError) Error() string {
	return e.code + ": " + e.detail
}

// newMCPFinding returns a terminal finding error with the given code and detail.
func newMCPFinding(code, format string, args ...any) error {
	return &mcpFindingError{code: code, detail: fmt.Sprintf(format, args...)}
}

// mcpVendored is a vendored MCP server tree ready to be packaged.
type mcpVendored struct {
	// tree is the directory that becomes the archive root.
	tree string
	// pkgDir is the directory that holds the server package, used for server.json and metadata reads.
	pkgDir string
	// descriptor is the launch descriptor relative to tree.
	descriptor *mcp.LaunchDescriptor
	// platforms lists the platform tags the tree was vendored for. It is empty for npm and source.
	platforms []string
}

// runMCPCommand runs an external tool used to vendor MCP packages. Tests replace it with a fake. The returned output
// is for diagnosis only and must not be copied into status or errors.
var runMCPCommand = func(ctx context.Context, dir string, env []string, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = env

	return cmd.CombinedOutput()
}

// mcpHostAllowed reports whether the URL uses https and its host is listed in allow.
func mcpHostAllowed(allow []string, raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return false
	}

	host := strings.ToLower(u.Host)
	for _, entry := range allow {
		if strings.ToLower(strings.TrimSpace(entry)) == host {
			return true
		}
	}

	return false
}

// mcpResolverFor returns the injected resolver or a client built from the allow-lists.
func (r *VersionReconciler) mcpResolverFor() (mcpResolver, error) {
	if r.mcpClient != nil {
		return r.mcpClient, nil
	}

	npmAllow, err := mcp.NewAllowList(r.MCPAllowedNpmRegistries)
	if err != nil {
		return nil, fmt.Errorf("mcp npm allow-list: %w", err)
	}

	pypiAllow, err := mcp.NewAllowList(r.MCPAllowedPyPIIndexes)
	if err != nil {
		return nil, fmt.Errorf("mcp pypi allow-list: %w", err)
	}

	return mcpCombinedResolver{npm: mcp.NewClient(npmAllow), pypi: mcp.NewClient(pypiAllow)}, nil
}

// mcpCombinedResolver routes npm and PyPI lookups to clients that enforce separate allow-lists.
type mcpCombinedResolver struct {
	npm  *mcp.Client
	pypi *mcp.Client
}

func (c mcpCombinedResolver) ResolveNPM(ctx context.Context, registry, pkgName, version string) (mcp.NPMVersion, error) {
	return c.npm.ResolveNPM(ctx, registry, pkgName, version)
}

func (c mcpCombinedResolver) ResolvePyPI(ctx context.Context, index, pkgName, version string, filter mcp.PyPIFilter) (mcp.PyPIRelease, error) {
	return c.pypi.ResolvePyPI(ctx, index, pkgName, version, filter)
}

// mcpResolveError maps resolver errors to terminal findings where the registry says the package cannot be used.
func mcpResolveError(err error) error {
	switch {
	case errors.Is(err, mcp.ErrNoCompatibleWheel):
		return newMCPFinding(mcpFindingSdistOnly, "no wheel matches the configured platforms; sdists are not fetched")
	case errors.Is(err, mcp.ErrVersionNotFound):
		return newMCPFinding(mcpFindingInvalid, "the requested version was not found in the registry")
	case errors.Is(err, mcp.ErrDenied):
		return newMCPFinding(mcpFindingInvalid, "the registry is not on the allow-list")
	}

	return fmt.Errorf("resolve package: %w", err)
}

// validateMCPSourcePath rejects source paths that are absolute or climb out of the repository.
func validateMCPSourcePath(p string) error {
	if p == "" {
		return nil
	}

	if strings.HasPrefix(p, "/") || filepath.IsAbs(p) || strings.HasPrefix(p, `\`) {
		return fmt.Errorf("source path %q must be relative to the repository root", p)
	}

	for _, segment := range strings.Split(filepath.ToSlash(p), "/") {
		if segment == ".." {
			return fmt.Errorf("source path %q must not contain '..'", p)
		}
	}

	return nil
}

// mcpMinimalEnv returns the environment for vendoring commands. Caches stay inside the scratch directory.
func mcpMinimalEnv(scratch string) []string {
	return []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + scratch,
		"npm_config_cache=" + filepath.Join(scratch, "npm-cache"),
		"PIP_CACHE_DIR=" + filepath.Join(scratch, "pip-cache"),
		"PIP_NO_CACHE_DIR=1",
		"PIP_DISABLE_PIP_VERSION_CHECK=1",
		"NPM_CONFIG_UPDATE_NOTIFIER=false",
		"CI=1",
	}
}

// vendorNPM vendors an npm MCP server into a scratch directory. The exact version and integrity come from the
// allow-listed registry, and npm verifies every tarball against the lockfile integrity before installing it.
func (r *VersionReconciler) vendorNPM(ctx context.Context, spec *opendepotv1alpha1.MCPNpmSource, version *opendepotv1alpha1.Version, transport string, entry *opendepotv1alpha1.MCPEntrypoint, scratch string) (*mcpVendored, error) {
	registry := mcpDefaultNPMRegistry
	if spec.Registry != nil && *spec.Registry != "" {
		registry = *spec.Registry
	}

	if !mcpHostAllowed(r.MCPAllowedNpmRegistries, registry) {
		return nil, newMCPFinding(mcpFindingInvalid, "npm registry is not on the allow-list")
	}

	resolver, err := r.mcpResolverFor()
	if err != nil {
		return nil, err
	}

	resolved, err := resolver.ResolveNPM(ctx, registry, spec.Package, version.Spec.Version)
	if err != nil {
		return nil, mcpResolveError(err)
	}

	if resolved.Integrity == "" {
		return nil, newMCPFinding(mcpFindingInvalid, "registry did not publish an integrity value for %s", spec.Package)
	}

	vendorDir := filepath.Join(scratch, "vendor")
	if err := os.MkdirAll(vendorDir, 0o750); err != nil {
		return nil, fmt.Errorf("create npm vendor directory: %w", err)
	}

	manifest := map[string]any{
		"name":    "opendepot-mcp-vendor",
		"version": "0.0.0",
		"private": true,
		"dependencies": map[string]string{
			spec.Package: resolved.Version,
		},
	}

	data, err := json.Marshal(manifest)
	if err != nil {
		return nil, fmt.Errorf("encode npm vendor manifest: %w", err)
	}

	if err := os.WriteFile(filepath.Join(vendorDir, "package.json"), data, 0o600); err != nil {
		return nil, fmt.Errorf("write npm vendor manifest: %w", err)
	}

	env := mcpMinimalEnv(scratch)
	lockArgs := []string{"install", "--package-lock-only", "--ignore-scripts", "--no-audit", "--no-fund", "--registry=" + registry}
	if _, err := runMCPCommand(ctx, vendorDir, env, "npm", lockArgs...); err != nil {
		return nil, fmt.Errorf("npm lockfile resolution failed: %w", err)
	}

	if err := checkNPMLockfile(filepath.Join(vendorDir, "package-lock.json"), spec.Package, resolved); err != nil {
		return nil, err
	}

	ciArgs := []string{"ci", "--omit=dev", "--ignore-scripts", "--no-audit", "--no-fund", "--registry=" + registry}
	if _, err := runMCPCommand(ctx, vendorDir, env, "npm", ciArgs...); err != nil {
		return nil, fmt.Errorf("npm ci failed: %w", err)
	}

	// npm creates .bin links that point outside the tree. The launch path does not need them.
	if err := os.RemoveAll(filepath.Join(vendorDir, "node_modules", ".bin")); err != nil {
		return nil, fmt.Errorf("remove npm bin links: %w", err)
	}

	if err := rejectMCPSymlinks(vendorDir); err != nil {
		return nil, err
	}

	if err := rejectNPMNativeOnDisk(vendorDir); err != nil {
		return nil, err
	}

	pkgDir := filepath.Join(vendorDir, "node_modules", filepath.FromSlash(spec.Package))
	rel, err := npmEntryPath(pkgDir, spec.Package, entry)
	if err != nil {
		return nil, err
	}

	command := "node"
	args := []string{rel}
	if entry != nil && entry.Command != "" {
		command = entry.Command
		args = append([]string(nil), entry.Args...)
	}

	return &mcpVendored{
		tree:   vendorDir,
		pkgDir: pkgDir,
		descriptor: &mcp.LaunchDescriptor{
			Command:   command,
			Args:      args,
			Transport: transport,
			Runtime:   mcp.RuntimeNode,
		},
	}, nil
}

// npmLockfile is the subset of package-lock.json that the vendoring checks read.
type npmLockfile struct {
	Packages map[string]npmLockEntry `json:"packages"`
}

type npmLockEntry struct {
	Version              string            `json:"version"`
	Resolved             string            `json:"resolved"`
	Integrity            string            `json:"integrity"`
	Link                 bool              `json:"link"`
	HasInstallScript     bool              `json:"hasInstallScript"`
	Optional             bool              `json:"optional"`
	OS                   []string          `json:"os"`
	CPU                  []string          `json:"cpu"`
	Dependencies         map[string]string `json:"dependencies"`
	OptionalDependencies map[string]string `json:"optionalDependencies"`
}

// checkNPMLockfile verifies that the lock pins the requested package to the resolved version and integrity, that
// every entry is fetched from an allow-listed host with an integrity value, and that no entry is a local link or a
// native addon.
func checkNPMLockfile(lockPath, pkgName string, resolved mcp.NPMVersion) error {
	data, err := os.ReadFile(lockPath)
	if err != nil {
		return fmt.Errorf("read npm lockfile: %w", err)
	}

	var lock npmLockfile
	if err := json.Unmarshal(data, &lock); err != nil {
		return newMCPFinding(mcpFindingInvalid, "npm lockfile is not valid JSON")
	}

	root, ok := lock.Packages["node_modules/"+pkgName]
	if !ok || root.Version != resolved.Version || root.Integrity != resolved.Integrity {
		return newMCPFinding(mcpFindingInvalid, "npm lockfile does not match the resolved version of %s", pkgName)
	}

	for key, entry := range lock.Packages {
		if key == "" {
			continue
		}

		name := mcpLockEntryName(key)
		if entry.Link {
			return newMCPFinding(mcpFindingInvalid, "npm lockfile links %s to a local path", name)
		}

		if entry.Integrity == "" {
			return newMCPFinding(mcpFindingInvalid, "npm lockfile entry %s has no integrity value", name)
		}

		if entry.Resolved != "" && !strings.HasPrefix(entry.Resolved, "https://") {
			return newMCPFinding(mcpFindingInvalid, "npm lockfile entry %s is not fetched over https", name)
		}

		if entry.HasInstallScript && mcpNativeInstall(name, entry) {
			return newMCPFinding(mcpFindingNativeAddon, "%s runs a native build during install", name)
		}

		if entry.Optional && (len(entry.OS) > 0 || len(entry.CPU) > 0) {
			return newMCPFinding(mcpFindingNativeAddon, "%s is a platform-constrained optional package", name)
		}
	}

	return nil
}

// mcpLockEntryName returns the package name for a lockfile key such as node_modules/@scope/name.
func mcpLockEntryName(key string) string {
	idx := strings.LastIndex(key, "node_modules/")
	if idx < 0 {
		return key
	}

	return key[idx+len("node_modules/"):]
}

// mcpNativeInstall reports whether a package with an install script builds native code. It does when the
// package is a native build tool itself or depends on one.
func mcpNativeInstall(name string, entry npmLockEntry) bool {
	if _, ok := mcpNativeBuildDeps[name]; ok {
		return true
	}

	for dep := range entry.Dependencies {
		if _, ok := mcpNativeBuildDeps[dep]; ok {
			return true
		}
	}

	for dep := range entry.OptionalDependencies {
		if _, ok := mcpNativeBuildDeps[dep]; ok {
			return true
		}
	}

	return false
}

// rejectNPMNativeOnDisk fails when the installed tree contains a native build file or a native addon binary.
func rejectNPMNativeOnDisk(root string) error {
	return filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			return nil
		}

		base := d.Name()
		if base == "binding.gyp" || strings.EqualFold(filepath.Ext(base), ".node") {
			return newMCPFinding(mcpFindingNativeAddon, "native addon file %s is present", relativeTo(root, p))
		}

		if base == "package.json" {
			data, readErr := os.ReadFile(p)
			if readErr != nil {
				return fmt.Errorf("read package manifest: %w", readErr)
			}

			var manifest struct {
				Gypfile bool `json:"gypfile"`
			}

			if json.Unmarshal(data, &manifest) == nil && manifest.Gypfile {
				return newMCPFinding(mcpFindingNativeAddon, "package %s builds with node-gyp", relativeTo(root, p))
			}
		}

		return nil
	})
}

// npmEntryPath returns the vendored path of the server entry point relative to the archive root. An explicit
// entrypoint wins. Otherwise the package bin, or main, is used.
func npmEntryPath(pkgDir, pkgName string, entry *opendepotv1alpha1.MCPEntrypoint) (string, error) {
	if entry != nil && entry.Command != "" {
		return "", nil
	}

	data, err := os.ReadFile(filepath.Join(pkgDir, "package.json"))
	if err != nil {
		return "", newMCPFinding(mcpFindingInvalid, "package %s has no package.json", pkgName)
	}

	var manifest struct {
		Bin  json.RawMessage `json:"bin"`
		Main string          `json:"main"`
	}

	if err := json.Unmarshal(data, &manifest); err != nil {
		return "", newMCPFinding(mcpFindingInvalid, "package %s has an invalid package.json", pkgName)
	}

	bins := map[string]string{}
	if len(manifest.Bin) > 0 {
		var single string
		if json.Unmarshal(manifest.Bin, &single) == nil {
			bins[path.Base(pkgName)] = single
		} else {
			_ = json.Unmarshal(manifest.Bin, &bins)
		}
	}

	target := ""
	switch {
	case len(bins) == 1:
		for _, v := range bins {
			target = v
		}
	case len(bins) > 1:
		target = bins[path.Base(pkgName)]
	}

	if target == "" {
		target = manifest.Main
	}

	if target == "" {
		return "", newMCPFinding(mcpFindingInvalid, "package %s declares no bin or main entry point", pkgName)
	}

	clean := path.Clean(strings.TrimPrefix(filepath.ToSlash(target), "./"))
	if path.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", newMCPFinding(mcpFindingInvalid, "package %s declares an entry point outside its directory", pkgName)
	}

	rel := filepath.ToSlash(filepath.Join("node_modules", filepath.FromSlash(pkgName), filepath.FromSlash(clean)))
	if _, err := os.Stat(filepath.Join(pkgDir, filepath.FromSlash(clean))); err != nil {
		return "", newMCPFinding(mcpFindingInvalid, "package %s entry point is missing from the vendored tree", pkgName)
	}

	return rel, nil
}

// vendorPyPI downloads wheel-only binaries for the configured platforms and installs them into a scratch tree.
// Source distributions are never fetched.
func (r *VersionReconciler) vendorPyPI(ctx context.Context, spec *opendepotv1alpha1.MCPPyPISource, version *opendepotv1alpha1.Version, transport string, entry *opendepotv1alpha1.MCPEntrypoint, scratch string) (*mcpVendored, error) {
	index := mcpDefaultPyPIIndex
	if spec.Index != nil && *spec.Index != "" {
		index = *spec.Index
	}

	if !mcpHostAllowed(r.MCPAllowedPyPIIndexes, index) {
		return nil, newMCPFinding(mcpFindingInvalid, "PyPI index is not on the allow-list")
	}

	platforms := spec.Platforms
	if len(platforms) == 0 {
		platforms = r.MCPPyPIDefaultPlatforms
	}

	if len(platforms) == 0 {
		return nil, newMCPFinding(mcpFindingInvalid, "no PyPI platforms are configured")
	}

	pythonVersion := ""
	if spec.PythonVersion != nil {
		pythonVersion = *spec.PythonVersion
	}

	resolver, err := r.mcpResolverFor()
	if err != nil {
		return nil, err
	}

	release, err := resolver.ResolvePyPI(ctx, index, spec.Package, version.Spec.Version, mcp.PyPIFilter{
		Platforms:     platforms,
		PythonVersion: pythonVersion,
	})
	if err != nil {
		return nil, mcpResolveError(err)
	}

	for _, wheel := range release.Wheels {
		if !mcpHostAllowed(r.MCPAllowedPyPIIndexes, wheel.URL) {
			return nil, newMCPFinding(mcpFindingInvalid, "wheel %s is hosted outside the allow-list", wheel.Filename)
		}
	}

	wheelsDir := filepath.Join(scratch, "wheels")
	if err := os.MkdirAll(wheelsDir, 0o750); err != nil {
		return nil, fmt.Errorf("create wheel directory: %w", err)
	}

	pin := spec.Package + "==" + release.Version
	env := mcpMinimalEnv(scratch)
	downloadArgs := []string{"download", "--only-binary=:all:", "--dest", wheelsDir, "--index-url", index, "--disable-pip-version-check", "--no-cache-dir"}
	downloadArgs = append(downloadArgs, mcpPlatformArgs(platforms, pythonVersion)...)
	downloadArgs = append(downloadArgs, pin)

	out, err := runMCPCommand(ctx, scratch, env, "pip3", downloadArgs...)
	if err != nil {
		if mcpSdistOnlyOutput(out) {
			return nil, newMCPFinding(mcpFindingSdistOnly, "%s has no wheel for the configured platforms; sdists are not fetched", spec.Package)
		}

		return nil, fmt.Errorf("pip download failed: %w", err)
	}

	if err := verifyPyPIWheels(wheelsDir, spec.Package, release); err != nil {
		return nil, err
	}

	tree := filepath.Join(scratch, "tree")
	installArgs := []string{"install", "--no-index", "--find-links", wheelsDir, "--target", tree, "--only-binary=:all:", "--no-cache-dir", "--disable-pip-version-check"}
	installArgs = append(installArgs, mcpPlatformArgs(platforms, pythonVersion)...)
	installArgs = append(installArgs, pin)

	if _, err := runMCPCommand(ctx, scratch, env, "pip3", installArgs...); err != nil {
		return nil, fmt.Errorf("pip install failed: %w", err)
	}

	if err := rejectMCPSymlinks(tree); err != nil {
		return nil, err
	}

	if entry == nil || entry.Command == "" {
		return nil, newMCPFinding(mcpFindingInvalid, "a PyPI server requires spec.entrypoint to launch")
	}

	matched := matchedPlatforms(release, platforms)
	if len(matched) > mcpMaxPlatforms {
		return nil, newMCPFinding(mcpFindingInvalid, "more than %d platforms matched", mcpMaxPlatforms)
	}

	return &mcpVendored{
		tree:      tree,
		pkgDir:    tree,
		platforms: matched,
		descriptor: &mcp.LaunchDescriptor{
			Command:   entry.Command,
			Args:      append([]string(nil), entry.Args...),
			Transport: transport,
			Runtime:   mcp.RuntimePython,
		},
	}, nil
}

// mcpPlatformArgs returns the pip flags that select wheels for the given platforms and Python version.
func mcpPlatformArgs(platforms []string, pythonVersion string) []string {
	var args []string
	for _, p := range platforms {
		args = append(args, "--platform", p)
	}

	if pythonVersion != "" {
		args = append(args, "--python-version", pythonVersion)
	}

	return args
}

// mcpSdistOnlyOutput reports whether pip output shows that no wheel matched.
func mcpSdistOnlyOutput(out []byte) bool {
	return bytes.Contains(out, []byte("from versions: none")) || bytes.Contains(out, []byte("No matching distribution"))
}

// verifyPyPIWheels checks the SHA-256 of every downloaded wheel that the index publishes for this release, and
// requires the package's own wheel to be present.
func verifyPyPIWheels(wheelsDir, pkgName string, release mcp.PyPIRelease) error {
	published := make(map[string]string, len(release.Wheels))
	for _, wheel := range release.Wheels {
		published[wheel.Filename] = strings.ToLower(wheel.SHA256)
	}

	entries, err := os.ReadDir(wheelsDir)
	if err != nil {
		return fmt.Errorf("read downloaded wheels: %w", err)
	}

	found := false
	for _, entry := range entries {
		want, ok := published[entry.Name()]
		if !ok {
			continue
		}

		sum, err := fileSHA256(filepath.Join(wheelsDir, entry.Name()))
		if err != nil {
			return err
		}

		if sum != want {
			return newMCPFinding(mcpFindingInvalid, "wheel %s does not match its published SHA-256", entry.Name())
		}

		if strings.HasPrefix(strings.ToLower(entry.Name()), strings.ReplaceAll(strings.ToLower(pkgName), "-", "_")+"-") {
			found = true
		}
	}

	if !found {
		return newMCPFinding(mcpFindingInvalid, "no verified wheel was downloaded for %s", pkgName)
	}

	return nil
}

// matchedPlatforms returns the configured platforms that at least one wheel of the release supports, sorted.
func matchedPlatforms(release mcp.PyPIRelease, platforms []string) []string {
	var out []string
	for _, p := range platforms {
		for _, wheel := range release.Wheels {
			if containsString(wheel.PlatformTags, p) {
				out = append(out, p)

				break
			}
		}
	}

	sort.Strings(out)

	return out
}

// rejectMCPSymlinks fails when the tree contains a symlink. Links could point outside the archive.
func rejectMCPSymlinks(root string) error {
	return filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.Type()&fs.ModeSymlink != 0 {
			return newMCPFinding(mcpFindingInvalid, "symlink %s is not allowed in an MCP archive", relativeTo(root, p))
		}

		return nil
	})
}

// fileSHA256 returns the lowercase hex SHA-256 of a file.
func fileSHA256(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", filepath.Base(p), err)
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("hash %s: %w", filepath.Base(p), err)
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// relativeTo returns p relative to root with slashes, falling back to the base name.
func relativeTo(root, p string) string {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return filepath.Base(p)
	}

	return filepath.ToSlash(rel)
}

// containsString reports whether values contains target.
func containsString(values []string, target string) bool {
	for _, v := range values {
		if v == target {
			return true
		}
	}

	return false
}
