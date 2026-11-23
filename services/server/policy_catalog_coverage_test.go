package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"k8s.io/client-go/kubernetes"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
)

// covGCatalogName is one named resource entry in the catalog response.
type covGCatalogName struct {
	Name string `json:"name"`
}

// covGCatalogResponse decodes the JSON body returned by the scan policy catalog endpoint.
type covGCatalogResponse struct {
	WritesEnabled bool `json:"writesEnabled"`
	Items         []struct {
		Namespace                      string            `json:"namespace"`
		CanRead                        bool              `json:"canRead"`
		CanWrite                       bool              `json:"canWrite"`
		CanManageNamespaceWidePolicies bool              `json:"canManageNamespaceWidePolicies"`
		Modules                        []covGCatalogName `json:"modules"`
		Providers                      []covGCatalogName `json:"providers"`
		Skills                         []covGCatalogName `json:"skills"`
		Agents                         []covGCatalogName `json:"agents"`
	} `json:"items"`
}

// covGCatalogNameList extracts the names from a catalog resource list.
func covGCatalogNameList(items []covGCatalogName) []string {
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item.Name)
	}

	return names
}

func covGCatalogAuth(clientset *kubernetes.Clientset, binding *opendepotv1alpha1.SecurityGroupBinding) func(http.ResponseWriter, *http.Request) (*kubernetes.Clientset, *opendepotv1alpha1.GroupBinding, *opendepotv1alpha1.SecurityGroupBinding, string, error) {
	return func(http.ResponseWriter, *http.Request) (*kubernetes.Clientset, *opendepotv1alpha1.GroupBinding, *opendepotv1alpha1.SecurityGroupBinding, string, error) {
		return clientset, nil, binding, "test-user", nil
	}
}

func covGCatalogList(kind string, names ...string) covGResponse {
	items := make([]map[string]any, 0, len(names))
	for _, name := range names {
		items = append(items, map[string]any{
			"apiVersion": "opendepot.defdev.io/v1alpha1",
			"kind":       kind,
			"metadata":   map[string]any{"name": name, "namespace": "team"},
		})
	}

	return covGResponse{body: map[string]any{
		"apiVersion": "opendepot.defdev.io/v1alpha1",
		"kind":       kind + "List",
		"items":      items,
	}}
}

func covGCatalogRoutes() map[string]covGResponse {
	return map[string]covGResponse{
		"/apis/opendepot.defdev.io/v1alpha1/namespaces/team/modules":    covGCatalogList("Module", "vpc", "network-core", "db"),
		"/apis/opendepot.defdev.io/v1alpha1/namespaces/team/providers":  covGCatalogList("Provider", "aws", "azurerm"),
		"/apis/opendepot.defdev.io/v1alpha1/namespaces/team/skills":     covGCatalogList("Skill", "terraform-review"),
		"/apis/opendepot.defdev.io/v1alpha1/namespaces/team/agents":     covGCatalogList("Agent", "reviewer"),
		"/apis/opendepot.defdev.io/v1alpha1/namespaces/ops/modules":     covGCatalogList("Module", "vpc"),
		"/apis/opendepot.defdev.io/v1alpha1/namespaces/ops/providers":   covGCatalogList("Provider"),
		"/apis/opendepot.defdev.io/v1alpha1/namespaces/ops/skills":      covGCatalogList("Skill"),
		"/apis/opendepot.defdev.io/v1alpha1/namespaces/ops/agents":      covGCatalogList("Agent"),
		"/apis/opendepot.defdev.io/v1alpha1/namespaces/empty/modules":   covGCatalogList("Module"),
		"/apis/opendepot.defdev.io/v1alpha1/namespaces/empty/providers": covGCatalogList("Provider"),
		"/apis/opendepot.defdev.io/v1alpha1/namespaces/empty/skills":    covGCatalogList("Skill"),
		"/apis/opendepot.defdev.io/v1alpha1/namespaces/empty/agents":    covGCatalogList("Agent"),
		"/api/v1/namespaces": {body: map[string]any{
			"apiVersion": "v1",
			"kind":       "NamespaceList",
			"items": []map[string]any{
				{"apiVersion": "v1", "kind": "Namespace", "metadata": map[string]any{"name": "team"}},
				{"apiVersion": "v1", "kind": "Namespace", "metadata": map[string]any{"name": "empty"}},
				{"apiVersion": "v1", "kind": "Namespace", "metadata": map[string]any{"name": "ops"}},
			},
		}},
	}
}

func covGCatalogSetup(t *testing.T, anonymous, enabled bool) {
	t.Helper()

	originalAnonymous := opendepotAnonymousAuth
	originalEnabled := opendepotPolicyManagementEnabled
	originalVerifier := oidcVerifier
	originalAuth := policyRequestAuth
	t.Cleanup(func() {
		opendepotAnonymousAuth = originalAnonymous
		opendepotPolicyManagementEnabled = originalEnabled
		oidcVerifier = originalVerifier
		policyRequestAuth = originalAuth
	})

	opendepotAnonymousAuth = &anonymous
	opendepotPolicyManagementEnabled = &enabled
	oidcVerifier = nil
}

func TestCovGHandleScanPolicyCatalog(t *testing.T) {
	t.Run("nil binding with no OIDC returns an empty catalog", func(t *testing.T) {
		anonymous := false
		enabled := true
		covGCatalogSetup(t, anonymous, enabled)

		api := covGAPIServer(t, covGCatalogRoutes())
		clientset := policyTestClientset(t, api.URL)
		policyRequestAuth = covGCatalogAuth(clientset, nil)

		recorder := httptest.NewRecorder()
		handleScanPolicyCatalog(recorder, covGRequest(http.MethodGet, "/opendepot/ui/v1/scan-policies/catalog", nil))

		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
		}

		var body covGCatalogResponse
		if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode catalog: %v", err)
		}

		if !body.WritesEnabled {
			t.Fatalf("writesEnabled = false, want true when policy management is enabled and auth is not anonymous")
		}

		if len(body.Items) != 0 {
			t.Fatalf("items = %+v, want none without a binding", body.Items)
		}
	})

	t.Run("anonymous auth disables writes", func(t *testing.T) {
		covGCatalogSetup(t, true, true)

		api := covGAPIServer(t, covGCatalogRoutes())
		clientset := policyTestClientset(t, api.URL)
		policyRequestAuth = covGCatalogAuth(clientset, nil)

		recorder := httptest.NewRecorder()
		handleScanPolicyCatalog(recorder, covGRequest(http.MethodGet, "/opendepot/ui/v1/scan-policies/catalog", nil))

		var body covGCatalogResponse
		if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode catalog: %v", err)
		}

		if body.WritesEnabled {
			t.Fatalf("writesEnabled = true, want false under anonymous auth")
		}
	})

	t.Run("OIDC without a binding is forbidden", func(t *testing.T) {
		covGCatalogSetup(t, false, true)
		oidcVerifier = &gooidc.IDTokenVerifier{}

		api := covGAPIServer(t, covGCatalogRoutes())
		clientset := policyTestClientset(t, api.URL)
		policyRequestAuth = covGCatalogAuth(clientset, nil)

		recorder := httptest.NewRecorder()
		handleScanPolicyCatalog(recorder, covGRequest(http.MethodGet, "/opendepot/ui/v1/scan-policies/catalog", nil))

		if recorder.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusForbidden)
		}
	})

	t.Run("auth failure stops before any Kubernetes call", func(t *testing.T) {
		covGCatalogSetup(t, false, true)
		policyRequestAuth = func(w http.ResponseWriter, _ *http.Request) (*kubernetes.Clientset, *opendepotv1alpha1.GroupBinding, *opendepotv1alpha1.SecurityGroupBinding, string, error) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)

			return nil, nil, nil, "", http.ErrNoCookie
		}

		recorder := httptest.NewRecorder()
		handleScanPolicyCatalog(recorder, covGRequest(http.MethodGet, "/opendepot/ui/v1/scan-policies/catalog", nil))

		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
		}
	})

	t.Run("scoped binding lists only allowed namespaces and resources", func(t *testing.T) {
		covGCatalogSetup(t, false, true)

		binding := &opendepotv1alpha1.SecurityGroupBinding{
			Spec: opendepotv1alpha1.SecurityGroupBindingSpec{
				Namespaces:        []string{"team", "ops", "empty"},
				ModuleResources:   []string{"vpc", "network-*"},
				ProviderResources: []string{"aws"},
				SkillResources:    []string{"terraform-*"},
				AgentResources:    []string{"reviewer"},
			},
		}

		api := covGAPIServer(t, covGCatalogRoutes())
		clientset := policyTestClientset(t, api.URL)
		policyRequestAuth = covGCatalogAuth(clientset, binding)

		recorder := httptest.NewRecorder()
		handleScanPolicyCatalog(recorder, covGRequest(http.MethodGet, "/opendepot/ui/v1/scan-policies/catalog", nil))

		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
		}

		var body covGCatalogResponse
		if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode catalog: %v", err)
		}

		// "empty" has no resources and no namespace-wide management, so it is dropped.
		var namespaces []string
		for _, item := range body.Items {
			namespaces = append(namespaces, item.Namespace)
		}

		if want := []string{"ops", "team"}; !reflect.DeepEqual(namespaces, want) {
			t.Fatalf("namespaces = %v, want %v", namespaces, want)
		}

		team := body.Items[1]
		if !team.CanRead || !team.CanWrite {
			t.Fatalf("team canRead=%v canWrite=%v, want both true", team.CanRead, team.CanWrite)
		}

		if team.CanManageNamespaceWidePolicies {
			t.Fatalf("team canManageNamespaceWidePolicies = true, want false for a binding without it")
		}

		wantModules := []string{"network-core", "vpc"}
		gotModules := covGCatalogNameList(team.Modules)
		if !reflect.DeepEqual(gotModules, wantModules) {
			t.Fatalf("team modules = %v, want %v", gotModules, wantModules)
		}

		gotProviders := covGCatalogNameList(team.Providers)
		if want := []string{"aws"}; !reflect.DeepEqual(gotProviders, want) {
			t.Fatalf("team providers = %v, want %v", gotProviders, want)
		}

		gotSkills := covGCatalogNameList(team.Skills)
		if want := []string{"terraform-review"}; !reflect.DeepEqual(gotSkills, want) {
			t.Fatalf("team skills = %v, want %v", gotSkills, want)
		}

		gotAgents := covGCatalogNameList(team.Agents)
		if want := []string{"reviewer"}; !reflect.DeepEqual(gotAgents, want) {
			t.Fatalf("team agents = %v, want %v", gotAgents, want)
		}

		ops := body.Items[0]
		if len(ops.Modules) != 1 || ops.Modules[0].Name != "vpc" {
			t.Fatalf("ops modules = %+v, want only vpc", ops.Modules)
		}

		if len(ops.Providers) != 0 || len(ops.Skills) != 0 || len(ops.Agents) != 0 {
			t.Fatalf("ops providers/skills/agents = %+v/%+v/%+v, want none", ops.Providers, ops.Skills, ops.Agents)
		}
	})

	t.Run("namespace-wide policy management keeps an otherwise empty namespace", func(t *testing.T) {
		covGCatalogSetup(t, false, true)

		binding := &opendepotv1alpha1.SecurityGroupBinding{
			Spec: opendepotv1alpha1.SecurityGroupBindingSpec{
				Namespaces:                    []string{"empty"},
				NamespaceWidePolicyManagement: true,
			},
		}

		api := covGAPIServer(t, covGCatalogRoutes())
		clientset := policyTestClientset(t, api.URL)
		policyRequestAuth = covGCatalogAuth(clientset, binding)

		recorder := httptest.NewRecorder()
		handleScanPolicyCatalog(recorder, covGRequest(http.MethodGet, "/opendepot/ui/v1/scan-policies/catalog", nil))

		var body covGCatalogResponse
		if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode catalog: %v", err)
		}

		if len(body.Items) != 1 || body.Items[0].Namespace != "empty" {
			t.Fatalf("items = %+v, want the empty namespace kept", body.Items)
		}

		if !body.Items[0].CanManageNamespaceWidePolicies {
			t.Fatalf("canManageNamespaceWidePolicies = false, want true")
		}
	})

	t.Run("wildcard namespace expands to every cluster namespace", func(t *testing.T) {
		covGCatalogSetup(t, false, true)

		binding := &opendepotv1alpha1.SecurityGroupBinding{
			Spec: opendepotv1alpha1.SecurityGroupBindingSpec{
				Namespaces:      []string{"*"},
				ModuleResources: []string{"*"},
			},
		}

		api := covGAPIServer(t, covGCatalogRoutes())
		clientset := policyTestClientset(t, api.URL)
		policyRequestAuth = covGCatalogAuth(clientset, binding)

		recorder := httptest.NewRecorder()
		handleScanPolicyCatalog(recorder, covGRequest(http.MethodGet, "/opendepot/ui/v1/scan-policies/catalog", nil))

		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
		}

		var body covGCatalogResponse
		if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode catalog: %v", err)
		}

		var namespaces []string
		for _, item := range body.Items {
			namespaces = append(namespaces, item.Namespace)
		}

		// "empty" holds no module, so it is dropped; the cluster list is sorted.
		if want := []string{"ops", "team"}; !reflect.DeepEqual(namespaces, want) {
			t.Fatalf("namespaces = %v, want %v", namespaces, want)
		}
	})

	t.Run("namespace list failure maps the Kubernetes status", func(t *testing.T) {
		covGCatalogSetup(t, false, true)

		binding := &opendepotv1alpha1.SecurityGroupBinding{
			Spec: opendepotv1alpha1.SecurityGroupBindingSpec{
				Namespaces: []string{"*"},
			},
		}

		api := covGAPIServer(t, map[string]covGResponse{
			"/api/v1/namespaces": {status: http.StatusForbidden, raw: `{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"Forbidden","code":403}`},
		})
		clientset := policyTestClientset(t, api.URL)
		policyRequestAuth = covGCatalogAuth(clientset, binding)

		recorder := httptest.NewRecorder()
		handleScanPolicyCatalog(recorder, covGRequest(http.MethodGet, "/opendepot/ui/v1/scan-policies/catalog", nil))

		if recorder.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusForbidden)
		}
	})

	t.Run("resource list failure maps the Kubernetes status", func(t *testing.T) {
		covGCatalogSetup(t, false, true)

		binding := &opendepotv1alpha1.SecurityGroupBinding{
			Spec: opendepotv1alpha1.SecurityGroupBindingSpec{
				Namespaces:      []string{"team"},
				ModuleResources: []string{"*"},
			},
		}

		routes := covGCatalogRoutes()
		delete(routes, "/apis/opendepot.defdev.io/v1alpha1/namespaces/team/modules")

		api := covGAPIServer(t, routes)
		clientset := policyTestClientset(t, api.URL)
		policyRequestAuth = covGCatalogAuth(clientset, binding)

		recorder := httptest.NewRecorder()
		handleScanPolicyCatalog(recorder, covGRequest(http.MethodGet, "/opendepot/ui/v1/scan-policies/catalog", nil))

		if recorder.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
		}
	})

	t.Run("malformed resource list returns an internal error", func(t *testing.T) {
		covGCatalogSetup(t, false, true)

		binding := &opendepotv1alpha1.SecurityGroupBinding{
			Spec: opendepotv1alpha1.SecurityGroupBindingSpec{
				Namespaces:      []string{"team"},
				ModuleResources: []string{"*"},
			},
		}

		routes := covGCatalogRoutes()
		routes["/apis/opendepot.defdev.io/v1alpha1/namespaces/team/modules"] = covGResponse{raw: `not-json`}

		api := covGAPIServer(t, routes)
		clientset := policyTestClientset(t, api.URL)
		policyRequestAuth = covGCatalogAuth(clientset, binding)

		recorder := httptest.NewRecorder()
		handleScanPolicyCatalog(recorder, covGRequest(http.MethodGet, "/opendepot/ui/v1/scan-policies/catalog", nil))

		if recorder.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
		}
	})
}
