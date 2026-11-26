package mcp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

// pypiProjectJSON is a PyPI JSON API document for example-tool. Release 1.0.0 has an sdist, four compatible wheels for
// CPython 3.12, one yanked wheel, and a Windows wheel. Release 0.9.0 is sdist-only. Release 1.0.1 points at a host that
// is not allowed. Release 1.0.2 has a wheel without a digest. %[1]s is the server base URL and %[2]s is a digest.
const pypiProjectJSON = `{
  "info": {"name": "example-tool", "version": "1.0.0"},
  "releases": {
    "0.9.0": [
      {"filename": "example_tool-0.9.0.tar.gz", "packagetype": "sdist",
       "url": "%[1]s/f/example_tool-0.9.0.tar.gz", "digests": {"sha256": "%[2]s"}}
    ],
    "1.0.0": [
      {"filename": "example_tool-1.0.0.tar.gz", "packagetype": "sdist",
       "url": "%[1]s/f/example_tool-1.0.0.tar.gz", "digests": {"sha256": "%[2]s"}},
      {"filename": "example_tool-1.0.0-py3-none-any.whl", "packagetype": "bdist_wheel",
       "url": "%[1]s/f/any.whl", "digests": {"sha256": "%[2]s"}},
      {"filename": "example_tool-1.0.0-cp312-cp312-manylinux_2_28_x86_64.whl", "packagetype": "bdist_wheel",
       "url": "%[1]s/f/cp312.whl", "digests": {"sha256": "%[2]s"}},
      {"filename": "example_tool-1.0.0-cp311-cp311-manylinux_2_28_x86_64.whl", "packagetype": "bdist_wheel",
       "url": "%[1]s/f/cp311.whl", "digests": {"sha256": "%[2]s"}},
      {"filename": "example_tool-1.0.0-cp38-abi3-manylinux_2_17_x86_64.whl", "packagetype": "bdist_wheel",
       "url": "%[1]s/f/abi3.whl", "digests": {"sha256": "%[2]s"}},
      {"filename": "example_tool-1.0.0-cp312-cp312-win_amd64.whl", "packagetype": "bdist_wheel",
       "url": "%[1]s/f/win.whl", "digests": {"sha256": "%[2]s"}},
      {"filename": "example_tool-1.0.0-cp312-cp312-macosx_11_0_arm64.whl", "packagetype": "bdist_wheel",
       "yanked": true, "url": "%[1]s/f/yanked.whl", "digests": {"sha256": "%[2]s"}}
    ],
    "1.0.1": [
      {"filename": "example_tool-1.0.1-py3-none-any.whl", "packagetype": "bdist_wheel",
       "url": "https://evil.example.net/x.whl", "digests": {"sha256": "%[2]s"}}
    ],
    "1.0.2": [
      {"filename": "example_tool-1.0.2-py3-none-any.whl", "packagetype": "bdist_wheel",
       "url": "%[1]s/f/nodigest.whl", "digests": {}}
    ]
  }
}`

// newPyPIServer serves the project document for example-tool and returns its simple index URL and a test client.
func newPyPIServer(t *testing.T) (string, *Client) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/pypi/example-tool/json" {
			http.NotFound(w, r)
			return
		}

		body := strings.NewReplacer("%[1]s", "http://"+r.Host, "%[2]s", testSHA256).Replace(pypiProjectJSON)
		_, _ = fmt.Fprint(w, body)
	}))
	t.Cleanup(server.Close)

	al, err := NewAllowList([]string{hostPort(t, server.URL)})
	if err != nil {
		t.Fatalf("NewAllowList() error = %v", err)
	}

	return server.URL + "/simple", newClient(al, allowLoopbackOnly, true)
}

func wheelNames(release PyPIRelease) []string {
	names := make([]string, 0, len(release.Wheels))
	for _, w := range release.Wheels {
		names = append(names, w.Filename)
	}

	slices.Sort(names)
	return names
}

func TestResolvePyPIFiltersWheels(t *testing.T) {
	index, client := newPyPIServer(t)
	ctx := context.Background()

	release, err := client.ResolvePyPI(ctx, index, "Example.Tool", "", PyPIFilter{
		Platforms:     []string{"manylinux_2_28_x86_64", "manylinux_2_17_x86_64"},
		PythonVersion: "3.12",
	})
	if err != nil {
		t.Fatalf("ResolvePyPI(3.12, manylinux) error = %v", err)
	}

	want := []string{
		"example_tool-1.0.0-cp312-cp312-manylinux_2_28_x86_64.whl",
		"example_tool-1.0.0-cp38-abi3-manylinux_2_17_x86_64.whl",
		"example_tool-1.0.0-py3-none-any.whl",
	}
	slices.Sort(want)
	if got := wheelNames(release); !slices.Equal(got, want) {
		t.Errorf("wheels = %v, want %v", got, want)
	}

	if release.Version != testVersion || release.Name != testPackage {
		t.Errorf("release = %s %s, want %s 1.0.0", release.Name, release.Version, testPackage)
	}

	for _, w := range release.Wheels {
		if w.SHA256 != testSHA256 || !strings.HasPrefix(w.URL, "http://127.0.0.1") {
			t.Errorf("wheel %s missing digest or URL: %+v", w.Filename, w)
		}
	}

	byPython, err := client.ResolvePyPI(ctx, index, testPackage, testVersion, PyPIFilter{PythonVersion: "3.11"})
	if err != nil {
		t.Fatalf("ResolvePyPI(3.11, no platform filter) error = %v", err)
	}

	if len(byPython.Wheels) != 3 {
		t.Errorf("3.11 wheels = %v, want cp311, abi3, and py3 wheels", wheelNames(byPython))
	}

	// A pure-Python wheel tagged any is compatible with every platform, so only it remains on musllinux.
	musl, err := client.ResolvePyPI(ctx, index, testPackage, testVersion, PyPIFilter{
		Platforms: []string{"musllinux_1_2_aarch64"},
	})
	if err != nil {
		t.Fatalf("ResolvePyPI(musllinux) error = %v", err)
	}

	if got := wheelNames(musl); !slices.Equal(got, []string{"example_tool-1.0.0-py3-none-any.whl"}) {
		t.Errorf("musllinux wheels = %v, want only the any wheel", got)
	}
}

func TestResolvePyPIRejects(t *testing.T) {
	index, client := newPyPIServer(t)
	ctx := context.Background()

	tests := []struct {
		name    string
		pkg     string
		version string
		filter  PyPIFilter
		wantErr error
		wantMsg string
	}{
		{name: "sdist-only release", pkg: testPackage, version: "0.9.0", wantErr: ErrNoCompatibleWheel},
		{name: "unknown version", pkg: testPackage, version: "7.7.7", wantErr: ErrVersionNotFound},
		{name: "wheel URL on an unlisted host", pkg: testPackage, version: "1.0.1", wantErr: ErrDenied},
		{name: "wheel without sha256", pkg: testPackage, version: "1.0.2", wantMsg: "no sha256 digest"},
		{name: "invalid package name", pkg: "../example", wantMsg: "invalid PyPI package name"},
		{
			name:    "invalid platform tag",
			pkg:     testPackage,
			filter:  PyPIFilter{Platforms: []string{"x/../y"}},
			wantMsg: "platform tag",
		},
		{
			name:    "invalid python version",
			pkg:     testPackage,
			filter:  PyPIFilter{PythonVersion: "3.x"},
			wantMsg: "python version",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := client.ResolvePyPI(ctx, index, tc.pkg, tc.version, tc.filter)
			if err == nil {
				t.Fatal("ResolvePyPI() error = nil, want a rejection")
			}

			if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Errorf("ResolvePyPI() error = %v, want %v", err, tc.wantErr)
			}

			if tc.wantMsg != "" && !strings.Contains(err.Error(), tc.wantMsg) {
				t.Errorf("ResolvePyPI() error = %v, want it to contain %q", err, tc.wantMsg)
			}
		})
	}
}

func TestResolvePyPIIndexAllowList(t *testing.T) {
	_, client := newPyPIServer(t)
	_, err := client.ResolvePyPI(context.Background(), testUnlistedIndex, testPackage, "", PyPIFilter{})
	if !errors.Is(err, ErrDenied) {
		t.Errorf("ResolvePyPI(unlisted index) error = %v, want ErrDenied", err)
	}
}

func TestProjectJSONURL(t *testing.T) {
	tests := map[string]string{
		testPyPIIndex:                        testDerivedJSON,
		"https://pypi.org/simple/":           testDerivedJSON,
		"https://pypi.org":                   testDerivedJSON,
		"https://mirror.example/repo/simple": "https://mirror.example/repo/pypi/x/json",
		"https://mirror.example/custom":      "https://mirror.example/custom/pypi/x/json",
	}

	for index, want := range tests {
		got, err := projectJSONURL(index, "x")
		if err != nil || got != want {
			t.Errorf("projectJSONURL(%q) = %q, %v; want %q", index, got, err, want)
		}
	}
}

func TestNormalizePyPIName(t *testing.T) {
	if got := normalizePyPIName("Example_Tool.Name--x"); got != "example-tool-name-x" {
		t.Errorf("normalizePyPIName() = %q, want PEP 503 normalization", got)
	}
}

func TestParseWheelName(t *testing.T) {
	wheel, err := parseWheelName("pkg-1.0-1build-py2.py3-none-any.whl")
	if err != nil {
		t.Fatalf("parseWheelName(build tag) error = %v", err)
	}

	pythonTags := []string{"py2", "py3"}
	if wheel.name != "pkg" || !slices.Equal(wheel.python, pythonTags) || !slices.Equal(wheel.platform, []string{"any"}) {
		t.Errorf("parseWheelName(build tag) = %+v", wheel)
	}

	for _, bad := range []string{"pkg-1.0.tar.gz", "pkg-1.0-py3-none.whl", "a-b-c-d-e-f-g.whl"} {
		if _, err := parseWheelName(bad); err == nil {
			t.Errorf("parseWheelName(%q) accepted a malformed name", bad)
		}
	}
}

func TestPythonTagCompatible(t *testing.T) {
	abi3 := []string{"abi3"}
	noneABI := []string{"none"}
	cpABI := []string{"cp312"}

	tests := []struct {
		tag  string
		abi  []string
		want bool
	}{
		{tag: "py3", abi: noneABI, want: true},
		{tag: "cp312", abi: cpABI, want: true},
		{tag: "py312", abi: noneABI, want: true},
		{tag: "cp311", abi: []string{"cp311"}, want: false},
		{tag: testCP38, abi: abi3, want: true},
		{tag: "cp313", abi: abi3, want: false},
		{tag: testCP38, abi: []string{testCP38}, want: false},
		{tag: "pp312", abi: []string{"pypy"}, want: false},
		{tag: "cp3", abi: abi3, want: false},
		{tag: "py2", abi: noneABI, want: false},
	}

	for _, tc := range tests {
		if got := pythonTagCompatible(tc.tag, tc.abi, 3, 12); got != tc.want {
			t.Errorf("pythonTagCompatible(%s, %v, 3.12) = %v, want %v", tc.tag, tc.abi, got, tc.want)
		}
	}
}
