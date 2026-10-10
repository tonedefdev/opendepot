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
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
)

func newDefinitionFixture(t *testing.T, interceptors interceptor.Funcs) (*VersionReconciler, *opendepotv1alpha1.Version) {
	t.Helper()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme core: %v", err)
	}

	if err := opendepotv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}

	parent := "code-review"
	version := &opendepotv1alpha1.Version{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "code-review-abc1234",
			Namespace: "default",
		},
		Spec: opendepotv1alpha1.VersionSpec{
			Type:         opendepotv1alpha1.OpenDepotSkill,
			SourceCommit: "abc1234",
			AgentSourceRef: &opendepotv1alpha1.AgentSourceConfig{
				Name: &parent,
			},
		},
	}

	reconciler := &VersionReconciler{
		Client: fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(version).
			WithStatusSubresource(&opendepotv1alpha1.Version{}).
			WithInterceptorFuncs(interceptors).
			Build(),
		Scheme: scheme,
	}

	return reconciler, version
}

func TestUpsertDefinitionConfigMapCreatesAndUpdates(t *testing.T) {
	reconciler, version := newDefinitionFixture(t, interceptor.Funcs{})
	ctx := context.Background()

	ref, err := reconciler.upsertDefinitionConfigMap(ctx, version, []byte("---\nname: code-review\n---\nbody"), "SKILL.md")
	if err != nil {
		t.Fatalf("upsertDefinitionConfigMap create: %v", err)
	}

	if ref.Name != "code-review-abc1234-definition" || ref.Key != definitionConfigMapKey || ref.FileName != "SKILL.md" {
		t.Fatalf("unexpected ref: %+v", ref)
	}

	cm := &corev1.ConfigMap{}
	if err := reconciler.Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: version.Namespace}, cm); err != nil {
		t.Fatalf("get configmap: %v", err)
	}

	decoded, err := base64.StdEncoding.DecodeString(cm.Data[definitionConfigMapKey])
	if err != nil {
		t.Fatalf("decode data: %v", err)
	}

	if string(decoded) != "---\nname: code-review\n---\nbody" {
		t.Fatalf("unexpected stored definition: %q", decoded)
	}

	if cm.Labels["opendepot.defdev.io/skill"] != "code-review" {
		t.Fatalf("expected skill label set to parent name, got %v", cm.Labels)
	}

	if cm.Labels["app.kubernetes.io/managed-by"] != opendepotControllerName {
		t.Fatalf("expected managed-by label, got %v", cm.Labels)
	}

	if len(cm.OwnerReferences) != 1 || cm.OwnerReferences[0].Name != version.Name || cm.OwnerReferences[0].Kind != "Version" {
		t.Fatalf("expected owner reference to the Version, got %+v", cm.OwnerReferences)
	}

	if _, err := reconciler.upsertDefinitionConfigMap(ctx, version, []byte("updated"), "SKILL.md"); err != nil {
		t.Fatalf("upsertDefinitionConfigMap update: %v", err)
	}

	if err := reconciler.Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: version.Namespace}, cm); err != nil {
		t.Fatalf("get configmap after update: %v", err)
	}

	decoded, _ = base64.StdEncoding.DecodeString(cm.Data[definitionConfigMapKey])
	if string(decoded) != "updated" {
		t.Fatalf("expected update to replace the definition, got %q", decoded)
	}
}

func TestUpsertDefinitionConfigMapLabelsAreValidForBranchVersionNames(t *testing.T) {
	reconciler, version := newDefinitionFixture(t, interceptor.Funcs{})
	ctx := context.Background()

	// Branch Version names embed a 40 character commit SHA and exceed the 63 character label value limit.
	version.Name = "skill-acquire-codebase-knowledge-b8f38227480c9f3fe04d6496d4fcab9a880e5a15"

	ref, err := reconciler.upsertDefinitionConfigMap(ctx, version, []byte("body"), "SKILL.md")
	if err != nil {
		t.Fatalf("upsertDefinitionConfigMap with branch version name: %v", err)
	}

	cm := &corev1.ConfigMap{}
	if err := reconciler.Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: version.Namespace}, cm); err != nil {
		t.Fatalf("get configmap: %v", err)
	}

	for key, value := range cm.Labels {
		if errs := validation.IsValidLabelValue(value); len(errs) > 0 {
			t.Fatalf("label %s has invalid value %q: %v", key, value, errs)
		}
	}
}

func TestUpsertDefinitionConfigMapIsIdempotent(t *testing.T) {
	reconciler, version := newDefinitionFixture(t, interceptor.Funcs{})
	ctx := context.Background()

	first, err := reconciler.upsertDefinitionConfigMap(ctx, version, []byte("same"), "agent.md")
	if err != nil {
		t.Fatalf("first upsert: %v", err)
	}

	second, err := reconciler.upsertDefinitionConfigMap(ctx, version, []byte("same"), "agent.md")
	if err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	if *first != *second {
		t.Fatalf("expected identical refs, got %+v and %+v", first, second)
	}

	list := &corev1.ConfigMapList{}
	if err := reconciler.List(ctx, list, client.InNamespace(version.Namespace)); err != nil {
		t.Fatalf("list configmaps: %v", err)
	}

	if len(list.Items) != 1 {
		t.Fatalf("expected one configmap, got %d", len(list.Items))
	}
}

func TestUpsertDefinitionConfigMapRejectsOversizedEntry(t *testing.T) {
	reconciler, version := newDefinitionFixture(t, interceptor.Funcs{})
	ctx := context.Background()

	oversized := []byte(strings.Repeat("a", maxDefinitionBytes+1))
	if _, err := reconciler.upsertDefinitionConfigMap(ctx, version, oversized, "SKILL.md"); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected size limit error, got %v", err)
	}

	cm := &corev1.ConfigMap{}
	err := reconciler.Get(ctx, types.NamespacedName{Name: version.Name + "-definition", Namespace: version.Namespace}, cm)
	if !k8serr.IsNotFound(err) {
		t.Fatalf("expected no configmap to be created, got err=%v", err)
	}

	atLimit := []byte(strings.Repeat("a", maxDefinitionBytes))
	if _, err := reconciler.upsertDefinitionConfigMap(ctx, version, atLimit, "SKILL.md"); err != nil {
		t.Fatalf("expected a definition at the limit to be accepted: %v", err)
	}
}

func TestDefinitionConfigMapExists(t *testing.T) {
	reconciler, version := newDefinitionFixture(t, interceptor.Funcs{})
	ctx := context.Background()

	exists, err := reconciler.definitionConfigMapExists(ctx, version)
	if err != nil || exists {
		t.Fatalf("expected no ref to report false, got exists=%v err=%v", exists, err)
	}

	ref, err := reconciler.upsertDefinitionConfigMap(ctx, version, []byte("body"), "SKILL.md")
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}

	version.Status.DefinitionConfigMapRef = ref

	exists, err = reconciler.definitionConfigMapExists(ctx, version)
	if err != nil || !exists {
		t.Fatalf("expected existing configmap to report true, got exists=%v err=%v", exists, err)
	}

	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: ref.Name, Namespace: version.Namespace}}
	if err := reconciler.Delete(ctx, cm); err != nil {
		t.Fatalf("delete configmap: %v", err)
	}

	exists, err = reconciler.definitionConfigMapExists(ctx, version)
	if err != nil || exists {
		t.Fatalf("expected deleted configmap to report false, got exists=%v err=%v", exists, err)
	}
}

func TestPersistDefinitionRefWritesStatusAndSkipsUnchanged(t *testing.T) {
	reconciler, version := newDefinitionFixture(t, interceptor.Funcs{})
	ctx := context.Background()

	ref := &opendepotv1alpha1.DefinitionConfigMapRef{Name: "code-review-abc1234-definition", Key: definitionConfigMapKey, FileName: "SKILL.md"}
	if err := reconciler.persistDefinitionRef(ctx, version, ref); err != nil {
		t.Fatalf("persist: %v", err)
	}

	stored := &opendepotv1alpha1.Version{}
	if err := reconciler.Get(ctx, client.ObjectKeyFromObject(version), stored); err != nil {
		t.Fatalf("get version: %v", err)
	}

	if stored.Status.DefinitionConfigMapRef == nil || *stored.Status.DefinitionConfigMapRef != *ref {
		t.Fatalf("expected ref persisted, got %+v", stored.Status.DefinitionConfigMapRef)
	}

	before := stored.ResourceVersion
	if err := reconciler.persistDefinitionRef(ctx, version, ref); err != nil {
		t.Fatalf("persist unchanged: %v", err)
	}

	if err := reconciler.Get(ctx, client.ObjectKeyFromObject(version), stored); err != nil {
		t.Fatalf("get version after no-op: %v", err)
	}

	if stored.ResourceVersion != before {
		t.Fatalf("expected no-op persist to skip the update, resourceVersion %s -> %s", before, stored.ResourceVersion)
	}
}

func TestPersistDefinitionRefRetriesOnConflict(t *testing.T) {
	conflicts := 0
	interceptors := interceptor.Funcs{
		SubResourceUpdate: func(ctx context.Context, c client.Client, subResourceName string, obj client.Object, opts ...client.SubResourceUpdateOption) error {
			if conflicts == 0 {
				conflicts++
				return k8serr.NewConflict(schema.GroupResource{Group: "opendepot.defdev.io", Resource: "versions"}, obj.GetName(), nil)
			}

			return c.SubResource(subResourceName).Update(ctx, obj, opts...)
		},
	}

	reconciler, version := newDefinitionFixture(t, interceptors)
	ctx := context.Background()

	ref := &opendepotv1alpha1.DefinitionConfigMapRef{Name: "code-review-abc1234-definition", Key: definitionConfigMapKey, FileName: "SKILL.md"}
	if err := reconciler.persistDefinitionRef(ctx, version, ref); err != nil {
		t.Fatalf("expected conflict to be retried, got %v", err)
	}

	if conflicts != 1 {
		t.Fatalf("expected one injected conflict, got %d", conflicts)
	}

	stored := &opendepotv1alpha1.Version{}
	if err := reconciler.Get(ctx, client.ObjectKeyFromObject(version), stored); err != nil {
		t.Fatalf("get version: %v", err)
	}

	if stored.Status.DefinitionConfigMapRef == nil || stored.Status.DefinitionConfigMapRef.Name != ref.Name {
		t.Fatalf("expected ref persisted after retry, got %+v", stored.Status.DefinitionConfigMapRef)
	}
}
