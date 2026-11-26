package mcp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

// ErrDenied is returned when a registry request targets a host, scheme, or address that policy does not permit.
var ErrDenied = errors.New("mcp: registry request denied")

const maxRedirects = 10

// AllowList is the set of registry hosts that OpenDepot may contact. Entries are bare hosts or host:port. An entry
// without a port matches any port. A nil or empty AllowList denies every host.
type AllowList struct {
	exact map[string][]string
}

// NewAllowList parses entries into an AllowList. It fails on an entry that is not a bare host or host:port, so a typo
// cannot silently widen or narrow the policy.
func NewAllowList(entries []string) (*AllowList, error) {
	al := &AllowList{exact: map[string][]string{}}
	for _, raw := range entries {
		entry := strings.ToLower(strings.TrimSpace(raw))
		if entry == "" || strings.ContainsAny(entry, "/@*") || strings.Contains(entry, "://") {
			return nil, fmt.Errorf("%w: allow-list entry %q must be a host or host:port", ErrDenied, raw)
		}

		parsed, err := url.Parse("https://" + entry)
		if err != nil || parsed.Hostname() == "" {
			return nil, fmt.Errorf("%w: invalid allow-list entry %q", ErrDenied, raw)
		}

		al.exact[parsed.Hostname()] = append(al.exact[parsed.Hostname()], parsed.Port())
	}

	return al, nil
}

// allows reports whether the host and port of u are on the list.
func (al *AllowList) allows(u *url.URL) bool {
	if al == nil {
		return false
	}

	for _, wantPort := range al.exact[strings.ToLower(u.Hostname())] {
		if wantPort == "" || wantPort == u.Port() {
			return true
		}
	}

	return false
}

// checkURL enforces the registry policy on one URL. The scheme must be https (or http when the test-only plainHTTP
// flag is set), the URL must not carry userinfo, and its host must be on the allow-list.
func checkURL(al *AllowList, u *url.URL, plainHTTP bool) error {
	switch {
	case u.Scheme == "https":
	case u.Scheme == "http" && plainHTTP:
	default:
		return fmt.Errorf("%w: scheme %q is not permitted", ErrDenied, u.Scheme)
	}

	if u.User != nil {
		return fmt.Errorf("%w: URL userinfo is not permitted", ErrDenied)
	}

	if !al.allows(u) {
		return fmt.Errorf("%w: host %q is not on the allow-list", ErrDenied, u.Hostname())
	}

	return nil
}

// cgnatPrefix is the shared address space (RFC 6598). Some clouds route instance metadata through it.
var cgnatPrefix = netip.MustParsePrefix("100.64.0.0/10")

// specialUsePrefixes are non-public and special-use ranges that the netip predicates do not cover. They are denied
// explicitly so the policy does not depend on the stdlib classification of these blocks.
var specialUsePrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("fec0::/10"),
}

// checkDialAddr is the production IP policy. It fails closed for every address that is not plainly public unicast.
func checkDialAddr(addr netip.Addr) error {
	addr = addr.Unmap()
	switch {
	case !addr.IsValid():
		return errors.New("invalid address")
	case addr.IsUnspecified():
		return errors.New("unspecified address")
	case addr.IsLoopback():
		return errors.New("loopback address")
	case addr.IsMulticast():
		return errors.New("multicast address")
	case addr.IsLinkLocalUnicast():
		return errors.New("link-local address (includes cloud metadata 169.254.169.254)")
	case addr.IsPrivate():
		return errors.New("private address")
	case cgnatPrefix.Contains(addr):
		return errors.New("shared address space")
	case inSpecialUse(addr):
		return errors.New("special-use address")
	}

	return nil
}

func inSpecialUse(addr netip.Addr) bool {
	for _, p := range specialUsePrefixes {
		if p.Contains(addr) {
			return true
		}
	}

	return false
}

// allowLoopbackOnly is a test-only IP policy. It admits loopback addresses so httptest servers can be reached, and it
// still rejects every other unsafe address class.
func allowLoopbackOnly(addr netip.Addr) error {
	if addr.Unmap().IsLoopback() {
		return nil
	}

	return checkDialAddr(addr)
}

// dialFunc matches the signature of http.Transport.DialContext.
type dialFunc func(ctx context.Context, network, address string) (net.Conn, error)

// guardedDialer resolves the target host, rejects the connection if any resolved address fails check, and then dials
// a validated IP literal, so the address that was checked is the address that is used (no DNS rebinding window).
func guardedDialer(check func(netip.Addr) error) dialFunc {
	d := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrDenied, err)
		}

		addrs, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			return nil, fmt.Errorf("mcp: resolve registry host: %w", err)
		}

		if len(addrs) == 0 {
			return nil, errors.New("mcp: registry host resolved to no addresses")
		}

		for _, addr := range addrs {
			if err := check(addr); err != nil {
				return nil, fmt.Errorf("%w: dial to %s refused: %w", ErrDenied, host, err)
			}
		}

		var lastErr error
		for _, addr := range addrs {
			conn, err := d.DialContext(ctx, network, net.JoinHostPort(addr.Unmap().String(), port))
			if err == nil {
				return conn, nil
			}

			lastErr = err
		}

		return nil, lastErr
	}
}

// redirectPolicy returns a CheckRedirect that applies the registry policy to every redirect target.
func redirectPolicy(al *AllowList, plainHTTP bool) func(*http.Request, []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if len(via) >= maxRedirects {
			return fmt.Errorf("mcp: stopped after %d redirects", maxRedirects)
		}

		return checkURL(al, req.URL, plainHTTP)
	}
}

// newHTTPClient builds the http.Client used for all registry traffic. Proxies are not used, so every connection goes
// through guardedDialer.
func newHTTPClient(al *AllowList, check func(netip.Addr) error, plainHTTP bool) *http.Client {
	transport := &http.Transport{
		Proxy:                 nil,
		DialContext:           guardedDialer(check),
		ForceAttemptHTTP2:     true,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 60 * time.Second,
		IdleConnTimeout:       90 * time.Second,
	}

	return &http.Client{
		Transport:     transport,
		CheckRedirect: redirectPolicy(al, plainHTTP),
		Timeout:       2 * time.Minute,
	}
}
