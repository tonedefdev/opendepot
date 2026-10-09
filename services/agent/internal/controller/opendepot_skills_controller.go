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
	skillsControllerName = "opendepot-skills-controller"
)

// SkillReconciler reconciles a Skill object
type SkillReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=opendepot.defdev.io,resources=skills,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=opendepot.defdev.io,resources=skills/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=opendepot.defdev.io,resources=versions,verbs=get;list;watch;create;update;patch;delete

// Reconcile creates the Versions for each Skill version and removes the Versions that are no longer declared.
func (r *SkillReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	skill := &opendepotv1alpha1.Skill{}
	err := r.Get(ctx, req.NamespacedName, skill)
	if err != nil {
		if k8serr.IsNotFound(err) {
			r.Log.V(5).Info("Skill resource not found. Ignoring since object must be deleted", "skill", req.Name)
			return ctrl.Result{}, nil
		}

		r.Log.Error(err, "Failed to get Skill", "skill", req.Name)
		return ctrl.Result{}, err
	}

	versions := make([]string, 0, len(skill.Spec.Versions))
	for _, version := range skill.Spec.Versions {
		versions = append(versions, version.Version)
	}

	keep := trimAgentVersions(versions, skill.Spec.AgentSourceConfig.VersionHistoryLimit)
	versionObjects, err := reconcileAgentVersions(ctx, r.Client, r.Scheme, r.Log, skill, skillKind, skill.Spec.AgentSourceConfig, keep)
	if err != nil {
		r.Log.Error(err, "Failed to reconcile Skill versions", "skill", skill.Name)
		return ctrl.Result{}, err
	}

	if len(keep) < len(versions) {
		keptVersions := make([]opendepotv1alpha1.SkillVersion, 0, len(keep))
		for _, v := range keep {
			keptVersions = append(keptVersions, opendepotv1alpha1.SkillVersion{Version: v})
		}

		if err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
			if err = r.Get(ctx, req.NamespacedName, skill); err != nil {
				return err
			}

			skill.Spec.Versions = keptVersions
			return r.Update(ctx, skill, &client.UpdateOptions{FieldManager: skillsControllerName})
		}); err != nil {
			r.Log.Error(err, "Failed to trim Skill versions to history limit", "skill", skill.Name)
			return ctrl.Result{}, err
		}
	}

	// If ForceSync is true set it to false now that we have successfully reconciled
	if skill.Spec.ForceSync {
		if err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
			if err = r.Get(ctx, req.NamespacedName, skill); err != nil {
				return err
			}

			skill.Spec.ForceSync = false
			return r.Update(ctx, skill, &client.UpdateOptions{FieldManager: skillsControllerName})
		}); err != nil {
			r.Log.Error(err, "Failed to update Skill", "skill", skill.Name)
			return ctrl.Result{}, err
		}
	}

	versionRefs := make(map[string]*opendepotv1alpha1.SkillVersion, len(keep))
	for _, v := range keep {
		versionObject := versionObjects[v]
		versionRefs[v] = &opendepotv1alpha1.SkillVersion{
			Name:     versionObject.Name,
			FileName: versionObject.Spec.FileName,
			Synced:   true,
			Version:  v,
		}
	}

	if err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		if err = r.Get(ctx, req.NamespacedName, skill); err != nil {
			return err
		}

		skill.Status.VersionRefs = versionRefs
		skill.Status.LatestVersion = latestAgentVersion(keep)
		skill.Status.Synced = true
		skill.Status.SyncStatus = "Successfully synced skill"

		return r.Status().Update(ctx, skill, &client.SubResourceUpdateOptions{
			UpdateOptions: client.UpdateOptions{FieldManager: skillsControllerName},
		})
	}); err != nil {
		r.Log.Error(err, "Failed to update Skill status", "skill", skill.Name)
		return ctrl.Result{}, err
	}

	if err = reconcileAgentVersionRemovals(ctx, r.Client, r.Log, skill, skillKind, keep); err != nil {
		return ctrl.Result{}, err
	}

	r.Log.V(5).Info("Successfully reconciled Skill", "skill", skill.Name)
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *SkillReconciler) SetupWithManager(mgr ctrl.Manager) error {
	versionPredicates := predicate.Funcs{
		// Do not reconcile on any events. This controller is only responsible for creating and deleting the Versions
		// when the Skill spec has changed.
		UpdateFunc: func(e event.UpdateEvent) bool {
			return false
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
		For(&opendepotv1alpha1.Skill{}).
		Owns(&opendepotv1alpha1.Version{}, builder.WithPredicates(versionPredicates)).
		Named(skillsControllerName).
		Complete(r)
}
