package oci

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"oras.land/oras-go/v2/registry/remote/auth"
)

// dockerConfigJSON is the subset of a kubernetes.io/dockerconfigjson document that OpenDepot reads.
type dockerConfigJSON struct {
	Auths map[string]dockerAuthEntry `json:"auths"`
}

type dockerAuthEntry struct {
	Auth          string `json:"auth,omitempty"`
	Username      string `json:"username,omitempty"`
	Password      string `json:"password,omitempty"`
	IdentityToken string `json:"identitytoken,omitempty"`
}

// parseDockerConfigJSON builds a CredentialFunc from the bytes of a dockerconfigjson document. Everything stays in
// memory: no temporary file is written and no secret is included in an error or a log line. An empty document
// yields a CredentialFunc that always returns an empty credential (anonymous access).
func parseDockerConfigJSON(data []byte) (auth.CredentialFunc, error) {
	if len(data) == 0 {
		return func(context.Context, string) (auth.Credential, error) {
			return auth.EmptyCredential, nil
		}, nil
	}

	var cfg dockerConfigJSON
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, errors.New("oci: dockerconfigjson is not valid JSON")
	}

	creds := make(map[string]auth.Credential, len(cfg.Auths))
	for server, entry := range cfg.Auths {
		host, err := registryHostport(server)
		if err != nil {
			return nil, err
		}

		cred, err := entry.credential()
		if err != nil {
			return nil, fmt.Errorf("oci: dockerconfigjson entry for %q: %w", host, err)
		}

		if cred == auth.EmptyCredential {
			continue
		}

		creds[host] = cred
	}

	return func(_ context.Context, hostport string) (auth.Credential, error) {
		if cred, ok := creds[strings.ToLower(hostport)]; ok {
			return cred, nil
		}

		return auth.EmptyCredential, nil
	}, nil
}

// registryHostport normalizes a dockerconfigjson key such as "https://index.docker.io/v1/" to the host[:port] that
// the registry client asks for. Docker Hub is served from registry-1.docker.io.
func registryHostport(server string) (string, error) {
	host := strings.TrimSpace(strings.ToLower(server))
	host = strings.TrimPrefix(host, "https://")
	host = strings.TrimPrefix(host, "http://")
	if idx := strings.Index(host, "/"); idx >= 0 {
		host = host[:idx]
	}

	if host == "" {
		return "", errors.New("oci: dockerconfigjson contains an empty registry key")
	}

	switch host {
	case "index.docker.io", "docker.io":
		return "registry-1.docker.io", nil
	}

	return host, nil
}

func (e dockerAuthEntry) credential() (auth.Credential, error) {
	if e.IdentityToken != "" {
		return auth.Credential{RefreshToken: e.IdentityToken}, nil
	}

	if e.Auth != "" {
		raw, err := base64.StdEncoding.DecodeString(e.Auth)
		if err != nil {
			return auth.Credential{}, errors.New("auth is not valid base64")
		}

		user, pass, ok := strings.Cut(string(raw), ":")
		if !ok {
			return auth.Credential{}, errors.New("auth does not contain username:password")
		}

		return auth.Credential{Username: user, Password: pass}, nil
	}

	if e.Username != "" || e.Password != "" {
		return auth.Credential{Username: e.Username, Password: e.Password}, nil
	}

	return auth.EmptyCredential, nil
}
