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

			if err := a.describeResource(context.Background(), kind, "acme/reviewer", host); err != nil {
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

	if err := a.describeResource(context.Background(), kindSkill, "acme/lint", host); err != nil {
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
		if err := a.describeResource(context.Background(), kindSkill, resource, host); err == nil {
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

			err := a.describeResource(context.Background(), kindSkill, "acme/lint", host)
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

	if err := a.describeResource(context.Background(), kindAgent, "acme/reviewer", ""); err != nil {
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
