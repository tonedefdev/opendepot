package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
)

// maxMetadataBytes bounds a registry metadata document (an npm packument or a PyPI project JSON).
const maxMetadataBytes = 64 << 20

// maxTarballBytes bounds a downloaded npm tarball. Larger packages are rejected rather than read into memory.
const maxTarballBytes = 64 << 20

// Client fetches package metadata from allow-listed npm and PyPI registries. Every request, including each redirect
// target, is checked against the allow-list, and every connection is checked against the IP policy at dial time.
type Client struct {
	allow     *AllowList
	http      *http.Client
	plainHTTP bool
}

// NewClient returns a Client that contacts only the hosts on allow. A nil or empty allow-list denies every request.
func NewClient(allow *AllowList) *Client {
	return newClient(allow, checkDialAddr, false)
}

// newClient is the shared constructor. Tests use it to reach httptest servers on loopback over plain HTTP.
func newClient(allow *AllowList, check func(netip.Addr) error, plainHTTP bool) *Client {
	return &Client{
		allow:     allow,
		http:      newHTTPClient(allow, check, plainHTTP),
		plainHTTP: plainHTTP,
	}
}

// getJSON fetches rawURL and decodes the JSON body into dest. The response body is bounded by maxMetadataBytes.
func (c *Client) getJSON(ctx context.Context, rawURL string, dest any) error {
	body, err := c.get(ctx, rawURL, "application/json", maxMetadataBytes)
	if err != nil {
		return err
	}

	if err := json.Unmarshal(body, dest); err != nil {
		return fmt.Errorf("mcp: decode registry response: %w", err)
	}

	return nil
}

// FetchTarball downloads an npm tarball from an allow-listed https host. The body is bounded by maxTarballBytes.
func (c *Client) FetchTarball(ctx context.Context, rawURL string) ([]byte, error) {
	return c.get(ctx, rawURL, "application/octet-stream", maxTarballBytes)
}

// get fetches rawURL through the allow-listed client and returns at most limit bytes of the body. A body larger than
// limit is an error.
func (c *Client) get(ctx context.Context, rawURL, accept string, limit int64) ([]byte, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("mcp: parse registry URL: %w", err)
	}

	if err := checkURL(c.allow, u, c.plainHTTP); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("mcp: build registry request: %w", err)
	}

	req.Header.Set("Accept", accept)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("mcp: fetch %s: %w", u.Redacted(), err)
	}

	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("mcp: fetch %s: unexpected status %d", u.Redacted(), resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("mcp: read %s: %w", u.Redacted(), err)
	}

	if int64(len(body)) > limit {
		return nil, fmt.Errorf("mcp: %s exceeds %d bytes", u.Redacted(), limit)
	}

	return body, nil
}
