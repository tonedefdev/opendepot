package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
)

// newInternalDexKeySet builds a KeySet that fetches JWKS from Dex's internal,
// in-cluster address rather than the external jwks_uri reported by the OIDC
// discovery document. This avoids requiring the server pod to hairpin back
// out through its own public ingress on every JWT verification when Dex is
// reverse-proxied through the server (--oidc-dex-proxy-enabled).
func newInternalDexKeySet(ctx context.Context, provider *gooidc.Provider, externalIssuerURL, internalDexURL string) (gooidc.KeySet, error) {
	var claims struct {
		JWKSURI string `json:"jwks_uri"`
	}

	if err := provider.Claims(&claims); err != nil {
		return nil, fmt.Errorf("dex proxy: read jwks_uri from discovery document: %w", err)
	}

	if claims.JWKSURI == "" || !strings.HasPrefix(claims.JWKSURI, externalIssuerURL) {
		return nil, fmt.Errorf("dex proxy: jwks_uri %q does not share the expected issuer prefix %q", claims.JWKSURI, externalIssuerURL)
	}

	internalJWKSURL := internalDexURL + strings.TrimPrefix(claims.JWKSURI, externalIssuerURL)

	return gooidc.NewRemoteKeySet(ctx, internalJWKSURL), nil
}

// newDexProxyHandler returns an http.Handler that reverse-proxies requests to
// the internal Dex service. Dex's own endpoints are already rooted at /dex,
// matching the mount path exactly, so no path rewriting is required.
func newDexProxyHandler(internalDexURL string) (http.Handler, error) {
	parsed, err := url.Parse(internalDexURL)
	if err != nil {
		return nil, fmt.Errorf("dex proxy: parse internal Dex URL: %w", err)
	}

	target := &url.URL{Scheme: parsed.Scheme, Host: parsed.Host}

	return httputil.NewSingleHostReverseProxy(target), nil
}
