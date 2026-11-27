package oci

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"time"
)

const maxRedirects = 10

// cgnatPrefix is the shared address space (RFC 6598). Some clouds route instance metadata and internal control
// planes through it, so it is treated as internal.
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
	netip.MustParsePrefix("64:ff9b:1::/48"),
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

// dialFunc matches the signature of http.Transport.DialContext.
type dialFunc func(ctx context.Context, network, address string) (net.Conn, error)

// guardedDialer resolves the target host, rejects the connection if any resolved address fails check, and then dials
// a validated IP literal so the address that was checked is the address that is used (no DNS rebinding window).
func guardedDialer(check func(netip.Addr) error) dialFunc {
	d := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrUpstreamDenied, err)
		}

		addrs, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			return nil, fmt.Errorf("oci: resolve upstream host: %w", err)
		}

		if len(addrs) == 0 {
			return nil, fmt.Errorf("oci: upstream host resolved to no addresses")
		}

		for _, addr := range addrs {
			if err := check(addr); err != nil {
				return nil, fmt.Errorf("%w: dial to %s refused: %w", ErrUpstreamDenied, host, err)
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

// redirectPolicy returns a CheckRedirect that applies the upstream policy to every redirect target.
func redirectPolicy(al *AllowList, plainHTTP bool) func(*http.Request, []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if len(via) >= maxRedirects {
			return fmt.Errorf("oci: stopped after %d redirects", maxRedirects)
		}

		return checkURL(al, req.URL, plainHTTP)
	}
}

// allowListTransport applies the upstream policy to every request, including token-service requests that oras
// makes to a realm host.
type allowListTransport struct {
	allow     *AllowList
	plainHTTP bool
	next      http.RoundTripper
}

// RoundTrip rejects the request before it is sent when its URL is not permitted.
func (t allowListTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := checkURL(t.allow, req.URL, t.plainHTTP); err != nil {
		return nil, err
	}

	return t.next.RoundTrip(req)
}

// newUpstreamHTTPClient builds the http.Client used for all upstream traffic. Proxies are not used, so every
// connection goes through guardedDialer.
func newUpstreamHTTPClient(al *AllowList, check func(netip.Addr) error, plainHTTP bool) *http.Client {
	transport := &http.Transport{
		Proxy:                 nil,
		DialContext:           guardedDialer(check),
		ForceAttemptHTTP2:     true,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 60 * time.Second,
		IdleConnTimeout:       90 * time.Second,
	}

	return &http.Client{
		Transport:     allowListTransport{allow: al, plainHTTP: plainHTTP, next: transport},
		CheckRedirect: redirectPolicy(al, plainHTTP),
	}
}
