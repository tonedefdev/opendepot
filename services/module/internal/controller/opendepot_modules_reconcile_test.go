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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
)

var _ = Describe("Module Controller version lifecycle", func() {
	ctx := context.Background()

	// createModule creates a zip Module in the default namespace with the given versions.
	createModule := func(name string, versions []string, historyLimit *int, forceSync bool) types.NamespacedName {
		fileFormat := "zip"
		moduleVersions := make([]opendepotv1alpha1.ModuleVersion, 0, len(versions))
		for _, v := range versions {
			moduleVersions = append(moduleVersions, opendepotv1alpha1.ModuleVersion{Version: v})
		}

		module := &opendepotv1alpha1.Module{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: "default",
			},
			Spec: opendepotv1alpha1.ModuleSpec{
				ModuleConfig: opendepotv1alpha1.ModuleConfig{
					FileFormat:          &fileFormat,
					VersionHistoryLimit: historyLimit,
				},
				Versions:  moduleVersions,
				ForceSync: forceSync,
			},
		}
		Expect(k8sClient.Create(ctx, module)).To(Succeed())

		key := types.NamespacedName{Name: name, Namespace: "default"}
		DeferCleanup(func() {
			_ = k8sClient.Delete(ctx, &opendepotv1alpha1.Module{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"}})
		})

		return key
	}

	reconcileModule := func(key types.NamespacedName) error {
		controllerReconciler := &ModuleReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
		}
		_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key})
		return err
	}

	It("returns no error when the Module no longer exists", func() {
		Expect(reconcileModule(types.NamespacedName{Name: "reconcile-missing", Namespace: "default"})).To(Succeed())
	})

	It("creates a Version for each module version and records the status", func() {
		key := createModule("reconcile-multi", []string{"1.0.0", "2.0.0"}, nil, false)

		Expect(reconcileModule(key)).To(Succeed())

		for _, name := range []string{"reconcile-multi-1-0-0", "reconcile-multi-2-0-0"} {
			version := &opendepotv1alpha1.Version{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, version)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, version)
			})
		}

		module := &opendepotv1alpha1.Module{}
		Expect(k8sClient.Get(ctx, key, module)).To(Succeed())
		Expect(module.Status.Synced).To(BeTrue())
		Expect(module.Status.SyncStatus).To(Equal("Successfully synced module"))
		Expect(module.Status.ModuleVersionRefs).To(HaveLen(2))
		Expect(module.Status.ModuleVersionRefs).To(HaveKey("1.0.0"))
		Expect(module.Status.ModuleVersionRefs).To(HaveKey("2.0.0"))
		Expect(module.Status.LatestVersion).NotTo(BeNil())
		Expect(*module.Status.LatestVersion).To(Equal("v2.0.0"))
	})

	It("updates an existing Version on a later reconcile without creating a duplicate", func() {
		key := createModule("reconcile-existing", []string{"1.0.0"}, nil, false)

		Expect(reconcileModule(key)).To(Succeed())
		Expect(reconcileModule(key)).To(Succeed())

		version := &opendepotv1alpha1.Version{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "reconcile-existing-1-0-0", Namespace: "default"}, version)).To(Succeed())
		DeferCleanup(func() {
			_ = k8sClient.Delete(ctx, version)
		})

		module := &opendepotv1alpha1.Module{}
		Expect(k8sClient.Get(ctx, key, module)).To(Succeed())
		Expect(module.Status.ModuleVersionRefs).To(HaveLen(1))
	})

	It("deletes the Version of a module version that was removed from the spec", func() {
		key := createModule("reconcile-prune", []string{"1.0.0", "2.0.0"}, nil, false)
		Expect(reconcileModule(key)).To(Succeed())

		module := &opendepotv1alpha1.Module{}
		Expect(k8sClient.Get(ctx, key, module)).To(Succeed())
		module.Spec.Versions = []opendepotv1alpha1.ModuleVersion{{Version: "1.0.0"}}
		Expect(k8sClient.Update(ctx, module)).To(Succeed())

		Expect(reconcileModule(key)).To(Succeed())

		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "reconcile-prune-1-0-0", Namespace: "default"}, &opendepotv1alpha1.Version{})).To(Succeed())
		err := k8sClient.Get(ctx, types.NamespacedName{Name: "reconcile-prune-2-0-0", Namespace: "default"}, &opendepotv1alpha1.Version{})
		Expect(err).To(HaveOccurred())
	})

	It("clears forceSync once the module has been reconciled", func() {
		key := createModule("reconcile-forcesync", []string{"1.0.0"}, nil, true)

		Expect(reconcileModule(key)).To(Succeed())

		module := &opendepotv1alpha1.Module{}
		Expect(k8sClient.Get(ctx, key, module)).To(Succeed())
		Expect(module.Spec.ForceSync).To(BeFalse())
	})

	It("trims the module versions to the version history limit", func() {
		historyLimit := 1
		key := createModule("reconcile-trim", []string{"1.0.0", "2.0.0"}, &historyLimit, false)

		Expect(reconcileModule(key)).To(Succeed())

		module := &opendepotv1alpha1.Module{}
		Expect(k8sClient.Get(ctx, key, module)).To(Succeed())
		Expect(module.Spec.Versions).To(HaveLen(1))
	})

	It("returns an error when the module has no versions to reconcile", func() {
		key := createModule("reconcile-empty", nil, nil, false)

		Expect(reconcileModule(key)).NotTo(Succeed())
	})
})
