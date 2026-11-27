package agentassembly

import (
	"fmt"
)

// Route deny codes. They are separate from the validation codes because a route decision is not a graph rule.
const (
	RouteCodeDigestMismatch = "digest_mismatch"
	RouteCodeGraphInvalid   = "graph_invalid"
	RouteCodeNodeUnknown    = "node_unknown"
	RouteCodeLinkUndeclared = "link_undeclared"
	RouteCodeRetryExhausted = "retry_exhausted"
)

// AssemblyManifest is the assembly.json model. Decisions are stored on the links of Graph, and Digest is the
// canonical digest of Graph.
type AssemblyManifest struct {
	Digest string   `json:"digest"`
	Graph  Document `json:"graph"`
}

// RouteRequest asks whether the agent From may hand work to the agent To. LinkID is optional. When it is set, the
// route must match that link exactly.
type RouteRequest struct {
	From   string `json:"from"`
	To     string `json:"to"`
	LinkID string `json:"linkId,omitempty"`
}

// RouteState holds the retry counters. Keys come from RouteKey. A missing key means zero iterations so far.
type RouteState map[string]int

// RouteDecision is the result of Route. Allowed is false for every denial, and Reason explains it. Iteration is the
// retry count after this route, and it is set only for an allowed retry.
type RouteDecision struct {
	Allowed   bool   `json:"allowed"`
	Code      string `json:"code,omitempty"`
	Reason    string `json:"reason"`
	LinkID    string `json:"linkId,omitempty"`
	LinkType  string `json:"linkType,omitempty"`
	Iteration int    `json:"iteration,omitempty"`
	StateKey  string `json:"stateKey,omitempty"`
}

// RouteKey returns the retry counter key for a link in an assembly. Counters are per digest, so a changed graph
// starts its counters at zero.
func RouteKey(assemblyDigest, linkID string) string {
	return assemblyDigest + "/" + linkID
}

// Route applies the deny-by-default policy to one hand-off. It allows a route only when the manifest is intact, the
// graph is valid for export, both nodes exist, and a matching delegates or retry link is declared. A retry is also
// bounded by its maxIterations. Route does not change state. Call RouteState.Apply with the decision to record it.
func Route(m AssemblyManifest, req RouteRequest, state RouteState) RouteDecision {
	digest, err := Digest(m.Graph)
	if err != nil || digest != m.Digest {
		return deny(RouteCodeDigestMismatch, "assembly digest does not match its graph")
	}

	diags, err := ValidateExport(m.Graph, Options{})
	if err != nil {
		return deny(RouteCodeGraphInvalid, err.Error())
	}

	if HasErrors(diags) {
		return deny(RouteCodeGraphInvalid, "graph has validation errors")
	}

	g := newGraph(m.Graph)
	if _, ok := g.nodes[req.From]; !ok {
		return deny(RouteCodeNodeUnknown, fmt.Sprintf("node %q is not in the assembly", req.From))
	}

	if _, ok := g.nodes[req.To]; !ok {
		return deny(RouteCodeNodeUnknown, fmt.Sprintf("node %q is not in the assembly", req.To))
	}

	link, ok := findRouteLink(g, req)
	if !ok {
		return deny(RouteCodeLinkUndeclared,
			fmt.Sprintf("no delegates or retry link is declared from %q to %q", req.From, req.To))
	}

	if link.Type == LinkDelegates {
		return RouteDecision{
			Allowed:  true,
			Reason:   fmt.Sprintf("delegates link %q allows %q to delegate to %q", link.ID, req.From, req.To),
			LinkID:   link.ID,
			LinkType: link.Type,
		}
	}

	key := RouteKey(digest, link.ID)
	current := state[key]
	if current >= link.MaxIterations {
		return RouteDecision{
			Code:     RouteCodeRetryExhausted,
			Reason:   fmt.Sprintf("retry link %q reached its limit of %d iterations", link.ID, link.MaxIterations),
			LinkID:   link.ID,
			LinkType: link.Type,
			StateKey: key,
		}
	}

	return RouteDecision{
		Allowed:   true,
		Reason:    fmt.Sprintf("retry link %q allows iteration %d of %d", link.ID, current+1, link.MaxIterations),
		LinkID:    link.ID,
		LinkType:  link.Type,
		Iteration: current + 1,
		StateKey:  key,
	}
}

// Apply records an allowed retry decision and returns the updated state. The state is created when it is nil.
// Denied decisions and delegates decisions leave the state unchanged.
func (s RouteState) Apply(d RouteDecision) RouteState {
	if !d.Allowed || d.LinkType != LinkRetry {
		return s
	}

	if s == nil {
		s = RouteState{}
	}

	s[d.StateKey] = d.Iteration

	return s
}

// findRouteLink returns the link that the request names. With a LinkID, the link must match From and To. Without
// one, a delegates link is preferred over a retry link, and the lowest link ID wins.
func findRouteLink(g *graph, req RouteRequest) (Link, bool) {
	if req.LinkID != "" {
		for _, l := range g.out[req.From] {
			if l.ID == req.LinkID && l.To == req.To && (l.Type == LinkDelegates || l.Type == LinkRetry) {
				return l, true
			}
		}

		return Link{}, false
	}

	for _, want := range []string{LinkDelegates, LinkRetry} {
		for _, l := range g.out[req.From] {
			if l.To == req.To && l.Type == want {
				return l, true
			}
		}
	}

	return Link{}, false
}

func deny(code, reason string) RouteDecision {
	return RouteDecision{Code: code, Reason: reason}
}
