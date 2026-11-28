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
	"net/http"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
)

var _ = Describe("Depot Controller OCI build artifact discovery", func() {
	const depotName = "oci-build-depot"

	ctx := context.Background()

	depotKey := types.NamespacedName{
		Name:      depotName,
		Namespace: "default",
	}

	var originalTransport http.RoundTripper

	BeforeEach(func() {
		originalTransport = http.DefaultTransport
		Expect(k8sClient.Create(ctx, &opendepotv1alpha1.Depot{
			ObjectMeta: metav1.ObjectMeta{
				Name:      depotName,
				Namespace: "default",
			},
		})).To(Succeed())
	})

	AfterEach(func() {
		http.DefaultTransport = originalTransport

		depot := &opendepotv1alpha1.Depot{}
		Expect(k8sClient.Get(ctx, depotKey, depot)).To(Succeed())
		Expect(k8sClient.Delete(ctx, depot)).To(Succeed())
	})

	It("discovers build tags that satisfy versionConstraints", func() {
		http.DefaultTransport = githubTagsTransport{tags: []string{discoveryTagV100, discoveryTagV200, discoveryTagV300, discoveryTagNotSemver}}

		artifactName := "built-constrained"
		setDiscoveryDepotSpec(ctx, depotKey, func(depot *opendepotv1alpha1.Depot) {
			depot.Spec.OCIArtifactConfigs = []opendepotv1alpha1.OCIArtifactConfig{{
				Name:         &artifactName,
				ArtifactType: discoveryArtifactImage,
				Build: &opendepotv1alpha1.OCIBuildSource{
					RepoOwner: discoveryRepoOwner,
				},
				VersionConstraints: ">= 2.0.0",
			}}
		})

		Expect(reconcileDiscoveryDepot(ctx, depotKey, newDiscoveryReconciler())).To(Succeed())

		artifact := &opendepotv1alpha1.OCIArtifact{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: artifactName, Namespace: "default"}, artifact)).To(Succeed())
		DeferCleanup(func() {
			_ = k8sClient.Delete(ctx, artifact)
		})

		names := make([]string, 0, len(artifact.Spec.Versions))
		for _, v := range artifact.Spec.Versions {
			names = append(names, v.Name)
		}
		Expect(names).To(ConsistOf(discoveryTagV200, discoveryTagV300))
		Expect(mustGetDepot(ctx, depotKey).Status.OCIArtifacts).To(ConsistOf(artifactName))
	})

	It("discovers build tags that match tagPattern instead of versionConstraints", func() {
		http.DefaultTransport = githubTagsTransport{tags: []string{discoveryTagV100, discoveryTagV200, discoveryTagV210, "release-2.0.0"}}

		artifactName := "built-pattern"
		pattern := `^v2\.`
		setDiscoveryDepotSpec(ctx, depotKey, func(depot *opendepotv1alpha1.Depot) {
			depot.Spec.OCIArtifactConfigs = []opendepotv1alpha1.OCIArtifactConfig{{
				Name:         &artifactName,
				ArtifactType: discoveryArtifactImage,
				Build: &opendepotv1alpha1.OCIBuildSource{
					RepoOwner: discoveryRepoOwner,
				},
				TagPattern: &pattern,
			}}
		})

		Expect(reconcileDiscoveryDepot(ctx, depotKey, newDiscoveryReconciler())).To(Succeed())

		artifact := &opendepotv1alpha1.OCIArtifact{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: artifactName, Namespace: "default"}, artifact)).To(Succeed())
		DeferCleanup(func() {
			_ = k8sClient.Delete(ctx, artifact)
		})

		names := make([]string, 0, len(artifact.Spec.Versions))
		for _, v := range artifact.Spec.Versions {
			names = append(names, v.Name)
		}
		Expect(names).To(ConsistOf(discoveryTagV200, discoveryTagV210))
	})
})
