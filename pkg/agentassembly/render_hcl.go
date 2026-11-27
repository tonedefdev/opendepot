package agentassembly

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// pinnedVersion matches an exact semantic version. A constraint such as "~> 1.2" does not match.
var pinnedVersion = regexp.MustCompile(`^\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$`)

var hclBlockTypes = map[string]string{
	KindAgent:     "agent",
	KindSkill:     "agent_skill",
	KindMCPServer: "mcp_server",
}

// RenderHCL returns the pinned opendepot.hcl for the document. It writes an opendepot block with the target, and one
// agent, agent_skill, or mcp_server block for each node, labeled by node ID. Each source is
// registryHost/namespace/name, and each version must be an exact version. RenderHCL does not resolve constraints, so
// the export step must pin them first. It refuses a document with validation errors, and a document with any
// placeholder node.
func RenderHCL(doc Document, target, registryHost string) ([]byte, error) {
	if target == "" {
		target = doc.Target
	}

	if !slices.Contains(validTargets, target) {
		return nil, fmt.Errorf("agentassembly: unknown export target %q", target)
	}

	if registryHost == "" {
		return nil, errors.New("agentassembly: registry host is required")
	}

	if err := exportGate(doc); err != nil {
		return nil, err
	}

	g := newGraph(doc)

	var b strings.Builder
	fmt.Fprintf(&b, "opendepot {\n  targets = [%s]\n}\n", hclQuote(target))
	for _, id := range g.ids {
		n := g.nodes[id]
		if !pinnedVersion.MatchString(n.Ref.Version) {
			return nil, fmt.Errorf("agentassembly: node %q version %q is not pinned to an exact version", id, n.Ref.Version)
		}

		source := registryHost + "/" + n.Ref.Namespace + "/" + n.Ref.Name
		fmt.Fprintf(&b, "\n%s %s {\n  source  = %s\n  version = %s\n}\n",
			hclBlockTypes[n.Kind], hclQuote(id), hclQuote(source), hclQuote(n.Ref.Version))
	}

	return []byte(b.String()), nil
}

// hclQuote writes s as an HCL quoted string. It escapes the quote, backslash, and control characters, and it escapes
// the ${ and %{ template openers so that the value is always a literal.
func hclQuote(s string) string {
	escaper := strings.NewReplacer(
		`\`, `\\`,
		`"`, `\"`,
		"\n", `\n`,
		"\r", `\r`,
		"\t", `\t`,
		"${", "$${",
		"%{", "%%{",
	)

	return `"` + escaper.Replace(s) + `"`
}
