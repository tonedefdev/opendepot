package main

import opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"

// ServiceDiscoveryResponse is the JSON body returned at /.well-known/terraform.json.
// It advertises the module and provider registry URLs and, when OIDC is configured,
// the login.v1 block required by the OpenTofu CLI login flow.
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
	Scopes     []string `json:"scopes"`
	Ports      []int    `json:"ports"`
}

// ModuleVersionsResponse is the JSON body returned by the module versions endpoint.
type ModuleVersionsResponse struct {
	Modules []ModuleVersions `json:"modules"`
}

// ModuleVersions holds the list of available versions for a single module.
type ModuleVersions struct {
	Versions []opendepotv1alpha1.ModuleVersion `json:"versions"`
}

// ProviderVersionsResponse is the JSON body returned by the provider versions endpoint.
type ProviderVersionsResponse struct {
	Versions []ProviderVersionDetails `json:"versions"`
}

// ProviderVersionDetails describes a single published provider version, including its
// supported protocols and target platforms.
type ProviderVersionDetails struct {
	Version   string             `json:"version"`
	Protocols []string           `json:"protocols,omitempty"`
	Platforms []ProviderPlatform `json:"platforms,omitempty"`
}

// ProviderPlatform represents an OS/architecture combination for a provider binary.
type ProviderPlatform struct {
	OS   string `json:"os"`
	Arch string `json:"arch"`
}

// ProviderPackageMetadataResponse is the JSON body returned by the provider package
// metadata endpoint. It provides download, checksum, and signing-key details consumed
// by the OpenTofu provider installer.
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

// ProviderSigningKeys wraps the list of GPG public keys used to verify provider binaries.
type ProviderSigningKeys struct {
	GPGPublicKeys []ProviderSigningKey `json:"gpg_public_keys"`
}

// ProviderSigningKey represents a single GPG public key used for provider signature verification.
type ProviderSigningKey struct {
	KeyID      string `json:"key_id"`
	ASCIIArmor string `json:"ascii_armor"`
	SourceURL  string `json:"source_url,omitempty"`
}

// BrowseScanCounts holds compact per-severity finding counts for UI card icons.
type BrowseScanCounts struct {
	Critical int `json:"critical"`
	High     int `json:"high"`
	Medium   int `json:"medium"`
	Low      int `json:"low"`
	Unknown  int `json:"unknown"`
}

// BrowseResource is a card-ready summary of a Module or Provider resource.
type BrowseResource struct {
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	// LatestVersion is the most recent version string, if any.
	LatestVersion string `json:"latestVersion,omitempty"`
	Synced        bool   `json:"synced"`
	SyncStatus    string `json:"syncStatus,omitempty"`
	// Module-specific fields.
	Provider string `json:"provider,omitempty"`
	RepoURL  string `json:"repoUrl,omitempty"`
	// Provider-specific fields.
	ProviderNamespace string             `json:"providerNamespace,omitempty"`
	Platforms         []ProviderPlatform `json:"platforms,omitempty"`
	// Scan metadata.
	ScanCounts  *BrowseScanCounts `json:"scanCounts,omitempty"`
	LastScanned string            `json:"lastScanned,omitempty"`
	// Public reports whether the namespace and resource are both explicitly public.
	Public bool `json:"public"`
}

// BrowseResourceList is the JSON body returned by the browse resources list endpoint.
type BrowseResourceList struct {
	Items      []BrowseResource `json:"items"`
	TotalCount int              `json:"totalCount"`
	Page       int              `json:"page"`
	PageSize   int              `json:"pageSize"`
}

// BrowseNamespace is a namespace visible to the caller.
type BrowseNamespace struct {
	Name   string `json:"name"`
	Public bool   `json:"public"`
}

// BrowseNamespaceList is the JSON body returned by the browse namespaces endpoint.
type BrowseNamespaceList struct {
	Items []BrowseNamespace `json:"items"`
}

// BrowseVersionSummary summarizes a single version for the detail page.
type BrowseVersionSummary struct {
	Version     string            `json:"version"`
	Synced      bool              `json:"synced"`
	SyncStatus  string            `json:"syncStatus,omitempty"`
	OS          string            `json:"os,omitempty"`
	Arch        string            `json:"arch,omitempty"`
	ScanCounts  *BrowseScanCounts `json:"scanCounts,omitempty"`
	LastScanned string            `json:"lastScanned,omitempty"`
}

// BrowseResourceDetail is the full drill-down payload for a single resource.
type BrowseResourceDetail struct {
	BrowseResource
	// Versions is the list of known versions for this resource.
	Versions []BrowseVersionSummary `json:"versions,omitempty"`
	// SourceScanFindings are the IaC (module) or go.mod (provider) vulnerability findings.
	SourceScanFindings []opendepotv1alpha1.SecurityFinding `json:"sourceScanFindings,omitempty"`
	// BinaryScanFindings are per-artifact (os/arch) provider binary vulnerability findings.
	// Keys are in the form "os/arch".
	BinaryScanFindings map[string][]opendepotv1alpha1.SecurityFinding `json:"binaryScanFindings,omitempty"`
}
