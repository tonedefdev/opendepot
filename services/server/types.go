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
