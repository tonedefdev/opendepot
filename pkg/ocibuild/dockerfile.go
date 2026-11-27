package ocibuild

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// DefaultDockerfilePath is the Dockerfile path used when a build does not set one.
const DefaultDockerfilePath = "Dockerfile"

// MaxDockerfileBytes is the largest Dockerfile, and the largest .dockerignore, that a build context may carry.
const MaxDockerfileBytes int64 = 1 << 20

// ErrUnsafePath is returned when a context or Dockerfile path could escape the repository root.
var ErrUnsafePath = errors.New("unsafe path")

// ErrInvalidDockerfile is returned when a Dockerfile fails the pre-checks that run before a build.
var ErrInvalidDockerfile = errors.New("invalid Dockerfile")

var (
	relPathPattern         = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)
	syntaxDirectivePattern = regexp.MustCompile(`(?im)^\s*#\s*syntax\s*=`)
	fromInstructionPattern = regexp.MustCompile(`(?im)^\s*FROM\s`)
	utf8BOM                = []byte("\xEF\xBB\xBF")
)

// ResolveContextPath returns the build context directory relative to the repository root. An empty path or "."
// means the root. It returns an error wrapping ErrUnsafePath for absolute paths and paths with dot segments.
func ResolveContextPath(contextPath string) (string, error) {
	if contextPath == "" || contextPath == "." {
		return ".", nil
	}

	return cleanRelativePath(contextPath)
}

// ResolveDockerfilePath returns the Dockerfile path relative to the repository root. An empty path means
// DefaultDockerfilePath. It returns an error wrapping ErrUnsafePath for absolute paths and paths with dot segments.
func ResolveDockerfilePath(dockerfilePath string) (string, error) {
	if dockerfilePath == "" {
		return DefaultDockerfilePath, nil
	}

	return cleanRelativePath(dockerfilePath)
}

// cleanRelativePath strips one trailing slash and rejects anything other than a relative path made of safe segments.
func cleanRelativePath(p string) (string, error) {
	trimmed := strings.TrimSuffix(p, "/")
	if trimmed == "" || strings.HasPrefix(trimmed, "/") || !relPathPattern.MatchString(trimmed) {
		return "", fmt.Errorf("%w: %q must be a relative path", ErrUnsafePath, p)
	}

	for segment := range strings.SplitSeq(trimmed, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return "", fmt.Errorf("%w: %q has an empty or dot segment", ErrUnsafePath, p)
		}
	}

	return trimmed, nil
}

// CheckDockerfile runs the pre-checks that run before a Dockerfile is sent to BuildKit. It rejects a '# syntax='
// parser directive, which would let the Dockerfile choose an arbitrary frontend image, and it rejects a Dockerfile
// with no FROM instruction. The directive check is case-insensitive and ignores leading whitespace and a byte
// order mark. It checks every line, not only the header, so a directive cannot hide after a blank line.
func CheckDockerfile(content []byte) error {
	if int64(len(content)) > MaxDockerfileBytes {
		return fmt.Errorf("%w: %d bytes exceeds %d", ErrLimitExceeded, len(content), MaxDockerfileBytes)
	}

	content = bytes.TrimPrefix(content, utf8BOM)
	if syntaxDirectivePattern.Match(content) {
		return fmt.Errorf("%w: a '# syntax=' parser directive is not allowed", ErrInvalidDockerfile)
	}

	if !fromInstructionPattern.Match(content) {
		return fmt.Errorf("%w: no FROM instruction", ErrInvalidDockerfile)
	}

	return nil
}
