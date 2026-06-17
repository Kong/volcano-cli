package functions

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	clifunction "github.com/Kong/volcano-cli/internal/function"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type aliasOptions struct {
	deps       cliruntime.Deps
	alias      string
	functionID string
	out        io.Writer
}

func newAlias(deps cliruntime.Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "alias",
		Short: "Manage function invoke aliases",
		Long:  "Manage per-user aliases used by functions invoke.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newAliasSet(deps))
	cmd.AddCommand(newAliasList(deps))
	cmd.AddCommand(newAliasDelete(deps))
	return cmd
}

func newAliasSet(deps cliruntime.Deps) *cobra.Command {
	return &cobra.Command{
		Use:     "set <alias> <function-id>",
		Short:   "Set a function invoke alias",
		Example: "  " + cliruntime.CommandPath(deps, "functions alias set hello 33333333-3333-4333-8333-333333333333"),
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAliasSet(cmd.Context(), aliasOptions{
				deps:       deps,
				alias:      strings.TrimSpace(args[0]),
				functionID: strings.TrimSpace(args[1]),
				out:        cmd.OutOrStdout(),
			})
		},
	}
}

func newAliasList(deps cliruntime.Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List function invoke aliases",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runAliasList(cmd.Context(), aliasOptions{
				deps: deps,
				out:  cmd.OutOrStdout(),
			})
		},
	}
}

func newAliasDelete(deps cliruntime.Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "delete <alias>",
		Short: "Delete a function invoke alias",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAliasDelete(cmd.Context(), aliasOptions{
				deps:  deps,
				alias: strings.TrimSpace(args[0]),
				out:   cmd.OutOrStdout(),
			})
		},
	}
}

func runAliasSet(ctx context.Context, opts aliasOptions) error {
	alias, err := clifunction.NewService(opts.deps).SetAlias(ctx, opts.alias, opts.functionID)
	if err != nil {
		return err
	}
	output.Success(opts.out, "Set function alias %q → %s", alias.Name, alias.FunctionID)
	return nil
}

func runAliasList(ctx context.Context, opts aliasOptions) error {
	aliases, err := clifunction.NewService(opts.deps).ListAliases(ctx)
	if err != nil {
		return err
	}
	if len(aliases) == 0 {
		fmt.Fprintln(opts.out, "No function aliases configured")
		return nil
	}

	fmt.Fprintf(opts.out, "%-24s  %-36s\n", "Alias", "Function ID")
	fmt.Fprintln(opts.out, strings.Repeat("-", 62))
	for _, alias := range aliases {
		fmt.Fprintf(opts.out, "%-24s  %-36s\n", alias.Name, alias.FunctionID)
	}
	return nil
}

func runAliasDelete(ctx context.Context, opts aliasOptions) error {
	if err := clifunction.NewService(opts.deps).DeleteAlias(ctx, opts.alias); err != nil {
		return err
	}
	output.Success(opts.out, "Deleted function alias %q", opts.alias)
	return nil
}
