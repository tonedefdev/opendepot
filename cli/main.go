package main

import (
	"context"
	"fmt"
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
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
