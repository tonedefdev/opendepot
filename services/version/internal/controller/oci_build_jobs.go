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
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/opencontainers/go-digest"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	"github.com/tonedefdev/opendepot/pkg/ocibuild"
)

const (
	// ociBuildKitContainerName and ociBuildUploadContainerName are the names of the containers that report a build's
	// failure and its manifest digest.
	ociBuildKitContainerName    = "buildkit"
	ociBuildUploadContainerName = "upload"
	// ociBuildMaxMessageBytes is the largest message that status keeps. The tail of a log is kept, since the end holds
	// the error.
	ociBuildMaxMessageBytes = 1024
	// ociBuildTimeoutReason is the Job condition reason when activeDeadlineSeconds expires.
	ociBuildTimeoutReason = "DeadlineExceeded"
	// ociBuildOOMReason is the container termination reason when the kernel kills a container for memory.
	ociBuildOOMReason = "OOMKilled"
	// ociBuildUnschedulableReason is the PodScheduled condition reason when no node can run the Pod.
	ociBuildUnschedulableReason = "Unschedulable"
	// ociBuildJobAPIVersion and ociBuildJobKind identify a Job in owner references.
	ociBuildJobAPIVersion = "batch/v1"
	ociBuildJobKind       = "Job"
)

// activeOCIBuildJobs returns the number of build Jobs in the builds namespace that have not finished.
func (r *VersionReconciler) activeOCIBuildJobs(ctx context.Context) (int, error) {
	jobs := &batchv1.JobList{}
	if err := r.List(ctx, jobs, client.InNamespace(r.OCIBuilds.Namespace), client.MatchingLabels{ocibuild.LabelBuild: "true"}); err != nil {
		return 0, err
	}

	active := 0
	for i := range jobs.Items {
		if !jobHasCondition(&jobs.Items[i], batchv1.JobComplete) && !jobHasCondition(&jobs.Items[i], batchv1.JobFailed) {
			active++
		}
	}

	return active, nil
}

// listOCIBuildJobs returns the Jobs of one build, keyed by name.
func (r *VersionReconciler) listOCIBuildJobs(ctx context.Context, buildID string) (map[string]*batchv1.Job, error) {
	list := &batchv1.JobList{}
	if err := r.List(ctx, list, client.InNamespace(r.OCIBuilds.Namespace), client.MatchingLabels(ocibuild.JobLabels(buildID))); err != nil {
		return nil, fmt.Errorf("list build Jobs: %w", err)
	}

	jobs := make(map[string]*batchv1.Job, len(list.Items))
	for i := range list.Items {
		jobs[list.Items[i].Name] = &list.Items[i]
	}

	return jobs, nil
}

// getOCIBuildSecret reads the Secret of a build. It reads through the API reader when one is set, because the cache must
// not watch Secrets across the cluster.
func (r *VersionReconciler) getOCIBuildSecret(ctx context.Context, buildID string) (*corev1.Secret, error) {
	var reader client.Reader = r.Client
	if r.APIReader != nil {
		reader = r.APIReader
	}

	secret := &corev1.Secret{}
	if err := reader.Get(ctx, types.NamespacedName{Namespace: r.OCIBuilds.Namespace, Name: ocibuild.SecretName(buildID)}, secret); err != nil {
		return nil, err
	}

	return secret, nil
}

// readOCIBuildTokens returns the two tokens that the build Secret holds. The values are never logged.
func readOCIBuildTokens(secret *corev1.Secret) (ociBuildTokens, error) {
	fetch := string(secret.Data[ocibuild.FetchTokenKey])
	upload := string(secret.Data[ocibuild.UploadTokenKey])
	if fetch == "" || upload == "" || fetch == upload {
		return ociBuildTokens{}, errors.New("the build Secret does not hold the build tokens")
	}

	return ociBuildTokens{fetch: fetch, upload: upload}, nil
}

// ensureOCIBuildSecretOwners makes the owner references of the build Secret match the Jobs that exist. Kubernetes
// removes the Secret when its last owner Job is gone.
func (r *VersionReconciler) ensureOCIBuildSecretOwners(ctx context.Context, buildID string, jobs map[string]*batchv1.Job) error {
	secret, err := r.getOCIBuildSecret(ctx, buildID)
	if err != nil {
		return fmt.Errorf("read build secret: %w", err)
	}

	names := make([]string, 0, len(jobs))
	for name := range jobs {
		names = append(names, name)
	}

	slices.Sort(names)
	desired := make([]metav1.OwnerReference, 0, len(names))
	for _, name := range names {
		job := jobs[name]
		desired = append(desired, metav1.OwnerReference{
			APIVersion: ociBuildJobAPIVersion,
			Kind:       ociBuildJobKind,
			Name:       job.Name,
			UID:        job.UID,
		})
	}

	if sameOwnerUIDs(secret.OwnerReferences, desired) {
		return nil
	}

	secret.OwnerReferences = desired
	if err := r.Update(ctx, secret); err != nil {
		return fmt.Errorf("set build secret owners: %w", err)
	}

	return nil
}

// sameOwnerUIDs reports whether the two sets of owner references hold the same UIDs.
func sameOwnerUIDs(current, desired []metav1.OwnerReference) bool {
	if len(current) != len(desired) {
		return false
	}

	for _, want := range desired {
		if !slices.ContainsFunc(current, func(have metav1.OwnerReference) bool {
			return have.UID == want.UID
		}) {
			return false
		}
	}

	return true
}

// deleteOCIBuildObjects deletes the Jobs of a build with background propagation, then its Secret. A Job or Secret that
// is already gone is not an error.
func (r *VersionReconciler) deleteOCIBuildObjects(ctx context.Context, buildID string) error {
	jobs, err := r.listOCIBuildJobs(ctx, buildID)
	if err != nil {
		return err
	}

	for _, job := range jobs {
		if err := r.Delete(ctx, job, client.PropagationPolicy(metav1.DeletePropagationBackground)); client.IgnoreNotFound(err) != nil {
			return fmt.Errorf("delete build Job %s: %w", job.Name, err)
		}
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: r.OCIBuilds.Namespace, Name: ocibuild.SecretName(buildID)},
	}
	if err := r.Delete(ctx, secret); client.IgnoreNotFound(err) != nil {
		return fmt.Errorf("delete build secret: %w", err)
	}

	return nil
}

// buildPodsFor returns the Pods that a build Job controls.
func (r *VersionReconciler) buildPodsFor(ctx context.Context, job *batchv1.Job) ([]corev1.Pod, error) {
	pods := &corev1.PodList{}
	if err := r.List(ctx, pods, client.InNamespace(job.Namespace), client.MatchingLabels{
		ocibuild.LabelBuild: "true",
		ocibuild.BuildIDKey: job.Labels[ocibuild.BuildIDKey],
	}); err != nil {
		return nil, fmt.Errorf("list build Pods: %w", err)
	}

	var owned []corev1.Pod
	for i := range pods.Items {
		if metav1.IsControlledBy(&pods.Items[i], job) {
			owned = append(owned, pods.Items[i])
		}
	}

	return owned, nil
}

// platformFailure returns the message for a failed platform. A deadline, an OOM kill, or the sanitized tail of the
// BuildKit termination message is reported. The message is never rendered as HTML.
func (r *VersionReconciler) platformFailure(ctx context.Context, job *batchv1.Job, platform ocibuild.Platform) (string, error) {
	reason, message := jobFailure(job)
	if reason == ociBuildTimeoutReason {
		return fmt.Sprintf("build for %s exceeded its timeout", platform), nil
	}

	pods, err := r.buildPodsFor(ctx, job)
	if err != nil {
		return "", err
	}

	for _, pod := range pods {
		for _, status := range podContainerStatuses(pod) {
			if status.State.Terminated == nil {
				continue
			}

			if status.Name == ociBuildKitContainerName && status.State.Terminated.Reason == ociBuildOOMReason {
				return fmt.Sprintf("BuildKit was OOM-killed for %s; raise the build memory limit", platform), nil
			}

			if (status.Name == ociBuildKitContainerName || status.Name == ociBuildUploadContainerName) && status.State.Terminated.ExitCode != 0 {
				if text := truncateOCIMessage(sanitizeOCIMessage(status.State.Terminated.Message)); text != "" {
					return fmt.Sprintf("build for %s failed: %s", platform, text), nil
				}
			}
		}
	}

	if message == "" {
		message = reason
	}

	if message == "" {
		message = "unknown error"
	}

	return fmt.Sprintf("build for %s failed: %s", platform, truncateOCIMessage(sanitizeOCIMessage(message))), nil
}

// uploadedManifestDigest returns the manifest digest that the upload container reported in its termination message.
func (r *VersionReconciler) uploadedManifestDigest(ctx context.Context, job *batchv1.Job) (string, error) {
	pods, err := r.buildPodsFor(ctx, job)
	if err != nil {
		return "", err
	}

	for _, pod := range pods {
		for _, status := range podContainerStatuses(pod) {
			if status.Name != ociBuildUploadContainerName || status.State.Terminated == nil || status.State.Terminated.ExitCode != 0 {
				continue
			}

			var reported struct {
				ManifestDigest string `json:"manifestDigest"`
			}

			if err := json.Unmarshal([]byte(status.State.Terminated.Message), &reported); err != nil {
				return "", errors.New("the upload result is not valid")
			}

			d, err := digest.Parse(reported.ManifestDigest)
			if err != nil || d.Algorithm() != digest.SHA256 {
				return "", errors.New("the upload result holds no manifest digest")
			}

			return d.String(), nil
		}
	}

	return "", errors.New("the upload result is no longer available")
}

// jobUnschedulable reports whether a Pod of the Job cannot be scheduled on any node, such as when no node has the
// platform's architecture.
func (r *VersionReconciler) jobUnschedulable(ctx context.Context, job *batchv1.Job) (bool, error) {
	pods, err := r.buildPodsFor(ctx, job)
	if err != nil {
		return false, err
	}

	for _, pod := range pods {
		if pod.Status.Phase != corev1.PodPending {
			continue
		}

		for _, cond := range pod.Status.Conditions {
			if cond.Type == corev1.PodScheduled && cond.Status == corev1.ConditionFalse && cond.Reason == ociBuildUnschedulableReason {
				return true, nil
			}
		}
	}

	return false, nil
}

// jobHasCondition reports whether the Job has the condition with status True.
func jobHasCondition(job *batchv1.Job, condType batchv1.JobConditionType) bool {
	for _, cond := range job.Status.Conditions {
		if cond.Type == condType && cond.Status == corev1.ConditionTrue {
			return true
		}
	}

	return false
}

// podContainerStatuses returns the status of the init containers and the containers of a Pod. BuildKit runs as an init
// container, and the upload helper runs as the main container.
func podContainerStatuses(pod corev1.Pod) []corev1.ContainerStatus {
	statuses := make([]corev1.ContainerStatus, 0, len(pod.Status.InitContainerStatuses)+len(pod.Status.ContainerStatuses))
	statuses = append(statuses, pod.Status.InitContainerStatuses...)
	return append(statuses, pod.Status.ContainerStatuses...)
}

// jobFailure returns the reason and message of the Job's Failed condition.
func jobFailure(job *batchv1.Job) (string, string) {
	for _, cond := range job.Status.Conditions {
		if cond.Type == batchv1.JobFailed && cond.Status == corev1.ConditionTrue {
			return cond.Reason, cond.Message
		}
	}

	return "", ""
}

// sanitizeOCIMessage removes control characters, so that text from a build log cannot change the layout of a status
// message.
func sanitizeOCIMessage(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}

		return r
	}, s)
}

// truncateOCIMessage keeps the last ociBuildMaxMessageBytes of s, starting on a rune boundary.
func truncateOCIMessage(s string) string {
	if len(s) <= ociBuildMaxMessageBytes {
		return s
	}

	s = s[len(s)-ociBuildMaxMessageBytes:]
	for len(s) > 0 && !utf8.RuneStart(s[0]) {
		s = s[1:]
	}

	return s
}

// cleanupOCIBuildsOnDelete deletes the Jobs and Secrets of the builds of a Version that is being deleted. A Job belongs to
// a Version when its annotations name the Version.
func (r *VersionReconciler) cleanupOCIBuildsOnDelete(ctx context.Context, version *opendepotv1alpha1.Version) error {
	if !r.OCIBuilds.Enabled {
		return nil
	}

	jobs := &batchv1.JobList{}
	if err := r.List(ctx, jobs, client.InNamespace(r.OCIBuilds.Namespace), client.MatchingLabels{ocibuild.LabelBuild: "true"}); err != nil {
		return fmt.Errorf("list build Jobs: %w", err)
	}

	buildIDs := map[string]bool{}
	for i := range jobs.Items {
		job := &jobs.Items[i]
		if job.Annotations[ocibuild.AnnotationVersionNamespace] != version.Namespace ||
			job.Annotations[ocibuild.AnnotationVersionName] != version.Name {
			continue
		}

		buildIDs[job.Labels[ocibuild.BuildIDKey]] = true
	}

	if version.Status.OCIBuild != nil {
		buildIDs[version.Status.OCIBuild.BuildID] = true
	}

	for buildID := range buildIDs {
		if buildID == "" {
			continue
		}

		if err := r.deleteOCIBuildObjects(ctx, buildID); err != nil {
			return err
		}
	}

	return nil
}

// versionForOCIBuildObject maps a build Job or Pod to the Version that owns it, which the annotations name. Label values
// are too short to hold a namespace and a name, so the annotations are read.
func versionForOCIBuildObject(_ context.Context, obj client.Object) []reconcile.Request {
	namespace := obj.GetAnnotations()[ocibuild.AnnotationVersionNamespace]
	name := obj.GetAnnotations()[ocibuild.AnnotationVersionName]
	if namespace == "" || name == "" {
		return nil
	}

	return []reconcile.Request{{NamespacedName: types.NamespacedName{Namespace: namespace, Name: name}}}
}
