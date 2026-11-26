package mcp

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// Keys and values used to build server.json fixtures.
const (
	keyName         = "name"
	keyDescription  = "description"
	keyVersion      = "version"
	keyRegistryType = "registryType"
	keyIdentifier   = "identifier"
	keyTransport    = "transport"
	keyType         = "type"
	keyURL          = "url"
	descriptionText = "A server"
	serverName      = "io.example/server"
)

// serverDoc marshals fields into a server.json document. It fills in the required fields unless a test overrides them.
func serverDoc(t *testing.T, fields map[string]any) []byte {
	t.Helper()
	doc := map[string]any{
		keyName:        serverName,
		keyDescription: descriptionText,
		keyVersion:     testVersion,
	}
	for k, v := range fields {
		doc[k] = v
	}

	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}

	return data
}

// packageDoc is a server.json with one packages[] entry.
func packageDoc(t *testing.T, pkg map[string]any) []byte {
	t.Helper()
	return serverDoc(t, map[string]any{"packages": []any{pkg}})
}

// remoteDoc is a server.json with one remotes[] entry.
func remoteDoc(t *testing.T, remote map[string]any) []byte {
	t.Helper()
	return serverDoc(t, map[string]any{"remotes": []any{remote}})
}

// transportOf returns the transport object for a transport type.
func transportOf(transportType string) map[string]any {
	return map[string]any{keyType: transportType}
}

func TestParseServerJSONValidFixture(t *testing.T) {
	data, err := os.ReadFile("testdata/server-valid.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	doc, err := ParseServerJSON(data)
	if err != nil {
		t.Fatalf("ParseServerJSON(valid fixture) error = %v", err)
	}

	if doc.Name != "io.github.tonedefdev/opendepot-search" || doc.Version != "1.2.0" {
		t.Errorf("required fields = %q %q, want name and version from fixture", doc.Name, doc.Version)
	}

	if doc.Title != "OpenDepot Search" || doc.WebsiteURL != "https://opendepot.example.com" {
		t.Errorf("optional top-level fields not parsed: title=%q websiteUrl=%q", doc.Title, doc.WebsiteURL)
	}

	if doc.Repository == nil || doc.Repository.Subfolder != "services/mcp/search" {
		t.Errorf("repository not parsed: %+v", doc.Repository)
	}

	if len(doc.Icons) != 1 || doc.Icons[0].Theme != "dark" {
		t.Errorf("icons not parsed: %+v", doc.Icons)
	}

	if _, ok := doc.Meta["io.modelcontextprotocol.registry/publisher-provided"]; !ok {
		t.Errorf("_meta not parsed: %+v", doc.Meta)
	}

	if len(doc.Packages) != 2 || doc.Packages[1].Transport.Type != TransportStreamableHTTP {
		t.Errorf("packages not parsed: %+v", doc.Packages)
	}

	if len(doc.Remotes) != 1 || doc.Remotes[0].Type != TransportSSE {
		t.Errorf("remotes not parsed: %+v", doc.Remotes)
	}
}

func TestParseServerJSONAcceptsMinimalDocument(t *testing.T) {
	if _, err := ParseServerJSON(serverDoc(t, nil)); err != nil {
		t.Fatalf("ParseServerJSON(minimal) error = %v", err)
	}
}

func TestParseServerJSONRejects(t *testing.T) {
	stdio := transportOf(TransportStdio)
	noRegistryType := map[string]any{keyIdentifier: "x", keyTransport: stdio}
	badRegistryType := map[string]any{keyRegistryType: "cargo", keyIdentifier: "x", keyTransport: stdio}
	noIdentifier := map[string]any{keyRegistryType: RegistryTypeNPM, keyTransport: stdio}
	noTransport := map[string]any{keyRegistryType: RegistryTypeNPM, keyIdentifier: "x"}
	badTransport := map[string]any{
		keyRegistryType: RegistryTypeNPM, keyIdentifier: "x", keyTransport: transportOf("websocket"),
	}
	httpNoURL := map[string]any{
		keyRegistryType: RegistryTypeOCI, keyIdentifier: "x", keyTransport: transportOf(TransportStreamableHTTP),
	}
	badSHA := map[string]any{
		keyRegistryType: RegistryTypeNPM, keyIdentifier: "x", "fileSha256": "ABC", keyTransport: stdio,
	}

	tests := []struct {
		name    string
		data    []byte
		wantErr string
	}{
		{
			name:    "missing name",
			data:    []byte(`{"description":"A server","version":"1.0.0"}`),
			wantErr: keyName,
		},
		{
			name:    "name without namespace slash",
			data:    serverDoc(t, map[string]any{keyName: "nonamespace"}),
			wantErr: keyName,
		},
		{
			name:    "name with disallowed character",
			data:    serverDoc(t, map[string]any{keyName: "io.example/server name"}),
			wantErr: keyName,
		},
		{
			name:    "name longer than 200 characters",
			data:    serverDoc(t, map[string]any{keyName: "io.example/" + strings.Repeat("a", 190)}),
			wantErr: keyName,
		},
		{
			name:    "missing description",
			data:    []byte(`{"name":"io.example/server","version":"1.0.0"}`),
			wantErr: keyDescription,
		},
		{
			name:    "missing version",
			data:    []byte(`{"name":"io.example/server","description":"A server"}`),
			wantErr: "version is required",
		},
		{
			name:    "version range",
			data:    serverDoc(t, map[string]any{keyVersion: "^1.2.3"}),
			wantErr: "not a range",
		},
		{
			name: "draft schema rejected",
			data: serverDoc(t, map[string]any{
				"$schema": "https://static.modelcontextprotocol.io/schemas/draft/server.schema.json",
			}),
			wantErr: "not the supported schema",
		},
		{
			name:    "package missing registryType",
			data:    packageDoc(t, noRegistryType),
			wantErr: keyRegistryType,
		},
		{
			name:    "package with unknown registryType",
			data:    packageDoc(t, badRegistryType),
			wantErr: "registryType \"cargo\"",
		},
		{
			name:    "package missing identifier",
			data:    packageDoc(t, noIdentifier),
			wantErr: keyIdentifier,
		},
		{
			name:    "package missing transport",
			data:    packageDoc(t, noTransport),
			wantErr: "transport type",
		},
		{
			name:    "package with unknown transport type",
			data:    packageDoc(t, badTransport),
			wantErr: "transport type \"websocket\"",
		},
		{
			name:    "streamable-http package without url",
			data:    packageDoc(t, httpNoURL),
			wantErr: "must be an http(s) URL",
		},
		{
			name:    "bad fileSha256",
			data:    packageDoc(t, badSHA),
			wantErr: "fileSha256",
		},
		{
			name:    "remote with stdio transport",
			data:    remoteDoc(t, transportOf(TransportStdio)),
			wantErr: "remotes[0]",
		},
		{
			name:    "remote without url",
			data:    remoteDoc(t, transportOf(TransportSSE)),
			wantErr: "remotes[0]",
		},
		{
			name:    "repository without source",
			data:    serverDoc(t, map[string]any{"repository": map[string]any{keyURL: "https://github.com/example/repo"}}),
			wantErr: "repository requires url and source",
		},
		{
			name:    "malformed JSON",
			data:    []byte(`{"name":`),
			wantErr: "parse server.json",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseServerJSON(tc.data)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("ParseServerJSON() error = %v, want it to contain %q", err, tc.wantErr)
			}
		})
	}
}

func TestServerJSONReportsEveryProblem(t *testing.T) {
	doc := serverDoc(t, map[string]any{
		keyName:        "bad",
		keyDescription: "",
		keyVersion:     "",
		"packages":     []any{map[string]any{keyIdentifier: "", keyTransport: transportOf(TransportStdio)}},
	})

	_, err := ParseServerJSON(doc)
	if err == nil {
		t.Fatal("ParseServerJSON() error = nil, want several problems")
	}

	for _, want := range []string{keyName, keyDescription, "version is required", keyRegistryType, keyIdentifier} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %v does not report %q", err, want)
		}
	}
}

func TestValidNPMPackageIsAccepted(t *testing.T) {
	pkg := map[string]any{
		keyRegistryType: RegistryTypeNPM,
		keyIdentifier:   "@scope/pkg",
		keyTransport:    transportOf(TransportStdio),
	}
	if _, err := ParseServerJSON(packageDoc(t, pkg)); err != nil {
		t.Fatalf("ParseServerJSON(valid npm package) error = %v", err)
	}
}
