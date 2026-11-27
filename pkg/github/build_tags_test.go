package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"testing"
	"time"
)

const (
	tagTestOwner       = "acme"
	tagTestRepo        = "modules"
	tagTestTag         = "v1.0.0"
	tagTestNextTag     = "v2.0.0"
	tagTestSHA         = "0123456789abcdef0123456789abcdef01234567"
	tagTestCommitted   = "2026-03-04T05:06:07Z"
	tagTestTagsPath    = "/repos/acme/modules/tags"
	tagTestPrefix      = "my-chart/"
	tagTestNotFound    = `{"message":"Not Found"}`
	tagTestEmptyPrefix = ""
)

func TestResolveTagCommit(t *testing.T) {
	wantTime, _ := time.Parse(time.RFC3339, tagTestCommitted)

	tests := []struct {
		name     string
		owner    string
		repo     string
		tag      string
		status   int
		body     string
		wantSHA  string
		wantTime time.Time
		wantErr  bool
		notFound bool
	}{
		{
			name:     "dereferenced commit SHA and committer date",
			owner:    tagTestOwner,
			repo:     tagTestRepo,
			tag:      "my-chart/v1.2.3",
			status:   http.StatusOK,
			body:     `{"sha":"` + tagTestSHA + `","commit":{"committer":{"date":"` + tagTestCommitted + `"}}}`,
			wantSHA:  tagTestSHA,
			wantTime: wantTime,
		},
		{
			name:     "missing tag maps to ErrRefNotFound",
			owner:    tagTestOwner,
			repo:     tagTestRepo,
			tag:      "v9.9.9",
			status:   http.StatusNotFound,
			body:     tagTestNotFound,
			wantErr:  true,
			notFound: true,
		},
		{
			name:    "malformed SHA from GitHub is rejected",
			owner:   tagTestOwner,
			repo:    tagTestRepo,
			tag:     tagTestTag,
			status:  http.StatusOK,
			body:    `{"sha":"not-a-sha","commit":{"committer":{"date":"` + tagTestCommitted + `"}}}`,
			wantErr: true,
		},
		{
			name:    "unsafe owner is rejected before any request",
			owner:   "bad/owner",
			repo:    tagTestRepo,
			tag:     tagTestTag,
			status:  http.StatusOK,
			body:    `{}`,
			wantErr: true,
		},
		{
			name:    "traversal tag is rejected before any request",
			owner:   tagTestOwner,
			repo:    tagTestRepo,
			tag:     "../" + tagTestTag,
			status:  http.StatusOK,
			body:    `{}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newFakeGithubClient(t, func(w http.ResponseWriter, r *http.Request) {
				wantPath := fmt.Sprintf("/repos/%s/%s/commits/%s", tt.owner, tt.repo, tt.tag)
				if r.URL.Path != wantPath && r.URL.EscapedPath() != wantPath {
					t.Errorf("unexpected path %q", r.URL.Path)
				}

				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			})

			sha, committedAt, err := ResolveTagCommit(context.Background(), client, tt.owner, tt.repo, tt.tag)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ResolveTagCommit() error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.notFound && !errors.Is(err, ErrRefNotFound) {
				t.Fatalf("expected ErrRefNotFound, got %v", err)
			}

			if tt.wantErr {
				return
			}

			if sha != tt.wantSHA {
				t.Fatalf("sha = %q, want %q", sha, tt.wantSHA)
			}

			if !committedAt.Equal(tt.wantTime) {
				t.Fatalf("committedAt = %v, want %v", committedAt, tt.wantTime)
			}
		})
	}
}

func TestListTagNamesPaginatesAndStripsPrefix(t *testing.T) {
	client := newFakeGithubClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != tagTestTagsPath {
			t.Errorf("unexpected path %q", r.URL.Path)
		}

		if r.URL.Query().Get("page") == "2" {
			_, _ = w.Write([]byte(`[{"name":"my-chart/` + tagTestNextTag +
				`"},{"name":"other/v1.0.0"},{"name":"my-chart/latest"}]`))
			return
		}

		w.Header().Set("Link", fmt.Sprintf(`<http://%s%s?page=2>; rel="next"`, r.Host, tagTestTagsPath))
		_, _ = w.Write([]byte(`[{"name":"my-chart/` + tagTestTag +
			`"},{"name":"unrelated"}]`))
	})

	got, err := ListTagNames(context.Background(), client, tagTestOwner, tagTestRepo, tagTestPrefix)
	if err != nil {
		t.Fatalf("ListTagNames: %v", err)
	}

	want := []string{tagTestTag, tagTestNextTag, "latest"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ListTagNames = %v, want %v", got, want)
	}
}

func TestListTagNamesEmptyPrefixReturnsAllNames(t *testing.T) {
	client := newFakeGithubClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"name":"` + tagTestTag + `"},{"name":"release-2"}]`))
	})

	got, err := ListTagNames(context.Background(), client, tagTestOwner, tagTestRepo, tagTestEmptyPrefix)
	if err != nil {
		t.Fatalf("ListTagNames: %v", err)
	}

	if want := []string{tagTestTag, "release-2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ListTagNames = %v, want %v", got, want)
	}
}

func TestListTagNamesRejectsUnsafeRepoName(t *testing.T) {
	client := newFakeGithubClient(t, func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("no request should be sent for an unsafe repository name")
	})

	if _, err := ListTagNames(context.Background(), client, tagTestOwner, "..", tagTestEmptyPrefix); err == nil {
		t.Fatal("expected error for dot-dot repository name")
	}
}
