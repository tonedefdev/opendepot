package agentassembly

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"testing"
)

const propertySeeds = 300

// randomDAG builds an agent graph whose delegates links only run from a lower index to a higher one, so it has no
// cycle. The entry agent is a0.
func randomDAG(r *rand.Rand) Document {
	n := 2 + r.IntN(11)
	density := 0.05 + r.Float64()*0.55
	modes := []string{ModeSubagent, ModeHandoff}
	contexts := []string{ContextTaskOnly, ContextSummary, ContextFullTranscript}

	doc := Document{Target: TargetCopilot}
	for i := range n {
		id := fmt.Sprintf("a%d", i)
		doc.Nodes = append(doc.Nodes, Node{ID: id, Kind: KindAgent, Ref: testRef(id), Entry: i == 0, Label: id})
	}

	for i := range n {
		for j := i + 1; j < n; j++ {
			p := density
			if i == 0 && r.IntN(3) == 0 {
				p = 1
			}

			if r.Float64() >= p {
				continue
			}

			doc.Links = append(doc.Links, Link{
				ID:      fmt.Sprintf("d%d-%d", i, j),
				From:    fmt.Sprintf("a%d", i),
				To:      fmt.Sprintf("a%d", j),
				Type:    LinkDelegates,
				Mode:    modes[r.IntN(len(modes))],
				Context: contexts[r.IntN(len(contexts))],
			})
		}
	}

	return doc
}

// delegateAdjacency returns the delegates links of doc as an adjacency list, independent of the package's graph.
func delegateAdjacency(doc Document) map[string][]string {
	adj := map[string][]string{}
	for _, l := range doc.Links {
		if l.Type == LinkDelegates {
			adj[l.From] = append(adj[l.From], l.To)
		}
	}

	return adj
}

// oracleReaches reports whether to can be reached from from through one or more delegates links.
func oracleReaches(adj map[string][]string, from, to string) bool {
	seen := map[string]bool{}
	stack := slices.Clone(adj[from])
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if cur == to {
			return true
		}

		if seen[cur] {
			continue
		}

		seen[cur] = true
		stack = append(stack, adj[cur]...)
	}

	return false
}

// oracleExpected returns the rule codes that a DAG from randomDAG must produce. Edges run from low to high index, so
// the longest path from a0 is computed in index order.
func oracleExpected(doc Document) []string {
	depth := map[string]int{"a0": 0}
	adj := delegateAdjacency(doc)
	for i := range len(doc.Nodes) {
		id := fmt.Sprintf("a%d", i)
		d, ok := depth[id]
		if !ok {
			continue
		}

		for _, next := range adj[id] {
			if cur, seen := depth[next]; !seen || d+1 > cur {
				depth[next] = d + 1
			}
		}
	}

	var want []string
	for _, d := range depth {
		if d > MaxDepth {
			want = append(want, CodeDepthLimit)

			break
		}
	}

	for _, targets := range adj {
		if len(targets) > MaxFanout {
			want = append(want, CodeFanoutLimit)

			break
		}
	}

	slices.Sort(want)

	return want
}

func distinctSorted(values []string) []string {
	out := slices.Clone(values)
	slices.Sort(out)

	return slices.Compact(out)
}

func TestPropertyRandomDAGMatchesOracle(t *testing.T) {
	var valid, depthHits, fanoutHits int
	for seed := range propertySeeds {
		r := rand.New(rand.NewPCG(uint64(seed), 0x0a55e7))
		doc := randomDAG(r)

		diags, err := Validate(doc, Options{})
		if err != nil {
			t.Fatalf("seed %d: structural error: %v", seed, err)
		}

		want := oracleExpected(doc)
		got := distinctSorted(codes(diags))
		if !slices.Equal(got, want) {
			t.Fatalf("seed %d: codes %v, oracle expects %v\n%+v", seed, got, want, diags)
		}

		if len(want) == 0 {
			valid++
		}

		if slices.Contains(want, CodeDepthLimit) {
			depthHits++
		}

		if slices.Contains(want, CodeFanoutLimit) {
			fanoutHits++
		}
	}

	if valid == 0 || depthHits == 0 || fanoutHits == 0 {
		t.Fatalf("generator did not cover all cases: valid=%d depth=%d fanout=%d",
			valid, depthHits, fanoutHits)
	}
}

func TestPropertyInjectedCycleIsRejected(t *testing.T) {
	for seed := range propertySeeds {
		r := rand.New(rand.NewPCG(uint64(seed), 0xc1c1e))
		doc := randomDAG(r)
		if len(doc.Links) == 0 {
			continue
		}

		link := doc.Links[r.IntN(len(doc.Links))]
		doc.Links = append(doc.Links, Link{
			ID:      "back-" + link.ID,
			From:    link.To,
			To:      link.From,
			Type:    LinkDelegates,
			Mode:    ModeSubagent,
			Context: ContextTaskOnly,
		})

		diags, err := Validate(doc, Options{})
		if err != nil {
			t.Fatalf("seed %d: structural error: %v", seed, err)
		}

		if !slices.Contains(codes(diags), CodeDelegateCycle) || !HasErrors(diags) {
			t.Fatalf("seed %d: injected cycle %q not rejected: %+v", seed, "back-"+link.ID, diags)
		}
	}
}

func TestPropertyRetryPlacement(t *testing.T) {
	var accepted, rejected int
	for seed := range propertySeeds {
		r := rand.New(rand.NewPCG(uint64(seed), 0x2e7f))
		doc := randomDAG(r)
		if len(doc.Nodes) < 2 {
			continue
		}

		from := fmt.Sprintf("a%d", r.IntN(len(doc.Nodes)))
		to := fmt.Sprintf("a%d", r.IntN(len(doc.Nodes)))
		if from == to {
			continue
		}

		adj := delegateAdjacency(doc)
		ancestor := oracleReaches(adj, to, from)
		doc.Links = append(doc.Links, Link{
			ID:            "retry-prop",
			From:          from,
			To:            to,
			Type:          LinkRetry,
			MaxIterations: MinRetryIterations + r.IntN(MaxRetryIterations),
			Until:         testUntil,
		})

		diags, err := Validate(doc, Options{})
		if err != nil {
			t.Fatalf("seed %d: structural error: %v", seed, err)
		}

		got := codes(diags)
		if slices.Contains(got, CodeRetryIterations) {
			t.Fatalf("seed %d: valid maxIterations rejected: %+v", seed, diags)
		}

		if ancestor == slices.Contains(got, CodeRetryNotAncestor) {
			t.Fatalf("seed %d: retry %s->%s ancestor=%v but codes=%v", seed, from, to, ancestor, got)
		}

		if slices.Contains(got, CodeDelegateCycle) || slices.Contains(got, CodeHandoffReturn) {
			t.Fatalf("seed %d: a retry link must not create a cycle or handoff return: %v", seed, got)
		}

		if ancestor {
			accepted++
		} else {
			rejected++
		}
	}

	if accepted == 0 || rejected == 0 {
		t.Fatalf("generator did not cover both retry placements: accepted=%d rejected=%d", accepted, rejected)
	}
}

func TestPropertyRetryIterationBounds(t *testing.T) {
	for _, n := range []int{-1, 0, MaxRetryIterations + 1, 1 << 20} {
		doc := baseDoc()
		findLink(&doc, testRetryID).MaxIterations = n

		diags := mustValidate(t, doc)
		if !slices.Contains(codes(diags), CodeRetryIterations) {
			t.Fatalf("maxIterations %d must be rejected: %+v", n, diags)
		}
	}
}

func TestPropertyDigestAndRenderAreStable(t *testing.T) {
	var rendered int
	for seed := range propertySeeds {
		r := rand.New(rand.NewPCG(uint64(seed), 0xd1635))
		doc := randomDAG(r)

		diags, err := Validate(doc, Options{})
		if err != nil {
			t.Fatalf("seed %d: structural error: %v", seed, err)
		}

		shuffled := shuffledCopy(doc, r)
		want := mustDigest(t, doc)
		if got := mustDigest(t, shuffled); got != want {
			t.Fatalf("seed %d: digest changed with ordering", seed)
		}

		mutated := doc
		mutated.Nodes = slices.Clone(doc.Nodes)
		mutated.Nodes[0].Label += " changed"
		if mustDigest(t, mutated) == want {
			t.Fatalf("seed %d: digest did not change with a label", seed)
		}

		if HasErrors(diags) {
			continue
		}

		first, err := Render(doc, TargetClaude)
		if err != nil {
			t.Fatalf("seed %d: valid document failed to render: %v", seed, err)
		}

		second, err := Render(shuffled, TargetClaude)
		if err != nil {
			t.Fatalf("seed %d: shuffled document failed to render: %v", seed, err)
		}

		if !slices.EqualFunc(first, second, func(a, b File) bool {
			return a.Path == b.Path && string(a.Content) == string(b.Content)
		}) {
			t.Fatalf("seed %d: render changed with ordering", seed)
		}

		rendered++
	}

	if rendered == 0 {
		t.Fatal("no valid random document was rendered")
	}
}

func shuffledCopy(doc Document, r *rand.Rand) Document {
	out := doc
	out.Nodes = slices.Clone(doc.Nodes)
	r.Shuffle(len(out.Nodes), func(i, j int) {
		out.Nodes[i], out.Nodes[j] = out.Nodes[j], out.Nodes[i]
	})

	out.Links = slices.Clone(doc.Links)
	r.Shuffle(len(out.Links), func(i, j int) {
		out.Links[i], out.Links[j] = out.Links[j], out.Links[i]
	})

	return out
}
