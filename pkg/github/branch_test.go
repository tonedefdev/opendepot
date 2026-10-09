package github

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/google/go-github/v81/github"
)

const testCommitSHA = "3f2a9c1d0e4b5a6f7c8d9e0f1a2b3c4d5e6f7a8b"

func newFakeGithubClient(t *testing.T, handler http.HandlerFunc) *github.Client {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client := github.NewClient(nil)
	baseURL, err := url.Parse(server.URL + "/")
	if err != nil {
		t.Fatalf("parse fake GitHub URL: %v", err)
	}

	client.BaseURL = baseURL
	return client
}

func TestLatestPathCommit(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		status     int
		body       string
		wantSHA    string
		wantErr    bool
		wantNotRef bool
		wantQuery  string
	}{
		{
			name:      "returns newest commit on path",
			path:      "/skills/code-review/",
			status:    http.StatusOK,
			body:      `[{"sha":"` + testCommitSHA + `"}]`,
			wantSHA:   testCommitSHA,
			wantQuery: "skills/code-review",
		},
		{
			name:      "repository root omits path filter",
			path:      "",
			status:    http.StatusOK,
			body:      `[{"sha":"` + testCommitSHA + `"}]`,
			wantSHA:   testCommitSHA,
			wantQuery: "",
		},
		{
			name:       "missing branch is ErrRefNotFound",
			path:       "skills/code-review",
			status:     http.StatusNotFound,
			body:       `{"message":"Not Found"}`,
			wantErr:    true,
			wantNotRef: true,
		},
		{
			name:       "unprocessable ref is ErrRefNotFound",
			path:       "skills/code-review",
			status:     http.StatusUnprocessableEntity,
			body:       `{"message":"No commit found for SHA"}`,
			wantErr:    true,
			wantNotRef: true,
		},
		{
			name:       "empty result is ErrRefNotFound",
			path:       "skills/code-review",
			status:     http.StatusOK,
			body:       `[]`,
			wantErr:    true,
			wantNotRef: true,
		},
		{
			name:    "malformed commit SHA is rejected",
			path:    "skills/code-review",
			status:  http.StatusOK,
			body:    `[{"sha":"not-a-sha"}]`,
			wantErr: true,
		},
		{
			name:    "server error is not ErrRefNotFound",
			path:    "skills/code-review",
			status:  http.StatusInternalServerError,
			body:    `{"message":"boom"}`,
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var gotQuery string
			client := newFakeGithubClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/repos/owner/repo/commits" {
					http.NotFound(w, r)
					return
				}

				gotQuery = r.URL.Query().Get("path")
				if r.URL.Query().Get("sha") != "main" {
					t.Errorf("sha query = %q, want main", r.URL.Query().Get("sha"))
				}

				if r.URL.Query().Get("per_page") != "1" {
					t.Errorf("per_page query = %q, want 1", r.URL.Query().Get("per_page"))
				}

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.body))
			})

			sha, err := LatestPathCommit(context.Background(), client, "owner", "repo", "main", test.path)
			if test.wantErr != (err != nil) {
				t.Fatalf("LatestPathCommit() error = %v, wantErr %v", err, test.wantErr)
			}

			if errors.Is(err, ErrRefNotFound) != test.wantNotRef {
				t.Fatalf("errors.Is(err, ErrRefNotFound) = %v, want %v", errors.Is(err, ErrRefNotFound), test.wantNotRef)
			}

			if sha != test.wantSHA {
				t.Fatalf("LatestPathCommit() sha = %q, want %q", sha, test.wantSHA)
			}

			if test.status == http.StatusOK && !test.wantErr && gotQuery != test.wantQuery {
				t.Fatalf("path query = %q, want %q", gotQuery, test.wantQuery)
			}
		})
	}
}

func TestLatestPathCommitRejectsUnsafeInput(t *testing.T) {
	client := newFakeGithubClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("GitHub must not be called for invalid input: %s", r.URL.Path)
	})

	tests := []struct {
		name  string
		owner string
		repo  string
		ref   string
		path  string
	}{
		{name: "traversal in path", owner: "owner", repo: "repo", ref: "main", path: "skills/../../secrets"},
		{name: "invalid owner", owner: "../owner", repo: "repo", ref: "main", path: ""},
		{name: "invalid ref", owner: "owner", repo: "repo", ref: "main/../../x", path: ""},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := LatestPathCommit(context.Background(), client, test.owner, test.repo, test.ref, test.path); err == nil {
				t.Fatal("LatestPathCommit() error = nil, want error")
			}
		})
	}
}

func TestValidateCommitSHA(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "full lowercase SHA", input: testCommitSHA},
		{name: "short SHA", input: "3f2a9c1", wantErr: true},
		{name: "uppercase SHA", input: "3F2A9C1D0E4B5A6F7C8D9E0F1A2B3C4D5E6F7A8B", wantErr: true},
		{name: "branch name", input: "main", wantErr: true},
		{name: "empty", input: "", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := ValidateCommitSHA(test.input); (err != nil) != test.wantErr {
				t.Fatalf("ValidateCommitSHA(%q) error = %v, wantErr %v", test.input, err, test.wantErr)
			}
		})
	}
}

func TestCleanSourcePath(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "empty is repository root", input: "", want: ""},
		{name: "slash is repository root", input: "/", want: ""},
		{name: "leading slash stripped", input: "/skills/code-review", want: "skills/code-review"},
		{name: "trailing slash stripped", input: "skills/code-review/", want: "skills/code-review"},
		{name: "parent segment", input: "skills/../secrets", wantErr: true},
		{name: "current segment", input: "./skills", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := CleanSourcePath(test.input)
			if (err != nil) != test.wantErr {
				t.Fatalf("CleanSourcePath(%q) error = %v, wantErr %v", test.input, err, test.wantErr)
			}

			if got != test.want {
				t.Fatalf("CleanSourcePath(%q) = %q, want %q", test.input, got, test.want)
			}
		})
	}
}

func TestValidateRepoURL(t *testing.T) {
	tests := []struct {
		name    string
		owner   string
		repoURL string
		wantErr bool
	}{
		{name: "valid repository URL", owner: "owner", repoURL: "https://github.com/owner/repo"},
		{name: "trailing slash", owner: "owner", repoURL: "https://github.com/owner/repo/"},
		{name: "owner case differs", owner: "Owner", repoURL: "https://github.com/owner/repo"},
		{name: "http scheme", owner: "owner", repoURL: "http://github.com/owner/repo", wantErr: true},
		{name: "other host", owner: "owner", repoURL: "https://evil.example.com/owner/repo", wantErr: true},
		{name: "owner mismatch", owner: "other", repoURL: "https://github.com/owner/repo", wantErr: true},
		{name: "extra path segment", owner: "owner", repoURL: "https://github.com/owner/repo/tree/main", wantErr: true},
		{name: "query string", owner: "owner", repoURL: "https://github.com/owner/repo?x=1", wantErr: true},
		{name: "user information", owner: "owner", repoURL: "https://user@github.com/owner/repo", wantErr: true},
		{name: "dot segment repo", owner: "owner", repoURL: "https://github.com/owner/..", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := ValidateRepoURL(test.owner, test.repoURL); (err != nil) != test.wantErr {
				t.Fatalf("ValidateRepoURL(%q, %q) error = %v, wantErr %v", test.owner, test.repoURL, err, test.wantErr)
			}
		})
	}
}
