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
	"context"
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
)

// covInterceptedClient wraps a watch-capable client so individual API calls can be forced to fail.
func covInterceptedClient(funcs interceptor.Funcs) client.Client {
	base, err := client.NewWithWatch(cfg, client.Options{Scheme: k8sClient.Scheme()})
	Expect(err).NotTo(HaveOccurred())

	return interceptor.NewClient(base, funcs)
}

var _ = Describe("Agent Controller reconcile coverage", func() {
	const (
		resourceName = "cov-agent"
		branchCommit = "89abcdef0123456789abcdef0123456789abcdef"
	)

	ctx := context.Background()

	key := types.NamespacedName{
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
					Path:                "agents/cov-agent",
					VersionHistoryLimit: limit,
				},
				Versions: versions,
			},
		}
	}

	reconcileWith := func(c client.Client) (reconcile.Result, error) {
		controllerReconciler := &AgentReconciler{
			Client: c,
			Scheme: k8sClient.Scheme(),
		}

		return controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key})
	}

	getAgent := func() *opendepotv1alpha1.Agent {
		agent := &opendepotv1alpha1.Agent{}
		Expect(k8sClient.Get(ctx, key, agent)).To(Succeed())
		return agent
	}

	AfterEach(func() {
		Expect(k8sClient.DeleteAllOf(ctx, &opendepotv1alpha1.Version{}, client.InNamespace("default"),
			client.MatchingLabels{"opendepot.defdev.io/agent": resourceName})).To(Succeed())

		agent := &opendepotv1alpha1.Agent{}
		if err := k8sClient.Get(ctx, key, agent); err == nil {
			Expect(k8sClient.Delete(ctx, agent)).To(Succeed())
		}
	})

	It("returns no error when the agent does not exist", func() {
		result, err := reconcileWith(k8sClient)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(BeZero())
	})

	It("returns the error when reading the agent fails", func() {
		failing := covInterceptedClient(interceptor.Funcs{
			Get: func(_ context.Context, _ client.WithWatch, _ client.ObjectKey, _ client.Object, _ ...client.GetOption) error {
				return errors.New("agent read failed")
			},
		})

		_, err := reconcileWith(failing)
		Expect(err).To(MatchError("agent read failed"))
	})

	It("returns the error and leaves status unsynced when a Version cannot be created", func() {
		Expect(k8sClient.Create(ctx, newAgent([]opendepotv1alpha1.AgentVersion{{Version: "1.0.0"}}, nil))).To(Succeed())

		failing := covInterceptedClient(interceptor.Funcs{
			Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
				if _, ok := obj.(*opendepotv1alpha1.Version); ok {
					return errors.New("version create failed")
				}
				return c.Create(ctx, obj, opts...)
			},
		})

		_, err := reconcileWith(failing)
		Expect(err).To(HaveOccurred())
		Expect(getAgent().Status.Synced).To(BeFalse())
		Expect(getAgent().Status.VersionRefs).To(BeEmpty())
	})

	It("returns the error when reading a branch Version fails", func() {
		ref := "main"
		agent := newAgent([]opendepotv1alpha1.AgentVersion{{Commit: branchCommit, State: "Pending"}}, nil)
		agent.Spec.AgentSourceConfig.Ref = &ref
		Expect(k8sClient.Create(ctx, agent)).To(Succeed())

		failing := covInterceptedClient(interceptor.Funcs{
			Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if _, ok := obj.(*opendepotv1alpha1.Version); ok {
					return errors.New("branch version read failed")
				}
				return c.Get(ctx, key, obj, opts...)
			},
		})

		_, err := reconcileWith(failing)
		Expect(err).To(MatchError(ContainSubstring("branch version read failed")))
	})

	It("returns the error when trimming versions cannot be persisted", func() {
		limit := 1
		Expect(k8sClient.Create(ctx, newAgent([]opendepotv1alpha1.AgentVersion{{Version: "1.0.0"}, {Version: "1.1.0"}}, &limit))).To(Succeed())

		failing := covInterceptedClient(interceptor.Funcs{
			Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
				if _, ok := obj.(*opendepotv1alpha1.Agent); ok {
					return errors.New("agent trim update failed")
				}
				return c.Update(ctx, obj, opts...)
			},
		})

		_, err := reconcileWith(failing)
		Expect(err).To(MatchError("agent trim update failed"))
	})

	It("trims the agent entries to the version history limit", func() {
		limit := 1
		Expect(k8sClient.Create(ctx, newAgent([]opendepotv1alpha1.AgentVersion{{Version: "1.0.0"}, {Version: "1.1.0"}}, &limit))).To(Succeed())

		_, err := reconcileWith(k8sClient)
		Expect(err).NotTo(HaveOccurred())
		Expect(getAgent().Spec.Versions).To(HaveLen(1))
		Expect(getAgent().Spec.Versions[0].Version).To(Equal("1.1.0"))
	})

	It("returns the error when clearing ForceSync fails", func() {
		agent := newAgent([]opendepotv1alpha1.AgentVersion{{Version: "1.0.0"}}, nil)
		agent.Spec.ForceSync = true
		Expect(k8sClient.Create(ctx, agent)).To(Succeed())

		failing := covInterceptedClient(interceptor.Funcs{
			Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
				if _, ok := obj.(*opendepotv1alpha1.Agent); ok {
					return errors.New("force sync update failed")
				}
				return c.Update(ctx, obj, opts...)
			},
		})

		_, err := reconcileWith(failing)
		Expect(err).To(MatchError("force sync update failed"))
		Expect(getAgent().Spec.ForceSync).To(BeTrue())
	})

	It("clears ForceSync after a successful reconcile", func() {
		agent := newAgent([]opendepotv1alpha1.AgentVersion{{Version: "1.0.0"}}, nil)
		agent.Spec.ForceSync = true
		Expect(k8sClient.Create(ctx, agent)).To(Succeed())

		_, err := reconcileWith(k8sClient)
		Expect(err).NotTo(HaveOccurred())
		Expect(getAgent().Spec.ForceSync).To(BeFalse())
	})

	It("returns the error when the status update fails", func() {
		Expect(k8sClient.Create(ctx, newAgent([]opendepotv1alpha1.AgentVersion{{Version: "1.0.0"}}, nil))).To(Succeed())

		failing := covInterceptedClient(interceptor.Funcs{
			SubResourceUpdate: func(_ context.Context, _ client.Client, _ string, _ client.Object, _ ...client.SubResourceUpdateOption) error {
				return errors.New("agent status update failed")
			},
		})

		_, err := reconcileWith(failing)
		Expect(err).To(MatchError("agent status update failed"))
		Expect(getAgent().Status.Synced).To(BeFalse())
	})

	It("returns the error when listing Versions for removal fails", func() {
		Expect(k8sClient.Create(ctx, newAgent([]opendepotv1alpha1.AgentVersion{{Version: "1.0.0"}}, nil))).To(Succeed())

		failing := covInterceptedClient(interceptor.Funcs{
			List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
				if _, ok := list.(*opendepotv1alpha1.VersionList); ok {
					return errors.New("version list failed")
				}
				return c.List(ctx, list, opts...)
			},
		})

		_, err := reconcileWith(failing)
		Expect(err).To(MatchError("version list failed"))
	})
})
