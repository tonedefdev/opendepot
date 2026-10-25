package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tonedefdev/opendepot/pkg/agentspec"
	"github.com/tonedefdev/opendepot/pkg/archive"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
)

type applyOptions struct {
	upgrade     bool
	allowYanked bool
	trustNewKey bool
	noPrompt    bool
	force       bool
	dryRun      bool
}

// stagedEntry is a fully verified entry whose archive is extracted to a temporary root.
// Nothing under the project is written until every entry in the project has been staged.
type stagedEntry struct {
	entry
	pkgName    string
	version    string
	latest     string
	pinnedKey  string
	signature  string
	zh         string
	h1         string
	root       string
	targets    []installTarget
	assessment *opendepotv1alpha1.JevAssessment
}

func (s *stagedEntry) bodyPath() string {
	if s.kind == kindSkill {
		return filepath.Join(s.root, "SKILL.md")
	}

	if path, err := agentspec.FindAgentEntry(s.root, s.pkgName); err == nil {
		return path
	}

	return filepath.Join(s.root, s.pkgName+".md")
}

func (a *app) path(rel string) string {
	if filepath.IsAbs(rel) {
		return rel
	}

	return filepath.Join(a.dir, rel)
}

// ensureNoSymlinks refuses rel when an existing component below the project directory (or below the
// home directory for user-level targets) is a symlink, so reads, writes, and removals cannot be
// redirected outside the install tree.
func (a *app) ensureNoSymlinks(rel string) error {
	base := a.dir
	if filepath.IsAbs(rel) {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}

		inHome, err := filepath.Rel(home, filepath.Clean(rel))
		if err != nil || !filepath.IsLocal(inHome) {
			return fmt.Errorf("path %s is outside the home directory", rel)
		}

		base = home
		rel = inHome
	}

	cur := base
	for _, part := range strings.Split(filepath.ToSlash(filepath.Clean(rel)), "/") {
		cur = filepath.Join(cur, part)
		info, err := os.Lstat(cur)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}

		if err != nil {
			return err
		}

		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to follow symlink %s", cur)
		}
	}

	return nil
}

func (a *app) confirm(question string) bool {
	fmt.Fprintf(a.out, "%s [y/N]: ", question)
	line, _ := a.in.ReadString('\n')
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes"
}

// safeInstallPath reports whether a lock-recorded or resolved install path is safe to write or
// remove. Relative paths must stay inside the project. Absolute paths are only accepted for the
// user-level layouts <home>/.copilot|.claude/{skills,agents}/<name>, <home>/.agents/skills/<name>, and
// <home>/.codex/agents/<name>.toml, so a lock file cannot point removals at arbitrary locations.
func safeInstallPath(p string) bool {
	clean := filepath.Clean(p)
	if !filepath.IsAbs(clean) {
		return filepath.IsLocal(clean) && clean != "." && clean != agentsMDFile
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}

	rel, err := filepath.Rel(home, clean)
	if err != nil || !filepath.IsLocal(rel) {
		return false
	}

	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) != 3 {
		return false
	}

	switch parts[0] {
	case copilotHomeDir, claudeHomeDir:
		return parts[1] == "skills" || parts[1] == "agents"
	case agentsHomeDir:
		return parts[1] == "skills"
	case codexHomeDir:
		return parts[1] == "agents" && strings.HasSuffix(parts[2], ".toml")
	}

	return false
}

// filterAgentTargets keeps the targets that accept an agent entry's format.
func filterAgentTargets(targets []installTarget, toml bool) []installTarget {
	var kept []installTarget
	for _, t := range targets {
		if agentTargetAccepts(t.label, toml) {
			kept = append(kept, t)
		}
	}

	return kept
}

// agentTargetAccepts reports whether an agent entry of the given format can install to the target. A TOML entry
// is a Codex agent, so it installs only to codex. A Markdown entry never installs to codex, and agents-md would
// write the raw Markdown body into AGENTS.md. A custom path copies the entry file verbatim and accepts any format.
func agentTargetAccepts(label string, toml bool) bool {
	switch label {
	case targetCustom:
		return true
	case targetCodex:
		return toml
	default:
		return !toml
	}
}

func readOptional(path string) ([]byte, error) {
	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}

	return content, err
}

// sourceError names the source entry that failed. Its detail goes on the lines below, indented.
type sourceError struct {
	source string
	err    error
}

func (e *sourceError) Error() string {
	return e.source + "\n  " + strings.ReplaceAll(e.err.Error(), "\n", "\n  ")
}

func (e *sourceError) Unwrap() error {
	return e.err
}

func chooseVersion(e entry, versions []agentVersion, prev *lockEntry, opts applyOptions) (string, error) {
	if prev != nil && !opts.upgrade && prev.Constraints == e.constraint {
		for _, v := range versions {
			if v.Version != prev.Version {
				continue
			}

			if v.Blocked {
				return "", &blockedVersionError{version: v.Version, reasons: v.BlockReasons, locked: true}
			}

			if v.Yanked && !opts.allowYanked {
				return "", fmt.Errorf("locked version %s is yanked; re-run with -allow-yanked or -upgrade", v.Version)
			}

			return prev.Version, nil
		}
	}

	return resolveVersion(versions, e.constraint, opts.allowYanked)
}

// stage runs the install verification chain for one entry. It fails before any write on mismatch.
func (a *app) stage(ctx context.Context, regs *registries, tmp string, e entry, targets []installTarget, prev *lockEntry, opts applyOptions) (*stagedEntry, error) {
	src, err := parseSource(e.source)
	if err != nil {
		return nil, err
	}

	reg, err := regs.get(ctx, src.host)
	if err != nil {
		return nil, err
	}

	versions, err := reg.versions(ctx, src, e.kind)
	if err != nil {
		return nil, err
	}

	chosen, err := chooseVersion(e, versions, prev, opts)
	if err != nil {
		return nil, err
	}

	// latest ignores the lock file so the plan can point out a newer remote version.
	latest := ""
	if v, err := resolveVersion(versions, e.constraint, opts.allowYanked); err == nil {
		latest = v
	}

	meta, err := reg.download(ctx, src, e.kind, chosen)
	if err != nil {
		return nil, err
	}

	if meta.Yanked && !opts.allowYanked {
		return nil, fmt.Errorf("version %s is yanked; re-run with -allow-yanked to install it", chosen)
	}

	if meta.Assessment != nil && meta.Assessment.Blocked {
		return nil, &blockedVersionError{version: chosen, reasons: meta.Assessment.BlockReasons}
	}

	if err := checkProtocols(meta.Protocols); err != nil {
		return nil, err
	}

	if meta.Kind != e.kind.singular() || meta.Name != src.name || meta.Version != chosen {
		return nil, fmt.Errorf("registry metadata does not describe %s@%s", src.name, chosen)
	}

	if meta.SigningKeys == nil {
		return nil, fmt.Errorf("registry advertised no signing keys")
	}

	sumsURL, err := reg.resolve(meta.ShasumsURL)
	if err != nil {
		return nil, err
	}

	sums, err := a.get(ctx, sumsURL, src.host)
	if err != nil {
		return nil, fmt.Errorf("fetch SHA256SUMS: %w", err)
	}

	sigURL, err := reg.resolve(meta.ShasumsSignatureURL)
	if err != nil {
		return nil, err
	}

	sig, err := a.get(ctx, sigURL, src.host)
	if err != nil {
		return nil, fmt.Errorf("fetch SHA256SUMS.sig: %w", err)
	}

	armored := make([]string, 0, len(meta.SigningKeys.GPGPublicKeys))
	for _, k := range meta.SigningKeys.GPGPublicKeys {
		armored = append(armored, k.ASCIIArmor)
	}

	signer, err := verifySHA256SUMS(sums, sig, armored)
	if err != nil {
		return nil, err
	}

	sigHash, err := signatureHash(sig)
	if err != nil {
		return nil, err
	}

	prevPin := ""
	if prev != nil {
		prevPin = prev.SigningKey
	}

	pinned, err := decideSigningKey(e.signingKey, prevPin, signer, opts.trustNewKey, opts.noPrompt, a.confirm)
	if err != nil {
		return nil, err
	}

	signedZH, err := findSHA256SUM(sums, meta.Filename)
	if err != nil {
		return nil, err
	}

	if signedZH != strings.ToLower(meta.Shasum) {
		return nil, fmt.Errorf("download metadata shasum does not match SHA256SUMS")
	}

	archiveURL, err := reg.resolve(meta.DownloadURL)
	if err != nil {
		return nil, err
	}

	data, err := a.fetch(ctx, archiveURL, src.host, maxArchiveBytes)
	if err != nil {
		return nil, fmt.Errorf("download archive: %w", err)
	}

	zh := archive.SHA256Hex(data)
	if zh != signedZH {
		return nil, fmt.Errorf("archive SHA-256 %s does not match signed SHA256SUMS", zh)
	}

	sameVersion := prev != nil && prev.Version == chosen
	if sameVersion && prev.hash("zh:") != "zh:"+zh {
		return nil, fmt.Errorf("archive does not match the zh: hash recorded in the lock file")
	}

	if sameVersion && pinned == prev.SigningKey && prev.Signature != sigHash {
		return nil, fmt.Errorf("SHA256SUMS signature does not match the signature_hash recorded in the lock file")
	}

	root, err := os.MkdirTemp(tmp, "entry-")
	if err != nil {
		return nil, err
	}

	if err := archive.ExtractToDir(data, root); err != nil {
		return nil, fmt.Errorf("extract archive: %w", err)
	}

	h1, err := dirHashTree(root)
	if err != nil {
		return nil, err
	}

	if sameVersion && prev.hash("h1:") != h1 {
		return nil, fmt.Errorf("extracted tree does not match the h1: hash recorded in the lock file")
	}

	staged := &stagedEntry{
		entry:     e,
		pkgName:   src.name,
		version:   chosen,
		latest:    latest,
		pinnedKey: pinned,
		signature: sigHash,
		zh:        "zh:" + zh,
		h1:        h1,
		root:      root,
		targets:   targets,
	}

	if staged.kind == kindAgent {
		entryPath, err := agentspec.FindAgentEntry(root, src.name)
		if err != nil {
			return nil, err
		}

		staged.targets = filterAgentTargets(targets, filepath.Ext(entryPath) == ".toml")
		if len(staged.targets) == 0 {
			return nil, fmt.Errorf("no install target accepts this agent: a TOML agent installs only to codex, and a Markdown agent installs to copilot, claude, or agents-md; add a matching target or a path on the entry")
		}
	}

	if _, err := os.Stat(staged.bodyPath()); err != nil {
		return nil, fmt.Errorf("archive has no %s", filepath.Base(staged.bodyPath()))
	}

	staged.assessment = meta.Assessment
	return staged, nil
}

// installEntry writes one staged entry to a non-AGENTS.md target.
func (a *app) installEntry(s *stagedEntry, t installTarget) error {
	if err := a.ensureNoSymlinks(t.path); err != nil {
		return err
	}

	dst := a.path(t.path)
	if s.kind == kindSkill {
		hash, err := dirHashTree(dst)
		if err == nil && hash == s.h1 {
			return nil
		}

		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}

		if err := os.RemoveAll(dst); err != nil {
			return err
		}

		return copyTree(s.root, dst)
	}

	want, err := hashFile(s.bodyPath())
	if err != nil {
		return err
	}

	hash, err := hashFile(dst)
	if err == nil && hash == want {
		return nil
	}

	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	return copyFile(s.bodyPath(), dst, 0o644)
}

// installedHash returns the hash recorded for an installed target.
func (a *app) installedHash(kind entryKind, rel string) (string, error) {
	if kind == kindSkill {
		return dirHashTree(a.path(rel))
	}

	return hashFile(a.path(rel))
}

func (a *app) applyProject(ctx context.Context, opts applyOptions) error {
	if err := a.ensureNoSymlinks(configFileName); err != nil {
		return err
	}

	proj, err := loadConfig(a.path(configFileName))
	if err != nil {
		return err
	}

	if err := a.ensureNoSymlinks(lockFileName); err != nil {
		return err
	}

	prev, err := loadLock(a.path(lockFileName))
	if err != nil {
		return err
	}

	tmp, err := os.MkdirTemp("", "opendepot-apply-")
	if err != nil {
		return err
	}

	defer os.RemoveAll(tmp)

	regs := a.newRegistries()
	var staged []*stagedEntry
	for _, e := range proj.entries {
		targets, err := proj.installTargets(e)
		if err != nil {
			return err
		}

		var prevEntry *lockEntry
		if le, ok := prev.find(e.source); ok {
			prevEntry = &le
		}

		s, err := a.stage(ctx, regs, tmp, e, targets, prevEntry, opts)
		if err != nil {
			return &sourceError{source: e.source, err: err}
		}

		if s.assessment != nil {
			a.printAssessment(s)
		}

		staged = append(staged, s)
	}

	// Plan every AGENTS.md change and every stale removal before the first write.
	desired := map[string]string{}
	recorded := map[string]string{}
	newPaths := map[string]bool{}
	for _, s := range staged {
		for _, t := range s.targets {
			if t.label == targetAgentsMD {
				body, err := os.ReadFile(s.bodyPath())
				if err != nil {
					return err
				}

				desired[s.source] = canonicalInner(string(body))
				continue
			}

			newPaths[filepath.Clean(t.path)] = true
		}
	}

	var stale []string
	for _, group := range []struct {
		kind    entryKind
		entries []lockEntry
	}{{kindSkill, prev.Skills}, {kindAgent, prev.Agents}} {
		for _, e := range group.entries {
			for _, in := range e.Installed {
				if in.Target == targetAgentsMD {
					recorded[e.Source] = in.Hash
					continue
				}

				if !safeInstallPath(in.Path) {
					return fmt.Errorf("lock file records unsafe path %q", in.Path)
				}

				if !newPaths[filepath.Clean(in.Path)] {
					stale = append(stale, filepath.Clean(in.Path))
				}
			}
		}
	}

	if err := a.ensureNoSymlinks(agentsMDFile); err != nil {
		return err
	}

	existing, err := readOptional(a.path(agentsMDFile))
	if err != nil {
		return err
	}

	agentsContent, err := planAgentsMD(string(existing), desired, recorded, opts.force)
	if err != nil {
		return err
	}

	for _, s := range staged {
		for _, t := range s.targets {
			if t.label == targetAgentsMD {
				continue
			}

			if !safeInstallPath(t.path) {
				return fmt.Errorf("%s: unsafe install path %q", s.source, t.path)
			}
		}
	}

	if opts.dryRun {
		return a.printPlan(staged, prev, existing, agentsContent, stale)
	}

	for _, s := range staged {
		for _, t := range s.targets {
			if t.label == targetAgentsMD {
				continue
			}

			if s.kind == kindSkill {
				changes, err := a.skillChanges(s, t)
				if err != nil {
					return fmt.Errorf("%s: install to %s: %w", s.source, t.path, err)
				}

				if hasFileChanges(changes) {
					fmt.Fprintf(a.out, "  %s  %s\n", s.source, t.path)
					a.printSkillTree(changes)
				}
			}

			if err := a.installEntry(s, t); err != nil {
				return fmt.Errorf("%s: install to %s: %w", s.source, t.path, err)
			}
		}
	}

	for _, p := range stale {
		if err := a.ensureNoSymlinks(p); err != nil {
			return err
		}

		if err := os.RemoveAll(a.path(p)); err != nil {
			return fmt.Errorf("remove stale %s: %w", p, err)
		}
	}

	if agentsContent != string(existing) {
		if err := a.ensureNoSymlinks(agentsMDFile); err != nil {
			return err
		}

		if err := os.WriteFile(a.path(agentsMDFile), []byte(agentsContent), 0o644); err != nil {
			return err
		}
	}

	agentsBlocks, err := parseAgentsBlocks(agentsContent)
	if err != nil {
		return err
	}

	lock := &lockFile{}
	for _, s := range staged {
		le := lockEntry{
			Source:      s.source,
			Version:     s.version,
			Constraints: s.constraint,
			SigningKey:  s.pinnedKey,
			Signature:   s.signature,
			Hashes:      []string{s.h1, s.zh},
		}

		sort.Strings(le.Hashes)
		for _, t := range s.targets {
			var hash string
			if t.label == targetAgentsMD {
				hash, err = blockHash(agentsBlocks, s.source)
			} else {
				hash, err = a.installedHash(s.kind, t.path)
			}

			if err != nil {
				return err
			}

			le.Installed = append(le.Installed, lockInstalled{Target: t.label, Path: t.path, Hash: hash})
		}

		if s.kind == kindSkill {
			lock.Skills = append(lock.Skills, le)
		} else {
			lock.Agents = append(lock.Agents, le)
		}

		labels := make([]string, 0, len(s.targets))
		for _, t := range s.targets {
			labels = append(labels, t.label)
		}

		st := newStyler(a.out)
		fmt.Fprintf(a.out, "%s Installed %s@%s → %s\n", st.paint(ansiGreen, "✓"), s.source, s.version, strings.Join(labels, ", "))
	}

	rendered := renderLock(lock)
	existingLock, err := readOptional(a.path(lockFileName))
	if err != nil {
		return err
	}

	if string(existingLock) == string(rendered) {
		return nil
	}

	if err := a.ensureNoSymlinks(lockFileName); err != nil {
		return err
	}

	return os.WriteFile(a.path(lockFileName), rendered, 0o644)
}

func blockHash(blocks []agentsBlock, source string) (string, error) {
	for _, b := range blocks {
		if b.source == source {
			return hashBlock(b.inner)
		}
	}

	return "", fmt.Errorf("AGENTS.md has no block for %s", source)
}

// verifyProject checks installed targets against the lock file without touching the network.
func (a *app) verifyProject() error {
	if err := a.ensureNoSymlinks(lockFileName); err != nil {
		return err
	}

	lock, err := loadLock(a.path(lockFileName))
	if err != nil {
		return err
	}

	if _, err := os.Stat(a.path(lockFileName)); err != nil {
		return fmt.Errorf("read %s: %w", lockFileName, err)
	}

	if err := a.ensureNoSymlinks(agentsMDFile); err != nil {
		return err
	}

	agentsContent, err := readOptional(a.path(agentsMDFile))
	if err != nil {
		return err
	}

	blocks, err := parseAgentsBlocks(string(agentsContent))
	if err != nil {
		return err
	}

	var failures []string
	checked := 0
	check := func(kind entryKind, e lockEntry) {
		for _, in := range e.Installed {
			checked++
			if in.Target == targetAgentsMD {
				hash, err := blockHash(blocks, e.Source)
				switch {
				case err != nil:
					failures = append(failures, fmt.Sprintf("%s: AGENTS.md block is missing", e.Source))
				case hash != in.Hash:
					failures = append(failures, fmt.Sprintf("%s: AGENTS.md block was edited by hand", e.Source))
				}

				continue
			}

			if !safeInstallPath(in.Path) {
				failures = append(failures, fmt.Sprintf("%s: unsafe path %q in lock file", e.Source, in.Path))
				continue
			}

			hash, err := a.installedHash(kind, in.Path)
			switch {
			case err != nil:
				failures = append(failures, fmt.Sprintf("%s: %s is missing or unreadable", e.Source, in.Path))
			case hash != in.Hash:
				failures = append(failures, fmt.Sprintf("%s: %s has been modified", e.Source, in.Path))
			}
		}
	}

	for _, e := range lock.Skills {
		check(kindSkill, e)
	}

	for _, e := range lock.Agents {
		check(kindAgent, e)
	}

	if len(failures) > 0 {
		return fmt.Errorf("verification failed:\n  %s", strings.Join(failures, "\n  "))
	}

	fmt.Fprintf(a.out, "verified %d installed target(s) against %s\n", checked, lockFileName)
	return nil
}

// validatePath runs the local agentspec checks on a SKILL.md, a directory containing one, or an agent file.
func validatePath(out io.Writer, path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}

	target := path
	if info.IsDir() {
		target = filepath.Join(path, "SKILL.md")
	}

	content, err := os.ReadFile(target)
	if err != nil {
		return err
	}

	parse := agentspec.ParseAgent
	if strings.EqualFold(filepath.Base(target), "SKILL.md") {
		parse = agentspec.Parse
	}

	res, err := parse(target, content)
	for _, f := range res.Findings {
		fmt.Fprintf(out, "%s: %s: %s\n", f.Severity, target, f.Title)
	}

	if err != nil {
		return err
	}

	fmt.Fprintf(out, "%s is valid\n", target)
	return nil
}
