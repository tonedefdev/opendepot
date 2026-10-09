package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
)

const (
	targetCopilot  = "copilot"
	targetClaude   = "claude"
	targetAgentsMD = "agents-md"
	targetCustom   = "path"
	agentsMDFile   = "AGENTS.md"

	maxArchiveBytes  = 256 << 20
	maxMetadataBytes = 16 << 20
)

// sourceAddress is a parsed host/namespace/name source.
type sourceAddress struct {
	host      string
	namespace string
	name      string
}

func parseSource(source string) (sourceAddress, error) {
	parts := strings.Split(source, "/")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return sourceAddress{}, fmt.Errorf("source %q must be host/namespace/name", source)
	}

	return sourceAddress{host: parts[0], namespace: parts[1], name: parts[2]}, nil
}

type discovery struct {
	AgentsV1 string     `json:"agents.v1"`
	LoginV1  *loginInfo `json:"login.v1,omitempty"`
}

type loginInfo struct {
	Client     string   `json:"client"`
	GrantTypes []string `json:"grant_types"`
	Authz      string   `json:"authz"`
	Token      string   `json:"token"`
	Scopes     []string `json:"scopes"`
	Ports      []int    `json:"ports"`
}

type agentVersionsResponse struct {
	Versions []agentVersion `json:"versions"`
}

type signingKey struct {
	KeyID      string `json:"key_id"`
	ASCIIArmor string `json:"ascii_armor"`
}

type agentDownload struct {
	Protocols           []string `json:"protocols"`
	Kind                string   `json:"kind"`
	Name                string   `json:"name"`
	Version             string   `json:"version"`
	Yanked              bool     `json:"yanked"`
	Filename            string   `json:"filename"`
	DownloadURL         string   `json:"download_url"`
	Shasum              string   `json:"shasum"`
	ShasumsURL          string   `json:"shasums_url"`
	ShasumsSignatureURL string   `json:"shasums_signature_url"`
	SigningKeys         *struct {
		GPGPublicKeys []signingKey `json:"gpg_public_keys"`
	} `json:"signing_keys"`
	Assessment *opendepotv1alpha1.JevAssessment `json:"assessment,omitempty"`
}

// registry is a discovered agents.v1 endpoint on one host.
type registry struct {
	app  *app
	host string
	base *url.URL
}

// discover fetches the service discovery document for host.
func (a *app) discover(ctx context.Context, host string) (discovery, error) {
	var d discovery
	root := a.rootURL(host, "/.well-known/terraform.json")
	body, err := a.get(ctx, root, "")
	if err != nil {
		return d, fmt.Errorf("discover %s: %w", host, err)
	}

	if err := json.Unmarshal(body, &d); err != nil {
		return d, fmt.Errorf("discover %s: %w", host, err)
	}

	return d, nil
}

func (a *app) rootURL(host, p string) string {
	return (&url.URL{Scheme: a.scheme, Host: host, Path: p}).String()
}

// openRegistry discovers the agents.v1 base URL for host.
func (a *app) openRegistry(ctx context.Context, host string) (*registry, error) {
	d, err := a.discover(ctx, host)
	if err != nil {
		return nil, err
	}

	if d.AgentsV1 == "" {
		return nil, fmt.Errorf("registry %s does not advertise agents.v1", host)
	}

	ref, err := url.Parse(d.AgentsV1)
	if err != nil {
		return nil, fmt.Errorf("registry %s: invalid agents.v1 URL: %w", host, err)
	}

	base, err := url.Parse(a.rootURL(host, "/"))
	if err != nil {
		return nil, err
	}

	base = base.ResolveReference(ref)
	if !strings.HasSuffix(base.Path, "/") {
		base.Path += "/"
	}

	return &registry{app: a, host: host, base: base}, nil
}

// resolve resolves a URL returned by the registry against its agents.v1 base.
func (r *registry) resolve(raw string) (string, error) {
	ref, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid registry URL %q: %w", raw, err)
	}

	return r.base.ResolveReference(ref).String(), nil
}

func (r *registry) versions(ctx context.Context, src sourceAddress, kind entryKind) ([]agentVersion, error) {
	u := r.base.JoinPath(src.namespace, kind.plural(), src.name, "versions").String()
	body, err := r.app.get(ctx, u, r.host)
	if err != nil {
		return nil, fmt.Errorf("list versions of %s: %w", src.name, err)
	}

	var resp agentVersionsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("list versions of %s: %w", src.name, err)
	}

	return resp.Versions, nil
}

func (r *registry) download(ctx context.Context, src sourceAddress, kind entryKind, ver string) (agentDownload, error) {
	var meta agentDownload
	u := r.base.JoinPath(src.namespace, kind.plural(), src.name, ver, "download").String()
	body, err := r.app.get(ctx, u, r.host)
	if err != nil {
		return meta, fmt.Errorf("download metadata for %s@%s: %w", src.name, ver, err)
	}

	if err := json.Unmarshal(body, &meta); err != nil {
		return meta, fmt.Errorf("download metadata for %s@%s: %w", src.name, ver, err)
	}

	return meta, nil
}

// get fetches a registry URL. The bearer token is attached only when the URL is on the
// registry host, and authHost is empty for unauthenticated fetches.
func (a *app) get(ctx context.Context, rawURL, authHost string) ([]byte, error) {
	return a.fetch(ctx, rawURL, authHost, maxMetadataBytes)
}

func (a *app) fetch(ctx context.Context, rawURL, authHost string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}

	if authHost != "" && req.URL.Host == authHost {
		if token := a.token(authHost); token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
	}

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("GET %s: %s", req.URL.Redacted(), resp.Status)
	}

	return io.ReadAll(io.LimitReader(resp.Body, limit))
}

// findSHA256SUM returns the signed archive digest for filename from a SHA256SUMS file.
func findSHA256SUM(sums []byte, filename string) (string, error) {
	for _, line := range strings.Split(string(sums), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == filename {
			return strings.ToLower(fields[0]), nil
		}
	}

	return "", fmt.Errorf("SHA256SUMS has no entry for %s", filename)
}

// checkProtocols refuses a registry that does not speak protocol major version 1.
func checkProtocols(protocols []string) error {
	if len(protocols) == 0 {
		return fmt.Errorf("registry advertises no protocols")
	}

	for _, p := range protocols {
		if strings.SplitN(p, ".", 2)[0] != "1" {
			return fmt.Errorf("unsupported agents protocol %q", p)
		}
	}

	return nil
}

// registries caches one discovered registry per host for the lifetime of a command.
type registries struct {
	app   *app
	byKey map[string]*registry
}

func (a *app) newRegistries() *registries {
	return &registries{app: a, byKey: map[string]*registry{}}
}

func (rs *registries) get(ctx context.Context, host string) (*registry, error) {
	if r, ok := rs.byKey[host]; ok {
		return r, nil
	}

	r, err := rs.app.openRegistry(ctx, host)
	if err != nil {
		return nil, err
	}

	rs.byKey[host] = r
	return r, nil
}
