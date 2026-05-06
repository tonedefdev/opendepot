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
	"fmt"
	"io"
	"net/http"
	"slices"
	"time"

	"github.com/go-logr/logr"
	"github.com/hashicorp/go-version"
	"k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
)

const (
	hashicorpReleasesAPI  = "https://api.releases.hashicorp.com"
	openTofuRegistryAPI   = "https://registry.opentofu.org"
	terraformRegistryAPI  = "https://registry.terraform.io"
)

type openTofuModuleVersion struct {
	Version string `json:"version"`
}

type openTofuModuleVersionsItem struct {
	Source   string                  `json:"source"`
	Versions []openTofuModuleVersion `json:"versions"`
}

type openTofuModuleVersionsResponse struct {
	Modules []openTofuModuleVersionsItem `json:"modules"`
}

type openTofuModuleMetadata struct {
	Source string `json:"source"`
}

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
				sourceURL, err := r.fetchOpenTofuModuleSourceURL(ctx, moduleConfig.RepoOwner, *moduleConfig.Name, moduleConfig.Provider)
				if err != nil {
					return ctrl.Result{}, fmt.Errorf("failed to look up source URL for module %q: %w", *moduleConfig.Name, err)
				}
				moduleConfig.RepoUrl = &sourceURL
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

			matchedVersions, err := r.listOpenTofuModuleVersions(ctx, moduleConfig.RepoOwner, *moduleConfig.Name, moduleConfig.Provider, moduleConfig.VersionConstraints)
			if err != nil {
				return ctrl.Result{}, err
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

			matchedVersions, err := r.listHashiCorpProviderVersions(ctx, providerName, providerConfig.VersionConstraints)
			if err != nil {
				return ctrl.Result{}, err
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

	if err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		if err := r.Get(ctx, req.NamespacedName, &depot); err != nil {
			return err
		}

		depot.Status.Modules = managedModules
		depot.Status.Providers = managedProviders
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

// listOpenTofuModuleVersions queries the OpenTofu registry and returns all versions of the given
// module that satisfy the provided version constraints.
func (r *DepotReconciler) listOpenTofuModuleVersions(ctx context.Context, namespace, name, provider, versionConstraints string) ([]string, error) {
	constraints, err := version.NewConstraint(versionConstraints)
	if err != nil {
		return nil, fmt.Errorf("invalid version constraints %q: %w", versionConstraints, err)
	}

	url := fmt.Sprintf("%s/v1/modules/%s/%s/%s/versions", openTofuRegistryAPI, namespace, name, provider)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	httpClient := &http.Client{Timeout: 30 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed for %q: %w", url, err)
	}

	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("failed to read response for %q: %w", url, err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("request to %q failed with status %d", url, resp.StatusCode)
	}

	var result openTofuModuleVersionsResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response from %q: %w", url, err)
	}

	if len(result.Modules) == 0 {
		return nil, fmt.Errorf("no module data returned from %q", url)
	}

	var matched []string
	for _, mv := range result.Modules[0].Versions {
		v, err := version.NewVersion(mv.Version)
		if err != nil {
			r.Log.V(5).Info("Skipping non-semver module version", "version", mv.Version)
			continue
		}

		if !constraints.Check(v) {
			continue
		}

		if slices.Contains(matched, v.String()) {
			continue
		}

		matched = append(matched, v.String())
	}

	return matched, nil
}

// fetchOpenTofuModuleSourceURL queries the Terraform registry for the source repository URL of a module.
// The OpenTofu registry does not expose a module metadata endpoint, so we fall back to registry.terraform.io
// which implements the same Terraform registry protocol and does expose this endpoint.
func (r *DepotReconciler) fetchOpenTofuModuleSourceURL(ctx context.Context, namespace, name, provider string) (string, error) {
	url := fmt.Sprintf("%s/v1/modules/%s/%s/%s", terraformRegistryAPI, namespace, name, provider)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}

	httpClient := &http.Client{Timeout: 30 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed for %q: %w", url, err)
	}

	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return "", fmt.Errorf("failed to read response for %q: %w", url, err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("request to %q failed with status %d", url, resp.StatusCode)
	}

	var meta openTofuModuleMetadata
	if err := json.Unmarshal(body, &meta); err != nil {
		return "", fmt.Errorf("failed to parse response from %q: %w", url, err)
	}

	if meta.Source == "" {
		return "", fmt.Errorf("module metadata from %q has no source URL", url)
	}

	return "https://" + meta.Source, nil
}

// hashicorpReleaseListItem is a single release entry returned by the HashiCorp Releases list endpoint.
type hashicorpReleaseListItem struct {
	Version          string `json:"version"`
	TimestampCreated string `json:"timestamp_created"`
}

// listHashiCorpProviderVersions queries the HashiCorp Releases API and returns all versions
// that satisfy the given version constraints string.
func (r *DepotReconciler) listHashiCorpProviderVersions(ctx context.Context, providerName, versionConstraints string) ([]string, error) {
	constraints, err := version.NewConstraint(versionConstraints)
	if err != nil {
		return nil, fmt.Errorf("invalid version constraints %q: %w", versionConstraints, err)
	}

	candidates := getProviderProductCandidates(providerName)

	var releases []hashicorpReleaseListItem
	var lastErr error

	for _, productName := range candidates {
		releases, lastErr = r.fetchHashiCorpReleaseList(ctx, productName)
		if lastErr == nil {
			break
		}
	}

	if lastErr != nil {
		return nil, fmt.Errorf("failed to list provider versions from HashiCorp Releases API for %q: %w", providerName, lastErr)
	}

	var matched []string
	for _, rel := range releases {
		v, err := version.NewVersion(rel.Version)
		if err != nil {
			r.Log.V(5).Info("Skipping non-semver provider release", "version", rel.Version)
			continue
		}

		if !constraints.Check(v) {
			continue
		}

		if slices.Contains(matched, v.String()) {
			continue
		}

		matched = append(matched, v.String())
	}

	return matched, nil
}

// fetchHashiCorpReleaseList retrieves all release versions from the HashiCorp Releases API for a product,
// paginating through all pages.
func (r *DepotReconciler) fetchHashiCorpReleaseList(ctx context.Context, productName string) ([]hashicorpReleaseListItem, error) {
	var all []hashicorpReleaseListItem
	pageToken := ""

	for {
		url := fmt.Sprintf("%s/v1/releases/%s?limit=20", hashicorpReleasesAPI, productName)
		if pageToken != "" {
			url += "&after=" + pageToken
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}

		httpClient := &http.Client{Timeout: 30 * time.Second}
		resp, err := httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("request failed for %q: %w", url, err)
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("failed to read response for %q: %w", url, err)
		}

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("request to %q failed with status %d", url, resp.StatusCode)
		}

		var page []hashicorpReleaseListItem
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, fmt.Errorf("failed to parse response from %q: %w", url, err)
		}

		all = append(all, page...)

		// The API returns at most `limit` entries per page. When fewer than
		// the limit are returned we have reached the end.
		if len(page) < 20 {
			break
		}

		// Use the timestamp_created of the last item as the cursor for the next page.
		pageToken = page[len(page)-1].TimestampCreated
	}

	return all, nil
}

// getProviderProductCandidates returns ordered HashiCorp product name candidates for a provider.
func getProviderProductCandidates(providerName string) []string {
	if providerName == "" {
		return nil
	}

	seen := map[string]struct{}{}
	var candidates []string

	add := func(s string) {
		if s == "" {
			return
		}
		if _, ok := seen[s]; ok {
			return
		}
		seen[s] = struct{}{}
		candidates = append(candidates, s)
	}

	add(providerName)
	add(fmt.Sprintf("terraform-provider-%s", providerName))

	return candidates
}

// SetupWithManager sets up the controller with the Manager.
func (r *DepotReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&opendepotv1alpha1.Depot{}).
		Named("depot").
		Complete(r)
}
