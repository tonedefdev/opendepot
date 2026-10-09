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
	"time"

	"github.com/go-logr/logr"
	"github.com/google/go-github/v81/github"
	"github.com/hashicorp/go-version"
	"k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	opendepotGithub "github.com/tonedefdev/opendepot/pkg/github"
	"github.com/tonedefdev/opendepot/pkg/registry"
)

var listProviderVersions = registry.ListProviderVersions

// Depot reconciles a Depot object
type DepotReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=opendepot.defdev.io,resources=depots,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=opendepot.defdev.io,resources=depots/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=opendepot.defdev.io,resources=depots/finalizers,verbs=update
// +kubebuilder:rbac:groups=opendepot.defdev.io,resources=modules,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=opendepot.defdev.io,resources=providers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=opendepot.defdev.io,resources=skills,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=opendepot.defdev.io,resources=agents,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/reconcile
func (r *DepotReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var depot opendepotv1alpha1.Depot
	err := r.Get(ctx, req.NamespacedName, &depot)
	if err != nil {
		if errors.IsNotFound(err) {
			r.Log.V(5).Info("Depot resource not found. Ignoring since object must be deleted", "depot", req.Name)
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		r.Log.Error(err, "Failed to get Depot", "depot", req.Name)
		return ctrl.Result{}, err
	}

	r.Log.V(5).Info(
		"Depot found: starting reconciliation",
		"depot", depot.ObjectMeta.Name,
	)

	var managedModules []string
	if len(depot.Spec.ModuleConfigs) > 0 {
		for _, moduleConfig := range depot.Spec.ModuleConfigs {
			// Set global configs if not set on module config
			if moduleConfig.StorageConfig == nil && depot.Spec.GlobalConfig != nil {
				moduleConfig.StorageConfig = depot.Spec.GlobalConfig.StorageConfig
			}

			if moduleConfig.GithubClientConfig == nil && depot.Spec.GlobalConfig != nil {
				moduleConfig.GithubClientConfig = depot.Spec.GlobalConfig.GithubClientConfig
			}

			if moduleConfig.FileFormat == nil && depot.Spec.GlobalConfig != nil && depot.Spec.GlobalConfig.ModuleConfig != nil {
				moduleConfig.FileFormat = depot.Spec.GlobalConfig.ModuleConfig.FileFormat
			}

			if moduleConfig.Immutable == nil && depot.Spec.GlobalConfig != nil && depot.Spec.GlobalConfig.ModuleConfig != nil {
				moduleConfig.Immutable = depot.Spec.GlobalConfig.ModuleConfig.Immutable
			}

			if moduleConfig.RepoUrl == nil {
				repoUrl := fmt.Sprintf("https://github.com/%s/%s", moduleConfig.RepoOwner, *moduleConfig.Name)
				moduleConfig.RepoUrl = &repoUrl
			}

			module := opendepotv1alpha1.Module{
				ObjectMeta: v1.ObjectMeta{
					Name:      *moduleConfig.Name,
					Namespace: req.Namespace,
				},
				Spec: opendepotv1alpha1.ModuleSpec{
					ModuleConfig: moduleConfig,
				},
			}

			moduleObject := client.ObjectKey{
				Name:      module.ObjectMeta.Name,
				Namespace: module.ObjectMeta.Namespace,
			}

			var githubClient *github.Client

			useAuthClient := false
			if module.Spec.ModuleConfig.GithubClientConfig != nil {
				useAuthClient = module.Spec.ModuleConfig.GithubClientConfig.UseAuthenticatedClient
			}

			var githubConfig *opendepotGithub.GithubClientConfig
			if useAuthClient {
				githubConfig, err = opendepotGithub.GetGithubApplicationSecret(ctx, r.Client, depot.Namespace)
				if err != nil {
					return ctrl.Result{}, err
				}
			}

			authGithubClient, err := opendepotGithub.CreateGithubClient(ctx, useAuthClient, githubConfig)
			if err != nil {
				return ctrl.Result{}, err
			}

			githubClient = authGithubClient
			opt := &github.ListOptions{
				Page:    1,
				PerPage: 100,
			}

			constraints, err := version.NewConstraint(moduleConfig.VersionConstraints)
			if err != nil {
				return ctrl.Result{}, err
			}

			var matchedVersions []string
			for {
				if err := opendepotGithub.ValidateRepoName(moduleConfig.RepoOwner, *moduleConfig.Name); err != nil {
					return ctrl.Result{}, err
				}

				releases, resp, err := githubClient.Repositories.ListReleases(ctx, moduleConfig.RepoOwner, *moduleConfig.Name, opt)
				if err != nil {
					return ctrl.Result{}, err
				}

				if releases == nil || resp == nil {
					return ctrl.Result{}, fmt.Errorf("releases was nil")
				}

				for _, release := range releases {
					if release.TagName == nil {
						continue
					}

					releaseVersion, err := version.NewVersion(*release.TagName)
					if err != nil {
						r.Log.V(5).Info("Skipping non-semver release tag", "tag", *release.TagName)
						continue
					}

					// Constraints returned from version.NewConstraint use AND semantics,
					// so a release must satisfy the full expression (e.g. >=6.0.0, <=7.0.0).
					if !constraints.Check(releaseVersion) {
						continue
					}

					if slices.Contains(matchedVersions, releaseVersion.String()) {
						continue
					}

					matchedVersions = append(matchedVersions, releaseVersion.String())
				}

				if resp.NextPage == 0 {
					break
				}

				opt.Page = resp.NextPage
			}

			r.Log.Info("Matched versions for module", "module", moduleConfig.Name, "versions", matchedVersions)

			var versions []opendepotv1alpha1.ModuleVersion
			for _, version := range matchedVersions {
				moduleVersion := opendepotv1alpha1.ModuleVersion{
					Version: version,
				}
				versions = append(versions, moduleVersion)
			}

			module.Spec.Versions = versions

			var currentModule opendepotv1alpha1.Module
			err = r.Get(ctx, moduleObject, &currentModule)
			if err != nil {
				if !errors.IsNotFound(err) {
					return ctrl.Result{}, err
				}

				if err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
					if err := r.Create(ctx, &module); err != nil {
						return err
					}
					return nil
				}); err != nil {
					return ctrl.Result{}, err
				}
			} else {
				if err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
					if err := r.Get(ctx, moduleObject, &currentModule); err != nil {
						return err
					}

					currentModule.Spec.ModuleConfig = moduleConfig
					currentModule.Spec.Versions = module.Spec.Versions
					if err := r.Update(ctx, &currentModule); err != nil {
						return err
					}
					return nil
				}); err != nil {
					return ctrl.Result{}, err
				}
			}

			managedModules = append(managedModules, *moduleConfig.Name)
		}
	}

	var managedProviders []string
	if len(depot.Spec.ProviderConfigs) > 0 {
		for _, providerConfig := range depot.Spec.ProviderConfigs {
			// Apply global storage config if not set on this provider config.
			if providerConfig.StorageConfig == nil && depot.Spec.GlobalConfig != nil {
				providerConfig.StorageConfig = depot.Spec.GlobalConfig.StorageConfig
			}

			providerName := ""
			if providerConfig.Name != nil {
				providerName = *providerConfig.Name
			}

			if providerName == "" {
				return ctrl.Result{}, fmt.Errorf("provider config name is required")
			}

			ns := registry.DefaultNamespace
			if providerConfig.Namespace != nil && strings.TrimSpace(*providerConfig.Namespace) != "" {
				ns = strings.TrimSpace(*providerConfig.Namespace)
			}

			upstreamRegistry := opendepotv1alpha1.ProviderUpstreamRegistry(&providerConfig)
			allVersions, err := listProviderVersions(ctx, upstreamRegistry, ns, providerName)
			if err != nil {
				return ctrl.Result{}, err
			}

			constraints, err := version.NewConstraint(providerConfig.VersionConstraints)
			if err != nil {
				return ctrl.Result{}, fmt.Errorf("invalid version constraints %q: %w", providerConfig.VersionConstraints, err)
			}

			var matchedVersions []string
			for _, v := range allVersions {
				parsed, parseErr := version.NewVersion(v)
				if parseErr != nil {
					r.Log.V(5).Info("Skipping non-semver provider version", "version", v)
					continue
				}

				if constraints.Check(parsed) && !slices.Contains(matchedVersions, parsed.String()) {
					matchedVersions = append(matchedVersions, parsed.String())
				}
			}

			r.Log.Info("Matched versions for provider", "provider", providerName, "versions", matchedVersions)

			var providerVersions []opendepotv1alpha1.ProviderVersion
			for _, v := range matchedVersions {
				providerVersions = append(providerVersions, opendepotv1alpha1.ProviderVersion{
					Version: v,
				})
			}

			provider := opendepotv1alpha1.Provider{
				ObjectMeta: v1.ObjectMeta{
					Name:      providerName,
					Namespace: req.Namespace,
				},
				Spec: opendepotv1alpha1.ProviderSpec{
					ProviderConfig: providerConfig,
					Versions:       providerVersions,
				},
			}

			providerObject := client.ObjectKey{
				Name:      provider.ObjectMeta.Name,
				Namespace: provider.ObjectMeta.Namespace,
			}

			var currentProvider opendepotv1alpha1.Provider
			err = r.Get(ctx, providerObject, &currentProvider)
			if err != nil {
				if !errors.IsNotFound(err) {
					return ctrl.Result{}, err
				}

				if err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
					if err := r.Create(ctx, &provider); err != nil {
						return err
					}
					return nil
				}); err != nil {
					return ctrl.Result{}, err
				}
			} else {
				if err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
					if err := r.Get(ctx, providerObject, &currentProvider); err != nil {
						return err
					}

					currentProvider.Spec.ProviderConfig = providerConfig
					currentProvider.Spec.Versions = provider.Spec.Versions
					if err := r.Update(ctx, &currentProvider); err != nil {
						return err
					}
					return nil
				}); err != nil {
					return ctrl.Result{}, err
				}
			}

			managedProviders = append(managedProviders, providerName)
		}
	}

	var managedSkills []string
	for _, skillConfig := range depot.Spec.SkillConfigs {
		// Apply global configs if not set on this skill config.
		if skillConfig.StorageConfig == nil && depot.Spec.GlobalConfig != nil {
			skillConfig.StorageConfig = depot.Spec.GlobalConfig.StorageConfig
		}

		if skillConfig.GithubClientConfig == nil && depot.Spec.GlobalConfig != nil {
			skillConfig.GithubClientConfig = depot.Spec.GlobalConfig.GithubClientConfig
		}

		if skillConfig.Name == nil || strings.TrimSpace(*skillConfig.Name) == "" {
			return ctrl.Result{}, fmt.Errorf("skill config name is required")
		}

		matchedVersions, err := r.matchAgentSourceVersions(ctx, req.Namespace, &skillConfig)
		if err != nil {
			return ctrl.Result{}, err
		}

		r.Log.Info("Matched versions for skill", "skill", *skillConfig.Name, "versions", matchedVersions)

		var skillVersions []opendepotv1alpha1.SkillVersion
		for _, v := range matchedVersions {
			skillVersions = append(skillVersions, opendepotv1alpha1.SkillVersion{
				Version: v,
			})
		}

		skill := opendepotv1alpha1.Skill{
			ObjectMeta: v1.ObjectMeta{
				Name:      *skillConfig.Name,
				Namespace: req.Namespace,
			},
			Spec: opendepotv1alpha1.SkillSpec{
				AgentSourceConfig: skillConfig,
				Versions:          skillVersions,
			},
		}

		skillObject := client.ObjectKey{
			Name:      skill.ObjectMeta.Name,
			Namespace: skill.ObjectMeta.Namespace,
		}

		var currentSkill opendepotv1alpha1.Skill
		err = r.Get(ctx, skillObject, &currentSkill)
		if err != nil {
			if !errors.IsNotFound(err) {
				return ctrl.Result{}, err
			}

			if err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
				if err := r.Create(ctx, &skill); err != nil {
					return err
				}
				return nil
			}); err != nil {
				return ctrl.Result{}, err
			}
		} else {
			if err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
				if err := r.Get(ctx, skillObject, &currentSkill); err != nil {
					return err
				}

				currentSkill.Spec.AgentSourceConfig = skillConfig
				currentSkill.Spec.Versions = skill.Spec.Versions
				if err := r.Update(ctx, &currentSkill); err != nil {
					return err
				}
				return nil
			}); err != nil {
				return ctrl.Result{}, err
			}
		}

		managedSkills = append(managedSkills, *skillConfig.Name)
	}

	var managedAgents []string
	for _, agentConfig := range depot.Spec.AgentConfigs {
		// Apply global configs if not set on this agent config.
		if agentConfig.StorageConfig == nil && depot.Spec.GlobalConfig != nil {
			agentConfig.StorageConfig = depot.Spec.GlobalConfig.StorageConfig
		}

		if agentConfig.GithubClientConfig == nil && depot.Spec.GlobalConfig != nil {
			agentConfig.GithubClientConfig = depot.Spec.GlobalConfig.GithubClientConfig
		}

		if agentConfig.Name == nil || strings.TrimSpace(*agentConfig.Name) == "" {
			return ctrl.Result{}, fmt.Errorf("agent config name is required")
		}

		matchedVersions, err := r.matchAgentSourceVersions(ctx, req.Namespace, &agentConfig)
		if err != nil {
			return ctrl.Result{}, err
		}

		r.Log.Info("Matched versions for agent", "agent", *agentConfig.Name, "versions", matchedVersions)

		var agentVersions []opendepotv1alpha1.AgentVersion
		for _, v := range matchedVersions {
			agentVersions = append(agentVersions, opendepotv1alpha1.AgentVersion{
				Version: v,
			})
		}

		agent := opendepotv1alpha1.Agent{
			ObjectMeta: v1.ObjectMeta{
				Name:      *agentConfig.Name,
				Namespace: req.Namespace,
			},
			Spec: opendepotv1alpha1.AgentSpec{
				AgentSourceConfig: agentConfig,
				Versions:          agentVersions,
			},
		}

		agentObject := client.ObjectKey{
			Name:      agent.ObjectMeta.Name,
			Namespace: agent.ObjectMeta.Namespace,
		}

		var currentAgent opendepotv1alpha1.Agent
		err = r.Get(ctx, agentObject, &currentAgent)
		if err != nil {
			if !errors.IsNotFound(err) {
				return ctrl.Result{}, err
			}

			if err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
				if err := r.Create(ctx, &agent); err != nil {
					return err
				}
				return nil
			}); err != nil {
				return ctrl.Result{}, err
			}
		} else {
			if err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
				if err := r.Get(ctx, agentObject, &currentAgent); err != nil {
					return err
				}

				currentAgent.Spec.AgentSourceConfig = agentConfig
				currentAgent.Spec.Versions = agent.Spec.Versions
				if err := r.Update(ctx, &currentAgent); err != nil {
					return err
				}
				return nil
			}); err != nil {
				return ctrl.Result{}, err
			}
		}

		managedAgents = append(managedAgents, *agentConfig.Name)
	}

	if err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		if err := r.Get(ctx, req.NamespacedName, &depot); err != nil {
			return err
		}

		depot.Status.Modules = managedModules
		depot.Status.Providers = managedProviders
		depot.Status.Skills = managedSkills
		depot.Status.Agents = managedAgents
		if err := r.Status().Update(ctx, &depot); err != nil {
			return err
		}
		return nil
	}); err != nil {
		r.Log.Error(err, "Failed to update Depot status", "depot", depot.Name)
		return ctrl.Result{}, err
	}

	if depot.Spec.PollingIntervalMinutes != nil {
		return ctrl.Result{RequeueAfter: time.Duration(*depot.Spec.PollingIntervalMinutes) * time.Minute}, nil
	}

	return ctrl.Result{}, nil
}

// matchAgentSourceVersions returns the semver tags in the agent source repository that satisfy the version constraints.
// The GitHub client is authenticated with the GitHub App secret when the source config requests it.
func (r *DepotReconciler) matchAgentSourceVersions(ctx context.Context, namespace string, sourceConfig *opendepotv1alpha1.AgentSourceConfig) ([]string, error) {
	useAuthClient := false
	if sourceConfig.GithubClientConfig != nil {
		useAuthClient = sourceConfig.GithubClientConfig.UseAuthenticatedClient
	}

	var githubConfig *opendepotGithub.GithubClientConfig
	if useAuthClient {
		var err error
		githubConfig, err = opendepotGithub.GetGithubApplicationSecret(ctx, r.Client, namespace)
		if err != nil {
			return nil, err
		}
	}

	githubClient, err := opendepotGithub.CreateGithubClient(ctx, useAuthClient, githubConfig)
	if err != nil {
		return nil, err
	}

	tagPrefix := ""
	if sourceConfig.TagPrefix != nil {
		tagPrefix = *sourceConfig.TagPrefix
	}

	return opendepotGithub.ListMatchingTags(ctx, githubClient, sourceConfig.RepoOwner, opendepotGithub.AgentSourceRepoName(sourceConfig), tagPrefix, sourceConfig.VersionConstraints)
}

// SetupWithManager sets up the controller with the Manager.
func (r *DepotReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&opendepotv1alpha1.Depot{}).
		Named("depot").
		Complete(r)
}
