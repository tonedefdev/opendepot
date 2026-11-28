package ocibuild

import (
	"fmt"
	"strings"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// supportedArchitectures maps each buildable architecture to the variants it accepts. An empty variant is always
// accepted for the architecture itself. Builds run on Linux only.
var supportedArchitectures = map[string][]string{
	"amd64":   {""},
	"arm64":   {"", "v8"},
	"arm":     {"v6", "v7"},
	"ppc64le": {""},
	"riscv64": {""},
	"s390x":   {""},
}

// Platform is a Linux build target such as linux/amd64 or linux/arm/v7.
type Platform struct {
	OS           string
	Architecture string
	Variant      string
}

// ParsePlatform parses "os/arch" or "os/arch/variant". It accepts only linux and the architectures and variants in
// supportedArchitectures.
func ParsePlatform(s string) (Platform, error) {
	parts := strings.Split(s, "/")
	if len(parts) < 2 || len(parts) > 3 {
		return Platform{}, fmt.Errorf("platform %q must be os/arch or os/arch/variant", s)
	}

	p := Platform{OS: parts[0], Architecture: parts[1]}
	if len(parts) == 3 {
		p.Variant = parts[2]
	}

	if p.OS != "linux" {
		return Platform{}, fmt.Errorf("platform %q: only linux builds are supported", s)
	}

	variants, ok := supportedArchitectures[p.Architecture]
	if !ok {
		return Platform{}, fmt.Errorf("platform %q: unsupported architecture %q", s, p.Architecture)
	}

	for _, v := range variants {
		if v == p.Variant {
			return p, nil
		}
	}

	return Platform{}, fmt.Errorf("platform %q: unsupported variant %q", s, p.Variant)
}

// String returns the platform as os/arch, or os/arch/variant when a variant is set.
func (p Platform) String() string {
	if p.Variant == "" {
		return p.OS + "/" + p.Architecture
	}

	return p.OS + "/" + p.Architecture + "/" + p.Variant
}

// JobSuffix returns the DNS-1123 fragment that names this platform's Job, such as amd64 or arm-v7.
func (p Platform) JobSuffix() string {
	if p.Variant == "" {
		return p.Architecture
	}

	return p.Architecture + "-" + p.Variant
}

// key identifies the platform for duplicate checks. An arm64 platform with no variant and one with variant v8
// share a key.
func (p Platform) key() string {
	return p.OS + "/" + p.Architecture + "/" + canonicalVariant(p.Architecture, p.Variant)
}

// OCI returns the platform as an OCI image-spec descriptor platform.
func (p Platform) OCI() *ocispec.Platform {
	return &ocispec.Platform{
		OS:           p.OS,
		Architecture: p.Architecture,
		Variant:      p.Variant,
	}
}

// matches reports whether an image config's os, architecture, and variant describe this platform. An arm64 image
// with no variant and one with variant v8 are the same platform.
func (p Platform) matches(goos, arch, variant string) bool {
	return p.OS == goos && p.Architecture == arch && canonicalVariant(arch, p.Variant) == canonicalVariant(arch, variant)
}

func canonicalVariant(arch, variant string) string {
	if arch == "arm64" && variant == "" {
		return "v8"
	}

	return variant
}
