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
	return filepath.Join(a.dir, rel)
}

// ensureNoSymlinks refuses rel when an existing component below the project directory is a symlink,
// so reads, writes, and removals cannot be redirected outside the project tree.
func (a *app) ensureNoSymlinks(rel string) error {
	cur := a.dir
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

// safeRelPath reports whether a lock-recorded path is a relative path inside the project that
// is safe to write or remove.
func safeRelPath(p string) bool {
	clean := filepath.Clean(p)
	return filepath.IsLocal(clean) && clean != "." && clean != agentsMDFile
}

func readOptional(path string) ([]byte, error) {
	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}

	return content, err
}

func chooseVersion(e entry, versions []agentVersion, prev *lockEntry, opts applyOptions) (string, error) {
	if prev != nil && !opts.upgrade && prev.Constraints == e.constraint {
		for _, v := range versions {
			if v.Version != prev.Version {
				continue
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
		return nil, fmt.Errorf("version %s is blocked by a Jev policy: %s", chosen, strings.Join(meta.Assessment.BlockReasons, "; "))
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
		if _, err := agentspec.FindAgentEntry(root, src.name); err != nil {
			return nil, err
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
			return fmt.Errorf("%s: %w", e.source, err)
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

				if !safeRelPath(in.Path) {
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

			if !safeRelPath(t.path) {
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

			if !safeRelPath(in.Path) {
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
