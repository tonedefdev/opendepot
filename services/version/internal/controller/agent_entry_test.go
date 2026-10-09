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
	"os"
	"path/filepath"
	"testing"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	"github.com/tonedefdev/opendepot/pkg/agentspec"
)

func TestLoadAgentContentAcceptsBothAgentLayouts(t *testing.T) {
	for _, entry := range []string{"code-review.agent.md", "code-review.md"} {
		t.Run(entry, func(t *testing.T) {
			dir := t.TempDir()
			body := "---\nname: code-review\ndescription: Reviews code changes.\n---\nBody.\n"

			if err := os.WriteFile(filepath.Join(dir, entry), []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}

			name := "code-review"
			version := &opendepotv1alpha1.Version{
				Spec: opendepotv1alpha1.VersionSpec{
					Type:           opendepotv1alpha1.OpenDepotAgent,
					AgentSourceRef: &opendepotv1alpha1.AgentSourceConfig{Name: &name},
				},
			}

			content, err := loadAgentContent(dir, version)
			if err != nil {
				t.Fatalf("loadAgentContent() error = %v", err)
			}

			if content.entryName != entry {
				t.Fatalf("entryName = %q, want %q", content.entryName, entry)
			}
		})
	}
}

func TestLoadAgentContentAppliesPlatform(t *testing.T) {
	tests := []struct {
		name     string
		platform string
		wantWarn bool
	}{
		{name: "copilot accepts argument-hint", platform: "copilot", wantWarn: false},
		{name: "claude flags argument-hint", platform: "claude", wantWarn: true},
		{name: "unset platform accepts argument-hint", platform: "", wantWarn: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			body := "---\nname: code-review\ndescription: Reviews code changes.\nargument-hint: [file]\n---\nBody.\n"

			if err := os.WriteFile(filepath.Join(dir, "code-review.agent.md"), []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}

			name := "code-review"
			version := &opendepotv1alpha1.Version{
				Spec: opendepotv1alpha1.VersionSpec{
					Type:           opendepotv1alpha1.OpenDepotAgent,
					AgentSourceRef: &opendepotv1alpha1.AgentSourceConfig{Name: &name},
				},
			}

			if test.platform != "" {
				version.Spec.AgentSourceRef.Platform = &test.platform
			}

			content, err := loadAgentContent(dir, version)
			if err != nil {
				t.Fatalf("loadAgentContent() error = %v", err)
			}

			warned := false

			for _, finding := range content.definition.Findings {
				if finding.VulnerabilityID == agentspec.FindingUnknownKey {
					warned = true
				}
			}

			if warned != test.wantWarn {
				t.Errorf("unknown key warning = %v, want %v", warned, test.wantWarn)
			}
		})
	}
}

func TestLoadAgentContentRejectsUnsupportedPlatform(t *testing.T) {
	dir := t.TempDir()
	body := "---\nname: code-review\ndescription: Reviews code changes.\n---\nBody.\n"

	if err := os.WriteFile(filepath.Join(dir, "code-review.agent.md"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	name := "code-review"
	platform := "unknown-platform"
	version := &opendepotv1alpha1.Version{
		Spec: opendepotv1alpha1.VersionSpec{
			Type:           opendepotv1alpha1.OpenDepotAgent,
			AgentSourceRef: &opendepotv1alpha1.AgentSourceConfig{Name: &name, Platform: &platform},
		},
	}

	if _, err := loadAgentContent(dir, version); err == nil {
		t.Fatal("loadAgentContent() error = nil, want unsupported platform error")
	}
}
