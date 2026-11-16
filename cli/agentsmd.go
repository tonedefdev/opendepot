package main

import (
	"fmt"
	"sort"
	"strings"
)

const (
	agentsBeginPrefix = "<!-- opendepot:begin "
	agentsEndPrefix   = "<!-- opendepot:end "
	agentsMarkerSufx  = " -->"
)

// agentsBlock is a delimited block in AGENTS.md. first and last are the marker line indexes.
type agentsBlock struct {
	source string
	inner  string
	first  int
	last   int
}

// canonicalInner normalizes block content so that it has exactly one trailing newline.
func canonicalInner(body string) string {
	trimmed := strings.TrimRight(body, "\n")
	if trimmed == "" {
		return ""
	}

	return trimmed + "\n"
}

func splitLines(content string) []string {
	if content == "" {
		return nil
	}

	return strings.Split(strings.TrimSuffix(content, "\n"), "\n")
}

func markerSource(line, prefix string) (string, bool) {
	if !strings.HasPrefix(line, prefix) || !strings.HasSuffix(line, agentsMarkerSufx) {
		return "", false
	}

	source := strings.TrimSuffix(strings.TrimPrefix(line, prefix), agentsMarkerSufx)
	return source, source != ""
}

// parseAgentsBlocks finds every opendepot block and rejects unbalanced, nested, or duplicate markers.
func parseAgentsBlocks(content string) ([]agentsBlock, error) {
	lines := splitLines(content)
	var blocks []agentsBlock
	seen := map[string]bool{}
	open := -1
	openSource := ""
	for i, raw := range lines {
		line := strings.TrimSpace(raw)
		if source, ok := markerSource(line, agentsBeginPrefix); ok {
			if open >= 0 {
				return nil, fmt.Errorf("AGENTS.md: block %s starts inside block %s", source, openSource)
			}

			if seen[source] {
				return nil, fmt.Errorf("AGENTS.md: duplicate block for %s", source)
			}

			seen[source] = true
			open = i
			openSource = source
			continue
		}

		if source, ok := markerSource(line, agentsEndPrefix); ok {
			if open < 0 || source != openSource {
				return nil, fmt.Errorf("AGENTS.md: end marker for %s has no matching begin marker", source)
			}

			inner := ""
			if i > open+1 {
				inner = strings.Join(lines[open+1:i], "\n") + "\n"
			}

			blocks = append(blocks, agentsBlock{source: source, inner: inner, first: open, last: i})
			open = -1
		}
	}

	if open >= 0 {
		return nil, fmt.Errorf("AGENTS.md: block %s has no end marker", openSource)
	}

	return blocks, nil
}

func blockLines(source, inner string) []string {
	out := []string{agentsBeginPrefix + source + agentsMarkerSufx}
	if inner != "" {
		out = append(out, strings.Split(strings.TrimSuffix(inner, "\n"), "\n")...)
	}

	return append(out, agentsEndPrefix+source+agentsMarkerSufx)
}

// planAgentsMD returns the AGENTS.md content with desired blocks inserted or updated, and blocks
// recorded in the lock but no longer desired removed. Blocks not tracked by the lock are left alone.
// A block whose content no longer matches its recorded hash was hand-edited and requires force.
// Unchanged content is returned as-is.
func planAgentsMD(content string, desired, recorded map[string]string, force bool) (string, error) {
	blocks, err := parseAgentsBlocks(content)
	if err != nil {
		return "", err
	}

	lines := splitLines(content)
	var out []string
	seen := map[string]bool{}
	changed := false
	pos := 0
	for _, b := range blocks {
		out = append(out, lines[pos:b.first]...)
		pos = b.last + 1
		want, wanted := desired[b.source]
		_, tracked := recorded[b.source]
		if !wanted && !tracked {
			out = append(out, lines[b.first:b.last+1]...)
			continue
		}

		if wanted && b.inner == want {
			out = append(out, lines[b.first:b.last+1]...)
			seen[b.source] = true
			continue
		}

		if !force {
			current, err := hashBlock(b.inner)
			if err != nil {
				return "", err
			}

			if recorded[b.source] != current {
				return "", fmt.Errorf("AGENTS.md block for %s was edited by hand; re-run with -force to overwrite", b.source)
			}
		}

		changed = true
		if wanted {
			out = append(out, blockLines(b.source, want)...)
			seen[b.source] = true
		}
	}

	out = append(out, lines[pos:]...)

	var missing []string
	for source := range desired {
		if !seen[source] {
			missing = append(missing, source)
		}
	}

	sort.Strings(missing)
	for _, source := range missing {
		changed = true
		if len(out) > 0 && out[len(out)-1] != "" {
			out = append(out, "")
		}

		out = append(out, blockLines(source, desired[source])...)
	}

	if !changed {
		return content, nil
	}

	if len(out) == 0 {
		return "", nil
	}

	return strings.Join(out, "\n") + "\n", nil
}
