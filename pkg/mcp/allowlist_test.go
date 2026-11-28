package mcp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"sync/atomic"
	"testing"
)

func TestNewAllowListRejectsMalformedEntries(t *testing.T) {
	entries := []string{"", "https://registry.npmjs.org", "registry.npmjs.org/path", "*.example.com", "a@b.com"}
	for _, entry := range entries {
		if _, err := NewAllowList([]string{entry}); !errors.Is(err, ErrDenied) {
			t.Errorf("NewAllowList(%q) error = %v, want ErrDenied", entry, err)
		}
	}
}

func TestAllowListDecisions(t *testing.T) {
	al, err := NewAllowList([]string{testAllowedHost, "pypi.example.com:8443"})
	if err != nil {
		t.Fatalf("NewAllowList() error = %v", err)
	}

	tests := []struct {
		name    string
		rawURL  string
		plain   bool
		allowed bool
	}{
		{name: "allowed host https", rawURL: "https://registry.npmjs.org/pkg", allowed: true},
		{name: "allowed host any port", rawURL: "https://registry.npmjs.org:444/pkg", allowed: true},
		{name: "host with port entry, right port", rawURL: "https://pypi.example.com:8443/simple", allowed: true},
		{name: "host with port entry, wrong port", rawURL: "https://pypi.example.com/simple", allowed: false},
		{name: "unlisted host", rawURL: testUnlistedHost, allowed: false},
		{name: "suffix lookalike is not a match", rawURL: "https://registry.npmjs.org.evil.net/pkg", allowed: false},
		{name: "http refused in production", rawURL: "http://registry.npmjs.org/pkg", allowed: false},
		{name: "http allowed only in tests", rawURL: "http://registry.npmjs.org/pkg", plain: true, allowed: true},
		{name: "userinfo refused", rawURL: "https://user:pw@registry.npmjs.org/pkg", allowed: false},
		{name: "file scheme refused", rawURL: "file:///etc/passwd", allowed: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			u, err := url.Parse(tc.rawURL)
			if err != nil {
				t.Fatalf("parse %q: %v", tc.rawURL, err)
			}

			err = checkURL(al, u, tc.plain)
			if tc.allowed && err != nil {
				t.Errorf("checkURL(%s) denied: %v", tc.rawURL, err)
			}

			if !tc.allowed && !errors.Is(err, ErrDenied) {
				t.Errorf("checkURL(%s) error = %v, want ErrDenied", tc.rawURL, err)
			}
		})
	}
}

func TestEmptyAllowListDeniesEverything(t *testing.T) {
	empty, err := NewAllowList(nil)
	if err != nil {
		t.Fatalf("NewAllowList(nil) error = %v", err)
	}

	for _, al := range []*AllowList{nil, empty} {
		u, _ := url.Parse(testUnlistedHost)
		if err := checkURL(al, u, false); !errors.Is(err, ErrDenied) {
			t.Errorf("empty allow-list allowed %s: %v", u, err)
		}
	}
}

func TestAllowListCheckURL(t *testing.T) {
	al, err := NewAllowList([]string{testAllowedHost})
	if err != nil {
		t.Fatalf("NewAllowList() error = %v", err)
	}

	if err := al.CheckURL("https://registry.npmjs.org/pkg/-/pkg-1.0.0.tgz"); err != nil {
		t.Fatalf("allow-listed https URL denied: %v", err)
	}

	for _, rawURL := range []string{"http://registry.npmjs.org/pkg", testUnlistedHost, "https://registry.npmjs.org.evil.net/x", "://bad", ""} {
		if err := al.CheckURL(rawURL); !errors.Is(err, ErrDenied) {
			t.Errorf("CheckURL(%q) error = %v, want ErrDenied", rawURL, err)
		}
	}
}

func TestCheckDialAddrBlocksUnsafeAddresses(t *testing.T) {
	unsafe := []string{
		"0.0.0.0",
		"127.0.0.1",
		"::1",
		"224.0.0.1",
		"169.254.169.254",
		"fe80::1",
		"10.0.0.5",
		"172.16.4.1",
		"192.168.1.10",
		"100.64.0.1",
		"fc00::1",
		"::ffff:10.1.2.3",
		"::ffff:169.254.169.254",
	}

	for _, raw := range unsafe {
		if err := checkDialAddr(netip.MustParseAddr(raw)); err == nil {
			t.Errorf("checkDialAddr(%s) allowed an unsafe address", raw)
		}
	}

	for _, raw := range []string{"93.184.216.34", "2606:4700:4700::1111"} {
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
		{prefix: "64:ff9b:1::/48", addr: "64:ff9b:1::1"},
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

func TestGuardedDialerRefusesLoopbackInProduction(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	dial := guardedDialer(checkDialAddr)
	conn, err := dial(context.Background(), "tcp", server.Listener.Addr().String())
	if err == nil {
		_ = conn.Close()
		t.Fatal("guardedDialer dialed a loopback address with the production policy")
	}

	if !errors.Is(err, ErrDenied) {
		t.Errorf("guardedDialer error = %v, want ErrDenied", err)
	}
}

func TestGuardedDialerRejectsMalformedAddress(t *testing.T) {
	dial := guardedDialer(checkDialAddr)
	if _, err := dial(context.Background(), "tcp", "no-port"); !errors.Is(err, ErrDenied) {
		t.Errorf("guardedDialer(no-port) error = %v, want ErrDenied", err)
	}
}

func TestClientDeniesRequestsOutsideAllowList(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	al, err := NewAllowList([]string{testAllowedHost})
	if err != nil {
		t.Fatalf("NewAllowList() error = %v", err)
	}

	client := newClient(al, allowLoopbackOnly, true)
	if _, err := client.ResolveNPM(context.Background(), server.URL, testNPMTool, ""); !errors.Is(err, ErrDenied) {
		t.Errorf("ResolveNPM(unlisted registry) error = %v, want ErrDenied", err)
	}

	if hits.Load() != 0 {
		t.Errorf("unlisted registry received %d request(s); the check must run before the request", hits.Load())
	}
}

func TestClientRefusesRedirectToUnlistedHost(t *testing.T) {
	redirected := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer redirected.Close()

	registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, redirected.URL+"/"+testNPMTool, http.StatusFound)
	}))
	defer registry.Close()

	al, err := NewAllowList([]string{hostPort(t, registry.URL)})
	if err != nil {
		t.Fatalf("NewAllowList() error = %v", err)
	}

	client := newClient(al, allowLoopbackOnly, true)
	if _, err := client.NPMVersions(context.Background(), registry.URL, testNPMTool); err == nil {
		t.Fatal("NPMVersions() followed a redirect to an unlisted host")
	}
}

func TestProductionClientBlocksPrivateDial(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	al, err := NewAllowList([]string{hostPort(t, server.URL)})
	if err != nil {
		t.Fatalf("NewAllowList() error = %v", err)
	}

	client := NewClient(al)
	if _, err := client.NPMVersions(context.Background(), server.URL, testNPMTool); !errors.Is(err, ErrDenied) {
		t.Errorf("production client error = %v, want the loopback dial to be refused with ErrDenied", err)
	}
}
