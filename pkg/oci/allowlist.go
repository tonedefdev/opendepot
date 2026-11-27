package oci

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// ErrUpstreamDenied is returned when a request targets a host, scheme, or credential placement that the upstream
// policy does not permit.
var ErrUpstreamDenied = errors.New("upstream request denied")

// AllowList is the set of upstream registry hosts that OpenDepot may contact. Entries are either a host, optionally
// with a port (ghcr.io, registry.example.com:5000), or a wildcard subdomain (*.azurecr.io). An entry without a port
// matches any port. A nil or empty AllowList denies every host.
type AllowList struct {
	exact     map[string][]string
	wildcards []string
}

// NewAllowList parses entries into an AllowList. It fails on an entry that is not a bare host, host:port, or
// *.host wildcard, so a typo cannot silently widen or narrow the policy.
func NewAllowList(entries []string) (*AllowList, error) {
	al := &AllowList{exact: map[string][]string{}}
	for _, raw := range entries {
		entry := strings.ToLower(strings.TrimSpace(raw))
		if entry == "" {
			return nil, fmt.Errorf("%w: empty allow-list entry", ErrUpstreamDenied)
		}

		if strings.HasPrefix(entry, "*.") {
			suffix := strings.TrimPrefix(entry, "*")
			if strings.ContainsAny(suffix, "/:@*") || strings.Count(suffix, ".") < 2 {
				return nil, fmt.Errorf("%w: invalid wildcard allow-list entry %q", ErrUpstreamDenied, entry)
			}

			al.wildcards = append(al.wildcards, suffix)
			continue
		}

		if strings.ContainsAny(entry, "/@*") || strings.Contains(entry, "://") {
			return nil, fmt.Errorf("%w: allow-list entry %q must be a host or host:port", ErrUpstreamDenied, entry)
		}

		parsed, err := url.Parse("https://" + entry)
		if err != nil || parsed.Hostname() == "" {
			return nil, fmt.Errorf("%w: invalid allow-list entry %q", ErrUpstreamDenied, entry)
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

	host := strings.ToLower(u.Hostname())
	port := u.Port()
	for _, wantPort := range al.exact[host] {
		if wantPort == "" || wantPort == port {
			return true
		}
	}

	for _, suffix := range al.wildcards {
		if strings.HasSuffix(host, suffix) && len(host) > len(suffix) {
			return true
		}
	}

	return false
}

// checkURL enforces the upstream policy on one URL: the scheme must be https (or http when the test-only plainHTTP
// flag is set), the URL must not carry userinfo, and its host must be on the allow-list.
func checkURL(al *AllowList, u *url.URL, plainHTTP bool) error {
	switch {
	case u.Scheme == "https":
	case u.Scheme == "http" && plainHTTP:
	default:
		return fmt.Errorf("%w: scheme %q is not permitted", ErrUpstreamDenied, u.Scheme)
	}

	if u.User != nil {
		return fmt.Errorf("%w: URL userinfo is not permitted", ErrUpstreamDenied)
	}

	if !al.allows(u) {
		return fmt.Errorf("%w: host %q is not on the allow-list", ErrUpstreamDenied, u.Hostname())
	}

	return nil
}
