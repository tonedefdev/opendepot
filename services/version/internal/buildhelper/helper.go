// Package buildhelper implements the fetch and upload helper modes of the version-controller image. The build Job runs
// them as its restricted init and main containers, with --build-helper=fetch or --build-helper=upload. Both modes talk
// to the build broker in the version controller and authenticate with the one-time token in their token file. They
// never read a token from the environment.
package buildhelper

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/opencontainers/go-digest"

	"github.com/tonedefdev/opendepot/pkg/ocibuild"
)

// Helper modes, passed as --build-helper.
const (
	ModeFetch  = "fetch"
	ModeUpload = "upload"
)

// DefaultTerminationLogPath is where the kubelet reads a container's termination message.
const DefaultTerminationLogPath = "/dev/termination-log"

const (
	// maxBrokerResponseBytes bounds a broker response body that is not the context archive. The broker's responses are
	// small.
	maxBrokerResponseBytes int64 = 4 << 10
	// maxTerminationMessageBytes is the largest termination message that the kubelet keeps.
	maxTerminationMessageBytes = 4096
	// helperHTTPTimeout bounds one broker request. A blob may be large, so it is generous.
	helperHTTPTimeout = 30 * time.Minute
)

// Config holds the flags of one helper run.
type Config struct {
	// Mode is ModeFetch or ModeUpload.
	Mode string
	// BuildURL is the broker route for the build: /internal/builds/v1/{namespace}/{version}/{buildID}.
	BuildURL string
	// TokenFile is the file that holds the token for this mode. It is never read from the environment.
	TokenFile string
	// ContextDigest is the sha256 digest of the context archive. The fetch mode verifies it.
	ContextDigest string
	// Workspace is the directory that the fetch mode extracts the context into. An empty value uses
	// ocibuild.WorkspaceMountPath.
	Workspace string
	// Layout is the OCI directory layout that BuildKit wrote. The upload mode reads it.
	Layout string
	// Platform is the platform of the layout, such as linux/amd64. The upload mode reads it.
	Platform string
	// TerminationLogPath is where the upload mode writes its manifest digest. An empty value uses
	// DefaultTerminationLogPath.
	TerminationLogPath string
	// HTTPClient sends the broker requests. A nil value uses a client with a 30 minute timeout.
	HTTPClient *http.Client
}

// Run executes the helper mode in cfg. Its errors never contain the token.
func Run(ctx context.Context, cfg Config) error {
	if err := checkBuildURL(cfg.BuildURL); err != nil {
		return err
	}

	switch cfg.Mode {
	case ModeFetch:
		return fetchContext(ctx, cfg)
	case ModeUpload:
		return uploadLayout(ctx, cfg)
	default:
		return fmt.Errorf("unknown build helper mode %q: must be %s or %s", cfg.Mode, ModeFetch, ModeUpload)
	}
}

// fetchContext downloads the context archive, verifies its digest, and extracts it into the workspace.
func fetchContext(ctx context.Context, cfg Config) error {
	want, err := digest.Parse(cfg.ContextDigest)
	if err != nil || want.Algorithm() != digest.SHA256 {
		return fmt.Errorf("context digest %q must be a sha256 digest", cfg.ContextDigest)
	}

	token, err := readToken(cfg.TokenFile)
	if err != nil {
		return err
	}

	workspace := cfg.Workspace
	if workspace == "" {
		workspace = ocibuild.WorkspaceMountPath
	}

	spool, err := os.CreateTemp(workspace, ".context-*.tar.gz")
	if err != nil {
		return fmt.Errorf("create context spool: %w", err)
	}
	defer func() {
		_ = spool.Close()
		_ = os.Remove(spool.Name())
	}()

	body, err := getBroker(ctx, cfg.client(), cfg.BuildURL+"/context", token)
	if err != nil {
		return fmt.Errorf("fetch context: %w", err)
	}
	defer func() { _ = body.Close() }()

	// The archive is verified in full before anything is extracted from it.
	verifier := want.Verifier()
	n, err := io.Copy(io.MultiWriter(spool, verifier), io.LimitReader(body, ocibuild.DefaultMaxCompressedBytes+1))
	if err != nil {
		return fmt.Errorf("read context: %w", err)
	}

	if n > ocibuild.DefaultMaxCompressedBytes {
		return fmt.Errorf("context archive exceeds %d bytes", ocibuild.DefaultMaxCompressedBytes)
	}

	if !verifier.Verified() {
		return fmt.Errorf("context digest mismatch: want %s", want)
	}

	if _, err := spool.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("rewind context spool: %w", err)
	}

	if err := ocibuild.Unpack(spool, workspace, ocibuild.Limits{}); err != nil {
		return fmt.Errorf("extract context: %w", err)
	}

	return nil
}

// uploadLayout validates the OCI layout for the platform, uploads its blobs and manifest through the broker, and writes
// the manifest digest to the termination message. The ingest directory is ignored, because ReadLayout reads only the
// blobs that index.json references.
func uploadLayout(ctx context.Context, cfg Config) error {
	platform, err := ocibuild.ParsePlatform(cfg.Platform)
	if err != nil {
		return err
	}

	token, err := readToken(cfg.TokenFile)
	if err != nil {
		return err
	}

	layout, err := ocibuild.ReadLayout(cfg.Layout, platform, ocibuild.LayoutLimits{})
	if err != nil {
		return fmt.Errorf("read OCI layout: %w", err)
	}

	client := cfg.client()
	for _, blob := range layout.Blobs {
		if err := uploadBlob(ctx, client, cfg.BuildURL, token, blob); err != nil {
			return err
		}
	}

	manifestDigest, err := uploadManifest(ctx, client, cfg.BuildURL, token, platform, layout)
	if err != nil {
		return err
	}

	path := cfg.TerminationLogPath
	if path == "" {
		path = DefaultTerminationLogPath
	}

	return writeTerminationMessage(path, manifestDigest)
}

// uploadBlob streams one blob from the layout to the broker.
func uploadBlob(ctx context.Context, client *http.Client, buildURL, token string, blob ocibuild.LayoutBlob) error {
	f, err := os.Open(blob.Path)
	if err != nil {
		return fmt.Errorf("open blob %s: %w", blob.Digest, err)
	}
	defer func() { _ = f.Close() }()

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, buildURL+"/blobs/"+blob.Digest.String(), f)
	if err != nil {
		return fmt.Errorf("upload blob %s: %w", blob.Digest, err)
	}

	req.ContentLength = blob.Size
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("upload blob %s: %w", blob.Digest, err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxBrokerResponseBytes))

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("upload blob %s: broker status %s", blob.Digest, resp.Status)
	}

	return nil
}

// uploadManifest sends the platform manifest and checks that the broker stored the digest the layout declares.
func uploadManifest(
	ctx context.Context,
	client *http.Client,
	buildURL, token string,
	platform ocibuild.Platform,
	layout *ocibuild.Layout,
) (digest.Digest, error) {
	want := layout.Manifest.Digest
	req, err := http.NewRequestWithContext(ctx, http.MethodPut,
		buildURL+"/manifests/"+url.PathEscape(platform.String()), bytes.NewReader(layout.ManifestBytes))
	if err != nil {
		return "", fmt.Errorf("upload manifest: %w", err)
	}

	req.Header.Set("Content-Type", layout.Manifest.MediaType)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("upload manifest: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", fmt.Errorf("upload manifest: broker status %s", resp.Status)
	}

	var stored struct {
		ManifestDigest string `json:"manifestDigest"`
	}

	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBrokerResponseBytes)).Decode(&stored); err != nil {
		return "", fmt.Errorf("upload manifest: decode broker response: %w", err)
	}

	if stored.ManifestDigest != want.String() {
		return "", fmt.Errorf("upload manifest: broker stored %q, want %s", stored.ManifestDigest, want)
	}

	return want, nil
}

// writeTerminationMessage records the manifest digest where the kubelet reads it. The controller reads it back from
// the upload container's termination message.
func writeTerminationMessage(path string, manifestDigest digest.Digest) error {
	data, err := json.Marshal(struct {
		ManifestDigest string `json:"manifestDigest"`
	}{ManifestDigest: manifestDigest.String()})
	if err != nil {
		return fmt.Errorf("encode termination message: %w", err)
	}

	if len(data) > maxTerminationMessageBytes {
		return errors.New("termination message is too large")
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write termination message: %w", err)
	}

	return nil
}

// getBroker sends an authenticated GET and returns the body of a 200 response.
func getBroker(ctx context.Context, client *http.Client, target, token string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxBrokerResponseBytes))
		_ = resp.Body.Close()
		return nil, fmt.Errorf("broker status %s", resp.Status)
	}

	return resp.Body, nil
}

// readToken reads the token file and checks that it holds one hex-encoded build token.
func readToken(path string) (string, error) {
	if path == "" {
		return "", errors.New("token file is required")
	}

	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return "", fmt.Errorf("read token file: %w", err)
	}

	token := strings.TrimSpace(string(data))
	if len(token) != ocibuild.TokenBytes*2 {
		return "", errors.New("token file must hold one build token")
	}

	if _, err := hex.DecodeString(token); err != nil {
		return "", errors.New("token file must hold one build token")
	}

	return token, nil
}

// checkBuildURL requires the build URL to be an absolute http or https URL.
func checkBuildURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("build URL %q must be an absolute http or https URL", raw)
	}

	return nil
}

func (c Config) client() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}

	return &http.Client{Timeout: helperHTTPTimeout}
}
