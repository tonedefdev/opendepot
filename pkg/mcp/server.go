package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// ServerSchemaURL is the server.json schema this package targets. The unreleased draft schema is not supported.
const ServerSchemaURL = "https://static.modelcontextprotocol.io/schemas/2025-12-11/server.schema.json"

// Registry types accepted for packages[].registryType.
const (
	RegistryTypeNPM   = "npm"
	RegistryTypePyPI  = "pypi"
	RegistryTypeOCI   = "oci"
	RegistryTypeNuGet = "nuget"
	RegistryTypeMCPB  = "mcpb"
)

// Transport types from the 2025-12-11 schema (StdioTransport, StreamableHttpTransport, SseTransport).
const (
	TransportStdio          = "stdio"
	TransportStreamableHTTP = "streamable-http"
	TransportSSE            = "sse"
)

const (
	minServerNameLength  = 3
	maxServerNameLength  = 200
	maxDescriptionLength = 100
	maxTitleLength       = 100
	maxVersionLength     = 255
	maxIconSourceLength  = 255
	maxIdentifierLength  = 1024
	maxRepositoryLength  = 2048
)

var (
	serverNamePattern   = regexp.MustCompile(`^[a-zA-Z0-9.-]+/[a-zA-Z0-9._-]+$`)
	versionRangePattern = regexp.MustCompile(`[\^~<>=*\s]`)
	sha256Pattern       = regexp.MustCompile(`^[a-f0-9]{64}$`)
	httpURLPattern      = regexp.MustCompile(`^https?://[^\s]+$`)
)

// ServerJSON is a server.json document targeting schema 2025-12-11.
type ServerJSON struct {
	Schema      string         `json:"$schema,omitempty"`
	Meta        map[string]any `json:"_meta,omitempty"`
	Name        string         `json:"name"`
	Title       string         `json:"title,omitempty"`
	Description string         `json:"description"`
	Version     string         `json:"version"`
	WebsiteURL  string         `json:"websiteUrl,omitempty"`
	Repository  *Repository    `json:"repository,omitempty"`
	Icons       []Icon         `json:"icons,omitempty"`
	Packages    []Package      `json:"packages,omitempty"`
	Remotes     []Transport    `json:"remotes,omitempty"`
}

// Repository is the source repository metadata of a server.
type Repository struct {
	URL       string `json:"url"`
	Source    string `json:"source"`
	ID        string `json:"id,omitempty"`
	Subfolder string `json:"subfolder,omitempty"`
}

// Icon is an optionally sized icon for user interfaces.
type Icon struct {
	Src      string   `json:"src"`
	MimeType string   `json:"mimeType,omitempty"`
	Sizes    []string `json:"sizes,omitempty"`
	Theme    string   `json:"theme,omitempty"`
}

// Package is one entry of packages[], describing how to obtain and run the server.
type Package struct {
	RegistryType    string    `json:"registryType"`
	RegistryBaseURL string    `json:"registryBaseUrl,omitempty"`
	Identifier      string    `json:"identifier"`
	Version         string    `json:"version,omitempty"`
	FileSHA256      string    `json:"fileSha256,omitempty"`
	RuntimeHint     string    `json:"runtimeHint,omitempty"`
	Transport       Transport `json:"transport"`
}

// Transport is the transport discriminator used by packages[].transport and remotes[]. URL is required for the
// streamable-http and sse types.
type Transport struct {
	Type    string          `json:"type"`
	URL     string          `json:"url,omitempty"`
	Headers []KeyValueInput `json:"headers,omitempty"`
}

// KeyValueInput is a header or environment variable definition.
type KeyValueInput struct {
	Name     string `json:"name"`
	Value    string `json:"value,omitempty"`
	IsSecret bool   `json:"isSecret,omitempty"`
}

// ParseServerJSON decodes a server.json document and validates it against schema 2025-12-11.
func ParseServerJSON(data []byte) (*ServerJSON, error) {
	var doc ServerJSON
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("mcp: parse server.json: %w", err)
	}

	if err := doc.Validate(); err != nil {
		return nil, fmt.Errorf("mcp: invalid server.json: %w", err)
	}

	return &doc, nil
}

// Validate checks the required fields and enums of the document. Every problem is reported, not just the first.
func (s *ServerJSON) Validate() error {
	var errs []error
	if s.Schema != "" && s.Schema != ServerSchemaURL {
		errs = append(errs, fmt.Errorf("$schema %q is not the supported schema %s", s.Schema, ServerSchemaURL))
	}

	if !serverNamePattern.MatchString(s.Name) || len(s.Name) < minServerNameLength || len(s.Name) > maxServerNameLength {
		errs = append(errs, fmt.Errorf("name %q must match %s and be %d to %d characters",
			s.Name, serverNamePattern, minServerNameLength, maxServerNameLength))
	}

	if hasDotSegment(s.Name) {
		errs = append(errs, fmt.Errorf("name %q must not contain a . or .. segment", s.Name))
	}

	if n := utf8.RuneCountInString(s.Description); n < 1 || n > maxDescriptionLength {
		errs = append(errs, fmt.Errorf("description must be 1 to %d characters", maxDescriptionLength))
	}

	if utf8.RuneCountInString(s.Title) > maxTitleLength {
		errs = append(errs, fmt.Errorf("title must be at most %d characters", maxTitleLength))
	}

	if err := validateVersion(s.Version); err != nil {
		errs = append(errs, err)
	}

	if s.Repository != nil {
		errs = append(errs, validateRepository(*s.Repository))
	}

	for i, icon := range s.Icons {
		if icon.Src == "" || len(icon.Src) > maxIconSourceLength {
			errs = append(errs, fmt.Errorf("icons[%d]: src is required and must be at most %d characters",
				i, maxIconSourceLength))
		}
	}

	for i, pkg := range s.Packages {
		if err := pkg.validate(); err != nil {
			errs = append(errs, fmt.Errorf("packages[%d]: %w", i, err))
		}
	}

	for i, remote := range s.Remotes {
		if remote.Type != TransportStreamableHTTP && remote.Type != TransportSSE {
			errs = append(errs, fmt.Errorf("remotes[%d]: type %q must be %q or %q",
				i, remote.Type, TransportStreamableHTTP, TransportSSE))
			continue
		}

		if err := remote.validateURL(); err != nil {
			errs = append(errs, fmt.Errorf("remotes[%d]: %w", i, err))
		}
	}

	return errors.Join(errs...)
}

// validate checks one packages[] entry.
func (p Package) validate() error {
	var errs []error
	if !validRegistryType(p.RegistryType) {
		errs = append(errs, fmt.Errorf("registryType %q must be one of npm, pypi, oci, nuget, mcpb", p.RegistryType))
	}

	if p.Identifier == "" || len(p.Identifier) > maxIdentifierLength {
		errs = append(errs, fmt.Errorf("identifier is required and must be at most %d characters", maxIdentifierLength))
	}

	if p.RegistryBaseURL != "" && !httpURLPattern.MatchString(p.RegistryBaseURL) {
		errs = append(errs, fmt.Errorf("registryBaseUrl %q must be an http(s) URL", p.RegistryBaseURL))
	}

	if p.FileSHA256 != "" && !sha256Pattern.MatchString(p.FileSHA256) {
		errs = append(errs, errors.New("fileSha256 must be 64 lowercase hex characters"))
	}

	switch p.Transport.Type {
	case TransportStdio:
	case TransportStreamableHTTP, TransportSSE:
		if err := p.Transport.validateURL(); err != nil {
			errs = append(errs, err)
		}
	default:
		errs = append(errs, fmt.Errorf("transport type %q must be %q, %q, or %q",
			p.Transport.Type, TransportStdio, TransportStreamableHTTP, TransportSSE))
	}

	return errors.Join(errs...)
}

// validateURL checks the url of an HTTP or SSE transport.
func (t Transport) validateURL() error {
	if !httpURLPattern.MatchString(t.URL) {
		return fmt.Errorf("transport %q url %q must be an http(s) URL", t.Type, t.URL)
	}

	return nil
}

// validateVersion rejects empty, oversized, and range-style versions. The schema forbids ranges such as ^1.2.3.
func validateVersion(version string) error {
	switch {
	case version == "":
		return errors.New("version is required")
	case len(version) > maxVersionLength:
		return fmt.Errorf("version must be at most %d characters", maxVersionLength)
	case versionRangePattern.MatchString(version):
		return fmt.Errorf("version %q must be a single version, not a range", version)
	}

	return nil
}

// validateRepository checks the required repository fields.
func validateRepository(repo Repository) error {
	if repo.URL == "" || repo.Source == "" || len(repo.URL) > maxRepositoryLength {
		return errors.New("repository requires url and source")
	}

	return nil
}

// hasDotSegment reports whether any slash-separated segment of a server name is . or .., which would traverse a path.
func hasDotSegment(name string) bool {
	for _, segment := range strings.Split(name, "/") {
		if segment == "." || segment == ".." {
			return true
		}
	}

	return false
}

// validRegistryType reports whether value is one of the registry types this package accepts.
func validRegistryType(value string) bool {
	switch value {
	case RegistryTypeNPM, RegistryTypePyPI, RegistryTypeOCI, RegistryTypeNuGet, RegistryTypeMCPB:
		return true
	}

	return false
}
