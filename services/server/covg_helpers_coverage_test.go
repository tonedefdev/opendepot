package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"k8s.io/client-go/rest"
)

// covGResponse is a canned Kubernetes API response served for one exact request path.
type covGResponse struct {
	status int
	body   any
	raw    string
}

// covGAPIServer serves canned responses keyed by exact request path. Paths without a route return a Kubernetes NotFound Status.
func covGAPIServer(t *testing.T, routes map[string]covGResponse) *httptest.Server {
	t.Helper()

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		route, ok := routes[r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"NotFound","code":404}`))

			return
		}

		if route.status != 0 {
			w.WriteHeader(route.status)
		}

		if route.raw != "" {
			_, _ = w.Write([]byte(route.raw))

			return
		}

		payload, err := json.Marshal(route.body)
		if err != nil {
			t.Errorf("marshal fake API payload for %s: %v", r.URL.Path, err)

			return
		}

		_, _ = w.Write(payload)
	}))
	t.Cleanup(api.Close)

	return api
}

// covGUseCluster points the in-cluster config at the fake API and enables anonymous auth for the test.
func covGUseCluster(t *testing.T, api *httptest.Server) {
	t.Helper()

	originalInClusterConfig := inClusterConfig
	t.Cleanup(func() { inClusterConfig = originalInClusterConfig })
	inClusterConfig = func() (*rest.Config, error) {
		return &rest.Config{Host: api.URL}, nil
	}

	agentTestAnonymousAuth(t)
}

// covGRequest builds a request with the given chi URL parameters attached to its route context.
func covGRequest(method, target string, params map[string]string) *http.Request {
	rctx := chi.NewRouteContext()
	for key, value := range params {
		rctx.URLParams.Add(key, value)
	}

	request := httptest.NewRequest(method, target, nil)

	return request.WithContext(context.WithValue(request.Context(), chi.RouteCtxKey, rctx))
}
