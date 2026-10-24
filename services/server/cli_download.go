package main

import (
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"

	"github.com/go-chi/chi/v5"
)

// cliTargets lists the GOOS/GOARCH pairs the server image ships CLI binaries for.
var cliTargets = map[string]struct{}{
	"linux/amd64":   {},
	"linux/arm64":   {},
	"darwin/amd64":  {},
	"darwin/arm64":  {},
	"windows/amd64": {},
}

// cliBinaryName returns the file name of the CLI binary for a target. The
// Dockerfile writes binaries with the same naming scheme.
func cliBinaryName(goos, goarch string) string {
	name := fmt.Sprintf("opendepot_%s_%s", goos, goarch)
	if goos == "windows" {
		name += ".exe"
	}
	return name
}

func serveCLIBinary(w http.ResponseWriter, r *http.Request) {
	goos := chi.URLParam(r, "os")
	goarch := chi.URLParam(r, "arch")
	if _, ok := cliTargets[goos+"/"+goarch]; !ok {
		http.NotFound(w, r)
		return
	}

	serveCLIFile(w, r, cliBinaryName(goos, goarch), "application/octet-stream")
}

func serveCLIChecksums(w http.ResponseWriter, r *http.Request) {
	serveCLIFile(w, r, "SHA256SUMS", "text/plain; charset=utf-8")
}

func serveCLIFile(w http.ResponseWriter, r *http.Request, name, contentType string) {
	f, err := os.Open(filepath.Join(*opendepotCLIDistDir, name))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			http.NotFound(w, r)
			return
		}
		logger.Error("failed to open CLI artifact", "file", name, "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, name, info.ModTime(), f)
}
