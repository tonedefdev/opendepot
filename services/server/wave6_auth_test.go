package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// wave6AuthGlobals snapshots the auth globals touched by these tests and restores them on cleanup.
func wave6AuthGlobals(t *testing.T) {
	t.Helper()

	originalAnonymous := opendepotAnonymousAuth
	originalUseBearerToken := opendepotUseBearerToken
	originalAllowSA := opendepotOIDCAllowSAFallback
	originalAllowCC := opendepotOIDCAllowClientCredentials
	originalIssuer := opendepotOIDCIssuerURL
	originalGroupsClaim := opendepotOIDCGroupsClaim
	originalNamespace := opendepotServerNamespace
	originalVerifier := oidcVerifier
	originalCCVerifiers := oidcCCVerifiers
	originalInClusterConfig := inClusterConfig
	t.Cleanup(func() {
		opendepotAnonymousAuth = originalAnonymous
		opendepotUseBearerToken = originalUseBearerToken
		opendepotOIDCAllowSAFallback = originalAllowSA
		opendepotOIDCAllowClientCredentials = originalAllowCC
		opendepotOIDCIssuerURL = originalIssuer
		opendepotOIDCGroupsClaim = originalGroupsClaim
		opendepotServerNamespace = originalNamespace
		oidcVerifier = originalVerifier
		oidcCCVerifiers = originalCCVerifiers
		inClusterConfig = originalInClusterConfig
	})

	anonymous := false
	useBearerToken := false
	allowSA := false
	allowCC := false
	issuer := agentTestIssuer
	groupsClaim := "groups"
	namespace := "opendepot-system"
	opendepotAnonymousAuth = &anonymous
	opendepotUseBearerToken = &useBearerToken
	opendepotOIDCAllowSAFallback = &allowSA
	opendepotOIDCAllowClientCredentials = &allowCC
	opendepotOIDCIssuerURL = &issuer
	opendepotOIDCGroupsClaim = &groupsClaim
	opendepotServerNamespace = &namespace
	oidcVerifier = gooidc.NewVerifier(agentTestIssuer, agentTestKeySet{}, &gooidc.Config{ClientID: "opendepot"})
	oidcCCVerifiers = nil
}

// wave6GroupBindingAPI serves GroupBinding and SecurityGroupBinding lists and reports a
// failure for the paths listed in failPaths.
func wave6GroupBindingAPI(t *testing.T, bindings []opendepotv1alpha1.GroupBinding, failPaths ...string) *httptest.Server {
	t.Helper()

	bindingPayload := agentTestJSON(t, opendepotv1alpha1.GroupBindingList{Items: bindings})
	securityPayload := agentTestJSON(t, opendepotv1alpha1.SecurityGroupBindingList{})
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, fail := range failPaths {
			if strings.HasSuffix(r.URL.Path, fail) {
				http.Error(w, "boom", http.StatusInternalServerError)

				return
			}
		}

		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/groupbindings"):
			_, _ = w.Write(bindingPayload)
		case strings.HasSuffix(r.URL.Path, "/securitygroupbindings"):
			_, _ = w.Write(securityPayload)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(api.Close)

	inClusterConfig = func() (*rest.Config, error) {
		return &rest.Config{Host: api.URL}, nil
	}

	return api
}

func wave6Binding(name, expression string) opendepotv1alpha1.GroupBinding {
	return opendepotv1alpha1.GroupBinding{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: opendepotv1alpha1.GroupBindingSpec{
			Expression:      expression,
			ModuleResources: []string{"net*"},
		},
	}
}

func TestWave6ExtractGroupsClaim(t *testing.T) {
	cases := []struct {
		name    string
		claims  map[string]any
		want    []string
		wantErr string
	}{
		{name: "missing claim", claims: map[string]any{}, wantErr: "not present"},
		{name: "array claim", claims: map[string]any{"groups": []any{"a", "b"}}, want: []string{"a", "b"}},
		{name: "empty array claim", claims: map[string]any{"groups": []any{}}, want: []string{}},
		{name: "single string claim", claims: map[string]any{"groups": "solo"}, want: []string{"solo"}},
		{name: "non-string array item", claims: map[string]any{"groups": []any{"a", 7}}, wantErr: "non-string"},
		{name: "unexpected type", claims: map[string]any{"groups": 42}, wantErr: "unexpected type"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := extractGroupsClaim(tc.claims, "groups")
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tc.wantErr)
				}

				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if strings.Join(got, ",") != strings.Join(tc.want, ",") || len(got) != len(tc.want) {
				t.Fatalf("groups = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestWave6ParseUnsignedJWTIssuer(t *testing.T) {
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"iss":"https://issuer.example.com"}`))
	if iss, err := parseUnsignedJWTIssuer("h." + payload + ".s"); err != nil || iss != "https://issuer.example.com" {
		t.Fatalf("iss = %q, err = %v", iss, err)
	}

	if _, err := parseUnsignedJWTIssuer("not-a-jwt"); err == nil {
		t.Fatal("expected error for malformed token")
	}

	bad := base64.RawURLEncoding.EncodeToString([]byte(`not json`))
	if _, err := parseUnsignedJWTIssuer("h." + bad + ".s"); err == nil {
		t.Fatal("expected error for non-JSON payload")
	}
}

func TestWave6IsResourceAllowed(t *testing.T) {
	binding := &opendepotv1alpha1.GroupBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "platform"},
		Spec: opendepotv1alpha1.GroupBindingSpec{
			ModuleResources:   []string{"net*", "[", "vpc"},
			ProviderResources: []string{"aws", "*"},
			SkillResources:    []string{"skill-*", "["},
			AgentResources:    []string{"agent-?", "["},
		},
	}

	cases := []struct {
		name         string
		resourceType string
		resource     string
		want         bool
	}{
		{name: "module glob match", resourceType: "module", resource: "network", want: true},
		{name: "module exact match", resourceType: "module", resource: "vpc", want: true},
		{name: "module malformed pattern skipped", resourceType: "module", resource: "[", want: false},
		{name: "module no match", resourceType: "module", resource: "storage", want: false},
		{name: "provider exact match", resourceType: "provider", resource: "aws", want: true},
		{name: "provider wildcard allows any", resourceType: "provider", resource: "gcp", want: true},
		{name: "skill glob match", resourceType: "skill", resource: "skill-lint", want: true},
		{name: "skill malformed pattern skipped", resourceType: "skill", resource: "[", want: false},
		{name: "skill no match", resourceType: "skill", resource: "tool", want: false},
		{name: "agent single char match", resourceType: "agent", resource: "agent-1", want: true},
		{name: "agent malformed pattern skipped", resourceType: "agent", resource: "[", want: false},
		{name: "agent no match", resourceType: "agent", resource: "agent-12", want: false},
		{name: "unknown kind denied", resourceType: "widget", resource: "anything", want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isResourceAllowed(binding, tc.resourceType, tc.resource); got != tc.want {
				t.Fatalf("isResourceAllowed(%s, %q) = %v, want %v", tc.resourceType, tc.resource, got, tc.want)
			}
		})
	}
}

func TestWave6FindGroupBinding(t *testing.T) {
	ctx := context.Background()

	t.Run("first sorted matching binding wins", func(t *testing.T) {
		wave6AuthGlobals(t)
		wave6GroupBindingAPI(t, []opendepotv1alpha1.GroupBinding{
			wave6Binding("zeta", `"dev" in groups`),
			wave6Binding("alpha", `"dev" in groups`),
		})

		cs := wave6Clientset(t)
		binding, err := findGroupBinding(ctx, cs, []string{"dev"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if binding.Name != "alpha" {
			t.Fatalf("binding = %q, want alpha", binding.Name)
		}
	})

	t.Run("no match is an error", func(t *testing.T) {
		wave6AuthGlobals(t)
		wave6GroupBindingAPI(t, []opendepotv1alpha1.GroupBinding{wave6Binding("ops", `"ops" in groups`)})

		if _, err := findGroupBinding(ctx, wave6Clientset(t), []string{"dev"}); err == nil || !strings.Contains(err.Error(), "no GroupBinding matched") {
			t.Fatalf("err = %v, want no-match error", err)
		}
	})

	t.Run("invalid expression fails closed", func(t *testing.T) {
		wave6AuthGlobals(t)
		wave6GroupBindingAPI(t, []opendepotv1alpha1.GroupBinding{wave6Binding("broken", "groups ===")})

		if _, err := findGroupBinding(ctx, wave6Clientset(t), []string{"dev"}); err == nil || !strings.Contains(err.Error(), "is invalid") {
			t.Fatalf("err = %v, want invalid-expression error", err)
		}
	})

	t.Run("evaluation failure fails closed", func(t *testing.T) {
		wave6AuthGlobals(t)
		wave6GroupBindingAPI(t, []opendepotv1alpha1.GroupBinding{wave6Binding("bad-type", "groups[0] + 1")})

		if _, err := findGroupBinding(ctx, wave6Clientset(t), []string{"dev"}); err == nil {
			t.Fatal("expected evaluation error")
		}
	})

	t.Run("listing failure is an error", func(t *testing.T) {
		wave6AuthGlobals(t)
		wave6GroupBindingAPI(t, nil, "/groupbindings")

		if _, err := findGroupBinding(ctx, wave6Clientset(t), []string{"dev"}); err == nil || !strings.Contains(err.Error(), "failed to list GroupBindings") {
			t.Fatalf("err = %v, want list error", err)
		}
	})
}

func TestWave6FindSecurityGroupBinding(t *testing.T) {
	ctx := context.Background()

	t.Run("no match is an error", func(t *testing.T) {
		wave6AuthGlobals(t)
		wave6GroupBindingAPI(t, nil)

		if _, err := findSecurityGroupBinding(ctx, wave6Clientset(t), []string{"dev"}); err == nil || !strings.Contains(err.Error(), "no SecurityGroupBinding matched") {
			t.Fatalf("err = %v, want no-match error", err)
		}
	})

	t.Run("listing failure is an error", func(t *testing.T) {
		wave6AuthGlobals(t)
		wave6GroupBindingAPI(t, nil, "/securitygroupbindings")

		if _, err := findSecurityGroupBinding(ctx, wave6Clientset(t), []string{"dev"}); err == nil || !strings.Contains(err.Error(), "failed to list SecurityGroupBindings") {
			t.Fatalf("err = %v, want list error", err)
		}
	})
}

func TestWave6GetKubeClientFromRequestOIDC(t *testing.T) {
	cases := []struct {
		name            string
		authorization   string
		token           string
		bindings        []opendepotv1alpha1.GroupBinding
		failPaths       []string
		requireBinding  bool
		wantStatus      int
		wantBindingName string
	}{
		{name: "missing header is 401", wantStatus: http.StatusUnauthorized, requireBinding: true},
		{name: "non-bearer scheme is 401", authorization: "Basic abc", wantStatus: http.StatusUnauthorized, requireBinding: true},
		{name: "invalid signature token is 401", authorization: "Bearer not.a.token", wantStatus: http.StatusUnauthorized, requireBinding: true},
		{
			name:            "groups without matching binding is 403 when required",
			token:           "dev",
			bindings:        []opendepotv1alpha1.GroupBinding{wave6Binding("ops", `"ops" in groups`)},
			requireBinding:  true,
			wantStatus:      http.StatusForbidden,
			wantBindingName: "",
		},
		{
			name:            "matching binding is returned",
			token:           "dev",
			bindings:        []opendepotv1alpha1.GroupBinding{wave6Binding("dev-binding", `"dev" in groups`)},
			requireBinding:  true,
			wantStatus:      http.StatusOK,
			wantBindingName: "dev-binding",
		},
		{
			name:           "listing failure is 403 when binding required",
			token:          "dev",
			failPaths:      []string{"/groupbindings"},
			requireBinding: true,
			wantStatus:     http.StatusForbidden,
		},
		{
			name:           "policy route without SecurityGroupBinding is 403 even when GroupBinding is optional",
			token:          "dev",
			bindings:       []opendepotv1alpha1.GroupBinding{wave6Binding("dev-binding", `"dev" in groups`)},
			requireBinding: false,
			wantStatus:     http.StatusForbidden,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wave6AuthGlobals(t)
			wave6GroupBindingAPI(t, tc.bindings, tc.failPaths...)

			authorization := tc.authorization
			if tc.token != "" {
				authorization = "Bearer " + agentTestBearerToken(t, []string{tc.token})
			}

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if authorization != "" {
				req.Header.Set("Authorization", authorization)
			}

			recorder := httptest.NewRecorder()
			_, binding, _, _, err := getKubeClientFromRequestWithGroupBinding(recorder, req, tc.requireBinding)
			if tc.wantStatus != http.StatusOK && recorder.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", recorder.Code, tc.wantStatus)
			}

			if tc.wantStatus == http.StatusOK {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}

				gotName := ""
				if binding != nil {
					gotName = binding.Name
				}

				if gotName != tc.wantBindingName {
					t.Fatalf("binding = %q, want %q", gotName, tc.wantBindingName)
				}

				return
			}

			if err == nil {
				t.Fatalf("expected error for status %d", tc.wantStatus)
			}
		})
	}
}

func TestWave6AnonymousAuthSkipsOIDC(t *testing.T) {
	wave6AuthGlobals(t)
	anonymous := true
	opendepotAnonymousAuth = &anonymous
	wave6GroupBindingAPI(t, nil)

	recorder := httptest.NewRecorder()
	cs, binding, _, sub, err := getKubeClientFromRequestWithGroupBinding(recorder, httptest.NewRequest(http.MethodGet, "/", nil), true)
	if err != nil || cs == nil || binding != nil || sub != "" {
		t.Fatalf("cs = %v, binding = %v, sub = %q, err = %v", cs != nil, binding, sub, err)
	}
}

func TestWave6ClientCredentialsToken(t *testing.T) {
	wave6AuthGlobals(t)

	allow := true
	opendepotOIDCAllowClientCredentials = &allow
	oidcCCVerifiers = []*gooidc.IDTokenVerifier{gooidc.NewVerifier(agentTestIssuer, agentTestKeySet{}, &gooidc.Config{ClientID: "ci-pipeline"})}

	ccToken := func(sub string) string {
		header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
		claims := map[string]any{
			"iss": agentTestIssuer,
			"aud": "ci-pipeline",
			"iat": time.Now().Unix(),
			"exp": time.Now().Add(time.Hour).Unix(),
		}
		if sub != "" {
			claims["sub"] = sub
		}

		payload, err := json.Marshal(claims)
		if err != nil {
			t.Fatalf("marshal claims: %v", err)
		}

		return header + "." + base64.RawURLEncoding.EncodeToString(payload) + ".c2ln"
	}

	t.Run("wrong issuer falls through", func(t *testing.T) {
		original := opendepotOIDCIssuerURL
		t.Cleanup(func() { opendepotOIDCIssuerURL = original })
		other := "https://other.example.com"
		opendepotOIDCIssuerURL = &other

		recorder := httptest.NewRecorder()
		cs, binding, _, sub, err := handleClientCredentialsToken(recorder, httptest.NewRequest(http.MethodGet, "/", nil), ccToken("svc"))
		if cs != nil || binding != nil || sub != "" || err != nil || recorder.Code != http.StatusOK {
			t.Fatalf("expected fall-through, got cs=%v binding=%v sub=%q err=%v code=%d", cs != nil, binding, sub, err, recorder.Code)
		}
	})

	t.Run("malformed token falls through", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		cs, _, _, _, err := handleClientCredentialsToken(recorder, httptest.NewRequest(http.MethodGet, "/", nil), "bad")
		if cs != nil || err != nil {
			t.Fatalf("expected fall-through, got cs=%v err=%v", cs != nil, err)
		}
	})

	t.Run("token without sub falls through", func(t *testing.T) {
		wave6GroupBindingAPI(t, nil)

		recorder := httptest.NewRecorder()
		cs, _, _, _, err := handleClientCredentialsToken(recorder, httptest.NewRequest(http.MethodGet, "/", nil), ccToken(""))
		if cs != nil || err != nil {
			t.Fatalf("expected fall-through, got cs=%v err=%v", cs != nil, err)
		}
	})

	t.Run("matching client binding is accepted", func(t *testing.T) {
		wave6GroupBindingAPI(t, []opendepotv1alpha1.GroupBinding{wave6Binding("ci", `"client:svc-ci" in groups`)})

		recorder := httptest.NewRecorder()
		cs, binding, _, sub, err := handleClientCredentialsToken(recorder, httptest.NewRequest(http.MethodGet, "/", nil), ccToken("svc-ci"))
		if err != nil || cs == nil || binding == nil || binding.Name != "ci" || sub != "svc-ci" {
			t.Fatalf("cs=%v binding=%v sub=%q err=%v", cs != nil, binding, sub, err)
		}
	})

	t.Run("no matching client binding is 403", func(t *testing.T) {
		wave6GroupBindingAPI(t, []opendepotv1alpha1.GroupBinding{wave6Binding("ci", `"client:other" in groups`)})

		recorder := httptest.NewRecorder()
		cs, _, _, _, err := handleClientCredentialsToken(recorder, httptest.NewRequest(http.MethodGet, "/", nil), ccToken("svc-ci"))
		if cs != nil || err == nil || recorder.Code != http.StatusForbidden {
			t.Fatalf("cs=%v err=%v code=%d, want 403", cs != nil, err, recorder.Code)
		}
	})

	t.Run("client credentials fallback is used by request path", func(t *testing.T) {
		allowCC := true
		opendepotOIDCAllowClientCredentials = &allowCC
		wave6GroupBindingAPI(t, []opendepotv1alpha1.GroupBinding{wave6Binding("ci", `"client:svc-ci" in groups`)})

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer "+ccToken("svc-ci"))
		recorder := httptest.NewRecorder()
		_, binding, _, sub, err := getKubeClientFromRequestWithGroupBinding(recorder, req, true)
		if err != nil || binding == nil || binding.Name != "ci" || sub != "svc-ci" {
			t.Fatalf("binding=%v sub=%q err=%v", binding, sub, err)
		}
	})
}

func TestWave6ExtractKubeconfig(t *testing.T) {
	t.Run("missing header is 401", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		if _, err := extractKubeconfig(recorder, httptest.NewRequest(http.MethodGet, "/", nil)); err == nil || recorder.Code != http.StatusUnauthorized {
			t.Fatalf("err=%v code=%d, want 401", err, recorder.Code)
		}
	})

	t.Run("invalid base64 is 500", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer !!!not-base64!!!")
		recorder := httptest.NewRecorder()
		if _, err := extractKubeconfig(recorder, req); err == nil || recorder.Code != http.StatusInternalServerError {
			t.Fatalf("err=%v code=%d, want 500", err, recorder.Code)
		}
	})

	t.Run("valid base64 kubeconfig is decoded", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer "+base64.StdEncoding.EncodeToString([]byte("kubeconfig-body")))
		recorder := httptest.NewRecorder()
		got, err := extractKubeconfig(recorder, req)
		if err != nil || string(got) != "kubeconfig-body" {
			t.Fatalf("got %q err=%v", got, err)
		}
	})
}

func TestWave6GetPolicyKubeClientFromRequestRequiresSecurityBinding(t *testing.T) {
	wave6AuthGlobals(t)
	wave6GroupBindingAPI(t, []opendepotv1alpha1.GroupBinding{wave6Binding("dev-binding", `"dev" in groups`)})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+agentTestBearerToken(t, []string{"dev"}))
	recorder := httptest.NewRecorder()
	cs, _, _, _, err := getPolicyKubeClientFromRequest(recorder, req)
	if err == nil || cs != nil || recorder.Code != http.StatusForbidden {
		t.Fatalf("cs=%v err=%v code=%d, want 403", cs != nil, err, recorder.Code)
	}
}

func TestWave6BearerTokenMode(t *testing.T) {
	wave6AuthGlobals(t)
	wave6GroupBindingAPI(t, nil)
	useBearer := true
	opendepotUseBearerToken = &useBearer
	oidcVerifier = nil

	t.Run("missing header is 401", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		if _, _, _, _, err := getKubeClientFromRequestWithGroupBinding(recorder, httptest.NewRequest(http.MethodGet, "/", nil), true); err == nil || recorder.Code != http.StatusUnauthorized {
			t.Fatalf("err=%v code=%d, want 401", err, recorder.Code)
		}
	})

	t.Run("bearer token builds a client without a binding", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer sa-token")
		recorder := httptest.NewRecorder()
		cs, binding, _, sub, err := getKubeClientFromRequestWithGroupBinding(recorder, req, true)
		if err != nil || cs == nil || binding != nil || sub != "" {
			t.Fatalf("cs=%v binding=%v sub=%q err=%v", cs != nil, binding, sub, err)
		}
	})
}

func TestWave6SAFallbackForForeignIssuer(t *testing.T) {
	wave6AuthGlobals(t)
	wave6GroupBindingAPI(t, nil)
	allowSA := true
	opendepotOIDCAllowSAFallback = &allowSA

	foreign := "https://kubernetes.default.svc"
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"iss":"` + foreign + `"}`))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer h."+payload+".s")
	recorder := httptest.NewRecorder()
	cs, binding, _, sub, err := getKubeClientFromRequestWithGroupBinding(recorder, req, true)
	if err != nil || cs == nil || binding != nil || sub != "" {
		t.Fatalf("cs=%v binding=%v sub=%q err=%v", cs != nil, binding, sub, err)
	}

	t.Run("OIDC issuer token that fails verification stays 401", func(t *testing.T) {
		issuerPayload := base64.RawURLEncoding.EncodeToString([]byte(`{"iss":"` + agentTestIssuer + `"}`))
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer h."+issuerPayload+".s")
		recorder := httptest.NewRecorder()
		if _, _, _, _, err := getKubeClientFromRequestWithGroupBinding(recorder, req, true); err == nil || recorder.Code != http.StatusUnauthorized {
			t.Fatalf("err=%v code=%d, want 401", err, recorder.Code)
		}
	})
}

func TestWave6MissingGroupsClaimIsForbidden(t *testing.T) {
	wave6AuthGlobals(t)
	wave6GroupBindingAPI(t, nil)

	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	claims, err := json.Marshal(map[string]any{
		"iss": agentTestIssuer,
		"aud": "opendepot",
		"sub": "alice",
		"iat": time.Now().Unix(),
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("marshal claims: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+header+"."+base64.RawURLEncoding.EncodeToString(claims)+".c2ln")
	recorder := httptest.NewRecorder()
	if _, _, _, _, err := getKubeClientFromRequestWithGroupBinding(recorder, req, true); err == nil || recorder.Code != http.StatusForbidden {
		t.Fatalf("err=%v code=%d, want 403", err, recorder.Code)
	}
}

// wave6Clientset builds a clientset whose REST host is the fake API server set by inClusterConfig.
func wave6Clientset(t *testing.T) *kubernetes.Clientset {
	t.Helper()

	cs, err := generateKubeClient(nil, nil, false)
	if err != nil {
		t.Fatalf("generate client: %v", err)
	}

	return cs
}
