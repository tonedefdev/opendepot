package github

import (
	"errors"
	"strings"
	"testing"
)

func TestReadArchiveLimitedAcceptsBodyWithinLimit(t *testing.T) {
	data, err := readArchiveLimited(strings.NewReader("0123456789"), 10)
	if err != nil {
		t.Fatalf("readArchiveLimited returned error: %v", err)
	}

	if string(data) != "0123456789" {
		t.Fatalf("data = %q; want the full body", string(data))
	}
}

func TestReadArchiveLimitedRejectsOversizeBody(t *testing.T) {
	data, err := readArchiveLimited(strings.NewReader("01234567890"), 10)
	if !errors.Is(err, ErrArchiveTooLarge) {
		t.Fatalf("err = %v; want ErrArchiveTooLarge", err)
	}

	if data != nil {
		t.Fatalf("data = %q; want nil so a truncated archive is never returned", string(data))
	}
}
