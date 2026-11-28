package github

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/go-github/v81/github"
)

// ResolveTagCommit returns the commit SHA that tag points to and the time that commit was committed. The commits API
// dereferences annotated tags, so the SHA is always a commit. It returns an error wrapping ErrRefNotFound when the
// tag does not exist.
func ResolveTagCommit(
	ctx context.Context,
	githubClient *github.Client,
	owner, repo, tag string,
) (sha string, committedAt time.Time, err error) {
	if err := ValidateRepoName(owner, repo); err != nil {
		return "", time.Time{}, err
	}

	if err := ValidateRef(tag); err != nil {
		return "", time.Time{}, err
	}

	commit, resp, err := githubClient.Repositories.GetCommit(ctx, owner, repo, tag, nil)
	if err != nil {
		if resp != nil && (resp.StatusCode == 404 || resp.StatusCode == 422) {
			return "", time.Time{}, fmt.Errorf("%w: %s/%s@%s: %v", ErrRefNotFound, owner, repo, tag, err)
		}

		return "", time.Time{}, fmt.Errorf("failed to resolve tag %s/%s@%s: %w", owner, repo, tag, err)
	}

	sha = commit.GetSHA()
	if err := ValidateCommitSHA(sha); err != nil {
		return "", time.Time{}, err
	}

	return sha, commit.GetCommit().GetCommitter().GetDate().Time, nil
}

// ListTagNames returns the name of every tag in the repository that begins with prefix, with the prefix removed. Unlike
// ListMatchingTags it does not parse or filter the remaining names as semantic versions. Callers apply their own
// pattern to the result.
func ListTagNames(ctx context.Context, githubClient *github.Client, owner, repo, prefix string) ([]string, error) {
	if err := ValidateRepoName(owner, repo); err != nil {
		return nil, err
	}

	opt := &github.ListOptions{
		Page:    1,
		PerPage: 100,
	}

	var names []string
	for {
		tags, resp, err := githubClient.Repositories.ListTags(ctx, owner, repo, opt)
		if err != nil {
			return nil, err
		}

		if resp == nil {
			return nil, fmt.Errorf("tags response was nil")
		}

		for _, tag := range tags {
			if !strings.HasPrefix(tag.GetName(), prefix) {
				continue
			}

			names = append(names, strings.TrimPrefix(tag.GetName(), prefix))
		}

		if resp.NextPage == 0 {
			break
		}

		opt.Page = resp.NextPage
	}

	return names, nil
}
