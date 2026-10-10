package testutils

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildGitHubAppSecretManifest(t *testing.T) {
	pem := "-----BEGIN RSA PRIVATE KEY-----\nMIIBOgIBAAJBAKj\n-----END RSA PRIVATE KEY-----\n"
	keyB64 := base64.StdEncoding.EncodeToString([]byte(pem))
	env := map[string]string{
		gitHubAppIDEnv:      "123456",
		gitHubInstallIDEnv:  "7890",
		gitHubPrivateKeyEnv: keyB64,
	}
	getenv := func(k string) string { return env[k] }

	t.Run("round-trips the private key text unchanged", func(t *testing.T) {
		manifest, err := buildGitHubAppSecretManifest("opendepot-system", getenv)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		var secret struct {
			Metadata struct {
				Name      string `json:"name"`
				Namespace string `json:"namespace"`
			} `json:"metadata"`
			Data map[string]string `json:"data"`
		}
		if err := json.Unmarshal(manifest, &secret); err != nil {
			t.Fatalf("manifest is not valid JSON: %v", err)
		}

		if secret.Metadata.Name != gitHubAppSecretName || secret.Metadata.Namespace != "opendepot-system" {
			t.Fatalf("unexpected metadata: %+v", secret.Metadata)
		}

		// Kubernetes decodes data once, so the stored bytes must equal the env text.
		stored, err := base64.StdEncoding.DecodeString(secret.Data["githubPrivateKey"])
		if err != nil {
			t.Fatalf("githubPrivateKey is not base64: %v", err)
		}
		if string(stored) != keyB64 {
			t.Fatalf("stored private key does not match the environment value")
		}
	})

	t.Run("missing variable names the variable without its value", func(t *testing.T) {
		missing := func(k string) string {
			if k == gitHubInstallIDEnv {
				return ""
			}

			return env[k]
		}

		_, err := buildGitHubAppSecretManifest("opendepot-system", missing)
		if err == nil {
			t.Fatal("expected an error for a missing variable")
		}

		if !strings.Contains(err.Error(), gitHubInstallIDEnv) {
			t.Fatalf("error does not name the missing variable: %v", err)
		}
	})

	t.Run("invalid private key does not echo the value", func(t *testing.T) {
		secretValue := "not-base64-!!"
		bad := func(k string) string {
			if k == gitHubPrivateKeyEnv {
				return secretValue
			}

			return env[k]
		}

		_, err := buildGitHubAppSecretManifest("opendepot-system", bad)
		if err == nil {
			t.Fatal("expected an error for an invalid private key")
		}

		if strings.Contains(err.Error(), secretValue) {
			t.Fatalf("error leaks the environment value: %v", err)
		}
	})
}
