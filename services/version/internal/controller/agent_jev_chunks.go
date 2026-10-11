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
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/tonedefdev/opendepot/pkg/jev"
)

// jevMaxChunks is the most parts a Version's content is assessed in. Content that needs more parts is blocked.
const jevMaxChunks = 16

// jevChunkPreambleReserve is the part of the Jev state limit kept for the preamble of each part.
const jevChunkPreambleReserve = 2048

// jevMaxHalvings is how many times a part may be halved after Jev rejects it for its size.
const jevMaxHalvings = 2

// jevChunkBodyBudget is the number of content bytes a part may hold before its preamble is added.
const jevChunkBodyBudget = jev.MaxStateBytes - jevChunkPreambleReserve

// jevChunkMarkerReserve is charged to each segment of a split file for its start and continuation markers.
const jevChunkMarkerReserve = 160

// jevChunkCutWindow is how far before a byte limit a cut looks for a newline.
const jevChunkCutWindow = 1024

// jevChunkReadSize is the fixed buffer used to stream file content.
const jevChunkReadSize = 32 << 10

// jevChunkListLimit is the most paths a status field lists. The exact count is kept alongside.
const jevChunkListLimit = 32

// jevChunkPathLimit is the byte limit for a path in a status field.
const jevChunkPathLimit = 256

// jevChunkNameLimit is the byte limit for the Jev name in a preamble.
const jevChunkNameLimit = 256

// jevChunkDescriptionLimit is the byte limit for the description in a preamble.
const jevChunkDescriptionLimit = 1024

// jevChunkToolsLimit is the byte limit for the tools in a preamble.
const jevChunkToolsLimit = 512

// jevChunkListHeader starts the bundled file list in the first part that holds it.
const jevChunkListHeader = "\n\nBundled files:\n"

// jevChunkListContinued starts the bundled file list where it continues in a later part.
const jevChunkListContinued = "Bundled files (continued):\n"

// errJevChunkOversize reports a built part that is over the Jev state limit.
var errJevChunkOversize = errors.New("part over Jev state limit")

// jevChunkClass is how a bundled file is sent to Jev.
type jevChunkClass int

const (
	// jevChunkText is a UTF-8 text file whose content is sent.
	jevChunkText jevChunkClass = iota

	// jevChunkWithheld is a file flagged for a secret. Its content is never sent.
	jevChunkWithheld

	// jevChunkBinary is a file with a NUL byte or invalid UTF-8. Its content is never sent.
	jevChunkBinary
)

// jevChunkInput is the content to split into parts. dir is the extracted bundle root and every path is relative
// to it, in slash form.
type jevChunkInput struct {
	dir          string
	entry        string
	entryFlagged bool
	flagged      map[string]struct{}
	files        []string
	kind         string
	name         string
	description  string
	tools        []string
}

// jevChunkFile is a bundled file with its size and class.
type jevChunkFile struct {
	path  string
	size  int64
	class jevChunkClass
}

// jevChunkSkip is a file whose content was not sent.
type jevChunkSkip struct {
	path   string
	reason string
}

// jevChunkPiece is one contiguous piece of a part. A literal piece carries text. A segment piece carries the byte
// range [start, end) of file, and its fileSize is the size of the whole file.
type jevChunkPiece struct {
	text     string
	file     string
	seg      bool
	entry    bool
	start    int64
	end      int64
	size     int64
	fileSize int64
	seq      int
	of       int
	next     string
}

// jevChunkPart is one part of the assessed content. Its label is 1-based, and a halved part adds an a or b suffix.
type jevChunkPart struct {
	label  string
	depth  int
	pieces []jevChunkPiece
	files  []string
}

// jevChunkLayout is the result of planning. parts is nil when over is set, and count is then a lower bound on the
// number of parts. skipped lists at most jevChunkListLimit files, and skippedCount is exact when over is not set.
type jevChunkLayout struct {
	parts        []jevChunkPart
	count        int
	over         bool
	skipped      []jevChunkSkip
	skippedCount int
}

// jevChunkPacker fills parts in order. A part holds at most budget bytes of content plus the marker reserves.
type jevChunkPacker struct {
	dir    string
	budget int64
	parts  []jevChunkPart
	cur    jevChunkPart
	used   int64
	over   bool
}

// jevChunkPlan classifies every bundled file and packs the content into parts. Files are read only while the content
// could still fit in jevMaxChunks parts. Once it cannot, planning stops and reports a lower bound on the count.
func jevChunkPlan(in *jevChunkInput) (jevChunkLayout, error) {
	budget := int64(jevChunkBodyBudget)
	sizes := make([]int64, len(in.files))
	var total int64
	for i, rel := range in.files {
		info, err := os.Stat(filepath.Join(in.dir, filepath.FromSlash(rel)))
		if err != nil {
			return jevChunkLayout{}, fmt.Errorf("stat %s: %w", rel, err)
		}

		sizes[i] = info.Size()
		total += info.Size()
	}

	lower := max(jevMaxChunks+1, int((total+budget-1)/budget))
	files := make([]jevChunkFile, len(in.files))
	var skipped []jevChunkSkip
	var text int64
	for i, rel := range in.files {
		class, err := in.classify(rel, sizes[i])
		if err != nil {
			return jevChunkLayout{}, err
		}

		if class == jevChunkText {
			text += sizes[i]
			if text > jevMaxChunks*budget {
				return jevChunkOver(lower, skipped), nil
			}
		}

		if class != jevChunkText {
			skipped = append(skipped, jevChunkSkip{path: rel, reason: jevChunkReason(class)})
		}

		files[i] = jevChunkFile{path: rel, size: sizes[i], class: class}
	}

	entryIndex := -1
	for i, f := range files {
		if f.path == in.entry {
			entryIndex = i
		}
	}

	if entryIndex < 0 {
		return jevChunkLayout{}, fmt.Errorf("entry file %s is not in the bundled files", in.entry)
	}

	p := &jevChunkPacker{dir: in.dir, budget: budget}
	p.item(jevChunkItem(files[entryIndex], true))
	for i, f := range files {
		text := "- " + f.path + "\n"
		if i == 0 {
			text = jevChunkListHeader + text
		} else if p.used+int64(len(text)) > budget {
			text = jevChunkListContinued + text
		}

		p.item([]jevChunkPiece{{text: text}})
	}

	for i, f := range files {
		if i == entryIndex {
			continue
		}

		p.item(jevChunkItem(f, false))
	}

	p.flush()
	if p.over {
		return jevChunkOver(lower, skipped), nil
	}

	parts, err := p.finish()
	if err != nil {
		return jevChunkLayout{}, err
	}

	return jevChunkLayout{
		parts:        parts,
		count:        len(parts),
		skipped:      jevChunkListSkips(skipped),
		skippedCount: len(skipped),
	}, nil
}

// jevChunkOver is the layout for content that needs more than jevMaxChunks parts.
func jevChunkOver(lower int, skipped []jevChunkSkip) jevChunkLayout {
	return jevChunkLayout{
		count:        lower,
		over:         true,
		skipped:      jevChunkListSkips(skipped),
		skippedCount: len(skipped),
	}
}

// classify decides how a bundled file is sent. A file flagged for a secret is withheld without being read. Any other
// file is streamed once to find NUL bytes and invalid UTF-8.
func (in *jevChunkInput) classify(rel string, size int64) (jevChunkClass, error) {
	if in.entryFlagged && rel == in.entry {
		return jevChunkWithheld, nil
	}

	if _, ok := in.flagged[path.Base(rel)]; ok {
		return jevChunkWithheld, nil
	}

	if size == 0 {
		return jevChunkText, nil
	}

	return jevChunkScan(filepath.Join(in.dir, filepath.FromSlash(rel)))
}

// jevChunkScan streams a file through a fixed buffer and reports jevChunkBinary when it holds a NUL byte or bytes
// that are not valid UTF-8. A rune split between two reads is carried into the next read.
func jevChunkScan(full string) (jevChunkClass, error) {
	f, err := os.Open(full)
	if err != nil {
		return 0, fmt.Errorf("open %s: %w", full, err)
	}

	defer f.Close()

	buf := make([]byte, jevChunkReadSize)
	var carry []byte
	for {
		n, rerr := f.Read(buf)
		if n > 0 {
			chunk := make([]byte, 0, len(carry)+n)
			chunk = append(chunk, carry...)
			chunk = append(chunk, buf[:n]...)
			if bytes.IndexByte(chunk, 0) >= 0 {
				return jevChunkBinary, nil
			}

			k := jevChunkPartialRune(chunk)
			if !utf8.Valid(chunk[:len(chunk)-k]) {
				return jevChunkBinary, nil
			}

			carry = chunk[len(chunk)-k:]
		}

		if rerr == io.EOF {
			break
		}

		if rerr != nil {
			return 0, fmt.Errorf("read %s: %w", full, rerr)
		}
	}

	if len(carry) > 0 {
		return jevChunkBinary, nil
	}

	return jevChunkText, nil
}

// jevChunkPartialRune returns how many trailing bytes of b begin a multi-byte rune that is not yet complete.
func jevChunkPartialRune(b []byte) int {
	for i := 1; i <= 3 && i <= len(b); i++ {
		c := b[len(b)-i]
		if c&0xC0 == 0x80 {
			continue
		}

		if n := jevChunkRuneLen(c); n > i {
			return i
		}

		return 0
	}

	return 0
}

// jevChunkRuneLen returns the length of the rune that starts with c, or 0 when c cannot start a rune.
func jevChunkRuneLen(c byte) int {
	switch {
	case c < 0x80:
		return 1
	case c&0xE0 == 0xC0:
		return 2
	case c&0xF0 == 0xE0:
		return 3
	case c&0xF8 == 0xF0:
		return 4
	}

	return 0
}

// jevChunkReason is the status reason for a skipped file.
func jevChunkReason(class jevChunkClass) string {
	if class == jevChunkWithheld {
		return "Withheld"
	}

	return "Binary"
}

// jevChunkItem returns the pieces that carry one bundled file. The entry file is sent as is, and every other file is
// introduced by a header line that names it.
func jevChunkItem(f jevChunkFile, entry bool) []jevChunkPiece {
	if f.class == jevChunkWithheld {
		text := "\n--- " + f.path + " ---\n[withheld: flagged for a secret]\n"
		if entry {
			text = "[withheld: entry file flagged for a secret]\n"
		}

		return []jevChunkPiece{{text: text, file: f.path, entry: entry}}
	}

	if f.class == jevChunkBinary {
		text := "\n--- " + f.path + " ---\n[skipped: binary file]\n"
		if entry {
			text = "[skipped: binary file]\n"
		}

		return []jevChunkPiece{{text: text, file: f.path, entry: entry}}
	}

	if entry {
		if f.size == 0 {
			return []jevChunkPiece{{file: f.path, entry: true}}
		}

		return []jevChunkPiece{jevChunkSegment(f, 0, f.size, entry)}
	}

	pieces := []jevChunkPiece{{text: "\n--- " + f.path + " ---\n", file: f.path}}
	if f.size > 0 {
		pieces = append(pieces, jevChunkSegment(f, 0, f.size, false))
	}

	return append(pieces, jevChunkPiece{text: "\n", file: f.path})
}

// jevChunkSegment returns a segment piece for the byte range [start, end) of a file.
func jevChunkSegment(f jevChunkFile, start, end int64, entry bool) jevChunkPiece {
	return jevChunkPiece{
		file:     f.path,
		seg:      true,
		entry:    entry,
		start:    start,
		end:      end,
		size:     end - start,
		fileSize: f.size,
	}
}

// item places the pieces of one item. An item that fits the open part is appended whole. An item that fits an empty
// part starts a new part. A larger item is split across parts at newlines or rune boundaries.
func (p *jevChunkPacker) item(pieces []jevChunkPiece) {
	if p.over {
		return
	}

	var raw int64
	for i := range pieces {
		if !pieces[i].seg {
			pieces[i].size = int64(len(pieces[i].text))
		}

		raw += pieces[i].size
	}

	if p.used+raw <= p.budget {
		for _, pc := range pieces {
			p.add(pc, 0)
		}

		return
	}

	if raw <= p.budget {
		p.flush()
		for _, pc := range pieces {
			p.add(pc, 0)
		}

		return
	}

	for _, pc := range pieces {
		if p.over {
			return
		}

		if !pc.seg {
			if p.used+pc.size > p.budget {
				p.flush()
			}

			p.add(pc, 0)
			continue
		}

		p.split(pc)
	}
}

// split places one segment of a split item. Each piece it places is charged the marker reserve. A cut that finds no
// boundary within the room left is an error only when the part is empty, because otherwise the part is flushed first.
func (p *jevChunkPacker) split(pc jevChunkPiece) {
	start := pc.start
	for start < pc.end && !p.over {
		room := p.budget - p.used - jevChunkMarkerReserve
		if room <= 0 {
			p.flush()
			continue
		}

		if pc.end-start <= room {
			rest := pc
			rest.start = start
			rest.size = pc.end - start
			rest.end = pc.end
			p.add(rest, jevChunkMarkerReserve)
			return
		}

		cut, err := jevChunkCut(p.dir, pc.file, start, start+room)
		if err != nil {
			p.over = true
			return
		}

		if cut <= start {
			if len(p.cur.pieces) == 0 {
				p.over = true
				return
			}

			p.flush()
			continue
		}

		head := pc
		head.start = start
		head.end = cut
		head.size = cut - start
		p.add(head, jevChunkMarkerReserve)
		p.flush()
		start = cut
	}
}

// add appends a piece to the open part. reserve is charged to the part's budget with the piece. A piece that would
// open part jevMaxChunks+1 sets over instead.
func (p *jevChunkPacker) add(pc jevChunkPiece, reserve int64) {
	if p.over {
		return
	}

	if len(p.cur.pieces) == 0 && len(p.parts) >= jevMaxChunks {
		p.over = true
		return
	}

	p.cur.pieces = append(p.cur.pieces, pc)
	p.used += pc.size + reserve
}

// flush closes the open part.
func (p *jevChunkPacker) flush() {
	if len(p.cur.pieces) == 0 {
		return
	}

	p.parts = append(p.parts, p.cur)
	p.cur = jevChunkPart{}
	p.used = 0
}

// finish closes the open part, labels the parts, and sets the sequence, count and continuation of each segment.
func (p *jevChunkPacker) finish() ([]jevChunkPart, error) {
	p.flush()
	parts := p.parts
	pieces := make(map[string]int)
	for i := range parts {
		for _, pc := range parts[i].pieces {
			if pc.seg {
				pieces[pc.file]++
			}
		}
	}

	seq := make(map[string]int)
	for i := range parts {
		parts[i].label = strconv.Itoa(i + 1)
		for j := range parts[i].pieces {
			pc := &parts[i].pieces[j]
			if !pc.seg {
				continue
			}

			seq[pc.file]++
			pc.seq = seq[pc.file]
			pc.of = pieces[pc.file]
			if pc.end < pc.fileSize {
				if i+1 >= len(parts) {
					return nil, fmt.Errorf("segment of %s continues past the last part", pc.file)
				}

				pc.next = strconv.Itoa(i + 2)
			}
		}

		parts[i].files = jevChunkFiles(parts[i].pieces)
	}

	return parts, nil
}

// jevChunkFiles returns the distinct files that have content in a set of pieces, in order.
func jevChunkFiles(pieces []jevChunkPiece) []string {
	var files []string
	seen := make(map[string]struct{})
	for _, pc := range pieces {
		if pc.file == "" {
			continue
		}

		if _, ok := seen[pc.file]; ok {
			continue
		}

		seen[pc.file] = struct{}{}
		files = append(files, pc.file)
	}

	return files
}

// jevChunkCut returns the byte offset at which to end a segment that starts at start and must end at or before
// limit. It cuts after the last newline in the window before limit. Without a newline, it cuts at limit and steps back
// over any continuation bytes so that no rune is split.
func jevChunkCut(dir, rel string, start, limit int64) (int64, error) {
	f, err := os.Open(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		return 0, fmt.Errorf("open %s: %w", rel, err)
	}

	defer f.Close()

	lo := max(start, limit-jevChunkCutWindow)
	window := make([]byte, limit-lo)
	n, err := f.ReadAt(window, lo)
	if n != len(window) {
		return 0, fmt.Errorf("read %s: %w", rel, err)
	}

	if i := bytes.LastIndexByte(window, '\n'); i >= 0 {
		return lo + int64(i) + 1, nil
	}

	cut := limit
	for k := 0; k < 3 && cut > start; k++ {
		var b [1]byte
		if _, err := f.ReadAt(b[:], cut); err != nil {
			return 0, fmt.Errorf("read %s: %w", rel, err)
		}

		if b[0]&0xC0 != 0x80 {
			break
		}

		cut--
	}

	return cut, nil
}

// jevChunkCopy writes the byte range of a segment piece to w, reading through a section reader.
func jevChunkCopy(w io.Writer, dir string, pc jevChunkPiece) error {
	f, err := os.Open(filepath.Join(dir, filepath.FromSlash(pc.file)))
	if err != nil {
		return fmt.Errorf("open %s: %w", pc.file, err)
	}

	defer f.Close()

	n, err := io.Copy(w, io.NewSectionReader(f, pc.start, pc.size))
	if err != nil {
		return fmt.Errorf("read %s: %w", pc.file, err)
	}

	if n != pc.size {
		return fmt.Errorf("read %s: got %d bytes, want %d", pc.file, n, pc.size)
	}

	return nil
}

// jevChunkMarker returns the header that tells Jev where a segment sits in its file.
func jevChunkMarker(pc jevChunkPiece) string {
	if pc.start == 0 {
		return fmt.Sprintf("[part %d of %d, bytes %d-%d of %d]\n", pc.seq, pc.of, pc.start, pc.end-1, pc.fileSize)
	}

	return fmt.Sprintf("[continued, bytes %d-%d of %d]\n", pc.start, pc.end-1, pc.fileSize)
}

// preamble returns the header that opens a part. A part without the entry file points to part 1, which holds it.
func (in *jevChunkInput) preamble(label string, total int, hasEntry bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "OpenDepot %s \"%s\", part %s of %d\n", in.kind, jevChunkTruncate(in.name, jevChunkNameLimit), label, total)
	b.WriteString(jevChunkTruncate(in.description, jevChunkDescriptionLimit))
	fmt.Fprintf(&b, "\nTools: %s\n", jevChunkTruncate(strings.Join(in.tools, ", "), jevChunkToolsLimit))
	if !hasEntry {
		b.WriteString("The entry file and the full bundled file list are in part 1.\n")
	}

	b.WriteString("\n")
	return b.String()
}

// build returns the full state for one part. total is the number of parts in the layout.
func (in *jevChunkInput) build(part jevChunkPart, total int) (string, error) {
	hasEntry := false
	for _, pc := range part.pieces {
		if pc.entry {
			hasEntry = true
		}
	}

	var b strings.Builder
	b.WriteString(in.preamble(part.label, total, hasEntry))
	for _, pc := range part.pieces {
		if !pc.seg {
			b.WriteString(pc.text)
			continue
		}

		if pc.of > 1 {
			b.WriteString(jevChunkMarker(pc))
		}

		if err := jevChunkCopy(&b, in.dir, pc); err != nil {
			return "", err
		}

		if pc.next != "" {
			fmt.Fprintf(&b, "\n[continues in part %s]\n", pc.next)
		}
	}

	if b.Len() > jev.MaxStateBytes {
		return "", fmt.Errorf("part %s is %d bytes, over the %d byte limit: %w", part.label, b.Len(), jev.MaxStateBytes, errJevChunkOversize)
	}

	return b.String(), nil
}

// halve splits a part at its byte midpoint. A segment that crosses the midpoint is cut at a newline or rune boundary
// near it. The first half continues into the second half, labelled with a b suffix. ok is false when the part is
// already halved jevMaxHalvings times, when a half would be empty, or when a half is still over the Jev limit.
func (in *jevChunkInput) halve(part jevChunkPart, total int) (first, second jevChunkPart, ok bool, err error) {
	if part.depth >= jevMaxHalvings {
		return first, second, false, nil
	}

	var raw int64
	for _, pc := range part.pieces {
		raw += pc.size
	}

	mid := raw / 2
	var firstPieces, secondPieces []jevChunkPiece
	var acc int64
	var cut bool
	crossed := false
	for _, pc := range part.pieces {
		if crossed {
			secondPieces = append(secondPieces, pc)
			continue
		}

		if acc+pc.size <= mid {
			firstPieces = append(firstPieces, pc)
			acc += pc.size
			continue
		}

		crossed = true
		offset := mid - acc
		if pc.seg && offset > 0 {
			at, err := jevChunkCut(in.dir, pc.file, pc.start, pc.start+offset)
			if err != nil {
				return first, second, false, err
			}

			if at > pc.start && at < pc.end {
				head := pc
				head.end = at
				head.size = at - pc.start
				tail := pc
				tail.start = at
				tail.size = pc.end - at
				firstPieces = append(firstPieces, head)
				secondPieces = append(secondPieces, tail)
				cut = true
				continue
			}
		}

		secondPieces = append(secondPieces, pc)
	}

	if !crossed {
		return first, second, false, nil
	}

	if n := len(firstPieces); n > 0 && (cut || firstPieces[n-1].next != "") {
		firstPieces[n-1].next = part.label + "b"
	}

	if jevChunkBytes(firstPieces) == 0 || jevChunkBytes(secondPieces) == 0 {
		return first, second, false, nil
	}

	first = jevChunkPart{label: part.label + "a", depth: part.depth + 1, pieces: firstPieces, files: jevChunkFiles(firstPieces)}
	second = jevChunkPart{label: part.label + "b", depth: part.depth + 1, pieces: secondPieces, files: jevChunkFiles(secondPieces)}
	for _, half := range []jevChunkPart{first, second} {
		if _, err := in.build(half, total); err != nil {
			if errors.Is(err, errJevChunkOversize) {
				return first, second, false, nil
			}

			return first, second, false, err
		}
	}

	return first, second, true, nil
}

// jevChunkBytes returns the raw content bytes in a set of pieces.
func jevChunkBytes(pieces []jevChunkPiece) int64 {
	var n int64
	for _, pc := range pieces {
		n += pc.size
	}

	return n
}

// jevChunkTruncate cuts s to at most limit bytes at a rune boundary.
func jevChunkTruncate(s string, limit int) string {
	if len(s) <= limit {
		return s
	}

	cut := limit
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}

	return s[:cut]
}

// jevChunkListSkips returns at most jevChunkListLimit skipped files, each with its path cut to jevChunkPathLimit.
func jevChunkListSkips(skipped []jevChunkSkip) []jevChunkSkip {
	listed := make([]jevChunkSkip, 0, min(len(skipped), jevChunkListLimit))
	for i, s := range skipped {
		if i == jevChunkListLimit {
			break
		}

		listed = append(listed, jevChunkSkip{path: jevChunkTruncate(s.path, jevChunkPathLimit), reason: s.reason})
	}

	return listed
}
