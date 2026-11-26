package oci

import (
	"context"
	"encoding/base64"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"testing"

	"oras.land/oras-go/v2/registry/remote/auth"
)

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()

	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}

	return u
}

func TestEmptyAllowListDeniesEverything(t *testing.T) {
	al, err := NewAllowList(nil)
	if err != nil {
		t.Fatalf("NewAllowList(nil): %v", err)
	}

	for _, raw := range []string{"https://ghcr.io/v2/", "https://registry.example.com/v2/"} {
		if err := checkURL(al, mustURL(t, raw), false); !errors.Is(err, ErrUpstreamDenied) {
			t.Errorf("empty allow-list permitted %s: %v", raw, err)
		}
	}

	var nilList *AllowList
	if err := checkURL(nilList, mustURL(t, "https://ghcr.io/"), false); !errors.Is(err, ErrUpstreamDenied) {
		t.Errorf("nil allow-list permitted a request: %v", err)
	}
}

func TestAllowListMatching(t *testing.T) {
	al, err := NewAllowList([]string{testGHCR, "registry.example.com:5000", "*.azurecr.io"})
	if err != nil {
		t.Fatalf("NewAllowList: %v", err)
	}

	cases := []struct {
		name string
		url  string
		want bool
	}{
		{"exact host", "https://ghcr.io/v2/", true},
		{"exact host is case insensitive", "https://GHCR.io/v2/", true},
		{"host with port entry matches its port", "https://registry.example.com:5000/v2/", true},
		{"host with port entry rejects another port", "https://registry.example.com:6000/v2/", false},
		{"host with port entry rejects default port", "https://registry.example.com/v2/", false},
		{"wildcard matches subdomain", "https://myreg.azurecr.io/v2/", true},
		{"wildcard does not match apex", "https://azurecr.io/v2/", false},
		{"wildcard does not match suffix collision", "https://evilazurecr.io/v2/", false},
		{"unlisted host", "https://quay.io/v2/", false},
		{"suffix of listed host", "https://notghcr.io/v2/", false},
		{"subdomain of exact host is not allowed", "https://sub.ghcr.io/v2/", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := checkURL(al, mustURL(t, tc.url), false)
			if got := err == nil; got != tc.want {
				t.Fatalf("checkURL(%s) allowed=%v, want %v (err=%v)", tc.url, got, tc.want, err)
			}

			if err != nil && !errors.Is(err, ErrUpstreamDenied) {
				t.Fatalf("denial must wrap ErrUpstreamDenied, got %v", err)
			}
		})
	}
}

func TestNewAllowListRejectsMalformedEntries(t *testing.T) {
	bad := []string{
		"",
		"  ",
		"https://ghcr.io",
		"ghcr.io/path",
		"user@ghcr.io",
		"*",
		"*.io",
		"*ghcr.io",
		"*.example.com/evil",
		"ghcr.io*",
	}
	for _, entry := range bad {
		if _, err := NewAllowList([]string{entry}); err == nil {
			t.Errorf("NewAllowList(%q) accepted a malformed entry", entry)
		}
	}
}

func TestAllowListKeepsEveryPortForAHost(t *testing.T) {
	al, err := NewAllowList([]string{"127.0.0.1:5000", "127.0.0.1:5001"})
	if err != nil {
		t.Fatal(err)
	}

	for _, raw := range []string{testLoopback, "http://127.0.0.1:5001/"} {
		if err := checkURL(al, mustURL(t, raw), true); err != nil {
			t.Errorf("%s should be allowed: %v", raw, err)
		}
	}

	if err := checkURL(al, mustURL(t, "http://127.0.0.1:5002/"), true); err == nil {
		t.Error("unlisted port on an allow-listed host must be denied")
	}
}

func TestCheckURLSchemeAndUserinfo(t *testing.T) {
	al, err := NewAllowList([]string{testGHCR, "127.0.0.1:5000"})
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		url        string
		plainHTTP  bool
		wantDenied bool
	}{
		{"https://ghcr.io/", false, false},
		{"http://ghcr.io/", false, true},
		{"http://ghcr.io/", true, false},
		{"file:///etc/passwd", true, true},
		{"ftp://ghcr.io/", false, true},
		{"https://user:pass@ghcr.io/", false, true},
		{testLoopback, false, true},
		{testLoopback, true, false},
	}
	for _, tc := range cases {
		err := checkURL(al, mustURL(t, tc.url), tc.plainHTTP)
		if denied := err != nil; denied != tc.wantDenied {
			t.Errorf("checkURL(%s, plainHTTP=%v) denied=%v, want %v (err=%v)", tc.url, tc.plainHTTP, denied, tc.wantDenied, err)
		}
	}
}

func TestCheckDialAddrBlocksUnsafeAddresses(t *testing.T) {
	blocked := []string{
		"127.0.0.1",
		"127.255.255.254",
		"::1",
		"::ffff:127.0.0.1",
		"10.0.0.1",
		"172.16.0.1",
		"172.31.255.255",
		"192.168.1.1",
		"169.254.169.254",
		"169.254.0.1",
		"fe80::1",
		"fc00::1",
		"fd00:ec2::254",
		"0.0.0.0",
		"::",
		"224.0.0.1",
		"ff02::1",
		"100.64.0.1",
		"::ffff:10.0.0.1",
	}
	for _, raw := range blocked {
		if err := checkDialAddr(netip.MustParseAddr(raw)); err == nil {
			t.Errorf("checkDialAddr(%s) allowed an unsafe address", raw)
		}
	}

	allowed := []string{"8.8.8.8", "1.1.1.1", "2606:4700:4700::1111", "172.15.255.255", "172.32.0.1"}
	for _, raw := range allowed {
		if err := checkDialAddr(netip.MustParseAddr(raw)); err != nil {
			t.Errorf("checkDialAddr(%s) blocked a public address: %v", raw, err)
		}
	}

	if err := checkDialAddr(netip.Addr{}); err == nil {
		t.Error("checkDialAddr(invalid) allowed the zero address")
	}
}

func TestCheckDialAddrDeniesSpecialUsePrefixes(t *testing.T) {
	tests := []struct {
		prefix string
		addr   string
	}{
		{prefix: "0.0.0.0/8", addr: "0.1.2.3"},
		{prefix: "192.0.0.0/24", addr: "192.0.0.8"},
		{prefix: "192.0.2.0/24", addr: "192.0.2.10"},
		{prefix: "198.18.0.0/15", addr: "198.19.255.254"},
		{prefix: "198.51.100.0/24", addr: "198.51.100.7"},
		{prefix: "203.0.113.0/24", addr: "203.0.113.200"},
		{prefix: "240.0.0.0/4", addr: "240.0.0.1"},
		{prefix: "64:ff9b::/96", addr: "64:ff9b::808:808"},
		{prefix: "2002::/16", addr: "2002:0102:0304::1"},
		{prefix: "fec0::/10", addr: "feff::1"},
	}

	for _, tc := range tests {
		addr := netip.MustParseAddr(tc.addr)
		if !netip.MustParsePrefix(tc.prefix).Contains(addr) {
			t.Fatalf("test address %s is not inside %s", tc.addr, tc.prefix)
		}

		if err := checkDialAddr(addr); err == nil {
			t.Errorf("checkDialAddr(%s) allowed %s", tc.addr, tc.prefix)
		}
	}
}

func TestAllowLoopbackOnlyKeepsOtherBlocks(t *testing.T) {
	if err := allowLoopbackOnly(netip.MustParseAddr("127.0.0.1")); err != nil {
		t.Errorf("test policy must admit loopback: %v", err)
	}

	for _, raw := range []string{"10.0.0.1", "169.254.169.254", "0.0.0.0"} {
		if err := allowLoopbackOnly(netip.MustParseAddr(raw)); err == nil {
			t.Errorf("test policy admitted %s", raw)
		}
	}
}

func TestGuardedDialerRefusesBlockedTargets(t *testing.T) {
	dial := guardedDialer(checkDialAddr)
	ctx := context.Background()

	for _, address := range []string{"127.0.0.1:443", "169.254.169.254:80", "[::1]:443", "localhost:443", "10.1.2.3:443"} {
		conn, err := dial(ctx, "tcp", address)
		if conn != nil {
			_ = conn.Close()
			t.Errorf("dial %s: expected ErrUpstreamDenied, got %v", address, err)
		}
	}

	if _, err := dial(ctx, "tcp", "no-port-here"); err == nil {
		t.Error("dial without a port must fail")
	}
}

func TestGuardedDialerAdmitsLoopbackOnlyForTests(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()

	go func() {
		c, err := l.Accept()
		if err == nil {
			_ = c.Close()
		}
	}()

	dial := guardedDialer(allowLoopbackOnly)
	conn, err := dial(context.Background(), "tcp", l.Addr().String())
	if err != nil {
		t.Fatalf("test policy should dial loopback: %v", err)
	}
	_ = conn.Close()
}

func TestProductionUpstreamDialRefusesLoopback(t *testing.T) {
	up, err := NewUpstream([]string{"127.0.0.1:9"}, nil)
	if err != nil {
		t.Fatalf("NewUpstream: %v", err)
	}

	transport, ok := up.client.Transport.(allowListTransport)
	if !ok {
		t.Fatalf("upstream transport is %T, want allowListTransport", up.client.Transport)
	}

	inner, ok := transport.next.(*http.Transport)
	if !ok {
		t.Fatalf("inner transport is %T, want *http.Transport", transport.next)
	}

	if inner.Proxy != nil {
		t.Fatal("upstream transport must not use a proxy")
	}

	conn, err := inner.DialContext(context.Background(), "tcp", "127.0.0.1:9")
	if conn != nil {
		_ = conn.Close()
	}

	if !errors.Is(err, ErrUpstreamDenied) {
		t.Fatalf("production upstream dialed loopback: err=%v", err)
	}
}

func TestRedirectPolicyEnforcesAllowListAndLimit(t *testing.T) {
	al, err := NewAllowList([]string{testGHCR})
	if err != nil {
		t.Fatal(err)
	}

	policy := redirectPolicy(al, false)
	req := &http.Request{URL: mustURL(t, "https://ghcr.io/blob")}
	if err := policy(req, nil); err != nil {
		t.Fatalf("allowed redirect target rejected: %v", err)
	}

	targets := []string{"https://169.254.169.254/latest/meta-data", "https://evil.example/x", "http://ghcr.io/x"}
	for _, raw := range targets {
		req := &http.Request{URL: mustURL(t, raw)}
		if err := policy(req, []*http.Request{{}}); !errors.Is(err, ErrUpstreamDenied) {
			t.Errorf("redirect to %s: expected ErrUpstreamDenied, got %v", raw, err)
		}
	}

	via := make([]*http.Request, maxRedirects)
	if err := policy(&http.Request{URL: mustURL(t, "https://ghcr.io/x")}, via); err == nil {
		t.Error("redirect chain at the limit must be stopped")
	}
}

func TestAllowListTransportBlocksBeforeSending(t *testing.T) {
	al, err := NewAllowList([]string{testGHCR})
	if err != nil {
		t.Fatal(err)
	}

	sentinel := roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("request reached the network for a disallowed host")
		return nil, nil
	})
	rt := allowListTransport{allow: al, next: sentinel}

	req, err := http.NewRequest(http.MethodGet, "https://auth.example.com/token", nil)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := rt.RoundTrip(req); !errors.Is(err, ErrUpstreamDenied) {
		t.Fatalf("expected ErrUpstreamDenied, got %v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func dockerConfig(t *testing.T, entries string) []byte {
	t.Helper()

	return []byte(`{"auths":{` + entries + `}}`)
}

func TestParseDockerConfigJSON(t *testing.T) {
	basic := base64.StdEncoding.EncodeToString([]byte("robot:s3cret:with:colons"))
	raw := dockerConfig(t, strings.Join([]string{
		`"ghcr.io":{"auth":"` + basic + `"}`,
		`"https://registry.example.com/v1/":{"username":"alice","password":"hunter2"}`,
		`"https://index.docker.io/v1/":{"identitytoken":"idtoken-123"}`,
		`"quay.io":{}`,
	}, ","))

	cred, err := parseDockerConfigJSON(raw)
	if err != nil {
		t.Fatalf("parseDockerConfigJSON: %v", err)
	}

	ctx := context.Background()
	cases := []struct {
		host string
		want auth.Credential
	}{
		{testGHCR, auth.Credential{Username: testUser, Password: "s3cret:with:colons"}},
		{"registry.example.com", auth.Credential{Username: "alice", Password: "hunter2"}},
		{"registry-1.docker.io", auth.Credential{RefreshToken: "idtoken-123"}},
		{"quay.io", auth.EmptyCredential},
		{"unlisted.example.com", auth.EmptyCredential},
	}
	for _, tc := range cases {
		got, err := cred(ctx, tc.host)
		if err != nil {
			t.Fatalf("credential(%s): %v", tc.host, err)
		}

		if got != tc.want {
			t.Errorf("credential(%s) = %+v, want %+v", tc.host, got, tc.want)
		}
	}
}

func TestParseDockerConfigJSONEmptyIsAnonymous(t *testing.T) {
	cred, err := parseDockerConfigJSON(nil)
	if err != nil {
		t.Fatalf("parseDockerConfigJSON(nil): %v", err)
	}

	got, err := cred(context.Background(), testGHCR)
	if err != nil || got != auth.EmptyCredential {
		t.Fatalf("empty config should be anonymous, got %+v, %v", got, err)
	}
}

func TestParseDockerConfigJSONErrorsDoNotLeakSecrets(t *testing.T) {
	secret := "hunter2-do-not-print"

	bad := map[string][]byte{
		"invalid json":   []byte(`{"auths": {"ghcr.io": ` + secret),
		"bad base64":     dockerConfig(t, `"ghcr.io":{"auth":"!!`+secret+`"}`),
		"no colon":       dockerConfig(t, `"ghcr.io":{"auth":"`+base64.StdEncoding.EncodeToString([]byte(secret))+`"}`),
		"empty registry": dockerConfig(t, `"https://":{"username":"u","password":"`+secret+`"}`),
	}
	for name, raw := range bad {
		_, err := parseDockerConfigJSON(raw)
		if err == nil {
			t.Errorf("%s: expected an error", name)
			continue
		}

		if strings.Contains(err.Error(), secret) {
			t.Errorf("%s: error leaks credential material: %v", name, err)
		}
	}
}

func TestUpstreamRejectsTagInRepositoryAndUnlistedHost(t *testing.T) {
	up, err := NewUpstream([]string{testGHCR}, nil)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	if _, err := up.ListTags(ctx, "ghcr.io/org/chart:1.0.0"); err == nil {
		t.Error("repository with a tag must be rejected")
	}

	if _, err := up.ListTags(ctx, "quay.io/org/chart"); !errors.Is(err, ErrUpstreamDenied) {
		t.Errorf("unlisted registry: expected ErrUpstreamDenied, got %v", err)
	}
}
