/*
Copyright 2026.

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
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
)

func TestConfigConflictMessage(t *testing.T) {
	name := "example"
	moduleRef := &opendepotv1alpha1.ModuleConfig{Name: &name}
	providerRef := &opendepotv1alpha1.ProviderConfig{Name: &name}
	agentRef := &opendepotv1alpha1.AgentSourceConfig{Name: &name}

	tests := []struct {
		name    string
		version opendepotv1alpha1.VersionSpec
		want    string
	}{
		{
			name:    "single module reference is valid",
			version: opendepotv1alpha1.VersionSpec{ModuleConfigRef: moduleRef},
			want:    "",
		},
		{
			name:    "single agent reference is valid",
			version: opendepotv1alpha1.VersionSpec{AgentSourceRef: agentRef},
			want:    "",
		},
		{
			name:    "module and provider conflict keeps the existing message",
			version: opendepotv1alpha1.VersionSpec{ModuleConfigRef: moduleRef, ProviderConfigRef: providerRef},
			want:    "Only one of 'ModuleConfigRef' or 'ProviderConfigRef' can be provided: both are defined",
		},
		{
			name:    "agent and module conflict",
			version: opendepotv1alpha1.VersionSpec{AgentSourceRef: agentRef, ModuleConfigRef: moduleRef},
			want:    "Only one of 'AgentSourceRef', 'ModuleConfigRef', or 'ProviderConfigRef' can be provided: multiple are defined",
		},
		{
			name:    "agent and provider conflict",
			version: opendepotv1alpha1.VersionSpec{AgentSourceRef: agentRef, ProviderConfigRef: providerRef},
			want:    "Only one of 'AgentSourceRef', 'ModuleConfigRef', or 'ProviderConfigRef' can be provided: multiple are defined",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			version := &opendepotv1alpha1.Version{Spec: tt.version}
			if got := configConflictMessage(version); got != tt.want {
				t.Errorf("configConflictMessage() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestVersionImmutable(t *testing.T) {
	trueVal := true
	falseVal := false

	tests := []struct {
		name    string
		version opendepotv1alpha1.VersionSpec
		want    bool
	}{
		{
			name:    "no source config is mutable",
			version: opendepotv1alpha1.VersionSpec{},
			want:    false,
		},
		{
			name: "module immutable",
			version: opendepotv1alpha1.VersionSpec{
				ModuleConfigRef: &opendepotv1alpha1.ModuleConfig{Immutable: &trueVal},
			},
			want: true,
		},
		{
			name: "agent immutable",
			version: opendepotv1alpha1.VersionSpec{
				AgentSourceRef: &opendepotv1alpha1.AgentSourceConfig{Immutable: &trueVal},
			},
			want: true,
		},
		{
			name: "agent explicitly mutable",
			version: opendepotv1alpha1.VersionSpec{
				AgentSourceRef: &opendepotv1alpha1.AgentSourceConfig{Immutable: &falseVal},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			version := &opendepotv1alpha1.Version{Spec: tt.version}
			if got := versionImmutable(version); got != tt.want {
				t.Errorf("versionImmutable() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAgentStorageNaming(t *testing.T) {
	name := "code-review"
	bucket := "bucket"
	storageConfig := &opendepotv1alpha1.StorageConfig{
		S3: &opendepotv1alpha1.AmazonS3Config{Bucket: bucket},
	}

	tests := []struct {
		name        string
		versionType string
		wantPrefix  string
	}{
		{name: "skill", versionType: opendepotv1alpha1.OpenDepotSkill, wantPrefix: "skill-code-review"},
		{name: "agent", versionType: opendepotv1alpha1.OpenDepotAgent, wantPrefix: "agent-code-review"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			version := &opendepotv1alpha1.Version{
				Spec: opendepotv1alpha1.VersionSpec{
					Type:           tt.versionType,
					AgentSourceRef: &opendepotv1alpha1.AgentSourceConfig{Name: &name, StorageConfig: storageConfig},
				},
			}

			gotName, err := getVersionName(version)
			if err != nil {
				t.Fatalf("getVersionName() error = %v", err)
			}

			if *gotName != tt.wantPrefix {
				t.Errorf("getVersionName() = %q, want %q", *gotName, tt.wantPrefix)
			}

			gotConfig, err := getVersionStorageConfig(version)
			if err != nil {
				t.Fatalf("getVersionStorageConfig() error = %v", err)
			}

			if gotConfig != storageConfig {
				t.Errorf("getVersionStorageConfig() did not return the agentSourceRef storage config")
			}
		})
	}
}

func TestNeedsDefinitionBackfill(t *testing.T) {
	ref := &opendepotv1alpha1.DefinitionConfigMapRef{Name: "example-definition", Key: "definition", FileName: "SKILL.md"}

	tests := []struct {
		name    string
		version opendepotv1alpha1.Version
		want    bool
	}{
		{
			name:    "skill without a definition reference needs backfill",
			version: opendepotv1alpha1.Version{Spec: opendepotv1alpha1.VersionSpec{Type: opendepotv1alpha1.OpenDepotSkill}},
			want:    true,
		},
		{
			name:    "agent without a definition reference needs backfill",
			version: opendepotv1alpha1.Version{Spec: opendepotv1alpha1.VersionSpec{Type: opendepotv1alpha1.OpenDepotAgent}},
			want:    true,
		},
		{
			name: "agent with a definition reference does not need backfill",
			version: opendepotv1alpha1.Version{
				Spec:   opendepotv1alpha1.VersionSpec{Type: opendepotv1alpha1.OpenDepotAgent},
				Status: opendepotv1alpha1.VersionStatus{DefinitionConfigMapRef: ref},
			},
			want: false,
		},
		{
			name:    "module without a definition reference does not need backfill",
			version: opendepotv1alpha1.Version{Spec: opendepotv1alpha1.VersionSpec{Type: opendepotv1alpha1.OpenDepotModule}},
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := needsDefinitionBackfill(&tt.version); got != tt.want {
				t.Errorf("needsDefinitionBackfill() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBlockedAndFailedWritesKeepDefinitionRef(t *testing.T) {
	ctx := context.Background()
	ref := &opendepotv1alpha1.DefinitionConfigMapRef{Name: "code-review-abc1234-definition", Key: "definition", FileName: "SKILL.md"}
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "code-review-abc1234", Namespace: "default"}}

	tests := []struct {
		name  string
		write func(r *VersionReconciler) error
	}{
		{
			name: "held branch block keeps the definition reference",
			write: func(r *VersionReconciler) error {
				return r.persistAgentBlock(ctx, req, nil, nil, "Agent scan failed: boom")
			},
		},
		{
			name: "sync status write keeps the definition reference",
			write: func(r *VersionReconciler) error {
				return r.persistSyncStatus(ctx, req, false, "version is marked immutable")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, _ := newDefinitionFixture(t, interceptor.Funcs{})

			current := &opendepotv1alpha1.Version{}
			if err := r.Get(ctx, req.NamespacedName, current); err != nil {
				t.Fatalf("Get: %v", err)
			}

			current.Status.DefinitionConfigMapRef = ref
			if err := r.Status().Update(ctx, current); err != nil {
				t.Fatalf("seed status: %v", err)
			}

			if err := tt.write(r); err != nil {
				t.Fatalf("write: %v", err)
			}

			got := &opendepotv1alpha1.Version{}
			if err := r.Get(ctx, req.NamespacedName, got); err != nil {
				t.Fatalf("Get after write: %v", err)
			}

			if got.Status.DefinitionConfigMapRef == nil || got.Status.DefinitionConfigMapRef.Name != ref.Name {
				t.Errorf("DefinitionConfigMapRef = %+v, want %+v", got.Status.DefinitionConfigMapRef, ref)
			}
		})
	}
}

func TestClearStaleReadmeRefOnlyChangesAgentVersions(t *testing.T) {
	reconciler, agent := newDefinitionFixture(t, interceptor.Funcs{})
	ctx := context.Background()

	module := &opendepotv1alpha1.Version{
		ObjectMeta: metav1.ObjectMeta{Name: "example-module-1.0.0", Namespace: agent.Namespace},
		Spec:       opendepotv1alpha1.VersionSpec{Type: opendepotv1alpha1.OpenDepotModule},
	}
	if err := reconciler.Create(ctx, module); err != nil {
		t.Fatalf("create module version: %v", err)
	}

	stale := &opendepotv1alpha1.ReadmeConfigMapRef{Name: "code-review-abc1234-readme", Key: "README.md"}
	for _, version := range []*opendepotv1alpha1.Version{agent, module} {
		current := &opendepotv1alpha1.Version{}
		if err := reconciler.Get(ctx, client.ObjectKeyFromObject(version), current); err != nil {
			t.Fatalf("get %s: %v", version.Name, err)
		}

		current.Status.ReadmeConfigMapRef = stale
		if err := reconciler.Status().Update(ctx, current); err != nil {
			t.Fatalf("set readme ref on %s: %v", version.Name, err)
		}
	}

	for _, version := range []*opendepotv1alpha1.Version{agent, module} {
		current := &opendepotv1alpha1.Version{}
		if err := reconciler.Get(ctx, client.ObjectKeyFromObject(version), current); err != nil {
			t.Fatalf("get %s: %v", version.Name, err)
		}

		req := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(version)}
		if err := reconciler.clearStaleReadmeRef(ctx, req, current); err != nil {
			t.Fatalf("clearStaleReadmeRef(%s) error = %v", version.Name, err)
		}
	}

	stored := &opendepotv1alpha1.Version{}
	if err := reconciler.Get(ctx, client.ObjectKeyFromObject(agent), stored); err != nil {
		t.Fatalf("get agent: %v", err)
	}

	if stored.Status.ReadmeConfigMapRef != nil {
		t.Fatalf("expected stale readme ref cleared on agent version, got %+v", stored.Status.ReadmeConfigMapRef)
	}

	if err := reconciler.Get(ctx, client.ObjectKeyFromObject(module), stored); err != nil {
		t.Fatalf("get module: %v", err)
	}

	if stored.Status.ReadmeConfigMapRef == nil || *stored.Status.ReadmeConfigMapRef != *stale {
		t.Fatalf("expected module readme ref untouched, got %+v", stored.Status.ReadmeConfigMapRef)
	}
}
