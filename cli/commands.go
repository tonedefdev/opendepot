package main

import (
	"bufio"
	"io"
	"net/http"
	"time"

	"github.com/spf13/cobra"
)

type app struct {
	in         *bufio.Reader
	out        io.Writer
	httpClient *http.Client
	scheme     string
	dir        string
	tokenFor   func(host string) string
}

func newRootCommand(in io.Reader, out, errOut io.Writer) *cobra.Command {
	a := &app{
		in:         bufio.NewReader(in),
		out:        out,
		httpClient: &http.Client{Timeout: 2 * time.Minute},
		scheme:     "https",
		dir:        ".",
	}

	root := &cobra.Command{
		Use:           "opendepot",
		Short:         "Install and verify OpenDepot skills and agents",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.SetOut(out)
	root.SetErr(errOut)

	root.AddCommand(&cobra.Command{
		Use:   "login <host>",
		Short: "Log in to a registry with the login.v1 flow and store the token",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.login(cmd.Context(), args[0])
		},
	})

	var opts applyOptions
	applyCmd := &cobra.Command{
		Use:   "apply",
		Short: "Resolve, verify, and install the entries in opendepot.hcl",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.applyProject(cmd.Context(), opts)
		},
	}

	applyCmd.Flags().BoolVar(&opts.upgrade, "upgrade", false, "re-resolve constraints instead of reusing locked versions")
	applyCmd.Flags().BoolVar(&opts.allowYanked, "allow-yanked", false, "allow yanked versions")
	applyCmd.Flags().BoolVar(&opts.trustNewKey, "trust-new-key", false, "re-pin a rotated signing key")
	applyCmd.Flags().BoolVar(&opts.noPrompt, "no-prompt", false, "never prompt; fail instead")
	applyCmd.Flags().BoolVar(&opts.force, "force", false, "overwrite AGENTS.md blocks that were edited by hand")
	root.AddCommand(applyCmd)

	var planOpts applyOptions
	planCmd := &cobra.Command{
		Use:   "plan",
		Short: "Show what apply would change without writing anything",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			planOpts.dryRun = true
			planOpts.noPrompt = true
			return a.applyProject(cmd.Context(), planOpts)
		},
	}

	planCmd.Flags().BoolVar(&planOpts.upgrade, "upgrade", false, "plan re-resolved constraints instead of locked versions")
	planCmd.Flags().BoolVar(&planOpts.allowYanked, "allow-yanked", false, "allow yanked versions")
	planCmd.Flags().BoolVar(&planOpts.trustNewKey, "trust-new-key", false, "plan a re-pin of a rotated signing key")
	planCmd.Flags().BoolVar(&planOpts.force, "force", false, "plan overwriting AGENTS.md blocks that were edited by hand")
	root.AddCommand(planCmd)

	root.AddCommand(&cobra.Command{
		Use:   "validate [path]",
		Short: "Validate a SKILL.md, a skill directory, or an agent .md file",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := a.dir
			if len(args) == 1 {
				target = args[0]
			}

			return validatePath(out, target)
		},
	})

	root.AddCommand(&cobra.Command{
		Use:   "verify",
		Short: "Check installed targets against opendepot.lock.hcl without network access",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.verifyProject()
		},
	})

	return root
}
