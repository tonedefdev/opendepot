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
	"encoding/base64"
	"fmt"

	"github.com/google/go-github/v81/github"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	opendepotGithub "github.com/tonedefdev/opendepot/pkg/github"
)

// readmeConfigMapKey is the ConfigMap data key holding a module Version's base64 encoded
// README content.
const readmeConfigMapKey = "README.md"

// fetchModuleReadme resolves a module Version's README content. It tries the GitHub API first
// (using the same authenticated/unauthenticated client selection as fetchModuleArchive), falling
// back to extracting a README file directly from the already-downloaded module archive. Returns
// nil, nil if no README can be resolved by either method — this is a non-fatal condition and
// callers must not fail reconciliation when it occurs.
func (r *VersionReconciler) fetchModuleReadme(ctx context.Context, version *opendepotv1alpha1.Version, fileBytes []byte) ([]byte, error) {
	var githubClientConfig *opendepotGithub.GithubClientConfig
	var githubClient *github.Client

	useAuthClient := false
	if version.Spec.ModuleConfigRef.GithubClientConfig != nil {
		useAuthClient = version.Spec.ModuleConfigRef.GithubClientConfig.UseAuthenticatedClient
	}

	var err error
	if useAuthClient {
		githubClientConfig, err = opendepotGithub.GetGithubApplicationSecret(ctx, r.Client, version.Namespace)
		if err != nil {
			return extractReadmeFromArchive(fileBytes)
		}
	}

	githubClient, err = opendepotGithub.CreateGithubClient(ctx, useAuthClient, githubClientConfig)
	if err != nil {
		return extractReadmeFromArchive(fileBytes)
	}

	readme, err := opendepotGithub.GetModuleReadme(ctx, githubClient, version.Spec.ModuleConfigRef.RepoOwner, *version.Spec.ModuleConfigRef.Name, version.Spec.Version)
	if err != nil {
		r.Log.V(5).Info("readme not found via Github API; falling back to module archive", "version", version.Name, "error", err.Error())
		return extractReadmeFromArchive(fileBytes)
	}

	return readme, nil
}

// upsertReadmeConfigMap creates or updates the ConfigMap holding a module Version's base64
// encoded README content and returns a reference to it. The ConfigMap is owned by the Version
// so that it is garbage collected automatically when the Version is deleted.
func (r *VersionReconciler) upsertReadmeConfigMap(ctx context.Context, version *opendepotv1alpha1.Version, readmeBytes []byte) (*opendepotv1alpha1.ReadmeConfigMapRef, error) {
	cmName := version.Name + "-readme"

	var moduleName string
	if version.Spec.ModuleConfigRef != nil && version.Spec.ModuleConfigRef.Name != nil {
		moduleName = *version.Spec.ModuleConfigRef.Name
	}

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cmName,
			Namespace: version.Namespace,
		},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, cm, func() error {
		labels := map[string]string{
			"opendepot.defdev.io/version":   version.Name,
			"opendepot.defdev.io/namespace": version.Namespace,
		}

		if moduleName != "" {
			labels["opendepot.defdev.io/module"] = moduleName
		}

		cm.Labels = labels

		if cm.Data == nil {
			cm.Data = map[string]string{}
		}

		cm.Data[readmeConfigMapKey] = base64.StdEncoding.EncodeToString(readmeBytes)

		return controllerutil.SetControllerReference(version, cm, r.Scheme)
	})
	if err != nil {
		return nil, fmt.Errorf("failed to upsert readme configmap %s: %w", cmName, err)
	}

	return &opendepotv1alpha1.ReadmeConfigMapRef{
		Name: cmName,
		Key:  readmeConfigMapKey,
	}, nil
}
