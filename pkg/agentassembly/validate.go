package agentassembly

import (
	"fmt"
	"slices"
	"strings"
)

// Validate checks doc against the validation rules. It returns the rule violations and warnings as Diagnostics. The
// error is non-nil only when doc is malformed, which is a different failure from a rule violation. Callers reject
// the document when HasErrors reports true.
//
// Validate allows placeholder nodes only in tutorial documents. ValidateExport also rejects them in tutorials.
func Validate(doc Document, opts Options) ([]Diagnostic, error) {
	if err := checkStructure(doc); err != nil {
		return nil, err
	}

	g := newGraph(doc)

	diags := make([]Diagnostic, 0, 16)
	diags = append(diags, g.checkEntry()...)
	diags = append(diags, g.checkDelegateCycles()...)
	diags = append(diags, g.checkRetries()...)
	diags = append(diags, g.checkHandoffs()...)
	diags = append(diags, g.checkLeaves()...)
	diags = append(diags, g.checkLinkKinds()...)
	diags = append(diags, g.checkDepth()...)
	diags = append(diags, g.checkFanout()...)
	diags = append(diags, g.checkParallelGroups()...)
	diags = append(diags, g.checkContextOutputs()...)
	diags = append(diags, g.checkPlaceholders()...)
	diags = append(diags, g.checkFullTranscripts(opts)...)

	return diags, nil
}

// ValidateExport runs Validate and also rejects placeholder nodes in tutorial documents. A placeholder always blocks
// export, so Render and RenderHCL refuse any document that has one.
func ValidateExport(doc Document, opts Options) ([]Diagnostic, error) {
	diags, err := Validate(doc, opts)
	if err != nil {
		return nil, err
	}

	if doc.Tutorial {
		for _, n := range doc.Nodes {
			if n.Placeholder != nil {
				diags = append(diags, errorDiag(CodePlaceholderNotExportable, n.ID, "",
					"placeholder node %q cannot be exported", n.ID))
			}
		}
	}

	return diags, nil
}

// graph is the indexed view of a structurally valid document that the rules read.
type graph struct {
	doc       Document
	nodes     map[string]Node
	ids       []string
	links     []Link
	out       map[string][]Link
	delegates map[string][]Link
	delAdj    map[string][]string
	delRev    map[string][]string
	entry     string
}

func newGraph(doc Document) *graph {
	g := &graph{
		doc:       doc,
		nodes:     make(map[string]Node, len(doc.Nodes)),
		out:       map[string][]Link{},
		delegates: map[string][]Link{},
		delAdj:    map[string][]string{},
		delRev:    map[string][]string{},
	}

	for _, n := range doc.Nodes {
		g.nodes[n.ID] = n
		g.ids = append(g.ids, n.ID)
	}
	slices.Sort(g.ids)

	g.links = slices.Clone(doc.Links)
	slices.SortFunc(g.links, func(a, b Link) int {
		return strings.Compare(a.ID, b.ID)
	})

	for _, l := range g.links {
		g.out[l.From] = append(g.out[l.From], l)
		if l.Type != LinkDelegates {
			continue
		}

		g.delegates[l.From] = append(g.delegates[l.From], l)
		g.delAdj[l.From] = append(g.delAdj[l.From], l.To)
		g.delRev[l.To] = append(g.delRev[l.To], l.From)
	}

	g.entry = g.entryID()

	return g
}

// entryID returns the ID of the entry agent, or "" when the document does not have exactly one entry agent.
func (g *graph) entryID() string {
	entries := g.entries()
	if len(entries) != 1 || g.nodes[entries[0]].Kind != KindAgent {
		return ""
	}

	return entries[0]
}

func (g *graph) entries() []string {
	var entries []string
	for _, id := range g.ids {
		if g.nodes[id].Entry {
			entries = append(entries, id)
		}
	}

	return entries
}

// reachable returns the nodes that are reachable from start by following adj through one or more edges. start is
// included only when a path leads back to it.
func reachable(adj map[string][]string, start string) map[string]bool {
	seen := map[string]bool{}
	queue := []string{start}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, next := range adj[cur] {
			if seen[next] {
				continue
			}

			seen[next] = true
			queue = append(queue, next)
		}
	}

	return seen
}

func (g *graph) checkEntry() []Diagnostic {
	entries := g.entries()
	if len(entries) != 1 {
		return []Diagnostic{errorDiag(CodeEntryCount, "", "",
			"exactly one entry agent is required, found %d", len(entries))}
	}

	if g.nodes[entries[0]].Kind != KindAgent {
		return []Diagnostic{errorDiag(CodeEntryCount, entries[0], "",
			"entry node %q must be an agent", entries[0])}
	}

	return nil
}

// checkDelegateCycles walks the delegates links depth first. A link that reaches a node still on the walk closes a
// cycle. Retry links are not walked, because they are the one allowed back edge.
func (g *graph) checkDelegateCycles() []Diagnostic {
	const (
		unvisited = iota
		onStack
		done
	)

	state := map[string]int{}
	var diags []Diagnostic

	var visit func(id string)
	visit = func(id string) {
		state[id] = onStack
		for _, l := range g.delegates[id] {
			switch state[l.To] {
			case onStack:
				diags = append(diags, errorDiag(CodeDelegateCycle, "", l.ID,
					"delegates link %q closes a cycle back to %q", l.ID, l.To))
			case unvisited:
				visit(l.To)
			}
		}

		state[id] = done
	}

	for _, id := range g.ids {
		if state[id] == unvisited {
			visit(id)
		}
	}

	return diags
}

func (g *graph) checkRetries() []Diagnostic {
	var diags []Diagnostic
	for _, l := range g.links {
		if l.Type != LinkRetry {
			continue
		}

		if l.MaxIterations < MinRetryIterations || l.MaxIterations > MaxRetryIterations {
			diags = append(diags, errorDiag(CodeRetryIterations, "", l.ID,
				"retry link %q maxIterations %d must be from %d to %d",
				l.ID, l.MaxIterations, MinRetryIterations, MaxRetryIterations))
		}

		if !reachable(g.delAdj, l.To)[l.From] {
			diags = append(diags, errorDiag(CodeRetryNotAncestor, "", l.ID,
				"retry link %q target %q does not reach %q through delegates links", l.ID, l.To, l.From))
		}
	}

	return diags
}

// checkHandoffs flags a node reached by a handoff link that can reach one of its own ancestors through delegates
// links. A retry link is the only allowed way back, so it is not part of the check.
func (g *graph) checkHandoffs() []Diagnostic {
	var diags []Diagnostic
	for _, l := range g.links {
		if l.Type != LinkDelegates || l.Mode != ModeHandoff {
			continue
		}

		ancestors := reachable(g.delRev, l.To)
		descendants := reachable(g.delAdj, l.To)
		for _, id := range g.ids {
			if ancestors[id] && descendants[id] {
				diags = append(diags, errorDiag(CodeHandoffReturn, l.To, l.ID,
					"handoff target %q can reach its ancestor %q through delegates links", l.To, id))

				break
			}
		}
	}

	return diags
}

func (g *graph) checkLeaves() []Diagnostic {
	var diags []Diagnostic
	for _, l := range g.links {
		kind := g.nodes[l.From].Kind
		if kind == KindSkill || kind == KindMCPServer {
			diags = append(diags, errorDiag(CodeLeafOutgoing, l.From, l.ID,
				"%s node %q cannot have outgoing link %q", kind, l.From, l.ID))
		}
	}

	return diags
}

func (g *graph) checkLinkKinds() []Diagnostic {
	var diags []Diagnostic
	for _, l := range g.links {
		from := g.nodes[l.From].Kind
		to := g.nodes[l.To].Kind

		var ok bool
		var want string
		switch l.Type {
		case LinkUses:
			ok, want = from == KindAgent && to == KindSkill, "an agent to a skill"
		case LinkCalls:
			ok, want = from == KindAgent && to == KindMCPServer, "an agent to an MCP server"
		default:
			ok, want = from == KindAgent && to == KindAgent, "an agent to an agent"
		}

		if !ok {
			diags = append(diags, errorDiag(CodeLinkKind, "", l.ID,
				"%s link %q must go %s", l.Type, l.ID, want))
		}
	}

	return diags
}

// checkDepth computes the longest delegation chain from the entry agent to each node. The relaxation runs at most
// one pass per node. A cycle keeps growing a depth, and the cap stops it. delegate_cycle reports the cycle itself.
func (g *graph) checkDepth() []Diagnostic {
	if g.entry == "" {
		return nil
	}

	depth := map[string]int{g.entry: 0}
	for pass := 0; pass < len(g.ids); pass++ {
		changed := false
		for _, l := range g.links {
			if l.Type != LinkDelegates {
				continue
			}

			d, ok := depth[l.From]
			if !ok {
				continue
			}

			if cur, seen := depth[l.To]; !seen || d+1 > cur {
				depth[l.To] = d + 1
				changed = true
			}
		}

		if !changed {
			break
		}
	}

	var diags []Diagnostic
	for _, id := range g.ids {
		if d, ok := depth[id]; ok && d > MaxDepth {
			diags = append(diags, errorDiag(CodeDepthLimit, id, "",
				"node %q is %d delegation levels from the entry agent, the limit is %d", id, d, MaxDepth))
		}
	}

	return diags
}

func (g *graph) checkFanout() []Diagnostic {
	var diags []Diagnostic
	for _, id := range g.ids {
		if n := len(g.delegates[id]); n > MaxFanout {
			diags = append(diags, errorDiag(CodeFanoutLimit, id, "",
				"agent %q has %d delegates links, the limit is %d", id, n, MaxFanout))
		}
	}

	return diags
}

// checkParallelGroups requires every link in a group to start at the same node and to use parallel mode.
func (g *graph) checkParallelGroups() []Diagnostic {
	groups := map[string][]Link{}
	var names []string
	for _, l := range g.links {
		if l.Type != LinkDelegates || l.Group == "" {
			continue
		}

		if _, ok := groups[l.Group]; !ok {
			names = append(names, l.Group)
		}

		groups[l.Group] = append(groups[l.Group], l)
	}

	var diags []Diagnostic
	for _, name := range names {
		links := groups[name]
		for _, l := range links {
			if l.From != links[0].From || l.Mode != ModeParallel {
				diags = append(diags, errorDiag(CodeParallelGroup, "", l.ID,
					"link %q in parallel group %q must start at %q in parallel mode",
					l.ID, name, links[0].From))
			}
		}
	}

	return diags
}

func (g *graph) checkContextOutputs() []Diagnostic {
	var diags []Diagnostic
	for _, l := range g.links {
		if l.Type != LinkDelegates {
			continue
		}

		if l.Context == ContextOutputs && len(l.Outputs) == 0 {
			diags = append(diags, errorDiag(CodeContextOutputs, "", l.ID,
				"delegates link %q uses the outputs context and needs outputs", l.ID))
		}

		if l.Context != ContextOutputs && len(l.Outputs) > 0 {
			diags = append(diags, errorDiag(CodeContextOutputs, "", l.ID,
				"delegates link %q lists outputs but its context is %q", l.ID, l.Context))
		}
	}

	return diags
}

func (g *graph) checkPlaceholders() []Diagnostic {
	if g.doc.Tutorial {
		return nil
	}

	var diags []Diagnostic
	for _, id := range g.ids {
		if g.nodes[id].Placeholder != nil {
			diags = append(diags, errorDiag(CodePlaceholderNotExportable, id, "",
				"placeholder node %q is only allowed in tutorial documents", id))
		}
	}

	return diags
}

// checkFullTranscripts warns when a delegate that receives the full transcript also calls an MCP server that is
// flagged for exfiltration risk. The warning does not block the document.
func (g *graph) checkFullTranscripts(opts Options) []Diagnostic {
	var diags []Diagnostic
	for _, l := range g.links {
		if l.Type != LinkDelegates || l.Context != ContextFullTranscript {
			continue
		}

		if flagged := g.flaggedCall(l.To, opts); flagged != "" {
			diags = append(diags, warningDiag(CodeRiskFullTranscript, "", l.ID,
				"delegates link %q sends the full transcript to %q, which calls flagged MCP server %q",
				l.ID, l.To, flagged))
		}
	}

	return diags
}

// flaggedCall returns the name of the first flagged MCP server that the agent calls, or "" when it calls none.
func (g *graph) flaggedCall(agentID string, opts Options) string {
	for _, l := range g.out[agentID] {
		if l.Type != LinkCalls {
			continue
		}

		ref := g.nodes[l.To].Ref
		if opts.FlaggedMCPServers[RefKey(ref.Namespace, ref.Name)] {
			return ref.Name
		}
	}

	return ""
}

func errorDiag(code, nodeID, linkID, format string, args ...any) Diagnostic {
	return newDiagnostic(code, SeverityError, nodeID, linkID, format, args...)
}

func warningDiag(code, nodeID, linkID, format string, args ...any) Diagnostic {
	return newDiagnostic(code, SeverityWarning, nodeID, linkID, format, args...)
}

func newDiagnostic(code, severity, nodeID, linkID, format string, args ...any) Diagnostic {
	return Diagnostic{
		Code:     code,
		Severity: severity,
		Message:  fmt.Sprintf(format, args...),
		NodeID:   nodeID,
		LinkID:   linkID,
	}
}
