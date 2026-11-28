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
	"regexp"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	opendepotGithub "github.com/tonedefdev/opendepot/pkg/github"
)

// discoverBuildVersions lists the git tags of a build source. Without a tagPattern the tags are matched against
// versionConstraints as semver. With a tagPattern the tag names are matched against the RE2 pattern instead.
func (r *DepotReconciler) discoverBuildVersions(ctx context.Context, namespace string, config *opendepotv1alpha1.OCIArtifactConfig, re *regexp.Regexp) ([]opendepotv1alpha1.OCIArtifactVersion, error) {
	build := config.Build
	githubClient, err := r.newGithubClient(ctx, namespace, config.GithubClientConfig)
	if err != nil {
		return nil, err
	}

	repo := repoNameFromURL(build.RepoUrl, config.Name)
	prefix := ""
	if build.TagPrefix != nil {
		prefix = *build.TagPrefix
	}

	var versionsFound []string
	if re == nil {
		versionsFound, err = opendepotGithub.ListMatchingTags(ctx, githubClient, build.RepoOwner, repo, prefix, config.VersionConstraints)
		if err != nil {
			return nil, err
		}
	} else {
		names, err := opendepotGithub.ListTagNames(ctx, githubClient, build.RepoOwner, repo, prefix)
		if err != nil {
			return nil, err
		}

		for _, name := range names {
			if re.MatchString(name) {
				versionsFound = append(versionsFound, name)
			}
		}
	}

	versions := make([]opendepotv1alpha1.OCIArtifactVersion, 0, len(versionsFound))
	for _, v := range versionsFound {
		versions = append(versions, opendepotv1alpha1.OCIArtifactVersion{
			Name:    prefix + v,
			Version: v,
		})
	}

	return versions, nil
}
