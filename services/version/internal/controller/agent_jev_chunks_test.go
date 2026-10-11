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
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/tonedefdev/opendepot/pkg/jev"
)

// jevChunkTestInput writes files into a temporary bundle and returns a chunk input for it. SKILL.md is always the entry.
func jevChunkTestInput(t *testing.T, files map[string]string) *jevChunkInput {
	t.Helper()

	dir := t.TempDir()
	if _, ok := files["SKILL.md"]; !ok {
		files["SKILL.md"] = "# entry\n"
	}

	rels := make([]string, 0, len(files))
	for rel, content := range files {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}

		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}

		rels = append(rels, rel)
	}

	sort.Strings(rels)
	return &jevChunkInput{
		dir:         dir,
		entry:       "SKILL.md",
		flagged:     map[string]struct{}{},
		files:       rels,
		kind:        "Module",
		name:        "demo",
		description: "a module",
		tools:       []string{"terraform"},
	}
}

// jevChunkTestPlan plans the input and fails the test on error.
func jevChunkTestPlan(t *testing.T, in *jevChunkInput) jevChunkLayout {
	t.Helper()

	layout, err := jevChunkPlan(in)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}

	return layout
}

// jevChunkTestBuild builds every part of a layout and checks that each one is within the Jev state limit.
func jevChunkTestBuild(t *testing.T, in *jevChunkInput, layout jevChunkLayout) []string {
	t.Helper()

	states := make([]string, 0, len(layout.parts))
	for _, part := range layout.parts {
		state, err := in.build(part, len(layout.parts))
		if err != nil {
			t.Fatalf("build part %s: %v", part.label, err)
		}

		if len(state) > jev.MaxStateBytes {
			t.Fatalf("part %s is %d bytes, over %d", part.label, len(state), jev.MaxStateBytes)
		}

		states = append(states, state)
	}

	return states
}

// jevChunkTestSegments returns the segment pieces of one file across all parts in order.
func jevChunkTestSegments(layout jevChunkLayout, rel string) []jevChunkPiece {
	var out []jevChunkPiece
	for _, part := range layout.parts {
		for _, pc := range part.pieces {
			if pc.seg && pc.file == rel {
				out = append(out, pc)
			}
		}
	}

	return out
}

func TestJevChunkSmallSinglePart(t *testing.T) {
	in := jevChunkTestInput(t, map[string]string{"SKILL.md": "hello\n", "main.tf": "resource {}\n"})
	layout := jevChunkTestPlan(t, in)
	if layout.over || layout.count != 1 || len(layout.parts) != 1 {
		t.Fatalf("want one part, got over=%v count=%d parts=%d", layout.over, layout.count, len(layout.parts))
	}

	states := jevChunkTestBuild(t, in, layout)
	if strings.Contains(states[0], "[part ") || strings.Contains(states[0], "continues in part") {
		t.Fatalf("single part must have no markers:\n%s", states[0])
	}

	for _, want := range []string{"OpenDepot Module \"demo\", part 1 of 1", "hello\n", "--- main.tf ---", "resource {}", "- main.tf\n"} {
		if !strings.Contains(states[0], want) {
			t.Fatalf("state missing %q:\n%s", want, states[0])
		}
	}
}

func TestJevChunkOverCeiling(t *testing.T) {
	budget := jevChunkBodyBudget
	in := jevChunkTestInput(t, map[string]string{
		"SKILL.md": strings.Repeat("a", 16*budget+1),
	})

	layout := jevChunkTestPlan(t, in)
	if !layout.over || layout.parts != nil || layout.count != 17 {
		t.Fatalf("want over with count 17 and nil parts, got over=%v count=%d parts=%v", layout.over, layout.count, layout.parts != nil)
	}
}

func TestJevChunkNotOverNearCeiling(t *testing.T) {
	budget := jevChunkBodyBudget
	in := jevChunkTestInput(t, map[string]string{
		"SKILL.md": strings.Repeat("a", 10*budget),
	})

	layout := jevChunkTestPlan(t, in)
	if layout.over {
		t.Fatalf("10 budgets of content must not be over")
	}

	if len(layout.parts) != layout.count || layout.count < 10 || layout.count > jevMaxChunks {
		t.Fatalf("want 10 to 16 parts, got count=%d parts=%d", layout.count, len(layout.parts))
	}

	jevChunkTestBuild(t, in, layout)
}

func TestJevChunkLargeTreeIsOverWithLowerBound(t *testing.T) {
	files := make(map[string]string)
	for i := 0; i < 5; i++ {
		files[fmt.Sprintf("big/f%d.txt", i)] = strings.Repeat("x", 1<<20)
	}

	in := jevChunkTestInput(t, files)
	layout := jevChunkTestPlan(t, in)
	total := 5 << 20
	want := (total + jevChunkBodyBudget - 1) / jevChunkBodyBudget
	if !layout.over || layout.parts != nil || layout.count != want {
		t.Fatalf("want over with count %d, got over=%v count=%d", want, layout.over, layout.count)
	}
}

func TestJevChunkManySmallFiles(t *testing.T) {
	files := make(map[string]string)
	for i := 0; i < 10000; i++ {
		files[fmt.Sprintf("f%05d.txt", i)] = "x"
	}

	in := jevChunkTestInput(t, files)
	layout := jevChunkTestPlan(t, in)
	if layout.over {
		t.Fatalf("10000 one-byte files must fit in %d parts", jevMaxChunks)
	}

	states := jevChunkTestBuild(t, in, layout)
	all := strings.Join(states, "")
	for _, rel := range []string{"f00000.txt", "f05000.txt", "f09999.txt"} {
		if !strings.Contains(all, "- "+rel+"\n") {
			t.Fatalf("file list is missing %s", rel)
		}
	}

	if !strings.Contains(states[0], "Bundled files:\n") {
		t.Fatalf("part 1 must hold the file list header")
	}

	if layout.skippedCount != 0 || len(layout.skipped) != 0 {
		t.Fatalf("text files must not be skipped, got %d", layout.skippedCount)
	}
}

func TestJevChunkSplitsLargeFileContiguously(t *testing.T) {
	content := strings.Repeat("line of text\n", 100*1024/13+1)
	in := jevChunkTestInput(t, map[string]string{"big.txt": content})
	layout := jevChunkTestPlan(t, in)
	if layout.over || layout.count < 3 {
		t.Fatalf("want at least 3 parts, got over=%v count=%d", layout.over, layout.count)
	}

	jevChunkTestBuild(t, in, layout)
	segs := jevChunkTestSegments(layout, "big.txt")
	var next int64
	for i, pc := range segs {
		if pc.start != next {
			t.Fatalf("segment %d starts at %d, want %d", i, pc.start, next)
		}

		if pc.seq != i+1 || pc.of != len(segs) {
			t.Fatalf("segment %d has seq %d of %d, want %d of %d", i, pc.seq, pc.of, i+1, len(segs))
		}

		if pc.end < pc.fileSize && pc.next == "" {
			t.Fatalf("segment %d ends before the file and has no continuation", i)
		}

		if pc.end == pc.fileSize && pc.next != "" {
			t.Fatalf("final segment must not continue")
		}

		next = pc.end
	}

	if next != int64(len(content)) {
		t.Fatalf("segments cover %d bytes, want %d", next, len(content))
	}
}

func TestJevChunkNewlineCut(t *testing.T) {
	content := strings.Repeat("line\n", 40000)
	in := jevChunkTestInput(t, map[string]string{"big.txt": content})
	layout := jevChunkTestPlan(t, in)
	segs := jevChunkTestSegments(layout, "big.txt")
	if len(segs) < 2 {
		t.Fatalf("want a split file, got %d segments", len(segs))
	}

	for _, pc := range segs[:len(segs)-1] {
		if content[pc.end-1] != '\n' {
			t.Fatalf("segment ending at %d does not end on a newline", pc.end)
		}
	}
}

func TestJevChunkRuneCut(t *testing.T) {
	content := strings.Repeat("€", 30000)
	in := jevChunkTestInput(t, map[string]string{"big.txt": content})
	layout := jevChunkTestPlan(t, in)
	segs := jevChunkTestSegments(layout, "big.txt")
	if len(segs) < 2 {
		t.Fatalf("want a split file, got %d segments", len(segs))
	}

	for _, pc := range segs[:len(segs)-1] {
		if !utf8.RuneStart(content[pc.end]) {
			t.Fatalf("segment ends inside a rune at %d", pc.end)
		}
	}

	jevChunkTestBuild(t, in, layout)
}

func TestJevChunkHalveSplitsAndLabels(t *testing.T) {
	content := strings.Repeat("line of text\n", 100*1024/13+1)
	in := jevChunkTestInput(t, map[string]string{"big.txt": content})
	layout := jevChunkTestPlan(t, in)
	first, second, ok, err := in.halve(layout.parts[0], layout.count)
	if err != nil || !ok {
		t.Fatalf("halve part 1: ok=%v err=%v", ok, err)
	}

	if first.label != "1a" || second.label != "1b" || first.depth != 1 || second.depth != 1 {
		t.Fatalf("unexpected halves %s/%s depth %d/%d", first.label, second.label, first.depth, second.depth)
	}

	for _, half := range []jevChunkPart{first, second} {
		if _, err := in.build(half, layout.count); err != nil {
			t.Fatalf("half %s: %v", half.label, err)
		}
	}

	for _, pc := range first.pieces {
		if pc.seg && pc.end < pc.fileSize && pc.next == "" {
			t.Fatalf("first half segment ends before the file with no continuation")
		}
	}

	if last := first.pieces[len(first.pieces)-1]; last.next != "1b" && last.end < last.fileSize {
		t.Fatalf("first half must continue into 1b, got %q", last.next)
	}

	deeper, _, ok, err := in.halve(first, layout.count)
	if err != nil || !ok || deeper.label != "1aa" || deeper.depth != 2 {
		t.Fatalf("second halving: label=%s ok=%v err=%v", deeper.label, ok, err)
	}

	if _, _, ok, _ := in.halve(deeper, layout.count); ok {
		t.Fatalf("a third halving must not be allowed")
	}
}

func TestJevChunkClassifyBinaryAndInvalidUTF8(t *testing.T) {
	dir := t.TempDir()
	nul := filepath.Join(dir, "nul.bin")
	if err := os.WriteFile(nul, []byte("abc\x00def"), 0o600); err != nil {
		t.Fatal(err)
	}

	bad := filepath.Join(dir, "bad.txt")
	if err := os.WriteFile(bad, []byte{'a', 0xff, 'b'}, 0o600); err != nil {
		t.Fatal(err)
	}

	for _, p := range []string{nul, bad} {
		class, err := jevChunkScan(p)
		if err != nil || class != jevChunkBinary {
			t.Fatalf("%s: want binary, got %v err=%v", p, class, err)
		}
	}
}

func TestJevChunkRuneStraddlesReadBoundary(t *testing.T) {
	dir := t.TempDir()
	full := filepath.Join(dir, "straddle.txt")
	data := strings.Repeat("a", jevChunkReadSize-1) + "€" + "tail"
	if err := os.WriteFile(full, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}

	class, err := jevChunkScan(full)
	if err != nil || class != jevChunkText {
		t.Fatalf("want text across the read boundary, got %v err=%v", class, err)
	}
}

func TestJevChunkWithheldFilesAreNotSent(t *testing.T) {
	in := jevChunkTestInput(t, map[string]string{
		"SKILL.md":    "# entry\n",
		"secret.env":  "TOPSECRETVALUE",
		"nested/x.go": "PRIVATEBODY",
	})

	in.flagged = map[string]struct{}{"secret.env": {}, "x.go": {}}
	layout := jevChunkTestPlan(t, in)
	states := jevChunkTestBuild(t, in, layout)
	all := strings.Join(states, "")
	if strings.Contains(all, "TOPSECRETVALUE") || strings.Contains(all, "PRIVATEBODY") {
		t.Fatalf("withheld content was sent")
	}

	if !strings.Contains(all, "[withheld: flagged for a secret]") {
		t.Fatalf("withheld marker missing")
	}

	if layout.skippedCount != 2 {
		t.Fatalf("want 2 skipped, got %d", layout.skippedCount)
	}
}

func TestJevChunkFlaggedEntryIsWithheld(t *testing.T) {
	in := jevChunkTestInput(t, map[string]string{"SKILL.md": "ENTRYSECRET"})
	in.entryFlagged = true
	layout := jevChunkTestPlan(t, in)
	states := jevChunkTestBuild(t, in, layout)
	if strings.Contains(states[0], "ENTRYSECRET") {
		t.Fatalf("flagged entry content was sent")
	}

	if !strings.Contains(states[0], "[withheld: entry file flagged for a secret]") {
		t.Fatalf("entry withheld marker missing")
	}
}

func TestJevChunkEmptyFileIsNotSkipped(t *testing.T) {
	in := jevChunkTestInput(t, map[string]string{"empty.txt": ""})
	layout := jevChunkTestPlan(t, in)
	if layout.skippedCount != 0 {
		t.Fatalf("empty file must not be skipped, got %d", layout.skippedCount)
	}

	jevChunkTestBuild(t, in, layout)
}

func TestJevChunkSkipListCapped(t *testing.T) {
	files := make(map[string]string)
	for i := 0; i < 40; i++ {
		files[fmt.Sprintf("bin%02d.dat", i)] = "\x00\x01"
	}

	in := jevChunkTestInput(t, files)
	layout := jevChunkTestPlan(t, in)
	if layout.skippedCount != 40 || len(layout.skipped) != jevChunkListLimit {
		t.Fatalf("want 40 skipped with 32 listed, got %d and %d", layout.skippedCount, len(layout.skipped))
	}
}

func TestJevChunkLongPathTruncated(t *testing.T) {
	long := strings.Repeat("a", 70) + "/" + strings.Repeat("b", 70) + "/" + strings.Repeat("c", 70) + ".dat"
	in := jevChunkTestInput(t, map[string]string{long: "\x00"})
	layout := jevChunkTestPlan(t, in)
	if len(layout.skipped) != 1 || len(layout.skipped[0].path) > jevChunkPathLimit {
		t.Fatalf("want one truncated skipped path, got %+v", layout.skipped)
	}
}

func TestJevChunkFullPreambleFitsLimit(t *testing.T) {
	in := jevChunkTestInput(t, map[string]string{
		"SKILL.md": strings.Repeat("a", jevChunkBodyBudget),
	})

	in.name = strings.Repeat("n", 1000)
	in.description = strings.Repeat("d", 5000)
	in.tools = []string{strings.Repeat("t", 2000)}
	layout := jevChunkTestPlan(t, in)
	jevChunkTestBuild(t, in, layout)
}

func TestJevChunkFileListOnlyInFirstPart(t *testing.T) {
	in := jevChunkTestInput(t, map[string]string{
		"big.txt": strings.Repeat("line of text\n", 100*1024/13+1),
	})

	layout := jevChunkTestPlan(t, in)
	states := jevChunkTestBuild(t, in, layout)
	if len(states) < 2 {
		t.Fatalf("want multiple parts")
	}

	if strings.Contains(states[1], "Bundled files:") {
		t.Fatalf("part 2 must not repeat the file list")
	}

	if !strings.Contains(states[1], "The entry file and the full bundled file list are in part 1.") {
		t.Fatalf("part 2 must point to part 1")
	}
}

func TestJevChunkBuildWithinLimitForSplitItems(t *testing.T) {
	budget := jevChunkBodyBudget
	in := jevChunkTestInput(t, map[string]string{
		"SKILL.md": strings.Repeat("a", budget-100),
		"b.txt":    strings.Repeat("b", budget+5),
	})

	layout := jevChunkTestPlan(t, in)
	jevChunkTestBuild(t, in, layout)
	if !utf8.ValidString(strings.Join(jevChunkTestBuild(t, in, layout), "")) {
		t.Fatalf("built state is not valid UTF-8")
	}
}

func TestJevChunkTruncateRespectsRunes(t *testing.T) {
	got := jevChunkTruncate("ab€€", 4)
	if got != "ab" {
		t.Fatalf("want ab, got %q", got)
	}
}

func TestJevChunkPlanMissingEntry(t *testing.T) {
	in := jevChunkTestInput(t, map[string]string{"other.txt": "x"})
	in.entry = "missing.md"
	if _, err := jevChunkPlan(in); err == nil {
		t.Fatalf("want error for a missing entry file")
	}
}
