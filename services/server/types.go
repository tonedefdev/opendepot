package main

import opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"

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
