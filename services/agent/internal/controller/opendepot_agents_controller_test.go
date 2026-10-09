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
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
)

var _ = Describe("Agent Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-agent"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		newAgent := func(versions []opendepotv1alpha1.AgentVersion, limit *int) *opendepotv1alpha1.Agent {
			return &opendepotv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: opendepotv1alpha1.AgentSpec{
					AgentSourceConfig: opendepotv1alpha1.AgentSourceConfig{
						RepoOwner:           "tonedefdev",
						Path:                "agents/test-agent",
						VersionHistoryLimit: limit,
					},
					Versions: versions,
				},
			}
		}

		reconcileAgent := func() {
			controllerReconciler := &AgentReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
		}

		AfterEach(func() {
			Expect(k8sClient.DeleteAllOf(ctx, &opendepotv1alpha1.Version{}, client.InNamespace("default"),
				client.MatchingLabels{"opendepot.defdev.io/agent": resourceName})).To(Succeed())

			agent := &opendepotv1alpha1.Agent{}
			err := k8sClient.Get(ctx, typeNamespacedName, agent)
			if err == nil {
				Expect(k8sClient.Delete(ctx, agent)).To(Succeed())
			}
		})

		It("should create a Version for each agent version and set the status", func() {
			agent := newAgent([]opendepotv1alpha1.AgentVersion{{Version: "1.0.0"}, {Version: "v1.1.0"}}, nil)
			Expect(k8sClient.Create(ctx, agent)).To(Succeed())

			reconcileAgent()

			version := &opendepotv1alpha1.Version{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: "agent-test-agent-1-0-0", Namespace: "default"}, version)).To(Succeed())
			Expect(version.Spec.Type).To(Equal(opendepotv1alpha1.OpenDepotAgent))
			Expect(version.Spec.Version).To(Equal("1.0.0"))
			Expect(version.Spec.FileName).NotTo(BeNil())
			Expect(version.Spec.AgentSourceRef).NotTo(BeNil())
			Expect(*version.Spec.AgentSourceRef.Name).To(Equal(resourceName))
			Expect(version.Spec.AgentSourceRef.Path).To(Equal("agents/test-agent"))
			Expect(version.Labels).To(HaveKeyWithValue("opendepot.defdev.io/agent", resourceName))
			Expect(version.OwnerReferences).To(HaveLen(1))

			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: "agent-test-agent-1-1-0", Namespace: "default"}, &opendepotv1alpha1.Version{})).To(Succeed())

			Expect(k8sClient.Get(ctx, typeNamespacedName, agent)).To(Succeed())
			Expect(agent.Status.Synced).To(BeTrue())
			Expect(agent.Status.SyncStatus).To(Equal("Successfully synced agent"))
			Expect(agent.Status.LatestVersion).NotTo(BeNil())
			Expect(*agent.Status.LatestVersion).To(Equal("v1.1.0"))
			Expect(agent.Status.VersionRefs).To(HaveLen(2))
			Expect(agent.Status.VersionRefs["1.0.0"].Name).To(Equal("agent-test-agent-1-0-0"))
			Expect(agent.Status.VersionRefs["v1.1.0"].Synced).To(BeTrue())
		})

		It("should not modify an existing Version on subsequent reconciles", func() {
			agent := newAgent([]opendepotv1alpha1.AgentVersion{{Version: "1.0.0"}}, nil)
			Expect(k8sClient.Create(ctx, agent)).To(Succeed())
			reconcileAgent()

			version := &opendepotv1alpha1.Version{}
			versionKey := client.ObjectKey{Name: "agent-test-agent-1-0-0", Namespace: "default"}
			Expect(k8sClient.Get(ctx, versionKey, version)).To(Succeed())
			version.Spec.Yanked = true
			Expect(k8sClient.Update(ctx, version)).To(Succeed())

			reconcileAgent()

			Expect(k8sClient.Get(ctx, versionKey, version)).To(Succeed())
			Expect(version.Spec.Yanked).To(BeTrue())
		})

		It("should trim versions to the version history limit", func() {
			limit := 1
			agent := newAgent([]opendepotv1alpha1.AgentVersion{{Version: "1.0.0"}, {Version: "2.0.0"}}, &limit)
			Expect(k8sClient.Create(ctx, agent)).To(Succeed())

			reconcileAgent()

			Expect(k8sClient.Get(ctx, typeNamespacedName, agent)).To(Succeed())
			Expect(agent.Spec.Versions).To(Equal([]opendepotv1alpha1.AgentVersion{{Version: "2.0.0"}}))
			Expect(agent.Status.VersionRefs).To(HaveLen(1))
			Expect(k8serr.IsNotFound(k8sClient.Get(ctx, client.ObjectKey{Name: "agent-test-agent-1-0-0", Namespace: "default"}, &opendepotv1alpha1.Version{}))).To(BeTrue())
		})

		It("should delete Versions that are removed from the agent spec", func() {
			agent := newAgent([]opendepotv1alpha1.AgentVersion{{Version: "1.0.0"}, {Version: "2.0.0"}}, nil)
			Expect(k8sClient.Create(ctx, agent)).To(Succeed())
			reconcileAgent()

			Expect(k8sClient.Get(ctx, typeNamespacedName, agent)).To(Succeed())
			agent.Spec.Versions = []opendepotv1alpha1.AgentVersion{{Version: "2.0.0"}}
			Expect(k8sClient.Update(ctx, agent)).To(Succeed())

			reconcileAgent()

			Expect(k8serr.IsNotFound(k8sClient.Get(ctx, client.ObjectKey{Name: "agent-test-agent-1-0-0", Namespace: "default"}, &opendepotv1alpha1.Version{}))).To(BeTrue())
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: "agent-test-agent-2-0-0", Namespace: "default"}, &opendepotv1alpha1.Version{})).To(Succeed())
		})
	})
})
