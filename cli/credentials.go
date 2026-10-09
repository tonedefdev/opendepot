package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type credentialsFile struct {
	Credentials map[string]struct {
		Token string `json:"token"`
	} `json:"credentials"`
}

func opendepotCredentialsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(home, ".config", "opendepot", "credentials.json"), nil
}

// token returns the bearer token for host. OPENDEPOT_TOKEN wins, then the OpenDepot credentials
// file. The token is never printed.
func (a *app) token(host string) string {
	if a.tokenFor != nil {
		return a.tokenFor(host)
	}

	return loadToken(host)
}

func loadToken(host string) string {
	if t := os.Getenv("OPENDEPOT_TOKEN"); t != "" {
		return t
	}

	p, err := opendepotCredentialsPath()
	if err != nil {
		return ""
	}

	content, err := os.ReadFile(p)
	if err != nil {
		return ""
	}

	var creds credentialsFile
	if err := json.Unmarshal(content, &creds); err != nil {
		return ""
	}

	return creds.Credentials[host].Token
}

// saveToken stores the token for host in the OpenDepot credentials file with 0600 permissions.
func saveToken(host, token string) error {
	path, err := opendepotCredentialsPath()
	if err != nil {
		return err
	}

	creds := credentialsFile{Credentials: map[string]struct {
		Token string `json:"token"`
	}{}}
	if content, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(content, &creds)
	}

	if creds.Credentials == nil {
		creds.Credentials = map[string]struct {
			Token string `json:"token"`
		}{}
	}

	creds.Credentials[host] = struct {
		Token string `json:"token"`
	}{Token: token}
	content, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".credentials-*")
	if err != nil {
		return err
	}

	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}

	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		return err
	}

	if err := tmp.Close(); err != nil {
		return err
	}

	return os.Rename(tmp.Name(), path)
}

func randomString(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}

	return base64.RawURLEncoding.EncodeToString(b), nil
}

// login runs the login.v1 authorization code flow with PKCE over a loopback listener.
// validateHost requires a bare hostname with an optional port, so a scheme or path is never
// passed through to the discovery URL.
func validateHost(host string) error {
	u, err := url.Parse("//" + host)
	if err != nil || u.Host != host || u.Hostname() == "" || u.Path != "" || u.User != nil {
		return fmt.Errorf("login requires a registry hostname such as registry.example.com, got %q", host)
	}

	return nil
}

func (a *app) login(ctx context.Context, host string) error {
	if err := validateHost(host); err != nil {
		return err
	}

	d, err := a.discover(ctx, host)
	if err != nil {
		return err
	}

	if d.LoginV1 == nil {
		return fmt.Errorf("registry %s does not advertise login.v1", host)
	}

	root, err := url.Parse(a.rootURL(host, "/"))
	if err != nil {
		return err
	}

	authz, err := url.Parse(d.LoginV1.Authz)
	if err != nil {
		return fmt.Errorf("invalid login.v1 authz URL: %w", err)
	}

	tokenURL, err := url.Parse(d.LoginV1.Token)
	if err != nil {
		return fmt.Errorf("invalid login.v1 token URL: %w", err)
	}

	var listener net.Listener
	for _, p := range d.LoginV1.Ports {
		listener, err = net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p))
		if err == nil {
			break
		}
	}

	if listener == nil {
		return fmt.Errorf("no login port available from %v", d.LoginV1.Ports)
	}

	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	redirectURI := fmt.Sprintf("http://localhost:%d/login", port)

	state, err := randomString(24)
	if err != nil {
		return err
	}

	verifier, err := randomString(48)
	if err != nil {
		return err
	}

	challenge := sha256.Sum256([]byte(verifier))
	query := url.Values{}
	query.Set("client_id", d.LoginV1.Client)
	query.Set("redirect_uri", redirectURI)
	query.Set("response_type", "code")
	query.Set("scope", strings.Join(d.LoginV1.Scopes, " "))
	query.Set("state", state)
	query.Set("code_challenge", base64.RawURLEncoding.EncodeToString(challenge[:]))
	query.Set("code_challenge_method", "S256")
	authURL := root.ResolveReference(authz)
	authURL.RawQuery = query.Encode()

	fmt.Fprintf(a.out, "Open this URL in a browser to log in to %s:\n%s\n", host, authURL.String())

	type callback struct {
		code string
		err  error
	}

	results := make(chan callback, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("state") != state {
			http.Error(w, "invalid state", http.StatusBadRequest)
			results <- callback{err: errors.New("login state mismatch")}
			return
		}

		if msg := q.Get("error"); msg != "" {
			http.Error(w, "login failed", http.StatusBadRequest)
			results <- callback{err: fmt.Errorf("login failed: %s", msg)}
			return
		}

		fmt.Fprintln(w, "Login complete. You may close this window.")
		results <- callback{code: q.Get("code")}
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go server.Serve(listener)
	defer server.Shutdown(context.Background())

	var res callback
	select {
	case res = <-results:
	case <-ctx.Done():
		return ctx.Err()
	}

	if res.err != nil {
		return res.err
	}

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", d.LoginV1.Client)
	form.Set("code", res.code)
	form.Set("redirect_uri", redirectURI)
	form.Set("code_verifier", verifier)
	token, err := a.exchangeCode(ctx, root.ResolveReference(tokenURL).String(), form)
	if err != nil {
		return err
	}

	if err := saveToken(host, token); err != nil {
		return fmt.Errorf("save credentials: %w", err)
	}

	fmt.Fprintf(a.out, "Logged in to %s. Token saved to the OpenDepot credentials file.\n", host)
	return nil
}

func (a *app) exchangeCode(ctx context.Context, tokenURL string, form url.Values) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("token exchange: %w", err)
	}

	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", fmt.Errorf("token exchange failed: %s", resp.Status)
	}

	var out struct {
		AccessToken string `json:"access_token"`
	}

	if err := json.Unmarshal(bytes.TrimSpace(body), &out); err != nil || out.AccessToken == "" {
		return "", errors.New("token exchange returned no access_token")
	}

	return out.AccessToken, nil
}
