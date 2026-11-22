package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
)

// oidcCCAudienceToken mints an unsigned-checked token for agentTestIssuer. A nil aud omits the claim.
func oidcCCAudienceToken(t *testing.T, aud any) string {
	t.Helper()

	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	claims := map[string]any{
		"iss": agentTestIssuer,
		"sub": "svc-ci",
		"iat": time.Now().Unix(),
		"exp": time.Now().Add(time.Hour).Unix(),
	}
	if aud != nil {
		claims["aud"] = aud
	}

	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshal claims: %v", err)
	}

	return header + "." + base64.RawURLEncoding.EncodeToString(payload) + ".c2ln"
}

func TestParseClientCredentialsAudiences(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want []string
	}{
		{name: "empty", raw: "", want: nil},
		{name: "blank entries are dropped", raw: " , ,", want: nil},
		{name: "single", raw: "ci-pipeline", want: []string{"ci-pipeline"}},
		{name: "trimmed and split", raw: " ci-pipeline , svc-other ", want: []string{"ci-pipeline", "svc-other"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseClientCredentialsAudiences(tc.raw)
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}

			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("got %v, want %v", got, tc.want)
				}
			}
		})
	}
}

func TestVerifyClientCredentialsTokenAudience(t *testing.T) {
	originalVerifiers := oidcCCVerifiers
	t.Cleanup(func() { oidcCCVerifiers = originalVerifiers })

	oidcCCVerifiers = []*gooidc.IDTokenVerifier{
		gooidc.NewVerifier(agentTestIssuer, agentTestKeySet{}, &gooidc.Config{ClientID: "ci-pipeline"}),
		gooidc.NewVerifier(agentTestIssuer, agentTestKeySet{}, &gooidc.Config{ClientID: "svc-other"}),
	}

	cases := []struct {
		name    string
		aud     any
		wantErr bool
	}{
		{name: "first allowlisted audience is accepted", aud: "ci-pipeline"},
		{name: "second allowlisted audience is accepted", aud: "svc-other"},
		{name: "primary opendepot audience not on allowlist is rejected", aud: "opendepot", wantErr: true},
		{name: "unlisted audience is rejected", aud: "attacker", wantErr: true},
		{name: "missing audience is rejected", aud: nil, wantErr: true},
		{name: "empty audience is rejected", aud: "", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			token, err := verifyClientCredentialsToken(context.Background(), oidcCCAudienceToken(t, tc.aud))
			if tc.wantErr {
				if err == nil || token != nil {
					t.Fatalf("expected rejection, got token=%v err=%v", token != nil, err)
				}

				return
			}

			if err != nil || token == nil {
				t.Fatalf("expected acceptance, got token=%v err=%v", token != nil, err)
			}

			var claims map[string]any
			if err := token.Claims(&claims); err != nil || claims["sub"] != "svc-ci" {
				t.Fatalf("claims = %v, err = %v", claims, err)
			}
		})
	}
}

func TestVerifyClientCredentialsTokenWrongIssuerRejected(t *testing.T) {
	originalVerifiers := oidcCCVerifiers
	t.Cleanup(func() { oidcCCVerifiers = originalVerifiers })

	oidcCCVerifiers = []*gooidc.IDTokenVerifier{
		gooidc.NewVerifier("https://other-issuer.example.com", agentTestKeySet{}, &gooidc.Config{ClientID: "ci-pipeline"}),
	}

	if _, err := verifyClientCredentialsToken(context.Background(), oidcCCAudienceToken(t, "ci-pipeline")); err == nil {
		t.Fatal("expected token from a different issuer to be rejected")
	}
}

func TestVerifyClientCredentialsTokenNoAudiencesConfigured(t *testing.T) {
	originalVerifiers := oidcCCVerifiers
	t.Cleanup(func() { oidcCCVerifiers = originalVerifiers })

	oidcCCVerifiers = nil

	if _, err := verifyClientCredentialsToken(context.Background(), oidcCCAudienceToken(t, "ci-pipeline")); err == nil {
		t.Fatal("expected rejection when no client credentials audiences are configured")
	}
}
