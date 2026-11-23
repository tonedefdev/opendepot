package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
)

func wave6ModuleVersion(name, moduleName, version string, synced bool, os, arch string) opendepotv1alpha1.Version {
	module := moduleName

	return opendepotv1alpha1.Version{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: browseAgentTestNamespace},
		Spec: opendepotv1alpha1.VersionSpec{
			Version:         version,
			ModuleConfigRef: &opendepotv1alpha1.ModuleConfig{Name: &module},
			OperatingSystem: os,
			Architecture:    arch,
		},
		Status: opendepotv1alpha1.VersionStatus{
			Synced:     synced,
			SourceScan: &opendepotv1alpha1.SourceScan{ScannedAt: "2026-01-01T00:00:00Z"},
		},
	}
}

func wave6DecodeVersionList(t *testing.T, body []byte) BrowseVersionList {
	t.Helper()

	var list BrowseVersionList
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatalf("decode version list: %v; body=%s", err, body)
	}

	return list
}

func wave6VersionStrings(items []BrowseVersionSummary) []string {
	versions := make([]string, 0, len(items))
	for _, item := range items {
		versions = append(versions, item.Version)
	}

	return versions
}

func wave6ModuleDetailAPI(t *testing.T, module opendepotv1alpha1.Module, nsLabels map[string]string, versions []opendepotv1alpha1.Version) *httptest.Server {
	t.Helper()

	namespacePayload, err := json.Marshal(map[string]any{"metadata": map[string]any{"name": browseAgentTestNamespace, "labels": nsLabels}})
	if err != nil {
		t.Fatalf("marshal namespace: %v", err)
	}

	modulePayload, err := json.Marshal(module)
	if err != nil {
		t.Fatalf("marshal module: %v", err)
	}

	versionPayload, err := json.Marshal(opendepotv1alpha1.VersionList{Items: versions})
	if err != nil {
		t.Fatalf("marshal versions: %v", err)
	}

	namespacePath := "/apis/opendepot.defdev.io/v1alpha1/namespaces/" + browseAgentTestNamespace
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/v1/namespaces/"+browseAgentTestNamespace:
			_, _ = w.Write(namespacePayload)
		case r.URL.Path == namespacePath+"/modules/"+module.Name:
			_, _ = w.Write(modulePayload)
		case r.URL.Path == namespacePath+"/versions":
			_, _ = w.Write(versionPayload)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(api.Close)

	return api
}

func TestWave6BrowseResourceDetailStatusCodes(t *testing.T) {
	module := opendepotv1alpha1.Module{
		ObjectMeta: metav1.ObjectMeta{Name: "alpha", Namespace: browseAgentTestNamespace, Labels: map[string]string{labelPublic: "true"}},
	}

	private := module
	private.Labels = nil

	cases := []struct {
		name       string
		kind       string
		resource   string
		module     opendepotv1alpha1.Module
		anonymous  bool
		oidc       bool
		wantStatus int
	}{
		{name: "unknown kind is rejected with 400", kind: "widget", resource: "alpha", module: module, anonymous: true, wantStatus: http.StatusBadRequest},
		{name: "missing module returns 404", kind: "module", resource: "missing", module: module, anonymous: true, wantStatus: http.StatusNotFound},
		{name: "private module is hidden from public-only callers", kind: "module", resource: "alpha", module: private, wantStatus: http.StatusNotFound},
		{name: "public module is returned", kind: "module", resource: "alpha", module: module, anonymous: true, wantStatus: http.StatusOK},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			api := wave6ModuleDetailAPI(t, tc.module, map[string]string{labelPublic: "true"}, nil)
			browseAgentTestSetup(t, api, tc.anonymous, tc.oidc)

			target := "/opendepot/ui/v1/resources/" + browseAgentTestNamespace + "/" + tc.kind + "/" + tc.resource
			rec := httptest.NewRecorder()
			browseAgentTestRouter().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tc.wantStatus, rec.Body.String())
			}
		})
	}
}

func TestWave6BrowseResourceDetailNamespaceLookupFailure(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(api.Close)
	browseAgentTestSetup(t, api, true, false)

	rec := httptest.NewRecorder()
	target := "/opendepot/ui/v1/resources/" + browseAgentTestNamespace + "/module/alpha"
	browseAgentTestRouter().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body=%s", rec.Code, rec.Body.String())
	}
}

func TestWave6BrowseVersionsListQueryAndPagination(t *testing.T) {
	versions := []opendepotv1alpha1.Version{
		wave6ModuleVersion("alpha-1-0-0", "alpha", "1.0.0", true, "linux", "amd64"),
		wave6ModuleVersion("alpha-1-1-0", "alpha", "1.1.0", true, "linux", "amd64"),
		wave6ModuleVersion("alpha-2-0-0", "alpha", "2.0.0", false, "darwin", "arm64"),
		wave6ModuleVersion("other-9-0-0", "other", "9.0.0", true, "linux", "amd64"),
	}

	module := opendepotv1alpha1.Module{
		ObjectMeta: metav1.ObjectMeta{Name: "alpha", Namespace: browseAgentTestNamespace, Labels: map[string]string{labelPublic: "true"}},
	}

	api := wave6ModuleDetailAPI(t, module, map[string]string{labelPublic: "true"}, versions)
	browseAgentTestSetup(t, api, true, false)

	cases := []struct {
		name         string
		query        string
		wantVersions []string
		wantTotal    int
		wantPage     int
		wantSize     int
	}{
		{
			name:         "versions are sorted descending and scoped to the module",
			query:        "",
			wantVersions: []string{"2.0.0", "1.1.0", "1.0.0"},
			wantTotal:    3,
			wantPage:     1,
			wantSize:     20,
		},
		{
			name:         "synced false returns only unsynced versions",
			query:        "?synced=false",
			wantVersions: []string{"2.0.0"},
			wantTotal:    1,
			wantPage:     1,
			wantSize:     20,
		},
		{
			name:         "os filter matches nothing because module versions carry no OS",
			query:        "?os=linux",
			wantVersions: []string{},
			wantTotal:    0,
			wantPage:     1,
			wantSize:     20,
		},
		{
			name:         "arch filter matches nothing because module versions carry no arch",
			query:        "?arch=arm64",
			wantVersions: []string{},
			wantTotal:    0,
			wantPage:     1,
			wantSize:     20,
		},
		{
			name:         "search text matches the version string",
			query:        "?q=1.1",
			wantVersions: []string{"1.1.0"},
			wantTotal:    1,
			wantPage:     1,
			wantSize:     20,
		},
		{
			name:         "page two of size one returns the second newest version",
			query:        "?page=2&page_size=1",
			wantVersions: []string{"1.1.0"},
			wantTotal:    3,
			wantPage:     2,
			wantSize:     1,
		},
		{
			name:         "page past the end is empty",
			query:        "?page=5&page_size=2",
			wantVersions: []string{},
			wantTotal:    3,
			wantPage:     5,
			wantSize:     2,
		},
		{
			name:         "no match returns an empty list",
			query:        "?q=does-not-exist",
			wantVersions: []string{},
			wantTotal:    0,
			wantPage:     1,
			wantSize:     20,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			target := "/opendepot/ui/v1/resources/" + browseAgentTestNamespace + "/module/alpha/versions" + tc.query
			rec := httptest.NewRecorder()
			browseAgentTestRouter().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
			}

			list := wave6DecodeVersionList(t, rec.Body.Bytes())
			got := strings.Join(wave6VersionStrings(list.Items), ",")
			if got != strings.Join(tc.wantVersions, ",") {
				t.Fatalf("versions = [%s], want [%s]", got, strings.Join(tc.wantVersions, ","))
			}

			if list.TotalCount != tc.wantTotal {
				t.Fatalf("totalCount = %d, want %d", list.TotalCount, tc.wantTotal)
			}

			if list.Page != tc.wantPage {
				t.Fatalf("page = %d, want %d", list.Page, tc.wantPage)
			}

			if list.PageSize != tc.wantSize {
				t.Fatalf("pageSize = %d, want %d", list.PageSize, tc.wantSize)
			}
		})
	}
}

func TestWave6BrowseVersionsListPrivateModuleIsNotFound(t *testing.T) {
	module := opendepotv1alpha1.Module{
		ObjectMeta: metav1.ObjectMeta{Name: "alpha", Namespace: browseAgentTestNamespace},
	}

	api := wave6ModuleDetailAPI(t, module, map[string]string{labelPublic: "true"}, nil)
	browseAgentTestSetup(t, api, false, false)

	rec := httptest.NewRecorder()
	target := "/opendepot/ui/v1/resources/" + browseAgentTestNamespace + "/module/alpha/versions"
	browseAgentTestRouter().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
}

func TestWave6BrowseScanFindingsVersionSelection(t *testing.T) {
	module := opendepotv1alpha1.Module{
		ObjectMeta: metav1.ObjectMeta{Name: "alpha", Namespace: browseAgentTestNamespace, Labels: map[string]string{labelPublic: "true"}},
	}

	versions := []opendepotv1alpha1.Version{
		wave6ModuleVersion("alpha-1-0-0", "alpha", "1.0.0", true, "linux", "amd64"),
		wave6ModuleVersion("alpha-2-0-0", "alpha", "2.0.0", true, "linux", "amd64"),
	}

	api := wave6ModuleDetailAPI(t, module, map[string]string{labelPublic: "true"}, versions)
	browseAgentTestSetup(t, api, true, false)

	cases := []struct {
		name         string
		query        string
		wantStatus   int
		wantSelected string
	}{
		{name: "no version selects the latest", query: "", wantStatus: http.StatusOK, wantSelected: "2.0.0"},
		{name: "leading v is trimmed from requested version", query: "?version=v1.0.0", wantStatus: http.StatusOK, wantSelected: "1.0.0"},
		{name: "unknown version falls back to the latest scanned version", query: "?version=9.9.9", wantStatus: http.StatusOK, wantSelected: "2.0.0"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			target := "/opendepot/ui/v1/resources/" + browseAgentTestNamespace + "/module/alpha/scan-findings" + tc.query
			rec := httptest.NewRecorder()
			browseAgentTestRouter().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tc.wantStatus, rec.Body.String())
			}

			if tc.wantStatus != http.StatusOK {
				return
			}

			var findings BrowseScanFindings
			if err := json.Unmarshal(rec.Body.Bytes(), &findings); err != nil {
				t.Fatalf("decode scan findings: %v; body=%s", err, rec.Body.String())
			}

			if findings.SelectedVersion != tc.wantSelected {
				t.Fatalf("selectedVersion = %q, want %q", findings.SelectedVersion, tc.wantSelected)
			}
		})
	}
}
