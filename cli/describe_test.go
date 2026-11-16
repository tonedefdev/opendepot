package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDescribeResourceShowsVersionsAndAssessment(t *testing.T) {
	for _, kind := range []entryKind{kindSkill, kindAgent} {
		t.Run(kind.singular(), func(t *testing.T) {
			var authorization string
			a, host := catalogServer(t, func(w http.ResponseWriter, r *http.Request) {
				wantPath := "/opendepot/ui/v1/resources/acme/" + kind.singular() + "/reviewer"
				if r.URL.Path != wantPath {
					t.Errorf("path = %q, want %q", r.URL.Path, wantPath)
				}

				authorization = r.Header.Get("Authorization")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"kind": kind.singular(), "namespace": "acme", "name": "reviewer",
					"definitionContent": "not for terminal output",
					"versions": []map[string]any{
						{"version": "1.0.0", "synced": true},
						{"version": "2.0.0", "blocked": true, "blockReasons": []string{"policy threshold exceeded"}},
						{"commit": "abc123", "branchPhase": "Rejected", "syncStatus": "Superseded by a newer commit"},
					},
					"jevAssessment": map[string]any{
						"riskLevel": "High", "riskScore": 2.5, "safeProbability": 0.3,
						"needsReview": true, "blocked": true,
						"blockReasons": []string{"policy threshold exceeded"}, "error": "assessment incomplete",
					},
				})
			})
			a.tokenFor = func(string) string { return "test-token" }

			if err := a.describeResource(context.Background(), kind, "acme/reviewer", host, false); err != nil {
				t.Fatal(err)
			}

			if authorization != "Bearer test-token" {
				t.Fatal("request did not carry the registry credential")
			}

			out := fmt.Sprint(a.out)
			for _, want := range []string{
				"◆ acme/reviewer", kind.singular(), host, "1.0.0 · synced",
				"2.0.0 · blocked", "policy threshold exceeded", "no version assigned",
				"abc123", "rejected", "Superseded by a newer commit",
				"Latest Jev assessment", "High (2.5)", "Safe", "0.30",
				"Review       needed", "Blocked      yes", "assessment incomplete",
			} {
				if !strings.Contains(out, want) {
					t.Errorf("missing %q:\n%s", want, out)
				}
			}

			if strings.Contains(out, "not for terminal output") || strings.Contains(out, "\x1b[") {
				t.Fatalf("unexpected definition content or terminal escapes:\n%s", out)
			}
		})
	}
}

func TestDescribeResourceNoAssessmentOrVersions(t *testing.T) {
	a, host := catalogServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"kind": "skill", "namespace": "acme", "name": "lint"})
	})

	if err := a.describeResource(context.Background(), kindSkill, "acme/lint", host, false); err != nil {
		t.Fatal(err)
	}

	out := fmt.Sprint(a.out)
	for _, want := range []string{"Versions     none reported", "Assessment   none reported"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
}

func TestDescribeResourceRejectsInvalidNamesBeforeRequest(t *testing.T) {
	a, host := catalogServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("invalid resource must not trigger a request")
	})

	for _, resource := range []string{"", "acme", "/lint", "acme/", "acme/lint/extra", "../lint", "acme/..", "ACME/lint"} {
		if err := a.describeResource(context.Background(), kindSkill, resource, host, false); err == nil {
			t.Errorf("resource %q should be rejected", resource)
		}
	}
}

func TestDescribeResourceReportsErrorsWithoutPartialOutput(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"unauthorized", 401, "", "not authorized (HTTP 401)"},
		{"forbidden", 403, "", "access denied (HTTP 403)"},
		{"not found", 404, "", "not found or not visible"},
		{"server error", 500, "", "500"},
		{"invalid JSON", 200, "{", "decode response"},
		{"wrong identity", 200, `{"kind":"skill","namespace":"acme","name":"other"}`, "does not identify the requested resource"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, host := catalogServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			})

			err := a.describeResource(context.Background(), kindSkill, "acme/lint", host, false)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}

			if out := fmt.Sprint(a.out); out != "" {
				t.Fatalf("unexpected partial output: %q", out)
			}
		})
	}
}

func TestDescribeCommandsRequireOneResource(t *testing.T) {
	root := newRootCommand(strings.NewReader(""), io.Discard, io.Discard)
	for _, kind := range []string{"skill", "agent"} {
		cmd, _, err := root.Find([]string{"describe", kind})
		if err != nil {
			t.Fatal(err)
		}

		if cmd.Args(cmd, nil) == nil || cmd.Args(cmd, []string{"acme/one", "acme/two"}) == nil {
			t.Fatalf("describe %s must require exactly one resource", kind)
		}

		if err := cmd.Args(cmd, []string{"acme/one"}); err != nil {
			t.Fatalf("describe %s rejected one resource: %v", kind, err)
		}

		if cmd.InheritedFlags().Lookup("registry") == nil {
			t.Fatalf("describe %s is missing --registry", kind)
		}
	}
}

func TestDescribeUsesProjectRegistryAnonymously(t *testing.T) {
	a, host := catalogServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("anonymous request must not send an Authorization header")
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"kind": "agent", "namespace": "acme", "name": "reviewer",
			"jevAssessment": map[string]any{"riskLevel": "Low", "exempted": true},
		})
	})
	writeListProject(t, a.dir, "registry = \""+host+"\"")

	if err := a.describeResource(context.Background(), kindAgent, "acme/reviewer", "", false); err != nil {
		t.Fatal(err)
	}

	out := fmt.Sprint(a.out)
	for _, want := range []string{"Exempted     yes", "Error        none reported"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
}

func TestDescribeCommandRequestsOneResource(t *testing.T) {
	t.Setenv("OPENDEPOT_TOKEN", "")
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/opendepot/ui/v1/resources/acme/skill/lint" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}

		_ = json.NewEncoder(w).Encode(map[string]any{"kind": "skill", "namespace": "acme", "name": "lint"})
	}))
	t.Cleanup(server.Close)

	transport := http.DefaultTransport
	http.DefaultTransport = server.Client().Transport
	t.Cleanup(func() { http.DefaultTransport = transport })

	var out bytes.Buffer
	root := newRootCommand(strings.NewReader(""), &out, io.Discard)
	root.SetArgs([]string{"describe", "skill", "acme/lint", "--registry", strings.TrimPrefix(server.URL, "https://")})

	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(out.String(), "◆ acme/lint") {
		t.Fatalf("resource description missing:\n%s", out.String())
	}
}

// fullDescribeDetail is a registry detail that populates every field the CLI reads, including untrusted text with
// terminal escapes, control characters, bidi overrides, and line separators.
func fullDescribeDetail() map[string]any {
	return map[string]any{
		"kind": "agent", "namespace": "acme", "name": "reviewer",
		"latestVersion": "2.0.0", "synced": true, "syncStatus": "Synced",
		"hasUnsyncedVersions": true, "public": true,
		"blocked": true, "blockReasons": []string{"threshold exceeded"},
		"jevRiskLevel": "High", "sourceRef": "main", "sourceCommit": "abc123",
		"sourcePath": "agents/reviewer.md", "repoUrl": "https://github.com/acme/agents", "repoOwner": "acme",
		"lastScanned":        "2026-01-02T03:04:05Z",
		"scanCounts":         map[string]any{"critical": 0, "high": 1, "medium": 2, "low": 3, "unknown": 0, "exempted": 1},
		"totalDownloads":     42,
		"lastDownloadedAt":   "2026-01-03T00:00:00Z",
		"definitionFileName": "AGENT.md",
		"definitionContent":  "body not printed in table",
		"agentMetadata": map[string]any{
			"name": "Reviewer", "model": "gpt-test", "tools": []string{"read", "grep"},
			"description": "Reviews code.\x1b[31m Red\x1b[0m\u2028Second\x07 paragraph\n\nThird\u202e line",
		},
		"versions": []map[string]any{{
			"name": "reviewer-2", "version": "2.0.0", "synced": true, "branchPhase": "Synced",
			"fileName": "AGENT.md", "checksum": "sha256:abc", "archiveSizeBytes": 2048,
			"downloadCount": 7, "lastDownloadedAt": "2026-01-03T00:00:00Z", "lastScanned": "2026-01-02T03:04:05Z",
			"scanCounts":  map[string]any{"critical": 0, "high": 0, "medium": 0, "low": 0, "unknown": 0, "exempted": 0},
			"schemaState": "Valid", "schemaMessage": "ok", "schemaAttemptedAt": "2026-01-02T03:00:00Z",
		}},
		"jevAssessment": map[string]any{
			"riskLevel": "High", "riskScore": 2.5, "safeProbability": 0.3,
			"needsReview": true, "blocked": true, "blockReasons": []string{"policy threshold exceeded"},
		},
		"jevExemptionGovernance": map[string]any{
			"governingPolicy": "strict", "governingPriority": 10, "matchingPolicy": "exempt-reviewer",
			"matchingPriority": 5, "matchingExpires": "2030-01-01T00:00:00Z", "matchingState": "active",
			"superseded": true,
		},
		"jevUnblock":    map[string]any{"state": "blocked", "digest": "sha256:d1", "governingPolicy": "strict"},
		"jevThresholds": map[string]any{"minSafeProbability": 0.5, "maxRiskScore": 3},
		"storageConfig": map[string]any{
			"backend": "s3", "bucket": "acme-bucket", "region": "us-east-1", "key": "skills/lint.zip",
			"presignEnabled": true, "presignTTL": "15m",
		},
		"githubConfig": map[string]any{"useAuthenticatedClient": true},
		"sourceScanFindings": []map[string]any{{
			"vulnerabilityID": "CVE-2026-0001", "pkgName": "lodash", "installedVersion": "4.17.20",
			"fixedVersion": "4.17.21", "severity": "HIGH",
			"title":    "Prototype \x1b[31mpollution\x1b[0m\u202e",
			"exempted": true, "exemptionReason": "accepted\x07", "exemptedBy": "policy-a",
		}},
	}
}

func TestDescribeResourceShowsEveryFieldInTable(t *testing.T) {
	a, host := catalogServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(fullDescribeDetail())
	})
	a.tokenFor = func(string) string { return "test-token" }

	if err := a.describeResource(context.Background(), kindAgent, "acme/reviewer", host, false); err != nil {
		t.Fatal(err)
	}

	out := fmt.Sprint(a.out)
	for _, want := range []string{
		"Latest       2.0.0", "synced · Synced", "Unsynced     yes", "Blocked      yes", "threshold exceeded",
		"Public       yes", "Risk         High", "https://github.com/acme/agents", "agents/reviewer.md", "abc123",
		"Owner        acme", "Ref          main", "File         AGENT.md", "Name         Reviewer", "Model        gpt-test",
		"Tools        read, grep", "Description  Reviews code. Red Second paragraph", "Third line",
		"Storage      s3", "Bucket       acme-bucket", "Region       us-east-1", "Key          skills/lint.zip",
		"Presign      enabled", "Presign TTL  15m", "GitHub auth  yes", "Findings     1 from source scan",
		"Finding      HIGH · CVE-2026-0001", "Package      lodash 4.17.20", "Fixed in     4.17.21",
		"Title        Prototype pollution", "Exempted     yes · policy-a · accepted",
		"Downloads    42", "critical 0 · high 1 · medium 2 · low 3 · unknown 0 · exempted 1", "2026-01-02T03:04:05Z",
		"2.0.0 · synced", "sha256:abc", "2048 bytes", "Schema       Valid · ok · 2026-01-02T03:00:00Z",
		"Governed by  strict", "Expires      2030-01-01T00:00:00Z", "Superseded   yes", "Unblock      blocked",
		"sha256:d1", "Threshold    min safe 0.50", "Threshold    max risk 3.00",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}

	for _, banned := range []string{"\x1b", "\x07", "\u2028", "\u202e", "body not printed in table"} {
		if strings.Contains(out, banned) {
			t.Fatalf("output contains %q:\n%s", banned, out)
		}
	}
}

func TestDescribeResourceJSONHasStableKeys(t *testing.T) {
	a, host := catalogServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(fullDescribeDetail())
	})
	a.tokenFor = func(string) string { return "test-token" }

	if err := a.describeResource(context.Background(), kindAgent, "acme/reviewer", host, true); err != nil {
		t.Fatal(err)
	}

	var got map[string]any
	if err := json.Unmarshal([]byte(fmt.Sprint(a.out)), &got); err != nil {
		t.Fatalf("output is not one JSON object: %v\n%s", err, a.out)
	}

	for _, key := range []string{
		"kind", "namespace", "name", "latestVersion", "synced", "syncStatus", "hasUnsyncedVersions", "public",
		"blocked", "blockReasons", "jevRiskLevel", "sourceRef", "sourceCommit", "sourcePath", "repoUrl", "repoOwner",
		"scanCounts", "lastScanned", "totalDownloads", "lastDownloadedAt", "definitionFileName", "definitionContent",
		"agentMetadata", "versions", "jevAssessment", "jevExemptionGovernance", "jevUnblock", "jevThresholds",
		"storageConfig", "githubConfig", "sourceScanFindings",
	} {
		if _, ok := got[key]; !ok {
			t.Errorf("JSON missing key %q", key)
		}
	}

	if got["definitionContent"] != "body not printed in table" {
		t.Errorf("definitionContent = %v", got["definitionContent"])
	}

	metadata := got["agentMetadata"].(map[string]any)
	if metadata["description"] != "Reviews code.\x1b[31m Red\x1b[0m\u2028Second\x07 paragraph\n\nThird\u202e line" {
		t.Errorf("agentMetadata.description = %q, want the full raw text", metadata["description"])
	}

	storage := got["storageConfig"].(map[string]any)
	for _, key := range []string{"backend", "bucket", "region", "key", "directoryPath", "accountName", "accountUrl", "subscriptionID", "resourceGroup", "presignEnabled", "presignTTL"} {
		if _, ok := storage[key]; !ok {
			t.Errorf("storageConfig JSON missing key %q", key)
		}
	}

	findings := got["sourceScanFindings"].([]any)
	finding := findings[0].(map[string]any)
	for _, key := range []string{"vulnerabilityID", "pkgName", "installedVersion", "fixedVersion", "severity", "title", "exempted", "exemptionReason", "exemptedBy"} {
		if _, ok := finding[key]; !ok {
			t.Errorf("sourceScanFindings JSON missing key %q", key)
		}
	}

	if finding["title"] != "Prototype \x1b[31mpollution\x1b[0m\u202e" {
		t.Errorf("finding title = %q, want the full raw text", finding["title"])
	}

	versions := got["versions"].([]any)
	first := versions[0].(map[string]any)
	for _, key := range []string{"name", "version", "synced", "syncStatus", "branchPhase", "blocked", "blockReasons", "checksum", "archiveSizeBytes", "schemaState"} {
		if _, ok := first[key]; !ok {
			t.Errorf("version JSON missing key %q", key)
		}
	}
}

func TestDescribeResourceEmptyFields(t *testing.T) {
	a, host := catalogServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"kind": "skill", "namespace": "acme", "name": "lint"})
	})
	a.tokenFor = func(string) string { return "test-token" }

	if err := a.describeResource(context.Background(), kindSkill, "acme/lint", host, false); err != nil {
		t.Fatal(err)
	}

	out := fmt.Sprint(a.out)
	for _, banned := range []string{"Description", "Downloads", "Checksum", "Governed by", "Threshold", "Tools", "Unsynced", "Blocked", "Public", "Storage", "GitHub auth", "Findings"} {
		if strings.Contains(out, banned) {
			t.Fatalf("empty field %q written:\n%s", banned, out)
		}
	}

	for _, want := range []string{"Versions     none reported", "Assessment   none reported", "Sync         not synced"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}

	a.out = new(bytes.Buffer)
	if err := a.describeResource(context.Background(), kindSkill, "acme/lint", host, true); err != nil {
		t.Fatal(err)
	}

	var got map[string]any
	if err := json.Unmarshal([]byte(fmt.Sprint(a.out)), &got); err != nil {
		t.Fatal(err)
	}

	if versions, ok := got["versions"].([]any); !ok || len(versions) != 0 {
		t.Errorf("versions = %#v, want empty array", got["versions"])
	}

	if reasons, ok := got["blockReasons"].([]any); !ok || len(reasons) != 0 {
		t.Errorf("blockReasons = %#v, want empty array", got["blockReasons"])
	}

	for _, key := range []string{"agentMetadata", "scanCounts", "jevAssessment", "jevUnblock", "jevThresholds", "storageConfig", "githubConfig"} {
		if v, ok := got[key]; !ok || v != nil {
			t.Errorf("%s = %#v, want present null", key, v)
		}
	}

	if findings, ok := got["sourceScanFindings"].([]any); !ok || len(findings) != 0 {
		t.Errorf("sourceScanFindings = %#v, want empty array", got["sourceScanFindings"])
	}
}

func TestSanitizeTextForDetails(t *testing.T) {
	cases := map[string]string{
		"a\u2028b\u2029c":                      "a b c",
		"  x\r\ny  \n\n":                       "x\ny",
		"\x1b[2Jtext\x00":                      "text",
		"line1\n\n\nline2":                     "line1\n\n\nline2",
		"\x1b]8;;http://x\x07link\x1b]8;;\x07": "link",
	}

	for in, want := range cases {
		if got := sanitizeText(in); got != want {
			t.Errorf("sanitizeText(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDescribeCommandJSONFlag(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"kind": "skill", "namespace": "acme", "name": "lint"})
	}))
	t.Cleanup(server.Close)

	transport := http.DefaultTransport
	http.DefaultTransport = server.Client().Transport
	t.Cleanup(func() { http.DefaultTransport = transport })

	var out bytes.Buffer
	root := newRootCommand(strings.NewReader(""), &out, io.Discard)
	root.SetArgs([]string{"describe", "skill", "acme/lint", "--json", "--registry", strings.TrimPrefix(server.URL, "https://")})

	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}

	if !strings.HasPrefix(out.String(), "{") {
		t.Fatalf("expected JSON object:\n%s", out.String())
	}
}
