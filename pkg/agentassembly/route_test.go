package agentassembly

import (
	"strings"
	"testing"
)

const testOtherDigest = "sha256:0000000000000000000000000000000000000000000000000000000000000000"

func testManifest(t *testing.T, doc Document) AssemblyManifest {
	t.Helper()

	return AssemblyManifest{Digest: mustDigest(t, doc), Graph: doc}
}

func TestRouteAllowsDeclaredDelegation(t *testing.T) {
	m := testManifest(t, baseDoc())
	state := RouteState{}

	d := Route(m, RouteRequest{From: testPlanner, To: testWorker}, state)
	if !d.Allowed || d.LinkID != testDelegate || d.LinkType != LinkDelegates {
		t.Fatalf("delegation should be allowed, got %+v", d)
	}

	if next := state.Apply(d); len(next) != 0 {
		t.Fatalf("a delegation must not change retry state, got %v", next)
	}
}

func TestRouteRetryCountsUntilExhausted(t *testing.T) {
	m := testManifest(t, baseDoc())
	var state RouteState

	for want := 1; want <= 3; want++ {
		d := Route(m, RouteRequest{From: testWorker, To: testPlanner}, state)
		if !d.Allowed || d.Iteration != want || d.LinkType != LinkRetry {
			t.Fatalf("retry %d should be allowed, got %+v", want, d)
		}

		state = state.Apply(d)
	}

	d := Route(m, RouteRequest{From: testWorker, To: testPlanner}, state)
	if d.Allowed || d.Code != RouteCodeRetryExhausted {
		t.Fatalf("retry beyond maxIterations must be denied, got %+v", d)
	}

	if state[RouteKey(m.Digest, testRetryID)] != 3 {
		t.Fatalf("denied retry must not advance the counter, got %v", state)
	}
}

func TestRouteDeniesUndeclaredLinks(t *testing.T) {
	m := testManifest(t, baseDoc())
	addAgent(&m.Graph, testExtra)
	m.Digest = mustDigest(t, m.Graph)

	tests := []struct {
		name string
		req  RouteRequest
	}{
		{"skill is not a delegate", RouteRequest{From: testPlanner, To: testNotes}},
		{"agent with no link", RouteRequest{From: testWorker, To: testExtra}},
		{"delegation to an undeclared agent", RouteRequest{From: testPlanner, To: testExtra}},
		{"reverse delegation", RouteRequest{From: testWorker, To: testPlanner, LinkID: testDelegate}},
		{"link id that points elsewhere", RouteRequest{From: testPlanner, To: testExtra, LinkID: testDelegate}},
		{"link id of a uses link", RouteRequest{From: testPlanner, To: testNotes, LinkID: "uses-notes"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := Route(m, tt.req, nil)
			if d.Allowed || d.Code != RouteCodeLinkUndeclared {
				t.Fatalf("want deny with %s, got %+v", RouteCodeLinkUndeclared, d)
			}

			if d.Reason == "" {
				t.Fatal("a denial must give a reason")
			}
		})
	}
}

func TestRouteDeniesUnknownNodes(t *testing.T) {
	m := testManifest(t, baseDoc())

	for _, req := range []RouteRequest{
		{From: testGhost, To: testWorker},
		{From: testPlanner, To: testGhost},
	} {
		d := Route(m, req, nil)
		if d.Allowed || d.Code != RouteCodeNodeUnknown {
			t.Fatalf("want deny with %s for %+v, got %+v", RouteCodeNodeUnknown, req, d)
		}
	}
}

func TestRouteAllowsMatchingLinkID(t *testing.T) {
	m := testManifest(t, baseDoc())

	d := Route(m, RouteRequest{From: testPlanner, To: testWorker, LinkID: testDelegate}, nil)
	if !d.Allowed {
		t.Fatalf("matching link id should be allowed, got %+v", d)
	}
}

func TestRouteDeniesDigestMismatch(t *testing.T) {
	m := testManifest(t, baseDoc())
	m.Graph.Target = TargetClaude

	d := Route(m, RouteRequest{From: testPlanner, To: testWorker}, nil)
	if d.Allowed || d.Code != RouteCodeDigestMismatch {
		t.Fatalf("a changed graph with the old digest must be denied, got %+v", d)
	}
}

func TestRouteDeniesInvalidGraph(t *testing.T) {
	doc := baseDoc()
	findNode(&doc, testNotes).Placeholder = &Placeholder{Description: testSample}
	doc.Tutorial = true

	d := Route(testManifest(t, doc), RouteRequest{From: testPlanner, To: testWorker}, nil)
	if d.Allowed || d.Code != RouteCodeGraphInvalid {
		t.Fatalf("a placeholder graph must be denied, got %+v", d)
	}
}

func TestRouteCountersAreKeyedByDigest(t *testing.T) {
	m := testManifest(t, baseDoc())

	state := RouteState{RouteKey(testOtherDigest, testRetryID): 3}
	d := Route(m, RouteRequest{From: testWorker, To: testPlanner}, state)
	if !d.Allowed || d.Iteration != 1 {
		t.Fatalf("counters from another digest must not apply, got %+v", d)
	}
}

func TestRouteApplyAllocatesNilState(t *testing.T) {
	m := testManifest(t, baseDoc())

	d := Route(m, RouteRequest{From: testWorker, To: testPlanner}, nil)
	state := RouteState(nil).Apply(d)

	if got := state[RouteKey(m.Digest, testRetryID)]; got != 1 {
		t.Fatalf("want counter 1, got %d", got)
	}
}

func TestRouteDenialReasonNamesTheLink(t *testing.T) {
	m := testManifest(t, baseDoc())
	state := RouteState{RouteKey(m.Digest, testRetryID): 3}

	d := Route(m, RouteRequest{From: testWorker, To: testPlanner}, state)
	if !strings.Contains(d.Reason, testRetryID) {
		t.Fatalf("reason should name the link, got %q", d.Reason)
	}
}
