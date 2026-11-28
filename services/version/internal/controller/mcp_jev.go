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

package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	"github.com/tonedefdev/opendepot/pkg/agentspec"
	"github.com/tonedefdev/opendepot/pkg/mcp"
	"github.com/tonedefdev/opendepot/services/version/internal/policy"
	ctrl "sigs.k8s.io/controller-runtime"
)

// mcpServerJSONFile and mcpPackageJSONFile name the metadata files that describe a vendored MCP server.
const (
	mcpServerJSONFile  = "server.json"
	mcpPackageJSONFile = "package.json"
	mcpPyProjectFile   = "pyproject.toml"
)

// mcpReadmeFiles are the README names checked, in order, when a package ships one.
var mcpReadmeFiles = []string{"README.md", "README", "readme.md"}

// mcpMeta is the server name and description read from a vendored MCP server.
type mcpMeta struct {
	name        string
	description string
}

// mcpJevGate is the outcome of the Jev gate that a Version publishes with.
type mcpJevGate struct {
	assessment *opendepotv1alpha1.JevAssessment
	governance *opendepotv1alpha1.JevExemptionGovernance
	unblock    *opendepotv1alpha1.JevUnblock
}

// mcpAgentShadow returns a copy of an MCPServer Version shaped as an Agent Version, so the Jev assessment and the
// repository archive helper can run unchanged. The shadow is never stored. Its AgentSourceRef is built from the
// MCPServer source, and its Status is carried over so a cached Jev assessment is reused.
func mcpAgentShadow(version *opendepotv1alpha1.Version) *opendepotv1alpha1.Version {
	shadow := &opendepotv1alpha1.Version{
		ObjectMeta: version.ObjectMeta,
		Spec:       version.Spec,
		Status:     version.Status,
	}
	shadow.Spec.Type = opendepotv1alpha1.OpenDepotAgent
	shadow.Spec.AgentSourceRef = nil

	cfg := version.Spec.MCPServerSourceRef
	if cfg == nil {
		return shadow
	}

	ref := &opendepotv1alpha1.AgentSourceConfig{
		Name:                cfg.Name,
		VersionConstraints:  cfg.VersionConstraints,
		VersionHistoryLimit: cfg.VersionHistoryLimit,
		Immutable:           cfg.Immutable,
		GithubClientConfig:  cfg.GithubClientConfig,
		StorageConfig:       cfg.StorageConfig,
		JevSecretRef:        cfg.JevSecretRef,
		JevPolicy:           cfg.JevPolicy,
	}

	if repo := cfg.Source.Source; repo != nil {
		ref.RepoOwner = repo.RepoOwner
		ref.RepoUrl = repo.RepoUrl
		ref.Path = repo.Path
		ref.TagPrefix = repo.TagPrefix
	}

	shadow.Spec.AgentSourceRef = ref

	return shadow
}

// stageMCPJev describes the metadata files of a vendored MCP server to the Jev assessment. Only server.json,
// package metadata, and the README are assessed, so the vendored dependencies are never sent to Jev.
func stageMCPJev(vendored *mcpVendored, metadata mcpMeta, checksum string) (*agentContent, error) {
	var files []string
	for _, name := range []string{mcpServerJSONFile, mcpPackageJSONFile, mcpPyProjectFile} {
		if _, err := os.Stat(filepath.Join(vendored.pkgDir, name)); err == nil {
			files = append(files, name)
		}
	}

	for _, name := range mcpReadmeFiles {
		if _, err := os.Stat(filepath.Join(vendored.pkgDir, name)); err == nil {
			files = append(files, name)
			break
		}
	}

	if len(files) == 0 {
		return nil, newMCPFinding(mcpFindingInvalid, "no server.json, package.json, pyproject.toml, or README found in the package")
	}

	return &agentContent{
		entryName:  files[0],
		entryRel:   files[0],
		dir:        vendored.pkgDir,
		files:      files,
		checksum:   checksum,
		definition: &agentspec.Result{Name: metadata.name, Description: metadata.description},
		release:    func() {},
	}, nil
}

// readMCPMetadata reads the server name and description from the vendored package. server.json is preferred, then
// package.json. The name in the MCPServer spec overrides both, and the source package or repository name is the
// fallback.
func readMCPMetadata(pkgDir string, cfg *opendepotv1alpha1.MCPServerSourceConfig, version *opendepotv1alpha1.Version) (mcpMeta, error) {
	var meta mcpMeta

	serverJSON, err := os.ReadFile(filepath.Join(pkgDir, mcpServerJSONFile))
	switch {
	case err == nil:
		doc, parseErr := mcp.ParseServerJSON(serverJSON)
		if parseErr != nil {
			return mcpMeta{}, newMCPFinding(mcpFindingInvalid, "server.json: %v", parseErr)
		}

		meta.name = doc.Name
		meta.description = doc.Description
	case !errors.Is(err, os.ErrNotExist):
		return mcpMeta{}, fmt.Errorf("mcp: read server.json: %w", err)
	}

	if meta.name == "" {
		packageJSON, readErr := os.ReadFile(filepath.Join(pkgDir, mcpPackageJSONFile))
		if readErr == nil {
			var pkg struct {
				Name        string `json:"name"`
				Description string `json:"description"`
			}

			if err := json.Unmarshal(packageJSON, &pkg); err != nil {
				return mcpMeta{}, newMCPFinding(mcpFindingInvalid, "package.json: %v", err)
			}

			meta.name = pkg.Name
			if meta.description == "" {
				meta.description = pkg.Description
			}
		}
	}

	if cfg.Name != nil && *cfg.Name != "" {
		meta.name = *cfg.Name
	}

	if meta.name == "" {
		meta.name = mcpSourceName(cfg, version)
	}

	if meta.name == "" {
		return mcpMeta{}, newMCPFinding(mcpFindingInvalid, "could not determine a server name")
	}

	meta.name = truncateMCPRunes(meta.name, mcpMaxNameRunes)
	meta.description = truncateMCPRunes(meta.description, mcpMaxDescriptionRunes)

	return meta, nil
}

// mcpSourceName returns the package or repository name that identifies the MCP server when its metadata has none.
func mcpSourceName(cfg *opendepotv1alpha1.MCPServerSourceConfig, version *opendepotv1alpha1.Version) string {
	switch cfg.Source.Type {
	case mcpSourceNpm:
		if cfg.Source.Npm != nil {
			return cfg.Source.Npm.Package
		}
	case mcpSourcePyPI:
		if cfg.Source.PyPI != nil {
			return cfg.Source.PyPI.Package
		}
	case mcpSourceRepo:
		if cfg.Source.Source != nil {
			return filepath.Base(filepath.Clean("/" + cfg.Source.Source.Path))
		}
	}

	return version.Name
}

// gateMCPJev runs the Jev assessment of an archived MCPServer and records any block. The gate returns stop when the
// Version must not publish. It then returns the result and error for the reconcile, the same as the agent gate.
func (r *VersionReconciler) gateMCPJev(
	ctx context.Context,
	req ctrl.Request,
	version *opendepotv1alpha1.Version,
	resolved policy.Resolved,
	policies []opendepotv1alpha1.ScanPolicy,
	sourceScan *opendepotv1alpha1.SourceScan,
	content *agentContent,
) (*mcpJevGate, ctrl.Result, bool, error) {
	gate := &mcpJevGate{}

	jevAssessment, jevErr := r.runAgentJev(ctx, mcpAgentShadow(version), content)
	if jevAssessment != nil {
		priorJevAssessment := version.Status.JevAssessment
		jevAssessment = applyJevExemption(jevAssessment, resolved, content.checksum, version.Name)
		gate.governance = policy.JevGovernance(policies, content.checksum, time.Now())
		gate.unblock = policy.JevUnblock(policies, content.checksum, jevAssessment.Blocked, time.Now())

		r.recordJevExemptionChange(version, jevExemptionTransition(priorJevAssessment, jevAssessment), jevAssessment)
		if jevAssessment.Exempted {
			r.Log.Info("Jev block exempted by ScanPolicy", "version", version.Name, "policy", jevAssessment.ExemptedBy,
				"digest", jevAssessment.ContentDigest, "reasons", len(jevAssessment.BlockReasons), "expires", jevExpiresText(jevAssessment))
		}
	}

	gate.assessment = jevAssessment

	retryKey := jevRetryKey(version, content)
	if jevErr == nil {
		r.jevRetries.clear(retryKey)
	} else if jevAssessment != nil {
		r.jevRetries.set(retryKey, jevAssessment.RetryCount)
	}

	if jevErr != nil {
		if err := r.persistAgentBlock(ctx, req, sourceScan, jevAssessment, gate.governance, gate.unblock, fmt.Sprintf("JevUnavailable: %v", jevErr)); err != nil {
			r.Log.Error(err, "Failed to persist jev unavailable status", "version", version.Name)
		}

		r.Log.Error(jevErr, "Jev assessment failed, retrying", "version", version.Name, "retryAfter", jevRetryRequeue)

		if jevRetriesExhausted(jevAssessment, r.JevMaxRetries) {
			r.Log.Info("Jev automatic retries exhausted, waiting for ForceSync or a spec change", "version", version.Name, "retryCount", jevAssessment.RetryCount)

			return gate, ctrl.Result{}, true, nil
		}

		var serviceErr *jevServiceError
		if jevAssessment != nil && jevAssessment.Error != "" && jevAssessment.Error != jevDisabledError && errors.As(jevErr, &serviceErr) {
			return gate, ctrl.Result{RequeueAfter: jevRetryRequeue}, true, nil
		}

		return gate, ctrl.Result{}, true, jevErr
	}

	if jevAssessment != nil && jevAssessment.Blocked {
		r.Log.Info("Jev assessment blocked the Version", "version", version.Name, "reasons", jevAssessment.BlockReasons)

		if err := r.persistAgentBlock(ctx, req, sourceScan, jevAssessment, gate.governance, gate.unblock, "JevBlocked: "+strings.Join(jevAssessment.BlockReasons, "; ")); err != nil {
			r.Log.Error(err, "Failed to persist jev block", "version", version.Name)
		}

		return gate, ctrl.Result{}, true, nil
	}

	return gate, ctrl.Result{}, false, nil
}
