// Package variables wires the volcano variables subcommands.
package variables

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/confirm"
	"github.com/Kong/volcano-cli/internal/envfile"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	clivariable "github.com/Kong/volcano-cli/internal/variable"
)

type deployOptions struct {
	deps cliruntime.Deps
	file string
	out  io.Writer
}

type listOptions struct {
	deps  cliruntime.Deps
	page  int
	limit int
	out   io.Writer
}

type getOptions struct {
	deps cliruntime.Deps
	name string
	out  io.Writer
}

type deleteOptions struct {
	deps cliruntime.Deps
	name string
	yes  bool
	in   io.Reader
	out  io.Writer
}

// New returns the variables command.
func New(deps cliruntime.Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "variables",
		Short: "Manage environment variables",
		Long:  "Deploy and list environment variables for the current cloud project.",
	}
	cmd.AddCommand(newDeploy(deps))
	cmd.AddCommand(newList(deps))
	cmd.AddCommand(newGet(deps))
	cmd.AddCommand(newDelete(deps))
	return cmd
}

func newDeploy(deps cliruntime.Deps) *cobra.Command {
	var file string
	cmd := &cobra.Command{
		Use:   "deploy",
		Short: "Deploy variables from volcano.env",
		Long: fmt.Sprintf(`Sync environment variables from volcano.env to your project.
Changed variables are saved immediately and then propagated asynchronously to
affected functions and frontends. Use "%s" to see the latest
propagation status.

By default, looks for volcano.env in these locations (in order):
  1. volcano/volcano.env (recommended)
  2. ./volcano.env (project root)

Use --file to specify a custom path.

Creates new variables and updates existing ones.`, cliruntime.CommandPath(deps, "variables list")),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDeploy(cmd.Context(), deployOptions{
				deps: deps,
				file: file,
				out:  cmd.OutOrStdout(),
			})
		},
	}
	cmd.Flags().StringVarP(&file, "file", "f", "", "Path to env file (default: volcano/volcano.env or ./volcano.env)")
	return cmd
}

func runDeploy(ctx context.Context, opts deployOptions) error {
	envFile, err := envfile.Load(opts.file)
	if err != nil {
		return err
	}

	fmt.Fprintf(opts.out, "\nReading %s...\n", envFile.Path)
	vars := envFile.Variables
	if len(vars) == 0 {
		fmt.Fprintf(opts.out, "No variables found in %s\n", envFile.Path)
		return nil
	}

	fmt.Fprintf(opts.out, "Found %d variable(s)\n", len(vars))
	fmt.Fprintln(opts.out, "\nSyncing with project...")

	service := clivariable.NewService(opts.deps)
	for name, value := range vars {
		if _, err := service.Create(ctx, name, value); err != nil {
			return err
		}
		fmt.Fprintf(opts.out, "  + %s (saved)\n", name)
	}

	fmt.Fprintln(opts.out)
	output.Success(opts.out, "%d variable(s) saved and propagation started", len(vars))
	return nil
}

func newList(deps cliruntime.Deps) *cobra.Command {
	var page int
	var limit int
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List variables",
		Long:  "List environment variables for the current project.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runList(cmd.Context(), listOptions{
				deps:  deps,
				page:  page,
				limit: limit,
				out:   cmd.OutOrStdout(),
			})
		},
	}
	cmd.Flags().IntVar(&page, "page", api.DefaultPage, "Page number to fetch")
	cmd.Flags().IntVar(&limit, "limit", api.DefaultLimit, "Number of variables per page")
	return cmd
}

func runList(ctx context.Context, opts listOptions) error {
	variables, err := clivariable.NewService(opts.deps).ListPage(ctx, opts.page, opts.limit)
	if err != nil {
		return err
	}

	output.Variables(opts.out, variables, cliruntime.CommandPath(opts.deps, ""))
	return nil
}

func newGet(deps cliruntime.Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "get <name>",
		Short: "Get a variable",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGet(cmd.Context(), getOptions{
				deps: deps,
				name: strings.TrimSpace(args[0]),
				out:  cmd.OutOrStdout(),
			})
		},
	}
}

func runGet(ctx context.Context, opts getOptions) error {
	variable, err := clivariable.NewService(opts.deps).Get(ctx, opts.name)
	if err != nil {
		return err
	}

	output.Variable(opts.out, variable)
	return nil
}

func newDelete(deps cliruntime.Deps) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a variable",
		Long: `Delete an environment variable from the current cloud project.

By default this command prompts for confirmation.
Use --yes to skip the prompt.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDelete(cmd.Context(), deleteOptions{
				deps: deps,
				name: strings.TrimSpace(args[0]),
				yes:  yes,
				in:   cmd.InOrStdin(),
				out:  cmd.OutOrStdout(),
			})
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt")
	return cmd
}

func runDelete(ctx context.Context, opts deleteOptions) error {
	if !opts.yes {
		confirmed, err := confirm.Delete(opts.in, opts.out, "variable", opts.name)
		if err != nil {
			return err
		}
		if !confirmed {
			return nil
		}
	}

	if err := clivariable.NewService(opts.deps).Delete(ctx, opts.name); err != nil {
		return err
	}

	output.Success(opts.out, "Variable '%s' deleted and propagation started", opts.name)
	return nil
}
