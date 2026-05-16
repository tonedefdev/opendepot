package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"path"
	"sort"
	"strings"

	"github.com/expr-lang/expr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
)

// generateKubeClient creates a new kubernetes client from either a kubeconfig as a byte slice
// or from a bearerToken. When using a bearerToken this function will use the in-cluster config
// to generate the necessary rest.Config settings for TLS connections.
func generateKubeClient(kubeconfig []byte, bearerToken *string, useBearerToken bool) (*kubernetes.Clientset, error) {
	var clientConfig *rest.Config
	var err error

	if bearerToken == nil && kubeconfig == nil {
		// Anonymous auth: use in-cluster config with the server's own service account
		clientConfig, err = rest.InClusterConfig()
		if err != nil {
			return nil, err
		}
	} else if useBearerToken {
		clientConfig, err = rest.InClusterConfig()
		if err != nil {
			return nil, err
		}

		clientConfig.BearerToken = *bearerToken
		clientConfig.BearerTokenFile = ""
	} else {
		config, err := clientcmd.NewClientConfigFromBytes(kubeconfig)
		if err != nil {
			return nil, err
		}

		clientConfig, err = config.ClientConfig()
		if err != nil {
			return nil, err
		}
	}

	clientset, err := kubernetes.NewForConfig(clientConfig)
	if err != nil {
		return nil, err
	}

	return clientset, nil
}

// getKubeClientFromRequest creates a Kubernetes clientset based on the configured auth mode.
// When anonymous auth is enabled, uses the server's in-cluster service account.
// When OIDC mode is enabled, validates the Bearer JWT, looks up the matching GroupBinding,
// and returns the server's SA clientset together with the binding for resource authorization.
// When bearer token mode is enabled, extracts the token from the Authorization header.
// Otherwise, extracts a base64-encoded kubeconfig from the Authorization header.
// The returned GroupBinding is non-nil only in OIDC mode. The returned string is the
// OIDC token subject (empty string for non-OIDC auth paths).
func getKubeClientFromRequest(w http.ResponseWriter, r *http.Request) (*kubernetes.Clientset, *opendepotv1alpha1.GroupBinding, string, error) {
	if *opendepotAnonymousAuth {
		cs, err := generateKubeClient(nil, nil, false)
		return cs, nil, "", err
	}

	if oidcVerifier != nil {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			http.Error(w, "missing Authorization header", http.StatusUnauthorized)
			return nil, nil, "", fmt.Errorf("missing Authorization header")
		}

		if !strings.HasPrefix(authHeader, "Bearer ") {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return nil, nil, "", fmt.Errorf("malformed Authorization header scheme")
		}

		rawToken := strings.TrimPrefix(authHeader, "Bearer ")
		idToken, err := oidcVerifier.Verify(r.Context(), rawToken)
		if err != nil {
			if *opendepotOIDCAllowSAFallback {
				iss, parseErr := parseUnsignedJWTIssuer(rawToken)
				// Only fall back to the SA bearer path when the token clearly comes from a
				// different issuer. A token that claims to be from the OIDC issuer but
				// failed verification (expired, bad signature, wrong audience) still gets
				// a 401 — we never fall back for bad Dex tokens.
				if parseErr == nil && iss != *opendepotOIDCIssuerURL {
					cs, saErr := generateKubeClient(nil, &rawToken, true)
					if saErr != nil {
						http.Error(w, "internal server error", http.StatusInternalServerError)
						return nil, nil, "", saErr
					}
					logger.Debug("SA fallback auth accepted", "issuer", iss)
					return cs, nil, "", nil
				}
			}
			// Client credentials fallback: accept Dex-issued tokens that failed the
			// primary audience check (aud != oidc-client-id). The secondary verifier
			// still enforces signature validity, expiry, and issuer. The token's sub
			// claim is mapped to a virtual group "client:<sub>" for GroupBinding evaluation.
			if *opendepotOIDCAllowClientCredentials && oidcCCVerifier != nil {
				cs, binding, sub, ccErr := handleClientCredentialsToken(w, r, rawToken)
				if ccErr != nil {
					return nil, nil, "", ccErr
				}
				if cs != nil {
					return cs, binding, sub, nil
				}
			}

			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return nil, nil, "", fmt.Errorf("OIDC token verification failed: %w", err)
		}

		var claims map[string]any
		if err := idToken.Claims(&claims); err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return nil, nil, "", fmt.Errorf("failed to extract JWT claims: %w", err)
		}

		groups, _ := extractGroupsClaim(claims, *opendepotOIDCGroupsClaim)

		cs, err := generateKubeClient(nil, nil, false)
		if err != nil {
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return nil, nil, "", err
		}

		// The groups claim is required when OIDC is enabled. A JWT that does not carry
		// the configured claim is denied — there is no bypass path.
		if len(groups) == 0 {
			logger.Warn("JWT missing required groups claim, denying access", "subject", idToken.Subject, "groups_claim", *opendepotOIDCGroupsClaim)
			http.Error(w, "forbidden", http.StatusForbidden)
			return nil, nil, "", fmt.Errorf("JWT missing required groups claim %q", *opendepotOIDCGroupsClaim)
		}

		logger.Debug("JWT verified", "subject", idToken.Subject, "groups_claim", *opendepotOIDCGroupsClaim, "groups", groups)

		binding, err := findGroupBinding(r.Context(), cs, groups)
		if err != nil {
			logger.Warn("GroupBinding evaluation failed", "subject", idToken.Subject, "groups", groups, "error", err)
			http.Error(w, "forbidden", http.StatusForbidden)
			return nil, nil, "", err
		}

		logger.Info("GroupBinding matched", "subject", idToken.Subject, "groups", groups, "binding_name", binding.Name, "expression", binding.Spec.Expression)

		return cs, binding, idToken.Subject, nil
	}

	var kubeconfig []byte
	var bearerToken string

	if *opendepotUseBearerToken {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			http.Error(w, "missing Authorization header", http.StatusUnauthorized)
			return nil, nil, "", fmt.Errorf("missing Authorization header")
		}
		bearerToken = strings.TrimPrefix(authHeader, "Bearer ")
	} else {
		config, err := extractKubeconfig(w, r)
		if err != nil {
			return nil, nil, "", err
		}
		kubeconfig = config
	}

	cs, err := generateKubeClient(kubeconfig, &bearerToken, *opendepotUseBearerToken)
	return cs, nil, "", err
}

// handleClientCredentialsToken attempts to verify a Dex client-credentials token using the
// secondary audience-skipping verifier. It returns (nil, nil, "", nil) when the token does
// not match the CC path (caller should fall through). On success it returns the clientset,
// binding, and subject. On failure it writes an HTTP error and returns a non-nil error.
func handleClientCredentialsToken(w http.ResponseWriter, r *http.Request, rawToken string) (*kubernetes.Clientset, *opendepotv1alpha1.GroupBinding, string, error) {
	iss, parseErr := parseUnsignedJWTIssuer(rawToken)
	if parseErr != nil || iss != *opendepotOIDCIssuerURL {
		return nil, nil, "", nil
	}

	ccToken, ccErr := oidcCCVerifier.Verify(r.Context(), rawToken)
	if ccErr != nil {
		return nil, nil, "", nil
	}

	var ccClaims map[string]any
	if claimsErr := ccToken.Claims(&ccClaims); claimsErr != nil {
		return nil, nil, "", nil
	}

	sub, _ := ccClaims["sub"].(string)
	if sub == "" {
		return nil, nil, "", nil
	}

	cs, csErr := generateKubeClient(nil, nil, false)
	if csErr != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return nil, nil, "", csErr
	}

	ccGroups := []string{"client:" + sub}
	logger.Debug("client credentials token accepted; mapped sub to virtual group", "subject", sub)

	binding, bindErr := findGroupBinding(r.Context(), cs, ccGroups)
	if bindErr != nil {
		logger.Warn("GroupBinding evaluation failed for client credentials token", "subject", sub, "error", bindErr)
		http.Error(w, "forbidden", http.StatusForbidden)
		return nil, nil, "", bindErr
	}

	logger.Info("GroupBinding matched for client credentials token", "subject", sub, "binding_name", binding.Name, "expression", binding.Spec.Expression)
	return cs, binding, sub, nil
}

// parseUnsignedJWTIssuer decodes the payload segment of a JWT without verifying the signature
// and returns the "iss" claim. Used only for SA fallback routing — signature validity is
// irrelevant here; the Kubernetes API server validates the token itself.
func parseUnsignedJWTIssuer(rawToken string) (string, error) {
	parts := strings.Split(rawToken, ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("malformed JWT: expected 3 parts, got %d", len(parts))
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", fmt.Errorf("failed to base64-decode JWT payload: %w", err)
	}
	var claims struct {
		Issuer string `json:"iss"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", fmt.Errorf("failed to unmarshal JWT payload: %w", err)
	}
	return claims.Issuer, nil
}

// extractGroupsClaim reads the named claim from a JWT claims map and returns it as a []string.
// Handles both []any (array claim) and string (single-value claim) representations.
func extractGroupsClaim(claims map[string]any, claimName string) ([]string, error) {
	raw, ok := claims[claimName]
	if !ok {
		return nil, fmt.Errorf("claim %q not present", claimName)
	}

	switch v := raw.(type) {
	case []any:
		groups := make([]string, 0, len(v))
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("claim %q contains non-string value", claimName)
			}
			groups = append(groups, s)
		}
		return groups, nil
	case string:
		return []string{v}, nil
	default:
		return nil, fmt.Errorf("claim %q has unexpected type %T", claimName, raw)
	}
}

// findGroupBinding lists all GroupBindings in the server namespace and returns the first one
// whose expr-lang expression evaluates to true for the provided groups.
// Returns an error if the listing fails, if any expression fails to compile or evaluate
// (fail-closed: a broken binding denies all access rather than being silently skipped),
// or if no binding matches.
func findGroupBinding(ctx context.Context, clientset *kubernetes.Clientset, groups []string) (*opendepotv1alpha1.GroupBinding, error) {
	result, err := clientset.RESTClient().
		Get().
		AbsPath("/apis/opendepot.defdev.io/v1alpha1").
		Namespace(*opendepotServerNamespace).
		Resource("groupbindings").
		DoRaw(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list GroupBindings: %w", err)
	}

	var list opendepotv1alpha1.GroupBindingList
	if err := json.Unmarshal(result, &list); err != nil {
		return nil, fmt.Errorf("failed to unmarshal GroupBindingList: %w", err)
	}

	sort.Slice(list.Items, func(i, j int) bool {
		return list.Items[i].Name < list.Items[j].Name
	})

	env := opendepotv1alpha1.GroupBindingExprEnv{Groups: groups}
	for i := range list.Items {
		binding := &list.Items[i]
		program, compileErr := expr.Compile(binding.Spec.Expression, expr.Env(opendepotv1alpha1.GroupBindingExprEnv{}), expr.AsBool())

		if compileErr != nil {
			logger.Error("GroupBinding expression invalid", "binding_name", binding.Name, "expression", binding.Spec.Expression, "error", compileErr)
			return nil, fmt.Errorf("GroupBinding %q expression is invalid: %w", binding.Name, compileErr)
		}

		out, runErr := expr.Run(program, env)
		if runErr != nil {
			logger.Error("GroupBinding expression evaluation failed", "binding_name", binding.Name, "expression", binding.Spec.Expression, "error", runErr)
			return nil, fmt.Errorf("GroupBinding %q expression evaluation failed: %w", binding.Name, runErr)
		}

		if out.(bool) {
			return binding, nil
		}
	}

	return nil, fmt.Errorf("no GroupBinding matched for the provided groups")
}

// isResourceAllowed reports whether resourceName is permitted by the given GroupBinding.
// For modules, patterns in ModuleResources are matched using path.Match (* wildcard).
// For providers, entries in ProviderResources are exact names or the literal "*" to allow all.
func isResourceAllowed(binding *opendepotv1alpha1.GroupBinding, resourceType, resourceName string) bool {
	switch resourceType {
	case "module":
		for _, pattern := range binding.Spec.ModuleResources {
			if matched, _ := path.Match(pattern, resourceName); matched {
				return true
			}
		}
	case "provider":
		for _, name := range binding.Spec.ProviderResources {
			if name == "*" || name == resourceName {
				return true
			}
		}
	}

	return false
}

func extractKubeconfig(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		http.Error(w, "missing Authorization header", http.StatusUnauthorized)
		return nil, fmt.Errorf("missing Authorization header")
	}

	kubeconfigBase64 := strings.ReplaceAll(authHeader, "Bearer ", "")
	kubeconfig, err := base64.StdEncoding.DecodeString(kubeconfigBase64)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return nil, err
	}

	return kubeconfig, nil
}
