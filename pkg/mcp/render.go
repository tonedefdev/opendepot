package mcp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// ManagedPrefix marks every MCP entry that OpenDepot owns. The renderers add, update, and remove only the entries whose
// key starts with this prefix, and every other entry in the file is kept as it was.
const ManagedPrefix = "opendepot-"

// Project-relative MCP config paths for each install target.
const (
	CopilotConfigPath = ".vscode/mcp.json"
	ClaudeConfigPath  = ".mcp.json"
	CodexConfigPath   = ".codex/config.toml"
)

// Top-level keys that hold the server table in each format.
const (
	copilotServersKey = "servers"
	claudeServersKey  = "mcpServers"
	codexServersKey   = "mcp_servers"
)

var managedNamePattern = regexp.MustCompile(`^opendepot-[a-z0-9][a-z0-9-]*$`)

// maxManagedNameLength keeps a managed key short enough for every client to display.
const maxManagedNameLength = 64

// ManagedServer is one OpenDepot-managed MCP entry. Name must start with ManagedPrefix.
type ManagedServer struct {
	Name   string
	Launch LaunchDescriptor
}

// jsonServer is one entry in a JSON config file (Copilot, Claude, and custom paths).
type jsonServer struct {
	Type    string   `json:"type"`
	Command string   `json:"command,omitempty"`
	Args    []string `json:"args,omitempty"`
	URL     string   `json:"url,omitempty"`
}

// codexServer is one [mcp_servers.<name>] table in .codex/config.toml.
type codexServer struct {
	Command string   `toml:"command,omitempty"`
	Args    []string `toml:"args,omitempty"`
	URL     string   `toml:"url,omitempty"`
}

// RenderCopilot merges the managed servers into an existing .vscode/mcp.json, or renders a fresh file when existing is
// empty. The servers table is the "servers" key.
func RenderCopilot(existing []byte, servers []ManagedServer) ([]byte, error) {
	return renderJSON(existing, copilotServersKey, servers)
}

// RenderClaude merges the managed servers into an existing .mcp.json. The servers table is the "mcpServers" key.
func RenderClaude(existing []byte, servers []ManagedServer) ([]byte, error) {
	return renderJSON(existing, claudeServersKey, servers)
}

// RenderCustom merges the managed servers into an existing JSON MCP config at a caller-provided path. serversKey names
// the top-level table that holds the servers.
func RenderCustom(existing []byte, serversKey string, servers []ManagedServer) ([]byte, error) {
	if serversKey == "" {
		return nil, errors.New("mcp: custom config requires a servers key")
	}

	return renderJSON(existing, serversKey, servers)
}

// RenderCodex merges the managed servers into an existing .codex/config.toml as [mcp_servers.<name>] tables. Codex
// reads this file only for trusted projects. Comments in an existing file are not preserved.
func RenderCodex(existing []byte, servers []ManagedServer) ([]byte, error) {
	desired, err := desiredEntries(servers)
	if err != nil {
		return nil, err
	}

	root := map[string]any{}
	if len(bytes.TrimSpace(existing)) > 0 {
		if err := toml.Unmarshal(existing, &root); err != nil {
			return nil, fmt.Errorf("mcp: parse existing %s: %w", CodexConfigPath, err)
		}
	}

	table := map[string]any{}
	if raw, ok := root[codexServersKey]; ok {
		existingTable, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("mcp: %s in %s is not a table", codexServersKey, CodexConfigPath)
		}

		table = existingTable
	}

	for name := range table {
		if _, keep := desired[name]; !keep && strings.HasPrefix(name, ManagedPrefix) {
			delete(table, name)
		}
	}

	for name, entry := range desired {
		table[name] = codexServer{Command: entry.Command, Args: entry.Args, URL: entry.URL}
	}

	root[codexServersKey] = table
	return toml.Marshal(root)
}

// renderJSON merges the managed entries into the servers table of a JSON config document. Unmanaged keys at every
// level are kept. Output keys are sorted so that the diff of a re-render is stable.
func renderJSON(existing []byte, serversKey string, servers []ManagedServer) ([]byte, error) {
	desired, err := desiredEntries(servers)
	if err != nil {
		return nil, err
	}

	root := map[string]json.RawMessage{}
	if len(bytes.TrimSpace(existing)) > 0 {
		if err := json.Unmarshal(existing, &root); err != nil {
			return nil, fmt.Errorf("mcp: parse existing %s config: %w", serversKey, err)
		}
	}

	if root == nil {
		root = map[string]json.RawMessage{}
	}

	entries := map[string]json.RawMessage{}
	if raw, ok := root[serversKey]; ok && string(bytes.TrimSpace(raw)) != "null" {
		if err := json.Unmarshal(raw, &entries); err != nil {
			return nil, fmt.Errorf("mcp: %q in existing config is not an object: %w", serversKey, err)
		}
	}

	for name := range entries {
		if _, keep := desired[name]; !keep && strings.HasPrefix(name, ManagedPrefix) {
			delete(entries, name)
		}
	}

	for name, entry := range desired {
		raw, err := encodeJSON(jsonServer(entry))
		if err != nil {
			return nil, err
		}

		entries[name] = bytes.TrimSpace(raw)
	}

	encodedEntries, err := encodeJSON(entries)
	if err != nil {
		return nil, err
	}

	root[serversKey] = bytes.TrimSpace(encodedEntries)
	return encodeJSON(root)
}

// desiredEntries validates the managed servers and converts each launch descriptor into a client-neutral entry.
func desiredEntries(servers []ManagedServer) (map[string]ServerEntry, error) {
	desired := make(map[string]ServerEntry, len(servers))
	for _, server := range servers {
		if len(server.Name) > maxManagedNameLength || !managedNamePattern.MatchString(server.Name) {
			return nil, fmt.Errorf("mcp: managed server name %q must match %s and be at most %d characters",
				server.Name, managedNamePattern, maxManagedNameLength)
		}

		if _, dup := desired[server.Name]; dup {
			return nil, fmt.Errorf("mcp: duplicate managed server %q", server.Name)
		}

		entry, err := server.Launch.Entry()
		if err != nil {
			return nil, fmt.Errorf("mcp: managed server %q: %w", server.Name, err)
		}

		desired[server.Name] = entry
	}

	return desired, nil
}

// encodeJSON writes v as indented JSON with a trailing newline. HTML characters are not escaped, so launch arguments
// keep their literal form.
func encodeJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, fmt.Errorf("mcp: encode config: %w", err)
	}

	return buf.Bytes(), nil
}
