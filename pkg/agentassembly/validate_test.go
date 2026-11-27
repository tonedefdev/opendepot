package agentassembly

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

const (
	testNamespace = "acme"
	testVersion   = "1.0.0"
	testPlanner   = "planner"
	testWorker    = "worker"
	testNotes     = "notes"
	testGitHub    = "github"
	testExtra     = "extra"
	testGroup     = "research"
	testUntil     = "tests pass"
	testRetryID   = "retry-planner"
	testDelegate  = "delegate-worker"
	testTool      = "list_issues"
	testSample    = "sample"
	testGhost     = "ghost"
)

// TestStructureRejectsInjectionBeforeRendering checks that a namespace, name, version, or tool carrying a newline,
// comma, hash, or control character is refused by the structural check that runs before any rule or render.
func TestStructureRejectsInjectionBeforeRendering(t *testing.T) {
	setPlannerRef := func(mutate func(*Ref)) func(*Document) {
		return func(d *Document) {
			mutate(&findNode(d, testPlanner).Ref)
		}
	}

	setTools := func(tools ...string) func(*Document) {
		return func(d *Document) {
			findLink(d, "calls-github").Tools = tools
		}
	}

	tests := []struct {
		name   string
		mutate func(*Document)
	}{
		{"namespace with newline", setPlannerRef(func(r *Ref) { r.Namespace = "acme\nsource = \"x\"" })},
		{"namespace with hash", setPlannerRef(func(r *Ref) { r.Namespace = "acme#evil" })},
		{"namespace with comma", setPlannerRef(func(r *Ref) { r.Namespace = "acme,evil" })},
		{"name with newline", setPlannerRef(func(r *Ref) { r.Name = "planner\n}" })},
		{"name with hash", setPlannerRef(func(r *Ref) { r.Name = "planner#evil" })},
		{"name with comma", setPlannerRef(func(r *Ref) { r.Name = "planner,evil" })},
		{"name with control byte", setPlannerRef(func(r *Ref) { r.Name = "planner\x00" })},
		{"version with newline", setPlannerRef(func(r *Ref) { r.Version = "1.0.0\nsource = \"x\"" })},
		{"version with hash", setPlannerRef(func(r *Ref) { r.Version = "1.0.0 # pinned" })},
		{"version with comma", setPlannerRef(func(r *Ref) { r.Version = "1.0.0, 2.0.0" })},
		{"version with control byte", setPlannerRef(func(r *Ref) { r.Version = "1.0.0\x07" })},
		{"tool with newline", setTools("list_issues\nevil")},
		{"tool with hash", setTools("list#issues")},
		{"tool with comma", setTools("list_issues,get_pull_request")},
		{"tool with space", setTools("list issues")},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			doc := baseDoc()
			tc.mutate(&doc)

			if _, err := Validate(doc, Options{}); !errors.Is(err, ErrInvalidDocument) {
				t.Fatalf("Validate() error = %v, want ErrInvalidDocument", err)
			}
		})
	}

	t.Run("pre-export constraint is still a valid document", func(t *testing.T) {
		doc := baseDoc()
		setPlannerRef(func(r *Ref) { r.Version = "~> 2.0" })(&doc)

		if _, err := Validate(doc, Options{}); errors.Is(err, ErrInvalidDocument) {
			t.Fatalf("Validate() rejected a constraint that export is expected to pin: %v", err)
		}
	})
}

func testRef(name string) Ref {
	return Ref{Namespace: testNamespace, Name: name, Version: testVersion}
}

// baseDoc is a valid assembly: planner uses notes, the worker calls GitHub, planner delegates to worker, and the
// worker retries planner.
func baseDoc() Document {
	return Document{
		Target: TargetCopilot,
		Nodes: []Node{
			{ID: testPlanner, Kind: KindAgent, Ref: testRef(testPlanner), Entry: true, Label: "Planner"},
			{ID: testWorker, Kind: KindAgent, Ref: testRef(testWorker), Label: "Worker"},
			{ID: testNotes, Kind: KindSkill, Ref: testRef(testNotes), Label: "Notes"},
			{ID: testGitHub, Kind: KindMCPServer, Ref: testRef(testGitHub), Label: "GitHub"},
		},
		Links: []Link{
			{ID: "uses-notes", From: testPlanner, To: testNotes, Type: LinkUses},
			{ID: "calls-github", From: testWorker, To: testGitHub, Type: LinkCalls, Tools: []string{testTool}},
			{ID: testDelegate, From: testPlanner, To: testWorker, Type: LinkDelegates,
				Mode: ModeSubagent, Context: ContextSummary},
			{ID: testRetryID, From: testWorker, To: testPlanner, Type: LinkRetry, MaxIterations: 3, Until: testUntil},
		},
	}
}

func addAgent(d *Document, id string) {
	d.Nodes = append(d.Nodes, Node{ID: id, Kind: KindAgent, Ref: testRef(id), Label: id})
}

func findLink(d *Document, id string) *Link {
	for i := range d.Links {
		if d.Links[i].ID == id {
			return &d.Links[i]
		}
	}

	return nil
}

func findNode(d *Document, id string) *Node {
	for i := range d.Nodes {
		if d.Nodes[i].ID == id {
			return &d.Nodes[i]
		}
	}

	return nil
}

func codes(diags []Diagnostic) []string {
	out := make([]string, 0, len(diags))
	for _, d := range diags {
		out = append(out, d.Code)
	}

	return out
}

func mustValidate(t *testing.T, doc Document) []Diagnostic {
	t.Helper()

	diags, err := Validate(doc, Options{})
	if err != nil {
		t.Fatalf("Validate returned structural error: %v", err)
	}

	return diags
}

func TestValidateBaseDocumentIsValid(t *testing.T) {
	diags := mustValidate(t, baseDoc())
	if len(diags) != 0 {
		t.Fatalf("base document has diagnostics: %+v", diags)
	}
}

func TestValidateRuleCodes(t *testing.T) {
	tests := []struct {
		name   string
		code   string
		mutate func(d *Document)
	}{
		{
			name: "entry_count with two entries",
			code: CodeEntryCount,
			mutate: func(d *Document) {
				findNode(d, testWorker).Entry = true
			},
		},
		{
			name: "entry_count with no entry",
			code: CodeEntryCount,
			mutate: func(d *Document) {
				findNode(d, testPlanner).Entry = false
			},
		},
		{
			name: "entry_count when the entry is not an agent",
			code: CodeEntryCount,
			mutate: func(d *Document) {
				findNode(d, testPlanner).Entry = false
				findNode(d, testNotes).Entry = true
			},
		},
		{
			name: "delegate_cycle",
			code: CodeDelegateCycle,
			mutate: func(d *Document) {
				d.Links = append(d.Links, Link{ID: "back-cycle", From: testWorker, To: testPlanner,
					Type: LinkDelegates, Mode: ModeSubagent, Context: ContextTaskOnly})
			},
		},
		{
			name: "delegate_cycle from a self loop",
			code: CodeDelegateCycle,
			mutate: func(d *Document) {
				d.Links = append(d.Links, Link{ID: "self", From: testWorker, To: testWorker,
					Type: LinkDelegates, Mode: ModeSubagent, Context: ContextTaskOnly})
			},
		},
		{
			name: "retry_not_ancestor when target cannot reach the source",
			code: CodeRetryNotAncestor,
			mutate: func(d *Document) {
				link := findLink(d, testRetryID)
				link.From, link.To = testPlanner, testWorker
			},
		},
		{
			name: "retry_not_ancestor for a self retry",
			code: CodeRetryNotAncestor,
			mutate: func(d *Document) {
				link := findLink(d, testRetryID)
				link.To = testWorker
			},
		},
		{
			name: "retry_iterations above the limit",
			code: CodeRetryIterations,
			mutate: func(d *Document) {
				findLink(d, testRetryID).MaxIterations = 11
			},
		},
		{
			name: "retry_iterations of zero",
			code: CodeRetryIterations,
			mutate: func(d *Document) {
				findLink(d, testRetryID).MaxIterations = 0
			},
		},
		{
			name: "handoff_return when a handoff target can return to an ancestor",
			code: CodeHandoffReturn,
			mutate: func(d *Document) {
				findLink(d, testDelegate).Mode = ModeHandoff
				d.Links = append(d.Links, Link{ID: "back-handoff", From: testWorker, To: testPlanner,
					Type: LinkDelegates, Mode: ModeSubagent, Context: ContextTaskOnly})
			},
		},
		{
			name: "leaf_outgoing from a skill",
			code: CodeLeafOutgoing,
			mutate: func(d *Document) {
				d.Links = append(d.Links, Link{ID: "leaf", From: testNotes, To: testGitHub, Type: LinkCalls})
			},
		},
		{
			name: "link_kind for calls to a skill",
			code: CodeLinkKind,
			mutate: func(d *Document) {
				d.Links = append(d.Links, Link{ID: "bad-calls", From: testWorker, To: testNotes, Type: LinkCalls})
			},
		},
		{
			name: "link_kind for uses to an MCP server",
			code: CodeLinkKind,
			mutate: func(d *Document) {
				d.Links = append(d.Links, Link{ID: "bad-uses", From: testWorker, To: testGitHub, Type: LinkUses})
			},
		},
		{
			name: "link_kind for delegates to a skill",
			code: CodeLinkKind,
			mutate: func(d *Document) {
				d.Links = append(d.Links, Link{ID: "bad-delegate", From: testPlanner, To: testNotes,
					Type: LinkDelegates, Mode: ModeSubagent, Context: ContextTaskOnly})
			},
		},
		{
			name: "depth_limit with a chain of six delegation levels",
			code: CodeDepthLimit,
			mutate: func(d *Document) {
				addChain(d, testWorker, 6)
			},
		},
		{
			name: "fanout_limit with nine delegates links",
			code: CodeFanoutLimit,
			mutate: func(d *Document) {
				for i := range 8 {
					id := "fan-" + string(rune('a'+i))
					addAgent(d, id)
					d.Links = append(d.Links, Link{ID: "d-" + id, From: testPlanner, To: id,
						Type: LinkDelegates, Mode: ModeSubagent, Context: ContextTaskOnly})
				}
			},
		},
		{
			name: "parallel_group with a link from another node",
			code: CodeParallelGroup,
			mutate: func(d *Document) {
				addAgent(d, testExtra)
				link := findLink(d, testDelegate)
				link.Mode, link.Group = ModeParallel, testGroup
				d.Links = append(d.Links, Link{ID: "p2", From: testWorker, To: testExtra,
					Type: LinkDelegates, Mode: ModeParallel, Context: ContextTaskOnly, Group: testGroup})
			},
		},
		{
			name: "parallel_group with a link that is not parallel",
			code: CodeParallelGroup,
			mutate: func(d *Document) {
				addAgent(d, testExtra)
				findLink(d, testDelegate).Group = testGroup
				d.Links = append(d.Links, Link{ID: "p2", From: testPlanner, To: testExtra,
					Type: LinkDelegates, Mode: ModeSubagent, Context: ContextTaskOnly, Group: testGroup})
			},
		},
		{
			name: "context_outputs when outputs context has no outputs",
			code: CodeContextOutputs,
			mutate: func(d *Document) {
				findLink(d, testDelegate).Context = ContextOutputs
			},
		},
		{
			name: "context_outputs when outputs are set on another context",
			code: CodeContextOutputs,
			mutate: func(d *Document) {
				findLink(d, testDelegate).Outputs = []string{"findings"}
			},
		},
		{
			name: "placeholder_not_exportable outside tutorial mode",
			code: CodePlaceholderNotExportable,
			mutate: func(d *Document) {
				findNode(d, testNotes).Placeholder = &Placeholder{Description: testSample}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := baseDoc()
			tt.mutate(&doc)

			diags := mustValidate(t, doc)
			if !slices.Contains(codes(diags), tt.code) {
				t.Fatalf("want code %q, got %+v", tt.code, diags)
			}

			if !HasErrors(diags) {
				t.Fatalf("want an error diagnostic, got %+v", diags)
			}
		})
	}
}

// addChain adds agents below from so that the deepest one is levels delegation levels away from the entry agent.
func addChain(d *Document, from string, levels int) {
	prev := from
	for i := 1; i <= levels; i++ {
		id := "chain-" + string(rune('a'+i))
		addAgent(d, id)
		d.Links = append(d.Links, Link{ID: "chain-link-" + id, From: prev, To: id,
			Type: LinkDelegates, Mode: ModeSubagent, Context: ContextTaskOnly})
		prev = id
	}
}

func TestValidateDepthLimitAllowsFiveLevels(t *testing.T) {
	doc := baseDoc()
	addChain(&doc, testPlanner, MaxDepth)

	diags := mustValidate(t, doc)
	if slices.Contains(codes(diags), CodeDepthLimit) {
		t.Fatalf("five levels should be allowed, got %+v", diags)
	}
}

func TestValidateFanoutLimitAllowsEightLinks(t *testing.T) {
	doc := baseDoc()
	for i := range 7 {
		id := "fan-" + string(rune('a'+i))
		addAgent(&doc, id)
		doc.Links = append(doc.Links, Link{ID: "d-" + id, From: testPlanner, To: id,
			Type: LinkDelegates, Mode: ModeSubagent, Context: ContextTaskOnly})
	}

	diags := mustValidate(t, doc)
	if slices.Contains(codes(diags), CodeFanoutLimit) {
		t.Fatalf("eight delegates links should be allowed, got %+v", diags)
	}
}

func TestValidateRetryAcceptsBoundaryIterations(t *testing.T) {
	for _, n := range []int{MinRetryIterations, MaxRetryIterations} {
		doc := baseDoc()
		findLink(&doc, testRetryID).MaxIterations = n

		diags := mustValidate(t, doc)
		if slices.Contains(codes(diags), CodeRetryIterations) {
			t.Fatalf("maxIterations %d should be allowed, got %+v", n, diags)
		}
	}
}

func TestValidateHandoffWithoutCycleIsAllowed(t *testing.T) {
	doc := baseDoc()
	findLink(&doc, testDelegate).Mode = ModeHandoff

	diags := mustValidate(t, doc)
	if slices.Contains(codes(diags), CodeHandoffReturn) {
		t.Fatalf("a plain handoff should be allowed, got %+v", diags)
	}
}

func TestValidateRiskFullTranscriptWarns(t *testing.T) {
	doc := baseDoc()
	findLink(&doc, testDelegate).Context = ContextFullTranscript

	diags, err := Validate(doc, Options{FlaggedMCPServers: map[string]bool{RefKey(testNamespace, testGitHub): true}})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}

	var warning *Diagnostic
	for i := range diags {
		if diags[i].Code == CodeRiskFullTranscript {
			warning = &diags[i]
		}
	}

	if warning == nil {
		t.Fatalf("want %s, got %+v", CodeRiskFullTranscript, diags)
	}

	if warning.Severity != SeverityWarning {
		t.Fatalf("want severity %q, got %q", SeverityWarning, warning.Severity)
	}

	if HasErrors(diags) {
		t.Fatalf("a warning must not block the document: %+v", diags)
	}
}

func TestValidateRiskFullTranscriptNeedsFlag(t *testing.T) {
	doc := baseDoc()
	findLink(&doc, testDelegate).Context = ContextFullTranscript

	diags := mustValidate(t, doc)
	if slices.Contains(codes(diags), CodeRiskFullTranscript) {
		t.Fatalf("an unflagged server must not warn, got %+v", diags)
	}
}

func TestValidatePlaceholderAllowedInTutorial(t *testing.T) {
	doc := baseDoc()
	doc.Tutorial = true
	findNode(&doc, testNotes).Placeholder = &Placeholder{Description: testSample}

	diags := mustValidate(t, doc)
	if len(diags) != 0 {
		t.Fatalf("a tutorial placeholder should validate, got %+v", diags)
	}

	exportDiags, err := ValidateExport(doc, Options{})
	if err != nil {
		t.Fatalf("ValidateExport: %v", err)
	}

	if !slices.Contains(codes(exportDiags), CodePlaceholderNotExportable) || !HasErrors(exportDiags) {
		t.Fatalf("a tutorial placeholder must block export, got %+v", exportDiags)
	}
}

func TestValidateExportAcceptsCleanDocument(t *testing.T) {
	diags, err := ValidateExport(baseDoc(), Options{})
	if err != nil {
		t.Fatalf("ValidateExport: %v", err)
	}

	if HasErrors(diags) {
		t.Fatalf("clean document should export, got %+v", diags)
	}
}

func TestRefUnresolvedDiagnostic(t *testing.T) {
	d := RefUnresolved(testWorker, "hidden from caller")
	if d.Code != CodeRefUnresolved || d.Severity != SeverityError || d.NodeID != testWorker {
		t.Fatalf("unexpected diagnostic: %+v", d)
	}

	if !strings.Contains(d.Message, "hidden from caller") {
		t.Fatalf("message should carry the reason: %q", d.Message)
	}

	if !HasErrors([]Diagnostic{d}) {
		t.Fatal("ref_unresolved must count as an error")
	}
}

func TestValidateStructuralErrors(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(d *Document)
	}{
		{"unsupported target", func(d *Document) { d.Target = "vscode" }},
		{"bad node id", func(d *Document) { findNode(d, testWorker).ID = "Worker" }},
		{"duplicate node id", func(d *Document) { addAgent(d, testWorker) }},
		{"unknown node kind", func(d *Document) { findNode(d, testNotes).Kind = "tool" }},
		{"missing ref name", func(d *Document) { findNode(d, testNotes).Ref.Name = "" }},
		{"empty link id", func(d *Document) { findLink(d, "uses-notes").ID = "" }},
		{"duplicate link id", func(d *Document) { findLink(d, "uses-notes").ID = "calls-github" }},
		{"link to unknown node", func(d *Document) { findLink(d, "uses-notes").To = testGhost }},
		{"unknown link type", func(d *Document) { findLink(d, "uses-notes").Type = "owns" }},
		{"tools on a delegates link", func(d *Document) {
			findLink(d, testDelegate).Tools = []string{"x"}
		}},
		{"delegates without mode", func(d *Document) { findLink(d, testDelegate).Mode = "" }},
		{"delegates with unknown context", func(d *Document) {
			findLink(d, testDelegate).Context = "everything"
		}},
		{"retry without until", func(d *Document) { findLink(d, testRetryID).Until = "" }},
		{"retry until longer than 512 characters", func(d *Document) {
			findLink(d, testRetryID).Until = strings.Repeat("a", MaxUntilRunes+1)
		}},
		{"retry fields on a uses link", func(d *Document) {
			findLink(d, "uses-notes").MaxIterations = 2
		}},
		{"decision on a uses link", func(d *Document) {
			findLink(d, "uses-notes").Decision = &Decision{DecidedBy: DecidedByUser}
		}},
		{"decision with unknown decidedBy", func(d *Document) {
			findLink(d, testDelegate).Decision = &Decision{DecidedBy: "robot"}
		}},
		{"decision probability above one", func(d *Document) {
			findLink(d, testDelegate).Decision = &Decision{
				DecidedBy:     DecidedByJev,
				Probabilities: Probabilities{Subagent: 1.5},
			}
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := baseDoc()
			tt.mutate(&doc)

			_, err := Validate(doc, Options{})
			if !errors.Is(err, ErrInvalidDocument) {
				t.Fatalf("want ErrInvalidDocument, got %v", err)
			}
		})
	}
}

func TestDiagnosticsAreDeterministic(t *testing.T) {
	doc := baseDoc()
	addAgent(&doc, testExtra)
	doc.Links = append(doc.Links,
		Link{ID: "back-diag", From: testWorker, To: testPlanner, Type: LinkDelegates,
			Mode: ModeHandoff, Context: ContextTaskOnly},
		Link{ID: "bad-calls", From: testExtra, To: testNotes, Type: LinkCalls})

	first := mustValidate(t, doc)
	for range 5 {
		next := mustValidate(t, doc)
		if !slices.Equal(codes(first), codes(next)) {
			t.Fatalf("diagnostics changed between runs: %v vs %v", codes(first), codes(next))
		}
	}
}
