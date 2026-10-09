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
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	"github.com/tonedefdev/opendepot/pkg/agentspec"
	"github.com/tonedefdev/opendepot/services/version/checks/agents"
	"github.com/tonedefdev/opendepot/services/version/internal/policy"
)

// scriptExtensions are the bundled file types whose content is evaluated by the Expr rules as scripts.
var scriptExtensions = map[string]struct{}{
	".sh":   {},
	".bash": {},
	".ps1":  {},
	".py":   {},
	".rb":   {},
	".js":   {},
	".ts":   {},
}

// agentContent is the parsed content of a Skill or Agent archive.
type agentContent struct {
	// entryName is the file that defines the Skill or Agent, such as SKILL.md.
	entryName string
	// entryBytes is the raw content of the entry file.
	entryBytes []byte
	// entryFlagged reports whether Trivy flagged the entry file for a secret.
	entryFlagged bool
	// parseErr is the agentspec validation error, which is non-nil only when a HIGH finding was raised.
	parseErr error
	// definition is the parsed frontmatter and body of the entry file.
	definition *agentspec.Result
	// scripts is the concatenated content of every bundled script file.
	scripts string
	// files lists every bundled file as a slash-separated path relative to the archive root.
	files []string
}

// agentEntryPath returns the path of the entry file for a Skill or Agent Version inside dir.
func agentEntryPath(dir string, version *opendepotv1alpha1.Version) (string, error) {
	if version.Spec.Type == opendepotv1alpha1.OpenDepotAgent && version.Spec.AgentSourceRef != nil && version.Spec.AgentSourceRef.Name != nil {
		return agentspec.FindAgentEntry(dir, *version.Spec.AgentSourceRef.Name)
	}

	return filepath.Join(dir, "SKILL.md"), nil
}

// skillName returns the registry name of a Skill Version, which is the directory a Skill installs into.
func skillName(version *opendepotv1alpha1.Version) string {
	if version.Spec.AgentSourceRef != nil && version.Spec.AgentSourceRef.Name != nil {
		return *version.Spec.AgentSourceRef.Name
	}

	return ""
}

// loadAgentContent reads the entry file, the bundled file list, and the bundled scripts from an
// extracted Skill or Agent directory.
func loadAgentContent(dir string, version *opendepotv1alpha1.Version) (*agentContent, error) {
	entryPath, err := agentEntryPath(dir, version)
	if err != nil {
		return nil, err
	}

	content := &agentContent{entryName: filepath.Base(entryPath)}
	entryBytes, err := os.ReadFile(entryPath)
	if err != nil {
		return nil, fmt.Errorf("read agent entry file %s: %w", content.entryName, err)
	}

	content.entryBytes = entryBytes
	var parsed *agentspec.Result
	var parseErr error
	platform := ""
	if version.Spec.AgentSourceRef != nil && version.Spec.AgentSourceRef.Platform != nil {
		platform = *version.Spec.AgentSourceRef.Platform
	}

	if version.Spec.Type == opendepotv1alpha1.OpenDepotAgent {
		parsed, parseErr = agentspec.ParseAgentFor(entryPath, entryBytes, platform)
	} else {
		parsed, parseErr = agentspec.ParseSkillNamedFor(entryPath, entryBytes, skillName(version), platform)
	}

	if parsed == nil || (parseErr != nil && len(parsed.Findings) == 0) {
		return nil, fmt.Errorf("parse agent entry file %s: %w", content.entryName, parseErr)
	}

	content.definition = parsed
	content.parseErr = parseErr

	var scripts strings.Builder
	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if d.IsDir() {
			return nil
		}

		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}

		rel = filepath.ToSlash(rel)
		content.files = append(content.files, rel)

		if _, ok := scriptExtensions[strings.ToLower(filepath.Ext(rel))]; !ok {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		scripts.WriteString(string(data))
		scripts.WriteString("\n")

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list agent files: %w", err)
	}

	sort.Strings(content.files)
	content.scripts = scripts.String()
	return content, nil
}

// runAgentScan extracts a stored Skill or Agent archive and evaluates it with the agentspec parser,
// the Expr rules, and a Trivy fs scan with the vuln, secret, and misconfig scanners. Trivy is always
// on for agents and fails closed: a scanner error is returned rather than an empty result. A non-nil
// blocking finding means the Version must not be published.
func (r *VersionReconciler) runAgentScan(
	ctx context.Context,
	version *opendepotv1alpha1.Version,
	archiveBytes []byte,
	cacheDir string,
	offline bool,
	resolved policy.Resolved,
) (*opendepotv1alpha1.SourceScan, *agentContent, *opendepotv1alpha1.SecurityFinding, error) {
	tmpDir, cleanup, err := extractArchiveToTempDir(archiveBytes, "opendepot-agentscan-*")
	if err != nil {
		return nil, nil, nil, err
	}
	defer cleanup()

	content, err := loadAgentContent(tmpDir, version)
	if err != nil {
		return nil, nil, nil, err
	}

	args := []string{"fs", "--format", "json", "--scanners", "vuln,secret,misconfig"}
	if offline {
		args = append(args, "--offline-scan", "--skip-db-update")
	}

	if cacheDir != "" {
		args = append(args, "--cache-dir", cacheDir)
	}

	args = append(args, "--quiet", tmpDir)

	// Serialise Trivy invocations: each process loads the full ~2 GiB DB.
	select {
	case r.scanSem <- struct{}{}:
	case <-ctx.Done():
		return nil, nil, nil, ctx.Err()
	}

	output, err := runTrivy(ctx, args...)
	<-r.scanSem
	if err != nil {
		return nil, nil, nil, fmt.Errorf("agent scan failed: %w", err)
	}

	if len(output) == 0 {
		return nil, nil, nil, fmt.Errorf("agent scan failed: trivy produced no output; verify the Trivy vulnerability DB is populated in the offline cache")
	}

	findings, err := parseTrivyReport(output, nil)
	if err != nil {
		return nil, nil, nil, err
	}

	secrets, flagged, err := parseTrivySecrets(output)
	if err != nil {
		return nil, nil, nil, err
	}

	for target := range flagged {
		if filepath.Base(target) == content.entryName {
			content.entryFlagged = true
		}
	}

	findings = append(findings, secrets...)
	findings = append(findings, content.definition.Findings...)

	ruleSet, err := agents.Default()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("agent rules unavailable: %w", err)
	}

	def := content.definition
	body := def.Body + "\n" + content.scripts
	exprFindings, err := ruleSet.Evaluate(agents.AgentRuleEnv{
		Name:           def.Name,
		Description:    def.Description,
		Tools:          def.Tools,
		Body:           def.Body,
		Scripts:        content.scripts,
		Domains:        agents.ExtractDomains(body),
		AllowedDomains: r.AgentAllowedDomains,
	})
	if err != nil {
		return nil, nil, nil, err
	}

	findings = append(findings, exprFindings...)
	annotated, blocking := policy.Apply(resolved, findings, policy.ScanTypeAgent)
	if blocking == nil && content.parseErr != nil {
		blocking = highestAgentSpecFinding(content.definition.Findings)
	}

	sourceScan := &opendepotv1alpha1.SourceScan{
		ScannedAt: time.Now().UTC().Format(time.RFC3339),
		Findings:  annotated,
	}

	return sourceScan, content, blocking, nil
}

// highestAgentSpecFinding returns the first HIGH finding raised by the agentspec parser. The parser
// always blocks on these, independent of the ScanPolicy thresholds.
func highestAgentSpecFinding(findings []opendepotv1alpha1.SecurityFinding) *opendepotv1alpha1.SecurityFinding {
	for i := range findings {
		if strings.EqualFold(findings[i].Severity, "HIGH") {
			return &findings[i]
		}
	}

	return nil
}
