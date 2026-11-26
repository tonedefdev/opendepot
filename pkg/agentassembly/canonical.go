package agentassembly

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// Digest returns the sha256 digest of the canonical JSON of doc, in the form sha256:<64 hex>. Nodes and links are
// ordered by ID, tools, outputs, and risk flags are sorted, and times are in UTC. The result does not depend on the
// order that the document lists its nodes, links, or set-like fields in. Digest does not validate doc.
func Digest(doc Document) (string, error) {
	raw, err := json.Marshal(canonical(doc))
	if err != nil {
		return "", fmt.Errorf("agentassembly: encode canonical document: %w", err)
	}

	var generic any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&generic); err != nil {
		return "", fmt.Errorf("agentassembly: decode canonical document: %w", err)
	}

	// Re-encoding the generic value writes object keys in sorted order.
	sorted, err := json.Marshal(generic)
	if err != nil {
		return "", fmt.Errorf("agentassembly: encode sorted document: %w", err)
	}

	sum := sha256.Sum256(sorted)

	return DigestPrefix + hex.EncodeToString(sum[:]), nil
}

func canonical(doc Document) Document {
	out := doc
	out.Nodes = make([]Node, 0, len(doc.Nodes))
	out.Nodes = append(out.Nodes, doc.Nodes...)
	slices.SortFunc(out.Nodes, func(a, b Node) int {
		return strings.Compare(a.ID, b.ID)
	})

	out.Links = make([]Link, 0, len(doc.Links))
	for _, l := range doc.Links {
		l.Tools = sortedCopy(l.Tools)
		l.Outputs = sortedCopy(l.Outputs)
		if l.Decision != nil {
			d := *l.Decision
			d.RiskFlags = sortedCopy(d.RiskFlags)
			d.DecidedAt = d.DecidedAt.UTC()
			l.Decision = &d
		}

		out.Links = append(out.Links, l)
	}

	slices.SortFunc(out.Links, func(a, b Link) int {
		return strings.Compare(a.ID, b.ID)
	})

	return out
}

func sortedCopy(in []string) []string {
	out := slices.Clone(in)
	slices.Sort(out)

	return out
}
