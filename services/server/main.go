package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path"
	"sort"
	"strings"
	"time"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"github.com/expr-lang/expr"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"golang.org/x/crypto/openpgp"
	k8sApiErrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	"github.com/tonedefdev/opendepot/pkg/storage"
	storageTypes "github.com/tonedefdev/opendepot/pkg/storage/types"
)

var (
	logger                       *slog.Logger
	opendepotAnonymousAuth       *bool
	opendepotUseBearerToken      *bool
	opendepotOIDCIssuerURL       *string
	opendepotOIDCClientID        *string
	opendepotOIDCGroupsClaim     *string
	opendepotOIDCAllowSAFallback *bool
	opendepotServerNamespace     *string

	// oidcVerifier is set at startup when --oidc-issuer-url and --oidc-client-id
	// are both provided. It is used to validate OIDC JWTs on every request.
	oidcVerifier *gooidc.IDTokenVerifier
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
	opendepotServerNamespace = flag.String("namespace", "opendepot-system", "namespace where GroupBinding resources are managed")
	opendepotCertPath := flag.String("tls-cert-path", "", "path to TLS certificate file for HTTPS server")
	opendepotCertKey := flag.String("tls-cert-key", "", "path to TLS certificate key file for HTTPS server")
	flag.Parse()

	if (*opendepotOIDCIssuerURL == "") != (*opendepotOIDCClientID == "") {
		logger.Error("--oidc-issuer-url and --oidc-client-id must both be set or both be empty")
		os.Exit(1)
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
		http.ListenAndServeTLS("", *opendepotCertPath, *opendepotCertKey, r)
	} else {
		logger.Info("Server started and listening on default port: 8080 without TLS. For secure communication, provide paths to TLS certificate and key using --tls-cert-path and --tls-cert-key flags.")
		if err := http.ListenAndServe(":8080", r); err != nil {
			logger.Error("Failed to start server", "error", err)
		}
	}

}

type ServiceDiscoveryResponse struct {
	ModulesURL   string       `json:"modules.v1"`
	ProvidersURL string       `json:"providers.v1"`
	LoginV1      *LoginV1Info `json:"login.v1,omitempty"`
}

// LoginV1Info carries the OIDC authorization endpoints advertised to tofu CLI
// clients via the service-discovery document. When OIDC is not enabled this
// field is nil and omitted from the JSON response, preserving existing behaviour.
type LoginV1Info struct {
	Client     string   `json:"client"`
	GrantTypes []string `json:"grant_types"`
	Authz      string   `json:"authz"`
	Token      string   `json:"token"`
	Ports      []int    `json:"ports"`
}

type ModuleVersionsResponse struct {
	Modules []ModuleVersions `json:"modules"`
}

type ModuleVersions struct {
	Versions []opendepotv1alpha1.ModuleVersion `json:"versions"`
}

type ProviderVersionsResponse struct {
	Versions []ProviderVersionDetails `json:"versions"`
}

type ProviderVersionDetails struct {
	Version   string             `json:"version"`
	Protocols []string           `json:"protocols,omitempty"`
	Platforms []ProviderPlatform `json:"platforms,omitempty"`
}

type ProviderPlatform struct {
	OS   string `json:"os"`
	Arch string `json:"arch"`
}

type ProviderPackageMetadataResponse struct {
	Protocols           []string            `json:"protocols"`
	OS                  string              `json:"os"`
	Arch                string              `json:"arch"`
	Filename            string              `json:"filename"`
	DownloadURL         string              `json:"download_url"`
	SHASumsURL          string              `json:"shasums_url"`
	SHASumsSignatureURL string              `json:"shasums_signature_url"`
	SHASum              string              `json:"shasum"`
	SigningKeys         ProviderSigningKeys `json:"signing_keys"`
}

type ProviderSigningKeys struct {
	GPGPublicKeys []ProviderSigningKey `json:"gpg_public_keys"`
}

type ProviderSigningKey struct {
	KeyID      string `json:"key_id"`
	ASCIIArmor string `json:"ascii_armor"`
	SourceURL  string `json:"source_url,omitempty"`
}

func serviceDiscoveryHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	response := ServiceDiscoveryResponse{
		ModulesURL:   "/opendepot/modules/v1/",
		ProvidersURL: "/opendepot/providers/v1/",
	}

	if oidcProvider != nil {
		endpoints := oidcProvider.Endpoint()
		response.LoginV1 = &LoginV1Info{
			Client:     *opendepotOIDCClientID,
			GrantTypes: []string{"authz_code", "device_code"},
			Authz:      endpoints.AuthURL,
			Token:      endpoints.TokenURL,
			Ports:      []int{10000, 10001, 10002, 10003, 10004, 10005, 10006, 10007, 10008, 10009, 10010},
		}
	}

	json.NewEncoder(w).Encode(response)
}

func normalizeVersion(versionString string) string {
	return strings.TrimPrefix(strings.TrimSpace(versionString), "v")
}

func getProviderVersionResource(clientset *kubernetes.Clientset, namespace, providerType, requestedVersion string, ctxName string, ctxReq *http.Request) (*opendepotv1alpha1.Version, error) {
	result, err := clientset.RESTClient().
		Get().
		AbsPath("/apis/opendepot.defdev.io/v1alpha1").
		Namespace(namespace).
		Resource("versions").
		DoRaw(ctxReq.Context())
	if err != nil {
		return nil, err
	}

	var versionList opendepotv1alpha1.VersionList
	if err = json.Unmarshal(result, &versionList); err != nil {
		return nil, fmt.Errorf("unable to unmarshal versions list for %s: %w", ctxName, err)
	}

	normalizedRequestedVersion := normalizeVersion(requestedVersion)
	for _, item := range versionList.Items {
		if item.Spec.ProviderConfigRef == nil || item.Spec.ProviderConfigRef.Name == nil {
			continue
		}

		if *item.Spec.ProviderConfigRef.Name != providerType {
			continue
		}

		if normalizeVersion(item.Spec.Version) != normalizedRequestedVersion {
			continue
		}

		return &item, nil
	}

	return nil, nil
}

func buildDownloadPathFromVersion(versionResource *opendepotv1alpha1.Version) (string, error) {
	var storageConfig *opendepotv1alpha1.StorageConfig
	var name *string

	if versionResource.Spec.ModuleConfigRef != nil && versionResource.Spec.ModuleConfigRef.StorageConfig != nil {
		storageConfig = versionResource.Spec.ModuleConfigRef.StorageConfig
		name = versionResource.Spec.ModuleConfigRef.Name
	} else if versionResource.Spec.ProviderConfigRef != nil && versionResource.Spec.ProviderConfigRef.StorageConfig != nil {
		storageConfig = versionResource.Spec.ProviderConfigRef.StorageConfig
		name = versionResource.Spec.ProviderConfigRef.Name
	}

	if storageConfig == nil || name == nil || versionResource.Spec.FileName == nil {
		return "", fmt.Errorf("storage configuration not available for version '%s'", versionResource.Name)
	}

	if storageConfig.AzureStorage != nil {
		return fmt.Sprintf("azure/%s/%s/%s/%s/%s/%s",
			storageConfig.AzureStorage.SubscriptionID,
			storageConfig.AzureStorage.ResourceGroup,
			storageConfig.AzureStorage.AccountName,
			url.PathEscape(storageConfig.AzureStorage.AccountUrl),
			*name,
			*versionResource.Spec.FileName,
		), nil
	}

	if storageConfig.FileSystem != nil && storageConfig.FileSystem.DirectoryPath != nil {
		return fmt.Sprintf("fileSystem/%s/%s/%s",
			base64.RawURLEncoding.EncodeToString([]byte(*storageConfig.FileSystem.DirectoryPath)),
			*name,
			*versionResource.Spec.FileName,
		), nil
	}

	if storageConfig.GCS != nil {
		return fmt.Sprintf("gcs/%s/%s/%s",
			storageConfig.GCS.Bucket,
			*name,
			*versionResource.Spec.FileName,
		), nil
	}

	if storageConfig.S3 != nil {
		return fmt.Sprintf("s3/%s/%s/%s",
			storageConfig.S3.Bucket,
			storageConfig.S3.Region,
			*storageConfig.S3.Key,
		), nil
	}

	return "", fmt.Errorf("unsupported storage configuration for version '%s'", versionResource.Name)
}

func requestBaseURL(r *http.Request) string {
	scheme := "https"
	if r.TLS == nil {
		if fwdProto := r.Header.Get("X-Forwarded-Proto"); fwdProto != "" {
			scheme = fwdProto
		} else {
			scheme = "http"
		}
	}

	return fmt.Sprintf("%s://%s", scheme, r.Host)
}

func getProviderSigningKeysFromEnv() (*ProviderSigningKeys, error) {
	keyID := strings.TrimSpace(os.Getenv("OPENDEPOT_PROVIDER_GPG_KEY_ID"))
	asciiArmor := os.Getenv("OPENDEPOT_PROVIDER_GPG_ASCII_ARMOR")
	if keyID == "" || asciiArmor == "" {
		return nil, fmt.Errorf("missing provider signing key env vars: OPENDEPOT_PROVIDER_GPG_KEY_ID and OPENDEPOT_PROVIDER_GPG_ASCII_ARMOR")
	}

	keys := &ProviderSigningKeys{
		GPGPublicKeys: []ProviderSigningKey{
			{
				KeyID:      strings.ToUpper(keyID),
				ASCIIArmor: asciiArmor,
				SourceURL:  strings.TrimSpace(os.Getenv("OPENDEPOT_PROVIDER_GPG_SOURCE_URL")),
			},
		},
	}

	return keys, nil
}

func decodeSHA256Checksum(base64Checksum string) (string, error) {
	decoded, err := base64.StdEncoding.DecodeString(base64Checksum)
	if err != nil {
		return "", err
	}

	return hex.EncodeToString(decoded), nil
}

func sanitizeModuleVersionForLookup(version string) string {
	if len(version) > 0 && version[0] == 'v' {
		version = version[1:]
	}
	version = strings.ToLower(version)
	version = strings.ReplaceAll(version, ".", "-")
	version = strings.ReplaceAll(version, "_", "-")
	return version
}

func getModuleVersion(clientset *kubernetes.Clientset, w http.ResponseWriter, r *http.Request) (*opendepotv1alpha1.Version, error) {
	name := chi.URLParam(r, "name")
	namespace := chi.URLParam(r, "namespace")
	version := chi.URLParam(r, "version")
	moduleName := fmt.Sprintf("%s-%s", name, sanitizeModuleVersionForLookup(version))

	result, err := clientset.RESTClient().
		Get().
		AbsPath("/apis/opendepot.defdev.io/v1alpha1").
		Namespace(namespace).
		Resource("versions").
		Name(moduleName).
		DoRaw(r.Context())
	if err != nil {
		return nil, err
	}

	var moduleVersion opendepotv1alpha1.Version
	if err = json.Unmarshal(result, &moduleVersion); err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return nil, err
	}

	return &moduleVersion, nil
}

func getDownloadModuleUrl(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	clientset, binding, subject, err := getKubeClientFromRequest(w, r)
	if err != nil {
		logger.Error("unable to generate kubeclient", "error", err)
		return
	}

	if binding != nil {
		name := chi.URLParam(r, "name")
		namespace := chi.URLParam(r, "namespace")

		if !isResourceAllowed(binding, "module", name) {
			logger.Warn("resource access denied", "subject", subject, "binding_name", binding.Name, "resource_type", "module", "resource_name", name, "namespace", namespace)
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		logger.Info("resource access allowed", "subject", subject, "binding_name", binding.Name, "resource_type", "module", "resource_name", name, "namespace", namespace)
	}

	moduleVersion, err := getModuleVersion(clientset, w, r)
	if err != nil {
		logger.Error("unable to get module version", "error", err)
		return
	}

	var downloadPath string
	if moduleVersion.Spec.ModuleConfigRef.StorageConfig.AzureStorage != nil {
		downloadPath = fmt.Sprintf("azure/%s/%s/%s/%s/%s/%s",
			moduleVersion.Spec.ModuleConfigRef.StorageConfig.AzureStorage.SubscriptionID,
			moduleVersion.Spec.ModuleConfigRef.StorageConfig.AzureStorage.ResourceGroup,
			moduleVersion.Spec.ModuleConfigRef.StorageConfig.AzureStorage.AccountName,
			url.PathEscape(moduleVersion.Spec.ModuleConfigRef.StorageConfig.AzureStorage.AccountUrl),
			*moduleVersion.Spec.ModuleConfigRef.Name,
			*moduleVersion.Spec.FileName,
		)
	}

	if moduleVersion.Spec.ModuleConfigRef.StorageConfig.FileSystem != nil {
		downloadPath = fmt.Sprintf("fileSystem/%s/%s/%s",
			base64.RawURLEncoding.EncodeToString([]byte(*moduleVersion.Spec.ModuleConfigRef.StorageConfig.FileSystem.DirectoryPath)),
			*moduleVersion.Spec.ModuleConfigRef.Name,
			*moduleVersion.Spec.FileName,
		)
	}

	if moduleVersion.Spec.ModuleConfigRef.StorageConfig.GCS != nil {
		downloadPath = fmt.Sprintf("gcs/%s/%s/%s",
			moduleVersion.Spec.ModuleConfigRef.StorageConfig.GCS.Bucket,
			*moduleVersion.Spec.ModuleConfigRef.Name,
			*moduleVersion.Spec.FileName,
		)
	}

	if moduleVersion.Spec.ModuleConfigRef.StorageConfig.S3 != nil {
		downloadPath = fmt.Sprintf("s3/%s/%s/%s",
			moduleVersion.Spec.ModuleConfigRef.StorageConfig.S3.Bucket,
			moduleVersion.Spec.ModuleConfigRef.StorageConfig.S3.Region,
			*moduleVersion.Spec.ModuleConfigRef.StorageConfig.S3.Key,
		)
	}

	if moduleVersion.Status.Checksum == nil {
		http.Error(w, "module version checksum not yet available", http.StatusServiceUnavailable)
		return
	}

	checksumQuery := url.QueryEscape(*moduleVersion.Status.Checksum)
	w.Header().Set("X-Terraform-Get", fmt.Sprintf("/opendepot/modules/v1/download/%s?fileChecksum=%s", downloadPath, checksumQuery))
	w.WriteHeader(http.StatusNoContent)
}

func serveModuleFromAzureBlob(w http.ResponseWriter, r *http.Request) {
	accountName := chi.URLParam(r, "account")
	accountUrl := chi.URLParam(r, "accountUrl")
	rg := chi.URLParam(r, "rg")
	subID := chi.URLParam(r, "subID")

	name := chi.URLParam(r, "name")
	fileName := chi.URLParam(r, "fileName")
	checksum := r.URL.Query().Get("fileChecksum")

	accountUrl, err := url.PathUnescape(accountUrl)
	if err != nil {
		logger.Error("failed to unescape account url", "error", err)
		http.Error(w, "failed to get module", http.StatusInternalServerError)
		return
	}

	storage := &storage.AzureBlobStorage{}
	if err := storage.NewClients(subID, accountUrl); err != nil {
		logger.Error("failed to init azure clients", "error", err, "storageAccountName", accountName)
		http.Error(w, "failed to get module", http.StatusInternalServerError)
		return
	}

	soi := &storageTypes.StorageObjectInput{
		FilePath:      &fileName,
		Method:        storageTypes.Get,
		ContainerName: &name,
		StorageConfig: &opendepotv1alpha1.StorageConfig{
			AzureStorage: &opendepotv1alpha1.AzureStorageConfig{
				AccountName:    accountName,
				AccountUrl:     accountUrl,
				ResourceGroup:  rg,
				SubscriptionID: subID,
			},
		},
	}

	getObjectFromStorageSystem(w, r, storage, soi, checksum)
}

func serveModuleFromFileSystem(w http.ResponseWriter, r *http.Request) {
	encodedDir := chi.URLParam(r, "directory")
	moduleName := chi.URLParam(r, "name")
	fileName := chi.URLParam(r, "fileName")
	checksum := r.URL.Query().Get("fileChecksum")

	// go-getter sends ?terraform-get=1 to detect source URLs via HTML meta tags.
	// We intercept this and return the X-Terraform-Get header pointing to the same
	// download URL. go-getter reads the header before parsing the body, then processes
	// the source URL through its full pipeline which detects the archive extension
	// and uses direct file download (no further terraform-get detection).
	if r.URL.Query().Get("terraform-get") == "1" {
		scheme := "https"
		if r.TLS == nil {
			if fwdProto := r.Header.Get("X-Forwarded-Proto"); fwdProto != "" {
				scheme = fwdProto
			} else {
				scheme = "http"
			}
		}

		q := url.Values{}
		q.Set("fileChecksum", checksum)

		// GitHub tarballs are gzip-compressed despite having .tar extension.
		// go-getter uses the archive param to select the decompressor, so we
		// must specify tar.gz for gzipped tarballs.
		ext := path.Ext(fileName)
		archiveType := strings.TrimPrefix(ext, ".")
		if archiveType == "tar" {
			archiveType = "tar.gz"
		}

		q.Set("archive", archiveType)
		sourceURL := fmt.Sprintf("%s://%s/opendepot/modules/v1/download/fileSystem/%s/%s/%s?%s",
			scheme, r.Host, encodedDir, moduleName, fileName, q.Encode())

		w.Header().Set("X-Terraform-Get", sourceURL)
		w.WriteHeader(http.StatusOK)
		return
	}

	dirBytes, err := base64.RawURLEncoding.DecodeString(encodedDir)
	if err != nil {
		logger.Error("failed to decode directory path", "error", err)
		http.Error(w, "failed to get module", http.StatusInternalServerError)
		return
	}
	dir := string(dirBytes)

	logger.Info("filesystem download", "dir", dir, "module", moduleName, "file", fileName)

	filePath := path.Join(
		dir,
		moduleName,
		fileName,
	)

	storage := &storage.FileSystem{}
	soi := &storageTypes.StorageObjectInput{
		FilePath: &filePath,
		Method:   storageTypes.Get,
	}

	getObjectFromStorageSystem(w, r, storage, soi, checksum)
}

func serveModuleFromGCS(w http.ResponseWriter, r *http.Request) {
	bucket := chi.URLParam(r, "bucket")
	name := chi.URLParam(r, "name")
	fileName := chi.URLParam(r, "fileName")
	checksum := r.URL.Query().Get("fileChecksum")

	gcsStorage := &storage.GoogleCloudStorage{}
	if err := gcsStorage.NewClient(r.Context()); err != nil {
		logger.Error("failed to init gcs client", "error", err, "bucket", bucket)
		http.Error(w, "failed to get module", http.StatusInternalServerError)
		return
	}

	soi := &storageTypes.StorageObjectInput{
		FilePath: aws.String(fmt.Sprintf("%s/%s", name, fileName)),
		Method:   storageTypes.Get,
		StorageConfig: &opendepotv1alpha1.StorageConfig{
			GCS: &opendepotv1alpha1.GoogleCloudStorageConfig{
				Bucket: bucket,
			},
		},
	}

	getObjectFromStorageSystem(w, r, gcsStorage, soi, checksum)
}

func serveModuleFromS3(w http.ResponseWriter, r *http.Request) {
	bucket := chi.URLParam(r, "bucket")
	region := chi.URLParam(r, "region")
	name := chi.URLParam(r, "name")
	fileName := chi.URLParam(r, "fileName")
	checksum := r.URL.Query().Get("fileChecksum")

	storage := &storage.AmazonS3Storage{}
	if err := storage.NewClient(r.Context(), region); err != nil {
		logger.Error("failed to init s3 client", "error", err, "bucket", bucket)
		http.Error(w, "failed to get module", http.StatusInternalServerError)
		return
	}

	soi := &storageTypes.StorageObjectInput{
		FilePath: aws.String(fmt.Sprintf("%s/%s", name, fileName)),
		Method:   storageTypes.Get,
		StorageConfig: &opendepotv1alpha1.StorageConfig{
			S3: &opendepotv1alpha1.AmazonS3Config{
				Bucket: bucket,
			},
		},
	}

	getObjectFromStorageSystem(w, r, storage, soi, checksum)
}

// getObjectFromStorage validates the object's sha256 checksum and when valid copies from the storage system src to the
// download stream dst provided by http.ResponseWriter
func getObjectFromStorageSystem(w http.ResponseWriter, r *http.Request, storage storage.Storage, soi *storageTypes.StorageObjectInput, checksum string) {
	if err := storage.GetObjectChecksum(r.Context(), soi); err != nil {
		logger.Error("failed to get checksum from storage system", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if soi.ObjectChecksum != nil && *soi.ObjectChecksum != checksum {
		logger.Error("checksum mismatch from storage system", "want", checksum, "received", *soi.ObjectChecksum)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	reader, err := storage.GetObject(r.Context(), soi)
	if err != nil {
		logger.Error("failed to get module from storage system", "error", err)
		http.Error(w, "failed to get module", http.StatusInternalServerError)
		return
	}

	if strings.HasSuffix(*soi.FilePath, ".zip") {
		w.Header().Set("Content-Type", "application/zip")
	} else {
		w.Header().Set("Content-Type", "application/x-tar")
	}

	if _, err := io.Copy(w, reader); err != nil {
		http.Error(w, fmt.Sprintf("failed to stream file: %v", err), http.StatusInternalServerError)
		return
	}
}

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
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return nil, nil, "", fmt.Errorf("OIDC token verification failed: %w", err)
		}

		var claims map[string]interface{}
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
// Handles both []interface{} (array claim) and string (single-value claim) representations.
func extractGroupsClaim(claims map[string]interface{}, claimName string) ([]string, error) {
	raw, ok := claims[claimName]
	if !ok {
		return nil, fmt.Errorf("claim %q not present", claimName)
	}
	switch v := raw.(type) {
	case []interface{}:
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

func getModuleVersions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	clientset, binding, subject, err := getKubeClientFromRequest(w, r)
	if err != nil {
		logger.Error("unable to generate kubeclient", "error", err)
		return
	}

	namespace := chi.URLParam(r, "namespace")
	name := chi.URLParam(r, "name")

	if binding != nil {
		if !isResourceAllowed(binding, "module", name) {
			logger.Warn("resource access denied", "subject", subject, "binding_name", binding.Name, "resource_type", "module", "resource_name", name, "namespace", namespace)
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		logger.Info("resource access allowed", "subject", subject, "binding_name", binding.Name, "resource_type", "module", "resource_name", name, "namespace", namespace)
	}

	result, err := clientset.RESTClient().
		Get().
		AbsPath("/apis/opendepot.defdev.io/v1alpha1").
		Namespace(namespace).
		Resource("modules").
		Name(name).
		DoRaw(r.Context())
	if err != nil {
		logger.Error("unable to get modules", "error", err, "namespace", namespace, "name", name, "responseBody", string(result))
		if k8sApiErrors.IsForbidden(err) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
	}

	var module opendepotv1alpha1.Module
	if err = json.Unmarshal(result, &module); err != nil {
		logger.Error("unable to unmarshal module", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	response := ModuleVersionsResponse{
		Modules: []ModuleVersions{
			{
				Versions: module.Spec.Versions,
			},
		},
	}

	json.NewEncoder(w).Encode(response)
}

func getProviderVersions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	clientset, binding, subject, err := getKubeClientFromRequest(w, r)
	if err != nil {
		logger.Error("unable to generate kubeclient", "error", err)
		return
	}

	namespace := chi.URLParam(r, "namespace")
	providerType := chi.URLParam(r, "type")

	if binding != nil {
		if !isResourceAllowed(binding, "provider", providerType) {
			logger.Warn("resource access denied", "subject", subject, "binding_name", binding.Name, "resource_type", "provider", "resource_name", providerType, "namespace", namespace)
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		logger.Info("resource access allowed", "subject", subject, "binding_name", binding.Name, "resource_type", "provider", "resource_name", providerType, "namespace", namespace)
	}

	_, err = clientset.RESTClient().
		Get().
		AbsPath("/apis/opendepot.defdev.io/v1alpha1").
		Namespace(namespace).
		Resource("providers").
		Name(providerType).
		DoRaw(r.Context())
	if err != nil {
		if k8sApiErrors.IsNotFound(err) {
			http.Error(w, "provider not found", http.StatusNotFound)
			return
		}

		logger.Error("unable to get provider resource", "error", err, "namespace", namespace, "type", providerType)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	result, err := clientset.RESTClient().
		Get().
		AbsPath("/apis/opendepot.defdev.io/v1alpha1").
		Namespace(namespace).
		Resource("versions").
		DoRaw(r.Context())
	if err != nil {
		logger.Error("unable to list versions", "error", err, "namespace", namespace, "type", providerType)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	var versionList opendepotv1alpha1.VersionList
	if err = json.Unmarshal(result, &versionList); err != nil {
		logger.Error("unable to unmarshal versions list", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	versionSet := make(map[string]struct{})
	providerVersions := make([]ProviderVersionDetails, 0)
	for _, item := range versionList.Items {
		if item.Spec.ProviderConfigRef == nil || item.Spec.ProviderConfigRef.Name == nil {
			continue
		}

		if *item.Spec.ProviderConfigRef.Name != providerType {
			continue
		}

		normalized := normalizeVersion(item.Spec.Version)
		if normalized == "" {
			continue
		}

		if _, exists := versionSet[normalized]; exists {
			continue
		}

		versionSet[normalized] = struct{}{}
		providerVersions = append(providerVersions, ProviderVersionDetails{
			Version: normalized,
		})
	}

	response := ProviderVersionsResponse{Versions: providerVersions}
	json.NewEncoder(w).Encode(response)
}

func getProviderPackageMetadata(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	clientset, binding, subject, err := getKubeClientFromRequest(w, r)
	if err != nil {
		logger.Error("unable to generate kubeclient", "error", err)
		return
	}

	namespace := chi.URLParam(r, "namespace")
	providerType := chi.URLParam(r, "type")
	requestedVersion := chi.URLParam(r, "version")
	osName := chi.URLParam(r, "os")
	arch := chi.URLParam(r, "arch")

	if binding != nil {
		if !isResourceAllowed(binding, "provider", providerType) {
			logger.Warn("resource access denied", "subject", subject, "binding_name", binding.Name, "resource_type", "provider", "resource_name", providerType, "namespace", namespace)
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		logger.Info("resource access allowed", "subject", subject, "binding_name", binding.Name, "resource_type", "provider", "resource_name", providerType, "namespace", namespace)
	}

	versionResource, err := getProviderVersionResource(clientset, namespace, providerType, requestedVersion, "provider package metadata", r)
	if err != nil {
		logger.Error("unable to locate provider version", "error", err, "namespace", namespace, "type", providerType, "version", requestedVersion)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if versionResource == nil {
		http.Error(w, "provider package not found", http.StatusNotFound)
		return
	}

	if versionResource.Spec.FileName == nil || versionResource.Status.Checksum == nil {
		http.Error(w, "provider package metadata incomplete", http.StatusNotImplemented)
		return
	}

	signingKeys, err := getProviderSigningKeysFromEnv()
	if err != nil {
		logger.Error("provider signing keys are not configured", "error", err)
		http.Error(w, "provider signing metadata not configured", http.StatusNotImplemented)
		return
	}

	checksumHex, err := decodeSHA256Checksum(*versionResource.Status.Checksum)
	if err != nil {
		logger.Error("unable to decode provider checksum", "error", err, "checksum", *versionResource.Status.Checksum)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	baseURL := requestBaseURL(r)
	versionString := normalizeVersion(versionResource.Spec.Version)

	response := ProviderPackageMetadataResponse{
		Protocols:           []string{"5.0"},
		OS:                  osName,
		Arch:                arch,
		Filename:            *versionResource.Spec.FileName,
		DownloadURL:         fmt.Sprintf("%s/opendepot/providers/v1/download/%s/%s/%s", baseURL, namespace, providerType, versionString),
		SHASumsURL:          fmt.Sprintf("%s/opendepot/providers/v1/%s/%s/%s/SHA256SUMS/%s/%s", baseURL, namespace, providerType, versionString, osName, arch),
		SHASumsSignatureURL: fmt.Sprintf("%s/opendepot/providers/v1/%s/%s/%s/SHA256SUMS.sig/%s/%s", baseURL, namespace, providerType, versionString, osName, arch),
		SHASum:              checksumHex,
		SigningKeys:         *signingKeys,
	}

	json.NewEncoder(w).Encode(response)
}

func serveProviderPackageDownload(w http.ResponseWriter, r *http.Request) {
	// Provider artifact download endpoints are accessed by OpenTofu without
	// credentials (per the Terraform Provider Registry Protocol spec). Use the
	// server's own service account for k8s access.
	clientset, err := generateKubeClient(nil, nil, false)
	if err != nil {
		logger.Error("unable to generate kubeclient for provider download", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	namespace := chi.URLParam(r, "namespace")
	providerType := chi.URLParam(r, "type")
	requestedVersion := chi.URLParam(r, "version")

	versionResource, err := getProviderVersionResource(clientset, namespace, providerType, requestedVersion, "provider package download", r)
	if err != nil {
		logger.Error("unable to locate provider version for download", "error", err, "namespace", namespace, "type", providerType, "version", requestedVersion)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if versionResource == nil {
		http.Error(w, "provider package not found", http.StatusNotFound)
		return
	}

	if versionResource.Status.Checksum == nil {
		http.Error(w, "provider package checksum unavailable", http.StatusNotImplemented)
		return
	}

	// Attempt a presigned URL redirect so Terraform downloads directly from the storage backend.
	if versionResource.Spec.ProviderConfigRef != nil &&
		versionResource.Spec.ProviderConfigRef.StorageConfig != nil &&
		versionResource.Spec.ProviderConfigRef.Name != nil &&
		versionResource.Spec.FileName != nil {

		storageConfig := versionResource.Spec.ProviderConfigRef.StorageConfig
		presignCfg := storageConfig.Presign

		if presignCfg != nil && presignCfg.Enabled != nil && *presignCfg.Enabled {
			name := versionResource.Spec.ProviderConfigRef.Name

			objectKey := fmt.Sprintf("%s/%s", *name, *versionResource.Spec.FileName)

			ttl := 15 * time.Minute
			if presignCfg.TTL != nil {
				ttl = presignCfg.TTL.Duration
			}

			soi := &storageTypes.StorageObjectInput{
				FilePath:      &objectKey,
				ContainerName: name,
				StorageConfig: storageConfig,
				PresignTTL:    ttl,
			}

			fallback := presignCfg.FallbackToProxy == nil || *presignCfg.FallbackToProxy

			storageBackend, initErr := initStorageBackend(r.Context(), storageConfig)
			if initErr != nil {
				if !fallback {
					logger.Error("failed to init storage backend for presign", "error", initErr)
					http.Error(w, "failed to initialize storage backend for pre-signed URL", http.StatusBadGateway)
					return
				}
				logger.Info("failed to init storage backend for presign, falling back to proxy", "error", initErr)
			} else if presignErr := storageBackend.PresignObject(r.Context(), soi); presignErr == nil {
				http.Redirect(w, r, *soi.PresignedURL, http.StatusTemporaryRedirect)
				return
			} else {
				if !fallback {
					logger.Error("presign failed", "error", presignErr)
					http.Error(w, "failed to generate pre-signed URL", http.StatusBadGateway)
					return
				}
				logger.Info("presign not supported or failed, falling back to proxy", "error", presignErr)
			}
		}
	}

	// Fallback: proxy the binary through the server.
	downloadPath, err := buildDownloadPathFromVersion(versionResource)
	if err != nil {
		logger.Error("unable to build download path for provider package", "error", err, "version", versionResource.Name)
		http.Error(w, "provider package download backend not implemented", http.StatusNotImplemented)
		return
	}

	checksumQuery := url.QueryEscape(*versionResource.Status.Checksum)
	http.Redirect(w, r, fmt.Sprintf("/opendepot/modules/v1/download/%s?fileChecksum=%s", downloadPath, checksumQuery), http.StatusFound)
}

// initStorageBackend creates and initialises a storage backend from the provided StorageConfig.
// It returns an error if the backend cannot be initialised or if no supported backend is configured.
func initStorageBackend(ctx context.Context, storageConfig *opendepotv1alpha1.StorageConfig) (storage.Storage, error) {
	if storageConfig.S3 != nil {
		s3Storage := &storage.AmazonS3Storage{}
		if err := s3Storage.NewClient(ctx, storageConfig.S3.Region); err != nil {
			return nil, fmt.Errorf("failed to init s3 client: %w", err)
		}
		return s3Storage, nil
	}

	if storageConfig.GCS != nil {
		gcsStorage := &storage.GoogleCloudStorage{}
		if err := gcsStorage.NewClient(ctx); err != nil {
			return nil, fmt.Errorf("failed to init gcs client: %w", err)
		}
		return gcsStorage, nil
	}

	if storageConfig.AzureStorage != nil {
		azStorage := &storage.AzureBlobStorage{}
		if err := azStorage.NewClients(storageConfig.AzureStorage.SubscriptionID, storageConfig.AzureStorage.AccountUrl); err != nil {
			return nil, fmt.Errorf("failed to init azure client: %w", err)
		}
		return azStorage, nil
	}

	if storageConfig.FileSystem != nil {
		return &storage.FileSystem{}, nil
	}

	return nil, fmt.Errorf("unsupported storage configuration")
}

func getProviderPackageSHA256SUMS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")

	// SHA256SUMS is a provider artifact endpoint fetched by OpenTofu without
	// credentials (per the Terraform Provider Registry Protocol spec). Access to
	// this URL is gated by the authenticated metadata endpoint that returns it.
	clientset, err := generateKubeClient(nil, nil, false)
	if err != nil {
		logger.Error("unable to generate kubeclient for provider shasums", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	namespace := chi.URLParam(r, "namespace")
	providerType := chi.URLParam(r, "type")
	requestedVersion := chi.URLParam(r, "version")

	versionResource, err := getProviderVersionResource(clientset, namespace, providerType, requestedVersion, "provider shasums", r)
	if err != nil {
		logger.Error("unable to locate provider version for shasums", "error", err, "namespace", namespace, "type", providerType, "version", requestedVersion)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if versionResource == nil || versionResource.Spec.FileName == nil || versionResource.Status.Checksum == nil {
		http.Error(w, "provider package not found", http.StatusNotFound)
		return
	}

	checksumHex, err := decodeSHA256Checksum(*versionResource.Status.Checksum)
	if err != nil {
		logger.Error("unable to decode provider checksum", "error", err, "checksum", *versionResource.Status.Checksum)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	_, _ = w.Write([]byte(fmt.Sprintf("%s  %s\n", checksumHex, *versionResource.Spec.FileName)))
}

func getProviderPackageSHA256SUMSSignature(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/octet-stream")

	// SHA256SUMS.sig is a provider artifact endpoint fetched by OpenTofu without
	// credentials (per the Terraform Provider Registry Protocol spec). Access to
	// this URL is gated by the authenticated metadata endpoint that returns it.
	clientset, err := generateKubeClient(nil, nil, false)
	if err != nil {
		logger.Error("unable to generate kubeclient for provider shasums signature", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	namespace := chi.URLParam(r, "namespace")
	providerType := chi.URLParam(r, "type")
	requestedVersion := chi.URLParam(r, "version")

	versionResource, err := getProviderVersionResource(clientset, namespace, providerType, requestedVersion, "provider shasums signature", r)
	if err != nil {
		logger.Error("unable to locate provider version for shasums signature", "error", err, "namespace", namespace, "type", providerType, "version", requestedVersion)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if versionResource == nil || versionResource.Spec.FileName == nil || versionResource.Status.Checksum == nil {
		http.Error(w, "provider package not found", http.StatusNotFound)
		return
	}

	checksumHex, err := decodeSHA256Checksum(*versionResource.Status.Checksum)
	if err != nil {
		logger.Error("unable to decode provider checksum", "error", err, "checksum", *versionResource.Status.Checksum)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	shasumsContent := fmt.Sprintf("%s  %s\n", checksumHex, *versionResource.Spec.FileName)

	privateKeyBase64 := strings.TrimSpace(os.Getenv("OPENDEPOT_PROVIDER_GPG_PRIVATE_KEY_BASE64"))
	if privateKeyBase64 == "" {
		http.Error(w, "provider gpg private key not configured", http.StatusNotImplemented)
		return
	}

	privateKeyArmor, err := base64.StdEncoding.DecodeString(privateKeyBase64)
	if err != nil {
		logger.Error("unable to decode gpg private key", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	entityList, err := openpgp.ReadArmoredKeyRing(bytes.NewReader(privateKeyArmor))
	if err != nil {
		logger.Error("unable to parse gpg private key", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if len(entityList) == 0 {
		logger.Error("no gpg entities found in private key")
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	var sigBuf bytes.Buffer
	if err = openpgp.DetachSign(&sigBuf, entityList[0], strings.NewReader(shasumsContent), nil); err != nil {
		logger.Error("unable to sign provider shasums", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	_, _ = w.Write(sigBuf.Bytes())
}
