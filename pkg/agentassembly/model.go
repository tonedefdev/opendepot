package agentassembly

import (
	"errors"
	"regexp"
	"time"
)

// Node kinds.
const (
	KindAgent     = "agent"
	KindSkill     = "skill"
	KindMCPServer = "mcpServer"
)

// Link types.
const (
	LinkUses      = "uses"
	LinkCalls     = "calls"
	LinkDelegates = "delegates"
	LinkRetry     = "retry"
)

// Delegation modes of a delegates link.
const (
	ModeSubagent = "subagent"
	ModeHandoff  = "handoff"
	ModeParallel = "parallel"
)

// Delegation contexts of a delegates link.
const (
	ContextTaskOnly       = "task-only"
	ContextSummary        = "summary"
	ContextFullTranscript = "full-transcript"
	ContextOutputs        = "outputs"
)

// Export targets.
const (
	TargetCopilot  = "copilot"
	TargetClaude   = "claude"
	TargetAgentsMD = "agents-md"
)

// Decision makers of a link decision.
const (
	DecidedByJev  = "jev"
	DecidedByUser = "user"
)

// Graph limits.
const (
	// MaxDepth is the longest delegation chain from the entry agent.
	MaxDepth = 5

	// MaxFanout is the most delegates links that one agent can have.
	MaxFanout = 8

	// MinRetryIterations and MaxRetryIterations bound the maxIterations of a retry link.
	MinRetryIterations = 1
	MaxRetryIterations = 10

	// MaxUntilRunes is the longest natural-language exit condition of a retry link.
	MaxUntilRunes = 512
)

// Diagnostic severities. They match the Schematic diagnostic enum.
const (
	SeverityError   = "Error"
	SeverityWarning = "Warning"
)

// Stable validation codes. Clients match on these strings, so they must not change.
const (
	CodeEntryCount               = "entry_count"
	CodeDelegateCycle            = "delegate_cycle"
	CodeRetryNotAncestor         = "retry_not_ancestor"
	CodeRetryIterations          = "retry_iterations"
	CodeHandoffReturn            = "handoff_return"
	CodeLeafOutgoing             = "leaf_outgoing"
	CodeLinkKind                 = "link_kind"
	CodeDepthLimit               = "depth_limit"
	CodeFanoutLimit              = "fanout_limit"
	CodeParallelGroup            = "parallel_group"
	CodeContextOutputs           = "context_outputs"
	CodeRefUnresolved            = "ref_unresolved"
	CodePlaceholderNotExportable = "placeholder_not_exportable"
	CodeRiskFullTranscript       = "risk_full_transcript"
)

// DigestPrefix is the prefix of every digest that Digest returns.
const DigestPrefix = "sha256:"

// ErrInvalidDocument is wrapped by every structural error. A structural error means the document is malformed,
// which is different from a rule violation that Validate reports as a Diagnostic.
var ErrInvalidDocument = errors.New("invalid agent assembly document")

var nodeIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)

// Document is an agent assembly: a typed graph of agents, skills, and MCP servers.
type Document struct {
	Nodes    []Node `json:"nodes"`
	Links    []Link `json:"links"`
	Target   string `json:"target"`
	Tutorial bool   `json:"tutorial,omitempty"`
}

// Node is one agent, skill, or MCP server in the graph.
type Node struct {
	ID          string       `json:"id"`
	Kind        string       `json:"kind"`
	Ref         Ref          `json:"ref"`
	Entry       bool         `json:"entry"`
	Label       string       `json:"label"`
	Placeholder *Placeholder `json:"placeholder,omitempty"`
}

// Ref points at a published OpenDepot resource. Version is a constraint until export pins it.
type Ref struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Version   string `json:"version"`
}

// Placeholder marks a sample node in a tutorial document. A placeholder can never be exported.
type Placeholder struct {
	Description string `json:"description,omitempty"`
}

// Link is one edge of the graph. The fields that apply depend on Type.
type Link struct {
	ID            string    `json:"id"`
	From          string    `json:"from"`
	To            string    `json:"to"`
	Type          string    `json:"type"`
	Tools         []string  `json:"tools,omitempty"`
	Mode          string    `json:"mode,omitempty"`
	Context       string    `json:"context,omitempty"`
	Outputs       []string  `json:"outputs,omitempty"`
	Group         string    `json:"group,omitempty"`
	MaxIterations int       `json:"maxIterations,omitempty"`
	Until         string    `json:"until,omitempty"`
	Decision      *Decision `json:"decision,omitempty"`
}

// Decision is the saved advice for a delegates or retry link.
type Decision struct {
	DecidedBy     string        `json:"decidedBy"`
	Model         string        `json:"model"`
	Probabilities Probabilities `json:"probabilities"`
	Confidence    float64       `json:"confidence"`
	RiskFlags     []string      `json:"riskFlags,omitempty"`
	DecidedAt     time.Time     `json:"decidedAt"`
}

// Probabilities holds the probability of each delegation mode.
type Probabilities struct {
	Subagent float64 `json:"subagent"`
	Handoff  float64 `json:"handoff"`
	Parallel float64 `json:"parallel"`
}

// Diagnostic is one rule violation or warning. Code is a stable validation code.
type Diagnostic struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
	NodeID   string `json:"nodeId,omitempty"`
	LinkID   string `json:"linkId,omitempty"`
}

// Options carries facts that the server knows and the document does not.
type Options struct {
	// FlaggedMCPServers holds the MCP servers that Jev or the scans flagged for exfiltration risk. Keys are
	// RefKey(namespace, name).
	FlaggedMCPServers map[string]bool
}

// HasErrors reports whether any diagnostic has the Error severity.
func HasErrors(diags []Diagnostic) bool {
	for _, d := range diags {
		if d.Severity == SeverityError {
			return true
		}
	}

	return false
}

// RefKey is the key of a reference in Options.FlaggedMCPServers.
func RefKey(namespace, name string) string {
	return namespace + "/" + name
}

// RefUnresolved returns the diagnostic that the server sets when a node's reference cannot be resolved or is hidden
// from the caller. Validate never sets it, because resolution needs the cluster.
func RefUnresolved(nodeID, reason string) Diagnostic {
	return Diagnostic{
		Code:     CodeRefUnresolved,
		Severity: SeverityError,
		Message:  "reference for node " + nodeID + " cannot be resolved: " + reason,
		NodeID:   nodeID,
	}
}
