package mcp

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

const testCustomKey = "customServers"

// testManagedServers is the desired managed set shared by every golden case. It covers each runtime and transport.
func testManagedServers() []ManagedServer {
	return []ManagedServer{
		{
			Name: "opendepot-filesystem",
			Launch: LaunchDescriptor{
				Command:   testNode,
				Args:      []string{".opendepot/mcp/filesystem/" + testEntryFile, testStdioFlag},
				Transport: TransportStdio,
				Runtime:   RuntimeNode,
			},
		},
		{
			Name: "opendepot-summarizer",
			Launch: LaunchDescriptor{
				Command:   testPython,
				Args:      []string{"-m", "summarizer"},
				Transport: TransportStdio,
				Runtime:   RuntimePython,
			},
		},
		{
			Name: "opendepot-search",
			Launch: LaunchDescriptor{
				Transport:   TransportStdio,
				Runtime:     RuntimeOCI,
				ImageRef:    testImageRef,
				ImageDigest: testDigest,
			},
		},
		{
			Name: "opendepot-remote",
			Launch: LaunchDescriptor{
				Command:   testNode,
				Args:      []string{"server.js"},
				Transport: TransportStreamableHTTP,
				Runtime:   RuntimeNode,
				URL:       testRemoteURL,
			},
		},
	}
}

// goldenCase drives one renderer through a fresh render or a merge into an existing file.
type goldenCase struct {
	name     string
	existing string
	render   func(existing []byte) ([]byte, error)
}

func renderCases() []goldenCase {
	servers := testManagedServers()
	return []goldenCase{
		{
			name:   "copilot-fresh.json",
			render: func(existing []byte) ([]byte, error) { return RenderCopilot(existing, servers) },
		},
		{
			name:     "copilot-merge.json",
			existing: "existing/copilot.json",
			render:   func(existing []byte) ([]byte, error) { return RenderCopilot(existing, servers) },
		},
		{
			name:   "claude-fresh.json",
			render: func(existing []byte) ([]byte, error) { return RenderClaude(existing, servers) },
		},
		{
			name:     "claude-merge.json",
			existing: "existing/claude.json",
			render:   func(existing []byte) ([]byte, error) { return RenderClaude(existing, servers) },
		},
		{
			name:   "codex-fresh.toml",
			render: func(existing []byte) ([]byte, error) { return RenderCodex(existing, servers) },
		},
		{
			name:     "codex-merge.toml",
			existing: "existing/codex.toml",
			render:   func(existing []byte) ([]byte, error) { return RenderCodex(existing, servers) },
		},
		{
			name:     "custom-merge.json",
			existing: "existing/custom.json",
			render:   func(existing []byte) ([]byte, error) { return RenderCustom(existing, testCustomKey, servers) },
		},
	}
}

func readTestData(t *testing.T, name string) []byte {
	t.Helper()
	if name == "" {
		return nil
	}

	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}

	return data
}

// checkGolden compares got with testdata/golden/name. Set UPDATE_GOLDEN=1 to rewrite the file, and review the diff.
func checkGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	golden := filepath.Join("testdata", "golden", name)
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(golden, got, 0o644); err != nil {
			t.Fatalf("write golden %s: %v", name, err)
		}
	}

	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden %s: %v (run with UPDATE_GOLDEN=1 to create it)", name, err)
	}

	if !bytes.Equal(got, want) {
		t.Errorf("%s does not match its golden file:\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}

func TestRenderGoldenFiles(t *testing.T) {
	for _, tc := range renderCases() {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.render(readTestData(t, tc.existing))
			if err != nil {
				t.Fatalf("render error = %v", err)
			}

			checkGolden(t, tc.name, got)
		})
	}
}

func TestRenderIsIdempotent(t *testing.T) {
	for _, tc := range renderCases() {
		t.Run(tc.name, func(t *testing.T) {
			first, err := tc.render(readTestData(t, tc.existing))
			if err != nil {
				t.Fatalf("first render error = %v", err)
			}

			second, err := tc.render(first)
			if err != nil {
				t.Fatalf("second render error = %v", err)
			}

			if !bytes.Equal(first, second) {
				t.Errorf("re-rendering changed the output:\n%s\nvs\n%s", first, second)
			}
		})
	}
}

// TestRenderPreservesUnmanagedEntries checks the merge contract directly, without the golden files: unmanaged entries
// survive, the stale managed entry is removed, and the managed entries that remain are updated.
func TestRenderPreservesUnmanagedEntries(t *testing.T) {
	t.Run("copilot", func(t *testing.T) {
		out, err := RenderCopilot(readTestData(t, "existing/copilot.json"), testManagedServers())
		if err != nil {
			t.Fatalf("RenderCopilot() error = %v", err)
		}

		doc := decodeJSONObject(t, out)
		servers := decodeRawMap(t, doc["servers"])
		if _, ok := doc["inputs"]; !ok {
			t.Error("top-level inputs key was dropped")
		}

		if !strings.Contains(string(servers["github"]), "https://api.githubcopilot.com/mcp/") {
			t.Errorf("unmanaged github entry changed: %s", servers["github"])
		}

		if _, ok := servers["opendepot-retired"]; ok {
			t.Error("stale managed entry opendepot-retired was not removed")
		}

		if !strings.Contains(string(servers["opendepot-filesystem"]), ".opendepot/mcp/filesystem/"+testEntryFile) {
			t.Errorf("managed entry opendepot-filesystem was not updated: %s", servers["opendepot-filesystem"])
		}
	})

	t.Run("claude", func(t *testing.T) {
		out, err := RenderClaude(readTestData(t, "existing/claude.json"), testManagedServers())
		if err != nil {
			t.Fatalf("RenderClaude() error = %v", err)
		}

		doc := decodeJSONObject(t, out)
		servers := decodeRawMap(t, doc["mcpServers"])
		if !strings.Contains(string(servers["playwright"]), "@playwright/mcp@latest") {
			t.Errorf("unmanaged playwright entry changed: %s", servers["playwright"])
		}

		if _, ok := doc["theme"]; !ok {
			t.Error("unrelated top-level key theme was dropped")
		}
	})

	t.Run("codex", func(t *testing.T) {
		out, err := RenderCodex(readTestData(t, "existing/codex.toml"), testManagedServers())
		if err != nil {
			t.Fatalf("RenderCodex() error = %v", err)
		}

		var doc map[string]any
		if err := toml.Unmarshal(out, &doc); err != nil {
			t.Fatalf("rendered TOML does not parse: %v", err)
		}

		servers, _ := doc["mcp_servers"].(map[string]any)
		github, _ := servers["github"].(map[string]any)
		if github["command"] != "github-mcp-server" {
			t.Errorf("unmanaged github table changed: %v", github)
		}

		if _, ok := servers["opendepot-retired"]; ok {
			t.Error("stale managed table opendepot-retired was not removed")
		}

		if doc["model"] != "gpt-6-luna" {
			t.Errorf("unrelated top-level key model = %v, want gpt-6-luna", doc["model"])
		}
	})

	t.Run("custom", func(t *testing.T) {
		out, err := RenderCustom(readTestData(t, "existing/custom.json"), testCustomKey, testManagedServers())
		if err != nil {
			t.Fatalf("RenderCustom() error = %v", err)
		}

		doc := decodeJSONObject(t, out)
		servers := decodeRawMap(t, doc[testCustomKey])
		if _, ok := servers["local-tool"]; !ok {
			t.Error("unmanaged local-tool entry was dropped")
		}

		if _, ok := servers["opendepot-retired"]; ok {
			t.Error("stale managed entry was not removed from the custom path")
		}
	})
}

// TestRenderImagePinsDigest checks the OCI entry in both formats. It must run the digest-pinned image reference.
func TestRenderImagePinsDigest(t *testing.T) {
	want := testImageRef + "@" + testDigest

	jsonOut, err := RenderClaude(nil, testManagedServers())
	if err != nil {
		t.Fatalf("RenderClaude() error = %v", err)
	}

	tomlOut, err := RenderCodex(nil, testManagedServers())
	if err != nil {
		t.Fatalf("RenderCodex() error = %v", err)
	}

	for name, out := range map[string][]byte{"claude": jsonOut, "codex": tomlOut} {
		if !bytes.Contains(out, []byte(want)) {
			t.Errorf("%s output does not pin the image digest %s:\n%s", name, want, out)
		}
	}
}

func TestRenderRejects(t *testing.T) {
	valid := testManagedServers()[0]
	tests := []struct {
		name    string
		render  func() ([]byte, error)
		wantErr string
	}{
		{
			name: "managed name without prefix",
			render: func() ([]byte, error) {
				return RenderClaude(nil, []ManagedServer{{Name: "filesystem", Launch: valid.Launch}})
			},
			wantErr: "must match",
		},
		{
			name: "managed name with uppercase",
			render: func() ([]byte, error) {
				return RenderClaude(nil, []ManagedServer{{Name: "opendepot-Filesystem", Launch: valid.Launch}})
			},
			wantErr: "must match",
		},
		{
			name: "duplicate managed name",
			render: func() ([]byte, error) {
				return RenderClaude(nil, []ManagedServer{valid, valid})
			},
			wantErr: "duplicate",
		},
		{
			name: "launch descriptor with shell wrapper",
			render: func() ([]byte, error) {
				bad := LaunchDescriptor{Command: "sh", Args: []string{"-c", "x"}, Transport: TransportStdio, Runtime: RuntimeNode}
				return RenderCopilot(nil, []ManagedServer{{Name: "opendepot-bad", Launch: bad}})
			},
			wantErr: testShellError,
		},
		{
			name: "malformed existing JSON",
			render: func() ([]byte, error) {
				return RenderClaude([]byte("{"), []ManagedServer{valid})
			},
			wantErr: "parse existing",
		},
		{
			name: "servers key is not an object",
			render: func() ([]byte, error) {
				return RenderClaude([]byte(`{"mcpServers": []}`), []ManagedServer{valid})
			},
			wantErr: "not an object",
		},
		{
			name: "malformed existing TOML",
			render: func() ([]byte, error) {
				return RenderCodex([]byte("[mcp_servers"), []ManagedServer{valid})
			},
			wantErr: "parse existing",
		},
		{
			name: "mcp_servers is not a table",
			render: func() ([]byte, error) {
				return RenderCodex([]byte(`mcp_servers = "x"`), []ManagedServer{valid})
			},
			wantErr: "is not a table",
		},
		{
			name: "custom path without a servers key",
			render: func() ([]byte, error) {
				return RenderCustom(nil, "", []ManagedServer{valid})
			},
			wantErr: "requires a servers key",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.render()
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("render error = %v, want it to contain %q", err, tc.wantErr)
			}
		})
	}
}

func TestRenderEmptyDesiredSetRemovesOnlyManagedEntries(t *testing.T) {
	out, err := RenderClaude(readTestData(t, "existing/claude.json"), nil)
	if err != nil {
		t.Fatalf("RenderClaude(nil) error = %v", err)
	}

	servers := decodeRawMap(t, decodeJSONObject(t, out)["mcpServers"])
	if len(servers) != 1 || servers["playwright"] == nil {
		t.Errorf("servers after clearing managed set = %d entries, want only playwright", len(servers))
	}
}

func decodeJSONObject(t *testing.T, data []byte) map[string]json.RawMessage {
	t.Helper()
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("output is not a JSON object: %v\n%s", err, data)
	}

	return doc
}

func decodeRawMap(t *testing.T, raw json.RawMessage) map[string]json.RawMessage {
	t.Helper()
	var out map[string]json.RawMessage
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("value is not an object: %v\n%s", err, raw)
	}

	return out
}
