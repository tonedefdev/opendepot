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
	"time"

	"github.com/go-logr/logr"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	"github.com/tonedefdev/opendepot/services/version/internal/policy"
)

const (
	scanPolicyControllerName = "opendepot-scanpolicies-controller"
	scanPolicyConditionReady = "Ready"
)

// ScanPolicyReconciler reports the observed blast radius of a ScanPolicy. It performs no
// enforcement of its own — enforcement happens in VersionReconciler at scan time — so this
// reconciler only ever writes to the ScanPolicy status subresource.
type ScanPolicyReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=opendepot.defdev.io,resources=scanpolicies,verbs=get;list;watch
// +kubebuilder:rbac:groups=opendepot.defdev.io,resources=scanpolicies/status,verbs=get;update;patch

func (r *ScanPolicyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	scanPolicy := &opendepotv1alpha1.ScanPolicy{}
	if err := r.Get(ctx, req.NamespacedName, scanPolicy); err != nil {
		if k8serr.IsNotFound(err) {
			r.Log.V(5).Info("scanpolicy resource not found. Ignoring since object must be deleted", "scanPolicy", req.Name)
			return ctrl.Result{}, nil
		}

		r.Log.Error(err, "Failed to get scanpolicy", "scanPolicy", req.Name)

		return ctrl.Result{}, err
	}

	policies := &opendepotv1alpha1.ScanPolicyList{}
	if err := r.List(ctx, policies, client.InNamespace(req.Namespace)); err != nil {
		r.Log.Error(err, "Failed to list ScanPolicies", "namespace", req.Namespace)
		return ctrl.Result{}, err
	}

	versions := &opendepotv1alpha1.VersionList{}
	if err := r.List(ctx, versions, client.InNamespace(req.Namespace)); err != nil {
		r.Log.Error(err, "Failed to list Versions", "namespace", req.Namespace)
		return ctrl.Result{}, err
	}

	now := time.Now()
	desired := r.observe(scanPolicy, policies.Items, versions.Items, now)

	if apiequality.Semantic.DeepEqual(scanPolicy.Status, desired) {
		r.Log.V(5).Info("ScanPolicy status already up to date", "scanPolicy", scanPolicy.Name)
		return ctrl.Result{RequeueAfter: r.nextExpiry(scanPolicy, now)}, nil
	}

	if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		current := &opendepotv1alpha1.ScanPolicy{}
		if err := r.Get(ctx, req.NamespacedName, current); err != nil {
			return err
		}

		current.Status = desired

		return r.Status().Update(ctx, current, &client.SubResourceUpdateOptions{
			UpdateOptions: client.UpdateOptions{FieldManager: scanPolicyControllerName},
		})
	}); err != nil {
		r.Log.Error(err, "Failed to update ScanPolicy status", "scanPolicy", scanPolicy.Name)
		return ctrl.Result{}, err
	}

	r.Log.V(5).Info("ScanPolicy status updated",
		"scanPolicy", scanPolicy.Name,
		"matchedVersions", desired.MatchedVersions,
		"activeExemptions", desired.ActiveExemptions,
		"supersededBy", desired.SupersededBy)

	return ctrl.Result{RequeueAfter: r.nextExpiry(scanPolicy, now)}, nil
}

// observe computes the status this ScanPolicy should report. It reuses the same selection
// logic the version controller enforces with, so the reported blast radius cannot drift
// from the policy that actually applies.
func (r *ScanPolicyReconciler) observe(
	scanPolicy *opendepotv1alpha1.ScanPolicy,
	policies []opendepotv1alpha1.ScanPolicy,
	versions []opendepotv1alpha1.Version,
	now time.Time,
) opendepotv1alpha1.ScanPolicyStatus {
	active, expired := policy.CountExemptions(scanPolicy, now)
	status := opendepotv1alpha1.ScanPolicyStatus{
		ActiveExemptions:  active,
		ExpiredExemptions: expired,
		Conditions:        scanPolicy.Status.Conditions,
	}

	var wins int
	var shadowedBy string

	for i := range versions {
		matched, err := policy.Matches(scanPolicy, &versions[i])
		if err != nil {
			r.Log.Error(err, "ScanPolicy has an invalid selector", "scanPolicy", scanPolicy.Name)

			meta.SetStatusCondition(&status.Conditions, metav1.Condition{
				Type:               scanPolicyConditionReady,
				Status:             metav1.ConditionFalse,
				Reason:             "InvalidSelector",
				Message:            err.Error(),
				ObservedGeneration: scanPolicy.Generation,
			})

			return status
		}

		if !matched {
			continue
		}

		status.MatchedVersions++

		winner, _ := policy.Select(policies, &versions[i])
		if winner != nil && winner.Name == scanPolicy.Name {
			wins++
			continue
		}

		if winner != nil && shadowedBy == "" {
			shadowedBy = winner.Name
		}
	}

	condition := metav1.Condition{
		Type:               scanPolicyConditionReady,
		Status:             metav1.ConditionTrue,
		Reason:             "Active",
		Message:            "The policy governs at least one Version",
		ObservedGeneration: scanPolicy.Generation,
	}

	switch {
	case status.MatchedVersions == 0:
		condition.Reason = "NoMatchingVersions"
		condition.Message = "The policy matches no Version in this namespace"
	case wins == 0:
		status.SupersededBy = shadowedBy
		condition.Reason = "Superseded"
		condition.Message = "Every matched Version is governed by a higher priority policy"
	}

	meta.SetStatusCondition(&status.Conditions, condition)

	return status
}

// nextExpiry returns the delay until the earliest future exemption expiry so that the
// reported exemption counts age out on their own. A zero result disables the requeue.
func (r *ScanPolicyReconciler) nextExpiry(scanPolicy *opendepotv1alpha1.ScanPolicy, now time.Time) time.Duration {
	var earliest *time.Time
	for _, exemption := range scanPolicy.Spec.Exemptions {
		if exemption.Expires == nil || !exemption.Expires.Time.After(now) {
			continue
		}

		if earliest == nil || exemption.Expires.Time.Before(*earliest) {
			expiry := exemption.Expires.Time
			earliest = &expiry
		}
	}

	if earliest == nil {
		return 0
	}

	return earliest.Sub(now)
}

// SetupWithManager sets up the controller with the Manager.
func (r *ScanPolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&opendepotv1alpha1.ScanPolicy{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		// A Version being created, deleted or respecced changes the matched count. Status
		// writes are filtered out: without this predicate every Version status update made
		// by VersionReconciler would bounce straight back into this controller.
		Watches(
			&opendepotv1alpha1.Version{},
			handler.EnqueueRequestsFromMapFunc(r.scanPoliciesInNamespace),
			builder.WithPredicates(predicate.GenerationChangedPredicate{}),
		).
		// Shadowing is relational: adding, respeccing or removing one ScanPolicy can change
		// which policy wins for a Version, and therefore the supersededBy of every other
		// policy in the namespace. The primary watch above only enqueues the policy that
		// actually changed, so fan the event out to its peers as well.
		Watches(
			&opendepotv1alpha1.ScanPolicy{},
			handler.EnqueueRequestsFromMapFunc(r.scanPoliciesInNamespace),
			builder.WithPredicates(predicate.GenerationChangedPredicate{}),
		).
		Named(scanPolicyControllerName).
		Complete(r)
}

// scanPoliciesInNamespace maps a Version or ScanPolicy event onto every ScanPolicy in the same namespace.
func (r *ScanPolicyReconciler) scanPoliciesInNamespace(ctx context.Context, obj client.Object) []reconcile.Request {
	policies := &opendepotv1alpha1.ScanPolicyList{}
	if err := r.List(ctx, policies, client.InNamespace(obj.GetNamespace())); err != nil {
		r.Log.Error(err, "Failed to list ScanPolicies for watched object event",
			"object", obj.GetName(), "namespace", obj.GetNamespace())

		return nil
	}

	requests := make([]reconcile.Request, 0, len(policies.Items))
	for _, scanPolicy := range policies.Items {
		requests = append(requests, reconcile.Request{
			NamespacedName: k8stypes.NamespacedName{Name: scanPolicy.Name, Namespace: scanPolicy.Namespace},
		})
	}

	return requests
}
