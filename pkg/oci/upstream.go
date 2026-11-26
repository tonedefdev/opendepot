package oci

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"strings"

	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
)

// Upstream is an allow-listed, SSRF-safe client for remote OCI registries. Every request, including redirects and
// token-service requests, is checked against the allow-list, and every connection is checked against the IP policy.
type Upstream struct {
	allow      *AllowList
	client     *http.Client
	credential auth.CredentialFunc
	plainHTTP  bool
}

// upstreamOptions holds settings that only in-package tests may change. They let httptest servers on loopback stand
// in for registries. Production code always uses the zero value, which keeps the policy fail-closed.
type upstreamOptions struct {
	plainHTTP     bool
	allowLoopback bool
}

// NewUpstream returns an Upstream that may contact only the hosts in allowedHosts. An empty allowedHosts denies
// every request. dockerConfigJSON is an optional kubernetes.io/dockerconfigjson document. It is parsed in memory and
// is never written to disk. Pass nil for anonymous access.
func NewUpstream(allowedHosts []string, dockerConfigJSON []byte) (*Upstream, error) {
	return newUpstream(allowedHosts, dockerConfigJSON, upstreamOptions{})
}

func newUpstream(allowedHosts []string, dockerConfigJSON []byte, opts upstreamOptions) (*Upstream, error) {
	allow, err := NewAllowList(allowedHosts)
	if err != nil {
		return nil, err
	}

	cred, err := parseDockerConfigJSON(dockerConfigJSON)
	if err != nil {
		return nil, err
	}

	check := checkDialAddr
	if opts.allowLoopback {
		check = allowLoopbackOnly
	}

	return &Upstream{
		allow:      allow,
		client:     newUpstreamHTTPClient(allow, check, opts.plainHTTP),
		credential: cred,
		plainHTTP:  opts.plainHTTP,
	}, nil
}

// allowLoopbackOnly is a test-only IP policy. It admits loopback addresses and still rejects every other unsafe
// address class.
func allowLoopbackOnly(addr netip.Addr) error {
	if addr.Unmap().IsLoopback() {
		return nil
	}

	return checkDialAddr(addr)
}

// ResolvedManifest is a manifest or index fetched from an upstream registry, with the platform manifests selected
// from it.
type ResolvedManifest struct {
	// Descriptor describes the root manifest or index as the registry served it.
	Descriptor ocispec.Descriptor
	// Content is the root manifest or index, verified against Descriptor.Digest.
	Content []byte
	// Manifests are the manifests to mirror. For an index they are the entries selected by platform, in index order.
	// For a single manifest the list holds that manifest.
	Manifests []PlatformManifest
}

// PlatformManifest is one selected image manifest, verified against its descriptor.
type PlatformManifest struct {
	Descriptor ocispec.Descriptor
	Content    []byte
	Manifest   ocispec.Manifest
}

// ListTags lists every tag in repository, where repository is host/path with no tag or digest.
func (u *Upstream) ListTags(ctx context.Context, repository string) ([]string, error) {
	repo, err := u.repository(repository)
	if err != nil {
		return nil, err
	}

	var tags []string
	if err := repo.Tags(ctx, "", func(page []string) error {
		tags = append(tags, page...)
		return nil
	}); err != nil {
		return nil, fmt.Errorf("oci: list tags for %s: %w", repository, err)
	}

	return tags, nil
}

// Resolve returns the descriptor that reference (a tag or a digest) points to, without downloading the manifest.
func (u *Upstream) Resolve(ctx context.Context, repository, reference string) (ocispec.Descriptor, error) {
	repo, err := u.repository(repository)
	if err != nil {
		return ocispec.Descriptor{}, err
	}

	desc, err := repo.Resolve(ctx, reference)
	if err != nil {
		return ocispec.Descriptor{}, fmt.Errorf("oci: resolve %s:%s: %w", repository, reference, err)
	}

	return desc, nil
}

// FetchManifest fetches the manifest or index that reference (a tag or a digest) points to and verifies its digest.
// When the root is an index, platforms selects the entries to mirror. Each value is os/arch or os/arch/variant, such
// as linux/amd64 or linux/arm/v7. An empty platforms list selects every entry except attestation entries. Platform
// filtering applies to indexes only. A single manifest is returned as is.
func (u *Upstream) FetchManifest(
	ctx context.Context,
	repository, reference string,
	platforms []string,
) (*ResolvedManifest, error) {
	repo, err := u.repository(repository)
	if err != nil {
		return nil, err
	}

	desc, rc, err := repo.FetchReference(ctx, reference)
	if err != nil {
		return nil, fmt.Errorf("oci: fetch %s:%s: %w", repository, reference, err)
	}

	raw, err := readVerified(rc, desc)
	if err != nil {
		return nil, fmt.Errorf("oci: fetch %s:%s: %w", repository, reference, err)
	}

	result := &ResolvedManifest{Descriptor: desc, Content: raw}
	switch desc.MediaType {
	case ocispec.MediaTypeImageIndex:
		var index ocispec.Index
		if err := json.Unmarshal(raw, &index); err != nil {
			return nil, fmt.Errorf("oci: decode index %s: %w", desc.Digest, err)
		}

		selected, err := selectPlatformManifests(index, platforms)
		if err != nil {
			return nil, fmt.Errorf("oci: %s:%s: %w", repository, reference, err)
		}

		for _, child := range selected {
			if child.MediaType == ocispec.MediaTypeImageIndex {
				return nil, fmt.Errorf("oci: nested index %s is not supported", child.Digest)
			}

			pm, err := fetchPlatformManifest(ctx, repo, child)
			if err != nil {
				return nil, err
			}

			result.Manifests = append(result.Manifests, pm)
		}
	default:
		var manifest ocispec.Manifest
		if err := json.Unmarshal(raw, &manifest); err != nil {
			return nil, fmt.Errorf("oci: decode manifest %s: %w", desc.Digest, err)
		}

		result.Manifests = []PlatformManifest{{Descriptor: desc, Content: raw, Manifest: manifest}}
	}

	return result, nil
}

func fetchPlatformManifest(
	ctx context.Context,
	repo *remote.Repository,
	desc ocispec.Descriptor,
) (PlatformManifest, error) {
	rc, err := repo.Fetch(ctx, desc)
	if err != nil {
		return PlatformManifest{}, fmt.Errorf("oci: fetch manifest %s: %w", desc.Digest, err)
	}

	raw, err := readVerified(rc, desc)
	if err != nil {
		return PlatformManifest{}, fmt.Errorf("oci: fetch manifest %s: %w", desc.Digest, err)
	}

	var manifest ocispec.Manifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return PlatformManifest{}, fmt.Errorf("oci: decode manifest %s: %w", desc.Digest, err)
	}

	return PlatformManifest{Descriptor: desc, Content: raw, Manifest: manifest}, nil
}

// FetchBlob streams the blob described by desc from repository. The returned reader fails with an error when the
// bytes do not match desc.Digest or desc.Size, so a caller can only obtain verified content.
func (u *Upstream) FetchBlob(ctx context.Context, repository string, desc ocispec.Descriptor) (io.ReadCloser, error) {
	repo, err := u.repository(repository)
	if err != nil {
		return nil, err
	}

	rc, err := repo.Fetch(ctx, desc)
	if err != nil {
		return nil, fmt.Errorf("oci: fetch blob %s: %w", desc.Digest, err)
	}

	return struct {
		io.Reader
		io.Closer
	}{Reader: content.NewVerifyReader(rc, desc), Closer: rc}, nil
}

// Referrers lists the OCI 1.1 referrers of desc in repository. An empty artifactType returns every referrer.
func (u *Upstream) Referrers(
	ctx context.Context,
	repository string,
	desc ocispec.Descriptor,
	artifactType string,
) ([]ocispec.Descriptor, error) {
	repo, err := u.repository(repository)
	if err != nil {
		return nil, err
	}

	var referrers []ocispec.Descriptor
	if err := repo.Referrers(ctx, desc, artifactType, func(page []ocispec.Descriptor) error {
		referrers = append(referrers, page...)
		return nil
	}); err != nil {
		return nil, fmt.Errorf("oci: list referrers of %s: %w", desc.Digest, err)
	}

	return referrers, nil
}

// repository builds an oras repository for host/path that uses the guarded client. It rejects references that carry
// a tag or digest and any host that is not on the allow-list before a request is made.
func (u *Upstream) repository(name string) (*remote.Repository, error) {
	repo, err := remote.NewRepository(name)
	if err != nil {
		return nil, fmt.Errorf("oci: invalid repository %q: %w", name, err)
	}

	if repo.Reference.Reference != "" {
		return nil, fmt.Errorf("oci: repository %q must not include a tag or digest", name)
	}

	scheme := "https"
	if u.plainHTTP {
		scheme = "http"
	}

	if err := checkURL(u.allow, &url.URL{Scheme: scheme, Host: repo.Reference.Host()}, u.plainHTTP); err != nil {
		return nil, err
	}

	repo.PlainHTTP = u.plainHTTP
	repo.Client = &auth.Client{
		Client:     u.client,
		Cache:      auth.NewCache(),
		Credential: u.credential,
	}

	return repo, nil
}

// readVerified reads all of rc, closes it, and checks the bytes against desc.
func readVerified(rc io.ReadCloser, desc ocispec.Descriptor) ([]byte, error) {
	defer closeReader(rc)
	return content.ReadAll(rc, desc)
}

type platformSpec struct {
	os      string
	arch    string
	variant string
}

func parsePlatformSpec(s string) (platformSpec, error) {
	parts := strings.Split(strings.TrimSpace(s), "/")
	if len(parts) < 2 || len(parts) > 3 {
		return platformSpec{}, fmt.Errorf("platform %q must be os/arch or os/arch/variant", s)
	}

	for _, p := range parts {
		if p == "" {
			return platformSpec{}, fmt.Errorf("platform %q has an empty component", s)
		}
	}

	spec := platformSpec{os: parts[0], arch: parts[1]}
	if len(parts) == 3 {
		spec.variant = parts[2]
	}

	return spec, nil
}

// selectPlatformManifests returns the index entries that match platforms, in index order and without duplicates.
func selectPlatformManifests(index ocispec.Index, platforms []string) ([]ocispec.Descriptor, error) {
	if len(platforms) == 0 {
		var all []ocispec.Descriptor
		for _, desc := range index.Manifests {
			if isAttestation(desc) {
				continue
			}

			all = append(all, desc)
		}

		if len(all) == 0 {
			return nil, errors.New("index has no manifests")
		}

		return all, nil
	}

	wants := make([]platformSpec, 0, len(platforms))
	for _, p := range platforms {
		spec, err := parsePlatformSpec(p)
		if err != nil {
			return nil, err
		}

		wants = append(wants, spec)
	}

	var selected []ocispec.Descriptor
	seen := map[digest.Digest]bool{}
	for _, desc := range index.Manifests {
		if desc.Platform == nil || isAttestation(desc) || seen[desc.Digest] {
			continue
		}

		for _, want := range wants {
			if matchesPlatform(desc.Platform, want) {
				seen[desc.Digest] = true
				selected = append(selected, desc)
				break
			}
		}
	}

	if len(selected) == 0 {
		return nil, fmt.Errorf("no index entry matches platforms %v", platforms)
	}

	return selected, nil
}

func matchesPlatform(p *ocispec.Platform, want platformSpec) bool {
	if p.OS != want.os || p.Architecture != want.arch {
		return false
	}

	return want.variant == "" || p.Variant == want.variant
}

// isAttestation reports whether an index entry is a build attestation rather than a runnable platform image.
func isAttestation(desc ocispec.Descriptor) bool {
	if desc.Platform != nil && desc.Platform.OS == "unknown" && desc.Platform.Architecture == "unknown" {
		return true
	}

	return desc.Annotations["vnd.docker.reference.type"] == "attestation-manifest"
}
