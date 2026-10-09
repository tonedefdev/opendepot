package github

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/google/go-github/v81/github"
	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
)

// newTestGithubClient returns a GitHub client whose API calls are served by the given handler.
func newTestGithubClient(t *testing.T, handler http.Handler) *github.Client {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	baseURL, err := url.Parse(server.URL + "/")
	if err != nil {
		t.Fatalf("failed to parse test server URL: %v", err)
	}

	client := github.NewClient(nil)
	client.BaseURL = baseURL

	return client
}

// tagsHandler serves the tag names for each page keyed by page number. Page "1" links to page "2" when present.
func tagsHandler(pages map[string][]string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/tags", func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		if page == "" {
			page = "1"
		}

		names, ok := pages[page]
		if !ok {
			http.NotFound(w, r)
			return
		}

		if _, ok := pages["2"]; ok && page == "1" {
			w.Header().Set("Link", fmt.Sprintf(`<http://%s/repos/owner/repo/tags?page=2&per_page=100>; rel="next"`, r.Host))
		}

		var tags []string
		for _, name := range names {
			tags = append(tags, fmt.Sprintf(`{"name": %q}`, name))
		}

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, "[%s]", strings.Join(tags, ","))
	})

	return mux
}

func TestListMatchingTags(t *testing.T) {
	allTags := []string{
		"skill-a/v1.0.0",
		"skill-a/v1.1.0",
		"skill-b/v9.0.0",
		"v3.0.0",
		"skill-a/latest",
		"skill-a/main",
	}

	tests := []struct {
		name        string
		pages       map[string][]string
		tagPrefix   string
		constraints string
		want        []string
	}{
		{
			name:      "with prefix strips prefix and skips other prefixes",
			pages:     map[string][]string{"1": allTags},
			tagPrefix: "skill-a/",
			want:      []string{"v1.0.0", "v1.1.0"},
		},
		{
			name:  "without prefix returns every semver tag",
			pages: map[string][]string{"1": {"v1.0.0", "2.0.0", "skill-a/v1.0.0", "latest"}},
			want:  []string{"v1.0.0", "2.0.0"},
		},
		{
			name: "paginates across two pages",
			pages: map[string][]string{
				"1": {"skill-a/v1.0.0", "skill-a/latest"},
				"2": {"skill-a/v2.0.0", "skill-b/v5.0.0"},
			},
			tagPrefix: "skill-a/",
			want:      []string{"v1.0.0", "v2.0.0"},
		},
		{
			name:      "non-semver tags are skipped",
			pages:     map[string][]string{"1": {"skill-a/latest", "skill-a/main", "skill-a/v1.0.0", "skill-a/release-candidate"}},
			tagPrefix: "skill-a/",
			want:      []string{"v1.0.0"},
		},
		{
			name:        "constraints filter matching versions",
			pages:       map[string][]string{"1": {"skill-a/v0.9.0", "skill-a/v1.0.0", "skill-a/v1.5.0", "skill-a/v2.0.0"}},
			tagPrefix:   "skill-a/",
			constraints: ">= 1.0.0, < 2.0.0",
			want:        []string{"v1.0.0", "v1.5.0"},
		},
		{
			name:        "empty constraints match all semver tags",
			pages:       map[string][]string{"1": {"skill-a/v0.9.0", "skill-a/v2.0.0"}},
			tagPrefix:   "skill-a/",
			constraints: "  ",
			want:        []string{"v0.9.0", "v2.0.0"},
		},
		{
			name:      "no matching tags returns nil",
			pages:     map[string][]string{"1": {"skill-b/v1.0.0"}},
			tagPrefix: "skill-a/",
			want:      nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newTestGithubClient(t, tagsHandler(tt.pages))

			got, err := ListMatchingTags(context.Background(), client, "owner", "repo", tt.tagPrefix, tt.constraints)
			if err != nil {
				t.Fatalf("ListMatchingTags() error = %v", err)
			}

			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ListMatchingTags() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestListMatchingTagsInvalidConstraints(t *testing.T) {
	client := newTestGithubClient(t, tagsHandler(map[string][]string{"1": {"v1.0.0"}}))

	_, err := ListMatchingTags(context.Background(), client, "owner", "repo", "", "not a constraint")
	if err == nil {
		t.Fatal("ListMatchingTags() expected error for malformed constraint, got nil")
	}
}

func TestTagCandidates(t *testing.T) {
	tests := []struct {
		name          string
		tagPrefix     string
		versionString string
		want          []string
	}{
		{
			name:          "bare version tries v prefix first",
			tagPrefix:     "skill-a/",
			versionString: "1.2.3",
			want:          []string{"skill-a/v1.2.3", "skill-a/1.2.3"},
		},
		{
			name:          "v prefixed version yields the same candidates",
			tagPrefix:     "",
			versionString: "v1.2.3",
			want:          []string{"v1.2.3", "1.2.3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TagCandidates(tt.tagPrefix, tt.versionString)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("TagCandidates() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetArchiveRequestUsesOwnerRepoAndRef(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/tarball/skill-a/v1.0.0", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://"+r.Host+"/archive.tar.gz", http.StatusFound)
	})
	mux.HandleFunc("/archive.tar.gz", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "archive-bytes")
	})

	client := newTestGithubClient(t, mux)

	resp, err := GetArchiveRequest(context.Background(), client, "owner", "repo", github.Tarball, "skill-a/v1.0.0")
	if err != nil {
		t.Fatalf("GetArchiveRequest() error = %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read archive body: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("GetArchiveRequest() status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	if string(body) != "archive-bytes" {
		t.Errorf("GetArchiveRequest() body = %q, want %q", body, "archive-bytes")
	}
}

func TestAgentSourceRepoName(t *testing.T) {
	name := "my-skill"
	repoURL := "https://github.com/tonedefdev/skills-monorepo/"

	tests := []struct {
		name   string
		config opendepotv1alpha1.AgentSourceConfig
		want   string
	}{
		{name: "uses last segment of repo url", config: opendepotv1alpha1.AgentSourceConfig{Name: &name, RepoUrl: &repoURL}, want: "skills-monorepo"},
		{name: "falls back to source name", config: opendepotv1alpha1.AgentSourceConfig{Name: &name}, want: "my-skill"},
		{name: "empty when nothing is set", config: opendepotv1alpha1.AgentSourceConfig{}, want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := AgentSourceRepoName(&tt.config); got != tt.want {
				t.Errorf("AgentSourceRepoName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestValidRepoSegment(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{name: "owner with hyphen", input: "tonedefdev", want: true},
		{name: "repo with dot and underscore", input: "terraform-aws_modules.v2", want: true},
		{name: "empty", input: "", want: false},
		{name: "dot segment", input: ".", want: false},
		{name: "dot dot segment", input: "..", want: false},
		{name: "traversal", input: "../../orgs/evil", want: false},
		{name: "slash", input: "owner/repo", want: false},
		{name: "query", input: "owner?x=1", want: false},
		{name: "space", input: "my repo", want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := validRepoSegment(test.input); got != test.want {
				t.Fatalf("validRepoSegment(%q) = %v, want %v", test.input, got, test.want)
			}
		})
	}
}

func TestValidRefName(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{name: "semver tag", input: "v1.2.3", want: true},
		{name: "prefixed agent tag", input: "gh-actions-debug-v1.0.0", want: true},
		{name: "build metadata", input: "v1.2.3+build.1", want: true},
		{name: "nested tag path", input: "skills/gh-actions-debug-v1.0.0", want: true},
		{name: "feature branch", input: "feat/my-skill-update", want: true},
		{name: "empty", input: "", want: false},
		{name: "traversal to other repo", input: "v1/../../../../OTHER/REPO/tarball/main", want: false},
		{name: "dot segment", input: "./main", want: false},
		{name: "empty segment", input: "a//b", want: false},
		{name: "query", input: "main?x=1", want: false},
		{name: "fragment", input: "main#x", want: false},
		{name: "backslash", input: `a\b`, want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := validRefName(test.input); got != test.want {
				t.Fatalf("validRefName(%q) = %v, want %v", test.input, got, test.want)
			}
		})
	}
}
