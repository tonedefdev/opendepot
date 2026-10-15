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
	"reflect"
	"strings"
	"testing"

	"github.com/tonedefdev/opendepot/pkg/jev"
)

// jevEvidenceTestScript returns a script whose excerpts each cost about 5 KB. It holds many single-signal excerpts
// followed by one excerpt that matches three signals, so the view cannot hold them all.
func jevEvidenceTestScript() string {
	filler := strings.Repeat("b", 450)

	var script strings.Builder
	for i := range 40 {
		fmt.Fprintf(&script, "os.system('curl %d') %s\n", i, filler)
		for range 20 {
			script.WriteString(filler + "\n")
		}
	}

	script.WriteString("token = os.environ['TOKEN']\n")
	script.WriteString("requests.post(url, data=token)\n")
	return script.String()
}

func TestJevEvidenceSelectsSignalExcerpts(t *testing.T) {
	var script strings.Builder
	for i := 1; i <= 60; i++ {
		switch i {
		case 20:
			script.WriteString("data = os.environ['TOKEN']\n")
		case 22:
			script.WriteString("requests.post(url, data=data)\n")
		default:
			fmt.Fprintf(&script, "x%d = %d\n", i, i)
		}
	}

	in := jevChunkTestInput(t, map[string]string{"scripts/run.py": script.String()})
	part, ok, err := jevEvidencePart(in)
	if err != nil {
		t.Fatalf("evidence part: %v", err)
	}

	if !ok {
		t.Fatal("expected an evidence part for matching lines")
	}

	if !part.evidence || part.label != jevEvidenceLabel {
		t.Fatalf("part = evidence %v label %q, want evidence part labelled %q", part.evidence, part.label, jevEvidenceLabel)
	}

	body := part.pieces[0].text
	// Lines 20 and 22 each keep five lines of context, and the two excerpts overlap, so they merge into lines 15-27.
	if !strings.Contains(body, "== scripts/run.py, lines 15-27 ==") {
		t.Fatalf("merged excerpt header missing:\n%s", body)
	}

	if !strings.Contains(body, "20: data = os.environ['TOKEN']") || !strings.Contains(body, "22: requests.post(url, data=data)") {
		t.Fatalf("matched lines missing with their line numbers:\n%s", body)
	}

	if strings.Contains(body, "x1 = 1") {
		t.Fatalf("line outside the context was included:\n%s", body)
	}

	if !reflect.DeepEqual(part.files, []string{"scripts/run.py"}) {
		t.Fatalf("files = %v, want [scripts/run.py]", part.files)
	}
}

func TestJevEvidenceNoSignalNoPart(t *testing.T) {
	in := jevChunkTestInput(t, map[string]string{"scripts/run.py": "print('hello')\n"})
	_, ok, err := jevEvidencePart(in)
	if err != nil {
		t.Fatalf("evidence part: %v", err)
	}

	if ok {
		t.Fatal("expected no evidence part when no line matches a signal")
	}
}

func TestJevEvidenceKeepsMultiSignalExcerptWhenFull(t *testing.T) {
	in := jevChunkTestInput(t, map[string]string{"scripts/run.py": jevEvidenceTestScript()})
	part, ok, err := jevEvidencePart(in)
	if err != nil {
		t.Fatalf("evidence part: %v", err)
	}

	if !ok {
		t.Fatal("expected an evidence part")
	}

	body := part.pieces[0].text
	if !strings.Contains(body, "token = os.environ['TOKEN']") || !strings.Contains(body, "requests.post(url, data=token)") {
		t.Fatalf("multi-signal excerpt was dropped from a full view:\n%.300s", body)
	}

	if !strings.Contains(body, "more excerpt(s) matched but were omitted") {
		t.Fatal("expected an omission note when excerpts do not fit")
	}
}

func TestJevEvidenceBuildWithinLimit(t *testing.T) {
	in := jevChunkTestInput(t, map[string]string{"scripts/run.py": jevEvidenceTestScript()})
	part, ok, err := jevEvidencePart(in)
	if err != nil {
		t.Fatalf("evidence part: %v", err)
	}

	if !ok {
		t.Fatal("expected an evidence part")
	}

	state, err := in.build(part, 2)
	if err != nil {
		t.Fatalf("build evidence part: %v", err)
	}

	if len(state) > jev.MaxStateBytes {
		t.Fatalf("evidence part is %d bytes, over %d", len(state), jev.MaxStateBytes)
	}

	if !strings.Contains(state, "evidence view") {
		t.Fatalf("preamble does not name the evidence view:\n%.200s", state)
	}

	if strings.Contains(state, "The entry file and the full bundled file list") {
		t.Fatalf("evidence preamble uses a numbered part or entry note:\n%.300s", state)
	}
}

func TestJevEvidenceIsNotHalved(t *testing.T) {
	in := jevChunkTestInput(t, map[string]string{"scripts/run.py": "requests.post(url)\n"})
	part, ok, err := jevEvidencePart(in)
	if err != nil {
		t.Fatalf("evidence part: %v", err)
	}

	if !ok {
		t.Fatal("expected an evidence part")
	}

	if _, _, halved, err := in.halve(part, 2); err != nil || halved {
		t.Fatalf("halve evidence part: halved %v, err %v; want not halved", halved, err)
	}
}
