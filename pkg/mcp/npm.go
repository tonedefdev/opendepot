package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strings"
)

// maxNPMPackageLength is the npm limit on a package name, including its scope.
const maxNPMPackageLength = 214

var npmPackagePattern = regexp.MustCompile(`^(@[a-z0-9-~][a-z0-9-._~]*/)?[a-z0-9-~][a-z0-9-._~]*$`)

// NPMVersion is the metadata of one published npm version that launch and vendoring need.
type NPMVersion struct {
	Name             string
	Version          string
	Tarball          string
	Integrity        string
	Shasum           string
	Bin              map[string]string
	Main             string
	OS               []string
	CPU              []string
	HasInstallScript bool
}

// npmPackument is the subset of a registry packument (GET <registry>/<package>) that this package reads.
type npmPackument struct {
	Name     string                   `json:"name"`
	DistTags map[string]string        `json:"dist-tags"`
	Versions map[string]npmVersionDoc `json:"versions"`
}

type npmVersionDoc struct {
	Version          string          `json:"version"`
	Bin              json.RawMessage `json:"bin"`
	Main             string          `json:"main"`
	OS               []string        `json:"os"`
	CPU              []string        `json:"cpu"`
	HasInstallScript bool            `json:"hasInstallScript"`
	Dist             struct {
		Tarball   string `json:"tarball"`
		Integrity string `json:"integrity"`
		Shasum    string `json:"shasum"`
	} `json:"dist"`
}

// fetchNPMPackument validates the package name and fetches its packument from the registry. The scoped name is
// escaped as one path segment (@scope%2Fname), which is how npm registries expect it.
func (c *Client) fetchNPMPackument(ctx context.Context, registry, pkgName string) (npmPackument, error) {
	if len(pkgName) > maxNPMPackageLength || !npmPackagePattern.MatchString(pkgName) {
		return npmPackument{}, fmt.Errorf("mcp: invalid npm package name %q", pkgName)
	}

	var doc npmPackument
	packumentURL := strings.TrimSuffix(registry, "/") + "/" + url.PathEscape(pkgName)
	if err := c.getJSON(ctx, packumentURL, &doc); err != nil {
		return npmPackument{}, err
	}

	if doc.Name != pkgName {
		return npmPackument{}, fmt.Errorf("mcp: packument name %q does not match %q", doc.Name, pkgName)
	}

	return doc, nil
}

// ResolveNPM returns the metadata of pkgName at version. An empty version or a dist-tag such as latest resolves
// through the packument's dist-tags. The tarball URL must also be on the allow-list.
func (c *Client) ResolveNPM(ctx context.Context, registry, pkgName, version string) (NPMVersion, error) {
	doc, err := c.fetchNPMPackument(ctx, registry, pkgName)
	if err != nil {
		return NPMVersion{}, err
	}

	resolved, err := doc.lookup(version)
	if err != nil {
		return NPMVersion{}, fmt.Errorf("%w: %s@%s", err, pkgName, version)
	}

	tarball, err := url.Parse(resolved.Dist.Tarball)
	if err != nil {
		return NPMVersion{}, fmt.Errorf("mcp: parse tarball URL: %w", err)
	}

	if err := checkURL(c.allow, tarball, c.plainHTTP); err != nil {
		return NPMVersion{}, err
	}

	if resolved.Dist.Integrity == "" && resolved.Dist.Shasum == "" {
		return NPMVersion{}, fmt.Errorf("mcp: npm %s@%s publishes no integrity or shasum", pkgName, resolved.Version)
	}

	bin, err := parseNPMBin(pkgName, resolved.Bin)
	if err != nil {
		return NPMVersion{}, fmt.Errorf("mcp: npm %s@%s: %w", pkgName, resolved.Version, err)
	}

	if resolved.Main != "" && !isLocalPath(resolved.Main) {
		return NPMVersion{}, fmt.Errorf("mcp: npm %s@%s main %q must be a local path",
			pkgName, resolved.Version, resolved.Main)
	}

	return NPMVersion{
		Name:             pkgName,
		Version:          resolved.Version,
		Tarball:          resolved.Dist.Tarball,
		Integrity:        resolved.Dist.Integrity,
		Shasum:           resolved.Dist.Shasum,
		Bin:              bin,
		Main:             resolved.Main,
		OS:               slices.Clone(resolved.OS),
		CPU:              slices.Clone(resolved.CPU),
		HasInstallScript: resolved.HasInstallScript,
	}, nil
}

// availableVersions returns the published versions of the packument in sorted order. Callers resolve constraints.
func (p npmPackument) availableVersions() []string {
	versions := make([]string, 0, len(p.Versions))
	for v := range p.Versions {
		versions = append(versions, v)
	}

	slices.Sort(versions)
	return versions
}

// NPMVersions returns the published versions of pkgName in sorted order. Callers resolve version constraints.
func (c *Client) NPMVersions(ctx context.Context, registry, pkgName string) ([]string, error) {
	doc, err := c.fetchNPMPackument(ctx, registry, pkgName)
	if err != nil {
		return nil, err
	}

	return doc.availableVersions(), nil
}

// lookup finds a version by exact name or by dist-tag. An empty request means the latest dist-tag.
func (p npmPackument) lookup(version string) (npmVersionDoc, error) {
	if version == "" {
		version = "latest"
	}

	if v, ok := p.Versions[version]; ok {
		return v, nil
	}

	if tagged, ok := p.DistTags[version]; ok {
		if v, ok := p.Versions[tagged]; ok {
			return v, nil
		}
	}

	return npmVersionDoc{}, ErrVersionNotFound
}

// parseNPMBin normalizes the bin field. A string names the command after the unscoped package name. Every path must
// be local to the package, so a launch cannot point outside the vendored tree.
func parseNPMBin(pkgName string, raw json.RawMessage) (map[string]string, error) {
	bin := map[string]string{}
	if len(raw) == 0 || string(raw) == "null" {
		return bin, nil
	}

	var single string
	if err := json.Unmarshal(raw, &single); err == nil {
		name := pkgName
		if i := strings.LastIndex(name, "/"); i >= 0 {
			name = name[i+1:]
		}

		bin[name] = single
	} else if err := json.Unmarshal(raw, &bin); err != nil {
		return nil, fmt.Errorf("bin must be a string or an object: %w", err)
	}

	for command, file := range bin {
		if command == "" || !isLocalPath(file) {
			return nil, fmt.Errorf("bin %q path %q must be a local path", command, file)
		}
	}

	return bin, nil
}

// isLocalPath reports whether an npm-declared path stays inside the package. npm paths are slash-separated, so this
// rejects absolute paths, backslashes, drive prefixes, and any path that escapes with "..".
func isLocalPath(p string) bool {
	if p == "" || strings.ContainsAny(p, `\:`) || strings.HasPrefix(p, "/") {
		return false
	}

	clean := path.Clean(p)
	return clean != "." && clean != ".." && !strings.HasPrefix(clean, "../")
}

// ErrVersionNotFound is returned when a registry has no version that matches the request.
var ErrVersionNotFound = errors.New("mcp: version not found")
