package agentspec

import (
	"bytes"
	"strings"
	"testing"
)

// legacySplitFrontmatter is the whole-string splitter that splitFrontmatter replaced. It is kept here as the
// reference the streamed splitter must match.
func legacySplitFrontmatter(content string) (string, string, bool) {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	rest, ok := strings.CutPrefix(content, "---\n")

	if !ok {
		return "", "", false
	}

	if strings.HasPrefix(rest, "---\n") || rest == "---" {
		return "", strings.TrimPrefix(strings.TrimPrefix(rest, "---"), "\n"), true
	}

	end := strings.Index(rest, "\n---\n")

	if end < 0 {
		if !strings.HasSuffix(rest, "\n---") {
			return "", "", false
		}

		return strings.TrimSuffix(rest, "\n---"), "", true
	}

	return rest[:end+1], strings.TrimPrefix(rest[end+len("\n---\n"):], "\n"), true
}

func TestSplitFrontmatterMatchesLegacy(t *testing.T) {
	inputs := []string{
		"",
		"no frontmatter\n",
		"---\nname: x\n---\nbody\n",
		"---\nname: x\n---\n\nbody\n",
		"---\r\nname: x\r\n---\r\n\r\nbody\r\n",
		"---\n---\n",
		"---\n---\n\nbody",
		"---\n---\nbody",
		"---\n---",
		"---",
		"---\nname: x\n---",
		"---\nname: x\n",
		"---\nname: x\n--- \nbody",
		"---\na: 1\nb: 2\n---\n---\nnot a body marker\n",
		"---\n\n---\n\nbody",
		"---x\n---\nbody",
	}

	for _, in := range inputs {
		wantFM, wantBody, wantOK := legacySplitFrontmatter(in)
		gotFM, gotBody, gotOK, err := splitFrontmatter(bytes.NewReader([]byte(in)))

		if err != nil {
			t.Fatalf("splitFrontmatter(%q) error: %v", in, err)
		}

		if gotFM != wantFM || gotBody != wantBody || gotOK != wantOK {
			t.Errorf("splitFrontmatter(%q) = (%q, %q, %v), want (%q, %q, %v)",
				in, gotFM, gotBody, gotOK, wantFM, wantBody, wantOK)
		}
	}
}

func TestParseAgentReaderMatchesParseAgent(t *testing.T) {
	entry := []byte("---\nname: reviewer\ndescription: Reviews code\n---\n\nYou review code.\n")

	want, err := ParseAgent("agents/reviewer/AGENT.md", entry)
	if err != nil {
		t.Fatalf("ParseAgent error: %v", err)
	}

	got, err := ParseAgentReader("agents/reviewer/AGENT.md", bytes.NewReader(entry))
	if err != nil {
		t.Fatalf("ParseAgentReader error: %v", err)
	}

	if got.Body != want.Body {
		t.Errorf("Body = %q, want %q", got.Body, want.Body)
	}

	if got.Name != want.Name || got.Description != want.Description {
		t.Errorf("parsed fields differ: got %+v, want %+v", got, want)
	}
}
