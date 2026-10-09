package main

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"golang.org/x/mod/sumdb/dirhash"
)

// dirHashTree returns the h1: directory hash of every regular file under root.
func dirHashTree(root string) (string, error) {
	var files []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}

		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}

		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("hash %s: %w", root, err)
	}

	return dirhash.Hash1(files, func(name string) (io.ReadCloser, error) {
		return os.Open(filepath.Join(root, filepath.FromSlash(name)))
	})
}

// hashFile returns the h1: hash of a single file, hashed as a one-file directory.
func hashFile(path string) (string, error) {
	dir, base := filepath.Split(path)
	return dirhash.Hash1([]string{base}, func(name string) (io.ReadCloser, error) {
		return os.Open(filepath.Join(dir, name))
	})
}

// hashBlock returns the h1: hash of an AGENTS.md block's inner content.
func hashBlock(inner string) (string, error) {
	return dirhash.Hash1([]string{"block"}, func(string) (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader([]byte(inner))), nil
	})
}

// copyTree copies the regular files under src into dst.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}

		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}

		if !d.Type().IsRegular() {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		return copyFile(p, target, info.Mode().Perm())
	})
}

func copyFile(src, dst string, perm os.FileMode) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}

	return os.WriteFile(dst, data, perm)
}
