package databases

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/confirm"
	clidatabase "github.com/Kong/volcano-cli/internal/database"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type resizeOptions struct {
	deps       cliruntime.Deps
	name       string
	size       string
	yes        bool
	jsonOutput bool
	in         io.Reader
	out        io.Writer
	errOut     io.Writer
}

func newResize(deps cliruntime.Deps) *cobra.Command {
	opts := resizeOptions{deps: deps}
	cmd := &cobra.Command{
		Use:   "resize <name> --type <size>",
		Short: "Change a database's size",
		Long: `Change a database's size. Active connections may drop briefly while it
changes. The database must be active.

Sizes, smallest first: ` + strings.Join(clidatabase.Sizes, ", ") + `.

By default this command prompts for confirmation. Use --yes to skip the prompt.`,
		Example: "  " + cliruntime.CommandPath(deps, "databases resize app --type volcano-db-m"),
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.name = strings.TrimSpace(args[0])
			opts.in, opts.out, opts.errOut = cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr()
			return runResize(cmd.Context(), opts)
		},
	}
	cmd.Flags().StringVar(&opts.size, "type", "", "New database size, such as volcano-db-m")
	cmd.Flags().BoolVarP(&opts.yes, "yes", "y", false, "Skip confirmation prompt")
	cmd.Flags().BoolVar(&opts.jsonOutput, "json", false, "Emit machine-readable JSON")
	_ = cmd.MarkFlagRequired("type")
	return cmd
}

func runResize(ctx context.Context, opts resizeOptions) error {
	opts.size = strings.ToLower(strings.TrimSpace(opts.size))
	if err := clidatabase.ValidateSize(opts.size); err != nil {
		return err
	}
	service := clidatabase.NewService(opts.deps)
	current, err := service.Get(ctx, opts.name)
	if err != nil {
		return err
	}
	if current.DatabaseType != nil && string(*current.DatabaseType) == opts.size {
		if opts.jsonOutput {
			return output.JSON(opts.out, current)
		}
		fmt.Fprintf(opts.out, "Database '%s' is already %s\n", opts.name, opts.size)
		return nil
	}

	if !opts.yes {
		// Keep JSON output parseable: the prompt goes to stderr.
		promptOut := opts.out
		if opts.jsonOutput {
			promptOut = opts.errOut
		}
		from := "its current size"
		if current.DatabaseType != nil {
			from = string(*current.DatabaseType)
		}
		confirmed, err := confirm.Action(opts.in, promptOut,
			fmt.Sprintf("Resizing database '%s' from %s to %s. Active connections may drop briefly.", opts.name, from, opts.size),
			"Resize it?")
		if err != nil {
			return err
		}
		if !confirmed {
			return nil
		}
	}

	resized, err := service.Resize(ctx, opts.name, opts.size)
	if err != nil {
		return err
	}
	if opts.jsonOutput {
		return output.JSON(opts.out, resized)
	}
	output.Success(opts.out, "Database '%s' resized to %s", opts.name, opts.size)
	return nil
}
