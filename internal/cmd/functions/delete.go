// Package functions wires the volcano functions subcommands.
package functions

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/confirm"
	clifunction "github.com/Kong/volcano-cli/internal/function"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type deleteOptions struct {
	deps       cliruntime.Deps
	identifier string
	yes        bool
	in         io.Reader
	out        io.Writer
}

func newDelete(deps cliruntime.Deps) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete <name-or-id>",
		Short: "Delete a function",
		Long: `Delete a function from the current project.

By default this command prompts for confirmation.
Use --yes to skip the prompt.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDelete(cmd.Context(), deleteOptions{
				deps:       deps,
				identifier: strings.TrimSpace(args[0]),
				yes:        yes,
				in:         cmd.InOrStdin(),
				out:        cmd.OutOrStdout(),
			})
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt")
	return cmd
}

func runDelete(ctx context.Context, opts deleteOptions) error {
	service := clifunction.NewService(opts.deps)
	function, err := service.Resolve(ctx, opts.identifier)
	if err != nil {
		return err
	}

	if !opts.yes {
		confirmed, err := confirm.Delete(opts.in, opts.out, "function", function.Name)
		if err != nil {
			return err
		}
		if !confirmed {
			return nil
		}
	}

	if err := service.DeleteByID(ctx, function.Id); err != nil {
		return err
	}

	output.Success(opts.out, "Function '%s' deletion started", function.Name)
	fmt.Fprintln(opts.out, `Status will be "deleting" until cleanup finishes; afterwards the function will no longer appear in list/get responses.`)
	return nil
}
