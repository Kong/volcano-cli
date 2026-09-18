package durable

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/confirm"
	clidurable "github.com/Kong/volcano-cli/internal/durable"
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
		Short: "Delete a durable function",
		Long: `Delete a durable function from the current project.

Teardown continues after the command returns, and it takes the function's
execution history with it. Stop an execution you need to end first, since
deleting the function does not wait for work already in flight.

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
	service := clidurable.NewService(opts.deps)
	// Read first so the prompt names the function rather than whatever the user
	// typed, and so a name that belongs to a standard function is refused before
	// anything is torn down.
	function, err := service.Get(ctx, opts.identifier)
	if err != nil {
		return err
	}

	if !opts.yes {
		confirmed, err := confirm.Delete(opts.in, opts.out, "durable function", function.Name)
		if err != nil {
			return err
		}
		if !confirmed {
			return nil
		}
	}

	if err := service.Delete(ctx, function.Id.String()); err != nil {
		return err
	}

	output.Success(opts.out, "Durable function '%s' deletion started", function.Name)
	fmt.Fprintln(opts.out, `Status will be "deleting" until cleanup finishes; afterwards the function and its executions will no longer appear in list/get responses.`)
	return nil
}
