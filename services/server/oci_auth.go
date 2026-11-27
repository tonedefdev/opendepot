package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"

	"k8s.io/client-go/kubernetes"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
)

const (
	ociRealm                 = "opendepot"
	ociTokenExchangeTimeout  = 10 * time.Second
	ociTokenResponseMaxBytes = 64 << 10
	ociTokenCacheMargin      = time.Minute
	ociTokenCacheMaxEntries  = 256
	ociIdentityCacheMaxAge   = 5 * time.Minute
	ociSchemeHTTP            = "http"
	ociSchemeHTTPS           = "https"
)

// ociNow returns the current time. Tests replace it to move the credential and index caches past their expiry.
var ociNow = time.Now

// ociCredentialCache holds the verified identities of client credentials that were exchanged with the IdP. It is keyed
// by ociCredentialCacheKey, so a secret is never stored in clear text. An identity holds no GroupBinding, so the
// binding is resolved on every request and a change to a GroupBinding applies to cached credentials at once.
var ociCredentialCache = newTTLCache[ociIdentity](ociTokenCacheMaxEntries)

// ociIdentity is the verified identity of a client credentials caller. The cache holds it until expiresAt.
type ociIdentity struct {
	subject   string
	groups    []string
	expiresAt time.Time
}

// ociPrincipal is an authenticated caller of the OCI and Helm read routes.
type ociPrincipal struct {
	clientset *kubernetes.Clientset
	// binding is the GroupBinding that matched the caller. It is nil when access is governed by RBAC alone.
	binding *opendepotv1alpha1.GroupBinding
}

// authenticateOCIRequest authenticates an OCI or Helm request. Bearer tokens and anonymous mode work as they do on the
// other routes. Basic credentials are accepted in two forms. The password is tried first as a bearer token. When that
// fails and OIDC is enabled, the username and password are exchanged with the IdP as client credentials. It returns
// the principal and status 0, or a nil principal and the HTTP status of the failure. It never logs the credentials.
func authenticateOCIRequest(r *http.Request) (*ociPrincipal, int) {
	if !*opendepotAnonymousAuth && hasBasicScheme(r.Header.Get("Authorization")) {
		username, password, ok := r.BasicAuth()
		if !ok {
			return nil, http.StatusUnauthorized
		}

		return authenticateOCIBasic(r, username, password)
	}

	return authenticateOCIBearer(r)
}

// hasBasicScheme reports whether an Authorization header uses the Basic scheme. The scheme name is case-insensitive.
func hasBasicScheme(header string) bool {
	return len(header) > len("Basic ") && strings.EqualFold(header[:len("Basic ")], "Basic ")
}

// authenticateOCIBearer runs the existing authentication path for the request. Its plain-text error responses are
// discarded and only the status is returned.
func authenticateOCIBearer(r *http.Request) (*ociPrincipal, int) {
	discard := &ociDiscardWriter{header: http.Header{}}
	clientset, binding, _, _, err := getKubeClientFromRequest(discard, r)
	if err != nil {
		return nil, discard.failureStatus()
	}

	return &ociPrincipal{clientset: clientset, binding: binding}, 0
}

// authenticateOCIBasic authenticates Basic credentials. A cached client credentials principal is used first. Then the
// password is validated as a bearer token. Last, the credentials are exchanged with the IdP when OIDC is enabled.
func authenticateOCIBasic(r *http.Request, username, password string) (*ociPrincipal, int) {
	exchangeable := oidcVerifier != nil && username != "" && password != ""
	cacheKey := ""
	if exchangeable {
		cacheKey = ociCredentialCacheKey(username, password)
		if identity, ok := ociCredentialCache.get(cacheKey, ociNow()); ok {
			return principalForIdentity(r.Context(), identity)
		}
	}

	if principal, status := authenticateOCIBearer(withBearer(r, password)); status == 0 {
		return principal, 0
	}

	if !exchangeable {
		return nil, http.StatusUnauthorized
	}

	return authenticateOCIClientCredentials(r, cacheKey, username, password)
}

// authenticateOCIClientCredentials exchanges client credentials with the IdP token endpoint and verifies the access
// token that comes back. The verified identity is cached for the token lifetime less a safety margin, capped at
// ociIdentityCacheMaxAge. The principal is then resolved from the identity.
func authenticateOCIClientCredentials(r *http.Request, cacheKey, clientID, clientSecret string) (*ociPrincipal, int) {
	tokenURL, err := ociTokenURL()
	if err != nil {
		logger.Warn("OCI client credentials unavailable", "error", err)
		return nil, http.StatusUnauthorized
	}

	accessToken, lifetime, err := exchangeOCIClientCredentials(r.Context(), tokenURL, clientID, clientSecret)
	if err != nil {
		logger.Warn("OCI client credentials exchange failed", "error", err)
		return nil, http.StatusUnauthorized
	}

	identity, err := verifyOCIAccessToken(r.Context(), accessToken)
	if err != nil {
		logger.Warn("OCI client credentials token rejected", "error", err)
		return nil, http.StatusUnauthorized
	}

	now := ociNow()
	if ttl := min(lifetime-ociTokenCacheMargin, ociIdentityCacheMaxAge); ttl > 0 {
		identity.expiresAt = now.Add(ttl)
		ociCredentialCache.put(cacheKey, identity, identity.expiresAt, now)
	}

	return principalForIdentity(r.Context(), identity)
}

// verifyOCIAccessToken verifies an access token that the IdP issued for client credentials and returns the identity it
// carries. It applies the rules of the bearer path. The OIDC verifier is tried first, and the groups come from the
// groups claim. Otherwise the client credentials verifier is tried, and the subject maps to the "client:<sub>" group.
func verifyOCIAccessToken(ctx context.Context, rawToken string) (ociIdentity, error) {
	if oidcVerifier != nil {
		if idToken, err := oidcVerifier.Verify(ctx, rawToken); err == nil {
			var claims map[string]any
			if err := idToken.Claims(&claims); err != nil {
				return ociIdentity{}, fmt.Errorf("extract JWT claims: %w", err)
			}

			groups, err := extractGroupsClaim(claims, *opendepotOIDCGroupsClaim)
			if err != nil || len(groups) == 0 {
				return ociIdentity{}, errors.New("token carries no usable groups claim")
			}

			return ociIdentity{subject: idToken.Subject, groups: groups}, nil
		}
	}

	if !*opendepotOIDCAllowClientCredentials || len(oidcCCVerifiers) == 0 {
		return ociIdentity{}, errors.New("token is not verified by the OIDC verifier")
	}

	iss, err := parseUnsignedJWTIssuer(rawToken)
	if err != nil || iss != *opendepotOIDCIssuerURL {
		return ociIdentity{}, errors.New("token is not issued by the configured issuer")
	}

	ccToken, err := verifyClientCredentialsToken(ctx, rawToken)
	if err != nil {
		return ociIdentity{}, fmt.Errorf("client credentials verification failed: %w", err)
	}

	var ccClaims map[string]any
	if err := ccToken.Claims(&ccClaims); err != nil {
		return ociIdentity{}, fmt.Errorf("extract client credentials claims: %w", err)
	}

	sub, _ := ccClaims["sub"].(string)
	if sub == "" {
		return ociIdentity{}, errors.New("client credentials token has no subject")
	}

	return ociIdentity{subject: sub, groups: []string{"client:" + sub}}, nil
}

// principalForIdentity builds the principal of a verified identity. The GroupBinding is resolved against the identity's
// groups on every call, so a narrowed or deleted GroupBinding takes effect even for cached credentials. A caller that
// no GroupBinding matches is forbidden, as it is on the bearer path.
func principalForIdentity(ctx context.Context, identity ociIdentity) (*ociPrincipal, int) {
	clientset, err := generateKubeClient(nil, nil, false)
	if err != nil {
		logger.Error("OCI principal client unavailable", "subject", identity.subject, "error", err)
		return nil, http.StatusInternalServerError
	}

	binding, err := findGroupBinding(ctx, clientset, identity.groups)
	if err != nil {
		logger.Warn("GroupBinding evaluation failed", "subject", identity.subject, "error", err)
		return nil, http.StatusForbidden
	}

	return &ociPrincipal{clientset: clientset, binding: binding}, 0
}

// exchangeOCIClientCredentials requests an access token with the client_credentials grant. The client authenticates
// with HTTP Basic. The request is bounded by a timeout, the response size is limited, and redirects are refused so the
// secret is never sent to another host. Errors never include the response body or the credentials.
func exchangeOCIClientCredentials(
	ctx context.Context,
	tokenURL, clientID, clientSecret string,
) (string, time.Duration, error) {
	form := url.Values{"grant_type": {"client_credentials"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", 0, fmt.Errorf("build token request: %w", err)
	}

	req.SetBasicAuth(clientID, clientSecret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", ociContentTypeJSON)

	client := &http.Client{
		Timeout: ociTokenExchangeTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("token endpoint redirects are not followed")
		},
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("token request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", 0, fmt.Errorf("token endpoint returned status %d", resp.StatusCode)
	}

	var body struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}

	if err := json.NewDecoder(io.LimitReader(resp.Body, ociTokenResponseMaxBytes)).Decode(&body); err != nil {
		return "", 0, errors.New("token endpoint returned an invalid response")
	}

	if body.AccessToken == "" {
		return "", 0, errors.New("token endpoint returned no access token")
	}

	return body.AccessToken, time.Duration(body.ExpiresIn) * time.Second, nil
}

// ociTokenURL returns the IdP token endpoint for the client credentials exchange. It is --oidc-token-url when set, and
// otherwise the token endpoint from the OIDC discovery document. It must use HTTPS, or HTTP on a loopback host.
func ociTokenURL() (string, error) {
	raw := ""
	if opendepotOIDCTokenURL != nil {
		raw = *opendepotOIDCTokenURL
	}

	if raw == "" && oidcProvider != nil {
		raw = oidcProvider.Endpoint().TokenURL
	}

	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || parsed.User != nil {
		return "", errors.New("token URL is not configured or is invalid")
	}

	switch {
	case parsed.Scheme == ociSchemeHTTPS:
	case parsed.Scheme == ociSchemeHTTP && isLoopbackHost(parsed.Hostname()):
	default:
		return "", errors.New("token URL must use HTTPS or a loopback host")
	}

	return parsed.String(), nil
}

// isLoopbackHost reports whether host is localhost or a loopback IP address.
func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}

	ip := net.ParseIP(host)

	return ip != nil && ip.IsLoopback()
}

// withBearer returns a copy of r that carries token as its bearer credential.
func withBearer(r *http.Request, token string) *http.Request {
	clone := r.Clone(r.Context())
	clone.Header.Set("Authorization", "Bearer "+token)

	return clone
}

// ociCredentialCacheKey returns the SHA-256 hash of the client ID and secret. The hash is the cache key, so neither
// value is held in the cache.
func ociCredentialCacheKey(clientID, clientSecret string) string {
	sum := sha256.Sum256([]byte(clientID + "\x00" + clientSecret))

	return hex.EncodeToString(sum[:])
}

// setOCIChallenge sets the Basic challenge that OCI clients use to request credentials.
func setOCIChallenge(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Basic realm="`+ociRealm+`"`)
}

// ociArtifactVisible reports whether the principal may read the OCIArtifact resource named name. A principal with no
// GroupBinding is governed by RBAC alone, as on the other routes. A GroupBinding grants the names that match its
// OCIArtifactResources patterns.
func ociArtifactVisible(principal *ociPrincipal, name string) bool {
	if principal.binding == nil {
		return true
	}

	for _, pattern := range principal.binding.Spec.OCIArtifactResources {
		matched, err := path.Match(pattern, name)
		if err != nil {
			logger.Error("auth: GroupBinding carries a malformed OCI artifact resource pattern",
				"groupBinding", principal.binding.Name, "pattern", pattern, "error", err)

			continue
		}

		if matched {
			return true
		}
	}

	return false
}

// ociBindingKey identifies what a principal may see, for use in cache keys. It changes when the matched GroupBinding
// changes, and it is constant for principals that RBAC alone governs.
func ociBindingKey(principal *ociPrincipal) string {
	if principal.binding == nil {
		return "rbac"
	}

	return principal.binding.Name + "@" + principal.binding.ResourceVersion + "|" +
		strings.Join(principal.binding.Spec.OCIArtifactResources, ",")
}

// ociDiscardWriter records the status that an authentication helper writes and discards the body. Its plain-text
// error responses are never sent to the client, because the OCI and Helm routes write their own error bodies.
type ociDiscardWriter struct {
	header http.Header
	status int
}

func (w *ociDiscardWriter) Header() http.Header {
	return w.header
}

func (w *ociDiscardWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}

func (w *ociDiscardWriter) Write(p []byte) (int, error) {
	w.WriteHeader(http.StatusOK)

	return len(p), nil
}

// failureStatus returns the status that the helper wrote. It returns 401 when the helper wrote none.
func (w *ociDiscardWriter) failureStatus() int {
	if w.status == 0 {
		return http.StatusUnauthorized
	}

	return w.status
}

// ttlCache is a bounded map whose entries expire. When the map is full, it drops the expired entries first, then the
// entry that expires first.
type ttlCache[V any] struct {
	mu         sync.Mutex
	maxEntries int
	entries    map[string]ttlCacheEntry[V]
}

type ttlCacheEntry[V any] struct {
	value     V
	expiresAt time.Time
}

func newTTLCache[V any](maxEntries int) *ttlCache[V] {
	return &ttlCache[V]{maxEntries: maxEntries, entries: make(map[string]ttlCacheEntry[V])}
}

// get returns the value stored under key while it has not expired at now.
func (c *ttlCache[V]) get(key string, now time.Time) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.entries[key]
	if !ok || !now.Before(entry.expiresAt) {
		delete(c.entries, key)

		var zero V

		return zero, false
	}

	return entry.value, true
}

// put stores value under key until expiresAt, evicting an entry when the cache is full.
func (c *ttlCache[V]) put(key string, value V, expiresAt, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.entries[key]; !exists && len(c.entries) >= c.maxEntries {
		c.evictLocked(now)
	}

	c.entries[key] = ttlCacheEntry[V]{value: value, expiresAt: expiresAt}
}

// evictLocked removes the expired entries and, when the map is still full, the entry that expires first.
func (c *ttlCache[V]) evictLocked(now time.Time) {
	for key, entry := range c.entries {
		if !now.Before(entry.expiresAt) {
			delete(c.entries, key)
		}
	}

	if len(c.entries) < c.maxEntries {
		return
	}

	var oldestKey string
	var oldest time.Time
	first := true
	for key, entry := range c.entries {
		if first || entry.expiresAt.Before(oldest) {
			oldestKey, oldest, first = key, entry.expiresAt, false
		}
	}

	delete(c.entries, oldestKey)
}
