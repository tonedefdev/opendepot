package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestBrowseCovStorageConfigToBrowse(t *testing.T) {
	key := "modules/"
	dir := "/data"
	enabled := true
	ttl := metav1.Duration{Duration: 15 * time.Minute}

	cases := []struct {
		name    string
		storage *opendepotv1alpha1.StorageConfig
		want    BrowseStorageConfig
		isNil   bool
	}{
		{name: "nil config returns nil", isNil: true},
		{
			name:    "s3 backend with key",
			storage: &opendepotv1alpha1.StorageConfig{S3: &opendepotv1alpha1.AmazonS3Config{Bucket: "b", Region: "us-east-1", Key: &key}},
			want:    BrowseStorageConfig{Backend: "s3", Bucket: "b", Region: "us-east-1", Key: &key},
		},
		{
			name: "azure backend",
			storage: &opendepotv1alpha1.StorageConfig{AzureStorage: &opendepotv1alpha1.AzureStorageConfig{
				AccountName: "acct", AccountUrl: "https://acct.blob", SubscriptionID: "sub", ResourceGroup: "rg",
			}},
			want: BrowseStorageConfig{Backend: "azureStorage", AccountName: "acct", AccountUrl: "https://acct.blob", SubscriptionID: "sub", ResourceGroup: "rg"},
		},
		{
			name:    "gcs backend",
			storage: &opendepotv1alpha1.StorageConfig{GCS: &opendepotv1alpha1.GoogleCloudStorageConfig{Bucket: "gcs-bucket"}},
			want:    BrowseStorageConfig{Backend: "gcs", Bucket: "gcs-bucket"},
		},
		{
			name:    "filesystem backend with presign ttl",
			storage: &opendepotv1alpha1.StorageConfig{FileSystem: &opendepotv1alpha1.FileSystemConfig{DirectoryPath: &dir}, Presign: &opendepotv1alpha1.PresignConfig{Enabled: &enabled, TTL: &ttl}},
			want:    BrowseStorageConfig{Backend: "fileSystem", DirectoryPath: &dir, PresignEnabled: &enabled, PresignTTL: "15m0s"},
		},
		{
			name:    "presign without ttl leaves ttl empty",
			storage: &opendepotv1alpha1.StorageConfig{GCS: &opendepotv1alpha1.GoogleCloudStorageConfig{Bucket: "b"}, Presign: &opendepotv1alpha1.PresignConfig{Enabled: &enabled}},
			want:    BrowseStorageConfig{Backend: "gcs", Bucket: "b", PresignEnabled: &enabled},
		},
		{
			name:    "no backend set still returns a config",
			storage: &opendepotv1alpha1.StorageConfig{},
			want:    BrowseStorageConfig{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := storageConfigToBrowse(tc.storage)
			if tc.isNil {
				if got != nil {
					t.Fatalf("storageConfigToBrowse() = %+v, want nil", got)
				}

				return
			}

			if got == nil {
				t.Fatal("storageConfigToBrowse() = nil, want config")
			}

			if got.Backend != tc.want.Backend || got.Bucket != tc.want.Bucket || got.Region != tc.want.Region ||
				got.AccountName != tc.want.AccountName || got.AccountUrl != tc.want.AccountUrl ||
				got.SubscriptionID != tc.want.SubscriptionID || got.ResourceGroup != tc.want.ResourceGroup ||
				got.PresignTTL != tc.want.PresignTTL {
				t.Fatalf("storageConfigToBrowse() = %+v, want %+v", got, tc.want)
			}

			if (got.Key == nil) != (tc.want.Key == nil) || (got.DirectoryPath == nil) != (tc.want.DirectoryPath == nil) ||
				(got.PresignEnabled == nil) != (tc.want.PresignEnabled == nil) {
				t.Fatalf("storageConfigToBrowse() pointer fields = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestBrowseCovLatestOf(t *testing.T) {
	if got := latestOf(); got != "" {
		t.Fatalf("latestOf() = %q, want empty", got)
	}

	if got := latestOf("2024-01-01T00:00:00Z", "2025-06-01T00:00:00Z", "2024-12-31T00:00:00Z"); got != "2025-06-01T00:00:00Z" {
		t.Fatalf("latestOf() = %q, want 2025-06-01T00:00:00Z", got)
	}
}

func TestBrowseCovSeverityRankAndHighest(t *testing.T) {
	ranks := map[string]int{"critical": 0, "HIGH": 1, "Medium": 2, "low": 3, "unknown": 4, "": 4}
	for in, want := range ranks {
		if got := severityRank(in); got != want {
			t.Fatalf("severityRank(%q) = %d, want %d", in, got, want)
		}
	}

	if got := highestSeverity(nil); got != "" {
		t.Fatalf("highestSeverity(nil) = %q, want empty", got)
	}

	cases := []struct {
		counts BrowseScanCounts
		want   string
	}{
		{counts: BrowseScanCounts{Critical: 1, High: 2}, want: "CRITICAL"},
		{counts: BrowseScanCounts{High: 1, Low: 5}, want: "HIGH"},
		{counts: BrowseScanCounts{Medium: 1}, want: "MEDIUM"},
		{counts: BrowseScanCounts{Low: 1}, want: "LOW"},
		{counts: BrowseScanCounts{Unknown: 1}, want: "UNKNOWN"},
		{counts: BrowseScanCounts{Exempted: 3}, want: ""},
	}

	for _, tc := range cases {
		counts := tc.counts
		if got := highestSeverity(&counts); got != tc.want {
			t.Fatalf("highestSeverity(%+v) = %q, want %q", tc.counts, got, tc.want)
		}
	}
}

func TestBrowseCovScanCounts(t *testing.T) {
	if got := browseScanCounts(nil); got != nil {
		t.Fatalf("browseScanCounts(nil) = %+v, want nil", got)
	}

	findings := []opendepotv1alpha1.SecurityFinding{
		{Severity: "critical"},
		{Severity: "HIGH"},
		{Severity: "Medium"},
		{Severity: "LOW"},
		{Severity: "NEGLIGIBLE"},
		{Severity: "HIGH", Exempted: true},
	}

	got := browseScanCounts(findings)
	want := BrowseScanCounts{Critical: 1, High: 1, Medium: 1, Low: 1, Unknown: 1, Exempted: 1}
	if *got != want {
		t.Fatalf("browseScanCounts() = %+v, want %+v", *got, want)
	}
}

func TestBrowseCovApplyBrowseFilters(t *testing.T) {
	items := []BrowseResource{
		{Name: "vpc", Namespace: "team", Provider: "aws", LatestVersion: "1.0.0", Synced: true, ScanCounts: &BrowseScanCounts{High: 1}},
		{Name: "dns", Namespace: "platform", Provider: "gcp", RepoURL: "https://example.com/dns", Synced: false, ScanCounts: &BrowseScanCounts{Low: 2}},
		{Name: "bare", Namespace: "team", Synced: true},
	}

	if got := applyBrowseFilters(items, "", "", ""); len(got) != len(items) {
		t.Fatalf("no filters returned %d items, want %d", len(got), len(items))
	}

	if got := namesOf(applyBrowseFilters(items, "gcp", "", "")); strings.Join(got, ",") != "dns" {
		t.Fatalf("search by provider = %v, want [dns]", got)
	}

	if got := namesOf(applyBrowseFilters(items, "example.com", "", "")); strings.Join(got, ",") != "dns" {
		t.Fatalf("search by repo url = %v, want [dns]", got)
	}

	if got := namesOf(applyBrowseFilters(items, "1.0.0", "", "")); strings.Join(got, ",") != "vpc" {
		t.Fatalf("search by latest version = %v, want [vpc]", got)
	}

	if got := namesOf(applyBrowseFilters(items, "", "true", "")); strings.Join(got, ",") != "vpc,bare" {
		t.Fatalf("synced=true = %v, want [vpc bare]", got)
	}

	if got := namesOf(applyBrowseFilters(items, "", "false", "")); strings.Join(got, ",") != "dns" {
		t.Fatalf("synced=false = %v, want [dns]", got)
	}

	if got := namesOf(applyBrowseFilters(items, "", "", "HIGH")); strings.Join(got, ",") != "vpc" {
		t.Fatalf("severity=HIGH = %v, want [vpc]", got)
	}

	if got := namesOf(applyBrowseFilters(items, "", "", "LOW")); strings.Join(got, ",") != "vpc,dns" {
		t.Fatalf("severity=LOW = %v, want [vpc dns]", got)
	}
}

func TestBrowseCovSortBrowseItems(t *testing.T) {
	base := func() []BrowseResource {
		return []BrowseResource{
			{Name: "c", Namespace: "b", LatestVersion: "2.0.0", Synced: false, LastScanned: "2024-03-01T00:00:00Z", ScanCounts: &BrowseScanCounts{Medium: 1}, LastUpdated: "2024-01-01T00:00:00Z"},
			{Name: "a", Namespace: "a", LatestVersion: "1.0.0", Synced: true, LastScanned: "2024-01-01T00:00:00Z", ScanCounts: &BrowseScanCounts{Critical: 1}, LastUpdated: "2024-03-01T00:00:00Z"},
			{Name: "b", Namespace: "a", LatestVersion: "3.0.0", Synced: true, LastScanned: "2024-02-01T00:00:00Z", LastUpdated: "2024-02-01T00:00:00Z"},
		}
	}

	cases := []struct {
		by, dir string
		want    string
	}{
		{by: "namespace", dir: "asc", want: "a,b,c"},
		{by: "latest_version", dir: "asc", want: "a,c,b"},
		{by: "synced", dir: "asc", want: "a,b,c"},
		{by: "severity", dir: "asc", want: "a,c,b"},
		{by: "last_scanned", dir: "asc", want: "a,b,c"},
		{by: "last_updated", dir: "asc", want: "c,b,a"},
		{by: "last_updated", dir: "desc", want: "a,b,c"},
		{by: "", dir: "asc", want: "a,b,c"},
	}

	for _, tc := range cases {
		t.Run(tc.by+"/"+tc.dir, func(t *testing.T) {
			items := base()
			sortBrowseItems(items, tc.by, tc.dir)
			names := make([]string, 0, len(items))
			for _, item := range items {
				names = append(names, item.Name)
			}

			if got := strings.Join(names, ","); got != tc.want {
				t.Fatalf("sortBrowseItems(%s, %s) = %s, want %s", tc.by, tc.dir, got, tc.want)
			}
		})
	}
}

func TestBrowseCovCollectSourceAndBinaryFindings(t *testing.T) {
	if got := collectModuleSourceFindings(nil); got != nil {
		t.Fatalf("collectModuleSourceFindings(nil) = %+v, want nil", got)
	}

	older := opendepotv1alpha1.Version{}
	older.Spec.Version = "1.0.0"
	older.Status.SourceScan = &opendepotv1alpha1.SourceScan{Findings: []opendepotv1alpha1.SecurityFinding{makeFinding("OLD")}}

	newer := opendepotv1alpha1.Version{}
	newer.Spec.Version = "1.2.0"
	newer.Status.SourceScan = &opendepotv1alpha1.SourceScan{Findings: []opendepotv1alpha1.SecurityFinding{makeFinding("NEW")}}

	unscanned := opendepotv1alpha1.Version{}
	unscanned.Spec.Version = "9.0.0"

	versions := []opendepotv1alpha1.Version{older, unscanned, newer}
	if got := collectModuleSourceFindings(versions); len(got) != 1 || got[0].VulnerabilityID != "NEW" {
		t.Fatalf("collectModuleSourceFindings() = %+v, want NEW from 1.2.0", got)
	}

	if got := findProviderSourceScanFromVersions(versions, ""); got != newer.Status.SourceScan {
		t.Fatalf("findProviderSourceScanFromVersions(latest) did not return 1.2.0 scan")
	}

	if got := findProviderSourceScanFromVersions(versions, "1.0.0"); got != older.Status.SourceScan {
		t.Fatalf("findProviderSourceScanFromVersions(1.0.0) did not return 1.0.0 scan")
	}

	if got := findProviderSourceScanFromVersions(versions, "5.0.0"); got != nil {
		t.Fatalf("findProviderSourceScanFromVersions(missing) = %+v, want nil", got)
	}

	if got := findProviderSourceScanFromVersions([]opendepotv1alpha1.Version{unscanned}, ""); got != nil {
		t.Fatalf("findProviderSourceScanFromVersions(no scans) = %+v, want nil", got)
	}

	linuxOld := makeProviderVersion("1.0.0", "linux", "amd64", makeBinaryScan(makeFinding("LINUX-OLD")))
	linuxNew := makeProviderVersion("1.1.0", "linux", "amd64", makeBinaryScan(makeFinding("LINUX-NEW")))
	darwin := makeProviderVersion("1.0.0", "darwin", "arm64", makeBinaryScan(makeFinding("DARWIN")))
	noScan := makeProviderVersion("2.0.0", "linux", "amd64", nil)
	emptyScan := makeProviderVersion("2.0.0", "darwin", "arm64", makeBinaryScan())

	findings := collectBinaryFindings([]opendepotv1alpha1.Version{linuxOld, linuxNew, darwin, noScan, emptyScan})
	if len(findings) != 2 {
		t.Fatalf("collectBinaryFindings() keys = %d, want 2: %+v", len(findings), findings)
	}

	if got := findings["linux/amd64"]; len(got) != 1 || got[0].VulnerabilityID != "LINUX-NEW" {
		t.Fatalf("linux/amd64 findings = %+v, want LINUX-NEW from latest version", got)
	}

	if got := findings["darwin/arm64"]; len(got) != 1 || got[0].VulnerabilityID != "DARWIN" {
		t.Fatalf("darwin/arm64 findings = %+v, want DARWIN", got)
	}

	pinned := collectBinaryFindingsForVersion([]opendepotv1alpha1.Version{linuxOld, linuxNew, darwin}, "1.0.0")
	if got := pinned["linux/amd64"]; len(got) != 1 || got[0].VulnerabilityID != "LINUX-OLD" {
		t.Fatalf("pinned linux/amd64 findings = %+v, want LINUX-OLD", got)
	}

	if _, ok := pinned["darwin/arm64"]; !ok {
		t.Fatal("pinned darwin/arm64 findings missing for version 1.0.0")
	}
}

func TestBrowseCovProviderVersionsFor(t *testing.T) {
	mine := opendepotv1alpha1.Version{}
	mine.Spec.Version = "1.0.0"
	mine.Spec.ProviderConfigRef = &opendepotv1alpha1.ProviderConfig{Name: ptr("aws")}

	other := opendepotv1alpha1.Version{}
	other.Spec.Version = "1.0.0"
	other.Spec.ProviderConfigRef = &opendepotv1alpha1.ProviderConfig{Name: ptr("gcp")}

	noRef := opendepotv1alpha1.Version{}
	noRef.Spec.Version = "1.0.0"

	got := providerVersionsFor([]opendepotv1alpha1.Version{mine, other, noRef}, "aws")
	if len(got) != 1 || got[0].Spec.ProviderConfigRef.Name == nil || *got[0].Spec.ProviderConfigRef.Name != "aws" {
		t.Fatalf("providerVersionsFor(aws) = %+v, want one aws version", got)
	}
}

func TestBrowseCovEnrichResourceWithDownloads(t *testing.T) {
	resource := BrowseResource{Kind: "module", Namespace: "team", Name: "vpc"}

	enrichResourceWithDownloads(&resource, nil)
	if resource.TotalDownloads != 0 || resource.LastDownloadedAt != "" {
		t.Fatalf("nil stats mutated resource: %+v", resource)
	}

	stats := map[string]resourceDownloadStats{
		"team/module/vpc": {Count: 42, LastAt: "2025-01-02T03:04:05Z"},
	}

	enrichResourceWithDownloads(&resource, stats)
	if resource.TotalDownloads != 42 || resource.LastDownloadedAt != "2025-01-02T03:04:05Z" {
		t.Fatalf("enriched resource = %+v, want 42 downloads at 2025-01-02T03:04:05Z", resource)
	}

	missing := BrowseResource{Kind: "provider", Namespace: "team", Name: "aws"}
	enrichResourceWithDownloads(&missing, stats)
	if missing.TotalDownloads != 0 {
		t.Fatalf("missing key enriched resource = %+v, want no downloads", missing)
	}
}

// browseCovFakeAPI serves the namespace, depot, module, provider, skill, agent, and group binding
// list endpoints the browse handlers read. Any other list path returns an empty list.
func browseCovFakeAPI(t *testing.T, namespaces map[string]map[string]string, lists map[string]any) *httptest.Server {
	t.Helper()

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.URL.Path == "/api/v1/namespaces" {
			selector := r.URL.Query().Get("labelSelector")
			items := make([]map[string]any, 0, len(namespaces))
			for name, labels := range namespaces {
				if selector != "" && labels[labelPublic] != "true" {
					continue
				}

				items = append(items, map[string]any{"metadata": map[string]any{"name": name, "labels": labels}})
			}

			payload, err := json.Marshal(map[string]any{"items": items})
			if err != nil {
				t.Fatalf("marshal namespaces: %v", err)
			}

			_, _ = w.Write(payload)
			return
		}

		for suffix, list := range lists {
			if strings.HasSuffix(r.URL.Path, "/"+suffix) {
				payload, err := json.Marshal(list)
				if err != nil {
					t.Fatalf("marshal %s: %v", suffix, err)
				}

				_, _ = w.Write(payload)
				return
			}
		}

		_, _ = w.Write([]byte(`{"items":[]}`))
	}))
	t.Cleanup(api.Close)

	return api
}

func browseCovDepot(ns, name string, labels map[string]string, modules, providers []string) opendepotv1alpha1.Depot {
	d := opendepotv1alpha1.Depot{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Labels: labels}}
	d.Status.Modules = modules
	d.Status.Providers = providers
	return d
}

func TestBrowseCovNamespacesAndDepots(t *testing.T) {
	publicNS := map[string]string{labelPublic: "true"}
	namespaces := map[string]map[string]string{
		"team":     publicNS,
		"internal": {},
	}

	depots := opendepotv1alpha1.DepotList{Items: []opendepotv1alpha1.Depot{
		browseCovDepot("team", "public-depot", map[string]string{labelPublic: "true"}, []string{"team/vpc"}, []string{"team/aws"}),
		browseCovDepot("internal", "private-depot", nil, nil, nil),
	}}

	lists := map[string]any{"depots": depots}
	api := browseCovFakeAPI(t, namespaces, lists)
	browseAgentTestSetup(t, api, false, false)

	router := chi.NewRouter()
	router.Get("/opendepot/ui/v1/namespaces", handleBrowseNamespaces)
	router.Get("/opendepot/ui/v1/depots", handleBrowseDepots)

	t.Run("namespaces lists only public namespaces for anonymous callers", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodGet, "http://registry.example.com/opendepot/ui/v1/namespaces", nil)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)

		if response.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
		}

		var result BrowseNamespaceList
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatalf("unmarshal namespaces: %v", err)
		}

		if len(result.Items) != 1 || result.Items[0].Name != "team" {
			t.Fatalf("namespaces = %+v, want only team", result.Items)
		}
	})

	t.Run("depots returns only visible depots for anonymous callers", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodGet, "http://registry.example.com/opendepot/ui/v1/depots", nil)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)

		if response.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
		}

		var result BrowseDepotList
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatalf("unmarshal depots: %v", err)
		}

		for _, d := range result.Items {
			if d.Name == "private-depot" {
				t.Fatalf("private depot leaked to anonymous caller: %+v", result.Items)
			}
		}
	})
}

func TestBrowseCovDepotsGraph(t *testing.T) {
	publicLabels := map[string]string{labelPublic: "true"}
	namespaces := map[string]map[string]string{
		"team":     publicLabels,
		"internal": {},
	}

	interval := 30
	depot := browseCovDepot("team", "graph-depot", publicLabels, []string{"team/vpc"}, []string{"team/aws"})
	depot.Spec.PollingIntervalMinutes = &interval
	depot.Status.Skills = []string{"team/reviewer"}
	depot.Status.Agents = []string{"team/triage"}
	depot.Spec.GlobalConfig = &opendepotv1alpha1.GlobalConfig{
		StorageConfig: &opendepotv1alpha1.StorageConfig{GCS: &opendepotv1alpha1.GoogleCloudStorageConfig{Bucket: "graph"}},
	}

	hidden := browseCovDepot("internal", "hidden-depot", nil, []string{"internal/hidden"}, nil)

	publicModule := opendepotv1alpha1.Module{ObjectMeta: metav1.ObjectMeta{Name: "vpc", Namespace: "team", Labels: publicLabels}}
	publicModule.Spec.ModuleConfig.Provider = "aws"
	publicModule.Spec.ModuleConfig.RepoUrl = ptr("https://example.com/vpc")
	publicModule.Status.SyncStatus = "Synced"
	publicModule.Status.LatestVersion = ptr("1.2.3")

	hiddenModule := opendepotv1alpha1.Module{ObjectMeta: metav1.ObjectMeta{Name: "secret", Namespace: "internal"}}
	hiddenModule.Status.SyncStatus = "Failed"

	publicProvider := opendepotv1alpha1.Provider{ObjectMeta: metav1.ObjectMeta{Name: "aws", Namespace: "team", Labels: publicLabels}}
	publicProvider.Spec.ProviderConfig.Namespace = ptr("hashicorp")
	publicProvider.Status.SyncStatus = "Synced"

	lists := map[string]any{
		"depots":    opendepotv1alpha1.DepotList{Items: []opendepotv1alpha1.Depot{depot, hidden}},
		"modules":   opendepotv1alpha1.ModuleList{Items: []opendepotv1alpha1.Module{publicModule, hiddenModule}},
		"providers": opendepotv1alpha1.ProviderList{Items: []opendepotv1alpha1.Provider{publicProvider}},
	}

	api := browseCovFakeAPI(t, namespaces, lists)
	browseAgentTestSetup(t, api, false, false)

	router := chi.NewRouter()
	router.Get("/opendepot/ui/v1/depots/graph", handleBrowseDepotsGraph)

	request := httptest.NewRequest(http.MethodGet, "http://registry.example.com/opendepot/ui/v1/depots/graph", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}

	var graph BrowseDepotGraph
	if err := json.Unmarshal(response.Body.Bytes(), &graph); err != nil {
		t.Fatalf("unmarshal graph: %v", err)
	}

	if len(graph.Depots) != 1 || graph.Depots[0].ID != "depot/team/graph-depot" {
		t.Fatalf("depots = %+v, want only public graph-depot", graph.Depots)
	}

	if graph.Depots[0].StorageBackend != "gcs" || graph.Depots[0].PollingIntervalMinutes == nil || *graph.Depots[0].PollingIntervalMinutes != 30 {
		t.Fatalf("depot node = %+v, want gcs backend and 30 minute polling", graph.Depots[0])
	}

	if len(graph.Modules) != 1 || graph.Modules[0].ID != "module/team/vpc" || !graph.Modules[0].Synced || graph.Modules[0].LatestVersion != "1.2.3" {
		t.Fatalf("modules = %+v, want synced team/vpc at 1.2.3", graph.Modules)
	}

	if len(graph.Providers) != 1 || graph.Providers[0].ProviderNamespace != "hashicorp" {
		t.Fatalf("providers = %+v, want aws with hashicorp namespace", graph.Providers)
	}

	if len(graph.Edges) != 2 {
		t.Fatalf("edges = %+v, want depot→module and depot→provider", graph.Edges)
	}

	if graph.Summary.TotalDepots != 1 || graph.Summary.TotalModules != 1 || graph.Summary.TotalProviders != 1 {
		t.Fatalf("summary = %+v, want one depot, module, and provider", graph.Summary)
	}

	if graph.GeneratedAt == "" {
		t.Fatal("generatedAt is empty")
	}
}

func TestBrowseCovDepotsGraphNamespaceScope(t *testing.T) {
	publicLabels := map[string]string{labelPublic: "true"}
	namespaces := map[string]map[string]string{"team": publicLabels}

	depot := browseCovDepot("team", "scoped", publicLabels, nil, nil)
	lists := map[string]any{
		"depots": opendepotv1alpha1.DepotList{Items: []opendepotv1alpha1.Depot{depot}},
	}

	api := browseCovFakeAPI(t, namespaces, lists)
	browseAgentTestSetup(t, api, false, false)

	router := chi.NewRouter()
	router.Get("/opendepot/ui/v1/depots/graph", handleBrowseDepotsGraph)

	request := httptest.NewRequest(http.MethodGet, "http://registry.example.com/opendepot/ui/v1/depots/graph?namespace=team", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}

	var graph BrowseDepotGraph
	if err := json.Unmarshal(response.Body.Bytes(), &graph); err != nil {
		t.Fatalf("unmarshal graph: %v", err)
	}

	if len(graph.Depots) != 1 || graph.Depots[0].Name != "scoped" {
		t.Fatalf("namespace-scoped depots = %+v, want scoped", graph.Depots)
	}
}
