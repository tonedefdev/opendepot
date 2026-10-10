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
	"reflect"

	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
)

// definitionConfigMapKey is the ConfigMap data key holding a Skill or Agent Version's base64 encoded
// definition file, for example SKILL.md or code-review.agent.md.
const definitionConfigMapKey = "definition"

// maxDefinitionBytes caps the raw definition file so the base64 encoded ConfigMap stays under the
// Kubernetes 1 MiB object limit.
const maxDefinitionBytes = 900 * 1024

// upsertDefinitionConfigMap creates or updates the ConfigMap holding a Skill or Agent Version's
// definition file and returns a reference to it. The full file is stored, including any YAML
// frontmatter. The ConfigMap is owned by the Version so that it is garbage collected with it.
func (r *VersionReconciler) upsertDefinitionConfigMap(ctx context.Context, version *opendepotv1alpha1.Version, definitionBytes []byte, fileName string) (*opendepotv1alpha1.DefinitionConfigMapRef, error) {
	if len(definitionBytes) > maxDefinitionBytes {
		return nil, fmt.Errorf("definition file %s is %d bytes, which exceeds the %d byte limit", fileName, len(definitionBytes), maxDefinitionBytes)
	}

	cmName := version.Name + "-definition"
	parentName := branchParentName(version)

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cmName,
			Namespace: version.Namespace,
		},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, cm, func() error {
		labels := map[string]string{
			"opendepot.defdev.io/namespace": version.Namespace,
			"app.kubernetes.io/managed-by":  opendepotControllerName,
		}

		if parentName != "" {
			labels[branchParentLabel(version.Spec.Type)] = parentName
		}

		cm.Labels = labels

		if cm.Data == nil {
			cm.Data = map[string]string{}
		}

		cm.Data[definitionConfigMapKey] = base64.StdEncoding.EncodeToString(definitionBytes)

		return controllerutil.SetControllerReference(version, cm, r.Scheme)
	})
	if err != nil {
		return nil, fmt.Errorf("failed to upsert definition configmap %s: %w", cmName, err)
	}

	return &opendepotv1alpha1.DefinitionConfigMapRef{
		Name:     cmName,
		Key:      definitionConfigMapKey,
		FileName: fileName,
	}, nil
}

// definitionConfigMapExists reports whether the ConfigMap referenced by a Version's status still exists.
// It returns false when the Version has no definition reference.
func (r *VersionReconciler) definitionConfigMapExists(ctx context.Context, version *opendepotv1alpha1.Version) (bool, error) {
	ref := version.Status.DefinitionConfigMapRef
	if ref == nil {
		return false, nil
	}

	cm := &corev1.ConfigMap{}
	if err := r.Get(ctx, client.ObjectKey{Name: ref.Name, Namespace: version.Namespace}, cm); err != nil {
		if k8serr.IsNotFound(err) {
			return false, nil
		}

		return false, fmt.Errorf("failed to get definition configmap %s: %w", ref.Name, err)
	}

	return true, nil
}

// persistDefinitionRef records the definition ConfigMap reference on the Version status. It writes only
// that field and skips the update when the reference is already current.
func (r *VersionReconciler) persistDefinitionRef(ctx context.Context, version *opendepotv1alpha1.Version, ref *opendepotv1alpha1.DefinitionConfigMapRef) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		currentVersion := &opendepotv1alpha1.Version{}
		if err := r.Get(ctx, client.ObjectKeyFromObject(version), currentVersion); err != nil {
			return err
		}

		if reflect.DeepEqual(currentVersion.Status.DefinitionConfigMapRef, ref) {
			return nil
		}

		currentVersion.Status.DefinitionConfigMapRef = ref

		return r.Status().Update(ctx, currentVersion, &client.SubResourceUpdateOptions{
			UpdateOptions: client.UpdateOptions{FieldManager: opendepotControllerName},
		})
	})
}
