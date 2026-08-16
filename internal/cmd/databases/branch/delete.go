package branch

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/confirm"
	clidatabase "github.com/Kong/volcano-cli/internal/database"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type deleteOptions struct {
	deps     cliruntime.Deps
	database string
	name     string
	yes      bool
	in       io.Reader
	out      io.Writer
}

func newDelete(deps cliruntime.Deps) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete <database> <branch>",
		Short: "Delete a branch",
		Long: `Delete a branch and everything on it. The parent database is left untouched.

By default this command prompts for confirmation.
Use --yes to skip the prompt.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			database, name, err := parseTarget(args)
			if err != nil {
				return err
			}
			return runDelete(cmd.Context(), deleteOptions{
				deps:     deps,
				database: database,
				name:     name,
				yes:      yes,
				in:       cmd.InOrStdin(),
				out:      cmd.OutOrStdout(),
			})
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt")
	return cmd
}

func runDelete(ctx context.Context, opts deleteOptions) error {
	if !opts.yes {
		confirmed, err := confirm.Delete(opts.in, opts.out, "database branch",
			fmt.Sprintf("%s of database %s", opts.name, opts.database))
		if err != nil {
			return err
		}
		if !confirmed {
			return nil
		}
	}

	if err := clidatabase.NewService(opts.deps).DeleteBranch(ctx, opts.database, opts.name); err != nil {
		return err
	}

	output.Success(opts.out, "Branch '%s' of database '%s' deleted", opts.name, opts.database)
	return nil
}
