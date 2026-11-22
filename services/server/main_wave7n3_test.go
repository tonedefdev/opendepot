package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func wave7nRegisteredRoutes(t *testing.T, router chi.Routes) map[string]bool {
	t.Helper()

	routes := make(map[string]bool)
	err := chi.Walk(router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		routes[method+" "+route] = true
		return nil
	})
	if err != nil {
		t.Fatalf("walk routes: %v", err)
	}

	return routes
}

func TestWave7NServerRouterRegistersCoreRoutes(t *testing.T) {
	router, err := newServerRouter(false, false, "")
	if err != nil {
		t.Fatalf("newServerRouter: %v", err)
	}

	routes := wave7nRegisteredRoutes(t, router)

	for _, want := range []string{
		"GET /.well-known/terraform.json",
		"GET /opendepot/modules/v1/{namespace}/{name}/{system}/versions",
		"GET /opendepot/providers/v1/{namespace}/{type}/versions",
		"GET /opendepot/agents/v1/catalog",
		"GET /opendepot/cli/v1/SHA256SUMS",
		"GET /opendepot/ui/v1/resources/{namespace}/{kind}/{name}/scan-findings",
		"GET /opendepot/ui/v1/depots/graph",
		"GET /opendepot/ui/v1/stats",
		"POST /opendepot/ui/v1/scan-policies/{namespace}/preview",
	} {
		if !routes[want] {
			t.Fatalf("route %q not registered; routes = %v", want, routes)
		}
	}
}

func TestWave7NServerRouterAssemblyToggle(t *testing.T) {
	const schema = "GET /opendepot/ui/v1/resources/{namespace}/provider/{name}/provider-schema"
	const export = "POST /opendepot/ui/v1/assembly/export"

	disabled := wave7nRegisteredRoutes(t, mustWave7NRouter(t, false, false, ""))
	if disabled[schema] || disabled[export] {
		t.Fatal("assembly routes registered while assembly is disabled")
	}

	enabled := wave7nRegisteredRoutes(t, mustWave7NRouter(t, true, false, ""))
	if !enabled[schema] || !enabled[export] {
		t.Fatalf("assembly routes missing while assembly is enabled; routes = %v", enabled)
	}
}

func wave7nHasDexRoute(routes map[string]bool) bool {
	for route := range routes {
		if strings.HasSuffix(route, " /dex/*") {
			return true
		}
	}

	return false
}

func TestWave7NServerRouterDexProxyToggle(t *testing.T) {
	if wave7nHasDexRoute(wave7nRegisteredRoutes(t, mustWave7NRouter(t, false, false, ""))) {
		t.Fatal("dex proxy mounted while dex proxy is disabled")
	}

	if !wave7nHasDexRoute(wave7nRegisteredRoutes(t, mustWave7NRouter(t, false, true, "http://dex.internal:5556"))) {
		t.Fatal("dex proxy not mounted while dex proxy is enabled")
	}
}

func TestWave7NServerRouterDexProxyRejectsInvalidURL(t *testing.T) {
	if _, err := newServerRouter(false, true, "http://[::1"); err == nil {
		t.Fatal("expected invalid dex internal URL to fail router construction")
	}
}

func TestWave7NServerRouterServesDiscovery(t *testing.T) {
	router := mustWave7NRouter(t, false, false, "")

	request := httptest.NewRequest(http.MethodGet, "/.well-known/terraform.json", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}
}

func mustWave7NRouter(t *testing.T, assemblyEnabled, dexProxyEnabled bool, dexInternalURL string) *chi.Mux {
	t.Helper()

	router, err := newServerRouter(assemblyEnabled, dexProxyEnabled, dexInternalURL)
	if err != nil {
		t.Fatalf("newServerRouter: %v", err)
	}

	return router
}
