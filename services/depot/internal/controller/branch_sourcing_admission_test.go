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
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
)

const branchTestSHA = "b8f38227480c9f3fe04d6496d4fcab9a880e5a15"

var _ = Describe("Branch sourcing admission", func() {
	newSkill := func(name string, source opendepotv1alpha1.AgentSourceConfig) *opendepotv1alpha1.Skill {
		return &opendepotv1alpha1.Skill{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: "default",
			},
			Spec: opendepotv1alpha1.SkillSpec{
				AgentSourceConfig: source,
				Versions:          []opendepotv1alpha1.SkillVersion{},
			},
		}
	}

	baseSource := func() opendepotv1alpha1.AgentSourceConfig {
		return opendepotv1alpha1.AgentSourceConfig{
			RepoOwner: "github",
			Path:      "skills/acquire-codebase-knowledge",
		}
	}

	It("rejects ref together with versionConstraints", func() {
		source := baseSource()
		source.Ref = ptr.To("main")
		source.VersionConstraints = ">= 1.0.0"
		err := k8sClient.Create(ctx, newSkill("admit-ref-constraints", source))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("ref and versionConstraints are mutually exclusive"))
	})

	It("rejects ref together with tagPrefix", func() {
		source := baseSource()
		source.Ref = ptr.To("main")
		source.TagPrefix = ptr.To("acquire/")
		err := k8sClient.Create(ctx, newSkill("admit-ref-tagprefix", source))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("tagPrefix is not used with ref"))
	})

	It("rejects branchPolicy without ref", func() {
		source := baseSource()
		source.BranchPolicy = &opendepotv1alpha1.BranchPolicy{}
		err := k8sClient.Create(ctx, newSkill("admit-policy-no-ref", source))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("branchPolicy requires ref"))
	})

	It("accepts a branch source with ref and branchPolicy", func() {
		source := baseSource()
		source.Ref = ptr.To("feat/my-skill-update")
		source.BranchPolicy = &opendepotv1alpha1.BranchPolicy{}
		skill := newSkill("admit-branch-ok", source)
		Expect(k8sClient.Create(ctx, skill)).To(Succeed())
		DeferCleanup(func() {
			Expect(k8sClient.Delete(ctx, skill)).To(Succeed())
		})
	})

	Context("branch Versions", func() {
		newVersion := func(name string, version string, sourceCommit string) *opendepotv1alpha1.Version {
			source := baseSource()
			source.Ref = ptr.To("main")

			return &opendepotv1alpha1.Version{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: "default",
				},
				Spec: opendepotv1alpha1.VersionSpec{
					AgentSourceRef: &source,
					SourceCommit:   sourceCommit,
					Type:           "Skill",
					Version:        version,
				},
			}
		}

		It("allows the write-once transition from an empty version to a semver", func() {
			version := newVersion("branch-write-once", "", branchTestSHA)
			Expect(k8sClient.Create(ctx, version)).To(Succeed())
			DeferCleanup(func() {
				Expect(k8sClient.Delete(ctx, version)).To(Succeed())
			})

			current := &opendepotv1alpha1.Version{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(version), current)).To(Succeed())
			current.Spec.Version = "1.0.0"
			Expect(k8sClient.Update(ctx, current)).To(Succeed())
		})

		It("rejects rewriting a non-empty version", func() {
			version := newVersion("branch-rewrite-version", "", branchTestSHA)
			Expect(k8sClient.Create(ctx, version)).To(Succeed())
			DeferCleanup(func() {
				Expect(k8sClient.Delete(ctx, version)).To(Succeed())
			})

			current := &opendepotv1alpha1.Version{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(version), current)).To(Succeed())
			current.Spec.Version = "1.0.0"
			Expect(k8sClient.Update(ctx, current)).To(Succeed())

			current.Spec.Version = "2.0.0"
			err := k8sClient.Update(ctx, current)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("version is write-once for branch Versions"))
		})

		It("rejects changing sourceCommit", func() {
			version := newVersion("branch-change-commit", "", branchTestSHA)
			Expect(k8sClient.Create(ctx, version)).To(Succeed())
			DeferCleanup(func() {
				Expect(k8sClient.Delete(ctx, version)).To(Succeed())
			})

			current := &opendepotv1alpha1.Version{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(version), current)).To(Succeed())
			current.Spec.SourceCommit = fmt.Sprintf("%040d", 1)
			err := k8sClient.Update(ctx, current)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("sourceCommit is immutable"))
		})

		It("rejects an empty version without a sourceCommit", func() {
			version := newVersion("branch-no-commit", "", "")
			version.Spec.SourceCommit = ""
			err := k8sClient.Create(ctx, version)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("version is required unless sourceCommit is set"))
		})
	})
})
