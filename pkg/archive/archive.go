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

// Package archive extracts module and provider archives safely and writes deterministic
// gzip-compressed tarballs so that the same source tree always produces identical bytes.
package archive

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	deterministicFileMode os.FileMode = 0644
	deterministicDirMode  os.FileMode = 0755
)

const (
	maxExtractedBytes   int64 = 2 << 30
	maxExtractedEntries       = 10000
)

// extractBudget caps the total decompressed bytes and entry count written by a single ExtractToDir call.
type extractBudget struct {
	bytesLeft   int64
	entriesLeft int
}

func newExtractBudget() *extractBudget {
	return &extractBudget{bytesLeft: maxExtractedBytes, entriesLeft: maxExtractedEntries}
}

func (b *extractBudget) takeEntry(name string) error {
	if b.entriesLeft <= 0 {
		return fmt.Errorf("archive exceeds %d entries (at %q)", maxExtractedEntries, name)
	}

	b.entriesLeft--

	return nil
}

// copyEntry copies at most the remaining byte budget from src to dst and fails when src holds more.
func (b *extractBudget) copyEntry(dst io.Writer, src io.Reader, name string) error {
	n, err := io.Copy(dst, io.LimitReader(src, b.bytesLeft+1))
	if n > b.bytesLeft {
		return fmt.Errorf("archive exceeds %d decompressed bytes (at %q)", maxExtractedBytes, name)
	}

	b.bytesLeft -= n

	return err
}

// ExtractToDir extracts the contents of an archive (zip or gzip tarball) held in memory into destDir.
// Entries whose cleaned path escapes destDir are rejected, and symbolic and hard link entries are
// refused outright so that no link can later redirect a write outside destDir.
func ExtractToDir(archiveBytes []byte, destDir string) error {
	budget := newExtractBudget()
	if zr, err := zip.NewReader(bytes.NewReader(archiveBytes), int64(len(archiveBytes))); err == nil {
		for _, f := range zr.File {
			if err := budget.takeEntry(f.Name); err != nil {
				return err
			}

			if err := extractZipEntry(f, destDir, budget); err != nil {
				return err
			}
		}

		return nil
	}

	gr, err := gzip.NewReader(bytes.NewReader(archiveBytes))
	if err != nil {
		return fmt.Errorf("archive is neither a valid zip nor a gzip tarball: %w", err)
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}

		if err != nil {
			return fmt.Errorf("failed to read tar entry: %w", err)
		}

		if err := budget.takeEntry(hdr.Name); err != nil {
			return err
		}

		if err := extractTarEntry(tr, hdr, destDir, budget); err != nil {
			return err
		}
	}

	return nil
}

// WriteDeterministicTarGz writes srcDir as a gzip-compressed tarball to w. Entries are emitted in
// lexical order, timestamps are set to the Unix epoch, ownership and user/group names are cleared,
// and modes are normalized to 0644 for files and 0755 for directories. The srcDir root itself is
// not included as an entry. Symbolic links, hard links, and special files are rejected.
func WriteDeterministicTarGz(srcDir string, w io.Writer) error {
	gw := gzip.NewWriter(w)
	tw := tar.NewWriter(gw)

	err := filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if path == srcDir {
			return nil
		}

		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return fmt.Errorf("failed to compute relative path for %s: %w", path, err)
		}

		name := filepath.ToSlash(rel)
		info, err := d.Info()
		if err != nil {
			return fmt.Errorf("failed to stat %s: %w", path, err)
		}

		switch {
		case info.IsDir():
			return tw.WriteHeader(deterministicHeader(name+"/", tar.TypeDir, deterministicDirMode, 0))
		case info.Mode().IsRegular():
			return writeDeterministicFile(tw, path, name, info.Size())
		default:
			return fmt.Errorf("unsupported file type for archive entry %s: %s", name, info.Mode().Type())
		}
	})
	if err != nil {
		return fmt.Errorf("failed to write deterministic archive: %w", err)
	}

	if err := tw.Close(); err != nil {
		return fmt.Errorf("failed to close tar writer: %w", err)
	}

	if err := gw.Close(); err != nil {
		return fmt.Errorf("failed to close gzip writer: %w", err)
	}

	return nil
}

// SHA256Hex returns the lowercase hex SHA-256 digest of archiveBytes. This is the value used for
// the zh: archive hash in lock files and signed SHA256SUMS.
func SHA256Hex(archiveBytes []byte) string {
	sum := sha256.Sum256(archiveBytes)

	return hex.EncodeToString(sum[:])
}

// extractZipEntry writes a single zip file entry into destDir, guarding against path traversal
// and refusing symbolic links.
func extractZipEntry(f *zip.File, destDir string, budget *extractBudget) error {
	dest := filepath.Join(destDir, f.Name)
	if !strings.HasPrefix(filepath.Clean(dest)+string(os.PathSeparator), filepath.Clean(destDir)+string(os.PathSeparator)) {
		return fmt.Errorf("zip entry %q escapes destination directory (path traversal)", f.Name)
	}

	if f.Mode()&fs.ModeSymlink != 0 {
		return fmt.Errorf("zip entry %q is a symbolic link, which is not allowed", f.Name)
	}

	if f.FileInfo().IsDir() {
		return os.MkdirAll(dest, 0700)
	}

	if err := os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
		return fmt.Errorf("failed to create directory for %s: %w", f.Name, err)
	}

	rc, err := f.Open()
	if err != nil {
		return fmt.Errorf("failed to open zip entry %s: %w", f.Name, err)
	}
	defer rc.Close()

	out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("failed to create file %s: %w", dest, err)
	}
	defer out.Close()

	if err := budget.copyEntry(out, rc, f.Name); err != nil { //nolint:gosec // G110: copy is capped by extractBudget
		return fmt.Errorf("failed to write zip entry %s: %w", f.Name, err)
	}

	return nil
}

// extractTarEntry writes a single tar entry into destDir, guarding against path traversal and
// refusing symbolic and hard links.
func extractTarEntry(tr *tar.Reader, hdr *tar.Header, destDir string, budget *extractBudget) error {
	dest := filepath.Join(destDir, hdr.Name)
	if !strings.HasPrefix(filepath.Clean(dest)+string(os.PathSeparator), filepath.Clean(destDir)+string(os.PathSeparator)) {
		return fmt.Errorf("tar entry %q escapes destination directory (path traversal)", hdr.Name)
	}

	switch hdr.Typeflag {
	case tar.TypeSymlink:
		return fmt.Errorf("tar entry %q is a symbolic link, which is not allowed", hdr.Name)
	case tar.TypeLink:
		return fmt.Errorf("tar entry %q is a hard link, which is not allowed", hdr.Name)
	case tar.TypeDir:
		return os.MkdirAll(dest, 0700)
	case tar.TypeReg:
		if err := os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
			return fmt.Errorf("failed to create directory for %s: %w", hdr.Name, err)
		}

		out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
		if err != nil {
			return fmt.Errorf("failed to create file %s: %w", dest, err)
		}
		defer out.Close()

		if err := budget.copyEntry(out, tr, hdr.Name); err != nil { //nolint:gosec // G110: copy is capped by extractBudget
			return fmt.Errorf("failed to write tar entry %s: %w", hdr.Name, err)
		}
	}

	return nil
}

// writeDeterministicFile writes a single regular file from path into tw under name.
func writeDeterministicFile(tw *tar.Writer, path, name string, size int64) error {
	if err := tw.WriteHeader(deterministicHeader(name, tar.TypeReg, deterministicFileMode, size)); err != nil {
		return fmt.Errorf("failed to write header for %s: %w", name, err)
	}

	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("failed to open %s: %w", path, err)
	}
	defer f.Close()

	if _, err := io.Copy(tw, f); err != nil {
		return fmt.Errorf("failed to write %s: %w", name, err)
	}

	return nil
}

// deterministicHeader returns a tar header with all host-specific metadata zeroed.
func deterministicHeader(name string, typeflag byte, mode os.FileMode, size int64) *tar.Header {
	return &tar.Header{
		Name:     name,
		Typeflag: typeflag,
		Mode:     int64(mode),
		Size:     size,
		ModTime:  time.Unix(0, 0),
	}
}
