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
	"encoding/base64"
	"net/http"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// wave7mNotFoundTransport replaces http.DefaultTransport so every request gets a 404, which makes GitHub API lookups fail.
func wave7mNotFoundTransport(t *testing.T) {
	t.Helper()

	original := http.DefaultTransport
	http.DefaultTransport = jevRoundTripper(func(req *http.Request) (*http.Response, error) {
		return jevHTTPResponse(req, http.StatusNotFound, nil, []byte("not found")), nil
	})
	t.Cleanup(func() { http.DefaultTransport = original })
}

// wave7mModuleConfigVersion returns a module Version whose ModuleConfigRef names a module.
func wave7mModuleConfigVersion(name string) *opendepotv1alpha1.Version {
	module := "vpc"
	version := wave6KModuleVersion(name, nil)
	repoURL := "https://github.com/acme/terraform-vpc"
	version.Spec.ModuleConfigRef = &opendepotv1alpha1.ModuleConfig{
		Name:      &module,
		RepoOwner: "acme",
		RepoUrl:   &repoURL,
	}

	return version
}

func TestWave7MUpsertReadmeConfigMapCreatesAndUpdates(t *testing.T) {
	version := wave7mModuleConfigVersion("vpc-1-0-0")
	version.Namespace = "default"
	reconciler := helpersCovReconciler(t, version)

	ref, err := reconciler.upsertReadmeConfigMap(context.Background(), version, []byte("# First"))
	if err != nil {
		t.Fatalf("upsertReadmeConfigMap create: %v", err)
	}

	if ref.Name != "vpc-1-0-0-readme" || ref.Key != readmeConfigMapKey {
		t.Fatalf("ref = %+v", ref)
	}

	var cm corev1.ConfigMap
	if err := reconciler.Get(context.Background(), types.NamespacedName{Name: ref.Name, Namespace: "default"}, &cm); err != nil {
		t.Fatalf("get configmap: %v", err)
	}

	raw, _ := base64.StdEncoding.DecodeString(cm.Data[readmeConfigMapKey])
	if string(raw) != "# First" {
		t.Fatalf("readme data = %q", raw)
	}

	if cm.Labels["opendepot.defdev.io/module"] != "vpc" || cm.Labels["opendepot.defdev.io/version"] != "vpc-1-0-0" {
		t.Fatalf("labels = %v", cm.Labels)
	}

	if len(cm.OwnerReferences) != 1 || cm.OwnerReferences[0].Name != "vpc-1-0-0" {
		t.Fatalf("owner references = %v, want the Version", cm.OwnerReferences)
	}

	if _, err := reconciler.upsertReadmeConfigMap(context.Background(), version, []byte("# Second")); err != nil {
		t.Fatalf("upsertReadmeConfigMap update: %v", err)
	}

	if err := reconciler.Get(context.Background(), types.NamespacedName{Name: ref.Name, Namespace: "default"}, &cm); err != nil {
		t.Fatalf("get updated configmap: %v", err)
	}

	raw, _ = base64.StdEncoding.DecodeString(cm.Data[readmeConfigMapKey])
	if string(raw) != "# Second" {
		t.Fatalf("updated readme data = %q", raw)
	}
}

func TestWave7MUpsertReadmeConfigMapWithoutModuleName(t *testing.T) {
	version := wave7mModuleConfigVersion("vpc-2-0-0")
	version.Namespace = "default"
	version.Spec.ModuleConfigRef.Name = nil
	reconciler := helpersCovReconciler(t, version)

	ref, err := reconciler.upsertReadmeConfigMap(context.Background(), version, []byte("body"))
	if err != nil {
		t.Fatalf("upsertReadmeConfigMap: %v", err)
	}

	var cm corev1.ConfigMap
	if err := reconciler.Get(context.Background(), types.NamespacedName{Name: ref.Name, Namespace: "default"}, &cm); err != nil {
		t.Fatalf("get configmap: %v", err)
	}

	if _, ok := cm.Labels["opendepot.defdev.io/module"]; ok {
		t.Fatalf("module label set without a module name: %v", cm.Labels)
	}
}

func TestWave7MUpsertReadmeConfigMapReportsWriteFailure(t *testing.T) {
	version := wave7mModuleConfigVersion("vpc-3-0-0")
	version.Namespace = "default"
	reconciler := helpersCovReconciler(t)
	reconciler.Client = fake.NewClientBuilder().WithScheme(runtime.NewScheme()).Build()

	if _, err := reconciler.upsertReadmeConfigMap(context.Background(), version, []byte("body")); err == nil {
		t.Fatal("upsertReadmeConfigMap succeeded without the scheme registered for ConfigMaps")
	}
}

func TestWave7MFetchModuleReadmeFallsBackToArchive(t *testing.T) {
	wave7mNotFoundTransport(t)

	version := wave7mModuleConfigVersion("vpc-4-0-0")
	version.Namespace = "default"
	reconciler := helpersCovReconciler(t, version)
	reconciler.Log = logr.Discard()

	archive := versionCovTarGz(t, "README.md")
	readme, err := reconciler.fetchModuleReadme(context.Background(), version, archive)
	if err != nil {
		t.Fatalf("fetchModuleReadme: %v", err)
	}

	if len(readme) == 0 {
		t.Fatal("fetchModuleReadme returned no README from the archive fallback")
	}
}

func TestWave7MFetchModuleReadmeAuthenticatedWithoutSecret(t *testing.T) {
	wave7mNotFoundTransport(t)

	version := wave7mModuleConfigVersion("vpc-5-0-0")
	version.Namespace = "default"
	version.Spec.ModuleConfigRef.GithubClientConfig = &opendepotv1alpha1.GithubClientConfig{UseAuthenticatedClient: true}
	reconciler := helpersCovReconciler(t, version)
	reconciler.APIReader = reconciler.Client
	reconciler.Log = logr.Discard()

	readme, err := reconciler.fetchModuleReadme(context.Background(), version, versionCovTarGz(t, "README.md"))
	if err != nil {
		t.Fatalf("fetchModuleReadme: %v", err)
	}

	if len(readme) == 0 {
		t.Fatal("fetchModuleReadme did not fall back to the archive when the GitHub secret is missing")
	}
}

func TestWave7MFetchModuleReadmeNoReadmeAnywhere(t *testing.T) {
	wave7mNotFoundTransport(t)

	version := wave7mModuleConfigVersion("vpc-6-0-0")
	version.Namespace = "default"
	reconciler := helpersCovReconciler(t, version)
	reconciler.Log = logr.Discard()

	readme, err := reconciler.fetchModuleReadme(context.Background(), version, versionCovTarGz(t, "main.tf"))
	if err != nil {
		t.Fatalf("fetchModuleReadme: %v", err)
	}

	if readme != nil {
		t.Fatalf("fetchModuleReadme = %q, want nil when no README exists", readme)
	}
}

func TestWave7MBuildContractForArchiveRequiresBytes(t *testing.T) {
	reconciler := helpersCovReconciler(t)
	version := wave7mModuleConfigVersion("vpc-7-0-0")

	_, err := reconciler.buildContractForArchive(context.Background(), version, nil, contractMetaFor(version))
	if err == nil || !strings.Contains(err.Error(), "no module archive bytes") {
		t.Fatalf("err = %v, want a missing-archive error", err)
	}
}

func TestWave7MBuildContractForArchiveRejectsInvalidArchive(t *testing.T) {
	reconciler := helpersCovReconciler(t)
	version := wave7mModuleConfigVersion("vpc-8-0-0")

	if _, err := reconciler.buildContractForArchive(context.Background(), version, []byte("not an archive"), contractMetaFor(version)); err == nil {
		t.Fatal("buildContractForArchive accepted bytes that are not an archive")
	}
}

func TestWave7MBuildContractForArchiveBuildsModuleContract(t *testing.T) {
	reconciler := helpersCovReconciler(t)
	version := wave7mModuleConfigVersion("vpc-9-0-0")

	archive := moduleTestArchive(t)
	contract, err := reconciler.buildContractForArchive(context.Background(), version, archive, contractMetaFor(version))
	if err != nil {
		t.Fatalf("buildContractForArchive: %v", err)
	}

	if contract == nil {
		t.Fatal("buildContractForArchive returned a nil contract")
	}

	encoded, err := encodeContract(contract)
	if err != nil || encoded == "" {
		t.Fatalf("encodeContract = %q, %v", encoded, err)
	}
}

func TestWave7MFetchAgentArchiveAtCommit(t *testing.T) {
	files := map[string]string{"SKILL.md": jevEntry}
	stubJevGitHub(t, jevReconcileTarball(t, files))

	repoURL := "https://github.com/acme/agents"
	version := newJevTestVersion(nil, nil)
	version.Name = "agent-1-0-0"
	version.Namespace = "default"
	version.Spec.Version = "1.0.0"
	version.Spec.AgentSourceRef.RepoOwner = "acme"
	version.Spec.AgentSourceRef.RepoUrl = &repoURL
	version.Spec.AgentSourceRef.Path = "skills/agent"
	version.Spec.AgentSourceRef.StorageConfig = &opendepotv1alpha1.StorageConfig{}
	reconciler := wave6KReconciler(t, version)
	reconciler.Log = logr.Discard()

	packed, checksum, err := reconciler.fetchAgentArchiveAt(context.Background(), version, "0123456789abcdef0123456789abcdef01234567")
	if err != nil {
		t.Fatalf("fetchAgentArchiveAt: %v", err)
	}

	if len(packed) == 0 || checksum == nil || *checksum == "" {
		t.Fatalf("fetchAgentArchiveAt returned packed=%d bytes, checksum=%v", len(packed), checksum)
	}
}
