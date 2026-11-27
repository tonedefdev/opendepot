package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
)

const (
	ociTestClientID     = "ci-client"
	ociTestClientSecret = "ci-secret-value"
	ociTestTokenPath    = "/token"
)

// ociTestIdP is a mock IdP token endpoint. It accepts one client ID and secret, and it counts the exchanges it
// receives.
type ociTestIdP struct {
	server *httptest.Server
	calls  atomic.Int32
}

// newOCITestIdP starts a token endpoint that issues token for the client credentials grant. A request with any other
// client ID or secret gets 401 invalid_client.
func newOCITestIdP(t *testing.T, clientSecret, token string, lifetime int64) *ociTestIdP {
	t.Helper()

	idp := &ociTestIdP{}
	idp.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		idp.calls.Add(1)

		if r.Method != http.MethodPost || r.URL.Path != ociTestTokenPath ||
			r.FormValue("grant_type") != "client_credentials" {
			http.Error(w, "unsupported request", http.StatusBadRequest)

			return
		}

		user, secret, ok := r.BasicAuth()
		if !ok || user != ociTestClientID || secret != clientSecret {
			w.Header().Set(ociTestContentType, "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"invalid_client"}`))

			return
		}

		w.Header().Set(ociTestContentType, "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": token,
			"token_type":   "bearer",
			"expires_in":   lifetime,
		})
	}))
	t.Cleanup(idp.server.Close)

	return idp
}

// setOCITokenURL sets --oidc-token-url for the test and restores it on cleanup.
func setOCITokenURL(t *testing.T, tokenURL string) {
	t.Helper()

	original := opendepotOIDCTokenURL
	opendepotOIDCTokenURL = &tokenURL
	t.Cleanup(func() { opendepotOIDCTokenURL = original })
}

// isolateOCICredentialCache gives the test an empty client credentials cache and restores the shared one on cleanup.
func isolateOCICredentialCache(t *testing.T) {
	t.Helper()

	original := ociCredentialCache
	ociCredentialCache = expirable.NewLRU[string, ociIdentity](ociTokenCacheMaxEntries, nil, ociIdentityCacheMaxAge)
	t.Cleanup(func() { ociCredentialCache = original })
}

// setOCIClock replaces the clock and restores it on cleanup. The test advances the returned pointer to move time.
func setOCIClock(t *testing.T, start time.Time) *time.Time {
	t.Helper()

	now := start
	original := ociNow
	ociNow = func() time.Time { return now }
	t.Cleanup(func() { ociNow = original })

	return &now
}

// captureOCILogs sends the package logger to a buffer and returns it, so a test can check what was logged.
func captureOCILogs(t *testing.T) *bytes.Buffer {
	t.Helper()

	buf := &bytes.Buffer{}
	original := logger
	logger = slog.New(slog.NewJSONHandler(buf, nil))
	t.Cleanup(func() { logger = original })

	return buf
}

// ociTestBasic returns an Authorization header that carries Basic credentials.
func ociTestBasic(username, password string) map[string]string {
	return map[string]string{
		ociTestAuthorization: ociTestBasicPrefix + base64.StdEncoding.EncodeToString([]byte(username+":"+password)),
	}
}

// ociTestClientCredentialsRequest returns a request that carries the test client credentials.
func ociTestClientCredentialsRequest() *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/v2/", nil)
	for key, value := range ociTestBasic(ociTestClientID, ociTestClientSecret) {
		req.Header.Set(key, value)
	}

	return req
}

func TestAuthenticateOCIRequestAnonymousIgnoresCredentials(t *testing.T) {
	newOCITestEnv(t, true)

	req := httptest.NewRequest(http.MethodGet, "/v2/", nil)
	req.Header.Set(ociTestAuthorization, ociTestBasicPrefix+"!!!not-base64")

	principal, status := authenticateOCIRequest(req)
	if status != 0 || principal == nil {
		t.Fatalf("anonymous status = %d, principal nil = %t; want an unauthenticated principal", status, principal == nil)
	}
}

func TestAuthenticateOCIRequestBearerTokens(t *testing.T) {
	env := newOCITestEnv(t, false)
	env.addBinding("*")

	t.Run("missing credentials are unauthorized", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v2/", nil)

		principal, status := authenticateOCIRequest(req)
		if principal != nil || status != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", status)
		}
	})

	t.Run("a bearer token in a matching group binds the principal", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v2/", nil)
		req.Header.Set(ociTestAuthorization, ociTestBearerPrefix+agentTestBearerToken(t, []string{ociTestGroup}))

		principal, status := authenticateOCIRequest(req)
		if status != 0 || principal == nil || principal.binding == nil || principal.binding.Name != "ocireader" {
			t.Fatalf("status = %d, principal = %+v", status, principal)
		}
	})

	t.Run("a token without a matching group is forbidden", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v2/", nil)
		req.Header.Set(ociTestAuthorization, ociTestBearerPrefix+agentTestBearerToken(t, []string{ociTestOther}))

		if _, status := authenticateOCIRequest(req); status != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", status)
		}
	})
}

func TestAuthenticateOCIRequestBasicBearerPassword(t *testing.T) {
	env := newOCITestEnv(t, false)
	env.addBinding("*")
	token := agentTestBearerToken(t, []string{ociTestGroup})

	req := httptest.NewRequest(http.MethodGet, "/v2/", nil)
	for key, value := range ociTestBasic("helm-user", token) {
		req.Header.Set(key, value)
	}

	principal, status := authenticateOCIRequest(req)
	if status != 0 || principal == nil {
		t.Fatalf("Basic bearer password status = %d, want success", status)
	}
}

func TestAuthenticateOCIRequestRejectsMalformedBasic(t *testing.T) {
	newOCITestEnv(t, false)

	req := httptest.NewRequest(http.MethodGet, "/v2/", nil)
	req.Header.Set(ociTestAuthorization, ociTestBasicPrefix+"!!!not-base64")

	if _, status := authenticateOCIRequest(req); status != http.StatusUnauthorized {
		t.Fatalf("malformed Basic status = %d, want 401", status)
	}
}

func TestAuthenticateOCIRequestBasicWithoutTokenExchangeIsUnauthorized(t *testing.T) {
	newOCITestEnv(t, false)
	isolateOCICredentialCache(t)
	setOCITokenURL(t, "")

	req := httptest.NewRequest(http.MethodGet, "/v2/", nil)
	for key, value := range ociTestBasic(ociTestClientID, "not-a-token") {
		req.Header.Set(key, value)
	}

	if _, status := authenticateOCIRequest(req); status != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 when no token URL is configured", status)
	}
}

func TestAuthenticateOCIRequestClientCredentialsExchangeIsCachedUntilExpiry(t *testing.T) {
	env := newOCITestEnv(t, false)
	env.addBinding("*")
	isolateOCICredentialCache(t)

	token := agentTestBearerToken(t, []string{ociTestGroup})
	idp := newOCITestIdP(t, ociTestClientSecret, token, 3600)
	setOCITokenURL(t, idp.server.URL+ociTestTokenPath)
	clock := setOCIClock(t, time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC))

	exchange := func() int {
		principal, status := authenticateOCIRequest(ociTestClientCredentialsRequest())
		if status == 0 && (principal == nil || principal.binding == nil) {
			t.Fatal("exchanged credentials did not bind a principal")
		}

		return status
	}

	if status := exchange(); status != 0 {
		t.Fatalf("first exchange status = %d, want success", status)
	}

	if calls := idp.calls.Load(); calls != 1 {
		t.Fatalf("IdP calls after first request = %d, want 1", calls)
	}

	if status := exchange(); status != 0 {
		t.Fatalf("cached status = %d, want success", status)
	}

	if calls := idp.calls.Load(); calls != 1 {
		t.Fatalf("IdP calls after cache hit = %d, want still 1", calls)
	}

	// The token lives for an hour and the cache keeps it for that hour less a minute, so 59m30s on the clock is past
	// the cached expiry and must exchange again.
	*clock = clock.Add(59*time.Minute + 30*time.Second)
	if status := exchange(); status != 0 {
		t.Fatalf("refresh status = %d, want success", status)
	}

	if calls := idp.calls.Load(); calls != 2 {
		t.Fatalf("IdP calls after expiry = %d, want 2", calls)
	}
}

func TestAuthenticateOCIRequestClientCredentialsShortLivedTokenExpiresOnTime(t *testing.T) {
	env := newOCITestEnv(t, false)
	env.addBinding("*")
	isolateOCICredentialCache(t)

	token := agentTestBearerToken(t, []string{ociTestGroup})
	idp := newOCITestIdP(t, ociTestClientSecret, token, 120)
	setOCITokenURL(t, idp.server.URL+ociTestTokenPath)
	clock := setOCIClock(t, time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC))

	if _, status := authenticateOCIRequest(ociTestClientCredentialsRequest()); status != 0 {
		t.Fatalf("first exchange status = %d, want success", status)
	}

	// A 120s token is cached for 60s, its lifetime less the one-minute margin, so 59s on the clock is still a hit.
	*clock = clock.Add(59 * time.Second)
	if _, status := authenticateOCIRequest(ociTestClientCredentialsRequest()); status != 0 {
		t.Fatalf("cached status = %d, want success", status)
	}

	if calls := idp.calls.Load(); calls != 1 {
		t.Fatalf("IdP calls before expiry = %d, want 1", calls)
	}

	*clock = clock.Add(2 * time.Second)
	if _, status := authenticateOCIRequest(ociTestClientCredentialsRequest()); status != 0 {
		t.Fatalf("refresh status = %d, want success", status)
	}

	if calls := idp.calls.Load(); calls != 2 {
		t.Fatalf("IdP calls after expiry = %d, want 2: a 120s token must expire before its lifetime", calls)
	}
}

// ociTestCachedCredentialsEnv returns an OCI test environment whose client credentials are exchanged with a mock IdP.
// The first exchange has already been cached when it returns. It also returns the IdP and the request target.
func ociTestCachedCredentialsEnv(t *testing.T, lifetime int64) (*ociTestEnv, *ociTestIdP, string) {
	t.Helper()

	env := newOCITestEnv(t, false)
	env.addBinding("*")
	env.addArtifact("podinfo-app", "podinfo", "Image")
	env.publishImage(t, "podinfo-app", ociTestTagOne)
	isolateOCICredentialCache(t)

	token := agentTestBearerToken(t, []string{ociTestGroup})
	idp := newOCITestIdP(t, ociTestClientSecret, token, lifetime)
	setOCITokenURL(t, idp.server.URL+ociTestTokenPath)
	setOCIClock(t, time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC))

	target := "/v2/" + ociTestNamespace + "/podinfo/manifests/" + ociTestTagOne
	assertOCIStatus(t, ociTestRequest(http.MethodGet, target, ociTestBasic(ociTestClientID, ociTestClientSecret)),
		http.StatusOK, "")

	return env, idp, target
}

func TestOCIClientCredentialsNarrowedBindingAppliesToCachedCredentials(t *testing.T) {
	env, idp, target := ociTestCachedCredentialsEnv(t, 3600)

	env.cluster.mu.Lock()
	env.cluster.bindings[0].Spec.OCIArtifactResources = []string{"unlisted-*"}
	env.cluster.mu.Unlock()

	rec := ociTestRequest(http.MethodGet, target, ociTestBasic(ociTestClientID, ociTestClientSecret))
	assertOCIStatus(t, rec, http.StatusNotFound, ociCodeNameUnknown)

	if calls := idp.calls.Load(); calls != 1 {
		t.Fatalf("IdP calls = %d, want 1: the cached identity should be reused", calls)
	}
}

func TestOCIClientCredentialsDeletedBindingAppliesToCachedCredentials(t *testing.T) {
	env, idp, target := ociTestCachedCredentialsEnv(t, 3600)

	env.cluster.mu.Lock()
	env.cluster.bindings = nil
	env.cluster.mu.Unlock()

	rec := ociTestRequest(http.MethodGet, target, ociTestBasic(ociTestClientID, ociTestClientSecret))
	assertOCIStatus(t, rec, http.StatusForbidden, ociCodeDenied)

	if calls := idp.calls.Load(); calls != 1 {
		t.Fatalf("IdP calls = %d, want 1: the cached identity should be reused", calls)
	}
}

func TestAuthenticateOCIRequestClientCredentialsCacheLifetimeIsCapped(t *testing.T) {
	env := newOCITestEnv(t, false)
	env.addBinding("*")
	isolateOCICredentialCache(t)

	token := agentTestBearerToken(t, []string{ociTestGroup})
	idp := newOCITestIdP(t, ociTestClientSecret, token, int64((24 * time.Hour).Seconds()))
	setOCITokenURL(t, idp.server.URL+ociTestTokenPath)
	clock := setOCIClock(t, time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC))

	exchange := func() {
		t.Helper()

		principal, status := authenticateOCIRequest(ociTestClientCredentialsRequest())
		if status != 0 || principal == nil || principal.binding == nil {
			t.Fatalf("exchange status = %d, principal = %v, want a bound principal", status, principal)
		}
	}

	exchange()
	if calls := idp.calls.Load(); calls != 1 {
		t.Fatalf("IdP calls after first exchange = %d, want 1", calls)
	}

	*clock = clock.Add(ociIdentityCacheMaxAge - time.Second)
	exchange()
	if calls := idp.calls.Load(); calls != 1 {
		t.Fatalf("IdP calls just before the cap = %d, want still 1", calls)
	}

	*clock = clock.Add(2 * time.Second)
	exchange()
	if calls := idp.calls.Load(); calls != 2 {
		t.Fatalf("IdP calls after the cap = %d, want 2: a 24h token must not be cached past five minutes", calls)
	}
}

func TestAuthenticateOCIRequestClientCredentialsRejectsBadSecret(t *testing.T) {
	env := newOCITestEnv(t, false)
	env.addBinding("*")
	isolateOCICredentialCache(t)

	const rightSecret = "right-secret-7731"
	const wrongSecret = "wrong-secret-4417"

	token := agentTestBearerToken(t, []string{ociTestGroup})
	idp := newOCITestIdP(t, rightSecret, token, 3600)
	setOCITokenURL(t, idp.server.URL+ociTestTokenPath)
	setOCIClock(t, time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC))
	logs := captureOCILogs(t)

	req := httptest.NewRequest(http.MethodGet, "/v2/", nil)
	for key, value := range ociTestBasic(ociTestClientID, wrongSecret) {
		req.Header.Set(key, value)
	}

	if _, status := authenticateOCIRequest(req); status != http.StatusUnauthorized {
		t.Fatalf("bad secret status = %d, want 401", status)
	}

	if calls := idp.calls.Load(); calls != 1 {
		t.Fatalf("IdP calls = %d, want 1", calls)
	}

	if ociCredentialCache.Len() != 0 {
		t.Fatal("a rejected exchange was cached")
	}

	for _, secret := range []string{wrongSecret, rightSecret, ociTestClientID, token} {
		if strings.Contains(logs.String(), secret) {
			t.Fatalf("log output leaks %q: %s", secret, logs.String())
		}
	}
}

func TestAuthenticateOCIRequestClientCredentialsDoesNotFollowRedirects(t *testing.T) {
	env := newOCITestEnv(t, false)
	env.addBinding("*")
	isolateOCICredentialCache(t)
	setOCIClock(t, time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC))

	var leaked atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		leaked.Add(1)
	}))
	t.Cleanup(target.Close)

	redirecting := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+ociTestTokenPath, http.StatusTemporaryRedirect)
	}))
	t.Cleanup(redirecting.Close)
	setOCITokenURL(t, redirecting.URL+ociTestTokenPath)

	if _, status := authenticateOCIRequest(ociTestClientCredentialsRequest()); status != http.StatusUnauthorized {
		t.Fatalf("redirect status = %d, want 401", status)
	}

	if leaked.Load() != 0 {
		t.Fatal("the client secret was sent to a redirect target")
	}
}

func TestAuthenticateOCIRequestRefusesInsecureTokenURL(t *testing.T) {
	env := newOCITestEnv(t, false)
	env.addBinding("*")
	isolateOCICredentialCache(t)
	setOCITokenURL(t, "http://idp.example.com"+ociTestTokenPath)
	logs := captureOCILogs(t)

	if _, status := authenticateOCIRequest(ociTestClientCredentialsRequest()); status != http.StatusUnauthorized {
		t.Fatalf("insecure token URL status = %d, want 401", status)
	}

	if !strings.Contains(logs.String(), "HTTPS or a loopback host") {
		t.Fatalf("expected the insecure URL to be refused, logs: %s", logs.String())
	}

	if strings.Contains(logs.String(), ociTestClientSecret) {
		t.Fatal("log output leaks the client secret")
	}
}

func TestOCITokenURLRequiresHTTPSOrLoopback(t *testing.T) {
	newOCITestEnv(t, false)

	cases := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{name: "https", raw: "https://idp.example.com/token"},
		{name: "loopback IPv4 over http", raw: "http://127.0.0.1:9/token"},
		{name: "localhost over http", raw: "http://localhost:9/token"},
		{name: "remote over http", raw: "http://idp.example.com/token", wantErr: true},
		{name: "userinfo", raw: "https://user:pass@idp.example.com/token", wantErr: true},
		{name: "unset", raw: "", wantErr: true},
		{name: "unsupported scheme", raw: "ftp://idp.example.com/token", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setOCITokenURL(t, tc.raw)

			_, err := ociTokenURL()
			if (err != nil) != tc.wantErr {
				t.Fatalf("ociTokenURL(%q) error = %v, wantErr %t", tc.raw, err, tc.wantErr)
			}
		})
	}
}

func TestOCIArtifactVisibility(t *testing.T) {
	cases := []struct {
		name     string
		binding  bool
		patterns []string
		resource string
		want     bool
	}{
		{name: "no binding is governed by RBAC", resource: "anything", want: true},
		{name: "empty grant denies", binding: true, resource: "podinfo", want: false},
		{
			name:     "glob grant allows a match",
			binding:  true,
			patterns: []string{"podinfo*"},
			resource: "podinfo-app",
			want:     true,
		},
		{
			name:     "glob grant denies a miss",
			binding:  true,
			patterns: []string{"podinfo*"},
			resource: ociTestOther,
			want:     false,
		},
		{name: "wildcard grant allows everything", binding: true, patterns: []string{"*"}, resource: "x", want: true},
		{name: "malformed pattern is skipped", binding: true, patterns: []string{"["}, resource: "x", want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			principal := &ociPrincipal{}
			if tc.binding {
				principal.binding = &opendepotv1alpha1.GroupBinding{
					Spec: opendepotv1alpha1.GroupBindingSpec{OCIArtifactResources: tc.patterns},
				}
			}

			if got := ociArtifactVisible(principal, tc.resource); got != tc.want {
				t.Fatalf("ociArtifactVisible() = %t, want %t", got, tc.want)
			}
		})
	}
}

func TestOCICredentialCacheKeyHidesCredentials(t *testing.T) {
	key := ociCredentialCacheKey(ociTestClientID, ociTestClientSecret)

	if len(key) != 64 {
		t.Fatalf("key length = %d, want a 64-character SHA-256 hex digest", len(key))
	}

	if strings.Contains(key, ociTestClientID) || strings.Contains(key, ociTestClientSecret) {
		t.Fatalf("key %q exposes the credentials", key)
	}

	if key == ociCredentialCacheKey(ociTestClientID, "other-secret") {
		t.Fatal("different secrets produced the same key")
	}

	if ociCredentialCacheKey("ab", "c") == ociCredentialCacheKey("a", "bc") {
		t.Fatal("the NUL separator does not keep the client ID and secret apart")
	}
}

func TestOCICredentialCacheIsBounded(t *testing.T) {
	isolateOCICredentialCache(t)

	for i := range ociTokenCacheMaxEntries + 1 {
		ociCredentialCache.Add(ociCredentialCacheKey(ociTestClientID, strconv.Itoa(i)), ociIdentity{subject: ociTestClientID})
	}

	if got := ociCredentialCache.Len(); got > ociTokenCacheMaxEntries {
		t.Fatalf("cache holds %d entries, want at most %d", got, ociTokenCacheMaxEntries)
	}
}

func TestHasBasicScheme(t *testing.T) {
	cases := map[string]bool{
		"Basic x":  true,
		"basic x":  true,
		"Bearer x": false,
		"Basic":    false,
		"":         false,
	}

	for header, want := range cases {
		if got := hasBasicScheme(header); got != want {
			t.Fatalf("hasBasicScheme(%q) = %t, want %t", header, got, want)
		}
	}
}
