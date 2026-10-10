package controller

import (
	"context"
	"errors"
	"time"

	"github.com/google/go-github/v81/github"
	"k8s.io/apimachinery/pkg/api/equality"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	opendepotGithub "github.com/tonedefdev/opendepot/pkg/github"
)

// branchEntry is the part of a Skill or Agent version entry that the head decision reads.
type branchEntry struct {
	Commit       string
	DiscoveredAt *v1.Time
	State        string
}

// newBranchGithubClient returns the GitHub client used for branch head detection. Tests replace it to reach a fake GitHub.
var newBranchGithubClient = func(ctx context.Context, k8sClient client.Client, namespace string, sourceConfig *opendepotv1alpha1.AgentSourceConfig) (*github.Client, error) {
	useAuthClient := false
	if sourceConfig.GithubClientConfig != nil {
		useAuthClient = sourceConfig.GithubClientConfig.UseAuthenticatedClient
	}

	var githubConfig *opendepotGithub.GithubClientConfig
	if useAuthClient {
		var err error
		githubConfig, err = opendepotGithub.GetGithubApplicationSecret(ctx, k8sClient, namespace)
		if err != nil {
			return nil, err
		}
	}

	return opendepotGithub.CreateGithubClient(ctx, useAuthClient, githubConfig)
}

// validateBranchSource returns an error when a branch source must not be reconciled. It checks the controller flag,
// the mutually exclusive fields, and every input used in a GitHub API path, before any GitHub call is made.
func (r *DepotReconciler) validateBranchSource(sourceConfig *opendepotv1alpha1.AgentSourceConfig) error {
	if !r.BranchSourcingEnabled {
		return errors.New("branch sourcing is disabled, start the depot controller with --branch-sourcing-enabled")
	}

	if sourceConfig.VersionConstraints != "" {
		return errors.New("ref and versionConstraints are mutually exclusive")
	}

	if sourceConfig.TagPrefix != nil {
		return errors.New("tagPrefix cannot be used with ref")
	}

	if policy := sourceConfig.BranchPolicy; policy != nil && policy.MinVersionInterval != nil && policy.MinVersionInterval.Duration < 0 {
		return errors.New("branchPolicy.minVersionInterval must not be negative")
	}

	if err := opendepotGithub.ValidateRepoName(sourceConfig.RepoOwner, opendepotGithub.AgentSourceRepoName(sourceConfig)); err != nil {
		return err
	}

	if sourceConfig.RepoUrl != nil {
		if err := opendepotGithub.ValidateRepoURL(sourceConfig.RepoOwner, *sourceConfig.RepoUrl); err != nil {
			return err
		}
	}

	if err := opendepotGithub.ValidateRef(*sourceConfig.Ref); err != nil {
		return err
	}

	_, err := opendepotGithub.CleanSourcePath(sourceConfig.Path)
	return err
}

// branchHead returns the SHA of the newest commit on the source branch that touched the source path.
func (r *DepotReconciler) branchHead(ctx context.Context, namespace string, sourceConfig *opendepotv1alpha1.AgentSourceConfig) (string, error) {
	githubClient, err := newBranchGithubClient(ctx, r.Client, namespace, sourceConfig)
	if err != nil {
		return "", err
	}

	return opendepotGithub.LatestPathCommit(ctx, githubClient, sourceConfig.RepoOwner, opendepotGithub.AgentSourceRepoName(sourceConfig), *sourceConfig.Ref, sourceConfig.Path)
}

// decideBranchEntry reports whether head should be added as a new pending entry. It adds nothing while the newest
// entry is pending, when head is already recorded, or before the interval has passed since the newest entry was
// discovered. When the interval holds the entry back, wait is the time left until it can be added.
func decideBranchEntry(entries []branchEntry, head string, interval time.Duration, now time.Time) (add bool, wait time.Duration) {
	if len(entries) == 0 {
		return true, 0
	}

	newest := entries[0]
	for _, entry := range entries[1:] {
		if entryDiscoveredAt(entry).After(entryDiscoveredAt(newest)) {
			newest = entry
		}
	}

	if newest.State == "Pending" {
		return false, 0
	}

	for _, entry := range entries {
		if entry.Commit == head {
			return false, 0
		}
	}

	elapsed := now.Sub(entryDiscoveredAt(newest))
	if interval > 0 && elapsed < interval {
		return false, interval - elapsed
	}

	return true, 0
}

// entryDiscoveredAt returns when a version entry was discovered. An entry without a discovery time sorts as the oldest.
func entryDiscoveredAt(entry branchEntry) time.Time {
	if entry.DiscoveredAt == nil {
		return time.Time{}
	}

	return entry.DiscoveredAt.Time
}

// reconcileBranchSkill adds a pending entry to the Skill when the source branch has a new head, and creates the Skill
// when it does not exist. Existing entries are never changed because the Agent controller owns version and state.
func (r *DepotReconciler) reconcileBranchSkill(ctx context.Context, namespace string, sourceConfig *opendepotv1alpha1.AgentSourceConfig) (time.Duration, error) {
	head, err := r.branchHead(ctx, namespace, sourceConfig)
	if err != nil {
		return 0, err
	}

	var wait time.Duration
	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		now := time.Now()
		var currentSkill opendepotv1alpha1.Skill
		err := r.Get(ctx, client.ObjectKey{Name: *sourceConfig.Name, Namespace: namespace}, &currentSkill)
		if k8serr.IsNotFound(err) {
			discoveredAt := v1.NewTime(now)
			skill := opendepotv1alpha1.Skill{
				ObjectMeta: v1.ObjectMeta{
					Name:      *sourceConfig.Name,
					Namespace: namespace,
				},
				Spec: opendepotv1alpha1.SkillSpec{
					AgentSourceConfig: *sourceConfig,
					Versions: []opendepotv1alpha1.SkillVersion{{
						Commit:       head,
						DiscoveredAt: &discoveredAt,
						State:        "Pending",
					}},
				},
			}

			wait = 0
			return r.Create(ctx, &skill)
		}

		if err != nil {
			return err
		}

		entries := make([]branchEntry, 0, len(currentSkill.Spec.Versions))
		for _, v := range currentSkill.Spec.Versions {
			entries = append(entries, branchEntry{Commit: v.Commit, DiscoveredAt: v.DiscoveredAt, State: v.State})
		}

		add, remaining := decideBranchEntry(entries, head, sourceConfig.BranchPolicy.EffectiveMinVersionInterval(), now)
		wait = remaining
		if !add && equality.Semantic.DeepEqual(currentSkill.Spec.AgentSourceConfig, *sourceConfig) {
			return nil
		}

		currentSkill.Spec.AgentSourceConfig = *sourceConfig
		if add {
			discoveredAt := v1.NewTime(now)
			currentSkill.Spec.Versions = append(currentSkill.Spec.Versions, opendepotv1alpha1.SkillVersion{
				Commit:       head,
				DiscoveredAt: &discoveredAt,
				State:        "Pending",
			})
		}

		return r.Update(ctx, &currentSkill)
	})

	return wait, err
}

// reconcileBranchAgent adds a pending entry to the Agent when the source branch has a new head, and creates the Agent
// when it does not exist. Existing entries are never changed because the Agent controller owns version and state.
func (r *DepotReconciler) reconcileBranchAgent(ctx context.Context, namespace string, sourceConfig *opendepotv1alpha1.AgentSourceConfig) (time.Duration, error) {
	head, err := r.branchHead(ctx, namespace, sourceConfig)
	if err != nil {
		return 0, err
	}

	var wait time.Duration
	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		now := time.Now()
		var currentAgent opendepotv1alpha1.Agent
		err := r.Get(ctx, client.ObjectKey{Name: *sourceConfig.Name, Namespace: namespace}, &currentAgent)
		if k8serr.IsNotFound(err) {
			discoveredAt := v1.NewTime(now)
			agent := opendepotv1alpha1.Agent{
				ObjectMeta: v1.ObjectMeta{
					Name:      *sourceConfig.Name,
					Namespace: namespace,
				},
				Spec: opendepotv1alpha1.AgentSpec{
					AgentSourceConfig: *sourceConfig,
					Versions: []opendepotv1alpha1.AgentVersion{{
						Commit:       head,
						DiscoveredAt: &discoveredAt,
						State:        "Pending",
					}},
				},
			}

			wait = 0
			return r.Create(ctx, &agent)
		}

		if err != nil {
			return err
		}

		entries := make([]branchEntry, 0, len(currentAgent.Spec.Versions))
		for _, v := range currentAgent.Spec.Versions {
			entries = append(entries, branchEntry{Commit: v.Commit, DiscoveredAt: v.DiscoveredAt, State: v.State})
		}

		add, remaining := decideBranchEntry(entries, head, sourceConfig.BranchPolicy.EffectiveMinVersionInterval(), now)
		wait = remaining
		if !add && equality.Semantic.DeepEqual(currentAgent.Spec.AgentSourceConfig, *sourceConfig) {
			return nil
		}

		currentAgent.Spec.AgentSourceConfig = *sourceConfig
		if add {
			discoveredAt := v1.NewTime(now)
			currentAgent.Spec.Versions = append(currentAgent.Spec.Versions, opendepotv1alpha1.AgentVersion{
				Commit:       head,
				DiscoveredAt: &discoveredAt,
				State:        "Pending",
			})
		}

		return r.Update(ctx, &currentAgent)
	})

	return wait, err
}

// earliestRequeue returns the shorter positive requeue duration. Zero means no requeue.
func earliestRequeue(current, candidate time.Duration) time.Duration {
	if candidate > 0 && (current == 0 || candidate < current) {
		return candidate
	}

	return current
}
