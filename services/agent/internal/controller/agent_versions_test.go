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
	"slices"
	"testing"
)

func TestSanitizeAgentVersion(t *testing.T) {
	tests := map[string]string{
		"1.0.0":   "1-0-0",
		"v1.2.3":  "1-2-3",
		"1.0_rc1": "1-0-rc1",
	}

	for input, want := range tests {
		if got := sanitizeAgentVersion(input); got != want {
			t.Errorf("sanitizeAgentVersion(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestAgentVersionName(t *testing.T) {
	if got := agentVersionName(skillKind, "my-skill", "v1.0.0"); got != "skill-my-skill-1-0-0" {
		t.Errorf("skill version name = %q, want %q", got, "skill-my-skill-1-0-0")
	}

	if got := agentVersionName(agentKindDef, "my-agent", "2.1.0"); got != "agent-my-agent-2-1-0" {
		t.Errorf("agent version name = %q, want %q", got, "agent-my-agent-2-1-0")
	}
}

func TestLatestAgentVersion(t *testing.T) {
	if got := latestAgentVersion(nil); got != nil {
		t.Errorf("latestAgentVersion(nil) = %q, want nil", *got)
	}

	got := latestAgentVersion([]string{"1.10.0", "v1.9.0", "1.2.0"})
	if got == nil || *got != "v1.10.0" {
		t.Errorf("latestAgentVersion() = %v, want v1.10.0", got)
	}
}

func TestTrimAgentVersions(t *testing.T) {
	limit := func(n int) *int { return &n }

	tests := []struct {
		name     string
		versions []string
		limit    *int
		want     []string
	}{
		{name: "nil limit keeps all", versions: []string{"1.0.0", "2.0.0"}, limit: nil, want: []string{"1.0.0", "2.0.0"}},
		{name: "zero limit keeps all", versions: []string{"1.0.0", "2.0.0"}, limit: limit(0), want: []string{"1.0.0", "2.0.0"}},
		{name: "limit above count keeps all", versions: []string{"1.0.0"}, limit: limit(3), want: []string{"1.0.0"}},
		{name: "keeps newest versions", versions: []string{"1.10.0", "1.2.0", "1.9.0"}, limit: limit(2), want: []string{"1.9.0", "1.10.0"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := trimAgentVersions(tt.versions, tt.limit); !slices.Equal(got, tt.want) {
				t.Errorf("trimAgentVersions() = %v, want %v", got, tt.want)
			}
		})
	}
}
