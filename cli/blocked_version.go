package main

import (
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

const (
	blockedLabelWidth = 12
	blockedValueWidth = 65
)

// printBlockedVersion explains why a Jev-blocked Version cannot be installed. It uses the same label column
// layout as the assessment output. source may be empty.
func printBlockedVersion(w io.Writer, source string, e *blockedVersionError) {
	st := newStyler(w)

	name := "version " + e.version
	if source != "" {
		name = source + "@" + e.version
	}

	next := "installs are refused until the policy allows this version"
	if e.locked {
		next = "the lock file pins this version; re-run with -upgrade to choose another"
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, st.paint(ansiBold+ansiRed, "✖ "+name))
	writeBlockedRows(w, st, "Status", "blocked by a Jev policy · not installable", ansiRed)

	if len(e.reasons) == 0 {
		writeBlockedRows(w, st, "Reason", "none recorded", ansiDim)
	}

	for i, reason := range e.reasons {
		label := ""
		if i == 0 {
			label = "Reason"
		}

		writeBlockedRows(w, st, label, reason, "")
	}

	writeBlockedRows(w, st, "Next", next, "")
}

// writeBlockedRows writes text after a label column, wrapping long text onto aligned continuation lines.
func writeBlockedRows(w io.Writer, st styler, label, text, color string) {
	for i, line := range wrapText(text, blockedValueWidth) {
		lab := ""
		if i == 0 {
			lab = label
		}

		fmt.Fprintf(w, "  %-*s %s\n", blockedLabelWidth, lab, st.paint(color, line))
	}
}

// wrapText breaks text at spaces into lines of at most width runes. A word longer than width keeps its own line.
func wrapText(text string, width int) []string {
	var lines []string
	line := ""
	for _, word := range strings.Fields(text) {
		switch {
		case line == "":
			line = word
		case utf8.RuneCountInString(line)+1+utf8.RuneCountInString(word) <= width:
			line += " " + word
		default:
			lines = append(lines, line)
			line = word
		}
	}

	if line != "" {
		lines = append(lines, line)
	}

	if len(lines) == 0 {
		return []string{""}
	}

	return lines
}
