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

var _ = Describe("Skill Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-skill"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		newSkill := func(versions []opendepotv1alpha1.SkillVersion, limit *int) *opendepotv1alpha1.Skill {
			return &opendepotv1alpha1.Skill{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: opendepotv1alpha1.SkillSpec{
					AgentSourceConfig: opendepotv1alpha1.AgentSourceConfig{
						RepoOwner:           "tonedefdev",
						Path:                "skills/test-skill",
						VersionHistoryLimit: limit,
					},
					Versions: versions,
				},
			}
		}

		reconcileSkill := func() {
			controllerReconciler := &SkillReconciler{
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
				client.MatchingLabels{"opendepot.defdev.io/skill": resourceName})).To(Succeed())

			skill := &opendepotv1alpha1.Skill{}
			err := k8sClient.Get(ctx, typeNamespacedName, skill)
			if err == nil {
				Expect(k8sClient.Delete(ctx, skill)).To(Succeed())
			}
		})

		It("should create a Version for each skill version and set the status", func() {
			skill := newSkill([]opendepotv1alpha1.SkillVersion{{Version: "1.0.0"}, {Version: "v1.1.0"}}, nil)
			Expect(k8sClient.Create(ctx, skill)).To(Succeed())

			reconcileSkill()

			version := &opendepotv1alpha1.Version{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: "skill-test-skill-1-0-0", Namespace: "default"}, version)).To(Succeed())
			Expect(version.Spec.Type).To(Equal(opendepotv1alpha1.OpenDepotSkill))
			Expect(version.Spec.Version).To(Equal("1.0.0"))
			Expect(version.Spec.FileName).NotTo(BeNil())
			Expect(version.Spec.AgentSourceRef).NotTo(BeNil())
			Expect(*version.Spec.AgentSourceRef.Name).To(Equal(resourceName))
			Expect(version.Spec.AgentSourceRef.Path).To(Equal("skills/test-skill"))
			Expect(version.Labels).To(HaveKeyWithValue("opendepot.defdev.io/skill", resourceName))
			Expect(version.OwnerReferences).To(HaveLen(1))

			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: "skill-test-skill-1-1-0", Namespace: "default"}, &opendepotv1alpha1.Version{})).To(Succeed())

			Expect(k8sClient.Get(ctx, typeNamespacedName, skill)).To(Succeed())
			Expect(skill.Status.Synced).To(BeTrue())
			Expect(skill.Status.SyncStatus).To(Equal("Successfully synced skill"))
			Expect(skill.Status.LatestVersion).NotTo(BeNil())
			Expect(*skill.Status.LatestVersion).To(Equal("v1.1.0"))
			Expect(skill.Status.VersionRefs).To(HaveLen(2))
			Expect(skill.Status.VersionRefs["1.0.0"].Name).To(Equal("skill-test-skill-1-0-0"))
			Expect(skill.Status.VersionRefs["v1.1.0"].Synced).To(BeTrue())
		})

		It("should not modify an existing Version on subsequent reconciles", func() {
			skill := newSkill([]opendepotv1alpha1.SkillVersion{{Version: "1.0.0"}}, nil)
			Expect(k8sClient.Create(ctx, skill)).To(Succeed())
			reconcileSkill()

			version := &opendepotv1alpha1.Version{}
			versionKey := client.ObjectKey{Name: "skill-test-skill-1-0-0", Namespace: "default"}
			Expect(k8sClient.Get(ctx, versionKey, version)).To(Succeed())
			version.Spec.Yanked = true
			Expect(k8sClient.Update(ctx, version)).To(Succeed())

			reconcileSkill()

			Expect(k8sClient.Get(ctx, versionKey, version)).To(Succeed())
			Expect(version.Spec.Yanked).To(BeTrue())
		})

		It("should trim versions to the version history limit", func() {
			limit := 1
			skill := newSkill([]opendepotv1alpha1.SkillVersion{{Version: "1.0.0"}, {Version: "2.0.0"}}, &limit)
			Expect(k8sClient.Create(ctx, skill)).To(Succeed())

			reconcileSkill()

			Expect(k8sClient.Get(ctx, typeNamespacedName, skill)).To(Succeed())
			Expect(skill.Spec.Versions).To(Equal([]opendepotv1alpha1.SkillVersion{{Version: "2.0.0"}}))
			Expect(skill.Status.VersionRefs).To(HaveLen(1))
			Expect(k8serr.IsNotFound(k8sClient.Get(ctx, client.ObjectKey{Name: "skill-test-skill-1-0-0", Namespace: "default"}, &opendepotv1alpha1.Version{}))).To(BeTrue())
		})

		It("should delete Versions that are removed from the skill spec", func() {
			skill := newSkill([]opendepotv1alpha1.SkillVersion{{Version: "1.0.0"}, {Version: "2.0.0"}}, nil)
			Expect(k8sClient.Create(ctx, skill)).To(Succeed())
			reconcileSkill()

			Expect(k8sClient.Get(ctx, typeNamespacedName, skill)).To(Succeed())
			skill.Spec.Versions = []opendepotv1alpha1.SkillVersion{{Version: "2.0.0"}}
			Expect(k8sClient.Update(ctx, skill)).To(Succeed())

			reconcileSkill()

			Expect(k8serr.IsNotFound(k8sClient.Get(ctx, client.ObjectKey{Name: "skill-test-skill-1-0-0", Namespace: "default"}, &opendepotv1alpha1.Version{}))).To(BeTrue())
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: "skill-test-skill-2-0-0", Namespace: "default"}, &opendepotv1alpha1.Version{})).To(Succeed())
		})
	})
})
