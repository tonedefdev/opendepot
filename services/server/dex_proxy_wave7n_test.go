package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
)

// wave7nDiscoveryProvider serves an OIDC discovery document whose jwks_uri is built from jwksURI(base).
func wave7nDiscoveryProvider(t *testing.T, jwksURI func(base string) string) (*gooidc.Provider, string) {
	t.Helper()

	var base string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/openid-configuration" {
			http.NotFound(w, r)

			return
		}

		doc := map[string]any{
			"issuer":                                base,
			"authorization_endpoint":                base + "/dex/auth",
			"token_endpoint":                        base + "/dex/token",
			"jwks_uri":                              jwksURI(base),
			"id_token_signing_alg_values_supported": []string{"RS256"},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(doc)
	}))
	t.Cleanup(server.Close)
	base = server.URL

	provider, err := gooidc.NewProvider(context.Background(), base)
	if err != nil {
		t.Fatalf("create OIDC provider: %v", err)
	}

	return provider, base
}

func TestWave7NNewInternalDexKeySetFetchesFromInternalURL(t *testing.T) {
	var internalHits atomic.Int32

	internal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		internalHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"keys":[]}`))
	}))
	t.Cleanup(internal.Close)

	provider, externalURL := wave7nDiscoveryProvider(t, func(base string) string {
		return base + "/dex/keys"
	})

	keySet, err := newInternalDexKeySet(context.Background(), provider, externalURL, internal.URL)
	if err != nil {
		t.Fatalf("newInternalDexKeySet returned error: %v", err)
	}

	if keySet == nil {
		t.Fatal("newInternalDexKeySet returned nil key set")
	}

	_, verifyErr := keySet.VerifySignature(context.Background(), "eyJhbGciOiJSUzI1NiJ9.e30.c2ln")
	if verifyErr == nil {
		t.Fatal("expected signature verification to fail against an empty key set")
	}

	if internalHits.Load() == 0 {
		t.Fatal("expected JWKS to be fetched from the internal Dex URL")
	}
}

func TestWave7NNewInternalDexKeySetRejectsJWKSOutsideIssuer(t *testing.T) {
	provider, _ := wave7nDiscoveryProvider(t, func(_ string) string {
		return "https://elsewhere.example.com/keys"
	})

	keySet, err := newInternalDexKeySet(context.Background(), provider, "https://dex.example.com", "http://dex.internal:5556")
	if err == nil {
		t.Fatal("expected error for jwks_uri outside the issuer prefix")
	}

	if keySet != nil {
		t.Fatal("expected nil key set on error")
	}
}

func TestWave7NNewInternalDexKeySetRejectsMissingJWKSURI(t *testing.T) {
	provider, _ := wave7nDiscoveryProvider(t, func(_ string) string {
		return ""
	})

	keySet, err := newInternalDexKeySet(context.Background(), provider, "https://dex.example.com", "http://dex.internal:5556")
	if err == nil {
		t.Fatal("expected error when discovery omits jwks_uri")
	}

	if keySet != nil {
		t.Fatal("expected nil key set on error")
	}
}

func TestWave7NNewDexProxyHandlerForwardsToInternalDex(t *testing.T) {
	var gotPath, gotHost string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotHost = r.Host
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("dex"))
	}))
	t.Cleanup(upstream.Close)

	handler, err := newDexProxyHandler(upstream.URL)
	if err != nil {
		t.Fatalf("newDexProxyHandler returned error: %v", err)
	}

	front := httptest.NewServer(handler)
	t.Cleanup(front.Close)

	response, err := http.Get(front.URL + "/dex/.well-known/openid-configuration")
	if err != nil {
		t.Fatalf("proxy request failed: %v", err)
	}
	t.Cleanup(func() { _ = response.Body.Close() })

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200 from proxied Dex, got %d", response.StatusCode)
	}

	if gotPath != "/dex/.well-known/openid-configuration" {
		t.Fatalf("expected path to be forwarded unchanged, got %q", gotPath)
	}

	if gotHost == "" {
		t.Fatal("expected upstream request to carry a host")
	}
}

func TestWave7NNewDexProxyHandlerRejectsInvalidURL(t *testing.T) {
	handler, err := newDexProxyHandler("://bad-url")
	if err == nil {
		t.Fatal("expected parse error for malformed internal Dex URL")
	}

	if handler != nil {
		t.Fatal("expected nil handler on error")
	}
}
