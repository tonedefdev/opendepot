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
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/google/go-github/v81/github"
	"github.com/hashicorp/go-version"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	opendepotGithub "github.com/tonedefdev/opendepot/pkg/github"
	"github.com/tonedefdev/opendepot/pkg/oci"
)

// maxTagPatternLength bounds the RE2 expressions accepted by tagPattern fields.
const maxTagPatternLength = 256

// compileTagPattern compiles an optional RE2 tag pattern. A nil or blank pattern returns a nil expression.
func compileTagPattern(pattern *string) (*regexp.Regexp, error) {
	if pattern == nil || strings.TrimSpace(*pattern) == "" {
		return nil, nil
	}

	if len(*pattern) > maxTagPatternLength {
		return nil, fmt.Errorf("tagPattern must be at most %d characters", maxTagPatternLength)
	}

	re, err := regexp.Compile(*pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid tagPattern: %w", err)
	}

	return re, nil
}

// filterTagNames returns, in input order, the names that begin with prefix and pass the filter. The filter is the
// tag pattern when one is set, otherwise the semver version constraints. Names with no filter set are all kept.
func filterTagNames(names []string, prefix string, re *regexp.Regexp, versionConstraints string) ([]string, error) {
	var constraints version.Constraints
	if re == nil && strings.TrimSpace(versionConstraints) != "" {
		var err error
		constraints, err = version.NewConstraint(versionConstraints)
		if err != nil {
			return nil, fmt.Errorf("invalid versionConstraints: %w", err)
		}
	}

	var matched []string
	for _, name := range names {
		if !strings.HasPrefix(name, prefix) {
			continue
		}

		bare := strings.TrimPrefix(name, prefix)
		switch {
		case re != nil:
			if !re.MatchString(bare) {
				continue
			}
		case constraints != nil:
			v, err := version.NewVersion(bare)
			if err != nil || !constraints.Check(v) {
				continue
			}
		}

		matched = append(matched, name)
	}

	return matched, nil
}

// mergeVersionEntries merges discovered entries into the entries already on a CR while preserving history. Existing
// entries that are still discovered or are synced are kept in their order, discovered entries not yet present are
// appended, and unsynced entries are trimmed from the tail until the list fits limit. Synced entries are never trimmed.
func mergeVersionEntries[T any](existing, discovered []T, key func(T) string, synced func(T) bool, limit *int) []T {
	discoveredKeys := make(map[string]struct{}, len(discovered))
	for _, d := range discovered {
		discoveredKeys[key(d)] = struct{}{}
	}

	existingKeys := make(map[string]struct{}, len(existing))
	var merged []T
	for _, e := range existing {
		_, isDiscovered := discoveredKeys[key(e)]
		if !isDiscovered && !synced(e) {
			continue
		}

		existingKeys[key(e)] = struct{}{}
		merged = append(merged, e)
	}

	for _, d := range discovered {
		if _, ok := existingKeys[key(d)]; ok {
			continue
		}

		existingKeys[key(d)] = struct{}{}
		merged = append(merged, d)
	}

	if limit == nil || *limit <= 0 {
		return merged
	}

	for i := len(merged) - 1; i >= 0 && len(merged) > *limit; i-- {
		if synced(merged[i]) {
			continue
		}

		merged = slices.Delete(merged, i, i+1)
	}

	return merged
}

// repoNameFromURL returns the repository name from repoURL, falling back to name when repoURL is not set.
func repoNameFromURL(repoURL *string, name *string) string {
	if repoURL != nil && strings.TrimSpace(*repoURL) != "" {
		segments := strings.Split(strings.TrimSuffix(strings.TrimSpace(*repoURL), "/"), "/")
		return segments[len(segments)-1]
	}

	if name != nil {
		return *name
	}

	return ""
}

// newGithubClient returns a GitHub client authenticated with the GitHub App secret when the config requests it.
func (r *DepotReconciler) newGithubClient(ctx context.Context, namespace string, cfg *opendepotv1alpha1.GithubClientConfig) (*github.Client, error) {
	useAuthClient := cfg != nil && cfg.UseAuthenticatedClient

	var githubConfig *opendepotGithub.GithubClientConfig
	if useAuthClient {
		var err error
		githubConfig, err = opendepotGithub.GetGithubApplicationSecret(ctx, r.APIReader, namespace)
		if err != nil {
			return nil, err
		}
	}

	return opendepotGithub.CreateGithubClient(ctx, useAuthClient, githubConfig)
}

// reconcileOCIArtifactConfig discovers the versions for one OCI artifact config and creates or updates its OCIArtifact
// in the Depot namespace. It returns the managed OCIArtifact name.
func (r *DepotReconciler) reconcileOCIArtifactConfig(ctx context.Context, depot *opendepotv1alpha1.Depot, config opendepotv1alpha1.OCIArtifactConfig) (string, error) {
	if config.Name == nil || strings.TrimSpace(*config.Name) == "" {
		return "", fmt.Errorf("oci artifact config name is required")
	}

	if errs := validation.IsDNS1123Subdomain(*config.Name); len(errs) > 0 {
		return "", fmt.Errorf("oci artifact config name %q is not a valid resource name: %s", *config.Name, strings.Join(errs, "; "))
	}

	re, err := compileTagPattern(config.TagPattern)
	if err != nil {
		return "", fmt.Errorf("oci artifact %q: %w", *config.Name, err)
	}

	discovered, err := r.discoverOCIArtifactVersions(ctx, depot.Namespace, &config, re)
	if err != nil {
		return "", fmt.Errorf("oci artifact %q: discover versions: %w", *config.Name, err)
	}

	r.Log.Info("Discovered versions for OCI artifact", "ociArtifact", *config.Name, "versions", len(discovered))

	objectKey := client.ObjectKey{Name: *config.Name, Namespace: depot.Namespace}
	var current opendepotv1alpha1.OCIArtifact
	err = r.Get(ctx, objectKey, &current)
	if err != nil && !errors.IsNotFound(err) {
		return "", err
	}

	if errors.IsNotFound(err) {
		artifact := opendepotv1alpha1.OCIArtifact{
			ObjectMeta: v1.ObjectMeta{
				Name:      *config.Name,
				Namespace: depot.Namespace,
			},
			Spec: opendepotv1alpha1.OCIArtifactSpec{
				OCIArtifactConfig: config,
				Versions:          mergeOCIVersions(nil, discovered, config.VersionHistoryLimit),
			},
		}

		if err := controllerutil.SetControllerReference(depot, &artifact, r.Scheme); err != nil {
			return "", err
		}

		if err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
			return r.Create(ctx, &artifact)
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
		desired.OCIArtifactConfig = config
		desired.Versions = mergeOCIVersions(current.Spec.Versions, discovered, config.VersionHistoryLimit)
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

// mergeOCIVersions applies mergeVersionEntries to OCIArtifact versions keyed by tag name.
func mergeOCIVersions(existing []opendepotv1alpha1.OCIArtifactVersion, discovered []opendepotv1alpha1.OCIArtifactVersion, limit *int) []opendepotv1alpha1.OCIArtifactVersion {
	return mergeVersionEntries(existing, discovered,
		func(v opendepotv1alpha1.OCIArtifactVersion) string { return v.Name },
		func(v opendepotv1alpha1.OCIArtifactVersion) bool { return v.Synced },
		limit,
	)
}

// discoverOCIArtifactVersions dispatches to the mirror, GitHub release asset, or build source of an OCI artifact config.
func (r *DepotReconciler) discoverOCIArtifactVersions(ctx context.Context, namespace string, config *opendepotv1alpha1.OCIArtifactConfig, re *regexp.Regexp) ([]opendepotv1alpha1.OCIArtifactVersion, error) {
	switch {
	case config.Mirror != nil:
		return r.discoverMirrorVersions(ctx, namespace, config, re)
	case config.Github != nil:
		return r.discoverGithubAssetVersions(ctx, namespace, config, re)
	case config.Build != nil:
		return r.discoverBuildVersions(ctx, namespace, config, re)
	default:
		return nil, fmt.Errorf("one of mirror, github, or build must be set")
	}
}

// discoverMirrorVersions lists the tags of an upstream OCI repository. The registry must be in OCIAllowedRegistries.
func (r *DepotReconciler) discoverMirrorVersions(ctx context.Context, namespace string, config *opendepotv1alpha1.OCIArtifactConfig, re *regexp.Regexp) ([]opendepotv1alpha1.OCIArtifactVersion, error) {
	if _, err := oci.NewAllowList(r.OCIAllowedRegistries); err != nil {
		return nil, err
	}

	var dockerConfigJSON []byte
	if ref := config.Mirror.CredentialsSecretRef; ref != nil {
		var secret corev1.Secret
		if err := r.APIReader.Get(ctx, client.ObjectKey{Name: ref.Name, Namespace: namespace}, &secret); err != nil {
			return nil, fmt.Errorf("read credentials secret: %w", err)
		}

		if secret.Type != corev1.SecretTypeDockerConfigJson {
			return nil, fmt.Errorf("credentials secret %q must be of type %s", ref.Name, corev1.SecretTypeDockerConfigJson)
		}

		dockerConfigJSON = secret.Data[corev1.DockerConfigJsonKey]
	}

	upstream, err := oci.NewUpstream(r.OCIAllowedRegistries, dockerConfigJSON)
	if err != nil {
		return nil, err
	}

	repository := strings.TrimSuffix(config.Mirror.Registry, "/") + "/" + strings.TrimPrefix(config.Mirror.Repository, "/")
	tags, err := upstream.ListTags(ctx, repository)
	if err != nil {
		return nil, err
	}

	matched, err := filterTagNames(tags, "", re, config.VersionConstraints)
	if err != nil {
		return nil, err
	}

	versions := make([]opendepotv1alpha1.OCIArtifactVersion, 0, len(matched))
	for _, tag := range matched {
		versions = append(versions, opendepotv1alpha1.OCIArtifactVersion{Name: tag, Version: tag})
	}

	return versions, nil
}

// discoverGithubAssetVersions lists the GitHub releases that publish an asset matching assetPattern.
func (r *DepotReconciler) discoverGithubAssetVersions(ctx context.Context, namespace string, config *opendepotv1alpha1.OCIArtifactConfig, re *regexp.Regexp) ([]opendepotv1alpha1.OCIArtifactVersion, error) {
	gh := config.Github
	githubClient, err := r.newGithubClient(ctx, namespace, config.GithubClientConfig)
	if err != nil {
		return nil, err
	}

	repo := repoNameFromURL(gh.RepoUrl, config.Name)
	if err := opendepotGithub.ValidateRepoName(gh.RepoOwner, repo); err != nil {
		return nil, err
	}

	prefix := ""
	if gh.TagPrefix != nil {
		prefix = *gh.TagPrefix
	}

	var tags []string
	opt := &github.ListOptions{Page: 1, PerPage: 100}
	for {
		releases, resp, err := githubClient.Repositories.ListReleases(ctx, gh.RepoOwner, repo, opt)
		if err != nil {
			return nil, err
		}

		if resp == nil {
			return nil, fmt.Errorf("releases response was nil")
		}

		for _, release := range releases {
			if release.GetDraft() || !strings.HasPrefix(release.GetTagName(), prefix) {
				continue
			}

			for _, asset := range release.Assets {
				matched, err := path.Match(gh.AssetPattern, asset.GetName())
				if err != nil {
					return nil, fmt.Errorf("invalid assetPattern: %w", err)
				}

				if matched {
					tags = append(tags, release.GetTagName())
					break
				}
			}
		}

		if resp.NextPage == 0 {
			break
		}

		opt.Page = resp.NextPage
	}

	matched, err := filterTagNames(tags, prefix, re, config.VersionConstraints)
	if err != nil {
		return nil, err
	}

	versions := make([]opendepotv1alpha1.OCIArtifactVersion, 0, len(matched))
	for _, tag := range matched {
		versions = append(versions, opendepotv1alpha1.OCIArtifactVersion{
			Name:    tag,
			Version: strings.TrimPrefix(tag, prefix),
		})
	}

	return versions, nil
}
