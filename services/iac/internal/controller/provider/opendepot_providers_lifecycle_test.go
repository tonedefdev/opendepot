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

package provider

import (
	"context"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
)

var _ = Describe("Provider Controller version lifecycle", func() {
	const testNamespace = "default"

	ctx := context.Background()

	var reconciler *ProviderReconciler

	BeforeEach(func() {
		reconciler = &ProviderReconciler{
			Client: k8sClient,
			Log:    logr.Discard(),
			Scheme: k8sClient.Scheme(),
		}
	})

	AfterEach(func() {
		versionList := &opendepotv1alpha1.VersionList{}
		_ = k8sClient.List(ctx, versionList, client.InNamespace(testNamespace))
		for i := range versionList.Items {
			_ = k8sClient.Delete(ctx, &versionList.Items[i])
		}
	})

	// createProvider creates a provider with one operating system and architecture for each version.
	createProvider := func(name string, versions []string, historyLimit *int) types.NamespacedName {
		upstreamRegistry := opendepotv1alpha1.TerraformRegistryHost
		providerVersions := make([]opendepotv1alpha1.ProviderVersion, 0, len(versions))
		for _, v := range versions {
			providerVersions = append(providerVersions, opendepotv1alpha1.ProviderVersion{Version: v})
		}

		provider := &opendepotv1alpha1.Provider{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: testNamespace,
			},
			Spec: opendepotv1alpha1.ProviderSpec{
				ProviderConfig: opendepotv1alpha1.ProviderConfig{
					UpstreamRegistry:    &upstreamRegistry,
					OperatingSystems:    []string{"linux"},
					Architectures:       []string{"amd64"},
					VersionHistoryLimit: historyLimit,
				},
				Versions: providerVersions,
			},
		}
		Expect(k8sClient.Create(ctx, provider)).To(Succeed())
		DeferCleanup(func() {
			_ = k8sClient.Delete(ctx, provider)
		})

		return types.NamespacedName{Name: name, Namespace: testNamespace}
	}

	It("returns no error when the Provider no longer exists", func() {
		_, err := reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: "lifecycle-missing", Namespace: testNamespace},
		})
		Expect(err).NotTo(HaveOccurred())
	})

	It("records the latest version in the status", func() {
		namespacedName := createProvider("lifecycle-latest", []string{"1.0.0", "2.0.0"}, nil)

		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: namespacedName})
		Expect(err).NotTo(HaveOccurred())

		updated := &opendepotv1alpha1.Provider{}
		Expect(k8sClient.Get(ctx, namespacedName, updated)).To(Succeed())
		Expect(updated.Status.LatestVersion).NotTo(BeNil())
		Expect(*updated.Status.LatestVersion).To(Equal("v2.0.0"))
	})

	It("keeps the file name of an existing Version when the provider is reconciled again", func() {
		namespacedName := createProvider("lifecycle-existing", []string{"1.0.0"}, nil)

		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: namespacedName})
		Expect(err).NotTo(HaveOccurred())

		versionKey := types.NamespacedName{Name: "lifecycle-existing-1-0-0-linux-amd64", Namespace: testNamespace}
		version := &opendepotv1alpha1.Version{}
		Expect(k8sClient.Get(ctx, versionKey, version)).To(Succeed())
		fileName := "existing-artifact.zip"
		version.Spec.FileName = &fileName
		Expect(k8sClient.Update(ctx, version)).To(Succeed())

		_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: namespacedName})
		Expect(err).NotTo(HaveOccurred())

		updatedVersion := &opendepotv1alpha1.Version{}
		Expect(k8sClient.Get(ctx, versionKey, updatedVersion)).To(Succeed())
		Expect(updatedVersion.Spec.FileName).NotTo(BeNil())
		Expect(*updatedVersion.Spec.FileName).To(Equal("existing-artifact.zip"))
	})

	It("trims the provider versions to the version history limit", func() {
		historyLimit := 1
		namespacedName := createProvider("lifecycle-trim", []string{"1.0.0", "2.0.0"}, &historyLimit)

		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: namespacedName})
		Expect(err).NotTo(HaveOccurred())

		updated := &opendepotv1alpha1.Provider{}
		Expect(k8sClient.Get(ctx, namespacedName, updated)).To(Succeed())
		Expect(updated.Spec.Versions).To(HaveLen(1))
	})
})
