package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
)

var errWave7NNoCluster = errors.New("no cluster config")

func wave7nDepotRouter() http.Handler {
	router := chi.NewRouter()
	router.Get("/opendepot/ui/v1/depots", handleBrowseDepots)
	router.Get("/opendepot/ui/v1/depots/graph", handleBrowseDepotsGraph)

	return router
}

func wave7nPublicNamespace() covGResponse {
	return covGResponse{body: map[string]any{"items": []any{
		map[string]any{"metadata": map[string]any{"name": "team", "labels": map[string]string{labelPublic: "true"}}},
		map[string]any{"metadata": map[string]any{"name": "internal", "labels": map[string]string{}}},
	}}}
}

func wave7nDepot(ns, name string, public bool) opendepotv1alpha1.Depot {
	d := opendepotv1alpha1.Depot{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns}}
	if public {
		d.Labels = map[string]string{labelPublic: "true"}
	}

	return d
}

func TestWave7NBrowseDepotsErrorPaths(t *testing.T) {
	depots := wave7nJSON(t, opendepotv1alpha1.DepotList{Items: []opendepotv1alpha1.Depot{wave7nDepot("team", "d", true)}})
	routes := map[string]covGResponse{
		"/apis/opendepot.defdev.io/v1alpha1/depots": {raw: depots},
		"/api/v1/namespaces":                        wave7nPublicNamespace(),
	}

	t.Run("depot list failure is a server error", func(t *testing.T) {
		api := covGAPIServer(t, map[string]covGResponse{
			"/apis/opendepot.defdev.io/v1alpha1/depots": {status: http.StatusInternalServerError, raw: `{}`},
			"/api/v1/namespaces":                        wave7nPublicNamespace(),
		})
		covGUseCluster(t, api)

		response := httptest.NewRecorder()
		wave7nDepotRouter().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/opendepot/ui/v1/depots", nil))

		if response.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
		}
	})

	t.Run("malformed depot list is a server error", func(t *testing.T) {
		api := covGAPIServer(t, map[string]covGResponse{
			"/apis/opendepot.defdev.io/v1alpha1/depots": {raw: `not-json`},
			"/api/v1/namespaces":                        wave7nPublicNamespace(),
		})
		covGUseCluster(t, api)

		response := httptest.NewRecorder()
		wave7nDepotRouter().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/opendepot/ui/v1/depots", nil))

		if response.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
		}
	})

	t.Run("namespace list failure is a server error", func(t *testing.T) {
		api := covGAPIServer(t, map[string]covGResponse{
			"/apis/opendepot.defdev.io/v1alpha1/depots": {raw: depots},
			"/api/v1/namespaces":                        {status: http.StatusInternalServerError, raw: `{}`},
		})
		covGUseCluster(t, api)

		response := httptest.NewRecorder()
		wave7nDepotRouter().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/opendepot/ui/v1/depots", nil))

		if response.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
		}
	})

	t.Run("cluster config failure is a server error", func(t *testing.T) {
		api := covGAPIServer(t, routes)
		covGUseCluster(t, api)

		original := inClusterConfig
		t.Cleanup(func() { inClusterConfig = original })
		inClusterConfig = func() (*rest.Config, error) {
			return nil, errWave7NNoCluster
		}

		response := httptest.NewRecorder()
		wave7nDepotRouter().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/opendepot/ui/v1/depots", nil))

		if response.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
		}
	})
}

func TestWave7NBrowseDepotsVisibilityAndStorage(t *testing.T) {
	private := wave7nDepot("internal", "hidden", false)
	public := wave7nDepot("team", "shown", true)
	public.Spec.GlobalConfig = &opendepotv1alpha1.GlobalConfig{
		StorageConfig: &opendepotv1alpha1.StorageConfig{GCS: &opendepotv1alpha1.GoogleCloudStorageConfig{Bucket: "graph"}},
	}
	public.Status.Modules = []string{"team/vpc"}

	depots := opendepotv1alpha1.DepotList{Items: []opendepotv1alpha1.Depot{public, private}}
	api := covGAPIServer(t, map[string]covGResponse{
		"/apis/opendepot.defdev.io/v1alpha1/depots": {body: depots},
		"/api/v1/namespaces":                        wave7nPublicNamespace(),
	})
	covGUseCluster(t, api)

	response := httptest.NewRecorder()
	wave7nDepotRouter().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/opendepot/ui/v1/depots", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}

	var result BrowseDepotList
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(result.Items) != 2 || result.Items[0].Name != "shown" || result.Items[0].StorageBackend != "gcs" {
		t.Fatalf("depots = %+v, want shown with gcs backend and anonymous access to hidden", result.Items)
	}
}

func TestWave7NBrowseDepotsPrivateRequiresBinding(t *testing.T) {
	private := wave7nDepot("internal", "hidden", false)
	api := covGAPIServer(t, map[string]covGResponse{
		"/apis/opendepot.defdev.io/v1alpha1/depots": {body: opendepotv1alpha1.DepotList{Items: []opendepotv1alpha1.Depot{private}}},
		"/api/v1/namespaces":                        wave7nPublicNamespace(),
	})
	covGUseCluster(t, api)

	original := opendepotAnonymousAuth
	t.Cleanup(func() { opendepotAnonymousAuth = original })
	anonymous := false
	opendepotAnonymousAuth = &anonymous

	response := httptest.NewRecorder()
	wave7nDepotRouter().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/opendepot/ui/v1/depots", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}

	var result BrowseDepotList
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(result.Items) != 0 {
		t.Fatalf("depots = %+v, want none for unauthenticated non-public callers", result.Items)
	}
}

func TestWave7NBrowseDepotsGraphErrorPaths(t *testing.T) {
	ok := map[string]covGResponse{
		"/apis/opendepot.defdev.io/v1alpha1/depots":    {body: opendepotv1alpha1.DepotList{}},
		"/apis/opendepot.defdev.io/v1alpha1/modules":   {body: opendepotv1alpha1.ModuleList{}},
		"/apis/opendepot.defdev.io/v1alpha1/providers": {body: opendepotv1alpha1.ProviderList{}},
		"/api/v1/namespaces":                           wave7nPublicNamespace(),
	}

	cases := map[string]string{
		"depots":    "/apis/opendepot.defdev.io/v1alpha1/depots",
		"modules":   "/apis/opendepot.defdev.io/v1alpha1/modules",
		"providers": "/apis/opendepot.defdev.io/v1alpha1/providers",
	}

	for name, path := range cases {
		t.Run(name+" list failure is a server error", func(t *testing.T) {
			routes := make(map[string]covGResponse, len(ok))
			for k, v := range ok {
				routes[k] = v
			}
			routes[path] = covGResponse{status: http.StatusInternalServerError, raw: `{}`}

			covGUseCluster(t, covGAPIServer(t, routes))

			response := httptest.NewRecorder()
			wave7nDepotRouter().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/opendepot/ui/v1/depots/graph", nil))

			if response.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
			}
		})

		t.Run(name+" malformed list is a server error", func(t *testing.T) {
			routes := make(map[string]covGResponse, len(ok))
			for k, v := range ok {
				routes[k] = v
			}
			routes[path] = covGResponse{raw: `not-json`}

			covGUseCluster(t, covGAPIServer(t, routes))

			response := httptest.NewRecorder()
			wave7nDepotRouter().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/opendepot/ui/v1/depots/graph", nil))

			if response.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
			}
		})
	}

	t.Run("namespace list failure is a server error", func(t *testing.T) {
		routes := make(map[string]covGResponse, len(ok))
		for k, v := range ok {
			routes[k] = v
		}
		routes["/api/v1/namespaces"] = covGResponse{status: http.StatusInternalServerError, raw: `{}`}

		covGUseCluster(t, covGAPIServer(t, routes))

		response := httptest.NewRecorder()
		wave7nDepotRouter().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/opendepot/ui/v1/depots/graph", nil))

		if response.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
		}
	})
}

func TestWave7NBrowseDepotsGraphAgentNodes(t *testing.T) {
	latest := "2.0.0"
	skill := opendepotv1alpha1.Skill{ObjectMeta: metav1.ObjectMeta{Name: "reviewer", Namespace: "team", Labels: map[string]string{labelPublic: "true"}}}
	skill.Status.Synced = true
	skill.Status.SyncStatus = "Synced"
	skill.Status.LatestVersion = &latest

	agent := opendepotv1alpha1.Agent{ObjectMeta: metav1.ObjectMeta{Name: "triage", Namespace: "team", Labels: map[string]string{labelPublic: "true"}}}
	agent.Status.SyncStatus = "Pending"

	routes := map[string]covGResponse{
		"/apis/opendepot.defdev.io/v1alpha1/depots":    {body: opendepotv1alpha1.DepotList{}},
		"/apis/opendepot.defdev.io/v1alpha1/modules":   {body: opendepotv1alpha1.ModuleList{}},
		"/apis/opendepot.defdev.io/v1alpha1/providers": {body: opendepotv1alpha1.ProviderList{}},
		"/apis/opendepot.defdev.io/v1alpha1/skills":    {body: map[string]any{"items": []opendepotv1alpha1.Skill{skill}}},
		"/apis/opendepot.defdev.io/v1alpha1/agents":    {body: map[string]any{"items": []opendepotv1alpha1.Agent{agent}}},
		"/api/v1/namespaces":                           wave7nPublicNamespace(),
	}

	covGUseCluster(t, covGAPIServer(t, routes))

	response := httptest.NewRecorder()
	wave7nDepotRouter().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/opendepot/ui/v1/depots/graph", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}

	var graph BrowseDepotGraph
	if err := json.Unmarshal(response.Body.Bytes(), &graph); err != nil {
		t.Fatalf("unmarshal graph: %v", err)
	}

	if len(graph.Skills) != 1 || graph.Skills[0].LatestVersion != "2.0.0" || !graph.Skills[0].Synced {
		t.Fatalf("skills = %+v, want synced reviewer at 2.0.0", graph.Skills)
	}

	if len(graph.Agents) != 1 || graph.Agents[0].ID != "agent/team/triage" || graph.Agents[0].Synced {
		t.Fatalf("agents = %+v, want unsynced triage", graph.Agents)
	}
}

func TestWave7NBrowseDepotsGraphAgentListFailure(t *testing.T) {
	routes := map[string]covGResponse{
		"/apis/opendepot.defdev.io/v1alpha1/depots":    {body: opendepotv1alpha1.DepotList{}},
		"/apis/opendepot.defdev.io/v1alpha1/modules":   {body: opendepotv1alpha1.ModuleList{}},
		"/apis/opendepot.defdev.io/v1alpha1/providers": {body: opendepotv1alpha1.ProviderList{}},
		"/apis/opendepot.defdev.io/v1alpha1/skills":    {status: http.StatusInternalServerError, raw: `{}`},
		"/api/v1/namespaces":                           wave7nPublicNamespace(),
	}

	covGUseCluster(t, covGAPIServer(t, routes))

	response := httptest.NewRecorder()
	wave7nDepotRouter().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/opendepot/ui/v1/depots/graph", nil))

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
}
