package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"github.com/go-chi/chi/v5"
	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	"k8s.io/client-go/kubernetes"
)

const wave7nPolicyBase = "/apis/opendepot.defdev.io/v1alpha1/namespaces/team/scanpolicies"

type wave7nPolicyResp struct {
	status int
	body   string
}

func wave7nPolicyNotFound() wave7nPolicyResp {
	return wave7nPolicyResp{status: http.StatusNotFound, body: `{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"NotFound","code":404}`}
}

func wave7nPolicyAPI(t *testing.T, routes map[string]wave7nPolicyResp) *kubernetes.Clientset {
	t.Helper()

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp, ok := routes[r.Method+" "+r.URL.Path]
		if !ok {
			resp = wave7nPolicyNotFound()
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.status)
		_, _ = w.Write([]byte(resp.body))
	}))
	t.Cleanup(api.Close)

	return policyTestClientset(t, api.URL)
}

func wave7nPolicyJSON(name, resourceVersion, targets, extraSpec string) string {
	return fmt.Sprintf(`{"apiVersion":"opendepot.defdev.io/v1alpha1","kind":"ScanPolicy","metadata":{"name":%q,"namespace":"team","resourceVersion":%q},"spec":{"targetRefs":[%s]%s}}`, name, resourceVersion, targets, extraSpec)
}

func wave7nModuleTarget(name string) string {
	return fmt.Sprintf(`{"kind":"Module","name":%q}`, name)
}

func wave7nModuleList(names ...string) string {
	items := make([]string, 0, len(names))
	for _, name := range names {
		items = append(items, fmt.Sprintf(`{"metadata":{"name":%q}}`, name))
	}

	return `{"apiVersion":"opendepot.defdev.io/v1alpha1","kind":"ModuleList","items":[` + strings.Join(items, ",") + `]}`
}

func wave7nPolicyRouter(t *testing.T, anonymous, enabled, oidc bool, clientset *kubernetes.Clientset, binding *opendepotv1alpha1.SecurityGroupBinding) *chi.Mux {
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
	if oidc {
		oidcVerifier = &gooidc.IDTokenVerifier{}
	}

	policyRequestAuth = func(http.ResponseWriter, *http.Request) (*kubernetes.Clientset, *opendepotv1alpha1.GroupBinding, *opendepotv1alpha1.SecurityGroupBinding, string, error) {
		return clientset, nil, binding, "test-user", nil
	}

	router := chi.NewRouter()
	router.Get("/opendepot/ui/v1/scan-policies/{namespace}/capabilities", handleScanPolicyCapabilitiesRequest)
	router.Post("/opendepot/ui/v1/scan-policies/{namespace}/preview", handleScanPolicyPreviewRequest)
	router.HandleFunc("/opendepot/ui/v1/scan-policies/{namespace}", handleScanPolicies)
	router.HandleFunc("/opendepot/ui/v1/scan-policies/{namespace}/{name}", handleScanPolicies)

	return router
}

func wave7nScopedBinding() *opendepotv1alpha1.SecurityGroupBinding {
	return &opendepotv1alpha1.SecurityGroupBinding{Spec: opendepotv1alpha1.SecurityGroupBindingSpec{
		Namespaces:      []string{"team"},
		ModuleResources: []string{"allowed"},
	}}
}

func wave7nWideBinding() *opendepotv1alpha1.SecurityGroupBinding {
	return &opendepotv1alpha1.SecurityGroupBinding{Spec: opendepotv1alpha1.SecurityGroupBindingSpec{
		Namespaces:                    []string{"team"},
		ModuleResources:               []string{"*"},
		NamespaceWidePolicyManagement: true,
	}}
}

func wave7nServe(router http.Handler, method, target, body, ifMatch string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if ifMatch != "" {
		req.Header.Set("If-Match", ifMatch)
	}

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	return recorder
}

func TestWave7NScanPolicyAccessGates(t *testing.T) {
	clientset := wave7nPolicyAPI(t, map[string]wave7nPolicyResp{
		"GET " + wave7nPolicyBase: {status: http.StatusOK, body: `{"apiVersion":"opendepot.defdev.io/v1alpha1","kind":"ScanPolicyList","items":[]}`},
	})

	t.Run("anonymous mutations are forbidden", func(t *testing.T) {
		router := wave7nPolicyRouter(t, true, true, false, clientset, nil)
		recorder := wave7nServe(router, http.MethodPost, "/opendepot/ui/v1/scan-policies/team", wave7nPolicyJSON("a", "", "", ""), "")
		if recorder.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusForbidden)
		}
	})

	t.Run("writes disabled return not found for mutations but reads still work", func(t *testing.T) {
		router := wave7nPolicyRouter(t, false, false, false, clientset, nil)
		write := wave7nServe(router, http.MethodPost, "/opendepot/ui/v1/scan-policies/team", wave7nPolicyJSON("a", "", "", ""), "")
		if write.Code != http.StatusNotFound {
			t.Fatalf("POST status = %d, want %d", write.Code, http.StatusNotFound)
		}

		read := wave7nServe(router, http.MethodGet, "/opendepot/ui/v1/scan-policies/team", "", "")
		if read.Code != http.StatusOK {
			t.Fatalf("GET status = %d, want %d", read.Code, http.StatusOK)
		}
	})

	t.Run("namespace outside the binding is not found", func(t *testing.T) {
		binding := &opendepotv1alpha1.SecurityGroupBinding{Spec: opendepotv1alpha1.SecurityGroupBindingSpec{Namespaces: []string{"other"}}}
		router := wave7nPolicyRouter(t, false, true, true, clientset, binding)
		recorder := wave7nServe(router, http.MethodGet, "/opendepot/ui/v1/scan-policies/team", "", "")
		if recorder.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
		}
	})

	t.Run("unsupported method is rejected", func(t *testing.T) {
		router := wave7nPolicyRouter(t, false, true, false, clientset, nil)
		recorder := wave7nServe(router, http.MethodPatch, "/opendepot/ui/v1/scan-policies/team/allowed", "", "")
		if recorder.Code != http.StatusMethodNotAllowed || recorder.Header().Get("Allow") == "" {
			t.Fatalf("status = %d allow = %q, want 405 with Allow header", recorder.Code, recorder.Header().Get("Allow"))
		}
	})
}

func TestWave7NScanPolicyCreateBranches(t *testing.T) {
	modules := wave7nModuleList("allowed", "other")
	clientset := wave7nPolicyAPI(t, map[string]wave7nPolicyResp{
		"GET " + "/apis/opendepot.defdev.io/v1alpha1/namespaces/team/modules": {status: http.StatusOK, body: modules},
		"POST " + wave7nPolicyBase: {status: http.StatusCreated, body: wave7nPolicyJSON("allowed", "3", wave7nModuleTarget("allowed"), "")},
	})

	cases := []struct {
		name    string
		binding *opendepotv1alpha1.SecurityGroupBinding
		oidc    bool
		body    string
		want    int
	}{
		{
			name: "namespace mismatch is rejected",
			body: `{"metadata":{"name":"allowed","namespace":"other"},"spec":{}}`,
			want: http.StatusBadRequest,
		},
		{
			name: "invalid JSON is rejected",
			body: `{"metadata":`,
			want: http.StatusBadRequest,
		},
		{
			name: "unknown target is rejected",
			body: wave7nPolicyJSON("allowed", "", wave7nModuleTarget("missing"), ""),
			want: http.StatusBadRequest,
		},
		{
			name:    "resource scoped binding cannot create namespace-wide policy",
			binding: wave7nScopedBinding(),
			oidc:    true,
			body:    `{"metadata":{"name":"allowed"},"spec":{"selector":{"matchLabels":{"a":"b"}}}}`,
			want:    http.StatusForbidden,
		},
		{
			name:    "resource scoped binding cannot target a module outside its grant",
			binding: wave7nScopedBinding(),
			oidc:    true,
			body:    wave7nPolicyJSON("allowed", "", wave7nModuleTarget("other"), ""),
			want:    http.StatusForbidden,
		},
		{
			name:    "resource scoped binding cannot add jev exemptions",
			binding: wave7nScopedBinding(),
			oidc:    true,
			body:    wave7nPolicyJSON("allowed", "", wave7nModuleTarget("allowed"), `,"jevExemptions":[{"contentDigest":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","reason":"reviewed"}]`),
			want:    http.StatusForbidden,
		},
		{
			name:    "namespace wide binding creates policy",
			binding: wave7nWideBinding(),
			oidc:    true,
			body:    wave7nPolicyJSON("allowed", "", wave7nModuleTarget("allowed"), ""),
			want:    http.StatusCreated,
		},
		{
			name: "anonymous disabled oidc creates policy",
			body: wave7nPolicyJSON("allowed", "", wave7nModuleTarget("allowed"), ""),
			want: http.StatusCreated,
		},
		{
			name: "oversized body is rejected",
			body: strings.Repeat("a", maxScanPolicyBody+1),
			want: http.StatusBadRequest,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router := wave7nPolicyRouter(t, false, true, tc.oidc, clientset, tc.binding)
			recorder := wave7nServe(router, http.MethodPost, "/opendepot/ui/v1/scan-policies/team", tc.body, "")
			if recorder.Code != tc.want {
				t.Fatalf("status = %d, want %d; body %s", recorder.Code, tc.want, recorder.Body.String())
			}
		})
	}

	t.Run("kubernetes conflict is surfaced", func(t *testing.T) {
		conflict := wave7nPolicyAPI(t, map[string]wave7nPolicyResp{
			"GET " + "/apis/opendepot.defdev.io/v1alpha1/namespaces/team/modules": {status: http.StatusOK, body: modules},
			"POST " + wave7nPolicyBase: {status: http.StatusConflict, body: `{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"AlreadyExists","code":409}`},
		})
		router := wave7nPolicyRouter(t, false, true, false, conflict, nil)
		recorder := wave7nServe(router, http.MethodPost, "/opendepot/ui/v1/scan-policies/team", wave7nPolicyJSON("allowed", "", wave7nModuleTarget("allowed"), ""), "")
		if recorder.Code != http.StatusConflict {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusConflict)
		}
	})
}

func TestWave7NScanPolicyUpdateBranches(t *testing.T) {
	persisted := wave7nPolicyJSON("allowed", "1", wave7nModuleTarget("allowed"), "")
	foreign := wave7nPolicyJSON("other", "1", wave7nModuleTarget("other"), "")
	routes := map[string]wave7nPolicyResp{
		"GET " + "/apis/opendepot.defdev.io/v1alpha1/namespaces/team/modules": {status: http.StatusOK, body: wave7nModuleList("allowed", "other")},
		"GET " + wave7nPolicyBase + "/allowed":                                {status: http.StatusOK, body: persisted},
		"GET " + wave7nPolicyBase + "/other":                                  {status: http.StatusOK, body: foreign},
		"PUT " + wave7nPolicyBase + "/allowed":                                {status: http.StatusOK, body: persisted},
	}
	clientset := wave7nPolicyAPI(t, routes)

	cases := []struct {
		name    string
		target  string
		binding *opendepotv1alpha1.SecurityGroupBinding
		oidc    bool
		ifMatch string
		body    string
		want    int
	}{
		{
			name:    "missing If-Match is a precondition failure",
			target:  "/opendepot/ui/v1/scan-policies/team/allowed",
			body:    persisted,
			ifMatch: "",
			want:    http.StatusPreconditionRequired,
		},
		{
			name:    "invalid JSON is rejected",
			ifMatch: `"1"`,
			body:    `{"metadata":`,
			want:    http.StatusBadRequest,
		},
		{
			name:    "body name must match URL name",
			ifMatch: `"1"`,
			body:    wave7nPolicyJSON("other", "1", wave7nModuleTarget("allowed"), ""),
			want:    http.StatusBadRequest,
		},
		{
			name:    "stale resource version conflicts",
			ifMatch: `"1"`,
			body:    wave7nPolicyJSON("allowed", "0", wave7nModuleTarget("allowed"), ""),
			want:    http.StatusConflict,
		},
		{
			name:    "missing resource version conflicts",
			ifMatch: `"1"`,
			body:    `{"metadata":{"name":"allowed"},"spec":{}}`,
			want:    http.StatusConflict,
		},
		{
			name:    "unknown target is rejected",
			ifMatch: `"1"`,
			body:    wave7nPolicyJSON("allowed", "1", wave7nModuleTarget("missing"), ""),
			want:    http.StatusBadRequest,
		},
		{
			name:    "resource scoped binding cannot edit a persisted policy that targets outside its grant",
			target:  "/opendepot/ui/v1/scan-policies/team/other",
			binding: wave7nScopedBinding(),
			oidc:    true,
			ifMatch: `"1"`,
			body:    wave7nPolicyJSON("other", "1", wave7nModuleTarget("other"), ""),
			want:    http.StatusForbidden,
		},
		{
			name:    "resource scoped binding cannot move a policy outside its grant",
			binding: wave7nScopedBinding(),
			oidc:    true,
			ifMatch: `"1"`,
			body:    wave7nPolicyJSON("allowed", "1", wave7nModuleTarget("other"), ""),
			want:    http.StatusForbidden,
		},
		{
			name:    "namespace wide binding updates policy",
			binding: wave7nWideBinding(),
			oidc:    true,
			ifMatch: `"1"`,
			body:    persisted,
			want:    http.StatusOK,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router := wave7nPolicyRouter(t, false, true, tc.oidc, clientset, tc.binding)
			target := tc.target
			if target == "" {
				target = "/opendepot/ui/v1/scan-policies/team/allowed"
			}

			recorder := wave7nServe(router, http.MethodPut, target, tc.body, tc.ifMatch)
			if recorder.Code != tc.want {
				t.Fatalf("status = %d, want %d; body %s", recorder.Code, tc.want, recorder.Body.String())
			}
		})
	}

	t.Run("persisted policy that cannot be decoded is an internal error", func(t *testing.T) {
		broken := wave7nPolicyAPI(t, map[string]wave7nPolicyResp{
			"GET " + "/apis/opendepot.defdev.io/v1alpha1/namespaces/team/modules": {status: http.StatusOK, body: wave7nModuleList("allowed")},
			"GET " + wave7nPolicyBase + "/allowed":                                {status: http.StatusOK, body: `not-json`},
		})
		router := wave7nPolicyRouter(t, false, true, false, broken, nil)
		recorder := wave7nServe(router, http.MethodPut, "/opendepot/ui/v1/scan-policies/team/allowed", persisted, `"1"`)
		if recorder.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
		}
	})

	t.Run("missing persisted policy is not found", func(t *testing.T) {
		missing := wave7nPolicyAPI(t, map[string]wave7nPolicyResp{
			"GET " + "/apis/opendepot.defdev.io/v1alpha1/namespaces/team/modules": {status: http.StatusOK, body: wave7nModuleList("allowed")},
		})
		router := wave7nPolicyRouter(t, false, true, false, missing, nil)
		recorder := wave7nServe(router, http.MethodPut, "/opendepot/ui/v1/scan-policies/team/allowed", persisted, `"1"`)
		if recorder.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
		}
	})

	t.Run("resource scoped binding cannot change jev exemptions", func(t *testing.T) {
		withExemption := wave7nPolicyJSON("allowed", "1", wave7nModuleTarget("allowed"), `,"jevExemptions":[{"contentDigest":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","reason":"reviewed"}]`)
		router := wave7nPolicyRouter(t, false, true, true, clientset, wave7nScopedBinding())
		recorder := wave7nServe(router, http.MethodPut, "/opendepot/ui/v1/scan-policies/team/allowed", withExemption, `"1"`)
		if recorder.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusForbidden)
		}
	})
}

func TestWave7NScanPolicyDeleteBranches(t *testing.T) {
	persisted := wave7nPolicyJSON("allowed", "1", wave7nModuleTarget("allowed"), "")
	foreign := wave7nPolicyJSON("other", "1", wave7nModuleTarget("other"), "")
	exempt := wave7nPolicyJSON("allowed", "1", wave7nModuleTarget("allowed"), `,"jevExemptions":[{"contentDigest":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","reason":"reviewed"}]`)
	clientset := wave7nPolicyAPI(t, map[string]wave7nPolicyResp{
		"GET " + wave7nPolicyBase + "/allowed":     {status: http.StatusOK, body: persisted},
		"GET " + wave7nPolicyBase + "/other":       {status: http.StatusOK, body: foreign},
		"GET " + wave7nPolicyBase + "/exempt":      {status: http.StatusOK, body: exempt},
		"DELETE " + wave7nPolicyBase + "/allowed":  {status: http.StatusOK, body: `{"kind":"Status","apiVersion":"v1","status":"Success","code":200}`},
		"GET " + wave7nPolicyBase + "/conflict":    {status: http.StatusOK, body: persisted},
		"DELETE " + wave7nPolicyBase + "/conflict": {status: http.StatusConflict, body: `{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"Conflict","code":409}`},
	})

	cases := []struct {
		name    string
		target  string
		binding *opendepotv1alpha1.SecurityGroupBinding
		oidc    bool
		ifMatch string
		want    int
	}{
		{
			name:    "collection delete without a name is rejected",
			target:  "/opendepot/ui/v1/scan-policies/team",
			ifMatch: `"1"`,
			want:    http.StatusBadRequest,
		},
		{
			name:    "missing If-Match is a precondition failure",
			target:  "/opendepot/ui/v1/scan-policies/team/allowed",
			ifMatch: "",
			want:    http.StatusPreconditionRequired,
		},
		{
			name:    "missing persisted policy is not found",
			target:  "/opendepot/ui/v1/scan-policies/team/absent",
			ifMatch: `"1"`,
			want:    http.StatusNotFound,
		},
		{
			name:    "resource scoped binding cannot delete a policy outside its grant",
			target:  "/opendepot/ui/v1/scan-policies/team/other",
			binding: wave7nScopedBinding(),
			oidc:    true,
			ifMatch: `"1"`,
			want:    http.StatusForbidden,
		},
		{
			name:    "resource scoped binding cannot delete jev exemptions",
			target:  "/opendepot/ui/v1/scan-policies/team/exempt",
			binding: wave7nScopedBinding(),
			oidc:    true,
			ifMatch: `"1"`,
			want:    http.StatusForbidden,
		},
		{
			name:    "kubernetes conflict on delete is surfaced",
			target:  "/opendepot/ui/v1/scan-policies/team/conflict",
			ifMatch: `"1"`,
			want:    http.StatusConflict,
		},
		{
			name:    "delete succeeds",
			target:  "/opendepot/ui/v1/scan-policies/team/allowed",
			ifMatch: `"1"`,
			want:    http.StatusNoContent,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router := wave7nPolicyRouter(t, false, true, tc.oidc, clientset, tc.binding)
			recorder := wave7nServe(router, http.MethodDelete, tc.target, "", tc.ifMatch)
			if recorder.Code != tc.want {
				t.Fatalf("status = %d, want %d; body %s", recorder.Code, tc.want, recorder.Body.String())
			}
		})
	}

	t.Run("persisted policy that cannot be decoded is an internal error", func(t *testing.T) {
		broken := wave7nPolicyAPI(t, map[string]wave7nPolicyResp{
			"GET " + wave7nPolicyBase + "/allowed": {status: http.StatusOK, body: `not-json`},
		})
		router := wave7nPolicyRouter(t, false, true, false, broken, nil)
		recorder := wave7nServe(router, http.MethodDelete, "/opendepot/ui/v1/scan-policies/team/allowed", "", `"1"`)
		if recorder.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
		}
	})
}

func TestWave7NScanPolicyReadFilteringAndErrors(t *testing.T) {
	list := `{"apiVersion":"opendepot.defdev.io/v1alpha1","kind":"ScanPolicyList","items":[` +
		wave7nPolicyJSON("allowed", "1", wave7nModuleTarget("allowed"), "") + "," +
		wave7nPolicyJSON("other", "2", wave7nModuleTarget("other"), "") + `]}`
	clientset := wave7nPolicyAPI(t, map[string]wave7nPolicyResp{
		"GET " + wave7nPolicyBase:                {status: http.StatusOK, body: list},
		"GET " + wave7nPolicyBase + "/other":     {status: http.StatusOK, body: wave7nPolicyJSON("other", "2", wave7nModuleTarget("other"), "")},
		"GET " + wave7nPolicyBase + "/forbidden": {status: http.StatusForbidden, body: `{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"Forbidden","code":403}`},
	})

	t.Run("list omits policies outside the binding", func(t *testing.T) {
		router := wave7nPolicyRouter(t, false, true, true, clientset, wave7nScopedBinding())
		recorder := wave7nServe(router, http.MethodGet, "/opendepot/ui/v1/scan-policies/team", "", "")
		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
		}

		body := recorder.Body.String()
		if !strings.Contains(body, `"allowed"`) || strings.Contains(body, `"other"`) {
			t.Fatalf("filtered list = %s, want only allowed", body)
		}
	})

	t.Run("single policy outside the binding is not found", func(t *testing.T) {
		router := wave7nPolicyRouter(t, false, true, true, clientset, wave7nScopedBinding())
		recorder := wave7nServe(router, http.MethodGet, "/opendepot/ui/v1/scan-policies/team/other", "", "")
		if recorder.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
		}
	})

	t.Run("kubernetes error on read is mapped to its status", func(t *testing.T) {
		router := wave7nPolicyRouter(t, false, true, false, clientset, nil)
		recorder := wave7nServe(router, http.MethodGet, "/opendepot/ui/v1/scan-policies/team/forbidden", "", "")
		if recorder.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusForbidden)
		}
	})

	t.Run("undecodable list is an internal error", func(t *testing.T) {
		broken := wave7nPolicyAPI(t, map[string]wave7nPolicyResp{
			"GET " + wave7nPolicyBase: {status: http.StatusOK, body: `not-json`},
		})
		router := wave7nPolicyRouter(t, false, true, false, broken, nil)
		recorder := wave7nServe(router, http.MethodGet, "/opendepot/ui/v1/scan-policies/team", "", "")
		if recorder.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
		}
	})

	t.Run("undecodable single policy is an internal error", func(t *testing.T) {
		broken := wave7nPolicyAPI(t, map[string]wave7nPolicyResp{
			"GET " + wave7nPolicyBase + "/allowed": {status: http.StatusOK, body: `not-json`},
		})
		router := wave7nPolicyRouter(t, false, true, false, broken, nil)
		recorder := wave7nServe(router, http.MethodGet, "/opendepot/ui/v1/scan-policies/team/allowed", "", "")
		if recorder.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
		}
	})
}

func TestWave7NScanPolicyCapabilitiesAndPreviewBranches(t *testing.T) {
	clientset := wave7nPolicyAPI(t, map[string]wave7nPolicyResp{
		"GET " + "/apis/opendepot.defdev.io/v1alpha1/namespaces/team/modules": {status: http.StatusOK, body: wave7nModuleList("allowed")},
	})

	t.Run("capabilities without oidc report writable", func(t *testing.T) {
		router := wave7nPolicyRouter(t, false, true, false, clientset, nil)
		recorder := wave7nServe(router, http.MethodGet, "/opendepot/ui/v1/scan-policies/team/capabilities", "", "")
		if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"canWrite":true`) {
			t.Fatalf("capabilities = %d %s", recorder.Code, recorder.Body.String())
		}
	})

	t.Run("capabilities for an unbound namespace are not found", func(t *testing.T) {
		binding := &opendepotv1alpha1.SecurityGroupBinding{Spec: opendepotv1alpha1.SecurityGroupBindingSpec{Namespaces: []string{"other"}}}
		router := wave7nPolicyRouter(t, false, true, true, clientset, binding)
		recorder := wave7nServe(router, http.MethodGet, "/opendepot/ui/v1/scan-policies/team/capabilities", "", "")
		if recorder.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
		}
	})

	t.Run("preview without a binding is not found", func(t *testing.T) {
		router := wave7nPolicyRouter(t, false, true, true, clientset, nil)
		recorder := wave7nServe(router, http.MethodPost, "/opendepot/ui/v1/scan-policies/team/preview", wave7nPolicyJSON("allowed", "", wave7nModuleTarget("allowed"), ""), "")
		if recorder.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
		}
	})

	t.Run("preview for an unbound namespace is not found", func(t *testing.T) {
		binding := &opendepotv1alpha1.SecurityGroupBinding{Spec: opendepotv1alpha1.SecurityGroupBindingSpec{Namespaces: []string{"other"}}}
		router := wave7nPolicyRouter(t, false, true, true, clientset, binding)
		recorder := wave7nServe(router, http.MethodPost, "/opendepot/ui/v1/scan-policies/team/preview", wave7nPolicyJSON("allowed", "", wave7nModuleTarget("allowed"), ""), "")
		if recorder.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
		}
	})

	t.Run("preview rejects invalid JSON", func(t *testing.T) {
		router := wave7nPolicyRouter(t, false, true, false, clientset, nil)
		recorder := wave7nServe(router, http.MethodPost, "/opendepot/ui/v1/scan-policies/team/preview", `{"spec":`, "")
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
		}
	})

	t.Run("preview rejects an unknown target", func(t *testing.T) {
		router := wave7nPolicyRouter(t, false, true, false, clientset, nil)
		recorder := wave7nServe(router, http.MethodPost, "/opendepot/ui/v1/scan-policies/team/preview", wave7nPolicyJSON("allowed", "", wave7nModuleTarget("missing"), ""), "")
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
		}
	})

	t.Run("preview outside the binding scope is forbidden", func(t *testing.T) {
		router := wave7nPolicyRouter(t, false, true, true, clientset, wave7nScopedBinding())
		recorder := wave7nServe(router, http.MethodPost, "/opendepot/ui/v1/scan-policies/team/preview", wave7nPolicyJSON("allowed", "", wave7nModuleTarget("allowed"), `,"selector":{"matchLabels":{"a":"b"}}`), "")
		if recorder.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusForbidden)
		}
	})

	t.Run("preview of a valid policy returns it", func(t *testing.T) {
		router := wave7nPolicyRouter(t, false, true, false, clientset, nil)
		recorder := wave7nServe(router, http.MethodPost, "/opendepot/ui/v1/scan-policies/team/preview", wave7nPolicyJSON("allowed", "", wave7nModuleTarget("allowed"), ""), "")
		if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"valid":true`) {
			t.Fatalf("preview = %d %s", recorder.Code, recorder.Body.String())
		}
	})
}

func TestWave7NDecodeAndValidateScanPolicyRejections(t *testing.T) {
	const digest = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	cases := map[string]string{
		"invalid name":         `{"metadata":{"name":"Bad_Name"},"spec":{}}`,
		"severity":             `{"metadata":{"name":"ok"},"spec":{"severityThreshold":"BOGUS"}}`,
		"target kind":          `{"metadata":{"name":"ok"},"spec":{"targetRefs":[{"kind":"Widget","name":"x"}]}}`,
		"target name":          `{"metadata":{"name":"ok"},"spec":{"targetRefs":[{"kind":"Module"}]}}`,
		"exemption reason":     `{"metadata":{"name":"ok"},"spec":{"exemptions":[{"reason":"  "}]}}`,
		"jev digest":           `{"metadata":{"name":"ok"},"spec":{"jevExemptions":[{"contentDigest":"XYZ","reason":"r"}]}}`,
		"jev duplicate digest": `{"metadata":{"name":"ok"},"spec":{"jevExemptions":[{"contentDigest":"` + digest + `","reason":"r"},{"contentDigest":"` + digest + `","reason":"r"}]}}`,
		"jev reason":           `{"metadata":{"name":"ok"},"spec":{"jevExemptions":[{"contentDigest":"` + digest + `","reason":""}]}}`,
		"unknown field":        `{"metadata":{"name":"ok"},"spec":{},"bogus":true}`,
	}

	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			var policy opendepotv1alpha1.ScanPolicy
			if err := decodeAndValidateScanPolicy([]byte(body), &policy); err == nil {
				t.Fatalf("decodeAndValidateScanPolicy(%s) returned nil error", name)
			}
		})
	}

	t.Run("valid severity is case insensitive", func(t *testing.T) {
		var policy opendepotv1alpha1.ScanPolicy
		if err := decodeAndValidateScanPolicy([]byte(`{"metadata":{"name":"ok"},"spec":{"severityThreshold":"high"}}`), &policy); err != nil {
			t.Fatalf("decodeAndValidateScanPolicy() error = %v", err)
		}
	})
}

func TestWave7NScanPolicyTargetValidation(t *testing.T) {
	clientset := wave7nPolicyAPI(t, map[string]wave7nPolicyResp{
		"GET " + "/apis/opendepot.defdev.io/v1alpha1/namespaces/team/providers": {status: http.StatusOK, body: `{"apiVersion":"opendepot.defdev.io/v1alpha1","kind":"ProviderList","items":[{"metadata":{"name":"aws"}}]}`},
		"GET " + "/apis/opendepot.defdev.io/v1alpha1/namespaces/team/skills":    {status: http.StatusOK, body: `{"apiVersion":"opendepot.defdev.io/v1alpha1","kind":"SkillList","items":[{"metadata":{"name":"lint"}}]}`},
		"GET " + "/apis/opendepot.defdev.io/v1alpha1/namespaces/team/agents":    {status: http.StatusOK, body: `{"apiVersion":"opendepot.defdev.io/v1alpha1","kind":"AgentList","items":[{"metadata":{"name":"bot"}}]}`},
		"GET " + "/apis/opendepot.defdev.io/v1alpha1/namespaces/team/modules":   {status: http.StatusOK, body: `not-json`},
	})

	client := clientset.RESTClient()
	ok := func(kind, name string) *opendepotv1alpha1.ScanPolicy {
		return &opendepotv1alpha1.ScanPolicy{Spec: opendepotv1alpha1.ScanPolicySpec{TargetRefs: []opendepotv1alpha1.ScanPolicyTargetRef{{Kind: kind, Name: name}}}}
	}

	cases := []struct {
		name    string
		policy  *opendepotv1alpha1.ScanPolicy
		wantErr bool
	}{
		{name: "provider target resolves", policy: ok("Provider", "aws")},
		{name: "skill target resolves", policy: ok("Skill", "lint")},
		{name: "agent target resolves", policy: ok("Agent", "bot")},
		{name: "unknown provider rejected", policy: ok("Provider", "gcp"), wantErr: true},
		{name: "wildcard rejected", policy: ok("Module", "*"), wantErr: true},
		{name: "undecodable module list rejected", policy: ok("Module", "allowed"), wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateScanPolicyTargets(t.Context(), client, "team", tc.policy)
			if (err != nil) != tc.wantErr {
				t.Fatalf("validateScanPolicyTargets() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestWave7NWriteKubernetesErrorMapping(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{name: "plain error is internal", err: fmt.Errorf("boom"), want: http.StatusInternalServerError},
		{name: "rejected request is conflict", err: fmt.Errorf("the server rejected our request due to an error"), want: http.StatusConflict},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			writeKubernetesError(recorder, tc.err)
			if recorder.Code != tc.want {
				t.Fatalf("status = %d, want %d", recorder.Code, tc.want)
			}
		})
	}
}
