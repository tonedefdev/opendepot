package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// catalogServer serves handler as the registry host and returns an app that talks to it.
func catalogServer(t *testing.T, handler http.HandlerFunc) (*app, string) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	host := strings.TrimPrefix(srv.URL, "http://")
	a := &app{
		in:         bufio.NewReader(strings.NewReader("")),
		out:        &bytes.Buffer{},
		httpClient: srv.Client(),
		scheme:     "http",
		dir:        t.TempDir(),
		tokenFor:   func(string) string { return "" },
	}

	return a, host
}

func catalogEntry(kind, ns, name, status string) map[string]any {
	return map[string]any{"kind": kind, "namespace": ns, "name": name, "status": status}
}

// catalogJSON writes a catalog page with the given items and total count.
func catalogJSON(w http.ResponseWriter, items []map[string]any, total, page, pageSize int) {
	w.Header().Set("Content-Type", "application/json")
	if items == nil {
		items = []map[string]any{}
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"items":      items,
		"totalCount": total,
		"page":       page,
		"pageSize":   pageSize,
	})
}

// pagedHandler serves all items in pages of size 2, so following pages is required.
func pagedHandler(t *testing.T, all []map[string]any, seenPages *[]int, seenNamespaces *[]string) http.HandlerFunc {
	t.Helper()
	var mu sync.Mutex

	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != catalogPath {
			http.NotFound(w, r)
			return
		}

		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		mu.Lock()
		*seenPages = append(*seenPages, page)
		*seenNamespaces = append(*seenNamespaces, r.URL.Query().Get("namespace"))
		mu.Unlock()

		start := min((page-1)*2, len(all))
		end := min(start+2, len(all))
		catalogJSON(w, all[start:end], len(all), page, 2)
	}
}

func TestListFollowsEveryPageAndFiltersKind(t *testing.T) {
	all := []map[string]any{
		catalogEntry("skill", "acme", "lint", "available"),
		catalogEntry("agent", "acme", "reviewer", "available"),
		catalogEntry("skill", "acme", "docs", "available"),
		catalogEntry("skill", "acme", "tests", "available"),
		catalogEntry("skill", "acme", "style", "available"),
	}

	var pages []int
	var namespaces []string
	a, host := catalogServer(t, pagedHandler(t, all, &pages, &namespaces))

	if err := a.listCatalog(context.Background(), kindSkill, listOptions{registry: host}); err != nil {
		t.Fatal(err)
	}

	if got := strings.Join(intsToStrings(pages), ","); got != "1,2,3" {
		t.Fatalf("pages requested = %s, want 1,2,3", got)
	}

	out := a.out.(*bytes.Buffer).String()
	for _, name := range []string{"acme/lint", "acme/docs", "acme/tests", "acme/style"} {
		if !strings.Contains(out, name) {
			t.Fatalf("output missing %s:\n%s", name, out)
		}
	}

	if strings.Contains(out, "acme/reviewer") {
		t.Fatalf("agent listed under skills:\n%s", out)
	}
}

func TestListOneRowPerItem(t *testing.T) {
	all := []map[string]any{
		catalogEntry("skill", "acme", "lint", "available"),
		catalogEntry("skill", "acme", "docs", "blocked"),
	}

	var pages []int
	var namespaces []string
	a, host := catalogServer(t, pagedHandler(t, all, &pages, &namespaces))

	if err := a.listCatalog(context.Background(), kindSkill, listOptions{registry: host}); err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(strings.TrimSpace(a.out.(*bytes.Buffer).String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want header plus 2 rows:\n%s", len(lines), strings.Join(lines, "\n"))
	}
}

func TestListNamespaceQueryParam(t *testing.T) {
	var pages []int
	var namespaces []string
	a, host := catalogServer(t, pagedHandler(t, []map[string]any{catalogEntry("skill", "acme", "lint", "available")}, &pages, &namespaces))

	if err := a.listCatalog(context.Background(), kindSkill, listOptions{registry: host, namespace: "acme"}); err != nil {
		t.Fatal(err)
	}

	if len(namespaces) == 0 {
		t.Fatal("no catalog requests were made")
	}

	for _, ns := range namespaces {
		if ns != "acme" {
			t.Fatalf("namespace query = %q, want acme", ns)
		}
	}
}

func TestListRendersUnknownRiskAndKnownRisk(t *testing.T) {
	score := 0.2
	known := catalogEntry("skill", "acme", "lint", "available")
	known["latestVersion"] = "1.0.0"
	known["riskLevel"] = "Low"
	known["riskScore"] = score

	unknown := catalogEntry("skill", "acme", "docs", "available")
	unknown["latestVersion"] = "1.0.0"

	var pages []int
	var namespaces []string
	a, host := catalogServer(t, pagedHandler(t, []map[string]any{known, unknown}, &pages, &namespaces))

	if err := a.listCatalog(context.Background(), kindSkill, listOptions{registry: host}); err != nil {
		t.Fatal(err)
	}

	out := a.out.(*bytes.Buffer).String()
	if !strings.Contains(out, "Low (0.2)") {
		t.Fatalf("known risk not rendered:\n%s", out)
	}

	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "acme/docs") && !strings.Contains(line, "unknown") {
			t.Fatalf("omitted risk not rendered as unknown: %q", line)
		}
	}
}

func TestPrintCatalogSeparatesAvailableVersionFromRejectedCandidate(t *testing.T) {
	var out bytes.Buffer
	a := &app{out: &out}
	a.printCatalog([]catalogItem{{
		Namespace:     "acme",
		Name:          "reviewer",
		LatestVersion: "1.0.0",
		Status:        "rejected",
		UnavailableCandidate: &unavailableCandidate{
			Commit:  "3f2a9c1",
			Status:  "rejected",
			Reasons: []string{"Superseded by a newer commit"},
		},
	}})

	lines := strings.Split(out.String(), "\n")
	if !strings.Contains(lines[0], "VERSION") ||
		!strings.Contains(lines[1], "1.0.0") ||
		!strings.Contains(lines[1], "available") ||
		!strings.Contains(lines[1], "candidate rejected") {
		t.Fatalf("available version and rejected candidate not distinguished:\n%s", out.String())
	}

	if len(lines) != 3 || strings.Contains(out.String(), "3f2a9c1") || strings.Contains(out.String(), "Superseded") {
		t.Fatalf("default output should have one row without candidate details:\n%s", out.String())
	}
}

func TestPrintCatalogAlignsDescriptionAfterCandidateNote(t *testing.T) {
	var out bytes.Buffer
	a := &app{out: &out}
	a.printCatalog([]catalogItem{
		{Namespace: "acme", Name: "plain", LatestVersion: "1.0.0", Status: "available", Description: "Plain description"},
		{
			Namespace:     "acme",
			Name:          "reviewer",
			LatestVersion: "1.0.0",
			Status:        "rejected",
			Description:   "Reviewer description",
			UnavailableCandidate: &unavailableCandidate{
				Commit: "3f2a9c1",
				Status: "rejected",
			},
		},
	})

	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("want header and two rows:\n%s", out.String())
	}

	headerColumn := len([]rune(lines[0][:strings.Index(lines[0], "DESCRIPTION")]))
	for i, text := range []string{"Plain description", "Reviewer description"} {
		column := len([]rune(lines[i+1][:strings.Index(lines[i+1], text)]))
		if column != headerColumn {
			t.Fatalf("description starts at column %d, want %d:\n%s", column, headerColumn, out.String())
		}
	}

	if !strings.Contains(lines[2], "candidate rejected") {
		t.Fatalf("candidate note missing from row:\n%s", out.String())
	}
}

func TestPrintCatalogKeepsLongSourcesOnOneLine(t *testing.T) {
	var out bytes.Buffer
	a := &app{out: &out}
	source := "opendepot-system/" + strings.Repeat("long-name-", 5)
	a.printCatalog([]catalogItem{
		{Namespace: "acme", Name: "short", LatestVersion: "1.0.0", Status: "available"},
		{
			Namespace: "opendepot-system",
			Name:      strings.Repeat("long-name-", 5),
			Status:    "blocked",
			UnavailableCandidate: &unavailableCandidate{
				Version: "2.0.0",
				Status:  "blocked",
				Reasons: []string{"policy threshold was exceeded"},
			},
		},
	})

	if !strings.Contains(out.String(), source+"  ") {
		t.Fatalf("long source was lost or truncated:\n%s", out.String())
	}

}

func TestListDoesNotAcceptDetailsFlag(t *testing.T) {
	root := newRootCommand(strings.NewReader(""), io.Discard, io.Discard)
	for _, kind := range []string{"skills", "agents"} {
		cmd, _, err := root.Find([]string{"list", kind})
		if err != nil {
			t.Fatal(err)
		}

		if flag := cmd.InheritedFlags().Lookup("details"); flag != nil {
			t.Fatalf("list %s: --details should not be available", kind)
		}
	}
}

func TestListEmptyResult(t *testing.T) {
	a, host := catalogServer(t, func(w http.ResponseWriter, r *http.Request) {
		catalogJSON(w, nil, 0, 1, 100)
	})

	if err := a.listCatalog(context.Background(), kindSkill, listOptions{registry: host}); err != nil {
		t.Fatal(err)
	}

	want := "no skills found on " + host + "\n"
	if got := a.out.(*bytes.Buffer).String(); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestListEmptyNamespaceResult(t *testing.T) {
	a, host := catalogServer(t, func(w http.ResponseWriter, r *http.Request) {
		catalogJSON(w, nil, 0, 1, 100)
	})

	if err := a.listCatalog(context.Background(), kindAgent, listOptions{registry: host, namespace: "acme"}); err != nil {
		t.Fatal(err)
	}

	if out := a.out.(*bytes.Buffer).String(); !strings.Contains(out, `no agents found on`+" "+host+` in namespace "acme"`) {
		t.Fatalf("output = %q", out)
	}
}

func TestListAuthFailures(t *testing.T) {
	for _, code := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		a, host := catalogServer(t, func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "unauthorized", code)
		})

		err := a.listCatalog(context.Background(), kindSkill, listOptions{registry: host})
		if err == nil {
			t.Fatalf("HTTP %d: expected an error", code)
		}

		want := "not authorized (HTTP " + strconv.Itoa(code) + "); run 'opendepot login " + host + "' and retry"
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("HTTP %d: error = %v", code, err)
		}

		if out := a.out.(*bytes.Buffer).String(); out != "" {
			t.Fatalf("HTTP %d: output presented on failure: %q", code, out)
		}
	}
}

func TestListOldServerNotFound(t *testing.T) {
	a, host := catalogServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})

	err := a.listCatalog(context.Background(), kindSkill, listOptions{registry: host})
	if err == nil || !strings.Contains(err.Error(), "does not support the agents.v1 catalog (HTTP 404)") {
		t.Fatalf("error = %v", err)
	}
}

func TestListServerFailureShowsNoPartialOutput(t *testing.T) {
	all := []map[string]any{
		catalogEntry("skill", "acme", "lint", "available"),
		catalogEntry("skill", "acme", "docs", "available"),
		catalogEntry("skill", "acme", "tests", "available"),
	}

	a, host := catalogServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "2" {
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		catalogJSON(w, all[:2], len(all), 1, 2)
	})

	err := a.listCatalog(context.Background(), kindSkill, listOptions{registry: host})
	if err == nil || !strings.Contains(err.Error(), "failed to list the catalog (HTTP 500); no results were shown") {
		t.Fatalf("error = %v", err)
	}

	if out := a.out.(*bytes.Buffer).String(); out != "" {
		t.Fatalf("partial output presented as complete: %q", out)
	}
}

func TestListSendsRegistryCredentials(t *testing.T) {
	var gotAuth string
	a, host := catalogServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		catalogJSON(w, nil, 0, 1, 100)
	})
	a.tokenFor = func(string) string { return "secret-token" }

	if err := a.listCatalog(context.Background(), kindSkill, listOptions{registry: host}); err != nil {
		t.Fatal(err)
	}

	if gotAuth != "Bearer secret-token" {
		t.Fatalf("Authorization = %q", gotAuth)
	}
}

func TestListRegistryResolution(t *testing.T) {
	a, host := catalogServer(t, func(w http.ResponseWriter, r *http.Request) {
		catalogJSON(w, nil, 0, 1, 100)
	})

	t.Run("explicit registry without a project file", func(t *testing.T) {
		if err := a.listCatalog(context.Background(), kindSkill, listOptions{registry: host}); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("project default registry", func(t *testing.T) {
		writeListProject(t, a.dir, "registry = \""+host+"\"")
		if err := a.listCatalog(context.Background(), kindSkill, listOptions{}); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("single host inferred from sources", func(t *testing.T) {
		writeListProject(t, a.dir, "")
		src := "agent_skill \"lint\" {\n  source = \"" + host + "/acme/lint\"\n  version = \"1.0.0\"\n}\n"
		appendProject(t, a.dir, src)
		if err := a.listCatalog(context.Background(), kindSkill, listOptions{}); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("no registry is an incomplete request", func(t *testing.T) {
		empty := t.TempDir()
		a.dir = empty
		err := a.listCatalog(context.Background(), kindSkill, listOptions{})
		if err == nil || !strings.Contains(err.Error(), "pass --registry") {
			t.Fatalf("error = %v", err)
		}
	})
}

func TestListNoColorEmitsNoEscapeCodes(t *testing.T) {
	t.Setenv("NO_COLOR", "1")

	item := catalogEntry("skill", "acme", "lint", "blocked")
	item["latestVersion"] = "1.0.0"
	item["riskLevel"] = "High"
	item["unavailableCandidate"] = map[string]any{"version": "2.0.0", "status": "blocked"}

	var pages []int
	var namespaces []string
	a, host := catalogServer(t, pagedHandler(t, []map[string]any{item}, &pages, &namespaces))

	if err := a.listCatalog(context.Background(), kindSkill, listOptions{registry: host}); err != nil {
		t.Fatal(err)
	}

	if out := a.out.(*bytes.Buffer).String(); strings.Contains(out, "\x1b[") {
		t.Fatalf("NO_COLOR output contains escape codes: %q", out)
	}
}

func TestListWritesNothing(t *testing.T) {
	isolateHome(t)

	item := catalogEntry("skill", "acme", "lint", "available")
	item["latestVersion"] = "1.0.0"

	var pages []int
	var namespaces []string
	a, host := catalogServer(t, pagedHandler(t, []map[string]any{item}, &pages, &namespaces))
	writeListProject(t, a.dir, "registry = \""+host+"\"")

	before := snapshotDir(t, a.dir)
	home := os.Getenv("HOME")
	homeBefore := snapshotDir(t, home)

	if err := a.listCatalog(context.Background(), kindSkill, listOptions{}); err != nil {
		t.Fatal(err)
	}

	if after := snapshotDir(t, a.dir); after != before {
		t.Fatalf("project directory changed:\nbefore: %s\nafter:  %s", before, after)
	}

	if after := snapshotDir(t, home); after != homeBefore {
		t.Fatalf("home directory changed:\nbefore: %s\nafter:  %s", homeBefore, after)
	}

	assertNoWrites(t, a.dir)
}

// snapshotDir returns a stable description of every entry under dir, including file contents.
func snapshotDir(t *testing.T, dir string) string {
	t.Helper()
	var b strings.Builder
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, _ := filepath.Rel(dir, p)
		if d.IsDir() {
			b.WriteString("d " + rel + "\n")
			return nil
		}

		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}

		b.WriteString("f " + rel + " " + string(data) + "\n")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	return b.String()
}

func writeListProject(t *testing.T, dir, registryAttr string) {
	t.Helper()
	body := "opendepot {\n  targets = [\"copilot\"]\n"
	if registryAttr != "" {
		body += "  " + registryAttr + "\n"
	}
	body += "}\n"

	if err := os.WriteFile(filepath.Join(dir, configFileName), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func appendProject(t *testing.T, dir, body string) {
	t.Helper()
	f, err := os.OpenFile(filepath.Join(dir, configFileName), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	if _, err := io.WriteString(f, body); err != nil {
		t.Fatal(err)
	}
}

func intsToStrings(vals []int) []string {
	out := make([]string, len(vals))
	for i, v := range vals {
		out[i] = strconv.Itoa(v)
	}

	return out
}

func TestListJSONCarriesFullDescription(t *testing.T) {
	full := "Lints code.\nSecond line with detail"
	described := catalogEntry("skill", "acme", "lint", "available")
	described["latestVersion"] = "1.0.0"
	described["description"] = full

	var pages []int
	var namespaces []string
	a, host := catalogServer(t, pagedHandler(t, []map[string]any{described, catalogEntry("skill", "acme", "docs", "blocked")}, &pages, &namespaces))

	if err := a.listCatalog(context.Background(), kindSkill, listOptions{registry: host, json: true}); err != nil {
		t.Fatal(err)
	}

	out := a.out.(*bytes.Buffer).String()
	if strings.Contains(out, "SOURCE") {
		t.Fatalf("--json printed the table:\n%s", out)
	}

	var got struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("--json output is not valid JSON: %v\n%s", err, out)
	}

	if len(got.Items) != 2 {
		t.Fatalf("items = %d, want 2:\n%s", len(got.Items), out)
	}

	if got.Items[0]["description"] != full {
		t.Fatalf("description = %q, want full text %q", got.Items[0]["description"], full)
	}

	if v, ok := got.Items[1]["description"]; !ok || v != "" {
		t.Fatalf("description for item without one = %v (present %v), want empty string", v, ok)
	}
}

func TestListJSONEmptyResultIsEmptyArray(t *testing.T) {
	a, host := catalogServer(t, func(w http.ResponseWriter, r *http.Request) {
		catalogJSON(w, nil, 0, 1, 100)
	})

	if err := a.listCatalog(context.Background(), kindAgent, listOptions{registry: host, json: true}); err != nil {
		t.Fatal(err)
	}

	var got struct {
		Items []catalogItem `json:"items"`
	}
	if err := json.Unmarshal(a.out.(*bytes.Buffer).Bytes(), &got); err != nil {
		t.Fatalf("--json output is not valid JSON: %v", err)
	}

	if got.Items == nil || len(got.Items) != 0 {
		t.Fatalf("items = %v, want an empty array", got.Items)
	}
}

func TestPrintCatalogShowsDescriptionColumn(t *testing.T) {
	var out bytes.Buffer
	a := &app{out: &out}
	a.printCatalog([]catalogItem{{Namespace: "acme", Name: "lint", LatestVersion: "1.0.0", Status: "available", Description: "Lints code"}})

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 || !strings.HasSuffix(lines[0], "DESCRIPTION") || !strings.HasSuffix(lines[1], "  Lints code") {
		t.Fatalf("table does not show the description column:\n%s", out.String())
	}
}

func TestPrintCatalogDescriptionCannotAddRowsOrEscapes(t *testing.T) {
	var out bytes.Buffer
	a := &app{out: &out}
	a.printCatalog([]catalogItem{{
		Namespace:     "acme",
		Name:          "lint",
		LatestVersion: "1.0.0",
		Status:        "available",
		Description:   "\x1b[2J\x1b[31mforged\x1b[0m\nacme/forged  1.0.0  unknown  available",
	}})

	if strings.Contains(out.String(), "\x1b[2J") || strings.Contains(out.String(), "\x1b[31m") {
		t.Fatalf("escape sequence reached the table: %q", out.String())
	}

	if lines := strings.Split(strings.TrimSpace(out.String()), "\n"); len(lines) != 2 {
		t.Fatalf("description added rows:\n%s", out.String())
	}
}

func TestTableDescriptionSanitizesAndTruncates(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{name: "first line only", in: "Lints code\nsecond line", want: "Lints code"},
		{name: "carriage return ends the line", in: "Lints\rcode", want: "Lints"},
		{name: "leading blank lines skipped", in: "\n\n  Real  ", want: "Real"},
		{name: "SGR colors removed", in: "\x1b[31mRed\x1b[0m text", want: "Red text"},
		{name: "OSC hyperlink removed", in: "\x1b]8;;https://example.com\x07link\x1b]8;;\x07", want: "link"},
		{name: "bare escape removed", in: "a\x1b", want: "a"},
		{name: "control characters become spaces", in: "tab\there\x00nul\x7fdel", want: "tab here nul del"},
		{name: "C1 control becomes space", in: "a\u009bb", want: "a b"},
		{name: "bidi override becomes space", in: "safe\u202Eevil", want: "safe evil"},
		{name: "line separator becomes space", in: "line\u2028sep\u2029end", want: "line sep end"},
		{name: "runs of spaces collapse", in: "many   spaces", want: "many spaces"},
		{name: "empty", in: "", want: ""},
		{name: "long text truncated to 80 runes", in: strings.Repeat("é", 500), want: strings.Repeat("é", 79) + "…"},
		{name: "80 runes kept whole", in: strings.Repeat("é", 80), want: strings.Repeat("é", 80)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tableDescription(tc.in)
			if got != tc.want {
				t.Fatalf("tableDescription(%q) = %q, want %q", tc.in, got, tc.want)
			}

			if n := len([]rune(got)); n > descriptionMaxRunes {
				t.Fatalf("tableDescription length = %d runes, want at most %d", n, descriptionMaxRunes)
			}
		})
	}
}
