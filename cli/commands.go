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
		Version:       buildVersion,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.SetOut(out)
	root.SetErr(errOut)

	defaultHelp := root.HelpFunc()
	root.SetHelpFunc(func(c *cobra.Command, args []string) {
		if c != root {
			defaultHelp(c, args)
			return
		}

		printBanner(c.OutOrStdout())

		// The banner already shows the tagline, so hide root's Short from the default help.
		short := root.Short
		root.Short = ""
		defaultHelp(c, args)
		root.Short = short
	})

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
	applyCmd.Flags().BoolVar(&opts.allowOlderVersions, "allow-older-versions", false, "install an older matching version when a newer one is blocked, rejected, or failed assessment")
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
	planCmd.Flags().BoolVar(&planOpts.allowOlderVersions, "allow-older-versions", false, "install an older matching version when a newer one is blocked, rejected, or failed assessment")
	planCmd.Flags().BoolVar(&planOpts.trustNewKey, "trust-new-key", false, "plan a re-pin of a rotated signing key")
	planCmd.Flags().BoolVar(&planOpts.force, "force", false, "plan overwriting AGENTS.md blocks that were edited by hand")
	root.AddCommand(planCmd)

	var listOpts listOptions
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List the Skills and Agents published by a registry",
		Args:  cobra.NoArgs,
	}

	listCmd.PersistentFlags().StringVar(&listOpts.registry, "registry", "", "registry host to list; defaults to the project registry")
	listCmd.PersistentFlags().StringVar(&listOpts.namespace, "namespace", "", "only list items in this namespace")
	listCmd.PersistentFlags().BoolVar(&listOpts.json, "json", false, "print the catalog as JSON with full descriptions")
	listCmd.AddCommand(&cobra.Command{
		Use:   "skills",
		Short: "List the Skills available from a registry",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.listCatalog(cmd.Context(), kindSkill, listOpts)
		},
	}, &cobra.Command{
		Use:   "agents",
		Short: "List the Agents available from a registry",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.listCatalog(cmd.Context(), kindAgent, listOpts)
		},
	})
	root.AddCommand(listCmd)

	var describeRegistry string
	describeCmd := &cobra.Command{
		Use:   "describe",
		Short: "Show details for a skill or agent",
		Args:  cobra.NoArgs,
	}
	describeCmd.PersistentFlags().StringVar(&describeRegistry, "registry", "", "registry host to query; defaults to the project registry")
	describeCmd.AddCommand(&cobra.Command{
		Use:   "skill <namespace/name>",
		Short: "Show details for one skill",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.describeResource(cmd.Context(), kindSkill, args[0], describeRegistry)
		},
	}, &cobra.Command{
		Use:   "agent <namespace/name>",
		Short: "Show details for one agent",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.describeResource(cmd.Context(), kindAgent, args[0], describeRegistry)
		},
	})
	root.AddCommand(describeCmd)

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
