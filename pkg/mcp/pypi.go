package mcp

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// ErrNoCompatibleWheel is returned when a release has no wheel for the requested platforms and Python version. A
// release with only sdists is reported here too, because sdists are never fetched.
var ErrNoCompatibleWheel = errors.New("mcp: no compatible wheel")

var (
	pypiNamePattern      = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9._-]*[A-Za-z0-9])?$`)
	pypiSeparators       = regexp.MustCompile(`[-_.]+`)
	platformTagPattern   = regexp.MustCompile(`^[A-Za-z0-9_]+$`)
	pythonVersionRegexp  = regexp.MustCompile(`^3\.[0-9]+$`)
	wheelFilenamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+!-]*\.whl$`)
)

// PyPIFilter selects wheels. Empty Platforms accepts every platform tag, and an empty PythonVersion accepts every
// Python tag. A wheel tagged any is compatible with every platform.
type PyPIFilter struct {
	Platforms     []string
	PythonVersion string
}

// PyPIWheel is one compatible binary wheel of a release. SHA256 is the digest that PyPI publishes for the file.
type PyPIWheel struct {
	Filename     string
	URL          string
	SHA256       string
	PythonTags   []string
	ABITags      []string
	PlatformTags []string
}

// PyPIRelease is the set of compatible wheels for one version of a project.
type PyPIRelease struct {
	Name    string
	Version string
	Wheels  []PyPIWheel
}

// pypiProject is the subset of the PyPI JSON API (<index>/pypi/<name>/json) that this package reads.
type pypiProject struct {
	Info struct {
		Version string `json:"version"`
	} `json:"info"`
	Releases map[string][]pypiFile `json:"releases"`
}

type pypiFile struct {
	Filename    string `json:"filename"`
	URL         string `json:"url"`
	PackageType string `json:"packagetype"`
	Yanked      bool   `json:"yanked"`
	Digests     struct {
		SHA256 string `json:"sha256"`
	} `json:"digests"`
}

// ResolvePyPI returns the compatible wheels of pkgName at version. An empty version means the project's current
// version. Sdists are skipped, and a release with no compatible wheel returns ErrNoCompatibleWheel.
func (c *Client) ResolvePyPI(
	ctx context.Context, index, pkgName, version string, filter PyPIFilter,
) (PyPIRelease, error) {
	if !pypiNamePattern.MatchString(pkgName) {
		return PyPIRelease{}, fmt.Errorf("mcp: invalid PyPI package name %q", pkgName)
	}

	if err := validateFilter(filter); err != nil {
		return PyPIRelease{}, err
	}

	name := normalizePyPIName(pkgName)
	jsonURL, err := projectJSONURL(index, name)
	if err != nil {
		return PyPIRelease{}, err
	}

	var project pypiProject
	if err := c.getJSON(ctx, jsonURL, &project); err != nil {
		return PyPIRelease{}, err
	}

	if version == "" {
		version = project.Info.Version
	}

	files, ok := project.Releases[version]
	if !ok {
		return PyPIRelease{}, fmt.Errorf("%w: %s==%s", ErrVersionNotFound, pkgName, version)
	}

	release := PyPIRelease{Name: name, Version: version}
	for _, file := range files {
		if file.PackageType != "bdist_wheel" || file.Yanked {
			continue
		}

		wheel, err := parseWheelName(file.Filename)
		if err != nil {
			return PyPIRelease{}, err
		}

		if normalizePyPIName(wheel.name) != name {
			return PyPIRelease{}, fmt.Errorf("mcp: wheel %q does not belong to project %q", file.Filename, name)
		}

		if !wheelCompatible(wheel, filter) {
			continue
		}

		if !sha256Pattern.MatchString(file.Digests.SHA256) {
			return PyPIRelease{}, fmt.Errorf("mcp: wheel %q publishes no sha256 digest", file.Filename)
		}

		fileURL, err := url.Parse(file.URL)
		if err != nil {
			return PyPIRelease{}, fmt.Errorf("mcp: parse wheel URL: %w", err)
		}

		if err := checkURL(c.allow, fileURL, c.plainHTTP); err != nil {
			return PyPIRelease{}, err
		}

		release.Wheels = append(release.Wheels, PyPIWheel{
			Filename:     file.Filename,
			URL:          file.URL,
			SHA256:       file.Digests.SHA256,
			PythonTags:   wheel.python,
			ABITags:      wheel.abi,
			PlatformTags: wheel.platform,
		})
	}

	if len(release.Wheels) == 0 {
		return PyPIRelease{}, fmt.Errorf("%w: %s==%s", ErrNoCompatibleWheel, pkgName, version)
	}

	slices.SortFunc(release.Wheels, func(a, b PyPIWheel) int { return strings.Compare(a.Filename, b.Filename) })
	return release, nil
}

// normalizePyPIName applies the PEP 503 normalization: lowercase, with runs of -, _, and . collapsed to one -.
// PyPIVersions lists the versions of pkgName that have at least one compatible, non-yanked wheel. Sdist-only
// versions are skipped, and the result is sorted.
func (c *Client) PyPIVersions(ctx context.Context, index, pkgName string, filter PyPIFilter) ([]string, error) {
	if !pypiNamePattern.MatchString(pkgName) {
		return nil, fmt.Errorf("mcp: invalid PyPI package name %q", pkgName)
	}

	if err := validateFilter(filter); err != nil {
		return nil, err
	}

	jsonURL, err := projectJSONURL(index, normalizePyPIName(pkgName))
	if err != nil {
		return nil, err
	}

	var project pypiProject
	if err := c.getJSON(ctx, jsonURL, &project); err != nil {
		return nil, err
	}

	var versions []string
	for version, files := range project.Releases {
		if hasCompatibleWheel(files, filter) {
			versions = append(versions, version)
		}
	}

	slices.Sort(versions)
	return versions, nil
}

// hasCompatibleWheel reports whether files contains a non-yanked binary wheel that matches filter.
func hasCompatibleWheel(files []pypiFile, filter PyPIFilter) bool {
	for _, file := range files {
		if file.PackageType != "bdist_wheel" || file.Yanked {
			continue
		}

		wheel, err := parseWheelName(file.Filename)
		if err != nil {
			continue
		}

		if wheelCompatible(wheel, filter) {
			return true
		}
	}

	return false
}

func normalizePyPIName(name string) string {
	return strings.ToLower(pypiSeparators.ReplaceAllString(name, "-"))
}

// projectJSONURL derives the JSON API URL from a simple index URL. The index is either a base such as
// https://pypi.org or a simple index such as https://pypi.org/simple, and both map to <base>/pypi/<name>/json.
func projectJSONURL(index, name string) (string, error) {
	u, err := url.Parse(index)
	if err != nil {
		return "", fmt.Errorf("mcp: parse PyPI index URL: %w", err)
	}

	base := strings.TrimSuffix(strings.TrimSuffix(u.Path, "/"), "/simple")
	u.Path = base + "/pypi/" + name + "/json"
	u.RawPath = ""
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

// validateFilter checks the platform tags and the Python version before they are used in a comparison.
func validateFilter(filter PyPIFilter) error {
	for _, tag := range filter.Platforms {
		if !platformTagPattern.MatchString(tag) {
			return fmt.Errorf("mcp: invalid wheel platform tag %q", tag)
		}
	}

	if filter.PythonVersion != "" && !pythonVersionRegexp.MatchString(filter.PythonVersion) {
		return fmt.Errorf("mcp: python version %q must look like 3.12", filter.PythonVersion)
	}

	return nil
}

// wheelName is the parsed name-version[-build]-python-abi-platform.whl filename.
type wheelName struct {
	name     string
	python   []string
	abi      []string
	platform []string
}

// parseWheelName splits a wheel filename into its name and compressed python, ABI, and platform tag sets.
func parseWheelName(filename string) (wheelName, error) {
	if !wheelFilenamePattern.MatchString(filename) {
		return wheelName{}, fmt.Errorf("mcp: %q is not a wheel filename", filename)
	}

	stem := strings.TrimSuffix(filename, ".whl")
	parts := strings.Split(stem, "-")
	if len(parts) != 5 && len(parts) != 6 {
		return wheelName{}, fmt.Errorf("mcp: wheel filename %q has an unexpected shape", filename)
	}

	tail := parts[len(parts)-3:]
	wheel := wheelName{
		name:     parts[0],
		python:   strings.Split(tail[0], "."),
		abi:      strings.Split(tail[1], "."),
		platform: strings.Split(tail[2], "."),
	}

	for _, tags := range [][]string{wheel.python, wheel.abi, wheel.platform} {
		for _, tag := range tags {
			if !platformTagPattern.MatchString(tag) {
				return wheelName{}, fmt.Errorf("mcp: wheel filename %q has an invalid tag %q", filename, tag)
			}
		}
	}

	return wheel, nil
}

// wheelCompatible reports whether a wheel fits the platform and Python filter.
func wheelCompatible(wheel wheelName, filter PyPIFilter) bool {
	if len(filter.Platforms) > 0 && !slices.Contains(wheel.platform, "any") &&
		!anyTagIn(wheel.platform, filter.Platforms) {
		return false
	}

	if filter.PythonVersion == "" {
		return true
	}

	major, minor, ok := splitPythonVersion(filter.PythonVersion)
	if !ok {
		return false
	}

	for _, tag := range wheel.python {
		if pythonTagCompatible(tag, wheel.abi, major, minor) {
			return true
		}
	}

	return false
}

// anyTagIn reports whether any tag in tags appears in allowed.
func anyTagIn(tags, allowed []string) bool {
	for _, tag := range tags {
		if slices.ContainsFunc(allowed, func(a string) bool { return strings.EqualFold(a, tag) }) {
			return true
		}
	}

	return false
}

// splitPythonVersion parses "3.12" into 3 and 12.
func splitPythonVersion(version string) (int, int, bool) {
	majorStr, minorStr, ok := strings.Cut(version, ".")
	if !ok {
		return 0, 0, false
	}

	major, errMajor := strconv.Atoi(majorStr)
	minor, errMinor := strconv.Atoi(minorStr)
	return major, minor, errMajor == nil && errMinor == nil
}

// pythonTagCompatible reports whether one wheel python tag runs on major.minor. The pure py3 tag always matches. The
// pyXY and cpXY tags match the exact minor version. A cpXY tag with the abi3 ABI runs on that minor version and every
// earlier one.
func pythonTagCompatible(tag string, abi []string, major, minor int) bool {
	if tag == "py"+strconv.Itoa(major) {
		return true
	}

	prefix := tag[:min(len(tag), 2)]
	if prefix != "py" && prefix != "cp" {
		return false
	}

	digits, ok := strings.CutPrefix(tag[2:], strconv.Itoa(major))
	if !ok {
		return false
	}

	wheelMinor, err := strconv.Atoi(digits)
	if err != nil {
		return false
	}

	if wheelMinor == minor {
		return true
	}

	return prefix == "cp" && slices.Contains(abi, "abi3") && wheelMinor <= minor
}
