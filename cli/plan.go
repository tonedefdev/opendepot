package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"
)

const (
	planAdd       = "+"
	planChange    = "~"
	planOverwrite = "!"
	planSame      = "="
	planRemove    = "-"
)

type planRow struct {
	sym   string
	label string
	path  string
	note  string
}

// printPlan reports what apply would change. It runs after verification and never writes.
func (a *app) printPlan(staged []*stagedEntry, prev *lockFile, existing []byte, agentsContent string, stale []string) error {
	st := newStyler(a.out)

	existingBlocks, err := parseAgentsBlocks(string(existing))
	if err != nil {
		return err
	}

	plannedBlocks, err := parseAgentsBlocks(agentsContent)
	if err != nil {
		return err
	}

	counts := map[string]int{}
	sources := map[string]bool{}
	fmt.Fprintln(a.out)
	fmt.Fprintln(a.out, st.paint(ansiBold, "Plan for opendepot.hcl"))
	for _, s := range staged {
		sources[s.source] = true
		le, locked := prev.find(s.source)

		var rows []planRow
		for _, t := range s.targets {
			row := planRow{label: t.label, path: t.path}
			if t.label == targetAgentsMD {
				row.sym, err = agentsBlockSymbol(s.source, existingBlocks, plannedBlocks)
			} else {
				row.sym, err = a.planTarget(s, t, lockedInstalledHash(le, locked, t.label))
			}

			if err != nil {
				return fmt.Errorf("%s: %w", s.source, err)
			}

			rows = append(rows, row)
		}

		headerSym := planSame
		detail := s.version
		hasChange := false
		for _, r := range rows {
			if r.sym != planSame {
				hasChange = true
			}
		}

		switch {
		case !locked:
			headerSym = planAdd
			detail = "new " + s.version
		case le.Version != s.version:
			headerSym = planChange
			detail = le.Version + " → " + s.version
		case hasChange:
			headerSym = planChange
		}

		fmt.Fprintf(a.out, "  %s %s  %s\n", st.paint(planColor(headerSym), headerSym), st.paint(ansiBold, s.source), detail)
		for _, r := range rows {
			counts[r.sym]++
			fmt.Fprintf(a.out, "      %s %-10s %-40s %s\n", st.paint(planColor(r.sym), r.sym), r.label, r.path, st.paint(ansiDim, planNote(r.sym)))
		}

		if s.latest != "" && s.latest != s.version {
			fmt.Fprintf(a.out, "      %s\n", st.paint(ansiYellow, fmt.Sprintf("↑ %s is available (locked %s); run apply --upgrade to move to it", s.latest, s.version)))
		}
	}

	for _, p := range stale {
		counts[planRemove]++
		fmt.Fprintf(a.out, "  %s %-50s %s\n", st.paint(planColor(planRemove), planRemove), p, st.paint(ansiDim, "no longer managed by opendepot.hcl"))
	}

	for _, b := range existingBlocks {
		if sources[b.source] {
			continue
		}

		counts[planRemove]++
		fmt.Fprintf(a.out, "  %s %-50s %s\n", st.paint(planColor(planRemove), planRemove), agentsMDFile+" ("+b.source+")", st.paint(ansiDim, "no longer managed by opendepot.hcl"))
	}

	fmt.Fprintln(a.out)
	var parts []string
	for _, c := range []struct {
		sym  string
		verb string
	}{{planAdd, "to add"}, {planChange, "to change"}, {planOverwrite, "to overwrite"}, {planRemove, "to remove"}} {
		if counts[c.sym] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", counts[c.sym], c.verb))
		}
	}

	if len(parts) == 0 {
		fmt.Fprintln(a.out, st.paint(ansiGreen, "No changes. The project matches the registry and lock file."))
		return nil
	}

	fmt.Fprintf(a.out, "Plan: %s.\n", strings.Join(parts, ", "))
	fmt.Fprintln(a.out, "No changes written. Run opendepot apply to make them.")
	return nil
}

// planTarget compares one install target against the staged content and the lock file.
func (a *app) planTarget(s *stagedEntry, t installTarget, locked string) (string, error) {
	if err := a.ensureNoSymlinks(t.path); err != nil {
		return "", err
	}

	cur, err := a.installedHash(s.kind, t.path)
	if errors.Is(err, os.ErrNotExist) {
		return planAdd, nil
	}

	if err != nil {
		return "", err
	}

	same := cur == s.h1
	if s.kind != kindSkill {
		same, err = sameFileContent(a.path(t.path), s.bodyPath())
		if err != nil {
			return "", err
		}
	}

	if same {
		return planSame, nil
	}

	if cur == locked {
		return planChange, nil
	}

	return planOverwrite, nil
}

// sameFileContent reports whether two files hold identical bytes. Agent files are compared by content because
// their hash depends on the file name, which differs between the staged body and the installed copy.
func sameFileContent(dst, src string) (bool, error) {
	want, err := os.ReadFile(src)
	if err != nil {
		return false, err
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		return false, err
	}

	return bytes.Equal(want, got), nil
}

// lockedInstalledHash returns the hash apply recorded for a target, or "" when none is recorded.
func lockedInstalledHash(le lockEntry, found bool, label string) string {
	if !found {
		return ""
	}

	for _, in := range le.Installed {
		if in.Target == label {
			return in.Hash
		}
	}

	return ""
}

// agentsBlockSymbol compares the managed AGENTS.md block for source before and after apply.
func agentsBlockSymbol(source string, existing, planned []agentsBlock) (string, error) {
	before, err := blockHash(existing, source)
	if err != nil {
		return planAdd, nil
	}

	after, err := blockHash(planned, source)
	if err != nil {
		return "", err
	}

	if before == after {
		return planSame, nil
	}

	return planChange, nil
}

func planColor(sym string) string {
	switch sym {
	case planAdd:
		return ansiGreen
	case planChange:
		return ansiYellow
	case planOverwrite, planRemove:
		return ansiRed
	default:
		return ansiDim
	}
}

func planNote(sym string) string {
	switch sym {
	case planAdd:
		return "create"
	case planChange:
		return "update"
	case planOverwrite:
		return "edited locally; will be overwritten"
	default:
		return "unchanged"
	}
}
