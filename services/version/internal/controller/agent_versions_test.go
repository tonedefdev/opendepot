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
	"testing"

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
