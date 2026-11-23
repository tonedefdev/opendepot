/*
Copyright 2025.

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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/go-github/v81/github"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
)

// covPagedReleasesTransport serves GitHub release listings one page at a time. Each page links to the next
// through the Link header, so the controller's pagination loop is exercised.
type covPagedReleasesTransport struct {
	pages [][]string
}

func (t covPagedReleasesTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if strings.HasSuffix(req.URL.Path, "/access_tokens") {
		return &http.Response{
			StatusCode: http.StatusCreated,
			Header:     http.Header{"Content-Type": {"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"token":"test-installation-token","expires_at":"2099-01-01T00:00:00Z"}`)),
			Request:    req,
		}, nil
	}

	page := 1
	if p := req.URL.Query().Get("page"); p != "" {
		_, _ = fmt.Sscanf(p, "%d", &page)
	}

	if page < 1 || page > len(t.pages) {
		page = len(t.pages)
	}

	releases := make([]map[string]string, 0)
	for _, tag := range t.pages[page-1] {
		releases = append(releases, map[string]string{"tag_name": tag, "name": tag})
	}

	body, err := json.Marshal(releases)
	if err != nil {
		return nil, err
	}

	header := http.Header{"Content-Type": {"application/json"}}
	if page < len(t.pages) {
		header.Set("Link", fmt.Sprintf(`<https://api.github.com/repositories/1/releases?page=%d>; rel="next"`, page+1))
	}

	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     header,
		Body:       io.NopCloser(bytes.NewReader(body)),
		Request:    req,
	}, nil
}

// covInterceptedClient wraps a watch-capable client so individual API calls can be forced to fail.
func covInterceptedClient(funcs interceptor.Funcs) client.Client {
	base, err := client.NewWithWatch(cfg, client.Options{Scheme: k8sClient.Scheme()})
	Expect(err).NotTo(HaveOccurred())

	return interceptor.NewClient(base, funcs)
}

var _ = Describe("Depot Controller reconcile coverage", func() {
	const (
		depotName  = "cov-depot"
		moduleName = "cov-module"
		providerNm = "cov-provider"
		skillName  = "cov-skill"
		agentName  = "cov-agent"
		namespace  = "default"
	)

	ctx := context.Background()

	depotKey := types.NamespacedName{Name: depotName, Namespace: namespace}

	var (
		originalTransport http.RoundTripper
		storageDirectory  string
	)

	BeforeEach(func() {
		originalTransport = http.DefaultTransport
		storageDirectory = GinkgoT().TempDir()
		originalListProviderVersions := listProviderVersions
		DeferCleanup(func() {
			listProviderVersions = originalListProviderVersions
		})
	})

	AfterEach(func() {
		http.DefaultTransport = originalTransport

		depot := &opendepotv1alpha1.Depot{}
		if err := k8sClient.Get(ctx, depotKey, depot); err == nil {
			Expect(k8sClient.Delete(ctx, depot)).To(Succeed())
		}

		module := &opendepotv1alpha1.Module{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: moduleName, Namespace: namespace}, module); err == nil {
			Expect(k8sClient.Delete(ctx, module)).To(Succeed())
		}

		provider := &opendepotv1alpha1.Provider{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: providerNm, Namespace: namespace}, provider); err == nil {
			Expect(k8sClient.Delete(ctx, provider)).To(Succeed())
		}

		skill := &opendepotv1alpha1.Skill{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: skillName, Namespace: namespace}, skill); err == nil {
			Expect(k8sClient.Delete(ctx, skill)).To(Succeed())
		}

		agent := &opendepotv1alpha1.Agent{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: agentName, Namespace: namespace}, agent); err == nil {
			Expect(k8sClient.Delete(ctx, agent)).To(Succeed())
		}
	})

	fileSystemStorage := func() *opendepotv1alpha1.StorageConfig {
		return &opendepotv1alpha1.StorageConfig{
			FileSystem: &opendepotv1alpha1.FileSystemConfig{DirectoryPath: &storageDirectory},
		}
	}

	createDepot := func(spec opendepotv1alpha1.DepotSpec) {
		Expect(k8sClient.Create(ctx, &opendepotv1alpha1.Depot{
			ObjectMeta: metav1.ObjectMeta{Name: depotName, Namespace: namespace},
			Spec:       spec,
		})).To(Succeed())
	}

	updateDepotSpec := func(mutate func(spec *opendepotv1alpha1.DepotSpec)) {
		current := &opendepotv1alpha1.Depot{}
		Expect(k8sClient.Get(ctx, depotKey, current)).To(Succeed())
		mutate(&current.Spec)
		Expect(k8sClient.Update(ctx, current)).To(Succeed())
	}

	reconcileWith := func(c client.Client, branchSourcing bool) (reconcile.Result, error) {
		reconciler := &DepotReconciler{
			Client:                c,
			APIReader:             k8sClient,
			Scheme:                k8sClient.Scheme(),
			BranchSourcingEnabled: branchSourcing,
		}

		return reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: depotKey})
	}

	reconcileDepot := func() (reconcile.Result, error) {
		return reconcileWith(k8sClient, false)
	}

	getDepot := func() *opendepotv1alpha1.Depot {
		depot := &opendepotv1alpha1.Depot{}
		Expect(k8sClient.Get(ctx, depotKey, depot)).To(Succeed())
		return depot
	}

	moduleVersions := func() []string {
		module := &opendepotv1alpha1.Module{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: moduleName, Namespace: namespace}, module)).To(Succeed())

		var versions []string
		for _, v := range module.Spec.Versions {
			versions = append(versions, v.Version)
		}

		return versions
	}

	moduleSpec := func(constraints string, historyLimit *int) opendepotv1alpha1.ModuleConfig {
		name := moduleName
		return opendepotv1alpha1.ModuleConfig{
			Name:                &name,
			RepoOwner:           "example",
			VersionConstraints:  constraints,
			VersionHistoryLimit: historyLimit,
		}
	}

	Context("when the depot lookup fails", func() {
		It("returns no error when the depot does not exist", func() {
			result, err := reconcileWith(k8sClient, false)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(BeZero())
		})

		It("returns the error when reading the depot fails", func() {
			failing := covInterceptedClient(interceptor.Funcs{
				Get: func(_ context.Context, _ client.WithWatch, _ client.ObjectKey, _ client.Object, _ ...client.GetOption) error {
					return errors.New("depot read failed")
				},
			})

			_, err := reconcileWith(failing, false)
			Expect(err).To(MatchError("depot read failed"))
		})
	})

	Context("when reconciling modules", func() {
		BeforeEach(func() {
			createDepot(opendepotv1alpha1.DepotSpec{
				GlobalConfig: &opendepotv1alpha1.GlobalConfig{StorageConfig: fileSystemStorage()},
			})
		})

		It("creates a module from paginated releases filtered by constraints, skipping invalid and duplicate tags", func() {
			http.DefaultTransport = covPagedReleasesTransport{pages: [][]string{
				{"v1.0.0", "v1.2.0", "not-a-version"},
				{"v1.1.0", "v2.0.0", "v1.0.0"},
			}}

			updateDepotSpec(func(spec *opendepotv1alpha1.DepotSpec) {
				spec.ModuleConfigs = []opendepotv1alpha1.ModuleConfig{moduleSpec(">= 1.0.0, < 2.0.0", nil)}
			})

			result, err := reconcileDepot()
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(BeZero())
			Expect(moduleVersions()).To(Equal([]string{"1.0.0", "1.2.0", "1.1.0"}))
			Expect(getDepot().Status.Modules).To(Equal([]string{moduleName}))
		})

		It("keeps only the newest versions when the history limit is exceeded", func() {
			http.DefaultTransport = covPagedReleasesTransport{pages: [][]string{
				{"v1.0.0", "v1.1.0"},
				{"v1.2.0"},
			}}

			limit := 2
			updateDepotSpec(func(spec *opendepotv1alpha1.DepotSpec) {
				spec.ModuleConfigs = []opendepotv1alpha1.ModuleConfig{moduleSpec(">= 1.0.0", &limit)}
			})

			_, err := reconcileDepot()
			Expect(err).NotTo(HaveOccurred())
			Expect(moduleVersions()).To(Equal([]string{"1.1.0", "1.2.0"}))
		})

		It("updates an existing module when the release set changes", func() {
			http.DefaultTransport = githubReleasesTransport{tags: []string{"v1.0.0"}}
			updateDepotSpec(func(spec *opendepotv1alpha1.DepotSpec) {
				spec.ModuleConfigs = []opendepotv1alpha1.ModuleConfig{moduleSpec(">= 1.0.0", nil)}
			})

			_, err := reconcileDepot()
			Expect(err).NotTo(HaveOccurred())
			Expect(moduleVersions()).To(Equal([]string{"1.0.0"}))

			http.DefaultTransport = githubReleasesTransport{tags: []string{"v1.0.0", "v1.1.0"}}
			_, err = reconcileDepot()
			Expect(err).NotTo(HaveOccurred())
			Expect(moduleVersions()).To(Equal([]string{"1.0.0", "1.1.0"}))
		})

		It("returns an error when the version constraints are malformed", func() {
			http.DefaultTransport = githubReleasesTransport{tags: []string{"v1.0.0"}}
			updateDepotSpec(func(spec *opendepotv1alpha1.DepotSpec) {
				spec.ModuleConfigs = []opendepotv1alpha1.ModuleConfig{moduleSpec("not-a-constraint", nil)}
			})

			_, err := reconcileDepot()
			Expect(err).To(HaveOccurred())
		})

		It("returns an error when the repository owner is not a valid GitHub name", func() {
			http.DefaultTransport = githubReleasesTransport{tags: []string{"v1.0.0"}}
			invalid := moduleSpec(">= 1.0.0", nil)
			invalid.RepoOwner = "bad/owner"
			updateDepotSpec(func(spec *opendepotv1alpha1.DepotSpec) {
				spec.ModuleConfigs = []opendepotv1alpha1.ModuleConfig{invalid}
			})

			_, err := reconcileDepot()
			Expect(err).To(MatchError(ContainSubstring("invalid GitHub owner or repository name")))
		})

		It("returns an error when the GitHub App secret is missing for an authenticated client", func() {
			authenticated := moduleSpec(">= 1.0.0", nil)
			authenticated.GithubClientConfig = &opendepotv1alpha1.GithubClientConfig{UseAuthenticatedClient: true}
			updateDepotSpec(func(spec *opendepotv1alpha1.DepotSpec) {
				spec.ModuleConfigs = []opendepotv1alpha1.ModuleConfig{authenticated}
			})

			_, err := reconcileDepot()
			Expect(err).To(HaveOccurred())
		})

		It("returns an error when listing GitHub releases fails", func() {
			http.DefaultTransport = failingReleasesTransport{}
			updateDepotSpec(func(spec *opendepotv1alpha1.DepotSpec) {
				spec.ModuleConfigs = []opendepotv1alpha1.ModuleConfig{moduleSpec(">= 1.0.0", nil)}
			})

			_, err := reconcileDepot()
			Expect(err).To(HaveOccurred())
		})

		It("returns an error when creating the module fails", func() {
			http.DefaultTransport = githubReleasesTransport{tags: []string{"v1.0.0"}}
			updateDepotSpec(func(spec *opendepotv1alpha1.DepotSpec) {
				spec.ModuleConfigs = []opendepotv1alpha1.ModuleConfig{moduleSpec(">= 1.0.0", nil)}
			})

			failing := covInterceptedClient(interceptor.Funcs{
				Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
					if _, ok := obj.(*opendepotv1alpha1.Module); ok {
						return errors.New("module create failed")
					}
					return c.Create(ctx, obj, opts...)
				},
			})

			_, err := reconcileWith(failing, false)
			Expect(err).To(MatchError("module create failed"))
		})

		It("returns an error when updating an existing module fails", func() {
			http.DefaultTransport = githubReleasesTransport{tags: []string{"v1.0.0"}}
			updateDepotSpec(func(spec *opendepotv1alpha1.DepotSpec) {
				spec.ModuleConfigs = []opendepotv1alpha1.ModuleConfig{moduleSpec(">= 1.0.0", nil)}
			})

			_, err := reconcileDepot()
			Expect(err).NotTo(HaveOccurred())

			failing := covInterceptedClient(interceptor.Funcs{
				Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
					if _, ok := obj.(*opendepotv1alpha1.Module); ok {
						return errors.New("module update failed")
					}
					return c.Update(ctx, obj, opts...)
				},
			})

			_, err = reconcileWith(failing, false)
			Expect(err).To(MatchError("module update failed"))
		})
	})

	Context("when reconciling providers", func() {
		BeforeEach(func() {
			createDepot(opendepotv1alpha1.DepotSpec{
				GlobalConfig: &opendepotv1alpha1.GlobalConfig{StorageConfig: fileSystemStorage()},
			})
		})

		providerSpec := func(constraints string, historyLimit *int) opendepotv1alpha1.ProviderConfig {
			name := providerNm
			return opendepotv1alpha1.ProviderConfig{
				Name:                &name,
				VersionConstraints:  constraints,
				VersionHistoryLimit: historyLimit,
			}
		}

		providerVersions := func() []string {
			provider := &opendepotv1alpha1.Provider{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: providerNm, Namespace: namespace}, provider)).To(Succeed())

			var versions []string
			for _, v := range provider.Spec.Versions {
				versions = append(versions, v.Version)
			}

			return versions
		}

		It("creates a provider using the default registry namespace and applies the history limit", func() {
			var calledNamespace string
			listProviderVersions = func(_ context.Context, _, namespace, _ string) ([]string, error) {
				calledNamespace = namespace
				return []string{"1.0.0", "1.1.0", "2.0.0", "bogus"}, nil
			}

			limit := 2
			updateDepotSpec(func(spec *opendepotv1alpha1.DepotSpec) {
				spec.ProviderConfigs = []opendepotv1alpha1.ProviderConfig{providerSpec(">= 1.0.0", &limit)}
			})

			_, err := reconcileDepot()
			Expect(err).NotTo(HaveOccurred())
			Expect(calledNamespace).To(Equal("hashicorp"))
			Expect(providerVersions()).To(Equal([]string{"1.1.0", "2.0.0"}))
			Expect(getDepot().Status.Providers).To(Equal([]string{providerNm}))
		})

		It("updates an existing provider when the upstream versions change", func() {
			listProviderVersions = func(_ context.Context, _, _, _ string) ([]string, error) {
				return []string{"1.0.0"}, nil
			}
			updateDepotSpec(func(spec *opendepotv1alpha1.DepotSpec) {
				spec.ProviderConfigs = []opendepotv1alpha1.ProviderConfig{providerSpec(">= 1.0.0", nil)}
			})

			_, err := reconcileDepot()
			Expect(err).NotTo(HaveOccurred())
			Expect(providerVersions()).To(Equal([]string{"1.0.0"}))

			listProviderVersions = func(_ context.Context, _, _, _ string) ([]string, error) {
				return []string{"1.0.0", "1.2.0"}, nil
			}
			_, err = reconcileDepot()
			Expect(err).NotTo(HaveOccurred())
			Expect(providerVersions()).To(Equal([]string{"1.0.0", "1.2.0"}))
		})

		It("returns an error when the provider name is missing", func() {
			updateDepotSpec(func(spec *opendepotv1alpha1.DepotSpec) {
				spec.ProviderConfigs = []opendepotv1alpha1.ProviderConfig{{VersionConstraints: ">= 1.0.0"}}
			})

			_, err := reconcileDepot()
			Expect(err).To(MatchError("provider config name is required"))
		})

		It("returns an error when the provider version constraints are malformed", func() {
			listProviderVersions = func(_ context.Context, _, _, _ string) ([]string, error) {
				return []string{"1.0.0"}, nil
			}
			updateDepotSpec(func(spec *opendepotv1alpha1.DepotSpec) {
				spec.ProviderConfigs = []opendepotv1alpha1.ProviderConfig{providerSpec("not-a-constraint", nil)}
			})

			_, err := reconcileDepot()
			Expect(err).To(MatchError(ContainSubstring("invalid version constraints")))
		})

		It("returns an error when listing upstream provider versions fails", func() {
			listProviderVersions = func(_ context.Context, _, _, _ string) ([]string, error) {
				return nil, errors.New("registry unavailable")
			}
			updateDepotSpec(func(spec *opendepotv1alpha1.DepotSpec) {
				spec.ProviderConfigs = []opendepotv1alpha1.ProviderConfig{providerSpec(">= 1.0.0", nil)}
			})

			_, err := reconcileDepot()
			Expect(err).To(MatchError("registry unavailable"))
		})

		It("returns an error when creating the provider fails", func() {
			listProviderVersions = func(_ context.Context, _, _, _ string) ([]string, error) {
				return []string{"1.0.0"}, nil
			}
			updateDepotSpec(func(spec *opendepotv1alpha1.DepotSpec) {
				spec.ProviderConfigs = []opendepotv1alpha1.ProviderConfig{providerSpec(">= 1.0.0", nil)}
			})

			failing := covInterceptedClient(interceptor.Funcs{
				Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
					if _, ok := obj.(*opendepotv1alpha1.Provider); ok {
						return errors.New("provider create failed")
					}
					return c.Create(ctx, obj, opts...)
				},
			})

			_, err := reconcileWith(failing, false)
			Expect(err).To(MatchError("provider create failed"))
		})

		It("returns an error when updating an existing provider fails", func() {
			listProviderVersions = func(_ context.Context, _, _, _ string) ([]string, error) {
				return []string{"1.0.0"}, nil
			}
			updateDepotSpec(func(spec *opendepotv1alpha1.DepotSpec) {
				spec.ProviderConfigs = []opendepotv1alpha1.ProviderConfig{providerSpec(">= 1.0.0", nil)}
			})

			_, err := reconcileDepot()
			Expect(err).NotTo(HaveOccurred())

			failing := covInterceptedClient(interceptor.Funcs{
				Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
					if _, ok := obj.(*opendepotv1alpha1.Provider); ok {
						return errors.New("provider update failed")
					}
					return c.Update(ctx, obj, opts...)
				},
			})

			_, err = reconcileWith(failing, false)
			Expect(err).To(MatchError("provider update failed"))
		})
	})

	Context("when reconciling skills and agents from GitHub tags", func() {
		BeforeEach(func() {
			createDepot(opendepotv1alpha1.DepotSpec{
				GlobalConfig: &opendepotv1alpha1.GlobalConfig{StorageConfig: fileSystemStorage()},
			})
		})

		sourceSpec := func(name string) opendepotv1alpha1.AgentSourceConfig {
			sourceName := name
			return opendepotv1alpha1.AgentSourceConfig{
				Name:               &sourceName,
				RepoOwner:          "example",
				VersionConstraints: ">= 1.0.0",
			}
		}

		It("creates a skill and an agent with the matched tags and records them in status", func() {
			http.DefaultTransport = githubTagsTransport{tags: []string{"v1.0.0", "v1.1.0"}}
			updateDepotSpec(func(spec *opendepotv1alpha1.DepotSpec) {
				spec.SkillConfigs = []opendepotv1alpha1.AgentSourceConfig{sourceSpec(skillName)}
				spec.AgentConfigs = []opendepotv1alpha1.AgentSourceConfig{sourceSpec(agentName)}
			})

			_, err := reconcileDepot()
			Expect(err).NotTo(HaveOccurred())

			skill := &opendepotv1alpha1.Skill{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: skillName, Namespace: namespace}, skill)).To(Succeed())
			Expect(skill.Spec.Versions).To(HaveLen(2))
			Expect(skill.Spec.Versions[0].Version).To(Equal("v1.0.0"))

			agent := &opendepotv1alpha1.Agent{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: agentName, Namespace: namespace}, agent)).To(Succeed())
			Expect(agent.Spec.Versions).To(HaveLen(2))

			depot := getDepot()
			Expect(depot.Status.Skills).To(Equal([]string{skillName}))
			Expect(depot.Status.Agents).To(Equal([]string{agentName}))
		})

		It("updates an existing skill and agent when the tags change", func() {
			http.DefaultTransport = githubTagsTransport{tags: []string{"v1.0.0"}}
			updateDepotSpec(func(spec *opendepotv1alpha1.DepotSpec) {
				spec.SkillConfigs = []opendepotv1alpha1.AgentSourceConfig{sourceSpec(skillName)}
				spec.AgentConfigs = []opendepotv1alpha1.AgentSourceConfig{sourceSpec(agentName)}
			})

			_, err := reconcileDepot()
			Expect(err).NotTo(HaveOccurred())

			http.DefaultTransport = githubTagsTransport{tags: []string{"v1.0.0", "v1.3.0"}}
			_, err = reconcileDepot()
			Expect(err).NotTo(HaveOccurred())

			skill := &opendepotv1alpha1.Skill{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: skillName, Namespace: namespace}, skill)).To(Succeed())
			Expect(skill.Spec.Versions).To(HaveLen(2))

			agent := &opendepotv1alpha1.Agent{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: agentName, Namespace: namespace}, agent)).To(Succeed())
			Expect(agent.Spec.Versions).To(HaveLen(2))
		})

		It("returns an error when a skill name is blank", func() {
			updateDepotSpec(func(spec *opendepotv1alpha1.DepotSpec) {
				spec.SkillConfigs = []opendepotv1alpha1.AgentSourceConfig{sourceSpec("   ")}
			})

			_, err := reconcileDepot()
			Expect(err).To(MatchError("skill config name is required"))
		})

		It("returns an error when an agent name is missing", func() {
			updateDepotSpec(func(spec *opendepotv1alpha1.DepotSpec) {
				spec.AgentConfigs = []opendepotv1alpha1.AgentSourceConfig{{RepoOwner: "example"}}
			})

			_, err := reconcileDepot()
			Expect(err).To(MatchError("agent config name is required"))
		})

		It("returns an error when listing skill tags fails", func() {
			http.DefaultTransport = failingReleasesTransport{}
			updateDepotSpec(func(spec *opendepotv1alpha1.DepotSpec) {
				spec.SkillConfigs = []opendepotv1alpha1.AgentSourceConfig{sourceSpec(skillName)}
			})

			_, err := reconcileDepot()
			Expect(err).To(HaveOccurred())
		})

		It("returns an error when listing agent tags fails", func() {
			http.DefaultTransport = failingReleasesTransport{}
			updateDepotSpec(func(spec *opendepotv1alpha1.DepotSpec) {
				spec.AgentConfigs = []opendepotv1alpha1.AgentSourceConfig{sourceSpec(agentName)}
			})

			_, err := reconcileDepot()
			Expect(err).To(HaveOccurred())
		})

		It("returns an error when creating the skill fails", func() {
			http.DefaultTransport = githubTagsTransport{tags: []string{"v1.0.0"}}
			updateDepotSpec(func(spec *opendepotv1alpha1.DepotSpec) {
				spec.SkillConfigs = []opendepotv1alpha1.AgentSourceConfig{sourceSpec(skillName)}
			})

			failing := covInterceptedClient(interceptor.Funcs{
				Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
					if _, ok := obj.(*opendepotv1alpha1.Skill); ok {
						return errors.New("skill create failed")
					}
					return c.Create(ctx, obj, opts...)
				},
			})

			_, err := reconcileWith(failing, false)
			Expect(err).To(MatchError("skill create failed"))
		})

		It("returns an error when updating an existing agent fails", func() {
			http.DefaultTransport = githubTagsTransport{tags: []string{"v1.0.0"}}
			updateDepotSpec(func(spec *opendepotv1alpha1.DepotSpec) {
				spec.AgentConfigs = []opendepotv1alpha1.AgentSourceConfig{sourceSpec(agentName)}
			})

			_, err := reconcileDepot()
			Expect(err).NotTo(HaveOccurred())

			failing := covInterceptedClient(interceptor.Funcs{
				Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
					if _, ok := obj.(*opendepotv1alpha1.Agent); ok {
						return errors.New("agent update failed")
					}
					return c.Update(ctx, obj, opts...)
				},
			})

			_, err = reconcileWith(failing, false)
			Expect(err).To(MatchError("agent update failed"))
		})

		It("skips a branch-sourced skill when branch sourcing is disabled", func() {
			ref := "main"
			branchSkill := sourceSpec(skillName)
			branchSkill.VersionConstraints = ""
			branchSkill.Ref = &ref
			updateDepotSpec(func(spec *opendepotv1alpha1.DepotSpec) {
				spec.SkillConfigs = []opendepotv1alpha1.AgentSourceConfig{branchSkill}
			})

			_, err := reconcileDepot()
			Expect(err).NotTo(HaveOccurred())
			Expect(getDepot().Status.Skills).To(BeEmpty())
		})

		It("skips a branch-sourced agent when branch sourcing is disabled", func() {
			ref := "main"
			branchAgent := sourceSpec(agentName)
			branchAgent.VersionConstraints = ""
			branchAgent.Ref = &ref
			updateDepotSpec(func(spec *opendepotv1alpha1.DepotSpec) {
				spec.AgentConfigs = []opendepotv1alpha1.AgentSourceConfig{branchAgent}
			})

			_, err := reconcileDepot()
			Expect(err).NotTo(HaveOccurred())
			Expect(getDepot().Status.Agents).To(BeEmpty())
		})

		It("keeps a branch-sourced skill in status when its branch head cannot be read", func() {
			originalBranchGithubClient := newBranchGithubClient
			DeferCleanup(func() {
				newBranchGithubClient = originalBranchGithubClient
			})

			newBranchGithubClient = func(_ context.Context, _ client.Reader, _ string, _ *opendepotv1alpha1.AgentSourceConfig) (*github.Client, error) {
				return nil, errors.New("branch client unavailable")
			}

			ref := "main"
			branchSkill := sourceSpec(skillName)
			branchSkill.VersionConstraints = ""
			branchSkill.Ref = &ref
			updateDepotSpec(func(spec *opendepotv1alpha1.DepotSpec) {
				spec.SkillConfigs = []opendepotv1alpha1.AgentSourceConfig{branchSkill}
			})

			_, err := reconcileWith(k8sClient, true)
			Expect(err).NotTo(HaveOccurred())
			Expect(getDepot().Status.Skills).To(Equal([]string{skillName}))
		})
	})

	Context("when updating depot status", func() {
		It("requeues after the configured polling interval", func() {
			createDepot(opendepotv1alpha1.DepotSpec{
				PollingIntervalMinutes: func() *int { v := 5; return &v }(),
			})

			result, err := reconcileDepot()
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(5 * time.Minute))
		})

		It("returns an error when the status update fails", func() {
			createDepot(opendepotv1alpha1.DepotSpec{})

			failing := covInterceptedClient(interceptor.Funcs{
				SubResourceUpdate: func(_ context.Context, _ client.Client, _ string, _ client.Object, _ ...client.SubResourceUpdateOption) error {
					return errors.New("status update failed")
				},
			})

			_, err := reconcileWith(failing, false)
			Expect(err).To(MatchError("status update failed"))
		})
	})
})
