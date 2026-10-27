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

func printError(w io.Writer, err error) {
	var blocked *blockedVersionError
	if errors.As(err, &blocked) {
		source := ""
		var src *sourceError
		if errors.As(err, &src) {
			source = src.source
		}

		printBlockedVersion(w, source, blocked)
		return
	}

	st := newStyler(w)
	header := "Error"
	detail := err.Error()
	if src, ok := err.(*sourceError); ok {
		header = src.source
		detail = src.err.Error()
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, st.paint(ansiBold+ansiRed, "✖ "+header))
	writeBlockedRows(w, st, "Error", detail, ansiRed)
}
