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

package provider

import (
	"context"
	"errors"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
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

var _ = Describe("Provider Controller reconcile coverage", func() {
	const (
		testNamespace = "default"
		providerName  = "cov-provider"
	)

	key := types.NamespacedName{Name: providerName, Namespace: testNamespace}

	newProvider := func(versions []opendepotv1alpha1.ProviderVersion, limit *int) *opendepotv1alpha1.Provider {
		return &opendepotv1alpha1.Provider{
			ObjectMeta: metav1.ObjectMeta{
				Name:      providerName,
				Namespace: testNamespace,
			},
			Spec: opendepotv1alpha1.ProviderSpec{
				ProviderConfig: opendepotv1alpha1.ProviderConfig{
					OperatingSystems:    []string{"linux"},
					Architectures:       []string{"amd64"},
					VersionHistoryLimit: limit,
				},
				Versions: versions,
			},
		}
	}

	newStaleVersion := func() *opendepotv1alpha1.Version {
		return &opendepotv1alpha1.Version{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cov-provider-0-9-0-linux-amd64",
				Namespace: testNamespace,
				Labels: map[string]string{
					"opendepot.defdev.io/provider":  providerName,
					"opendepot.defdev.io/namespace": testNamespace,
				},
			},
			Spec: opendepotv1alpha1.VersionSpec{
				Architecture:    "amd64",
				OperatingSystem: "linux",
				Type:            opendepotv1alpha1.OpenDepotProvider,
				Version:         "0.9.0",
			},
		}
	}

	reconcileWith := func(c client.Client, scheme *runtime.Scheme) (reconcile.Result, error) {
		controllerReconciler := &ProviderReconciler{
			Client: c,
			Log:    logr.Discard(),
			Scheme: scheme,
		}

		return controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key})
	}

	reconcileDefault := func(c client.Client) (reconcile.Result, error) {
		return reconcileWith(c, k8sClient.Scheme())
	}

	getProvider := func() *opendepotv1alpha1.Provider {
		provider := &opendepotv1alpha1.Provider{}
		Expect(k8sClient.Get(ctx, key, provider)).To(Succeed())
		return provider
	}

	AfterEach(func() {
		versionList := &opendepotv1alpha1.VersionList{}
		Expect(k8sClient.List(ctx, versionList, client.InNamespace(testNamespace),
			client.MatchingLabels{"opendepot.defdev.io/provider": providerName})).To(Succeed())
		for i := range versionList.Items {
			Expect(k8sClient.Delete(ctx, &versionList.Items[i])).To(Succeed())
		}

		provider := &opendepotv1alpha1.Provider{}
		if err := k8sClient.Get(ctx, key, provider); err == nil {
			Expect(k8sClient.Delete(ctx, provider)).To(Succeed())
		}
	})

	It("returns the error when reading the provider fails", func() {
		failing := covInterceptedClient(interceptor.Funcs{
			Get: func(_ context.Context, _ client.WithWatch, _ client.ObjectKey, _ client.Object, _ ...client.GetOption) error {
				return errors.New("provider read failed")
			},
		})

		_, err := reconcileDefault(failing)
		Expect(err).To(MatchError("provider read failed"))
	})

	It("returns an error when operating systems are empty", func() {
		provider := newProvider([]opendepotv1alpha1.ProviderVersion{{Version: "1.0.0"}}, nil)
		provider.Spec.ProviderConfig.OperatingSystems = nil
		Expect(k8sClient.Create(ctx, provider)).To(Succeed())

		_, err := reconcileDefault(k8sClient)
		Expect(err).To(MatchError("the provider operatingSystems field cannot be empty"))
	})

	It("returns an error when architectures are empty", func() {
		provider := newProvider([]opendepotv1alpha1.ProviderVersion{{Version: "1.0.0"}}, nil)
		provider.Spec.ProviderConfig.Architectures = nil
		Expect(k8sClient.Create(ctx, provider)).To(Succeed())

		_, err := reconcileDefault(k8sClient)
		Expect(err).To(MatchError("the provider architectures field cannot be empty"))
	})

	It("returns the error when trimming versions cannot be persisted", func() {
		limit := 1
		Expect(k8sClient.Create(ctx, newProvider([]opendepotv1alpha1.ProviderVersion{{Version: "1.0.0"}, {Version: "1.1.0"}}, &limit))).To(Succeed())

		failing := covInterceptedClient(interceptor.Funcs{
			Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
				if _, ok := obj.(*opendepotv1alpha1.Provider); ok {
					return errors.New("provider trim update failed")
				}
				return c.Update(ctx, obj, opts...)
			},
		})

		_, err := reconcileDefault(failing)
		Expect(err).To(MatchError("provider trim update failed"))
	})

	It("trims provider versions to the history limit", func() {
		limit := 1
		Expect(k8sClient.Create(ctx, newProvider([]opendepotv1alpha1.ProviderVersion{{Version: "1.0.0"}, {Version: "1.1.0"}}, &limit))).To(Succeed())

		_, err := reconcileDefault(k8sClient)
		Expect(err).NotTo(HaveOccurred())
		Expect(getProvider().Spec.Versions).To(HaveLen(1))
		Expect(getProvider().Spec.Versions[0].Version).To(Equal("v1.1.0"))
	})

	It("returns the error when a Version lookup fails with a non-NotFound error", func() {
		Expect(k8sClient.Create(ctx, newProvider([]opendepotv1alpha1.ProviderVersion{{Version: "1.0.0"}}, nil))).To(Succeed())

		failing := covInterceptedClient(interceptor.Funcs{
			Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if _, ok := obj.(*opendepotv1alpha1.Version); ok {
					return errors.New("version read failed")
				}
				return c.Get(ctx, key, obj, opts...)
			},
		})

		_, err := reconcileDefault(failing)
		Expect(err).To(MatchError("version read failed"))
	})

	It("returns the error when a Version cannot be created", func() {
		Expect(k8sClient.Create(ctx, newProvider([]opendepotv1alpha1.ProviderVersion{{Version: "1.0.0"}}, nil))).To(Succeed())

		failing := covInterceptedClient(interceptor.Funcs{
			Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
				if _, ok := obj.(*opendepotv1alpha1.Version); ok {
					return errors.New("version create failed")
				}
				return c.Create(ctx, obj, opts...)
			},
		})

		_, err := reconcileDefault(failing)
		Expect(err).To(MatchError("version create failed"))
		Expect(getProvider().Status.Synced).To(BeFalse())
	})

	It("returns the error when an existing Version cannot be updated", func() {
		Expect(k8sClient.Create(ctx, newProvider([]opendepotv1alpha1.ProviderVersion{{Version: "1.0.0"}}, nil))).To(Succeed())

		_, err := reconcileDefault(k8sClient)
		Expect(err).NotTo(HaveOccurred())

		failing := covInterceptedClient(interceptor.Funcs{
			Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
				if _, ok := obj.(*opendepotv1alpha1.Version); ok {
					return errors.New("version update failed")
				}
				return c.Update(ctx, obj, opts...)
			},
		})

		_, err = reconcileDefault(failing)
		Expect(err).To(MatchError("version update failed"))
	})

	It("returns the error when the Version controller reference cannot be set", func() {
		Expect(k8sClient.Create(ctx, newProvider([]opendepotv1alpha1.ProviderVersion{{Version: "1.0.0"}}, nil))).To(Succeed())

		_, err := reconcileWith(k8sClient, runtime.NewScheme())
		Expect(err).To(HaveOccurred())
	})

	It("returns the error when clearing ForceSync fails", func() {
		provider := newProvider([]opendepotv1alpha1.ProviderVersion{{Version: "1.0.0"}}, nil)
		provider.Spec.ForceSync = true
		Expect(k8sClient.Create(ctx, provider)).To(Succeed())

		failing := covInterceptedClient(interceptor.Funcs{
			Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
				if _, ok := obj.(*opendepotv1alpha1.Provider); ok {
					return errors.New("provider force sync update failed")
				}
				return c.Update(ctx, obj, opts...)
			},
		})

		_, err := reconcileDefault(failing)
		Expect(err).To(MatchError("provider force sync update failed"))
		Expect(getProvider().Spec.ForceSync).To(BeTrue())
	})

	It("clears ForceSync after a successful reconcile", func() {
		provider := newProvider([]opendepotv1alpha1.ProviderVersion{{Version: "1.0.0"}}, nil)
		provider.Spec.ForceSync = true
		Expect(k8sClient.Create(ctx, provider)).To(Succeed())

		_, err := reconcileDefault(k8sClient)
		Expect(err).NotTo(HaveOccurred())
		Expect(getProvider().Spec.ForceSync).To(BeFalse())
	})

	It("returns the error when the status update fails", func() {
		Expect(k8sClient.Create(ctx, newProvider([]opendepotv1alpha1.ProviderVersion{{Version: "1.0.0"}}, nil))).To(Succeed())

		failing := covInterceptedClient(interceptor.Funcs{
			SubResourceUpdate: func(_ context.Context, _ client.Client, _ string, _ client.Object, _ ...client.SubResourceUpdateOption) error {
				return errors.New("provider status update failed")
			},
		})

		_, err := reconcileDefault(failing)
		Expect(err).To(MatchError("provider status update failed"))
		Expect(getProvider().Status.Synced).To(BeFalse())
	})

	It("returns the error when listing Versions for removal fails", func() {
		Expect(k8sClient.Create(ctx, newProvider([]opendepotv1alpha1.ProviderVersion{{Version: "1.0.0"}}, nil))).To(Succeed())

		failing := covInterceptedClient(interceptor.Funcs{
			List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
				if _, ok := list.(*opendepotv1alpha1.VersionList); ok {
					return errors.New("version list failed")
				}
				return c.List(ctx, list, opts...)
			},
		})

		_, err := reconcileDefault(failing)
		Expect(err).To(MatchError("version list failed"))
	})

	It("returns the error when a stale Version cannot be deleted", func() {
		Expect(k8sClient.Create(ctx, newProvider([]opendepotv1alpha1.ProviderVersion{{Version: "1.0.0"}}, nil))).To(Succeed())

		_, err := reconcileDefault(k8sClient)
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Create(ctx, newStaleVersion())).To(Succeed())

		failing := covInterceptedClient(interceptor.Funcs{
			Delete: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
				if _, ok := obj.(*opendepotv1alpha1.Version); ok {
					return errors.New("version delete failed")
				}
				return c.Delete(ctx, obj, opts...)
			},
		})

		_, err = reconcileDefault(failing)
		Expect(err).To(MatchError("version delete failed"))
	})

	It("removes stale Versions that are no longer declared", func() {
		Expect(k8sClient.Create(ctx, newProvider([]opendepotv1alpha1.ProviderVersion{{Version: "1.0.0"}}, nil))).To(Succeed())
		Expect(k8sClient.Create(ctx, newStaleVersion())).To(Succeed())

		_, err := reconcileDefault(k8sClient)
		Expect(err).NotTo(HaveOccurred())

		err = k8sClient.Get(ctx, client.ObjectKey{Name: "cov-provider-0-9-0-linux-amd64", Namespace: testNamespace}, &opendepotv1alpha1.Version{})
		Expect(err).To(HaveOccurred())

		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: "cov-provider-1-0-0-linux-amd64", Namespace: testNamespace}, &opendepotv1alpha1.Version{})).To(Succeed())
	})
})
