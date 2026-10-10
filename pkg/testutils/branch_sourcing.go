/*
Copyright 2026 Anthony Owens.

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

package testutils

import (
	"fmt"
	"net/http"
	"os"
	"time"
)

const (
	// BranchFixtureRepo is the public repository used by branch-sourcing e2e suites.
	BranchFixtureRepo = "github/awesome-copilot"

	// BranchFixtureRef is the branch whose head is followed by branch-sourcing e2e suites.
	BranchFixtureRef = "main"

	// SkillFixturePath is a single skill directory containing SKILL.md at its root.
	SkillFixturePath = "skills/acquire-codebase-knowledge"

	// JevSkillFixturePath is a second skill directory at SkillFixtureSHA. Its SKILL.md name matches the directory,
	// so a Skill named after it passes the name check.
	JevSkillFixturePath = "skills/conventional-commit"

	// SkillFixtureSHA pins the skill fixture to a commit where SKILL.md exists and passes checks.
	SkillFixtureSHA = "b8f38227480c9f3fe04d6496d4fcab9a880e5a15"

	// AgentFixturePath is the directory that contains the agent fixture entry file.
	AgentFixturePath = "agents"

	// AgentFixtureName is the agent name that agentspec.FindAgentEntry resolves in AgentFixturePath.
	AgentFixtureName = "oracle-to-postgres-migration-expert"

	// AgentFixtureSHA pins the agent fixture to a commit where the entry file exists and passes agentspec.
	AgentFixtureSHA = "41892f2b635130901532c576b90bdc09c8aad49c"

	// JevAPIKeyEnvVar names the environment variable that holds the Jev API key for live classification.
	JevAPIKeyEnvVar = "OPENDEPOT_JEV_API_KEY"
)

// RequireFixtureAtMain returns an error when path no longer exists on BranchFixtureRef.
// Callers should fail the spec with this error, not skip it, so an upstream removal is visible.
func RequireFixtureAtMain(path string) error {
	url := fmt.Sprintf(
		"https://github.com/%s/tree/%s/%s",
		BranchFixtureRepo, BranchFixtureRef, path,
	)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("check fixture %q at %s: %w", path, BranchFixtureRef, err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	switch resp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusNotFound:
		return fmt.Errorf("fixture removed upstream: %q not found at %s", path, BranchFixtureRef)
	default:
		return fmt.Errorf("check fixture %q at %s: unexpected HTTP %d", path, BranchFixtureRef, resp.StatusCode)
	}
}

// RequireJevAPIKey returns the Jev API key or an error when it is unset.
// Jev specs must fail, not skip, when the key is missing.
func RequireJevAPIKey() (string, error) {
	key := os.Getenv(JevAPIKeyEnvVar)
	if key == "" {
		return "", fmt.Errorf("%s must be set to run Jev e2e specs", JevAPIKeyEnvVar)
	}

	return key, nil
}
