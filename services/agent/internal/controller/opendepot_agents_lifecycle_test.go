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
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
)

var _ = Describe("Agent Controller Lifecycle", func() {
	ctx := context.Background()

	It("should ignore a request for an Agent that does not exist", func() {
		controllerReconciler := &AgentReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
		}

		_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: "agent-lifecycle-missing", Namespace: "default"},
		})
		Expect(err).NotTo(HaveOccurred())
	})

	It("should clear ForceSync after a successful reconcile", func() {
		agent := &opendepotv1alpha1.Agent{
			ObjectMeta: metav1.ObjectMeta{Name: "agent-lifecycle-force", Namespace: "default"},
			Spec: opendepotv1alpha1.AgentSpec{
				AgentSourceConfig: opendepotv1alpha1.AgentSourceConfig{
					RepoOwner: "tonedefdev",
					Path:      "agents/lifecycle",
				},
				Versions:  []opendepotv1alpha1.AgentVersion{{Version: "v1.0.0", State: "Pending"}},
				ForceSync: true,
			},
		}
		Expect(k8sClient.Create(ctx, agent)).To(Succeed())
		DeferCleanup(func() {
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, agent))).To(Succeed())
		})

		key := types.NamespacedName{Name: agent.Name, Namespace: agent.Namespace}
		controllerReconciler := &AgentReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
		}

		_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(ctx, key, agent)).To(Succeed())
		Expect(agent.Spec.ForceSync).To(BeFalse())
		Expect(agent.Status.Synced).To(BeTrue())
	})
})
