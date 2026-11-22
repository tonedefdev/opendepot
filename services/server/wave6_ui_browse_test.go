package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
)

// wave6BrowseListAPI serves a public namespace, modules, providers, and their versions for list handler tests.
// failPath, when set, makes any request whose path ends with it return 500.
func wave6BrowseListAPI(t *testing.T, modules []opendepotv1alpha1.Module, providers []opendepotv1alpha1.Provider, failPath string) *httptest.Server {
	t.Helper()

	namespaces := map[string]any{"items": []map[string]any{
		{"metadata": map[string]any{"name": "team", "labels": map[string]string{labelPublic: "true"}}},
	}}

	moduleList := opendepotv1alpha1.ModuleList{Items: modules}
	providerList := opendepotv1alpha1.ProviderList{Items: providers}

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if failPath != "" && strings.HasSuffix(r.URL.Path, failPath) {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}

		switch {
		case r.URL.Path == "/api/v1/namespaces":
			payload, _ := json.Marshal(namespaces)
			_, _ = w.Write(payload)
		case strings.HasSuffix(r.URL.Path, "/modules"):
			payload, _ := json.Marshal(moduleList)
			_, _ = w.Write(payload)
		case strings.HasSuffix(r.URL.Path, "/providers"):
			payload, _ := json.Marshal(providerList)
			_, _ = w.Write(payload)
		default:
			_, _ = w.Write([]byte(`{"items":[]}`))
		}
	}))
	t.Cleanup(api.Close)

	return api
}

func wave6Module(name string, labels map[string]string) opendepotv1alpha1.Module {
	return opendepotv1alpha1.Module{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "team", Labels: labels},
		Status:     opendepotv1alpha1.ModuleStatus{Synced: true},
	}
}

func wave6Provider(name string, labels map[string]string) opendepotv1alpha1.Provider {
	return opendepotv1alpha1.Provider{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "team", Labels: labels},
		Status:     opendepotv1alpha1.ProviderStatus{Synced: true},
	}
}

func wave6ResourceNames(items []BrowseResource) []string {
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item.Name)
	}

	return names
}

func wave6DecodeResourceList(t *testing.T, body []byte) BrowseResourceList {
	t.Helper()

	var list BrowseResourceList
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatalf("decode resource list: %v; body=%s", err, body)
	}

	return list
}

func wave6ResourcesRouter() http.Handler {
	router := chi.NewRouter()
	router.Get("/opendepot/ui/v1/resources", handleBrowseResources)

	return router
}

func TestWave6BrowseResourcesQueryAndPagination(t *testing.T) {
	publicLabels := map[string]string{labelPublic: "true"}
	modules := []opendepotv1alpha1.Module{
		wave6Module("zeta", publicLabels),
		wave6Module("alpha", publicLabels),
		wave6Module("mid", publicLabels),
	}
	providers := []opendepotv1alpha1.Provider{
		wave6Provider("aws", publicLabels),
	}

	api := wave6BrowseListAPI(t, modules, providers, "")
	browseAgentTestSetup(t, api, true, false)

	router := wave6ResourcesRouter()

	cases := []struct {
		name      string
		target    string
		wantNames []string
		wantTotal int
		wantPage  int
		wantSize  int
	}{
		{
			name:      "default kind lists modules and providers sorted by name",
			target:    "/opendepot/ui/v1/resources",
			wantNames: []string{"alpha", "aws", "mid", "zeta"},
			wantTotal: 4,
			wantPage:  1,
			wantSize:  20,
		},
		{
			name:      "descending sort reverses name order",
			target:    "/opendepot/ui/v1/resources?kind=module&sort_dir=DESC",
			wantNames: []string{"zeta", "mid", "alpha"},
			wantTotal: 3,
			wantPage:  1,
			wantSize:  20,
		},
		{
			name:      "page two of size two returns the trailing item",
			target:    "/opendepot/ui/v1/resources?kind=module&page=2&page_size=2",
			wantNames: []string{"zeta"},
			wantTotal: 3,
			wantPage:  2,
			wantSize:  2,
		},
		{
			name:      "page past the end returns an empty page with the total intact",
			target:    "/opendepot/ui/v1/resources?kind=module&page=9&page_size=2",
			wantNames: []string{},
			wantTotal: 3,
			wantPage:  9,
			wantSize:  2,
		},
		{
			name:      "invalid page and page size fall back to defaults",
			target:    "/opendepot/ui/v1/resources?kind=module&page=abc&page_size=-4",
			wantNames: []string{"alpha", "mid", "zeta"},
			wantTotal: 3,
			wantPage:  1,
			wantSize:  20,
		},
		{
			name:      "page size above the cap is clamped to 100",
			target:    "/opendepot/ui/v1/resources?kind=module&page_size=500",
			wantNames: []string{"alpha", "mid", "zeta"},
			wantTotal: 3,
			wantPage:  1,
			wantSize:  100,
		},
		{
			name:      "provider kind filters to providers only",
			target:    "/opendepot/ui/v1/resources?kind=provider",
			wantNames: []string{"aws"},
			wantTotal: 1,
			wantPage:  1,
			wantSize:  20,
		},
		{
			name:      "search text matches the name case-insensitively",
			target:    "/opendepot/ui/v1/resources?kind=module&q=ALP",
			wantNames: []string{"alpha"},
			wantTotal: 1,
			wantPage:  1,
			wantSize:  20,
		},
		{
			name:      "search with no match returns an empty list",
			target:    "/opendepot/ui/v1/resources?kind=module&q=nothing-here",
			wantNames: []string{},
			wantTotal: 0,
			wantPage:  1,
			wantSize:  20,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.target, nil))

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
			}

			list := wave6DecodeResourceList(t, rec.Body.Bytes())
			got := wave6ResourceNames(list.Items)
			if strings.Join(got, ",") != strings.Join(tc.wantNames, ",") {
				t.Fatalf("names = %v, want %v", got, tc.wantNames)
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

func TestWave6BrowseResourcesUpstreamErrors(t *testing.T) {
	publicLabels := map[string]string{labelPublic: "true"}

	cases := []struct {
		name     string
		failPath string
		target   string
	}{
		{name: "module list failure returns 500", failPath: "/modules", target: "/opendepot/ui/v1/resources?kind=module"},
		{name: "provider list failure returns 500", failPath: "/providers", target: "/opendepot/ui/v1/resources?kind=provider"},
		{name: "versions index failure returns 500", failPath: "/versions", target: "/opendepot/ui/v1/resources?kind=module"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			api := wave6BrowseListAPI(t, []opendepotv1alpha1.Module{wave6Module("alpha", publicLabels)}, nil, tc.failPath)
			browseAgentTestSetup(t, api, true, false)

			rec := httptest.NewRecorder()
			wave6ResourcesRouter().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.target, nil))

			if rec.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want 500; body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestWave6BrowseResourcesPublicOnlyFiltersPrivateResources(t *testing.T) {
	modules := []opendepotv1alpha1.Module{
		wave6Module("open", map[string]string{labelPublic: "true"}),
		wave6Module("hidden", nil),
	}

	api := wave6BrowseListAPI(t, modules, nil, "")
	browseAgentTestSetup(t, api, false, false)

	rec := httptest.NewRecorder()
	wave6ResourcesRouter().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/opendepot/ui/v1/resources?kind=module&public_only=true", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	list := wave6DecodeResourceList(t, rec.Body.Bytes())
	if got := wave6ResourceNames(list.Items); len(got) != 1 || got[0] != "open" {
		t.Fatalf("names = %v, want [open]", got)
	}

	if list.TotalCount != 1 {
		t.Fatalf("totalCount = %d, want 1", list.TotalCount)
	}
}
