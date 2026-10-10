/*
Copyright 2026 Anthony Owens.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package testutils

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

const (
	gitHubAppSecretName = "opendepot-github-application-secret"
	gitHubAppIDEnv      = "OPENDEPOT_GH_APP_ID"
	gitHubInstallIDEnv  = "OPENDEPOT_GH_INSTALL_ID"
	gitHubPrivateKeyEnv = "OPENDEPOT_GH_PRIVATE_KEY"
)

// EnsureGitHubAppSecret creates or updates the GitHub App Secret read by the
// controllers so e2e GitHub calls use an authenticated client. The app ID,
// install ID, and base64-encoded private key are read from the environment.
// A missing or invalid variable returns an error that names the variable only.
func EnsureGitHubAppSecret(namespace string) error {
	manifest, err := buildGitHubAppSecretManifest(namespace, os.Getenv)
	if err != nil {
		return err
	}

	applyCmd := exec.Command("kubectl", "apply", "-f", "-")
	applyCmd.Stdin = strings.NewReader(string(manifest))
	if _, err := Run(applyCmd); err != nil {
		return fmt.Errorf("apply GitHub App Secret: %w", err)
	}

	return nil
}

// buildGitHubAppSecretManifest renders the GitHub App Secret as a JSON manifest.
// The private key is stored as the base64 text from the environment, which the
// controller decodes once, so the manifest value is that text encoded once more.
func buildGitHubAppSecretManifest(namespace string, getenv func(string) string) ([]byte, error) {
	appID := strings.TrimSpace(getenv(gitHubAppIDEnv))
	if appID == "" {
		return nil, fmt.Errorf("%s is not set", gitHubAppIDEnv)
	}

	if _, err := strconv.ParseInt(appID, 10, 64); err != nil {
		return nil, fmt.Errorf("%s is not a valid integer", gitHubAppIDEnv)
	}

	installID := strings.TrimSpace(getenv(gitHubInstallIDEnv))
	if installID == "" {
		return nil, fmt.Errorf("%s is not set", gitHubInstallIDEnv)
	}

	if _, err := strconv.ParseInt(installID, 10, 64); err != nil {
		return nil, fmt.Errorf("%s is not a valid integer", gitHubInstallIDEnv)
	}

	privateKey := strings.TrimSpace(getenv(gitHubPrivateKeyEnv))
	if privateKey == "" {
		return nil, fmt.Errorf("%s is not set", gitHubPrivateKeyEnv)
	}

	pem, err := base64.StdEncoding.DecodeString(privateKey)
	if err != nil {
		return nil, fmt.Errorf("%s is not valid base64: %w", gitHubPrivateKeyEnv, err)
	}

	if !strings.Contains(string(pem), "-----BEGIN RSA PRIVATE KEY-----") {
		return nil, errors.New(gitHubPrivateKeyEnv + " does not decode to an RSA private key PEM")
	}

	manifest := map[string]any{
		"apiVersion": "v1",
		"kind":       "Secret",
		"metadata": map[string]string{
			"name":      gitHubAppSecretName,
			"namespace": namespace,
		},
		"type": "Opaque",
		"data": map[string]string{
			"githubAppID":      base64.StdEncoding.EncodeToString([]byte(appID)),
			"githubInstallID":  base64.StdEncoding.EncodeToString([]byte(installID)),
			"githubPrivateKey": base64.StdEncoding.EncodeToString([]byte(privateKey)),
		},
	}

	return json.Marshal(manifest)
}
