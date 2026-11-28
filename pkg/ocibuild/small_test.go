package ocibuild

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/opencontainers/go-digest"
)

func TestResolveContextPath(t *testing.T) {
	tests := map[string]struct {
		in      string
		want    string
		wantErr bool
	}{
		"empty means root":      {in: "", want: "."},
		"dot means root":        {in: ".", want: "."},
		"simple directory":      {in: "services/app", want: "services/app"},
		"trailing slash":        {in: testApp + "/", want: testApp},
		"absolute path":         {in: "/" + testApp, wantErr: true},
		"parent traversal":      {in: "../app", wantErr: true},
		"inner traversal":       {in: "a/../b", wantErr: true},
		"dot segment":           {in: "a/./b", wantErr: true},
		"empty segment":         {in: "a//b", wantErr: true},
		"space not allowed":     {in: "a b", wantErr: true},
		"backslash not allowed": {in: `a\b`, wantErr: true},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := ResolveContextPath(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ResolveContextPath(%q) error = %v, wantErr %v", tt.in, err, tt.wantErr)
			}

			if tt.wantErr {
				if !errors.Is(err, ErrUnsafePath) {
					t.Fatalf("error = %v, want ErrUnsafePath", err)
				}

				return
			}

			if got != tt.want {
				t.Fatalf("ResolveContextPath(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestResolveDockerfilePath(t *testing.T) {
	tests := map[string]struct {
		in      string
		want    string
		wantErr bool
	}{
		"empty defaults":    {in: "", want: DefaultDockerfilePath},
		"nested file":       {in: "docker/app.Dockerfile", want: "docker/app.Dockerfile"},
		"dot is not a file": {in: ".", wantErr: true},
		"dotdot":            {in: "..", wantErr: true},
		"absolute path":     {in: "/" + dockerfileName, wantErr: true},
		"traversal":         {in: "../" + dockerfileName, wantErr: true},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := ResolveDockerfilePath(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ResolveDockerfilePath(%q) error = %v, wantErr %v", tt.in, err, tt.wantErr)
			}

			if !tt.wantErr && got != tt.want {
				t.Fatalf("ResolveDockerfilePath(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestCheckDockerfile(t *testing.T) {
	bom := []byte("\xEF\xBB\xBF")
	withBOM := append(append([]byte{}, bom...), []byte("# syntax=evil\nFROM x\n")...)

	tests := []struct {
		name    string
		content []byte
		wantErr error
	}{
		{name: "plain FROM", content: []byte("FROM alpine:3.20\nRUN true\n")},
		{name: "lowercase from", content: []byte("from alpine\n")},
		{name: "comment that mentions syntax", content: []byte("# this is about syntax\nFROM x\n")},
		{name: "syntax directive", content: []byte("# syntax=docker/dockerfile:1\nFROM x\n"), wantErr: ErrInvalidDockerfile},
		{name: "upper case directive key", content: []byte("#SYNTAX = evil/frontend\nFROM x\n"),
			wantErr: ErrInvalidDockerfile},
		{name: "leading whitespace", content: []byte("   # syntax=evil\nFROM x\n"), wantErr: ErrInvalidDockerfile},
		{name: "byte order mark", content: withBOM, wantErr: ErrInvalidDockerfile},
		{name: "CRLF line endings", content: []byte("FROM x\r\n# Syntax=evil\r\n"), wantErr: ErrInvalidDockerfile},
		{name: "directive after instructions", content: []byte("FROM x\n# syntax=late\n"), wantErr: ErrInvalidDockerfile},
		{name: "no FROM", content: []byte("RUN echo hi\n"), wantErr: ErrInvalidDockerfile},
		{
			name:    "oversize",
			content: []byte("FROM x\n" + strings.Repeat("#", int(MaxDockerfileBytes))),
			wantErr: ErrLimitExceeded,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CheckDockerfile(tt.content)
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("CheckDockerfile: %v", err)
				}

				return
			}

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("CheckDockerfile error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestTokens(t *testing.T) {
	a, err := NewToken()
	if err != nil {
		t.Fatalf("NewToken: %v", err)
	}

	b, err := NewToken()
	if err != nil {
		t.Fatalf("NewToken: %v", err)
	}

	if len(a) != 2*TokenBytes || a == b {
		t.Fatalf("tokens must be %d hex characters and unique: %q %q", 2*TokenBytes, a, b)
	}

	const abcSHA256 = "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	if got := HashToken("abc"); got != abcSHA256 {
		t.Fatalf("HashToken(abc) = %s, want %s", got, abcSHA256)
	}

	if !TokenMatches(a, HashToken(a)) {
		t.Fatal("TokenMatches rejected the matching hash")
	}

	if TokenMatches(b, HashToken(a)) || TokenMatches("", HashToken(a)) {
		t.Fatal("TokenMatches accepted a different token")
	}
}

func TestParsePlatform(t *testing.T) {
	tests := map[string]struct {
		in      string
		want    string
		wantErr bool
	}{
		"amd64":          {in: testPlatformAmd64, want: testPlatformAmd64},
		"arm64":          {in: testPlatformArm64, want: testPlatformArm64},
		"arm v7":         {in: testPlatformArmV7, want: testPlatformArmV7},
		"arm v6":         {in: "linux/arm/v6", want: "linux/arm/v6"},
		"arm64 v8":       {in: "linux/arm64/v8", want: "linux/arm64/v8"},
		"windows":        {in: "windows/amd64", wantErr: true},
		"unknown arch":   {in: "linux/386", wantErr: true},
		"bare arm":       {in: "linux/" + testArm, wantErr: true},
		"bad variant":    {in: "linux/amd64/v3", wantErr: true},
		"too many parts": {in: "linux/arm/v7/x", wantErr: true},
		"one part":       {in: testLinux, wantErr: true},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			p, err := ParsePlatform(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParsePlatform(%q) error = %v, wantErr %v", tt.in, err, tt.wantErr)
			}

			if !tt.wantErr && p.String() != tt.want {
				t.Fatalf("ParsePlatform(%q).String() = %q, want %q", tt.in, p.String(), tt.want)
			}
		})
	}
}

func TestPlatformJobSuffixAndOCI(t *testing.T) {
	p, err := ParsePlatform(testPlatformArmV7)
	if err != nil {
		t.Fatalf("ParsePlatform: %v", err)
	}

	if got := p.JobSuffix(); got != "arm-v7" {
		t.Fatalf("JobSuffix = %q, want arm-v7", got)
	}

	oci := p.OCI()
	if oci.OS != testLinux || oci.Architecture != testArm || oci.Variant != "v7" {
		t.Fatalf("OCI() = %+v", oci)
	}

	arm64, err := ParsePlatform(testPlatformArm64)
	if err != nil {
		t.Fatalf("ParsePlatform: %v", err)
	}

	if !arm64.matches(testLinux, testArm64, "v8") || !arm64.matches(testLinux, testArm64, "") {
		t.Fatal("arm64 with no variant should match an arm64 config that names v8")
	}

	if arm64.matches(testLinux, testAmd64, "") {
		t.Fatal("arm64 must not match amd64")
	}
}

func TestBuildIndex(t *testing.T) {
	meta := IndexMetadata{
		Source:   testSource,
		Revision: strings.Repeat("c", 40),
		Version:  "v1.2.3",
		BuildID:  testBuildID,
	}

	amd64 := platformManifest(t, testPlatformAmd64, "1")
	arm := platformManifest(t, testPlatformArmV7, "2")

	forward, err := BuildIndex([]PlatformManifest{amd64, arm}, meta)
	if err != nil {
		t.Fatalf("BuildIndex: %v", err)
	}

	reversed, err := BuildIndex([]PlatformManifest{arm, amd64}, meta)
	if err != nil {
		t.Fatalf("BuildIndex reversed: %v", err)
	}

	if !bytes.Equal(forward, reversed) {
		t.Fatal("index bytes depend on input order")
	}

	if !strings.Contains(string(forward), `"org.opencontainers.image.revision":"`+meta.Revision+`"`) {
		t.Fatalf("revision annotation missing: %s", forward)
	}

	if !strings.Contains(string(forward), `"mediaType":"application/vnd.oci.image.index.v1+json"`) {
		t.Fatalf("index media type missing: %s", forward)
	}

	if strings.Index(string(forward), `"architecture":"amd64"`) > strings.Index(string(forward), `"architecture":"arm"`) {
		t.Fatal("platform entries are not sorted")
	}
}

func TestBuildIndexRejectsInvalidInput(t *testing.T) {
	meta := IndexMetadata{
		Source:   testSource,
		Revision: strings.Repeat("a", 40),
		Version:  "v1.0.0",
		BuildID:  "build-1",
	}

	nested := platformManifest(t, testPlatformAmd64, "1")
	nested.MediaType = "application/vnd.oci.image.index.v1+json"

	badDigest := platformManifest(t, testPlatformAmd64, "1")
	badDigest.Digest = digest.Digest("sha512:" + strings.Repeat("a", 128))

	zeroSize := platformManifest(t, testPlatformAmd64, "1")
	zeroSize.Size = 0

	duplicate := []PlatformManifest{
		platformManifest(t, testPlatformArm64, "1"),
		platformManifest(t, testPlatformArm64+"/v8", "2"),
	}

	badRevision := meta
	badRevision.Revision = "main"

	noSource := meta
	noSource.Source = " "

	amd64 := []PlatformManifest{platformManifest(t, testPlatformAmd64, "1")}

	tests := []struct {
		name      string
		manifests []PlatformManifest
		meta      IndexMetadata
	}{
		{name: "no manifests", manifests: nil, meta: meta},
		{name: "nested index", manifests: []PlatformManifest{nested}, meta: meta},
		{name: "non-sha256 digest", manifests: []PlatformManifest{badDigest}, meta: meta},
		{name: "zero size", manifests: []PlatformManifest{zeroSize}, meta: meta},
		{name: "duplicate platform", manifests: duplicate, meta: meta},
		{name: "revision is not a commit SHA", manifests: amd64, meta: badRevision},
		{name: "missing source", manifests: amd64, meta: noSource},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := BuildIndex(tt.manifests, tt.meta); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}
