package github

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/google/go-github/v81/github"
)

var (
	commitSHAPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

	// ErrRefNotFound is returned when GitHub has no commit for a branch or when the source path has no commits on it.
	ErrRefNotFound = errors.New("git ref or source path not found")
)

// ValidateRef returns an error when ref is not a safe git branch or tag name for a GitHub API path.
func ValidateRef(ref string) error {
	if !validRefName(ref) {
		return fmt.Errorf("invalid git ref %q", ref)
	}

	return nil
}

// ValidateCommitSHA returns an error when sha is not a full 40 character lowercase hex commit SHA.
func ValidateCommitSHA(sha string) error {
	if !commitSHAPattern.MatchString(sha) {
		return fmt.Errorf("invalid commit SHA %q", sha)
	}

	return nil
}

// CleanSourcePath returns the source path without leading or trailing slashes. An empty result means the repository root.
// It returns an error when the path contains a dot segment that could climb out of the source directory.
func CleanSourcePath(path string) (string, error) {
	cleaned := strings.Trim(path, "/")
	if cleaned == "" {
		return "", nil
	}

	for segment := range strings.SplitSeq(cleaned, "/") {
		if segment == "." || segment == ".." {
			return "", fmt.Errorf("invalid source path %q", path)
		}
	}

	return cleaned, nil
}

// ValidateRepoURL returns an error unless repoURL is https://github.com/<owner>/<repo> for the given owner with
// no query, fragment, or user information.
func ValidateRepoURL(owner, repoURL string) error {
	parsed, err := url.Parse(repoURL)
	if err != nil {
		return fmt.Errorf("invalid repoUrl %q: %w", repoURL, err)
	}

	if parsed.Scheme != "https" || parsed.Host != "github.com" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("repoUrl %q must be https://github.com/<owner>/<repo>", repoURL)
	}

	segments := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(segments) != 2 {
		return fmt.Errorf("repoUrl %q must be https://github.com/<owner>/<repo>", repoURL)
	}

	if !strings.EqualFold(segments[0], owner) {
		return fmt.Errorf("repoUrl %q owner does not match repoOwner %q", repoURL, owner)
	}

	return ValidateRepoName(segments[0], segments[1])
}

// LatestPathCommit returns the SHA of the newest commit on ref that touched path. An empty path means the whole
// repository. It returns ErrRefNotFound when the ref does not exist or no commit touched the path.
func LatestPathCommit(ctx context.Context, githubClient *github.Client, owner, repo, ref, path string) (string, error) {
	if err := ValidateRepoName(owner, repo); err != nil {
		return "", err
	}

	if err := ValidateRef(ref); err != nil {
		return "", err
	}

	cleanedPath, err := CleanSourcePath(path)
	if err != nil {
		return "", err
	}

	commits, resp, err := githubClient.Repositories.ListCommits(ctx, owner, repo, &github.CommitsListOptions{
		SHA:         ref,
		Path:        cleanedPath,
		ListOptions: github.ListOptions{PerPage: 1},
	})

	if err != nil {
		if resp != nil && (resp.StatusCode == 404 || resp.StatusCode == 422) {
			return "", fmt.Errorf("%w: %s/%s@%s: %v", ErrRefNotFound, owner, repo, ref, err)
		}

		return "", fmt.Errorf("failed to list commits for %s/%s@%s: %w", owner, repo, ref, err)
	}

	if len(commits) == 0 {
		return "", fmt.Errorf("%w: %s/%s@%s path %q", ErrRefNotFound, owner, repo, ref, cleanedPath)
	}

	sha := commits[0].GetSHA()
	if err := ValidateCommitSHA(sha); err != nil {
		return "", err
	}

	return sha, nil
}
