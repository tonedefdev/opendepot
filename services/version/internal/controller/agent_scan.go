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
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
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

const (
	// scriptWindowBytes is the size of each script window handed to the Expr rules.
	scriptWindowBytes = 1 << 20
	// scriptCarryBytes is the tail of each script window repeated at the start of the next, so a match that
	// straddles a window boundary is still seen whole.
	scriptCarryBytes = 64 << 10
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

// agentContent is the parsed content of a Skill or Agent archive. The extracted directory stays on disk until
// release is called, so the Jev assessment can read the entry file and the bundled files again.
type agentContent struct {
	// entryName is the file that defines the Skill or Agent, such as SKILL.md.
	entryName string
	// entryRel is the slash-separated path of the entry file relative to dir.
	entryRel string
	// dir is the extracted archive directory.
	dir string
	// entryFlagged reports whether Trivy flagged the entry file for a secret.
	entryFlagged bool
	// flaggedFiles holds the base names of every file Trivy flagged for a secret.
	flaggedFiles map[string]struct{}
	// parseErr is the agentspec validation error, which is non-nil only when a HIGH finding was raised.
	parseErr error
	// definition is the parsed frontmatter and body of the entry file.
	definition *agentspec.Result
	// files lists every bundled file as a slash-separated path relative to the archive root.
	files []string
	// release removes the extracted directory. It is set only when runAgentScan succeeds.
	release func()
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

// loadAgentContent parses the entry file and lists the bundled files of an extracted Skill or Agent directory.
func loadAgentContent(dir string, version *opendepotv1alpha1.Version) (*agentContent, error) {
	entryPath, err := agentEntryPath(dir, version)
	if err != nil {
		return nil, err
	}

	content := &agentContent{
		entryName: filepath.Base(entryPath),
		dir:       dir,
	}

	entryFile, err := os.Open(entryPath)
	if err != nil {
		return nil, fmt.Errorf("read agent entry file %s: %w", content.entryName, err)
	}
	defer entryFile.Close()

	var parsed *agentspec.Result
	var parseErr error
	platform := ""
	if version.Spec.AgentSourceRef != nil && version.Spec.AgentSourceRef.Platform != nil {
		platform = *version.Spec.AgentSourceRef.Platform
	}

	if version.Spec.Type == opendepotv1alpha1.OpenDepotAgent {
		parsed, parseErr = agentspec.ParseAgentForReader(entryPath, entryFile, platform)
	} else {
		parsed, parseErr = agentspec.ParseSkillNamedForReader(entryPath, entryFile, skillName(version), platform)
	}

	if parsed == nil || (parseErr != nil && len(parsed.Findings) == 0) {
		return nil, fmt.Errorf("parse agent entry file %s: %w", content.entryName, parseErr)
	}

	content.definition = parsed
	content.parseErr = parseErr

	entryRel, err := filepath.Rel(dir, entryPath)
	if err != nil {
		return nil, fmt.Errorf("resolve agent entry file: %w", err)
	}

	content.entryRel = filepath.ToSlash(entryRel)
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

		content.files = append(content.files, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list agent files: %w", err)
	}

	sort.Strings(content.files)
	return content, nil
}

// evaluateScriptStream streams the bundled script files through the Expr rules in fixed-size windows. Each
// window is evaluated with the carry of the previous one in front, and the files are joined with a newline so
// a rule never matches across two files by accident. evaluate is called once per window; when there are no
// script bytes it is called once with no segments, which runs the body-only rules.
func evaluateScriptStream(dir string, scriptFiles []string, evaluate func(segments []agents.ScriptSegment) error) error {
	readers := make([]io.Reader, 0, 2*len(scriptFiles))
	closers := make([]io.Closer, 0, len(scriptFiles))
	defer func() {
		for _, c := range closers {
			_ = c.Close()
		}
	}()

	for _, rel := range scriptFiles {
		f, err := os.Open(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			return err
		}

		closers = append(closers, f)
		readers = append(readers, f, strings.NewReader("\n"))
	}

	stream := bufio.NewReaderSize(io.MultiReader(readers...), scriptWindowBytes)
	chunk := make([]byte, scriptWindowBytes)
	var carry []byte
	windows := 0
	for {
		n, readErr := io.ReadFull(stream, chunk)
		final := readErr == io.EOF || errors.Is(readErr, io.ErrUnexpectedEOF)
		if readErr != nil && !final {
			return readErr
		}

		window := make([]byte, 0, len(carry)+n)
		window = append(window, carry...)
		window = append(window, chunk[:n]...)
		if len(window) > 0 {
			windows++
			if err := evaluate([]agents.ScriptSegment{{Text: string(window), CutMidLine: !final}}); err != nil {
				return err
			}
		}

		if final {
			break
		}

		carry = window[max(0, len(window)-scriptCarryBytes):]
	}

	if windows == 0 {
		return evaluate(nil)
	}

	return nil
}

// runAgentScan extracts a stored Skill or Agent archive and evaluates it with the agentspec parser,
// the Expr rules, and a Trivy fs scan with the vuln, secret, and misconfig scanners. Trivy is always
// on for agents and fails closed: a scanner error is returned rather than an empty result. A non-nil
// blocking finding means the Version must not be published. On success the extracted directory is kept
// until the returned content's release is called; on error it is removed before returning.
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

	keep := false
	defer func() {
		if !keep {
			cleanup()
		}
	}()

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

	content.flaggedFiles = make(map[string]struct{}, len(flagged))
	for target := range flagged {
		content.flaggedFiles[filepath.Base(target)] = struct{}{}
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
	var scriptFiles []string
	for _, rel := range content.files {
		if _, ok := scriptExtensions[strings.ToLower(filepath.Ext(rel))]; ok {
			scriptFiles = append(scriptFiles, rel)
		}
	}

	env := agents.AgentRuleEnv{
		Name:           def.Name,
		Description:    def.Description,
		Tools:          def.Tools,
		Body:           def.Body,
		AllowedDomains: r.AgentAllowedDomains,
	}

	var exprFindings []opendepotv1alpha1.SecurityFinding
	seen := make(map[string]struct{})
	err = evaluateScriptStream(tmpDir, scriptFiles, func(segments []agents.ScriptSegment) error {
		found, err := ruleSet.EvaluateSegments(env, segments)
		if err != nil {
			return err
		}

		for _, finding := range found {
			if _, ok := seen[finding.VulnerabilityID]; ok {
				continue
			}

			seen[finding.VulnerabilityID] = struct{}{}
			exprFindings = append(exprFindings, finding)
		}

		return nil
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

	content.release = cleanup
	keep = true
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
