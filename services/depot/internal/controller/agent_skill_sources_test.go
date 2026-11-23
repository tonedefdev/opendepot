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
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
)

// githubTagsTransport answers GitHub tag listing requests with the given tags and
// GitHub App installation token requests with a placeholder token.
type githubTagsTransport struct {
	tags []string
}

func (t githubTagsTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if strings.HasSuffix(req.URL.Path, "/access_tokens") {
		return &http.Response{
			StatusCode: http.StatusCreated,
			Header:     http.Header{"Content-Type": {"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"token":"test-installation-token","expires_at":"2099-01-01T00:00:00Z"}`)),
			Request:    req,
		}, nil
	}

	tags := make([]map[string]string, 0, len(t.tags))
	for _, tag := range t.tags {
		tags = append(tags, map[string]string{"name": tag})
	}

	body, err := json.Marshal(tags)
	if err != nil {
		return nil, err
	}

	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(bytes.NewReader(body)),
		Request:    req,
	}, nil
}

// failingReleasesTransport answers every GitHub request with a server error.
type failingReleasesTransport struct{}

func (failingReleasesTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusInternalServerError,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(bytes.NewReader([]byte(`{"message":"boom"}`))),
		Request:    req,
	}, nil
}

var _ = Describe("Depot Controller agent and skill version sources", func() {
	const depotName = "agent-sources-depot"

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

	// setSourceConfigs stores the skill and agent configs on the depot and returns the directory used for storage.
	setSourceConfigs := func(skillConfigs, agentConfigs []opendepotv1alpha1.AgentSourceConfig) {
		directoryPath := GinkgoT().TempDir()
		current := &opendepotv1alpha1.Depot{}
		Expect(k8sClient.Get(ctx, depotKey, current)).To(Succeed())
		for i := range skillConfigs {
			skillConfigs[i].StorageConfig = &opendepotv1alpha1.StorageConfig{FileSystem: &opendepotv1alpha1.FileSystemConfig{DirectoryPath: &directoryPath}}
		}
		for i := range agentConfigs {
			agentConfigs[i].StorageConfig = &opendepotv1alpha1.StorageConfig{FileSystem: &opendepotv1alpha1.FileSystemConfig{DirectoryPath: &directoryPath}}
		}
		current.Spec.SkillConfigs = skillConfigs
		current.Spec.AgentConfigs = agentConfigs
		Expect(k8sClient.Update(ctx, current)).To(Succeed())
	}

	reconcileDepot := func() error {
		controllerReconciler := &DepotReconciler{
			Client:    k8sClient,
			APIReader: k8sClient,
			Scheme:    k8sClient.Scheme(),
		}
		_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: depotKey})
		return err
	}

	It("creates a skill from the semver tags that satisfy the version constraints", func() {
		http.DefaultTransport = githubTagsTransport{tags: []string{"v1.0.0", "v2.0.0", "v3.0.0", "not-a-version"}}
		skillName := "constrained-skill"
		setSourceConfigs([]opendepotv1alpha1.AgentSourceConfig{{
			Name:               &skillName,
			RepoOwner:          "tonedefdev",
			VersionConstraints: ">= 2.0.0",
		}}, nil)

		Expect(reconcileDepot()).To(Succeed())

		skill := &opendepotv1alpha1.Skill{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: skillName, Namespace: "default"}, skill)).To(Succeed())
		versions := make([]string, 0, len(skill.Spec.Versions))
		for _, v := range skill.Spec.Versions {
			versions = append(versions, v.Version)
		}
		Expect(versions).To(ConsistOf("v2.0.0", "v3.0.0"))
		DeferCleanup(func() {
			_ = k8sClient.Delete(ctx, skill)
		})
	})

	It("creates an agent from the tags that match the tag prefix and version constraints", func() {
		http.DefaultTransport = githubTagsTransport{tags: []string{"agent-v1.0.0", "agent-v1.5.0", "v9.9.9"}}
		agentName := "prefixed-agent"
		tagPrefix := "agent-"
		setSourceConfigs(nil, []opendepotv1alpha1.AgentSourceConfig{{
			Name:               &agentName,
			RepoOwner:          "tonedefdev",
			TagPrefix:          &tagPrefix,
			VersionConstraints: ">= 1.0.0",
		}})

		Expect(reconcileDepot()).To(Succeed())

		agent := &opendepotv1alpha1.Agent{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: agentName, Namespace: "default"}, agent)).To(Succeed())
		versions := make([]string, 0, len(agent.Spec.Versions))
		for _, v := range agent.Spec.Versions {
			versions = append(versions, v.Version)
		}
		Expect(versions).To(ConsistOf("v1.0.0", "v1.5.0"))
		DeferCleanup(func() {
			_ = k8sClient.Delete(ctx, agent)
		})
	})

	It("lists agent tags with the GitHub App client when the source config requests it", func() {
		signingKey, err := rsa.GenerateKey(rand.Reader, 2048)
		Expect(err).NotTo(HaveOccurred())
		keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(signingKey)})
		appSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      opendepotv1alpha1.OpenDepotGithubSecretName,
				Namespace: "default",
			},
			Type: corev1.SecretTypeOpaque,
			Data: map[string][]byte{
				opendepotv1alpha1.OpenDepotGithubSecretDataFieldAppID:      []byte("1"),
				opendepotv1alpha1.OpenDepotGithubSecretDataFieldInstallID:  []byte("2"),
				opendepotv1alpha1.OpenDepotGithubSecretDataFieldPrivateKey: []byte(base64.StdEncoding.EncodeToString(keyPEM)),
			},
		}
		Expect(k8sClient.Create(ctx, appSecret)).To(Succeed())
		DeferCleanup(func() {
			_ = k8sClient.Delete(ctx, appSecret)
		})

		http.DefaultTransport = githubTagsTransport{tags: []string{"v1.0.0", "v1.1.0"}}
		skillName := "app-client-skill"
		useAuthenticatedClient := true
		setSourceConfigs([]opendepotv1alpha1.AgentSourceConfig{{
			Name:               &skillName,
			RepoOwner:          "tonedefdev",
			VersionConstraints: ">= 1.0.0",
			GithubClientConfig: &opendepotv1alpha1.GithubClientConfig{UseAuthenticatedClient: useAuthenticatedClient},
		}}, nil)

		Expect(reconcileDepot()).To(Succeed())

		skill := &opendepotv1alpha1.Skill{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: skillName, Namespace: "default"}, skill)).To(Succeed())
		Expect(skill.Spec.Versions).To(HaveLen(2))
		DeferCleanup(func() {
			_ = k8sClient.Delete(ctx, skill)
		})
	})

	It("returns an error when GitHub fails to list the source tags", func() {
		http.DefaultTransport = failingReleasesTransport{}
		agentName := "failing-agent"
		setSourceConfigs(nil, []opendepotv1alpha1.AgentSourceConfig{{
			Name:               &agentName,
			RepoOwner:          "tonedefdev",
			VersionConstraints: ">= 1.0.0",
		}})

		Expect(reconcileDepot()).NotTo(Succeed())
	})
})
