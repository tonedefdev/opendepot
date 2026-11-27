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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
)

var _ = Describe("Version Controller unsupported types", func() {
	ctx := context.Background()

	DescribeTable("dispatches the Version to the unsupported stub",
		func(resourceName string, versionType string) {
			namespacedName := types.NamespacedName{Name: resourceName, Namespace: "default"}

			resource := &opendepotv1alpha1.Version{
				ObjectMeta: metav1.ObjectMeta{
					Name:       resourceName,
					Namespace:  "default",
					Finalizers: []string{opendepotv1alpha1.OpenDepotFinalizer},
				},
				Spec: opendepotv1alpha1.VersionSpec{
					Type:    versionType,
					Version: "1.0.0",
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			DeferCleanup(func() {
				current := &opendepotv1alpha1.Version{}
				if err := k8sClient.Get(ctx, namespacedName, current); err == nil {
					current.Finalizers = nil
					_ = k8sClient.Update(ctx, current)
					_ = k8sClient.Delete(ctx, current)
				}
			})

			reconciler := &VersionReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
				Log:    logr.Discard(),
			}

			for range 2 {
				result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: namespacedName})
				Expect(err).NotTo(HaveOccurred())
				Expect(result.RequeueAfter).To(BeZero())
				Expect(result.Requeue).To(BeFalse())
			}

			current := &opendepotv1alpha1.Version{}
			Expect(k8sClient.Get(ctx, namespacedName, current)).To(Succeed())
			Expect(current.Status.Synced).To(BeFalse())
			Expect(current.Status.SyncStatus).To(Equal("unsupported in this build"))
		},
		Entry("OCIArtifact", "test-oci-unsupported", opendepotv1alpha1.OpenDepotOCIArtifact),
		Entry("MCPServer", "test-mcp-unsupported", opendepotv1alpha1.OpenDepotMCPServer),
	)
})
