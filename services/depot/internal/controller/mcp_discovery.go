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
	"fmt"
	"slices"
	"strings"

	"github.com/hashicorp/go-version"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	opendepotGithub "github.com/tonedefdev/opendepot/pkg/github"
	"github.com/tonedefdev/opendepot/pkg/mcp"
)

const (
	defaultMCPNpmRegistry = "https://registry.npmjs.org"
	defaultMCPPyPIIndex   = "https://pypi.org"
	mcpSourceTypeNpm      = "Npm"
	mcpSourceTypePyPI     = "PyPI"
	mcpSourceTypeSource   = "Source"
	mcpSourceTypeImage    = "Image"
)

// reconcileMCPServerConfig discovers the versions for one MCP server source config and creates or updates its
// MCPServer in the Depot namespace. It returns the managed MCPServer name.
func (r *DepotReconciler) reconcileMCPServerConfig(ctx context.Context, depot *opendepotv1alpha1.Depot, config opendepotv1alpha1.MCPServerSourceConfig) (string, error) {
	if config.Name == nil || strings.TrimSpace(*config.Name) == "" {
		return "", fmt.Errorf("mcp server config name is required")
	}

	if errs := validation.IsDNS1123Subdomain(*config.Name); len(errs) > 0 {
		return "", fmt.Errorf("mcp server config name %q is not a valid resource name: %s", *config.Name, strings.Join(errs, "; "))
	}

	discovered, err := r.discoverMCPServerVersions(ctx, depot.Namespace, &config)
	if err != nil {
		return "", fmt.Errorf("mcp server %q: discover versions: %w", *config.Name, err)
	}

	r.Log.Info("Discovered versions for MCP server", "mcpServer", *config.Name, "versions", len(discovered))

	objectKey := client.ObjectKey{Name: *config.Name, Namespace: depot.Namespace}
	var current opendepotv1alpha1.MCPServer
	err = r.Get(ctx, objectKey, &current)
	if err != nil && !errors.IsNotFound(err) {
		return "", err
	}

	if errors.IsNotFound(err) {
		server := opendepotv1alpha1.MCPServer{
			ObjectMeta: v1.ObjectMeta{
				Name:      *config.Name,
				Namespace: depot.Namespace,
			},
			Spec: opendepotv1alpha1.MCPServerSpec{
				MCPServerSourceConfig: config,
				Versions:              mergeMCPVersions(nil, discovered, config.VersionHistoryLimit),
			},
		}

		if err := controllerutil.SetControllerReference(depot, &server, r.Scheme); err != nil {
			return "", err
		}

		if err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
			return r.Create(ctx, &server)
		}); err != nil {
			return "", err
		}

		return *config.Name, nil
	}

	if err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		if err := r.Get(ctx, objectKey, &current); err != nil {
			return err
		}

		desired := current.Spec
		desired.MCPServerSourceConfig = config
		desired.Versions = mergeMCPVersions(current.Spec.Versions, discovered, config.VersionHistoryLimit)
		if apiequality.Semantic.DeepEqual(current.Spec, desired) && v1.IsControlledBy(&current, depot) {
			return nil
		}

		current.Spec = desired
		if err := controllerutil.SetControllerReference(depot, &current, r.Scheme); err != nil {
			return err
		}

		return r.Update(ctx, &current)
	}); err != nil {
		return "", err
	}

	return *config.Name, nil
}

// mergeMCPVersions applies mergeVersionEntries to MCPServer versions keyed by version name.
func mergeMCPVersions(existing []opendepotv1alpha1.MCPServerVersion, discovered []opendepotv1alpha1.MCPServerVersion, limit *int) []opendepotv1alpha1.MCPServerVersion {
	return mergeVersionEntries(existing, discovered,
		func(v opendepotv1alpha1.MCPServerVersion) string { return v.Name },
		func(v opendepotv1alpha1.MCPServerVersion) bool { return v.Synced },
		limit,
	)
}

// discoverMCPServerVersions dispatches to the npm, PyPI, source repository, or image source of an MCP server config.
func (r *DepotReconciler) discoverMCPServerVersions(ctx context.Context, namespace string, config *opendepotv1alpha1.MCPServerSourceConfig) ([]opendepotv1alpha1.MCPServerVersion, error) {
	source := config.Source
	var found []string
	var err error
	switch source.Type {
	case mcpSourceTypeNpm:
		found, err = r.discoverNpmVersions(ctx, config)
	case mcpSourceTypePyPI:
		found, err = r.discoverPyPIVersions(ctx, config)
	case mcpSourceTypeSource:
		return r.discoverMCPSourceVersions(ctx, namespace, config)
	case mcpSourceTypeImage:
		found, err = r.discoverImageVersions(ctx, namespace, config)
	default:
		return nil, fmt.Errorf("unsupported MCP source type %q", source.Type)
	}

	if err != nil {
		return nil, err
	}

	versions := make([]opendepotv1alpha1.MCPServerVersion, 0, len(found))
	for _, v := range found {
		versions = append(versions, opendepotv1alpha1.MCPServerVersion{Name: v, Version: v})
	}

	return versions, nil
}

// discoverNpmVersions returns the npm package versions that satisfy the version constraints. The registry must be in
// MCPAllowedNpmRegistries.
func (r *DepotReconciler) discoverNpmVersions(ctx context.Context, config *opendepotv1alpha1.MCPServerSourceConfig) ([]string, error) {
	npm := config.Source.Npm
	if npm == nil {
		return nil, fmt.Errorf("npm source is required for type Npm")
	}

	allow, err := mcp.NewAllowList(r.MCPAllowedNpmRegistries)
	if err != nil {
		return nil, err
	}

	registry := defaultMCPNpmRegistry
	if npm.Registry != nil && strings.TrimSpace(*npm.Registry) != "" {
		registry = withScheme(*npm.Registry)
	}

	all, err := mcp.NewClient(allow).NPMVersions(ctx, registry, npm.Package)
	if err != nil {
		return nil, err
	}

	return filterSemverVersions(all, config.VersionConstraints)
}

// discoverPyPIVersions returns the PyPI versions that have a compatible wheel and satisfy the version constraints.
// The index must be in MCPAllowedPyPIIndexes.
func (r *DepotReconciler) discoverPyPIVersions(ctx context.Context, config *opendepotv1alpha1.MCPServerSourceConfig) ([]string, error) {
	pypi := config.Source.PyPI
	if pypi == nil {
		return nil, fmt.Errorf("pypi source is required for type PyPI")
	}

	allow, err := mcp.NewAllowList(r.MCPAllowedPyPIIndexes)
	if err != nil {
		return nil, err
	}

	index := defaultMCPPyPIIndex
	if pypi.Index != nil && strings.TrimSpace(*pypi.Index) != "" {
		index = withScheme(*pypi.Index)
	}

	platforms := pypi.Platforms
	if len(platforms) == 0 {
		platforms = r.MCPPyPIDefaultPlatforms
	}

	filter := mcp.PyPIFilter{Platforms: platforms}
	if pypi.PythonVersion != nil {
		filter.PythonVersion = *pypi.PythonVersion
	}

	all, err := mcp.NewClient(allow).PyPIVersions(ctx, index, pypi.Package, filter)
	if err != nil {
		return nil, err
	}

	return filterSemverVersions(all, config.VersionConstraints)
}

// discoverMCPSourceVersions lists the git tags of the MCP server source repository.
func (r *DepotReconciler) discoverMCPSourceVersions(ctx context.Context, namespace string, config *opendepotv1alpha1.MCPServerSourceConfig) ([]opendepotv1alpha1.MCPServerVersion, error) {
	src := config.Source.Source
	if src == nil {
		return nil, fmt.Errorf("source is required for type Source")
	}

	githubClient, err := r.newGithubClient(ctx, namespace, config.GithubClientConfig)
	if err != nil {
		return nil, err
	}

	repo := repoNameFromURL(src.RepoUrl, config.Name)
	prefix := ""
	if src.TagPrefix != nil {
		prefix = *src.TagPrefix
	}

	found, err := opendepotGithub.ListMatchingTags(ctx, githubClient, src.RepoOwner, repo, prefix, config.VersionConstraints)
	if err != nil {
		return nil, err
	}

	versions := make([]opendepotv1alpha1.MCPServerVersion, 0, len(found))
	for _, v := range found {
		versions = append(versions, opendepotv1alpha1.MCPServerVersion{Name: prefix + v, Version: v})
	}

	return versions, nil
}

// discoverImageVersions returns the synced tags of the OCIArtifact referenced by the image source. The OCIArtifact
// must exist in the Depot namespace.
func (r *DepotReconciler) discoverImageVersions(ctx context.Context, namespace string, config *opendepotv1alpha1.MCPServerSourceConfig) ([]string, error) {
	image := config.Source.Image
	if image == nil {
		return nil, fmt.Errorf("image source is required for type Image")
	}

	var artifact opendepotv1alpha1.OCIArtifact
	if err := r.Get(ctx, client.ObjectKey{Name: image.OCIArtifactRef.Name, Namespace: namespace}, &artifact); err != nil {
		return nil, fmt.Errorf("get OCIArtifact %q: %w", image.OCIArtifactRef.Name, err)
	}

	var synced []string
	for key, ref := range artifact.Status.VersionRefs {
		if ref != nil && ref.Synced {
			synced = append(synced, key)
		}
	}

	slices.Sort(synced)
	return filterSemverVersions(synced, config.VersionConstraints)
}

// filterSemverVersions keeps the versions that parse as semver and satisfy constraints. Blank constraints keep the
// semver versions that parse, in input order.
func filterSemverVersions(versions []string, constraints string) ([]string, error) {
	var parsed version.Constraints
	if strings.TrimSpace(constraints) != "" {
		var err error
		parsed, err = version.NewConstraint(constraints)
		if err != nil {
			return nil, fmt.Errorf("invalid versionConstraints: %w", err)
		}
	}

	var matched []string
	for _, v := range versions {
		sv, err := version.NewVersion(v)
		if err != nil {
			continue
		}

		if parsed != nil && !parsed.Check(sv) {
			continue
		}

		matched = append(matched, v)
	}

	return matched, nil
}

// withScheme returns value as a URL, defaulting to https when no scheme is given.
func withScheme(value string) string {
	value = strings.TrimSpace(value)
	if strings.Contains(value, "://") {
		return value
	}

	return "https://" + value
}
