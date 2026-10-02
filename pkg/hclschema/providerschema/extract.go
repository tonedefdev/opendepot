/*
Copyright 2026 Tony Owens.

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

package providerschema

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// maxSchemaBytes caps the raw schema JSON a provider may emit. The largest published
// providers produce tens of megabytes; anything beyond this is treated as hostile.
const maxSchemaBytes = 512 << 20

// ExtractInput describes a single provider schema extraction.
type ExtractInput struct {
	// ProviderZipPath is the downloaded provider archive on local disk.
	ProviderZipPath string
	// Namespace is the provider's registry namespace, e.g. "hashicorp".
	Namespace string
	// Name is the provider's registry name, e.g. "null".
	Name string
	// Version is the provider version without a leading "v", e.g. "3.2.3".
	Version string
	// OS and Arch identify the platform the archive was built for.
	OS   string
	Arch string
	// WorkDir is a caller-owned temporary directory the extraction runs in.
	WorkDir string
	// TofuBinPath is the path to the tofu binary. Defaults to "tofu" on PATH.
	TofuBinPath string
	// RegistryHost is the registry hostname recorded in the filesystem mirror layout.
	// Defaults to "registry.opentofu.org".
	RegistryHost string
}

// Extract produces the raw `tofu providers schema -json` output for a provider archive.
// It builds an offline filesystem mirror from the already-downloaded zip, writes a
// throwaway root module that requires only that provider, and runs `tofu init` followed
// by `tofu providers schema -json` with a scrubbed environment. No network access is
// required or permitted.
func Extract(ctx context.Context, in ExtractInput) ([]byte, error) {
	if err := in.validate(); err != nil {
		return nil, err
	}

	host := in.RegistryHost
	if host == "" {
		host = "registry.opentofu.org"
	}

	tofuBin := in.TofuBinPath
	if tofuBin == "" {
		tofuBin = "tofu"
	}

	mirrorDir := filepath.Join(in.WorkDir, "mirror", host, in.Namespace, in.Name)
	if err := os.MkdirAll(mirrorDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create provider mirror directory: %w", err)
	}

	mirrorZip := filepath.Join(mirrorDir,
		fmt.Sprintf("terraform-provider-%s_%s_%s_%s.zip", in.Name, in.Version, in.OS, in.Arch))
	if err := copyFile(in.ProviderZipPath, mirrorZip); err != nil {
		return nil, fmt.Errorf("failed to stage provider archive into mirror: %w", err)
	}

	configDir := filepath.Join(in.WorkDir, "config")
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create tofu config directory: %w", err)
	}

	mainTF := fmt.Sprintf(`terraform {
  required_providers {
    %s = {
      source  = "%s/%s/%s"
      version = "%s"
    }
  }
}
`, in.Name, host, in.Namespace, in.Name, in.Version)

	if err := os.WriteFile(filepath.Join(configDir, "main.tf"), []byte(mainTF), 0600); err != nil {
		return nil, fmt.Errorf("failed to write throwaway root module: %w", err)
	}

	cliConfigPath := filepath.Join(in.WorkDir, "tofurc")
	cliConfig := fmt.Sprintf(`provider_installation {
  filesystem_mirror {
    path    = %q
    include = ["*/*/*"]
  }
  direct {
    exclude = ["*/*/*"]
  }
}
`, filepath.Join(in.WorkDir, "mirror"))

	if err := os.WriteFile(cliConfigPath, []byte(cliConfig), 0600); err != nil {
		return nil, fmt.Errorf("failed to write tofu CLI config: %w", err)
	}

	// A scrubbed environment: no inherited cloud credentials reach the provider binary,
	// and every path tofu might write to is confined to the per-invocation work dir.
	env := []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + in.WorkDir,
		"TF_CLI_CONFIG_FILE=" + cliConfigPath,
		"TF_IN_AUTOMATION=1",
		"TF_INPUT=0",
		"CHECKPOINT_DISABLE=1",
		"TMPDIR=" + in.WorkDir,
	}

	if _, err := runTofu(ctx, tofuBin, configDir, env, "init", "-input=false", "-backend=false"); err != nil {
		return nil, fmt.Errorf("tofu init failed for %s/%s %s: %w", in.Namespace, in.Name, in.Version, err)
	}

	out, err := runTofu(ctx, tofuBin, configDir, env, "providers", "schema", "-json")
	if err != nil {
		return nil, fmt.Errorf("tofu providers schema failed for %s/%s %s: %w", in.Namespace, in.Name, in.Version, err)
	}

	return out, nil
}

// validate rejects incomplete extraction inputs before any process is spawned.
func (in ExtractInput) validate() error {
	missing := make([]string, 0, 6)
	if strings.TrimSpace(in.ProviderZipPath) == "" {
		missing = append(missing, "providerZipPath")
	}

	if strings.TrimSpace(in.Namespace) == "" {
		missing = append(missing, "namespace")
	}

	if strings.TrimSpace(in.Name) == "" {
		missing = append(missing, "name")
	}

	if strings.TrimSpace(in.Version) == "" {
		missing = append(missing, "version")
	}

	if strings.TrimSpace(in.OS) == "" || strings.TrimSpace(in.Arch) == "" {
		missing = append(missing, "os/arch")
	}

	if strings.TrimSpace(in.WorkDir) == "" {
		missing = append(missing, "workDir")
	}

	if len(missing) > 0 {
		return fmt.Errorf("provider schema extraction is missing required input: %s", strings.Join(missing, ", "))
	}

	return nil
}

// runTofu executes a single tofu subcommand, bounding its output size and returning
// stderr as part of any error so failures are diagnosable from controller logs alone.
func runTofu(ctx context.Context, bin, dir string, env []string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = dir
	cmd.Env = env

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%w — stderr: %s", err, strings.TrimSpace(stderr.String()))
	}

	if stdout.Len() > maxSchemaBytes {
		return nil, fmt.Errorf("provider schema output exceeded the %d byte limit", maxSchemaBytes)
	}

	return stdout.Bytes(), nil
}

// copyFile copies src to dst, creating dst with owner-only permissions.
func copyFile(src, dst string) error {
	in, err := os.Open(src) //nolint:gosec // path is controller-generated, not user input
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}

	return out.Sync()
}
