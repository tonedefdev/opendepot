package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"net/url"
	"os"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

var (
	logger                              *slog.Logger
	opendepotAnonymousAuth              *bool
	opendepotUseBearerToken             *bool
	opendepotOIDCIssuerURL              *string
	opendepotOIDCClientID               *string
	opendepotOIDCGroupsClaim            *string
	opendepotOIDCAllowSAFallback        *bool
	opendepotOIDCAllowClientCredentials *bool
	opendepotOIDCAuthzURL               *string
	opendepotOIDCTokenURL               *string
	opendepotServerNamespace            *string
	opendepotFilesystemMountPath        *string

	// oidcVerifier is set at startup when --oidc-issuer-url and --oidc-client-id
	// are both provided. It is used to validate OIDC JWTs on every request.
	oidcVerifier *gooidc.IDTokenVerifier
	// oidcCCVerifier is like oidcVerifier but skips the audience check. It is
	// used only for the client credentials path when --oidc-allow-client-credentials
	// is set, to accept tokens issued for a different client ID by the same Dex.
	oidcCCVerifier *gooidc.IDTokenVerifier
	// oidcProvider is the discovered OIDC provider; its Endpoint() is used to
	// populate the login.v1 service discovery response.
	oidcProvider *gooidc.Provider
)

func init() {
	logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)
}

func main() {
	opendepotAnonymousAuth = flag.Bool("anonymous-auth", false, "when true use the server's service account to serve modules and versions without requiring client authentication")
	opendepotUseBearerToken = flag.Bool("use-bearer-token", false, "when true use a bearer token instead of a base64 encoded kubeconfig to authenticate with the kubernetes API server")
	opendepotOIDCIssuerURL = flag.String("oidc-issuer-url", "", "OIDC issuer URL (e.g. https://dex.example.com/dex); when set together with --oidc-client-id the server validates OIDC JWTs and uses its own service account for Kubernetes API calls")
	opendepotOIDCClientID = flag.String("oidc-client-id", "", "OIDC client ID; must be set together with --oidc-issuer-url")
	opendepotOIDCGroupsClaim = flag.String("oidc-groups-claim", "groups", "JWT claim name containing the OIDC groups; used to match GroupBinding expressions")
	opendepotOIDCAllowSAFallback = flag.Bool("oidc-allow-sa-fallback", false, "when true and OIDC mode is active, Kubernetes ServiceAccount bearer tokens with a non-OIDC issuer are authenticated via the bearer token path using the caller's own RBAC; GroupBinding is bypassed for SA tokens")
	opendepotOIDCAllowClientCredentials = flag.Bool("oidc-allow-client-credentials", false, "when true and OIDC mode is active, Dex client credentials tokens (audience != oidc-client-id) are accepted; the token's sub claim is mapped to a virtual group \"client:<sub>\" and evaluated against GroupBinding resources")
	opendepotOIDCAuthzURL = flag.String("oidc-authz-url", "", "override the authorization URL advertised in /.well-known/terraform.json login.v1; when blank uses the authz URL from the OIDC provider discovery document")
	opendepotOIDCTokenURL = flag.String("oidc-token-url", "", "override the token URL advertised in /.well-known/terraform.json login.v1; when blank uses the token URL from the OIDC provider discovery document")
	opendepotServerNamespace = flag.String("namespace", "opendepot-system", "namespace where GroupBinding resources are managed")
	opendepotFilesystemMountPath = flag.String("filesystem-mount-path", "/data/modules", "allowed root path for filesystem module storage; download requests for paths outside this prefix are rejected")
	opendepotCertPath := flag.String("tls-cert-path", "", "path to TLS certificate file for HTTPS server")
	opendepotCertKey := flag.String("tls-cert-key", "", "path to TLS certificate key file for HTTPS server")
	flag.Parse()

	if (*opendepotOIDCIssuerURL == "") != (*opendepotOIDCClientID == "") {
		logger.Error("--oidc-issuer-url and --oidc-client-id must both be set or both be empty")
		os.Exit(1)
	}

	for flagName, rawURL := range map[string]string{
		"--oidc-authz-url": *opendepotOIDCAuthzURL,
		"--oidc-token-url": *opendepotOIDCTokenURL,
	} {
		if rawURL == "" {
			continue
		}
		parsed, err := url.ParseRequestURI(rawURL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			logger.Error("flag must be a well-formed http or https URL", "flag", flagName, "value", rawURL)
			os.Exit(1)
		}
	}

	if *opendepotOIDCIssuerURL != "" {
		ctx := context.Background()
		provider, err := gooidc.NewProvider(ctx, *opendepotOIDCIssuerURL)
		if err != nil {
			logger.Error("failed to discover OIDC provider", "issuerUrl", *opendepotOIDCIssuerURL, "error", err)
			os.Exit(1)
		}

		oidcProvider = provider
		oidcVerifier = provider.Verifier(&gooidc.Config{ClientID: *opendepotOIDCClientID})
		if *opendepotOIDCAllowClientCredentials {
			oidcCCVerifier = provider.Verifier(&gooidc.Config{SkipClientIDCheck: true})
		}

		logger.Info("OIDC auth mode enabled", "issuerUrl", *opendepotOIDCIssuerURL, "clientId", *opendepotOIDCClientID)
	}

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Get("/.well-known/terraform.json", serviceDiscoveryHandler)
	r.Get("/opendepot/modules/v1/{namespace}/{name}/{system}/versions", getModuleVersions)
	r.Get("/opendepot/modules/v1/{namespace}/{name}/{system}/{version}/download", getDownloadModuleUrl)
	r.Get("/opendepot/providers/v1/{namespace}/{type}/versions", getProviderVersions)
	r.Get("/opendepot/providers/v1/{namespace}/{type}/{version}/download/{os}/{arch}", getProviderPackageMetadata)
	r.Get("/opendepot/providers/v1/download/{namespace}/{type}/{version}", serveProviderPackageDownload)
	r.Get("/opendepot/providers/v1/{namespace}/{type}/{version}/SHA256SUMS/{os}/{arch}", getProviderPackageSHA256SUMS)
	r.Get("/opendepot/providers/v1/{namespace}/{type}/{version}/SHA256SUMS.sig/{os}/{arch}", getProviderPackageSHA256SUMSSignature)

	r.Get("/opendepot/modules/v1/download/azure/{subID}/{rg}/{account}/{accountUrl}/{name}/{fileName}", serveModuleFromAzureBlob)
	r.Get("/opendepot/modules/v1/download/fileSystem/{directory}/{name}/{fileName}", serveModuleFromFileSystem)
	r.Get("/opendepot/modules/v1/download/gcs/{bucket}/{name}/{fileName}", serveModuleFromGCS)
	r.Get("/opendepot/modules/v1/download/s3/{bucket}/{region}/{name}/{fileName}", serveModuleFromS3)

	if *opendepotCertPath != "" && *opendepotCertKey != "" {
		logger.Info("Server started and listening on port 8080 with TLS")
		if err := http.ListenAndServeTLS(":8080", *opendepotCertPath, *opendepotCertKey, r); err != nil {
			logger.Error("Failed to start server with TLS", "error", err)
		}
	} else {
		logger.Info("Server started and listening on default port: 8080 without TLS. For secure communication, provide paths to TLS certificate and key using --tls-cert-path and --tls-cert-key flags.")
		if err := http.ListenAndServe(":8080", r); err != nil {
			logger.Error("Failed to start server", "error", err)
		}
	}
}
