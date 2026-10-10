/*
Copyright 2026 Tony Owens.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package archive

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func buildZip(t *testing.T, entries map[string]fs.FileMode, contents map[string]string) []byte {
	t.Helper()

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, mode := range entries {
		hdr := &zip.FileHeader{Name: name}
		hdr.SetMode(mode)
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			t.Fatalf("CreateHeader(%q) error = %v", name, err)
		}

		if _, err := io.WriteString(w, contents[name]); err != nil {
			t.Fatalf("write zip entry %q error = %v", name, err)
		}
	}

	if err := zw.Close(); err != nil {
		t.Fatalf("zip close error = %v", err)
	}

	return buf.Bytes()
}

func buildTarGz(t *testing.T, headers []*tar.Header, contents map[string]string) []byte {
	t.Helper()

	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	for _, hdr := range headers {
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("WriteHeader(%q) error = %v", hdr.Name, err)
		}

		if hdr.Typeflag == tar.TypeReg {
			if _, err := io.WriteString(tw, contents[hdr.Name]); err != nil {
				t.Fatalf("write tar entry %q error = %v", hdr.Name, err)
			}
		}
	}

	if err := tw.Close(); err != nil {
		t.Fatalf("tar close error = %v", err)
	}

	if err := gw.Close(); err != nil {
		t.Fatalf("gzip close error = %v", err)
	}

	return buf.Bytes()
}

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()

	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatalf("MkdirAll error = %v", err)
		}

		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatalf("WriteFile error = %v", err)
		}
	}
}

// readTree returns every regular file under root keyed by slash-separated relative path.
func readTree(t *testing.T, root string) map[string]string {
	t.Helper()

	files := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			return nil
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		files[filepath.ToSlash(rel)] = string(content)

		return nil
	})
	if err != nil {
		t.Fatalf("WalkDir error = %v", err)
	}

	return files
}

func assertNoEscape(t *testing.T, parent string) {
	t.Helper()

	if _, err := os.Stat(filepath.Join(parent, "escaped.txt")); err == nil {
		t.Fatal("entry escaped the destination directory")
	}
}

func TestExtractToDirRejectsTraversal(t *testing.T) {
	traversal := []string{"../escaped.txt", "a/../../escaped.txt"}

	for _, name := range traversal {
		t.Run("zip "+name, func(t *testing.T) {
			parent := t.TempDir()
			dest := filepath.Join(parent, "dest")
			if err := os.Mkdir(dest, 0755); err != nil {
				t.Fatal(err)
			}

			archive := buildZip(t, map[string]fs.FileMode{name: 0644}, map[string]string{name: "x"})
			if err := ExtractToDir(archive, dest); err == nil {
				t.Fatal("ExtractToDir() error = nil, want path traversal error")
			}

			assertNoEscape(t, parent)
		})

		t.Run("tar "+name, func(t *testing.T) {
			parent := t.TempDir()
			dest := filepath.Join(parent, "dest")
			if err := os.Mkdir(dest, 0755); err != nil {
				t.Fatal(err)
			}

			hdr := &tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0644, Size: 1}
			archive := buildTarGz(t, []*tar.Header{hdr}, map[string]string{name: "x"})
			if err := ExtractToDir(archive, dest); err == nil {
				t.Fatal("ExtractToDir() error = nil, want path traversal error")
			}

			assertNoEscape(t, parent)
		})
	}
}

func TestExtractToDirRejectsLinks(t *testing.T) {
	t.Run("zip symlink", func(t *testing.T) {
		dest := t.TempDir()
		archive := buildZip(t, map[string]fs.FileMode{"link": fs.ModeSymlink | 0777}, map[string]string{"link": "/etc/passwd"})
		if err := ExtractToDir(archive, dest); err == nil {
			t.Fatal("ExtractToDir() error = nil, want symlink rejection")
		}
	})

	t.Run("tar symlink", func(t *testing.T) {
		dest := t.TempDir()
		hdr := &tar.Header{Name: "link", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd", Mode: 0777}
		archive := buildTarGz(t, []*tar.Header{hdr}, nil)
		if err := ExtractToDir(archive, dest); err == nil {
			t.Fatal("ExtractToDir() error = nil, want symlink rejection")
		}

		if _, err := os.Lstat(filepath.Join(dest, "link")); err == nil {
			t.Fatal("symlink entry was created")
		}
	})

	t.Run("tar hardlink", func(t *testing.T) {
		dest := t.TempDir()
		hdr := &tar.Header{Name: "link", Typeflag: tar.TypeLink, Linkname: "/etc/passwd", Mode: 0644}
		archive := buildTarGz(t, []*tar.Header{hdr}, nil)
		if err := ExtractToDir(archive, dest); err == nil {
			t.Fatal("ExtractToDir() error = nil, want hard link rejection")
		}
	})
}

func TestWriteDeterministicTarGzRejectsSymlinks(t *testing.T) {
	src := t.TempDir()
	writeTree(t, src, map[string]string{"main.tf": "resource {}"})
	if err := os.Symlink("/etc/passwd", filepath.Join(src, "link")); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := WriteDeterministicTarGz(src, &buf); err == nil {
		t.Fatal("WriteDeterministicTarGz() error = nil, want symlink rejection")
	}
}

func TestWriteDeterministicTarGzIsRepeatable(t *testing.T) {
	src := t.TempDir()
	writeTree(t, src, map[string]string{
		"main.tf":            "resource {}",
		"modules/net/vars.f": "variable {}",
		"README.md":          "# readme",
	})

	var first, second bytes.Buffer
	if err := WriteDeterministicTarGz(src, &first); err != nil {
		t.Fatalf("first WriteDeterministicTarGz() error = %v", err)
	}

	if err := WriteDeterministicTarGz(src, &second); err != nil {
		t.Fatalf("second WriteDeterministicTarGz() error = %v", err)
	}

	if !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Fatal("repacking the same tree produced different bytes")
	}

	mtime := time.Date(2001, 2, 3, 4, 5, 6, 0, time.UTC)
	if err := os.Chtimes(filepath.Join(src, "main.tf"), mtime, mtime); err != nil {
		t.Fatal(err)
	}

	var afterTouch bytes.Buffer
	if err := WriteDeterministicTarGz(src, &afterTouch); err != nil {
		t.Fatalf("WriteDeterministicTarGz() after chtimes error = %v", err)
	}

	if !bytes.Equal(first.Bytes(), afterTouch.Bytes()) {
		t.Fatal("changing a file mtime changed the archive bytes")
	}
}

func TestWriteDeterministicTarGzNormalizesHeaders(t *testing.T) {
	src := t.TempDir()
	writeTree(t, src, map[string]string{"dir/main.tf": "resource {}"})
	if err := os.Chmod(filepath.Join(src, "dir", "main.tf"), 0755); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := WriteDeterministicTarGz(src, &buf); err != nil {
		t.Fatalf("WriteDeterministicTarGz() error = %v", err)
	}

	gr, err := gzip.NewReader(&buf)
	if err != nil {
		t.Fatalf("gzip.NewReader() error = %v", err)
	}

	tr := tar.NewReader(gr)
	var names []string
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}

		if err != nil {
			t.Fatalf("tar Next() error = %v", err)
		}

		names = append(names, hdr.Name)
		if hdr.Uid != 0 || hdr.Gid != 0 || hdr.Uname != "" || hdr.Gname != "" {
			t.Errorf("%s: ownership not zeroed: uid=%d gid=%d uname=%q gname=%q", hdr.Name, hdr.Uid, hdr.Gid, hdr.Uname, hdr.Gname)
		}

		if !hdr.ModTime.Equal(time.Unix(0, 0)) {
			t.Errorf("%s: mtime = %v, want Unix epoch", hdr.Name, hdr.ModTime)
		}

		wantMode := int64(0644)
		if hdr.Typeflag == tar.TypeDir {
			wantMode = 0755
		}

		if hdr.Mode != wantMode {
			t.Errorf("%s: mode = %o, want %o", hdr.Name, hdr.Mode, wantMode)
		}
	}

	want := []string{"dir/", "dir/main.tf"}
	if !slices.Equal(names, want) {
		t.Fatalf("entries = %v, want %v", names, want)
	}
}

func TestDeterministicRoundTrip(t *testing.T) {
	src := t.TempDir()
	files := map[string]string{
		"main.tf":                "resource {}",
		"modules/net/main.tf":    "module {}",
		"modules/net/outputs.tf": "output {}",
	}
	writeTree(t, src, files)

	var buf bytes.Buffer
	if err := WriteDeterministicTarGz(src, &buf); err != nil {
		t.Fatalf("WriteDeterministicTarGz() error = %v", err)
	}

	dest := t.TempDir()
	if err := ExtractToDir(buf.Bytes(), dest); err != nil {
		t.Fatalf("ExtractToDir() error = %v", err)
	}

	got := readTree(t, dest)
	if len(got) != len(files) {
		t.Fatalf("extracted %d files, want %d: %v", len(got), len(files), got)
	}

	for name, content := range files {
		if got[name] != content {
			t.Errorf("%s = %q, want %q", name, got[name], content)
		}
	}
}

func TestSHA256Hex(t *testing.T) {
	want := "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	if got := SHA256Hex([]byte("abc")); got != want {
		t.Fatalf("SHA256Hex() = %q, want %q", got, want)
	}
}

func TestExtractBudgetRejectsOversizedEntry(t *testing.T) {
	budget := &extractBudget{bytesLeft: 4, entriesLeft: 1}
	var out bytes.Buffer
	if err := budget.copyEntry(&out, strings.NewReader("12345"), "big.bin"); err == nil {
		t.Fatal("copyEntry() succeeded past the byte budget, want error")
	}

	budget = &extractBudget{bytesLeft: 5, entriesLeft: 1}
	out.Reset()
	if err := budget.copyEntry(&out, strings.NewReader("12345"), "exact.bin"); err != nil {
		t.Fatalf("copyEntry() error = %v, want nil at exact budget", err)
	}
}

func TestExtractBudgetRejectsTooManyEntries(t *testing.T) {
	budget := &extractBudget{bytesLeft: 1, entriesLeft: 1}
	if err := budget.takeEntry("a"); err != nil {
		t.Fatalf("takeEntry() first error = %v", err)
	}

	if err := budget.takeEntry("b"); err == nil {
		t.Fatal("takeEntry() succeeded past the entry budget, want error")
	}
}

func TestExtractToDirRejectsTooManyEntries(t *testing.T) {
	modes := make(map[string]fs.FileMode, maxExtractedEntries+1)
	contents := make(map[string]string, maxExtractedEntries+1)
	for i := range maxExtractedEntries + 1 {
		name := fmt.Sprintf("f%05d.txt", i)
		modes[name] = 0644
		contents[name] = "x"
	}

	dest := t.TempDir()
	archive := buildZip(t, modes, contents)
	if err := ExtractToDir(archive, dest); err == nil {
		t.Fatal("ExtractToDir() error = nil, want entry budget error")
	}
}

func TestExtractSubtreeToDirSkipsLinksOutsideSubtree(t *testing.T) {
	dest := t.TempDir()
	headers := []*tar.Header{
		{Name: "repo-abc/", Typeflag: tar.TypeDir, Mode: 0755},
		{Name: "repo-abc/skills/", Typeflag: tar.TypeDir, Mode: 0755},
		{Name: "repo-abc/skills/aspire/", Typeflag: tar.TypeDir, Mode: 0755},
		{Name: "repo-abc/skills/aspire/SKILL.md", Typeflag: tar.TypeReg, Mode: 0644, Size: 1},
		{Name: "repo-abc/plugins/agents/link.md", Typeflag: tar.TypeSymlink, Linkname: "../../skills/aspire/SKILL.md", Mode: 0777},
		{Name: "repo-abc/other/file.md", Typeflag: tar.TypeReg, Mode: 0644, Size: 1},
	}
	contents := map[string]string{
		"repo-abc/skills/aspire/SKILL.md": "x",
		"repo-abc/other/file.md":          "y",
	}

	archive := buildTarGz(t, headers, contents)
	if err := ExtractSubtreeToDir(archive, dest, "skills/aspire"); err != nil {
		t.Fatalf("ExtractSubtreeToDir() error = %v", err)
	}

	got := readTree(t, dest)
	want := map[string]string{"repo-abc/skills/aspire/SKILL.md": "x"}
	if !maps.Equal(got, want) {
		t.Fatalf("extracted tree = %v, want %v", got, want)
	}
}

func TestExtractSubtreeToDirRejectsLinksInsideSubtree(t *testing.T) {
	dest := t.TempDir()
	headers := []*tar.Header{
		{Name: "repo-abc/skills/aspire/link", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd", Mode: 0777},
	}

	archive := buildTarGz(t, headers, nil)
	if err := ExtractSubtreeToDir(archive, dest, "skills/aspire"); err == nil {
		t.Fatal("ExtractSubtreeToDir() error = nil, want symlink rejection")
	}

	if _, err := os.Lstat(filepath.Join(dest, "repo-abc/skills/aspire/link")); err == nil {
		t.Fatal("symlink entry was created")
	}
}

func TestExtractSubtreeToDirDoesNotMatchSiblingPrefix(t *testing.T) {
	dest := t.TempDir()
	headers := []*tar.Header{
		{Name: "repo-abc/skills/aspire-extra/SKILL.md", Typeflag: tar.TypeReg, Mode: 0644, Size: 1},
	}
	contents := map[string]string{"repo-abc/skills/aspire-extra/SKILL.md": "x"}

	archive := buildTarGz(t, headers, contents)
	if err := ExtractSubtreeToDir(archive, dest, "skills/aspire"); err == nil {
		t.Fatal("ExtractSubtreeToDir() error = nil, want no-match error")
	}

	if got := readTree(t, dest); len(got) != 0 {
		t.Fatalf("extracted tree = %v, want empty", got)
	}
}

func TestExtractSubtreeToDirZipSkipsLinksOutsideSubtree(t *testing.T) {
	dest := t.TempDir()
	modes := map[string]fs.FileMode{
		"repo-abc/skills/aspire/SKILL.md": 0644,
		"repo-abc/plugins/agents/link.md": fs.ModeSymlink | 0777,
		"repo-abc/other/file.md":          0644,
	}
	contents := map[string]string{
		"repo-abc/skills/aspire/SKILL.md": "x",
		"repo-abc/plugins/agents/link.md": "../../skills/aspire/SKILL.md",
		"repo-abc/other/file.md":          "y",
	}

	archive := buildZip(t, modes, contents)
	if err := ExtractSubtreeToDir(archive, dest, "skills/aspire"); err != nil {
		t.Fatalf("ExtractSubtreeToDir() error = %v", err)
	}

	got := readTree(t, dest)
	want := map[string]string{"repo-abc/skills/aspire/SKILL.md": "x"}
	if !maps.Equal(got, want) {
		t.Fatalf("extracted tree = %v, want %v", got, want)
	}
}

func TestExtractSubtreeToDirZipRejectsLinksInsideSubtree(t *testing.T) {
	dest := t.TempDir()
	modes := map[string]fs.FileMode{"repo-abc/skills/aspire/link": fs.ModeSymlink | 0777}
	contents := map[string]string{"repo-abc/skills/aspire/link": "/etc/passwd"}

	archive := buildZip(t, modes, contents)
	if err := ExtractSubtreeToDir(archive, dest, "skills/aspire"); err == nil {
		t.Fatal("ExtractSubtreeToDir() error = nil, want symlink rejection")
	}

	if _, err := os.Lstat(filepath.Join(dest, "repo-abc/skills/aspire/link")); err == nil {
		t.Fatal("symlink entry was created")
	}
}

func TestExtractSubtreeToDirZipDoesNotMatchSiblingPrefix(t *testing.T) {
	dest := t.TempDir()
	modes := map[string]fs.FileMode{"repo-abc/skills/aspire-extra/SKILL.md": 0644}
	contents := map[string]string{"repo-abc/skills/aspire-extra/SKILL.md": "x"}

	archive := buildZip(t, modes, contents)
	if err := ExtractSubtreeToDir(archive, dest, "skills/aspire"); err == nil {
		t.Fatal("ExtractSubtreeToDir() error = nil, want no-match error")
	}

	if got := readTree(t, dest); len(got) != 0 {
		t.Fatalf("extracted tree = %v, want empty", got)
	}
}

func TestExtractSubtreeToDirSkipsDotDotEntries(t *testing.T) {
	names := []string{
		"repo-abc/skills/aspire/../../other/escape.txt",
		"repo-abc/skills/aspire/../../../escaped.txt",
		"repo-abc/skills/aspire/../../../../escaped.txt",
	}

	t.Run("tar", func(t *testing.T) {
		dest := t.TempDir()
		headers := []*tar.Header{{Name: "repo-abc/skills/aspire/SKILL.md", Typeflag: tar.TypeReg, Mode: 0644, Size: 1}}
		contents := map[string]string{"repo-abc/skills/aspire/SKILL.md": "x"}
		for _, name := range names {
			headers = append(headers, &tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0644, Size: 1})
			contents[name] = "y"
		}

		archive := buildTarGz(t, headers, contents)
		if err := ExtractSubtreeToDir(archive, dest, "skills/aspire"); err != nil {
			t.Fatalf("ExtractSubtreeToDir() error = %v", err)
		}

		got := readTree(t, dest)
		want := map[string]string{"repo-abc/skills/aspire/SKILL.md": "x"}
		if !maps.Equal(got, want) {
			t.Fatalf("extracted tree = %v, want %v", got, want)
		}

		assertNoEscape(t, dest)
		assertNoEscape(t, filepath.Dir(dest))
	})

	t.Run("zip", func(t *testing.T) {
		dest := t.TempDir()
		modes := map[string]fs.FileMode{"repo-abc/skills/aspire/SKILL.md": 0644}
		contents := map[string]string{"repo-abc/skills/aspire/SKILL.md": "x"}
		for _, name := range names {
			modes[name] = 0644
			contents[name] = "y"
		}

		archive := buildZip(t, modes, contents)
		if err := ExtractSubtreeToDir(archive, dest, "skills/aspire"); err != nil {
			t.Fatalf("ExtractSubtreeToDir() error = %v", err)
		}

		got := readTree(t, dest)
		want := map[string]string{"repo-abc/skills/aspire/SKILL.md": "x"}
		if !maps.Equal(got, want) {
			t.Fatalf("extracted tree = %v, want %v", got, want)
		}

		assertNoEscape(t, dest)
		assertNoEscape(t, filepath.Dir(dest))
	})
}

func TestExtractSubtreeToDirCountsSkippedEntries(t *testing.T) {
	headers := make([]*tar.Header, 0, maxExtractedEntries+1)
	modes := make(map[string]fs.FileMode, maxExtractedEntries+1)
	for i := range maxExtractedEntries + 1 {
		name := fmt.Sprintf("repo-abc/other/f%05d.txt", i)
		headers = append(headers, &tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0644})
		modes[name] = 0644
	}

	t.Run("tar", func(t *testing.T) {
		headers = append(headers, &tar.Header{Name: "repo-abc/skills/aspire/SKILL.md", Typeflag: tar.TypeReg, Mode: 0644, Size: 1})
		archive := buildTarGz(t, headers, map[string]string{"repo-abc/skills/aspire/SKILL.md": "x"})
		if err := ExtractSubtreeToDir(archive, t.TempDir(), "skills/aspire"); err == nil {
			t.Fatal("ExtractSubtreeToDir() error = nil, want entry budget error from skipped entries")
		}
	})

	t.Run("zip", func(t *testing.T) {
		modes["repo-abc/skills/aspire/SKILL.md"] = 0644
		archive := buildZip(t, modes, map[string]string{"repo-abc/skills/aspire/SKILL.md": "x"})
		if err := ExtractSubtreeToDir(archive, t.TempDir(), "skills/aspire"); err == nil {
			t.Fatal("ExtractSubtreeToDir() error = nil, want entry budget error from skipped entries")
		}
	})
}

func TestExtractBudgetSkipEntryChargesBytes(t *testing.T) {
	budget := &extractBudget{bytesLeft: 4, entriesLeft: 1}
	if err := budget.skipEntry("big.bin", 5); err == nil {
		t.Fatal("skipEntry() succeeded past the byte budget, want error")
	}

	if err := budget.skipEntry("negative.bin", -1); err == nil {
		t.Fatal("skipEntry() accepted a negative size, want error")
	}

	if err := budget.skipEntry("exact.bin", 4); err != nil {
		t.Fatalf("skipEntry() error = %v, want nil at exact budget", err)
	}

	if budget.bytesLeft != 0 {
		t.Fatalf("bytesLeft = %d, want 0", budget.bytesLeft)
	}
}

func TestExtractSubtreeToDirRootExtractsWholeArchive(t *testing.T) {
	for _, subtree := range []string{"", "/"} {
		t.Run("subtree "+subtree, func(t *testing.T) {
			dest := t.TempDir()
			headers := []*tar.Header{
				{Name: "repo-abc/README.md", Typeflag: tar.TypeReg, Mode: 0644, Size: 1},
				{Name: "repo-abc/skills/aspire/SKILL.md", Typeflag: tar.TypeReg, Mode: 0644, Size: 1},
			}
			contents := map[string]string{"repo-abc/README.md": "r", "repo-abc/skills/aspire/SKILL.md": "x"}

			archive := buildTarGz(t, headers, contents)
			if err := ExtractSubtreeToDir(archive, dest, subtree); err != nil {
				t.Fatalf("ExtractSubtreeToDir() error = %v", err)
			}

			got := readTree(t, dest)
			want := map[string]string{"repo-abc/README.md": "r", "repo-abc/skills/aspire/SKILL.md": "x"}
			if !maps.Equal(got, want) {
				t.Fatalf("extracted tree = %v, want %v", got, want)
			}
		})
	}
}

func TestExtractSubtreeToDirNoMatchReturnsClearError(t *testing.T) {
	headers := []*tar.Header{{Name: "repo-abc/other/file.md", Typeflag: tar.TypeReg, Mode: 0644, Size: 1}}
	archive := buildTarGz(t, headers, map[string]string{"repo-abc/other/file.md": "y"})

	err := ExtractSubtreeToDir(archive, t.TempDir(), "skills/missing")
	if err == nil || !strings.Contains(err.Error(), `no entries under subtree "skills/missing"`) {
		t.Fatalf("ExtractSubtreeToDir() error = %v, want no-entries error naming the subtree", err)
	}
}
