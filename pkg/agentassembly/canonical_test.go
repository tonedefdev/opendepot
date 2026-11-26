package agentassembly

import (
	"regexp"
	"slices"
	"testing"
	"time"
)

var digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

func TestDigestFormat(t *testing.T) {
	digest, err := Digest(baseDoc())
	if err != nil {
		t.Fatalf("Digest: %v", err)
	}

	if !digestPattern.MatchString(digest) {
		t.Fatalf("digest %q does not match sha256:<64 hex>", digest)
	}
}

func TestDigestIgnoresOrdering(t *testing.T) {
	doc := baseDoc()
	addAgent(&doc, testExtra)
	doc.Links = append(doc.Links, Link{ID: "tool-two", From: testWorker, To: testGitHub, Type: LinkCalls,
		Tools: []string{"get_issue", testTool}})
	findLink(&doc, "calls-github").Tools = []string{testTool, "get_issue"}

	want := mustDigest(t, doc)

	shuffled := doc
	shuffled.Nodes = slices.Clone(doc.Nodes)
	slices.Reverse(shuffled.Nodes)
	shuffled.Links = slices.Clone(doc.Links)
	slices.Reverse(shuffled.Links)
	for i := range shuffled.Links {
		shuffled.Links[i].Tools = slices.Clone(shuffled.Links[i].Tools)
		slices.Reverse(shuffled.Links[i].Tools)
	}

	if got := mustDigest(t, shuffled); got != want {
		t.Fatalf("digest changed with ordering: %s != %s", got, want)
	}
}

func TestDigestIgnoresNilAndEmptySlices(t *testing.T) {
	doc := baseDoc()
	doc.Links = slices.Clone(doc.Links)
	doc.Links[0].Tools = []string{}

	empty := baseDoc()
	if mustDigest(t, doc) != mustDigest(t, empty) {
		t.Fatal("empty and nil slices must produce the same digest")
	}

	noNodes := Document{Target: TargetCopilot, Links: []Link{}}
	nilNodes := Document{Target: TargetCopilot}
	if mustDigest(t, noNodes) != mustDigest(t, nilNodes) {
		t.Fatal("nil and empty node lists must produce the same digest")
	}
}

func TestDigestChangesWithContent(t *testing.T) {
	want := mustDigest(t, baseDoc())

	changes := map[string]func(d *Document){
		"label":          func(d *Document) { findNode(d, testWorker).Label = "Other" },
		"max iterations": func(d *Document) { findLink(d, testRetryID).MaxIterations = 4 },
		"tools":          func(d *Document) { findLink(d, "calls-github").Tools = []string{"other"} },
		"target":         func(d *Document) { d.Target = TargetClaude },
		"tutorial":       func(d *Document) { d.Tutorial = true },
		"decision": func(d *Document) {
			findLink(d, testDelegate).Decision = &Decision{DecidedBy: DecidedByJev, Model: "m"}
		},
	}

	for name, mutate := range changes {
		doc := baseDoc()
		mutate(&doc)

		if mustDigest(t, doc) == want {
			t.Errorf("digest did not change with %s", name)
		}
	}
}

func TestDigestNormalizesTimeZone(t *testing.T) {
	when := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	local := when.In(time.FixedZone("UTC-8", -8*60*60))

	a := baseDoc()
	findLink(&a, testDelegate).Decision = &Decision{DecidedBy: DecidedByJev, DecidedAt: when}

	b := baseDoc()
	findLink(&b, testDelegate).Decision = &Decision{DecidedBy: DecidedByJev, DecidedAt: local}

	if mustDigest(t, a) != mustDigest(t, b) {
		t.Fatal("the same instant in two zones must produce the same digest")
	}
}

func mustDigest(t *testing.T, doc Document) string {
	t.Helper()

	digest, err := Digest(doc)
	if err != nil {
		t.Fatalf("Digest: %v", err)
	}

	return digest
}
