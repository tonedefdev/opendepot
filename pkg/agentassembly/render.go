package agentassembly

import (
	"encoding/json"
	"fmt"
	"path"
	"slices"
	"strings"
)

// File is one generated file. Path is relative to the export root and always uses forward slashes.
type File struct {
	Path    string
	Content []byte
}

// ExportError is returned when a document has validation errors, so it cannot be exported.
type ExportError struct {
	Diagnostics []Diagnostic
}

// Error lists the codes of the error diagnostics.
func (e *ExportError) Error() string {
	var codes []string
	for _, d := range e.Diagnostics {
		if d.Severity == SeverityError {
			codes = append(codes, d.Code)
		}
	}

	return "agentassembly: export blocked by " + strings.Join(codes, ", ")
}

// Render returns the agent files for target. An empty target uses doc.Target. Render refuses a document that has
// validation errors, and a document with any placeholder node. Every file is a function of doc alone, so the same
// document always renders the same bytes.
//
// Each agent's tools list only its own calls links, and its skills list only its own uses links. A node never
// inherits access from another node. The delegation tool is listed only when the agent has delegates links.
func Render(doc Document, target string) ([]File, error) {
	if target == "" {
		target = doc.Target
	}

	if !slices.Contains(validTargets, target) {
		return nil, fmt.Errorf("agentassembly: unknown export target %q", target)
	}

	if err := exportGate(doc); err != nil {
		return nil, err
	}

	g := newGraph(doc)
	files := make([]File, 0, len(g.ids))
	for _, id := range g.agentIDs() {
		files = append(files, g.platformFile(target, id))
	}

	slices.SortFunc(files, func(a, b File) int {
		return strings.Compare(a.Path, b.Path)
	})

	return files, nil
}

func exportGate(doc Document) error {
	diags, err := ValidateExport(doc, Options{})
	if err != nil {
		return err
	}

	if HasErrors(diags) {
		return &ExportError{Diagnostics: diags}
	}

	return nil
}

type grant struct {
	server string
	all    bool
	tools  []string
}

func (g *graph) agentIDs() []string {
	var ids []string
	for _, id := range g.ids {
		if g.nodes[id].Kind == KindAgent {
			ids = append(ids, id)
		}
	}

	return ids
}

func (g *graph) platformFile(target, id string) File {
	switch target {
	case TargetClaude:
		return File{Path: path.Join(".claude", "agents", id+".md"), Content: g.claudeAgent(id)}
	case TargetAgentsMD:
		return File{Path: path.Join(id, "AGENTS.md"), Content: g.agentsMD(id)}
	default:
		return File{Path: path.Join(".github", "agents", id+".agent.md"), Content: g.copilotAgent(id)}
	}
}

// copilotAgent writes the Copilot harness keys. It never writes handoffs, which the harness does not accept.
// Handoffs and the delegation allowlist go in the Delegation body section instead.
func (g *graph) copilotAgent(id string) []byte {
	var b strings.Builder
	tools := copilotTools(g.grants(id))
	if len(g.delegationTargets(id)) > 0 {
		tools = append(tools, "task")
	}

	b.WriteString("---\n")
	fmt.Fprintf(&b, "name: %s\n", id)
	fmt.Fprintf(&b, "description: %s\n", jsonString(g.label(id)))
	fmt.Fprintf(&b, "tools: %s\n", jsonList(tools))
	if skills := g.skillNames(id); len(skills) > 0 {
		fmt.Fprintf(&b, "skills: %s\n", jsonList(skills))
	}

	b.WriteString("---\n\n")
	g.writeBody(&b, id, false)

	return []byte(b.String())
}

// claudeAgent writes the Claude subagent keys. The delegation allowlist is Agent(a, b) inside tools, and the
// handoffs go in the Delegation body section, because Claude has no handoffs key.
func (g *graph) claudeAgent(id string) []byte {
	var b strings.Builder
	tools := claudeTools(g.grants(id))
	if targets := g.delegationTargets(id); len(targets) > 0 {
		tools = append(tools, "Agent("+strings.Join(targets, ", ")+")")
	}

	toolList := strings.Join(tools, ", ")
	if toolList == "" {
		toolList = `""`
	}

	b.WriteString("---\n")
	fmt.Fprintf(&b, "name: %s\n", id)
	fmt.Fprintf(&b, "description: %s\n", jsonString(g.label(id)))
	fmt.Fprintf(&b, "tools: %s\n", toolList)
	if skills := g.skillNames(id); len(skills) > 0 {
		fmt.Fprintf(&b, "skills: %s\n", jsonList(skills))
	}

	b.WriteString("---\n\n")
	g.writeBody(&b, id, false)

	return []byte(b.String())
}

// agentsMD writes one AGENTS.md per agent. It has no frontmatter, so its skills and tools go in body sections.
func (g *graph) agentsMD(id string) []byte {
	var b strings.Builder
	g.writeBody(&b, id, true)

	return []byte(b.String())
}

// writeBody writes the heading, the optional skills and tools sections, and the Delegation section.
func (g *graph) writeBody(b *strings.Builder, id string, sections bool) {
	fmt.Fprintf(b, "# %s\n\n", oneLine(g.label(id)))
	if sections {
		if skills := g.skillNames(id); len(skills) > 0 {
			b.WriteString("## Skills\n\n")
			for _, s := range skills {
				fmt.Fprintf(b, "- `%s`\n", s)
			}

			b.WriteString("\n")
		}

		if tools := copilotTools(g.grants(id)); len(tools) > 0 {
			b.WriteString("## Tools\n\n")
			for _, t := range tools {
				fmt.Fprintf(b, "- `%s`\n", t)
			}

			b.WriteString("\n")
		}
	}

	g.writeDelegation(b, id)
}

// writeDelegation writes the Delegation section. It lists the allowlist, the handoffs, and the retries of the agent,
// in link ID order. Nothing is written when the agent has no delegates or retry links.
func (g *graph) writeDelegation(b *strings.Builder, id string) {
	var links []Link
	for _, l := range g.out[id] {
		if l.Type == LinkDelegates || l.Type == LinkRetry {
			links = append(links, l)
		}
	}

	if len(links) == 0 {
		return
	}

	b.WriteString("## Delegation\n\n")
	if targets := g.delegationTargets(id); len(targets) > 0 {
		fmt.Fprintf(b, "Delegate only to %s.\n\n", codeList(targets))
	}

	for _, l := range links {
		if l.Type == LinkRetry {
			fmt.Fprintf(b, "- Retry %s up to %d times until %s (link `%s`).\n",
				codeList([]string{l.To}), l.MaxIterations, jsonString(oneLine(l.Until)), l.ID)

			continue
		}

		fmt.Fprintf(b, "- %s with context `%s`%s (link `%s`).\n",
			delegationPhrase(l), l.Context, outputsPhrase(l), l.ID)
	}

	b.WriteString("\n")
}

func delegationPhrase(l Link) string {
	target := codeList([]string{l.To})
	switch l.Mode {
	case ModeHandoff:
		return "Hand off to " + target
	case ModeParallel:
		return fmt.Sprintf("Run %s in parallel (group `%s`)", target, oneLine(l.Group))
	default:
		return "Run " + target + " as a subagent"
	}
}

func outputsPhrase(l Link) string {
	if len(l.Outputs) == 0 {
		return ""
	}

	return ", returning " + codeList(l.Outputs)
}

// grants returns the MCP servers that the agent calls, with the tools of each. An empty tool list on any calls link
// to a server grants all of its tools.
func (g *graph) grants(id string) []grant {
	byServer := map[string]*grant{}
	var names []string
	for _, l := range g.out[id] {
		if l.Type != LinkCalls {
			continue
		}

		server := g.nodes[l.To].Ref.Name
		gr, ok := byServer[server]
		if !ok {
			gr = &grant{server: server}
			byServer[server] = gr
			names = append(names, server)
		}

		if len(l.Tools) == 0 {
			gr.all = true
			continue
		}

		gr.tools = append(gr.tools, l.Tools...)
	}

	slices.Sort(names)
	out := make([]grant, 0, len(names))
	for _, name := range names {
		gr := byServer[name]
		gr.tools = uniqueSorted(gr.tools)
		out = append(out, *gr)
	}

	return out
}

// skillNames returns the names of the skills that the agent uses, sorted and unique.
func (g *graph) skillNames(id string) []string {
	var names []string
	for _, l := range g.out[id] {
		if l.Type == LinkUses {
			names = append(names, g.nodes[l.To].Ref.Name)
		}
	}

	return uniqueSorted(names)
}

// delegationTargets returns the agent IDs that the agent may delegate to, sorted and unique.
func (g *graph) delegationTargets(id string) []string {
	targets := make([]string, 0, len(g.delegates[id]))
	for _, l := range g.delegates[id] {
		targets = append(targets, l.To)
	}

	return uniqueSorted(targets)
}

func (g *graph) label(id string) string {
	if label := oneLine(g.nodes[id].Label); label != "" {
		return label
	}

	return id
}

func copilotTools(grants []grant) []string {
	var tools []string
	for _, gr := range grants {
		if gr.all {
			tools = append(tools, gr.server+"/*")

			continue
		}

		for _, t := range gr.tools {
			tools = append(tools, gr.server+"/"+t)
		}
	}

	return tools
}

func claudeTools(grants []grant) []string {
	var tools []string
	for _, gr := range grants {
		if gr.all {
			tools = append(tools, "mcp__"+gr.server)

			continue
		}

		for _, t := range gr.tools {
			tools = append(tools, "mcp__"+gr.server+"__"+t)
		}
	}

	return tools
}

func uniqueSorted(values []string) []string {
	out := slices.Clone(values)
	slices.Sort(out)

	return slices.Compact(out)
}

// codeList formats items as a comma-separated list of inline code spans.
func codeList(items []string) string {
	quoted := make([]string, 0, len(items))
	for _, item := range items {
		quoted = append(quoted, "`"+oneLine(item)+"`")
	}

	return strings.Join(quoted, ", ")
}

// oneLine collapses all whitespace, including newlines, so user text stays on one line in Markdown and YAML.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// jsonString quotes s as a JSON string, which is also a valid YAML double-quoted scalar. Encoding a string cannot
// fail, so the error is ignored.
func jsonString(s string) string {
	raw, _ := json.Marshal(s)

	return string(raw)
}

// jsonList writes a YAML flow sequence of quoted strings. A nil list writes []. Encoding a string cannot fail.
func jsonList(items []string) string {
	quoted := make([]string, 0, len(items))
	for _, item := range items {
		quoted = append(quoted, jsonString(item))
	}

	return "[" + strings.Join(quoted, ", ") + "]"
}
