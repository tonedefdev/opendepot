package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
)

// buildVersion is stamped at build time with -ldflags "-X main.buildVersion=...".
var buildVersion = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	err := newRootCommand(os.Stdin, os.Stdout, os.Stderr).ExecuteContext(ctx)
	stop()
	if err != nil {
		printError(os.Stderr, err)
		os.Exit(1)
	}
}

// printError explains a Jev-blocked Version in columns and prints every other failure as an error line.
func printError(w io.Writer, err error) {
	var blocked *blockedVersionError
	if !errors.As(err, &blocked) {
		fmt.Fprintln(w, "error:", err)
		return
	}

	source := ""
	var src *sourceError
	if errors.As(err, &src) {
		source = src.source
	}

	printBlockedVersion(w, source, blocked)
}
