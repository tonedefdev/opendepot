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
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"time"

	"github.com/go-logr/logr"
	"github.com/google/go-github/v81/github"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	opendepotGithub "github.com/tonedefdev/opendepot/pkg/github"
)

var _ = Describe("decideBranchEntry", func() {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	old := metav1.NewTime(now.Add(-time.Hour))
	recent := metav1.NewTime(now.Add(-time.Minute))
	interval := 10 * time.Minute

	It("adds the first head when there are no entries", func() {
		add, wait := decideBranchEntry(nil, branchTestSHA, interval, now)
		Expect(add).To(BeTrue())
		Expect(wait).To(BeZero())
	})

	It("adds nothing while the newest entry is pending", func() {
		entries := []branchEntry{{Commit: "1111111111111111111111111111111111111111", DiscoveredAt: &old, State: "Pending"}}
		add, wait := decideBranchEntry(entries, branchTestSHA, interval, now)
		Expect(add).To(BeFalse())
		Expect(wait).To(BeZero())
	})

	It("adds nothing when the head is already recorded", func() {
		entries := []branchEntry{
			{Commit: branchTestSHA, DiscoveredAt: &old, State: "Assigned"},
			{Commit: "2222222222222222222222222222222222222222", DiscoveredAt: &recent, State: "Rejected"},
		}
		add, _ := decideBranchEntry(entries, branchTestSHA, interval, now)
		Expect(add).To(BeFalse())
	})

	It("adds a new head once the interval has passed", func() {
		entries := []branchEntry{{Commit: "1111111111111111111111111111111111111111", DiscoveredAt: &old, State: "Assigned"}}
		add, wait := decideBranchEntry(entries, branchTestSHA, interval, now)
		Expect(add).To(BeTrue())
		Expect(wait).To(BeZero())
	})

	It("holds a new head back until the interval has passed", func() {
		entries := []branchEntry{{Commit: "1111111111111111111111111111111111111111", DiscoveredAt: &recent, State: "Rejected"}}
		add, wait := decideBranchEntry(entries, branchTestSHA, interval, now)
		Expect(add).To(BeFalse())
		Expect(wait).To(Equal(9 * time.Minute))
	})

	It("compares the newest entry by discovery time rather than list order", func() {
		entries := []branchEntry{
			{Commit: "2222222222222222222222222222222222222222", DiscoveredAt: &recent, State: "Pending"},
			{Commit: "1111111111111111111111111111111111111111", DiscoveredAt: &old, State: "Assigned"},
		}
		add, _ := decideBranchEntry(entries, branchTestSHA, interval, now)
		Expect(add).To(BeFalse())
	})
})

var _ = Describe("Branch sourcing reconcile", func() {
	var (
		server     *httptest.Server
		head       string
		reconciler *DepotReconciler
		original   func(context.Context, client.Client, string, *opendepotv1alpha1.AgentSourceConfig) (*github.Client, error)
	)

	branchSource := func(name string) opendepotv1alpha1.AgentSourceConfig {
		return opendepotv1alpha1.AgentSourceConfig{
			Name:      ptr.To(name),
			RepoOwner: "github",
			Path:      "skills/acquire-codebase-knowledge",
			Ref:       ptr.To("main"),
		}
	}

	BeforeEach(func() {
		original = newBranchGithubClient
		head = branchTestSHA
		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if head == "" {
				_, _ = fmt.Fprint(w, `[]`)
				return
			}

			_, _ = fmt.Fprintf(w, `[{"sha":%q}]`, head)
		}))

		baseURL, err := url.Parse(server.URL + "/")
		Expect(err).NotTo(HaveOccurred())
		newBranchGithubClient = func(_ context.Context, _ client.Client, _ string, _ *opendepotv1alpha1.AgentSourceConfig) (*github.Client, error) {
			githubClient := github.NewClient(nil)
			githubClient.BaseURL = baseURL
			return githubClient, nil
		}

		reconciler = &DepotReconciler{
			Client:                k8sClient,
			Log:                   logr.Discard(),
			Scheme:                k8sClient.Scheme(),
			BranchSourcingEnabled: true,
		}
	})

	AfterEach(func() {
		newBranchGithubClient = original
		server.Close()
	})

	It("creates a Skill with one pending entry on first sighting", func() {
		name := "branch-first-skill"
		source := branchSource(name)
		DeferCleanup(func() {
			_ = k8sClient.Delete(ctx, &opendepotv1alpha1.Skill{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"}})
		})

		_, err := reconciler.reconcileBranchSkill(ctx, "default", &source)
		Expect(err).NotTo(HaveOccurred())

		var skill opendepotv1alpha1.Skill
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: name, Namespace: "default"}, &skill)).To(Succeed())
		Expect(skill.Spec.Versions).To(HaveLen(1))
		Expect(skill.Spec.Versions[0].Commit).To(Equal(branchTestSHA))
		Expect(skill.Spec.Versions[0].State).To(Equal("Pending"))
		Expect(skill.Spec.Versions[0].DiscoveredAt).NotTo(BeNil())
	})

	It("adds a pending entry for a new head and keeps the existing entry unchanged", func() {
		name := "branch-existing-agent"
		source := branchSource(name)
		discoveredAt := metav1.NewTime(time.Now().Add(-time.Hour))
		DeferCleanup(func() {
			_ = k8sClient.Delete(ctx, &opendepotv1alpha1.Agent{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"}})
		})

		Expect(k8sClient.Create(ctx, &opendepotv1alpha1.Agent{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Spec: opendepotv1alpha1.AgentSpec{
				AgentSourceConfig: source,
				Versions: []opendepotv1alpha1.AgentVersion{{
					Commit:       "1111111111111111111111111111111111111111",
					DiscoveredAt: &discoveredAt,
					State:        "Assigned",
					Version:      "1.0.0",
				}},
			},
		})).To(Succeed())

		_, err := reconciler.reconcileBranchAgent(ctx, "default", &source)
		Expect(err).NotTo(HaveOccurred())

		var agent opendepotv1alpha1.Agent
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: name, Namespace: "default"}, &agent)).To(Succeed())
		Expect(agent.Spec.Versions).To(HaveLen(2))
		Expect(agent.Spec.Versions[0].State).To(Equal("Assigned"))
		Expect(agent.Spec.Versions[0].Version).To(Equal("1.0.0"))
		Expect(agent.Spec.Versions[1].Commit).To(Equal(branchTestSHA))
		Expect(agent.Spec.Versions[1].State).To(Equal("Pending"))
	})

	It("holds back a new head while the interval has not passed", func() {
		name := "branch-held-skill"
		source := branchSource(name)
		discoveredAt := metav1.NewTime(time.Now())
		DeferCleanup(func() {
			_ = k8sClient.Delete(ctx, &opendepotv1alpha1.Skill{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"}})
		})

		Expect(k8sClient.Create(ctx, &opendepotv1alpha1.Skill{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Spec: opendepotv1alpha1.SkillSpec{
				AgentSourceConfig: source,
				Versions: []opendepotv1alpha1.SkillVersion{{
					Commit:       "1111111111111111111111111111111111111111",
					DiscoveredAt: &discoveredAt,
					State:        "Rejected",
				}},
			},
		})).To(Succeed())

		wait, err := reconciler.reconcileBranchSkill(ctx, "default", &source)
		Expect(err).NotTo(HaveOccurred())
		Expect(wait).To(BeNumerically(">", 0))
		Expect(wait).To(BeNumerically("<=", opendepotv1alpha1.DefaultMinVersionInterval))

		var skill opendepotv1alpha1.Skill
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: name, Namespace: "default"}, &skill)).To(Succeed())
		Expect(skill.Spec.Versions).To(HaveLen(1))
	})

	It("leaves the parent unchanged when the branch does not exist", func() {
		name := "branch-missing-skill"
		source := branchSource(name)
		head = ""

		_, err := reconciler.reconcileBranchSkill(ctx, "default", &source)
		Expect(err).To(HaveOccurred())
		Expect(errors.Is(err, opendepotGithub.ErrRefNotFound)).To(BeTrue())

		var skill opendepotv1alpha1.Skill
		err = k8sClient.Get(ctx, client.ObjectKey{Name: name, Namespace: "default"}, &skill)
		Expect(err).To(HaveOccurred())
	})

	Describe("validateBranchSource", func() {
		It("rejects branch sources when the controller flag is off", func() {
			reconciler.BranchSourcingEnabled = false
			source := branchSource("flag-off")
			Expect(reconciler.validateBranchSource(&source)).To(MatchError(ContainSubstring("--branch-sourcing-enabled")))
		})

		It("rejects a negative minVersionInterval", func() {
			source := branchSource("negative-interval")
			source.BranchPolicy = &opendepotv1alpha1.BranchPolicy{
				MinVersionInterval: &metav1.Duration{Duration: -time.Minute},
			}
			Expect(reconciler.validateBranchSource(&source)).To(MatchError(ContainSubstring("must not be negative")))
		})

		It("rejects ref with versionConstraints", func() {
			source := branchSource("both-set")
			source.VersionConstraints = ">= 1.0.0"
			Expect(reconciler.validateBranchSource(&source)).To(MatchError(ContainSubstring("mutually exclusive")))
		})

		It("rejects a repoUrl whose owner does not match repoOwner", func() {
			source := branchSource("owner-mismatch")
			source.RepoUrl = ptr.To("https://github.com/someone-else/acquire-codebase-knowledge")
			Expect(reconciler.validateBranchSource(&source)).To(HaveOccurred())
		})

		It("rejects a ref that contains a dot segment", func() {
			source := branchSource("dot-segment")
			source.Ref = ptr.To("feat/../main")
			Expect(reconciler.validateBranchSource(&source)).To(HaveOccurred())
		})

		It("accepts a valid branch source", func() {
			source := branchSource("valid-source")
			source.Ref = ptr.To("feat/bs/depot-head")
			Expect(reconciler.validateBranchSource(&source)).To(Succeed())
		})
	})
})
