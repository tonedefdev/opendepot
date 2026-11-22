package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
)

const wave6PolicyBody = `{"apiVersion":"opendepot.defdev.io/v1alpha1","kind":"ScanPolicy","metadata":{"name":"gate"},"spec":{"severityThreshold":"HIGH","targetRefs":[{"kind":"Module","name":"allowed"}]}}`

// wave6PolicyAPI serves a ScanPolicy "existing" at resourceVersion 7, a Module named "allowed", and accepts writes.
func wave6PolicyAPI(t *testing.T) *httptest.Server {
	t.Helper()

	existing := `{"apiVersion":"opendepot.defdev.io/v1alpha1","kind":"ScanPolicy","metadata":{"name":"existing","namespace":"team","resourceVersion":"7"},"spec":{"severityThreshold":"HIGH","targetRefs":[{"kind":"Module","name":"allowed"}]}}`
	modules := opendepotv1alpha1.ModuleList{Items: []opendepotv1alpha1.Module{{}}}
	modules.Items[0].Name = "allowed"
	modulePayload, err := json.Marshal(modules)
	if err != nil {
		t.Fatalf("marshal modules: %v", err)
	}

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case strings.HasSuffix(r.URL.Path, "/modules"):
			_, _ = w.Write(modulePayload)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/scanpolicies/existing"):
			_, _ = w.Write([]byte(existing))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/scanpolicies/missing"):
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"apiVersion":"opendepot.defdev.io/v1alpha1","kind":"ScanPolicy","metadata":{"name":"gate","namespace":"team","resourceVersion":"8"},"spec":{}}`))
		case r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/scanpolicies/missing"):
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodPut:
			_, _ = w.Write([]byte(`{"apiVersion":"opendepot.defdev.io/v1alpha1","kind":"ScanPolicy","metadata":{"name":"existing","namespace":"team","resourceVersion":"8"},"spec":{}}`))
		case r.Method == http.MethodDelete:
			var options metav1.DeleteOptions
			if err := json.NewDecoder(r.Body).Decode(&options); err != nil || options.Preconditions == nil || options.Preconditions.ResourceVersion == nil || *options.Preconditions.ResourceVersion != "7" {
				w.WriteHeader(http.StatusConflict)
				return
			}

			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"apiVersion":"v1","kind":"Status","status":"Success"}`))
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	t.Cleanup(api.Close)

	return api
}

// wave6PolicySetup points the policy handlers at the fake API with the given anonymous and management flags.
func wave6PolicySetup(t *testing.T, anonymous, enabled bool) {
	t.Helper()

	api := wave6PolicyAPI(t)
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

	clientset := policyTestClientset(t, api.URL)
	policyRequestAuth = func(http.ResponseWriter, *http.Request) (*kubernetes.Clientset, *opendepotv1alpha1.GroupBinding, *opendepotv1alpha1.SecurityGroupBinding, string, error) {
		return clientset, nil, nil, "test-user", nil
	}
}

func wave6PolicyRouter() http.Handler {
	router := chi.NewRouter()
	router.HandleFunc("/opendepot/ui/v1/scan-policies/{namespace}", handleScanPolicies)
	router.HandleFunc("/opendepot/ui/v1/scan-policies/{namespace}/{name}", handleScanPolicies)

	return router
}

func wave6PolicyRequest(method, target, body string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	rec := httptest.NewRecorder()
	wave6PolicyRouter().ServeHTTP(rec, req)

	return rec
}

func TestWave6ScanPolicyDispatchAndGates(t *testing.T) {
	cases := []struct {
		name       string
		anonymous  bool
		enabled    bool
		method     string
		target     string
		body       string
		wantStatus int
	}{
		{name: "unsupported method returns 405", enabled: true, method: http.MethodPatch, target: "/opendepot/ui/v1/scan-policies/team", wantStatus: http.StatusMethodNotAllowed},
		{name: "anonymous create is forbidden", anonymous: true, enabled: true, method: http.MethodPost, target: "/opendepot/ui/v1/scan-policies/team", body: wave6PolicyBody, wantStatus: http.StatusForbidden},
		{name: "disabled management rejects writes with 404", enabled: false, method: http.MethodPost, target: "/opendepot/ui/v1/scan-policies/team", body: wave6PolicyBody, wantStatus: http.StatusNotFound},
		{name: "anonymous read is allowed", anonymous: true, enabled: true, method: http.MethodGet, target: "/opendepot/ui/v1/scan-policies/team/existing", wantStatus: http.StatusOK},
		{name: "disabled management still allows reads", enabled: false, method: http.MethodGet, target: "/opendepot/ui/v1/scan-policies/team/existing", wantStatus: http.StatusOK},
		{name: "empty namespace is a bad request", enabled: true, method: http.MethodGet, target: "/opendepot/ui/v1/scan-policies/", wantStatus: http.StatusNotFound},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wave6PolicySetup(t, tc.anonymous, tc.enabled)

			rec := wave6PolicyRequest(tc.method, tc.target, tc.body, map[string]string{"If-Match": "7"})
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tc.wantStatus, rec.Body.String())
			}
		})
	}
}

func TestWave6ScanPolicyMethodNotAllowedSetsAllowHeader(t *testing.T) {
	wave6PolicySetup(t, false, true)

	rec := wave6PolicyRequest(http.MethodPatch, "/opendepot/ui/v1/scan-policies/team/existing", "", nil)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}

	if allow := rec.Header().Get("Allow"); !strings.Contains(allow, http.MethodGet) {
		t.Fatalf("Allow = %q, want it to list GET", allow)
	}
}

func TestWave6ScanPolicyGetResponses(t *testing.T) {
	wave6PolicySetup(t, false, true)

	rec := wave6PolicyRequest(http.MethodGet, "/opendepot/ui/v1/scan-policies/team/existing", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	if !strings.Contains(rec.Body.String(), `"resourceVersion":"7"`) {
		t.Fatalf("body missing resourceVersion 7: %s", rec.Body.String())
	}

	rec = wave6PolicyRequest(http.MethodGet, "/opendepot/ui/v1/scan-policies/team/missing", "", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing policy status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
}

func TestWave6ScanPolicyCreateValidation(t *testing.T) {
	cases := []struct {
		name       string
		body       string
		wantStatus int
		wantText   string
	}{
		{name: "valid policy is created", body: wave6PolicyBody, wantStatus: http.StatusCreated},
		{name: "malformed JSON is rejected", body: `{"spec":`, wantStatus: http.StatusBadRequest, wantText: "invalid ScanPolicy JSON"},
		{name: "unknown field is rejected", body: `{"metadata":{"name":"gate"},"bogus":true}`, wantStatus: http.StatusBadRequest, wantText: "invalid ScanPolicy JSON"},
		{name: "invalid name is rejected", body: `{"metadata":{"name":"Bad_Name"},"spec":{}}`, wantStatus: http.StatusBadRequest, wantText: "DNS-compatible"},
		{name: "invalid severity is rejected", body: `{"metadata":{"name":"gate"},"spec":{"severityThreshold":"SEVERE"}}`, wantStatus: http.StatusBadRequest, wantText: "severityThreshold"},
		{name: "invalid target kind is rejected", body: `{"metadata":{"name":"gate"},"spec":{"targetRefs":[{"kind":"Widget","name":"allowed"}]}}`, wantStatus: http.StatusBadRequest, wantText: "targetRefs[0].kind"},
		{name: "missing target name is rejected", body: `{"metadata":{"name":"gate"},"spec":{"targetRefs":[{"kind":"Module"}]}}`, wantStatus: http.StatusBadRequest, wantText: "name is required"},
		{name: "wildcard target is rejected", body: `{"metadata":{"name":"gate"},"spec":{"targetRefs":[{"kind":"Module","name":"*"}]}}`, wantStatus: http.StatusBadRequest, wantText: "wildcard"},
		{name: "unknown target resource is rejected", body: `{"metadata":{"name":"gate"},"spec":{"targetRefs":[{"kind":"Module","name":"ghost"}]}}`, wantStatus: http.StatusBadRequest, wantText: "ghost"},
		{name: "exemption without reason is rejected", body: `{"metadata":{"name":"gate"},"spec":{"exemptions":[{"reason":" "}]}}`, wantStatus: http.StatusBadRequest, wantText: "reason is required"},
		{name: "namespace mismatch is rejected", body: `{"metadata":{"name":"gate","namespace":"other"},"spec":{}}`, wantStatus: http.StatusBadRequest, wantText: "must match the URL namespace"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wave6PolicySetup(t, false, true)

			rec := wave6PolicyRequest(http.MethodPost, "/opendepot/ui/v1/scan-policies/team", tc.body, nil)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tc.wantStatus, rec.Body.String())
			}

			if tc.wantText != "" && !strings.Contains(rec.Body.String(), tc.wantText) {
				t.Fatalf("body = %q, want it to contain %q", rec.Body.String(), tc.wantText)
			}
		})
	}
}

func TestWave6ScanPolicyBodyLimit(t *testing.T) {
	wave6PolicySetup(t, false, true)

	oversized := `{"metadata":{"name":"gate"},"spec":{"description":"` + strings.Repeat("x", maxScanPolicyBody) + `"}}`
	rec := wave6PolicyRequest(http.MethodPost, "/opendepot/ui/v1/scan-policies/team", oversized, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}

	if !strings.Contains(rec.Body.String(), "exceeds") {
		t.Fatalf("body = %q, want it to mention the size limit", rec.Body.String())
	}
}

func TestWave6ScanPolicyUpdatePreconditions(t *testing.T) {
	cases := []struct {
		name       string
		target     string
		body       string
		headers    map[string]string
		wantStatus int
		wantText   string
	}{
		{name: "missing If-Match is 428", target: "/opendepot/ui/v1/scan-policies/team/existing", body: wave6PolicyBody, wantStatus: http.StatusPreconditionRequired},
		{name: "body without resourceVersion is 409", target: "/opendepot/ui/v1/scan-policies/team/existing", body: `{"metadata":{"name":"existing"},"spec":{}}`, headers: map[string]string{"If-Match": "7"}, wantStatus: http.StatusConflict, wantText: "If-Match"},
		{name: "stale If-Match is 409", target: "/opendepot/ui/v1/scan-policies/team/existing", body: `{"metadata":{"name":"existing","resourceVersion":"7"},"spec":{}}`, headers: map[string]string{"If-Match": "3"}, wantStatus: http.StatusConflict, wantText: "If-Match"},
		{name: "body name must match URL name", target: "/opendepot/ui/v1/scan-policies/team/existing", body: `{"metadata":{"name":"other","resourceVersion":"7"},"spec":{}}`, headers: map[string]string{"If-Match": "7"}, wantStatus: http.StatusBadRequest, wantText: "must match the URL name"},
		{name: "matching If-Match updates the policy", target: "/opendepot/ui/v1/scan-policies/team/existing", body: `{"metadata":{"name":"existing","resourceVersion":"7"},"spec":{"severityThreshold":"HIGH","targetRefs":[{"kind":"Module","name":"allowed"}]}}`, headers: map[string]string{"If-Match": "7"}, wantStatus: http.StatusOK},
		{name: "update of missing policy is 404", target: "/opendepot/ui/v1/scan-policies/team/missing", body: `{"metadata":{"name":"missing","resourceVersion":"7"},"spec":{}}`, headers: map[string]string{"If-Match": "7"}, wantStatus: http.StatusNotFound},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wave6PolicySetup(t, false, true)

			rec := wave6PolicyRequest(http.MethodPut, tc.target, tc.body, tc.headers)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tc.wantStatus, rec.Body.String())
			}

			if tc.wantText != "" && !strings.Contains(rec.Body.String(), tc.wantText) {
				t.Fatalf("body = %q, want it to contain %q", rec.Body.String(), tc.wantText)
			}
		})
	}
}

func TestWave6ScanPolicyDeletePreconditions(t *testing.T) {
	cases := []struct {
		name       string
		target     string
		headers    map[string]string
		wantStatus int
	}{
		{name: "missing If-Match is 428", target: "/opendepot/ui/v1/scan-policies/team/existing", wantStatus: http.StatusPreconditionRequired},
		{name: "stale If-Match is 409", target: "/opendepot/ui/v1/scan-policies/team/existing", headers: map[string]string{"If-Match": "3"}, wantStatus: http.StatusConflict},
		{name: "matching If-Match deletes with 204", target: "/opendepot/ui/v1/scan-policies/team/existing", headers: map[string]string{"If-Match": "7"}, wantStatus: http.StatusNoContent},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wave6PolicySetup(t, false, true)

			rec := wave6PolicyRequest(http.MethodDelete, tc.target, "", tc.headers)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tc.wantStatus, rec.Body.String())
			}
		})
	}
}

func TestWave6ScanPolicyPreviewRejectsInvalidJSON(t *testing.T) {
	wave6PolicySetup(t, false, true)

	router := chi.NewRouter()
	router.Post("/opendepot/ui/v1/scan-policies/{namespace}/preview", handleScanPolicyPreviewRequest)

	req := httptest.NewRequest(http.MethodPost, "/opendepot/ui/v1/scan-policies/team/preview", strings.NewReader(`{"spec":`))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}
