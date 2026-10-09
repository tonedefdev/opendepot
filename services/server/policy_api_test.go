package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"github.com/go-chi/chi/v5"
	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

func TestDecodeAndValidateScanPolicy(t *testing.T) {
	t.Run("accepts a valid policy", func(t *testing.T) {
		var policy opendepotv1alpha1.ScanPolicy
		err := decodeAndValidateScanPolicy([]byte(`{
			"metadata":{"name":"release-exemptions"},
			"spec":{"severityThreshold":"HIGH","exemptions":[{"vulnerabilityIDs":["CVE-1"],"reason":"patched next release"}]}
		}`), &policy)
		if err != nil {
			t.Fatalf("decodeAndValidateScanPolicy() error = %v", err)
		}

	})

	tests := []struct {
		name string
		body string
	}{
		{"invalid threshold", `{"metadata":{"name":"policy"},"spec":{"severityThreshold":"URGENT"}}`},
		{"missing exemption reason", `{"metadata":{"name":"policy"},"spec":{"exemptions":[{}]}}`},
		{"invalid target kind", `{"metadata":{"name":"policy"},"spec":{"targetRefs":[{"kind":"Version","name":"v"}]}}`},
		{"unknown field", `{"metadata":{"name":"policy"},"spec":{"unexpected":true}}`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var policy opendepotv1alpha1.ScanPolicy
			if err := decodeAndValidateScanPolicy([]byte(test.body), &policy); err == nil {
				t.Fatal("decodeAndValidateScanPolicy() error = nil, want validation error")
			}
		})
	}
}

func TestFindSecurityGroupBindingFirstMatchAndFailClosed(t *testing.T) {
	originalNamespace := opendepotServerNamespace
	namespace := "opendepot-system"
	opendepotServerNamespace = &namespace
	defer func() { opendepotServerNamespace = originalNamespace }()

	t.Run("first matching binding wins alphabetically", func(t *testing.T) {
		api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"items":[{"metadata":{"name":"20-later"},"spec":{"expression":"\"team\" in groups","namespaces":["later"]}},{"metadata":{"name":"10-first"},"spec":{"expression":"\"team\" in groups","namespaces":["first"]}}]}`))
		}))
		defer api.Close()

		binding, err := findSecurityGroupBinding(context.Background(), policyTestClientset(t, api.URL), []string{"team"})
		if err != nil {
			t.Fatalf("findSecurityGroupBinding() error = %v", err)
		}
		if binding.Name != "10-first" {
			t.Fatalf("binding = %q, want 10-first", binding.Name)
		}
	})

	t.Run("invalid first binding fails closed", func(t *testing.T) {
		api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"items":[{"metadata":{"name":"10-invalid"},"spec":{"expression":"not valid"}},{"metadata":{"name":"20-later"},"spec":{"expression":"true"}}]}`))
		}))
		defer api.Close()

		if _, err := findSecurityGroupBinding(context.Background(), policyTestClientset(t, api.URL), []string{"team"}); err == nil {
			t.Fatal("findSecurityGroupBinding() error = nil, want fail-closed error")
		}
	})
}

func TestSecurityPolicyScopeMatching(t *testing.T) {
	originalVerifier := oidcVerifier
	oidcVerifier = &gooidc.IDTokenVerifier{}
	defer func() { oidcVerifier = originalVerifier }()

	binding := &opendepotv1alpha1.SecurityGroupBinding{Spec: opendepotv1alpha1.SecurityGroupBindingSpec{
		Namespaces:        []string{"team"},
		ModuleResources:   []string{"terraform-aws-*"},
		ProviderResources: []string{"aws"},
	}}

	if !isSecurityResourceAllowed(binding, "module", "terraform-aws-vpc") || isSecurityResourceAllowed(binding, "module", "terraform-azurerm-vpc") {
		t.Fatal("module glob matching is incorrect")
	}
	if !isSecurityResourceAllowed(binding, "provider", "aws") || isSecurityResourceAllowed(binding, "provider", "google") {
		t.Fatal("provider matching is incorrect")
	}

	targeted := &opendepotv1alpha1.ScanPolicy{Spec: opendepotv1alpha1.ScanPolicySpec{
		TargetRefs: []opendepotv1alpha1.ScanPolicyTargetRef{{Kind: "Module", Name: "terraform-aws-vpc"}},
	}}
	if !policyScopeAuthorized(binding, "team", targeted) {
		t.Fatal("authorized targeted policy was denied")
	}

	targeted.Spec.TargetRefs = append(targeted.Spec.TargetRefs, opendepotv1alpha1.ScanPolicyTargetRef{Kind: "Provider", Name: "google"})
	if policyScopeAuthorized(binding, "team", targeted) {
		t.Fatal("policy with a disallowed target was allowed")
	}

	widelyScoped := binding.DeepCopy()
	widelyScoped.Spec.NamespaceWidePolicyManagement = true
	if !policyScopeAuthorized(widelyScoped, "team", &opendepotv1alpha1.ScanPolicy{}) {
		t.Fatal("namespace-wide policy was denied for namespace-wide binding")
	}
}

func TestScanPolicyAuthorization(t *testing.T) {
	originalVerifier := oidcVerifier
	defer func() { oidcVerifier = originalVerifier }()

	oidcVerifier = nil
	if !policyWriteAuthorized(nil, "team") {
		t.Fatal("anonymous mode should not require a GroupBinding")
	}

	oidcVerifier = &gooidc.IDTokenVerifier{}
	if policyWriteAuthorized(nil, "team") {
		t.Fatal("OIDC mode must deny a missing GroupBinding")
	}

	withoutManagement := &opendepotv1alpha1.SecurityGroupBinding{}
	withoutManagement.Spec.Namespaces = []string{"team"}
	if !policyNamespaceAuthorized(withoutManagement, "team") {
		t.Fatal("explicit namespace scope should allow reads")
	}

	withManagement := withoutManagement.DeepCopy()
	withManagement.Spec.NamespaceWidePolicyManagement = true
	if !policyWriteAuthorized(withManagement, "team") {
		t.Fatal("explicit namespace scope should allow writes")
	}

	targeted := &opendepotv1alpha1.ScanPolicy{
		Spec: opendepotv1alpha1.ScanPolicySpec{
			TargetRefs: []opendepotv1alpha1.ScanPolicyTargetRef{{Kind: "Module", Name: "allowed"}},
		},
	}
	withoutManagement.Spec.ModuleResources = []string{"allowed"}
	if !policyReadAuthorized(withoutManagement, "team", targeted) {
		t.Fatal("permitted targeted policies should be readable")
	}

	targeted.Spec.TargetRefs[0].Name = "hidden"
	if policyReadAuthorized(withoutManagement, "team", targeted) {
		t.Fatal("unpermitted targeted policies should be hidden")
	}

	targeted.Spec.TargetRefs = nil
	if policyReadAuthorized(withoutManagement, "team", targeted) {
		t.Fatal("namespace-wide policies should be hidden")
	}
}

func TestRequireIfMatch(t *testing.T) {
	for _, test := range []struct {
		name   string
		header string
		want   string
		ok     bool
	}{
		{name: "missing", ok: false},
		{name: "quoted", header: `"17"`, want: "17", ok: true},
		{name: "wildcard", header: "*", ok: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPut, "/", nil)
			request.Header.Set("If-Match", test.header)
			got, err := requireIfMatch(request)
			if (err == nil) != test.ok {
				t.Fatalf("requireIfMatch() error = %v, want success %t", err, test.ok)
			}
			if got != test.want {
				t.Fatalf("requireIfMatch() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestScanPolicyCapabilitiesAndPreview(t *testing.T) {
	anonymous := true
	enabled := true
	originalAnonymous := opendepotAnonymousAuth
	originalEnabled := opendepotPolicyManagementEnabled
	originalVerifier := oidcVerifier
	defer func() {
		opendepotAnonymousAuth = originalAnonymous
		opendepotPolicyManagementEnabled = originalEnabled
		oidcVerifier = originalVerifier
	}()

	opendepotAnonymousAuth = &anonymous
	opendepotPolicyManagementEnabled = &enabled
	oidcVerifier = nil

	capabilities := httptest.NewRecorder()
	handleScanPolicyCapabilities(capabilities, httptest.NewRequest(http.MethodGet, "/", nil), nil, "team")
	if capabilities.Code != http.StatusOK {
		t.Fatalf("capabilities status = %d, want %d", capabilities.Code, http.StatusOK)
	}

	var value map[string]any
	if err := json.Unmarshal(capabilities.Body.Bytes(), &value); err != nil {
		t.Fatalf("decode capabilities: %v", err)
	}
	if value["writesEnabled"] != false || value["canWrite"] != false || value["supportsPreview"] != true {
		t.Fatalf("unexpected anonymous capabilities: %#v", value)
	}

	preview := httptest.NewRecorder()
	previewRequest := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"metadata":{"name":"preview"},"spec":{"severityThreshold":"HIGH"}}`))
	handleScanPolicyPreview(preview, previewRequest, nil, nil, "team")
	if preview.Code != http.StatusOK {
		t.Fatalf("preview status = %d, want %d", preview.Code, http.StatusOK)
	}
}

func TestValidateScanPolicyTargets(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/apis/opendepot.defdev.io/v1alpha1/namespaces/team/modules":
			_, _ = w.Write([]byte(`{"items":[{"metadata":{"name":"onboarded-module"}}]}`))
		case "/apis/opendepot.defdev.io/v1alpha1/namespaces/team/providers":
			_, _ = w.Write([]byte(`{"items":[{"metadata":{"name":"onboarded-provider"}}]}`))
		case "/apis/opendepot.defdev.io/v1alpha1/namespaces/team/skills":
			_, _ = w.Write([]byte(`{"items":[{"metadata":{"name":"onboarded-skill"}}]}`))
		case "/apis/opendepot.defdev.io/v1alpha1/namespaces/team/agents":
			_, _ = w.Write([]byte(`{"items":[{"metadata":{"name":"onboarded-agent"}}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer api.Close()

	clientset := policyTestClientset(t, api.URL)
	for _, test := range []struct {
		name   string
		target opendepotv1alpha1.ScanPolicyTargetRef
		valid  bool
	}{
		{name: "onboarded module", target: opendepotv1alpha1.ScanPolicyTargetRef{Kind: "Module", Name: "onboarded-module"}, valid: true},
		{name: "onboarded provider", target: opendepotv1alpha1.ScanPolicyTargetRef{Kind: "Provider", Name: "onboarded-provider"}, valid: true},
		{name: "nonexistent module", target: opendepotv1alpha1.ScanPolicyTargetRef{Kind: "Module", Name: "missing-module"}},
		{name: "wildcard module", target: opendepotv1alpha1.ScanPolicyTargetRef{Kind: "Module", Name: "*"}},
		{name: "future provider", target: opendepotv1alpha1.ScanPolicyTargetRef{Kind: "Provider", Name: "future-provider"}},
		{name: "onboarded skill", target: opendepotv1alpha1.ScanPolicyTargetRef{Kind: "Skill", Name: "onboarded-skill"}, valid: true},
		{name: "onboarded agent", target: opendepotv1alpha1.ScanPolicyTargetRef{Kind: "Agent", Name: "onboarded-agent"}, valid: true},
		{name: "future skill", target: opendepotv1alpha1.ScanPolicyTargetRef{Kind: "Skill", Name: "future-skill"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			policy := &opendepotv1alpha1.ScanPolicy{Spec: opendepotv1alpha1.ScanPolicySpec{TargetRefs: []opendepotv1alpha1.ScanPolicyTargetRef{test.target}}}
			err := validateScanPolicyTargets(context.Background(), clientset.RESTClient(), "team", policy)
			if (err == nil) != test.valid {
				t.Fatalf("validateScanPolicyTargets() error = %v, want valid %t", err, test.valid)
			}
		})
	}
}

func TestScanPolicyHTTPRoutesAndAuthorization(t *testing.T) {
	originalAnonymous := opendepotAnonymousAuth
	originalEnabled := opendepotPolicyManagementEnabled
	originalVerifier := oidcVerifier
	originalAuth := policyRequestAuth
	defer func() {
		opendepotAnonymousAuth = originalAnonymous
		opendepotPolicyManagementEnabled = originalEnabled
		oidcVerifier = originalVerifier
		policyRequestAuth = originalAuth
	}()

	anonymous := false
	enabled := true
	opendepotAnonymousAuth = &anonymous
	opendepotPolicyManagementEnabled = &enabled
	oidcVerifier = nil

	var requests []string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")

		switch r.Method {
		case http.MethodGet:
			if strings.HasSuffix(r.URL.Path, "/scanpolicies") {
				_, _ = w.Write([]byte(`{"apiVersion":"opendepot.defdev.io/v1alpha1","kind":"ScanPolicyList","items":[{"apiVersion":"opendepot.defdev.io/v1alpha1","kind":"ScanPolicy","metadata":{"name":"allowed","resourceVersion":"1"},"spec":{"targetRefs":[{"kind":"Module","name":"allowed"}]}},{"apiVersion":"opendepot.defdev.io/v1alpha1","kind":"ScanPolicy","metadata":{"name":"wide","resourceVersion":"2"},"spec":{}}]}`))
			} else if strings.HasSuffix(r.URL.Path, "/wide") {
				_, _ = w.Write([]byte(`{"apiVersion":"opendepot.defdev.io/v1alpha1","kind":"ScanPolicy","metadata":{"name":"wide","resourceVersion":"2"},"spec":{}}`))
			} else {
				_, _ = w.Write([]byte(`{"apiVersion":"opendepot.defdev.io/v1alpha1","kind":"ScanPolicy","metadata":{"name":"allowed","resourceVersion":"1"},"spec":{"targetRefs":[{"kind":"Module","name":"allowed"}]}}`))
			}
		case http.MethodPost, http.MethodPut:
			_, _ = w.Write([]byte(`{"apiVersion":"opendepot.defdev.io/v1alpha1","kind":"ScanPolicy","metadata":{"name":"allowed","namespace":"team","resourceVersion":"3"},"spec":{}}`))
		case http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer api.Close()

	clientset := policyTestClientset(t, api.URL)
	policyRequestAuth = func(http.ResponseWriter, *http.Request) (*kubernetes.Clientset, *opendepotv1alpha1.GroupBinding, *opendepotv1alpha1.SecurityGroupBinding, string, error) {
		return clientset, nil, nil, "test-user", nil
	}

	router := chi.NewRouter()
	router.Get("/opendepot/ui/v1/scan-policies/{namespace}/capabilities", handleScanPolicyCapabilitiesRequest)
	router.Post("/opendepot/ui/v1/scan-policies/{namespace}/preview", handleScanPolicyPreviewRequest)
	router.HandleFunc("/opendepot/ui/v1/scan-policies/{namespace}", handleScanPolicies)
	router.HandleFunc("/opendepot/ui/v1/scan-policies/{namespace}/{name}", handleScanPolicies)

	t.Run("collection and item CRUD forward namespace and name", func(t *testing.T) {
		create := httptest.NewRecorder()
		router.ServeHTTP(create, httptest.NewRequest(http.MethodPost, "/opendepot/ui/v1/scan-policies/team", strings.NewReader(`{"metadata":{"name":"allowed"},"spec":{}}`)))
		if create.Code != http.StatusCreated {
			t.Fatalf("POST status = %d, want %d", create.Code, http.StatusCreated)
		}

		list := httptest.NewRecorder()
		router.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/opendepot/ui/v1/scan-policies/team", nil))
		if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), `"allowed"`) {
			t.Fatalf("GET collection response = %d %s", list.Code, list.Body.String())
		}

		update := httptest.NewRequest(http.MethodPut, "/opendepot/ui/v1/scan-policies/team/allowed", strings.NewReader(`{"metadata":{"name":"allowed","resourceVersion":"1"},"spec":{}}`))
		update.Header.Set("If-Match", `"1"`)
		updated := httptest.NewRecorder()
		router.ServeHTTP(updated, update)
		if updated.Code != http.StatusOK {
			t.Fatalf("PUT status = %d, want %d", updated.Code, http.StatusOK)
		}

		remove := httptest.NewRequest(http.MethodDelete, "/opendepot/ui/v1/scan-policies/team/allowed", nil)
		remove.Header.Set("If-Match", "1")
		deleted := httptest.NewRecorder()
		router.ServeHTTP(deleted, remove)
		if deleted.Code != http.StatusNoContent {
			t.Fatalf("DELETE status = %d, want %d", deleted.Code, http.StatusNoContent)
		}

		joined := strings.Join(requests, "\n")
		for _, want := range []string{
			"POST /apis/opendepot.defdev.io/v1alpha1/namespaces/team/scanpolicies",
			"GET /apis/opendepot.defdev.io/v1alpha1/namespaces/team/scanpolicies",
			"PUT /apis/opendepot.defdev.io/v1alpha1/namespaces/team/scanpolicies/allowed",
			"DELETE /apis/opendepot.defdev.io/v1alpha1/namespaces/team/scanpolicies/allowed",
		} {
			if !strings.Contains(joined, want) {
				t.Fatalf("Kubernetes request log %q does not contain %q", joined, want)
			}
		}
	})

	t.Run("capabilities and preview routes", func(t *testing.T) {
		capabilities := httptest.NewRecorder()
		router.ServeHTTP(capabilities, httptest.NewRequest(http.MethodGet, "/opendepot/ui/v1/scan-policies/team/capabilities", nil))
		if capabilities.Code != http.StatusOK || !strings.Contains(capabilities.Body.String(), `"supportsPreview":true`) {
			t.Fatalf("capabilities response = %d %s", capabilities.Code, capabilities.Body.String())
		}

		preview := httptest.NewRecorder()
		router.ServeHTTP(preview, httptest.NewRequest(http.MethodPost, "/opendepot/ui/v1/scan-policies/team/preview", strings.NewReader(`{"metadata":{"name":"preview"},"spec":{}}`)))
		if preview.Code != http.StatusOK || !strings.Contains(preview.Body.String(), `"valid":true`) {
			t.Fatalf("preview response = %d %s", preview.Code, preview.Body.String())
		}
	})

	t.Run("anonymous mutation is rejected before forwarding", func(t *testing.T) {
		*opendepotAnonymousAuth = true
		before := len(requests)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/opendepot/ui/v1/scan-policies/team", strings.NewReader(`{"metadata":{"name":"denied"},"spec":{}}`)))
		if response.Code != http.StatusForbidden {
			t.Fatalf("anonymous POST status = %d, want %d", response.Code, http.StatusForbidden)
		}
		if len(requests) != before {
			t.Fatal("anonymous mutation was forwarded to Kubernetes")
		}
		*opendepotAnonymousAuth = false
	})

	t.Run("OIDC management permission separates reads from writes", func(t *testing.T) {
		oidcVerifier = &gooidc.IDTokenVerifier{}
		binding := &opendepotv1alpha1.SecurityGroupBinding{Spec: opendepotv1alpha1.SecurityGroupBindingSpec{Namespaces: []string{"team"}, ModuleResources: []string{"allowed"}}}
		policyRequestAuth = func(http.ResponseWriter, *http.Request) (*kubernetes.Clientset, *opendepotv1alpha1.GroupBinding, *opendepotv1alpha1.SecurityGroupBinding, string, error) {
			return clientset, nil, binding, "oidc-user", nil
		}

		list := httptest.NewRecorder()
		router.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/opendepot/ui/v1/scan-policies/team", nil))
		if list.Code != http.StatusOK || strings.Contains(list.Body.String(), `"wide"`) || !strings.Contains(list.Body.String(), `"allowed"`) {
			t.Fatalf("filtered OIDC list = %d %s", list.Code, list.Body.String())
		}

		hidden := httptest.NewRecorder()
		router.ServeHTTP(hidden, httptest.NewRequest(http.MethodGet, "/opendepot/ui/v1/scan-policies/team/wide", nil))
		if hidden.Code != http.StatusNotFound {
			t.Fatalf("out-of-scope OIDC item status = %d, want %d", hidden.Code, http.StatusNotFound)
		}

		write := httptest.NewRecorder()
		router.ServeHTTP(write, httptest.NewRequest(http.MethodPost, "/opendepot/ui/v1/scan-policies/team", strings.NewReader(`{"metadata":{"name":"denied"},"spec":{}}`)))
		if write.Code != http.StatusForbidden {
			t.Fatalf("OIDC write without permission status = %d, want %d", write.Code, http.StatusForbidden)
		}

		binding.Spec.NamespaceWidePolicyManagement = true
		write = httptest.NewRecorder()
		router.ServeHTTP(write, httptest.NewRequest(http.MethodPost, "/opendepot/ui/v1/scan-policies/team", strings.NewReader(`{"metadata":{"name":"allowed"},"spec":{}}`)))
		if write.Code != http.StatusCreated {
			t.Fatalf("OIDC write with permission status = %d, want %d", write.Code, http.StatusCreated)
		}

		allowedCapabilities := httptest.NewRecorder()
		router.ServeHTTP(allowedCapabilities, httptest.NewRequest(http.MethodGet, "/opendepot/ui/v1/scan-policies/team/capabilities", nil))
		if allowedCapabilities.Code != http.StatusOK {
			t.Fatalf("allowed namespace capabilities status = %d, want %d", allowedCapabilities.Code, http.StatusOK)
		}

		allowedPreview := httptest.NewRecorder()
		router.ServeHTTP(allowedPreview, httptest.NewRequest(http.MethodPost, "/opendepot/ui/v1/scan-policies/team/preview", strings.NewReader(`{"metadata":{"name":"preview"},"spec":{}}`)))
		if allowedPreview.Code != http.StatusOK {
			t.Fatalf("allowed namespace preview status = %d, want %d", allowedPreview.Code, http.StatusOK)
		}

		for _, test := range []struct {
			name   string
			method string
			path   string
			body   string
		}{
			{name: "read", method: http.MethodGet, path: "/opendepot/ui/v1/scan-policies/other"},
			{name: "write", method: http.MethodPost, path: "/opendepot/ui/v1/scan-policies/other", body: `{"metadata":{"name":"denied"},"spec":{}}`},
			{name: "capabilities", method: http.MethodGet, path: "/opendepot/ui/v1/scan-policies/other/capabilities"},
			{name: "preview", method: http.MethodPost, path: "/opendepot/ui/v1/scan-policies/other/preview", body: `{"metadata":{"name":"preview"},"spec":{}}`},
		} {
			t.Run("cross-namespace "+test.name, func(t *testing.T) {
				response := httptest.NewRecorder()
				router.ServeHTTP(response, httptest.NewRequest(test.method, test.path, strings.NewReader(test.body)))
				if response.Code != http.StatusNotFound {
					t.Fatalf("%s %s status = %d, want %d", test.method, test.path, response.Code, http.StatusNotFound)
				}
			})
		}
	})

	t.Run("If-Match preconditions return 428 and 409", func(t *testing.T) {
		missing := httptest.NewRecorder()
		router.ServeHTTP(missing, httptest.NewRequest(http.MethodPut, "/opendepot/ui/v1/scan-policies/team/allowed", strings.NewReader(`{"metadata":{"name":"allowed","resourceVersion":"1"},"spec":{}}`)))
		if missing.Code != http.StatusPreconditionRequired {
			t.Fatalf("missing If-Match status = %d, want %d", missing.Code, http.StatusPreconditionRequired)
		}

		stale := httptest.NewRequest(http.MethodPut, "/opendepot/ui/v1/scan-policies/team/allowed", strings.NewReader(`{"metadata":{"name":"allowed","resourceVersion":"1"},"spec":{}}`))
		stale.Header.Set("If-Match", "2")
		conflict := httptest.NewRecorder()
		router.ServeHTTP(conflict, stale)
		if conflict.Code != http.StatusConflict {
			t.Fatalf("stale If-Match status = %d, want %d", conflict.Code, http.StatusConflict)
		}
	})
}

func policyTestClientset(t *testing.T, apiURL string) *kubernetes.Clientset {
	t.Helper()

	clientset, err := kubernetes.NewForConfig(&rest.Config{Host: apiURL})
	if err != nil {
		t.Fatalf("create test Kubernetes client: %v", err)
	}

	return clientset
}
