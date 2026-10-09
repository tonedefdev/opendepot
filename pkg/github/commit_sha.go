package github

import (
	"fmt"
	"regexp"
)

var commitSHAPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

// ValidateCommitSHA returns an error unless sha is a full, lowercase, 40-character hexadecimal commit SHA.
// Branch and tag names are rejected so that a source can only be downloaded at an exact commit.
func ValidateCommitSHA(sha string) error {
	if !commitSHAPattern.MatchString(sha) {
		return fmt.Errorf("invalid commit SHA %q: must be 40 lowercase hexadecimal characters", sha)
	}

	return nil
}
