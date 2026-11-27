//go:build coverage

package main

import (
	"fmt"
	"os"
	"runtime/coverage"
)

// coverageFlush writes coverage metadata when main returns. The runtime then writes counters on exit,
// which Go does not do for a plain return from main.
func coverageFlush() {
	dir := os.Getenv("GOCOVERDIR")
	if dir == "" {
		return
	}

	if err := coverage.WriteMetaDir(dir); err != nil {
		fmt.Fprintln(os.Stderr, "coverage: write meta:", err)
	}
}
