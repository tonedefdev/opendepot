package ocibuild

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

const (
	testAbsoluteName = "absolute entry name"
	testSymlinkName  = "symlink escaping the root"
)

type tarEntry struct {
	name     string
	typeflag byte
	body     string
	linkname string
	mode     int64
	uid      int
	modTime  time.Time
}

func dirEntry(name string) tarEntry {
	return tarEntry{name: name, typeflag: tar.TypeDir, mode: 0o755}
}

func fileEntry(name, body string) tarEntry {
	return tarEntry{name: name, typeflag: tar.TypeReg, body: body, mode: 0o644}
}

func execEntry(name, body string) tarEntry {
	return tarEntry{name: name, typeflag: tar.TypeReg, body: body, mode: 0o755}
}

func linkEntry(name, target string) tarEntry {
	return tarEntry{name: name, typeflag: tar.TypeSymlink, linkname: target, mode: 0o777}
}

// makeArchive writes entries as a gzip-compressed tar stream, the shape of a GitHub repository archive.
func makeArchive(t *testing.T, entries ...tarEntry) []byte {
	t.Helper()

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		modTime := e.modTime
		if modTime.IsZero() {
			modTime = time.Unix(1700000000, 0)
		}

		hdr := &tar.Header{
			Name:     e.name,
			Typeflag: e.typeflag,
			Linkname: e.linkname,
			Mode:     e.mode,
			Uid:      e.uid,
			Gid:      e.uid,
			ModTime:  modTime,
		}

		if e.typeflag == tar.TypeReg {
			hdr.Size = int64(len(e.body))
		}

		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("write header %q: %v", e.name, err)
		}

		if e.typeflag == tar.TypeReg {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatalf("write body %q: %v", e.name, err)
			}
		}
	}

	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}

	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}

	return buf.Bytes()
}

// baseRepo is a small repository with a context in app/, a Dockerfile at the root, and a file outside the context.
func baseRepo() []tarEntry {
	return []tarEntry{
		dirEntry(testRoot + "/"),
		fileEntry(testRoot+"/"+dockerfileName, testDockerfile),
		fileEntry(testRoot+"/"+dockerfileName+".dockerignore", "*.tmp\n"),
		fileEntry(testRoot+"/README.md", "readme"),
		dirEntry(testRoot + "/" + testApp + "/"),
		fileEntry(testRoot+"/app/main.txt", "hello"),
		execEntry(testRoot+"/app/run.sh", "#!/bin/sh\n"),
		linkEntry(testRoot+"/app/alias.txt", "main.txt"),
		fileEntry(testRoot+"/app/.dockerignore", "*.log\n"),
		dirEntry(testRoot + "/other/"),
		fileEntry(testRoot+"/other/secret.txt", "outside the context"),
	}
}

func appendEntries(base []tarEntry, extra ...tarEntry) []tarEntry {
	return append(slices.Clone(base), extra...)
}

// appSpec selects the app/ context with the root Dockerfile.
func appSpec() ContextSpec {
	return ContextSpec{ContextPath: testApp, DockerfilePath: dockerfileName}
}

// stage runs StageContext against a fresh work directory.
func stage(t *testing.T, archive []byte, spec ContextSpec, limits Limits) ([]byte, error) {
	t.Helper()

	var out bytes.Buffer
	err := StageContext(&out, bytes.NewReader(archive), t.TempDir(), spec, limits)
	return out.Bytes(), err
}

type packedEntry struct {
	typeflag byte
	body     string
	linkname string
	mode     int64
}

// readPacked returns the entries of a tar.gz keyed by name.
func readPacked(t *testing.T, data []byte) map[string]packedEntry {
	t.Helper()

	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("open packed archive: %v", err)
	}

	tr := tar.NewReader(gz)
	entries := map[string]packedEntry{}
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			t.Fatalf("read packed archive: %v", err)
		}

		body, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("read packed body %q: %v", hdr.Name, err)
		}

		entries[hdr.Name] = packedEntry{
			typeflag: hdr.Typeflag,
			body:     string(body),
			linkname: hdr.Linkname,
			mode:     hdr.Mode,
		}
	}

	return entries
}

func keys(m map[string]packedEntry) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}

	slices.Sort(out)
	return out
}

func TestStageContextPacksContextAndDockerfile(t *testing.T) {
	data, err := stage(t, makeArchive(t, baseRepo()...), appSpec(), Limits{})
	if err != nil {
		t.Fatalf("StageContext: %v", err)
	}

	got := readPacked(t, data)
	want := []string{
		"context/",
		"context/.dockerignore",
		"context/alias.txt",
		"context/main.txt",
		"context/run.sh",
		"dockerfile/",
		"dockerfile/Dockerfile",
		"dockerfile/Dockerfile.dockerignore",
	}

	if len(got) != len(want) {
		t.Fatalf("packed %d entries, want %d: %v", len(got), len(want), keys(got))
	}

	for _, name := range want {
		if _, ok := got[name]; !ok {
			t.Fatalf("missing packed entry %q; have %v", name, keys(got))
		}
	}

	if got["dockerfile/Dockerfile"].body != testDockerfile {
		t.Fatalf("dockerfile body = %q", got["dockerfile/Dockerfile"].body)
	}

	if got["dockerfile/Dockerfile.dockerignore"].body != "*.tmp\n" {
		t.Fatalf("dockerignore next to the Dockerfile was not packed")
	}

	alias := got["context/alias.txt"]
	if alias.typeflag != tar.TypeSymlink || alias.linkname != "main.txt" {
		t.Fatalf("symlink not preserved: %+v", alias)
	}

	if got["context/run.sh"].mode != 0o755 || got["context/main.txt"].mode != 0o644 {
		t.Fatalf("modes not normalized: run.sh=%o main.txt=%o", got["context/run.sh"].mode, got["context/main.txt"].mode)
	}
}

func TestStageContextRootContextKeepsEverything(t *testing.T) {
	data, err := stage(t, makeArchive(t, baseRepo()...), ContextSpec{}, Limits{})
	if err != nil {
		t.Fatalf("StageContext: %v", err)
	}

	got := readPacked(t, data)
	for _, name := range []string{
		"context/" + dockerfileName,
		"context/app/main.txt",
		"context/other/secret.txt",
		"dockerfile/" + dockerfileName,
	} {
		if _, ok := got[name]; !ok {
			t.Fatalf("missing %q; have %v", name, keys(got))
		}
	}

	if _, ok := got["dockerfile/Dockerfile.dockerignore"]; !ok {
		t.Fatalf("dockerignore missing from dockerfile/")
	}
}

func TestStageContextIsDeterministic(t *testing.T) {
	first := makeArchive(t, baseRepo()...)

	mutated := baseRepo()
	for i := range mutated {
		mutated[i].modTime = time.Unix(1893456000, 0)
		mutated[i].uid = 1000
		if mutated[i].typeflag == tar.TypeDir {
			mutated[i].mode = 0o700
		}
	}

	slices.Reverse(mutated)
	second := makeArchive(t, mutated...)

	a, err := stage(t, first, appSpec(), Limits{})
	if err != nil {
		t.Fatalf("StageContext first: %v", err)
	}

	b, err := stage(t, second, appSpec(), Limits{})
	if err != nil {
		t.Fatalf("StageContext second: %v", err)
	}

	if sha256.Sum256(a) != sha256.Sum256(b) {
		t.Fatal("same content with different timestamps, owners, and order produced different digests")
	}
}

func TestStageContextRejectsUnsafeArchiveEntries(t *testing.T) {
	tests := []struct {
		name    string
		entries []tarEntry
	}{
		{
			name:    "parent traversal in name",
			entries: appendEntries(baseRepo(), fileEntry(testRoot+"/../escape", "x")),
		},
		{
			name:    testAbsoluteName,
			entries: []tarEntry{fileEntry("/etc/passwd", "x")},
		},
		{
			name:    "backslash in name",
			entries: appendEntries(baseRepo(), fileEntry(testRoot+`\evil`, "x")),
		},
		{
			name:    testSymlinkName,
			entries: appendEntries(baseRepo(), linkEntry(testRoot+"/app/up.txt", "../../../etc/passwd")),
		},
		{
			name:    "absolute symlink target",
			entries: appendEntries(baseRepo(), linkEntry(testRoot+"/app/abs", "/etc/passwd")),
		},
		{
			name:    "symlink climbing out of the top-level directory",
			entries: appendEntries(baseRepo(), linkEntry(testRoot+"/up", "../outside")),
		},
		{
			name: "hardlink",
			entries: appendEntries(baseRepo(), tarEntry{
				name: testRoot + "/app/hard", typeflag: tar.TypeLink, linkname: testRoot + "/app/main.txt", mode: 0o644,
			}),
		},
		{
			name:    "character device",
			entries: appendEntries(baseRepo(), tarEntry{name: testRoot + "/dev0", typeflag: tar.TypeChar, mode: 0o600}),
		},
		{
			name:    "fifo",
			entries: appendEntries(baseRepo(), tarEntry{name: testRoot + "/pipe", typeflag: tar.TypeFifo, mode: 0o600}),
		},
		{
			name: "write through a symlink",
			entries: appendEntries(baseRepo(),
				linkEntry(testRoot+"/app/inner", "."),
				fileEntry(testRoot+"/app/inner/evil.txt", "x")),
		},
		{
			name:    "second top-level directory",
			entries: appendEntries(baseRepo(), fileEntry("other-root/file.txt", "x")),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := stage(t, makeArchive(t, tt.entries...), appSpec(), Limits{})
			if !errors.Is(err, ErrUnsafeEntry) {
				t.Fatalf("StageContext error = %v, want ErrUnsafeEntry", err)
			}
		})
	}
}

func TestStageContextRejectsSymlinkLeavingContext(t *testing.T) {
	entries := appendEntries(baseRepo(), linkEntry(testRoot+"/app/up", "../other/secret.txt"))

	_, err := stage(t, makeArchive(t, entries...), appSpec(), Limits{})
	if !errors.Is(err, ErrUnsafeEntry) {
		t.Fatalf("StageContext error = %v, want ErrUnsafeEntry", err)
	}
}

func TestStageContextSymlinkOutsideContextIsIgnored(t *testing.T) {
	entries := appendEntries(baseRepo(), linkEntry(testRoot+"/other/link", "../README.md"))

	if _, err := stage(t, makeArchive(t, entries...), appSpec(), Limits{}); err != nil {
		t.Fatalf("symlink outside the context should not block staging: %v", err)
	}
}

func TestStageContextLimits(t *testing.T) {
	archive := makeArchive(t, baseRepo()...)

	t.Run("entries", func(t *testing.T) {
		_, err := stage(t, archive, appSpec(), Limits{MaxEntries: 3})
		if !errors.Is(err, ErrLimitExceeded) {
			t.Fatalf("error = %v, want ErrLimitExceeded", err)
		}
	})

	t.Run("decompressed bytes", func(t *testing.T) {
		big := makeArchive(t, append(baseRepo(), fileEntry(testRoot+"/app/big.bin", strings.Repeat("x", 64<<10)))...)
		_, err := stage(t, big, appSpec(), Limits{MaxDecompressedBytes: 1024})
		if !errors.Is(err, ErrLimitExceeded) {
			t.Fatalf("error = %v, want ErrLimitExceeded", err)
		}
	})

	t.Run("compressed bytes", func(t *testing.T) {
		_, err := stage(t, archive, appSpec(), Limits{MaxCompressedBytes: 32})
		if !errors.Is(err, ErrLimitExceeded) {
			t.Fatalf("error = %v, want ErrLimitExceeded", err)
		}
	})
}

func TestStageContextRequiresPathsAndDockerfile(t *testing.T) {
	tests := []struct {
		name    string
		entries []tarEntry
		spec    ContextSpec
		want    error
	}{
		{
			name:    "missing context directory",
			entries: baseRepo(),
			spec:    ContextSpec{ContextPath: "missing", DockerfilePath: dockerfileName},
		},
		{
			name:    "missing Dockerfile",
			entries: baseRepo(),
			spec:    ContextSpec{ContextPath: testApp, DockerfilePath: "Nope"},
		},
		{
			name:    "traversal in context path",
			entries: baseRepo(),
			spec:    ContextSpec{ContextPath: "../app", DockerfilePath: dockerfileName},
			want:    ErrUnsafePath,
		},
		{
			name:    "context path is a symlink",
			entries: appendEntries(baseRepo(), linkEntry(testRoot+"/alias", testApp)),
			spec:    ContextSpec{ContextPath: "alias", DockerfilePath: dockerfileName},
			want:    ErrUnsafeEntry,
		},
		{
			name: "Dockerfile path goes through a symlink",
			entries: appendEntries(baseRepo(),
				linkEntry(testRoot+"/linkdir", testApp),
				fileEntry(testRoot+"/app/Dockerfile", testDockerfile)),
			spec: ContextSpec{ContextPath: testApp, DockerfilePath: "linkdir/Dockerfile"},
			want: ErrUnsafeEntry,
		},
		{
			name:    "Dockerfile is a symlink",
			entries: appendEntries(baseRepo(), linkEntry(testRoot+"/Link", "README.md")),
			spec:    ContextSpec{ContextPath: testApp, DockerfilePath: "Link"},
			want:    ErrUnsafeEntry,
		},
		{
			name: "Dockerfile over 1 MiB",
			entries: appendEntries(baseRepo(),
				fileEntry(testRoot+"/Big", "FROM x\n"+strings.Repeat("#", int(MaxDockerfileBytes)))),
			spec: ContextSpec{ContextPath: testApp, DockerfilePath: "Big"},
			want: ErrLimitExceeded,
		},
		{
			name:    "Dockerfile with a syntax directive",
			entries: appendEntries(baseRepo(), fileEntry(testRoot+"/Syntax", "# syntax=docker/dockerfile:1\nFROM alpine\n")),
			spec:    ContextSpec{ContextPath: testApp, DockerfilePath: "Syntax"},
			want:    ErrInvalidDockerfile,
		},
		{
			name:    "Dockerfile without FROM",
			entries: appendEntries(baseRepo(), fileEntry(testRoot+"/NoFrom", "RUN echo hi\n")),
			spec:    ContextSpec{ContextPath: testApp, DockerfilePath: "NoFrom"},
			want:    ErrInvalidDockerfile,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := stage(t, makeArchive(t, tt.entries...), tt.spec, Limits{})
			if err == nil {
				t.Fatal("expected an error")
			}

			if tt.want != nil && !errors.Is(err, tt.want) {
				t.Fatalf("error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestUnpackRoundTripsStagedContext(t *testing.T) {
	packed, err := stage(t, makeArchive(t, baseRepo()...), appSpec(), Limits{})
	if err != nil {
		t.Fatalf("StageContext: %v", err)
	}

	dst := t.TempDir()
	if err := Unpack(bytes.NewReader(packed), dst, Limits{}); err != nil {
		t.Fatalf("Unpack: %v", err)
	}

	body, err := os.ReadFile(filepath.Join(dst, "dockerfile", dockerfileName))
	if err != nil || string(body) != testDockerfile {
		t.Fatalf("dockerfile not restored: %v %q", err, body)
	}

	target, err := os.Readlink(filepath.Join(dst, "context", "alias.txt"))
	if err != nil || target != "main.txt" {
		t.Fatalf("symlink not restored: %v %q", err, target)
	}

	info, err := os.Stat(filepath.Join(dst, "context", "run.sh"))
	if err != nil || info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("executable bit not restored: %v %v", err, info)
	}
}

func TestUnpackRejectsUnsafeEntries(t *testing.T) {
	tests := []struct {
		name    string
		entries []tarEntry
	}{
		{name: "traversal", entries: []tarEntry{fileEntry("a/../../x", "x")}},
		{name: "absolute", entries: []tarEntry{fileEntry("/x", "x")}},
		{name: "escaping symlink", entries: []tarEntry{linkEntry("l", "../x")}},
		{name: "absolute symlink", entries: []tarEntry{linkEntry("l", "/etc")}},
		{
			name: "hardlink",
			entries: []tarEntry{
				fileEntry("a", "x"),
				{name: "b", typeflag: tar.TypeLink, linkname: "a"},
			},
		},
		{name: "device", entries: []tarEntry{{name: "d", typeflag: tar.TypeBlock}}},
		{
			name:    "write through symlink",
			entries: []tarEntry{linkEntry("d", "."), fileEntry("d/x", "y")},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Unpack(bytes.NewReader(makeArchive(t, tt.entries...)), t.TempDir(), Limits{})
			if !errors.Is(err, ErrUnsafeEntry) {
				t.Fatalf("Unpack error = %v, want ErrUnsafeEntry", err)
			}
		})
	}
}
