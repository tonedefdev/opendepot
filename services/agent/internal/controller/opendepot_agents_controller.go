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
	"slices"

	"github.com/go-logr/logr"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
)

const (
	agentsControllerName = "opendepot-agents-controller"
)

// AgentReconciler reconciles an Agent object
type AgentReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=opendepot.defdev.io,resources=agents,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=opendepot.defdev.io,resources=agents/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=opendepot.defdev.io,resources=versions,verbs=get;list;watch;create;update;patch;delete

// Reconcile creates the Versions for each Agent version and removes the Versions that are no longer declared.
func (r *AgentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	agent := &opendepotv1alpha1.Agent{}
	err := r.Get(ctx, req.NamespacedName, agent)
	if err != nil {
		if k8serr.IsNotFound(err) {
			r.Log.V(5).Info("Agent resource not found. Ignoring since object must be deleted", "agent", req.Name)
			return ctrl.Result{}, nil
		}

		r.Log.Error(err, "Failed to get Agent", "agent", req.Name)
		return ctrl.Result{}, err
	}

	sourceConfig := agent.Spec.AgentSourceConfig
	configName := agentConfigName(agent, sourceConfig)
	branch := sourceConfig.Ref != nil

	entries := make([]opendepotv1alpha1.AgentVersion, 0, len(agent.Spec.Versions))
	for _, version := range agent.Spec.Versions {
		entries = append(entries, version)
	}

	original := slices.Clone(entries)
	var branchVersions map[string]*opendepotv1alpha1.Version
	if branch {
		branchVersions, err = getBranchVersions(ctx, r.Client, agent, agentKindDef, configName, entries)
		if err != nil {
			r.Log.Error(err, "Failed to get Agent branch versions", "agent", agent.Name)
			return ctrl.Result{}, err
		}

		syncBranchEntries(agentKindDef, configName, entries, branchVersions)
	}

	limit := agentVersionHistoryLimit(sourceConfig)
	keep := trimAgentEntries(entries, limit, branch)
	versionObjects, err := reconcileAgentVersions(ctx, r.Client, r.Scheme, r.Log, agent, agentKindDef, sourceConfig, keep)
	if err != nil {
		r.Log.Error(err, "Failed to reconcile Agent versions", "agent", agent.Name)
		return ctrl.Result{}, err
	}

	if !agentEntriesEqual(original, keep) {
		if err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
			if err = r.Get(ctx, req.NamespacedName, agent); err != nil {
				return err
			}

			// Recompute from the latest spec so entries added while reconciling are not overwritten.
			latest := make([]opendepotv1alpha1.AgentVersion, 0, len(agent.Spec.Versions))
			for _, version := range agent.Spec.Versions {
				latest = append(latest, version)
			}

			syncBranchEntries(agentKindDef, configName, latest, branchVersions)
			keep = trimAgentEntries(latest, limit, branch)

			agent.Spec.Versions = make([]opendepotv1alpha1.AgentVersion, 0, len(keep))
			for _, entry := range keep {
				agent.Spec.Versions = append(agent.Spec.Versions, entry)
			}

			return r.Update(ctx, agent, &client.UpdateOptions{FieldManager: agentsControllerName})
		}); err != nil {
			r.Log.Error(err, "Failed to update Agent versions", "agent", agent.Name)
			return ctrl.Result{}, err
		}
	}

	// If ForceSync is true set it to false now that we have successfully reconciled
	if agent.Spec.ForceSync {
		if err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
			if err = r.Get(ctx, req.NamespacedName, agent); err != nil {
				return err
			}

			agent.Spec.ForceSync = false
			return r.Update(ctx, agent, &client.UpdateOptions{FieldManager: agentsControllerName})
		}); err != nil {
			r.Log.Error(err, "Failed to update Agent", "agent", agent.Name)
			return ctrl.Result{}, err
		}
	}

	versionRefs := make(map[string]*opendepotv1alpha1.AgentVersion, len(keep))
	assigned := make([]string, 0, len(keep))
	for _, entry := range keep {
		versionObject, ok := versionObjects[agentEntryVersionName(agentKindDef, configName, entry, branch)]
		if !ok || entry.Version == "" {
			continue
		}

		assigned = append(assigned, entry.Version)
		versionRefs[entry.Version] = &opendepotv1alpha1.AgentVersion{
			Name:     versionObject.Name,
			FileName: versionObject.Spec.FileName,
			Synced:   true,
			Version:  entry.Version,
			Commit:   entry.Commit,
		}
	}

	if err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		if err = r.Get(ctx, req.NamespacedName, agent); err != nil {
			return err
		}

		agent.Status.VersionRefs = versionRefs
		agent.Status.LatestVersion = latestAgentVersion(assigned)
		agent.Status.Synced = true
		agent.Status.SyncStatus = "Successfully synced agent"

		return r.Status().Update(ctx, agent, &client.SubResourceUpdateOptions{
			UpdateOptions: client.UpdateOptions{FieldManager: agentsControllerName},
		})
	}); err != nil {
		r.Log.Error(err, "Failed to update Agent status", "agent", agent.Name)
		return ctrl.Result{}, err
	}

	if err = reconcileAgentVersionRemovals(ctx, r.Client, r.Log, agent, agentKindDef, agentVersionNameSet(versionObjects)); err != nil {
		return ctrl.Result{}, err
	}

	r.Log.V(5).Info("Successfully reconciled Agent", "agent", agent.Name)
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *AgentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	versionPredicates := predicate.Funcs{
		// Only reconcile when a branch Version is assigned a semantic version or rejected so that the Agent can copy
		// the result back to its entry. Other events are ignored.
		UpdateFunc: func(e event.UpdateEvent) bool {
			return branchVersionChanged(e)
		},

		CreateFunc: func(e event.CreateEvent) bool {
			return false
		},

		DeleteFunc: func(e event.DeleteEvent) bool {
			return false
		},

		GenericFunc: func(e event.GenericEvent) bool {
			return false
		},
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&opendepotv1alpha1.Agent{}).
		Owns(&opendepotv1alpha1.Version{}, builder.WithPredicates(versionPredicates)).
		Named(agentsControllerName).
		Complete(r)
}
