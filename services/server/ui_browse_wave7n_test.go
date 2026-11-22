package main

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
)

const (
	wave7nNamespacePath = "/apis/opendepot.defdev.io/v1alpha1/namespaces/" + browseAgentTestNamespace
	wave7nCoreNSPath    = "/api/v1/namespaces/" + browseAgentTestNamespace
)

func wave7nJSON(t *testing.T, v any) string {
	t.Helper()

	payload, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}

	return string(payload)
}

func wave7nClientset(t *testing.T, api *httptest.Server) *kubernetes.Clientset {
	t.Helper()

	cs, err := kubernetes.NewForConfig(&rest.Config{Host: api.URL})
	if err != nil {
		t.Fatalf("clientset: %v", err)
	}

	return cs
}

func wave7nNamespace(labels map[string]string) covGResponse {
	return covGResponse{body: map[string]any{"metadata": map[string]any{"name": browseAgentTestNamespace, "labels": labels}}}
}

func wave7nModule(name string, latest string) opendepotv1alpha1.Module {
	return opendepotv1alpha1.Module{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: browseAgentTestNamespace},
		Status:     opendepotv1alpha1.ModuleStatus{LatestVersion: &latest},
	}
}

func wave7nProvider(name string, latest string) opendepotv1alpha1.Provider {
	return opendepotv1alpha1.Provider{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: browseAgentTestNamespace},
		Status:     opendepotv1alpha1.ProviderStatus{LatestVersion: &latest},
	}
}

func wave7nProviderVersion(name, version, os, arch string, synced bool, binary *opendepotv1alpha1.BinaryScan) opendepotv1alpha1.Version {
	provider := name

	return opendepotv1alpha1.Version{
		ObjectMeta: metav1.ObjectMeta{Name: name + "-" + version + "-" + os + "-" + arch, Namespace: browseAgentTestNamespace},
		Spec: opendepotv1alpha1.VersionSpec{
			Version:           version,
			ProviderConfigRef: &opendepotv1alpha1.ProviderConfig{Name: &provider},
			OperatingSystem:   os,
			Architecture:      arch,
		},
		Status: opendepotv1alpha1.VersionStatus{Synced: synced, BinaryScan: binary},
	}
}

func wave7nModuleVersion(module, version, os, arch string, synced bool) opendepotv1alpha1.Version {
	return wave6ModuleVersion(module+"-"+version+"-"+os+"-"+arch, module, version, synced, os, arch)
}

func TestWave7NEnrichProviderCardPlatformsAndScans(t *testing.T) {
	name := "aws"
	latest := "1.0.0"
	provider := wave7nProvider(name, latest)
	versions := []opendepotv1alpha1.Version{
		wave7nProviderVersion(name, "1.0.0", "linux", "amd64", true, &opendepotv1alpha1.BinaryScan{
			ScannedAt: "2026-01-02T00:00:00Z",
			Findings:  []opendepotv1alpha1.SecurityFinding{{VulnerabilityID: "CVE-1", Severity: "CRITICAL"}},
		}),
		wave7nProviderVersion(name, "1.0.0", "darwin", "arm64", false, &opendepotv1alpha1.BinaryScan{
			ScannedAt: "2026-01-03T00:00:00Z",
			Findings:  []opendepotv1alpha1.SecurityFinding{{VulnerabilityID: "CVE-2", Severity: "HIGH"}},
		}),
		wave7nProviderVersion(name, "0.9.0", "linux", "amd64", true, &opendepotv1alpha1.BinaryScan{
			ScannedAt: "2025-12-01T00:00:00Z",
			Findings:  []opendepotv1alpha1.SecurityFinding{{VulnerabilityID: "CVE-3", Severity: "LOW"}},
		}),
		wave7nProviderVersion("other", "1.0.0", "linux", "amd64", true, nil),
	}

	t.Run("collects platforms and flags unsynced versions", func(t *testing.T) {
		card := BrowseResource{}
		enrichProviderCard(&card, provider, versions, "", "")

		if !card.HasUnsyncedVersions {
			t.Fatal("expected unsynced version to be flagged")
		}

		if len(card.Platforms) != 2 {
			t.Fatalf("platforms = %+v, want 2 entries", card.Platforms)
		}

		if card.Platforms[0].OS != "darwin" || card.Platforms[1].OS != "linux" {
			t.Fatalf("platforms not sorted by OS: %+v", card.Platforms)
		}

		if card.ScanCounts == nil {
			t.Fatal("expected scan counts from latest binary scan")
		}

		if card.LastScanned != "2026-01-03T00:00:00Z" {
			t.Fatalf("LastScanned = %q, want latest binary scan time", card.LastScanned)
		}
	})

	t.Run("applies OS and arch filters to platforms", func(t *testing.T) {
		card := BrowseResource{}
		enrichProviderCard(&card, provider, versions, "linux", "amd64")

		if len(card.Platforms) != 1 || card.Platforms[0].OS != "linux" || card.Platforms[0].Arch != "amd64" {
			t.Fatalf("platforms = %+v, want only linux/amd64", card.Platforms)
		}
	})

	t.Run("leaves scan counts empty without scans", func(t *testing.T) {
		card := BrowseResource{}
		enrichProviderCard(&card, wave7nProvider(name, latest), []opendepotv1alpha1.Version{
			wave7nProviderVersion(name, "1.0.0", "linux", "amd64", true, nil),
		}, "", "")

		if card.ScanCounts != nil {
			t.Fatalf("ScanCounts = %+v, want nil", card.ScanCounts)
		}

		if card.HasUnsyncedVersions {
			t.Fatal("synced version must not be flagged")
		}
	})
}

func TestWave7NBrowseModuleReadme(t *testing.T) {
	readmeRef := &opendepotv1alpha1.ReadmeConfigMapRef{Name: "readme-cm", Key: "README.md"}
	versionWith := func(ref *opendepotv1alpha1.ReadmeConfigMapRef) []opendepotv1alpha1.Version {
		v := wave7nModuleVersion("vpc", "1.0.0", "linux", "amd64", true)
		v.Status.ReadmeConfigMapRef = ref

		return []opendepotv1alpha1.Version{v}
	}

	tests := []struct {
		name     string
		versions []opendepotv1alpha1.Version
		routes   map[string]covGResponse
		want     *string
	}{
		{
			name:     "no readme reference",
			versions: versionWith(nil),
			routes:   map[string]covGResponse{},
		},
		{
			name:     "decodes readme from configmap",
			versions: versionWith(readmeRef),
			routes: map[string]covGResponse{
				wave7nCoreNSPath + "/configmaps/readme-cm": {body: map[string]any{"data": map[string]string{"README.md": base64.StdEncoding.EncodeToString([]byte("# VPC"))}}},
			},
			want: wave7nPtr("# VPC"),
		},
		{
			name:     "missing key yields nil",
			versions: versionWith(readmeRef),
			routes: map[string]covGResponse{
				wave7nCoreNSPath + "/configmaps/readme-cm": {body: map[string]any{"data": map[string]string{"other": "eA=="}}},
			},
		},
		{
			name:     "invalid base64 yields nil",
			versions: versionWith(readmeRef),
			routes: map[string]covGResponse{
				wave7nCoreNSPath + "/configmaps/readme-cm": {body: map[string]any{"data": map[string]string{"README.md": "!!not-base64!!"}}},
			},
		},
		{
			name:     "missing configmap yields nil",
			versions: versionWith(readmeRef),
			routes:   map[string]covGResponse{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := covGAPIServer(t, tt.routes)
			cs := wave7nClientset(t, api)
			r := covGRequest(http.MethodGet, "/", nil)

			got := browseModuleReadme(cs, r, browseAgentTestNamespace, tt.versions, "1.0.0")
			if tt.want == nil {
				if got != nil {
					t.Fatalf("readme = %q, want nil", *got)
				}

				return
			}

			if got == nil || *got != *tt.want {
				t.Fatalf("readme = %v, want %q", got, *tt.want)
			}
		})
	}
}

func TestWave7NBrowseDepotRefLookup(t *testing.T) {
	depots := opendepotv1alpha1.DepotList{Items: []opendepotv1alpha1.Depot{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "alpha", Namespace: browseAgentTestNamespace},
			Status:     opendepotv1alpha1.DepotStatus{Modules: []string{"vpc"}},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Name: "beta", Namespace: browseAgentTestNamespace},
			Status:     opendepotv1alpha1.DepotStatus{Providers: []string{"aws"}},
		},
	}}

	api := covGAPIServer(t, map[string]covGResponse{
		wave7nNamespacePath + "/depots": {body: depots},
	})
	cs := wave7nClientset(t, api)
	r := covGRequest(http.MethodGet, "/", nil)

	if got := browseDepotForModule(cs, r, browseAgentTestNamespace, "vpc"); got == nil || got.Name != "alpha" || got.Namespace != browseAgentTestNamespace {
		t.Fatalf("module depot = %+v, want alpha", got)
	}

	if got := browseDepotForModule(cs, r, browseAgentTestNamespace, "missing"); got != nil {
		t.Fatalf("unknown module depot = %+v, want nil", got)
	}

	if got := browseDepotForProvider(cs, r, browseAgentTestNamespace, "aws"); got == nil || got.Name != "beta" {
		t.Fatalf("provider depot = %+v, want beta", got)
	}

	if got := browseDepotForProvider(cs, r, browseAgentTestNamespace, "gcp"); got != nil {
		t.Fatalf("unknown provider depot = %+v, want nil", got)
	}

	failing := covGAPIServer(t, map[string]covGResponse{
		wave7nNamespacePath + "/depots": {status: http.StatusInternalServerError, raw: `{"kind":"Status"}`},
	})
	failingCS := wave7nClientset(t, failing)

	if got := browseDepotForModule(failingCS, r, browseAgentTestNamespace, "vpc"); got != nil {
		t.Fatalf("depot lookup on API error = %+v, want nil", got)
	}

	malformed := covGAPIServer(t, map[string]covGResponse{
		wave7nNamespacePath + "/depots": {raw: `not-json`},
	})
	malformedCS := wave7nClientset(t, malformed)

	if got := browseDepotForProvider(malformedCS, r, browseAgentTestNamespace, "aws"); got != nil {
		t.Fatalf("depot lookup on malformed body = %+v, want nil", got)
	}
}

func TestWave7NBrowseAuthCheckModes(t *testing.T) {
	api := covGAPIServer(t, nil)

	t.Run("anonymous grants all access", func(t *testing.T) {
		browseAgentTestSetup(t, api, true, false)

		w := httptest.NewRecorder()
		binding, allAccess, ok := browseAuthCheck(w, covGRequest(http.MethodGet, "/", nil))
		if !ok || !allAccess || binding != nil {
			t.Fatalf("anonymous: binding=%v allAccess=%v ok=%v", binding, allAccess, ok)
		}
	})

	t.Run("non-OIDC mode is public only", func(t *testing.T) {
		browseAgentTestSetup(t, api, false, false)

		w := httptest.NewRecorder()
		binding, allAccess, ok := browseAuthCheck(w, covGRequest(http.MethodGet, "/", nil))
		if !ok || allAccess || binding != nil {
			t.Fatalf("non-OIDC: binding=%v allAccess=%v ok=%v", binding, allAccess, ok)
		}
	})

	t.Run("OIDC rejects missing bearer token", func(t *testing.T) {
		browseAgentTestSetup(t, api, false, true)

		w := httptest.NewRecorder()
		_, _, ok := browseAuthCheck(w, covGRequest(http.MethodGet, "/", nil))
		if ok || w.Code != http.StatusUnauthorized {
			t.Fatalf("missing header: ok=%v status=%d, want 401", ok, w.Code)
		}
	})

	t.Run("OIDC rejects non-bearer authorization", func(t *testing.T) {
		browseAgentTestSetup(t, api, false, true)

		r := covGRequest(http.MethodGet, "/", nil)
		r.Header.Set("Authorization", "Basic abc")

		w := httptest.NewRecorder()
		_, _, ok := browseAuthCheck(w, r)
		if ok || w.Code != http.StatusUnauthorized {
			t.Fatalf("basic auth: ok=%v status=%d, want 401", ok, w.Code)
		}
	})
}

func TestWave7NModuleDetailDepotAndReadme(t *testing.T) {
	module := wave7nModule("vpc", "1.0.0")
	version := wave7nModuleVersion("vpc", "1.0.0", "linux", "amd64", true)
	version.Status.ReadmeConfigMapRef = &opendepotv1alpha1.ReadmeConfigMapRef{Name: "readme-cm", Key: "README.md"}
	depots := opendepotv1alpha1.DepotList{Items: []opendepotv1alpha1.Depot{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "alpha", Namespace: browseAgentTestNamespace},
			Status:     opendepotv1alpha1.DepotStatus{Modules: []string{"vpc"}},
		},
	}}

	api := covGAPIServer(t, map[string]covGResponse{
		wave7nCoreNSPath:                           wave7nNamespace(nil),
		wave7nNamespacePath + "/modules/vpc":       {body: module},
		wave7nNamespacePath + "/versions":          {body: opendepotv1alpha1.VersionList{Items: []opendepotv1alpha1.Version{version}}},
		wave7nNamespacePath + "/depots":            {body: depots},
		wave7nCoreNSPath + "/configmaps/readme-cm": {body: map[string]any{"data": map[string]string{"README.md": base64.StdEncoding.EncodeToString([]byte("# VPC"))}}},
	})
	covGUseCluster(t, api)

	w := httptest.NewRecorder()
	browseAgentTestRouter().ServeHTTP(w, covGRequest(http.MethodGet, "/opendepot/ui/v1/resources/team/module/vpc", map[string]string{
		"namespace": browseAgentTestNamespace,
		"kind":      "module",
		"name":      "vpc",
	}))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}

	var detail BrowseResourceDetail
	if err := json.Unmarshal(w.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode detail: %v", err)
	}

	if detail.ReadmeContent == nil || *detail.ReadmeContent != "# VPC" {
		t.Fatalf("readme = %v, want # VPC", detail.ReadmeContent)
	}

	if detail.DepotRef == nil || detail.DepotRef.Name != "alpha" {
		t.Fatalf("depotRef = %+v, want alpha", detail.DepotRef)
	}
}

func TestWave7NModuleDetailNamespaceFailureIsServerError(t *testing.T) {
	api := covGAPIServer(t, map[string]covGResponse{
		wave7nCoreNSPath:                     {status: http.StatusInternalServerError, raw: `{"kind":"Status"}`},
		wave7nNamespacePath + "/modules/vpc": {body: wave7nModule("vpc", "1.0.0")},
	})
	covGUseCluster(t, api)

	w := httptest.NewRecorder()
	browseAgentTestRouter().ServeHTTP(w, covGRequest(http.MethodGet, "/opendepot/ui/v1/resources/team/module/vpc", map[string]string{
		"namespace": browseAgentTestNamespace,
		"kind":      "module",
		"name":      "vpc",
	}))

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
}

func TestWave7NProviderDetailDepotRef(t *testing.T) {
	provider := wave7nProvider("aws", "1.0.0")
	versions := []opendepotv1alpha1.Version{
		wave7nProviderVersion("aws", "1.0.0", "linux", "amd64", true, &opendepotv1alpha1.BinaryScan{
			ScannedAt: "2026-01-02T00:00:00Z",
			Findings:  []opendepotv1alpha1.SecurityFinding{{VulnerabilityID: "CVE-1", Severity: "HIGH"}},
		}),
	}
	depots := opendepotv1alpha1.DepotList{Items: []opendepotv1alpha1.Depot{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "beta", Namespace: browseAgentTestNamespace},
			Status:     opendepotv1alpha1.DepotStatus{Providers: []string{"aws"}},
		},
	}}

	api := covGAPIServer(t, map[string]covGResponse{
		wave7nCoreNSPath:                       wave7nNamespace(nil),
		wave7nNamespacePath + "/providers/aws": {body: provider},
		wave7nNamespacePath + "/versions":      {body: opendepotv1alpha1.VersionList{Items: versions}},
		wave7nNamespacePath + "/depots":        {body: depots},
	})
	covGUseCluster(t, api)

	w := httptest.NewRecorder()
	browseAgentTestRouter().ServeHTTP(w, covGRequest(http.MethodGet, "/opendepot/ui/v1/resources/team/provider/aws", map[string]string{
		"namespace": browseAgentTestNamespace,
		"kind":      "provider",
		"name":      "aws",
	}))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}

	var detail BrowseResourceDetail
	if err := json.Unmarshal(w.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode detail: %v", err)
	}

	if detail.DepotRef == nil || detail.DepotRef.Name != "beta" {
		t.Fatalf("depotRef = %+v, want beta", detail.DepotRef)
	}
}

func TestWave7NModuleVersionsFiltersAndPaging(t *testing.T) {
	module := wave7nModule("vpc", "2.0.0")
	versions := []opendepotv1alpha1.Version{
		wave7nModuleVersion("vpc", "1.0.0", "linux", "amd64", true),
		wave7nModuleVersion("vpc", "2.0.0", "linux", "amd64", true),
		wave7nModuleVersion("vpc", "2.0.0", "darwin", "arm64", false),
	}

	api := covGAPIServer(t, map[string]covGResponse{
		wave7nCoreNSPath:                     wave7nNamespace(nil),
		wave7nNamespacePath + "/modules/vpc": {body: module},
		wave7nNamespacePath + "/versions":    {body: opendepotv1alpha1.VersionList{Items: versions}},
	})
	covGUseCluster(t, api)

	params := map[string]string{"namespace": browseAgentTestNamespace, "kind": "module", "name": "vpc"}

	tests := []struct {
		name      string
		target    string
		wantTotal int
	}{
		{name: "unfiltered", target: "/x/versions", wantTotal: 3},
		{name: "synced filter", target: "/x/versions?synced=true", wantTotal: 2},
		{name: "page size", target: "/x/versions?page=1&page_size=1", wantTotal: 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			handleBrowseVersionsList(w, covGRequest(http.MethodGet, tt.target, params))

			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
			}

			list := wave6DecodeVersionList(t, w.Body.Bytes())
			if list.TotalCount != tt.wantTotal {
				t.Fatalf("totalCount = %d, want %d", list.TotalCount, tt.wantTotal)
			}
		})
	}

	t.Run("page size limits items", func(t *testing.T) {
		w := httptest.NewRecorder()
		handleBrowseVersionsList(w, covGRequest(http.MethodGet, "/x/versions?page=1&page_size=1", params))

		list := wave6DecodeVersionList(t, w.Body.Bytes())
		if len(list.Items) != 1 || list.PageSize != 1 {
			t.Fatalf("items=%d pageSize=%d, want 1/1", len(list.Items), list.PageSize)
		}
	})
}

func TestWave7NProviderVersionsList(t *testing.T) {
	versions := []opendepotv1alpha1.Version{
		wave7nProviderVersion("aws", "1.0.0", "linux", "amd64", true, nil),
		wave7nProviderVersion("aws", "1.1.0", "linux", "amd64", true, nil),
	}

	api := covGAPIServer(t, map[string]covGResponse{
		wave7nCoreNSPath:                       wave7nNamespace(nil),
		wave7nNamespacePath + "/providers/aws": {body: wave7nProvider("aws", "1.1.0")},
		wave7nNamespacePath + "/versions":      {body: opendepotv1alpha1.VersionList{Items: versions}},
	})
	covGUseCluster(t, api)

	w := httptest.NewRecorder()
	handleBrowseVersionsList(w, covGRequest(http.MethodGet, "/x/versions", map[string]string{
		"namespace": browseAgentTestNamespace,
		"kind":      "provider",
		"name":      "aws",
	}))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}

	list := wave6DecodeVersionList(t, w.Body.Bytes())
	if list.TotalCount != 2 || list.Items[0].Version != "1.1.0" {
		t.Fatalf("list = %+v, want 2 versions newest first", list)
	}

	w = httptest.NewRecorder()
	handleBrowseVersionsList(w, covGRequest(http.MethodGet, "/x/versions?os=darwin", map[string]string{
		"namespace": browseAgentTestNamespace,
		"kind":      "provider",
		"name":      "aws",
	}))

	if list := wave6DecodeVersionList(t, w.Body.Bytes()); list.TotalCount != 0 {
		t.Fatalf("os=darwin totalCount = %d, want 0", list.TotalCount)
	}
}

func TestWave7NScanFindingsModuleAndProviderSelection(t *testing.T) {
	module := wave7nModule("vpc", "1.0.0")
	moduleVersion := wave7nModuleVersion("vpc", "1.0.0", "linux", "amd64", true)
	moduleVersion.Status.SourceScan = &opendepotv1alpha1.SourceScan{
		ScannedAt: "2026-01-02T00:00:00Z",
		Findings:  []opendepotv1alpha1.SecurityFinding{{VulnerabilityID: "CVE-9", Severity: "HIGH"}},
	}

	moduleAPI := covGAPIServer(t, map[string]covGResponse{
		wave7nCoreNSPath:                     wave7nNamespace(nil),
		wave7nNamespacePath + "/modules/vpc": {body: module},
		wave7nNamespacePath + "/versions":    {body: opendepotv1alpha1.VersionList{Items: []opendepotv1alpha1.Version{moduleVersion}}},
	})
	covGUseCluster(t, moduleAPI)

	moduleParams := map[string]string{"namespace": browseAgentTestNamespace, "kind": "module", "name": "vpc"}

	w := httptest.NewRecorder()
	handleBrowseScanFindings(w, covGRequest(http.MethodGet, "/x/scan-findings", moduleParams))

	if w.Code != http.StatusOK {
		t.Fatalf("module scan-findings status = %d, body=%s", w.Code, w.Body.String())
	}

	var findings BrowseScanFindings
	if err := json.Unmarshal(w.Body.Bytes(), &findings); err != nil {
		t.Fatalf("decode findings: %v", err)
	}

	if len(findings.SourceScanFindings) != 1 || findings.SourceScanFindings[0].VulnerabilityID != "CVE-9" {
		t.Fatalf("module source findings = %+v, want CVE-9", findings.SourceScanFindings)
	}

	providerVersions := []opendepotv1alpha1.Version{
		wave7nProviderVersion("aws", "1.0.0", "linux", "amd64", true, &opendepotv1alpha1.BinaryScan{
			ScannedAt: "2026-01-02T00:00:00Z",
			Findings:  []opendepotv1alpha1.SecurityFinding{{VulnerabilityID: "CVE-A", Severity: "HIGH"}},
		}),
		wave7nProviderVersion("aws", "2.0.0", "linux", "amd64", true, &opendepotv1alpha1.BinaryScan{
			ScannedAt: "2026-02-02T00:00:00Z",
			Findings:  []opendepotv1alpha1.SecurityFinding{{VulnerabilityID: "CVE-B", Severity: "LOW"}},
		}),
	}

	providerAPI := covGAPIServer(t, map[string]covGResponse{
		wave7nCoreNSPath:                       wave7nNamespace(nil),
		wave7nNamespacePath + "/providers/aws": {body: wave7nProvider("aws", "2.0.0")},
		wave7nNamespacePath + "/versions":      {body: opendepotv1alpha1.VersionList{Items: providerVersions}},
	})
	covGUseCluster(t, providerAPI)

	providerParams := map[string]string{"namespace": browseAgentTestNamespace, "kind": "provider", "name": "aws"}

	w = httptest.NewRecorder()
	handleBrowseScanFindings(w, covGRequest(http.MethodGet, "/x/scan-findings?binaryVersion=1.0.0", providerParams))

	if w.Code != http.StatusOK {
		t.Fatalf("provider scan-findings status = %d, body=%s", w.Code, w.Body.String())
	}

	findings = BrowseScanFindings{}
	if err := json.Unmarshal(w.Body.Bytes(), &findings); err != nil {
		t.Fatalf("decode provider findings: %v", err)
	}

	linux := findings.BinaryScanFindings["linux/amd64"]
	if len(linux) != 1 || linux[0].VulnerabilityID != "CVE-A" {
		t.Fatalf("binary findings for requested 1.0.0 = %+v, want CVE-A", findings.BinaryScanFindings)
	}
}

func wave7nPtr(s string) *string {
	return &s
}
