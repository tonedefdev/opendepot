package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// syncBuffer lets a test wait for login to write the authorization URL while login is still blocked.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.buf.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.buf.String()
}

// loginServer serves discovery and the token endpoint for a login.v1 registry.
func loginServer(t *testing.T, tokenStatus int, tokenBody string, ports []int) *httptest.Server {
	t.Helper()

	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/terraform.json":
			login := map[string]any{
				"client":      "opendepot-cli",
				"authz":       srv.URL + "/authorize",
				"token":       srv.URL + "/token",
				"ports":       ports,
				"scopes":      []string{"agents.v1"},
				"grant_types": []string{"authorization_code"},
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"login.v1": login})
		case "/token":
			if r.Method != http.MethodPost {
				http.Error(w, "method", http.StatusMethodNotAllowed)
				return
			}

			if err := r.ParseForm(); err != nil || r.PostForm.Get("code_verifier") == "" {
				http.Error(w, "missing verifier", http.StatusBadRequest)
				return
			}

			w.WriteHeader(tokenStatus)
			_, _ = io.WriteString(w, tokenBody)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	return srv
}

// browserCallback waits for the authorization URL, then plays the browser by calling the
// loopback redirect URI with the supplied extra query parameters.
func browserCallback(t *testing.T, out *syncBuffer, extra func(state string) url.Values) {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(out.String(), "Open this URL") {
		if time.Now().After(deadline) {
			t.Error("login never printed the authorization URL")
			return
		}

		time.Sleep(10 * time.Millisecond)
	}

	line := strings.TrimSpace(strings.Split(out.String(), "\n")[1])
	authURL, err := url.Parse(line)
	if err != nil {
		t.Errorf("parse authorization URL: %v", err)
		return
	}

	q := authURL.Query()
	redirect, err := url.Parse(q.Get("redirect_uri"))
	if err != nil {
		t.Errorf("parse redirect URI: %v", err)
		return
	}

	params := extra(q.Get("state"))
	redirect.Host = fmt.Sprintf("127.0.0.1:%s", redirect.Port())
	redirect.RawQuery = params.Encode()
	resp, err := http.Get(redirect.String())
	if err != nil {
		t.Errorf("browser callback: %v", err)
		return
	}

	_ = resp.Body.Close()
}

func TestLoginSavesTokenAfterCallback(t *testing.T) {
	isolateHome(t)
	t.Setenv("OPENDEPOT_TOKEN", "")

	srv := loginServer(t, http.StatusOK, `{"access_token":"new-token"}`, []int{0})
	host := strings.TrimPrefix(srv.URL, "http://")

	out := &syncBuffer{}
	a := &app{out: out, httpClient: srv.Client(), scheme: "http", dir: "."}

	done := make(chan error, 1)
	go func() {
		done <- a.login(context.Background(), host)
	}()

	browserCallback(t, out, func(state string) url.Values {
		return url.Values{"code": {"auth-code"}, "state": {state}}
	})

	if err := <-done; err != nil {
		t.Fatalf("login returned error: %v", err)
	}

	if got := loadToken(host); got != "new-token" {
		t.Fatalf("loadToken() = %q, want new-token", got)
	}

	if !strings.Contains(out.String(), "Logged in to "+host) {
		t.Fatalf("login output missing success message: %q", out.String())
	}
}

func TestLoginRejectsStateMismatch(t *testing.T) {
	isolateHome(t)
	t.Setenv("OPENDEPOT_TOKEN", "")

	srv := loginServer(t, http.StatusOK, `{"access_token":"unused"}`, []int{0})
	host := strings.TrimPrefix(srv.URL, "http://")

	out := &syncBuffer{}
	a := &app{out: out, httpClient: srv.Client(), scheme: "http", dir: "."}

	done := make(chan error, 1)
	go func() {
		done <- a.login(context.Background(), host)
	}()

	browserCallback(t, out, func(string) url.Values {
		return url.Values{"code": {"auth-code"}, "state": {"wrong"}}
	})

	if err := <-done; err == nil || !strings.Contains(err.Error(), "state mismatch") {
		t.Fatalf("login error = %v, want state mismatch", err)
	}
}

func TestLoginReportsProviderError(t *testing.T) {
	isolateHome(t)
	t.Setenv("OPENDEPOT_TOKEN", "")

	srv := loginServer(t, http.StatusOK, `{"access_token":"unused"}`, []int{0})
	host := strings.TrimPrefix(srv.URL, "http://")

	out := &syncBuffer{}
	a := &app{out: out, httpClient: srv.Client(), scheme: "http", dir: "."}

	done := make(chan error, 1)
	go func() {
		done <- a.login(context.Background(), host)
	}()

	browserCallback(t, out, func(state string) url.Values {
		return url.Values{"error": {"access_denied"}, "state": {state}}
	})

	if err := <-done; err == nil || !strings.Contains(err.Error(), "access_denied") {
		t.Fatalf("login error = %v, want access_denied", err)
	}
}

func TestLoginHonorsContextCancellation(t *testing.T) {
	isolateHome(t)
	t.Setenv("OPENDEPOT_TOKEN", "")

	srv := loginServer(t, http.StatusOK, `{"access_token":"unused"}`, []int{0})
	host := strings.TrimPrefix(srv.URL, "http://")

	ctx, cancel := context.WithCancel(context.Background())
	out := &syncBuffer{}
	a := &app{out: out, httpClient: srv.Client(), scheme: "http", dir: "."}

	done := make(chan error, 1)
	go func() {
		done <- a.login(ctx, host)
	}()

	for !strings.Contains(out.String(), "Open this URL") {
		time.Sleep(10 * time.Millisecond)
	}

	cancel()
	if err := <-done; err != context.Canceled {
		t.Fatalf("login error = %v, want context.Canceled", err)
	}
}

func TestLoginRejectsInvalidHostAndMissingLoginV1(t *testing.T) {
	isolateHome(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"agents.v1":"/agents/v1/"}`)
	}))
	t.Cleanup(srv.Close)

	a := &app{out: io.Discard, httpClient: srv.Client(), scheme: "http", dir: "."}
	if err := a.login(context.Background(), "https://registry.example.com"); err == nil {
		t.Fatal("login accepted a URL instead of a hostname")
	}

	host := strings.TrimPrefix(srv.URL, "http://")
	if err := a.login(context.Background(), host); err == nil || !strings.Contains(err.Error(), "login.v1") {
		t.Fatalf("login error = %v, want missing login.v1", err)
	}
}

func TestLoginFailsWhenNoPortAvailable(t *testing.T) {
	isolateHome(t)

	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()

	port := busy.Addr().(*net.TCPAddr).Port
	srv := loginServer(t, http.StatusOK, `{"access_token":"unused"}`, []int{port})
	host := strings.TrimPrefix(srv.URL, "http://")

	if err := loginWithDiscard(srv, host); err == nil || !strings.Contains(err.Error(), "no login port available") {
		t.Fatalf("login error = %v, want no login port available", err)
	}
}

func TestLoginFailsOnBadAuthzURL(t *testing.T) {
	isolateHome(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"login.v1":{"client":"c","authz":"%zz","token":"/token","ports":[0]}}`)
	}))
	t.Cleanup(srv.Close)

	host := strings.TrimPrefix(srv.URL, "http://")
	if err := loginWithDiscard(srv, host); err == nil || !strings.Contains(err.Error(), "authz") {
		t.Fatalf("login error = %v, want invalid authz URL", err)
	}
}

// loginWithDiscard runs login against srv with a background context and discards output.
func loginWithDiscard(srv *httptest.Server, host string) error {
	a := &app{out: io.Discard, httpClient: srv.Client(), scheme: "http", dir: "."}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	return a.login(ctx, host)
}

func TestExchangeCodeErrors(t *testing.T) {
	cases := map[string]struct {
		status int
		body   string
		want   string
	}{
		"non-2xx status":     {status: http.StatusUnauthorized, body: `{}`, want: "token exchange failed"},
		"missing token":      {status: http.StatusOK, body: `{"other":"x"}`, want: "no access_token"},
		"malformed response": {status: http.StatusOK, body: `not json`, want: "no access_token"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			t.Cleanup(srv.Close)

			a := &app{httpClient: srv.Client()}
			_, err := a.exchangeCode(context.Background(), srv.URL, url.Values{})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("exchangeCode error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestExchangeCodeTransportError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	srv.Close()

	a := &app{httpClient: srv.Client()}
	if _, err := a.exchangeCode(context.Background(), srv.URL, url.Values{}); err == nil || !strings.Contains(err.Error(), "token exchange") {
		t.Fatalf("exchangeCode error = %v, want transport error", err)
	}
}

func TestExchangeCodeReturnsAccessToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ct := r.Header.Get("Content-Type"); ct != "application/x-www-form-urlencoded" {
			http.Error(w, ct, http.StatusUnsupportedMediaType)
			return
		}

		_, _ = io.WriteString(w, `{"access_token":"abc"}`)
	}))
	t.Cleanup(srv.Close)

	a := &app{httpClient: srv.Client()}
	got, err := a.exchangeCode(context.Background(), srv.URL, url.Values{"code": {"c"}})
	if err != nil || got != "abc" {
		t.Fatalf("exchangeCode = %q, %v; want abc", got, err)
	}
}

func TestSaveTokenPreservesOtherHostsAndRecoversFromCorruptFile(t *testing.T) {
	isolateHome(t)
	t.Setenv("OPENDEPOT_TOKEN", "")

	path, err := opendepotCredentialsPath()
	if err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := saveToken("one.example.com", "token-one"); err != nil {
		t.Fatalf("saveToken over corrupt file: %v", err)
	}

	if err := saveToken("two.example.com", "token-two"); err != nil {
		t.Fatal(err)
	}

	if got := loadToken("one.example.com"); got != "token-one" {
		t.Fatalf("one.example.com token = %q", got)
	}

	if got := loadToken("two.example.com"); got != "token-two" {
		t.Fatalf("two.example.com token = %q", got)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("credentials file permissions = %o, want 600", perm)
	}
}

func TestSaveTokenFailsWhenDirectoryIsAFile(t *testing.T) {
	isolateHome(t)

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(filepath.Join(home, ".config"), 0o700); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(home, ".config", "opendepot"), []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := saveToken("registry.example.com", "token"); err == nil {
		t.Fatal("saveToken succeeded when the credentials directory is a file")
	}
}

func TestLoadTokenPrefersEnvironmentAndHandlesMissingFile(t *testing.T) {
	isolateHome(t)

	t.Setenv("OPENDEPOT_TOKEN", "")
	if got := loadToken("registry.example.com"); got != "" {
		t.Fatalf("loadToken without file = %q, want empty", got)
	}

	t.Setenv("OPENDEPOT_TOKEN", "env-token")
	if got := loadToken("registry.example.com"); got != "env-token" {
		t.Fatalf("loadToken = %q, want env-token", got)
	}
}

func TestAppTokenUsesOverrideWhenSet(t *testing.T) {
	a := &app{tokenFor: func(host string) string { return "override-" + host }}
	if got := a.token("registry.example.com"); got != "override-registry.example.com" {
		t.Fatalf("token() = %q, want override", got)
	}

	isolateHome(t)
	t.Setenv("OPENDEPOT_TOKEN", "from-env")

	b := &app{}
	if got := b.token("registry.example.com"); got != "from-env" {
		t.Fatalf("token() without override = %q, want from-env", got)
	}
}

func TestRandomStringLengthAndEncoding(t *testing.T) {
	first, err := randomString(24)
	if err != nil {
		t.Fatal(err)
	}

	second, err := randomString(24)
	if err != nil {
		t.Fatal(err)
	}

	if first == second {
		t.Fatal("randomString returned repeated values")
	}

	if strings.ContainsAny(first, "+/=") {
		t.Fatalf("randomString %q is not raw URL encoded", first)
	}
}
