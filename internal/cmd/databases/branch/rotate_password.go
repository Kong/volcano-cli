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

type rotatePasswordOptions struct {
	deps                 cliruntime.Deps
	database             string
	name                 string
	showConnectionString bool
	yes                  bool
	in                   io.Reader
	out                  io.Writer
}

func newRotatePassword(deps cliruntime.Deps) *cobra.Command {
	var showConnectionString bool
	var yes bool
	cmd := &cobra.Command{
		Use:   "rotate-password <database> <branch>",
		Short: "Rotate a branch's password",
		Long: `Give a branch's role a new password.

The branch's previous connection string stops working immediately, so anything
still using it must be updated.

By default this command prompts for confirmation.
Use --yes to skip the prompt.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			database, name, err := parseTarget(args)
			if err != nil {
				return err
			}
			return runRotatePassword(cmd.Context(), rotatePasswordOptions{
				deps:                 deps,
				database:             database,
				name:                 name,
				showConnectionString: showConnectionString,
				yes:                  yes,
				in:                   cmd.InOrStdin(),
				out:                  cmd.OutOrStdout(),
			})
		},
	}
	cmd.Flags().BoolVar(&showConnectionString, "show-connection-string", false, "Show the new branch connection string")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt")
	return cmd
}

func runRotatePassword(ctx context.Context, opts rotatePasswordOptions) error {
	if !opts.yes {
		confirmed, err := confirm.Action(opts.in, opts.out,
			"Rotating the password invalidates the branch's current connection string immediately.",
			fmt.Sprintf("Rotate the password for branch '%s' of database '%s'?", opts.name, opts.database))
		if err != nil {
			return err
		}
		if !confirmed {
			return nil
		}
	}

	branch, err := clidatabase.NewService(opts.deps).RotateBranchPassword(ctx, opts.database, opts.name)
	if err != nil {
		return err
	}

	output.Success(opts.out, "Password rotated for branch '%s' of database '%s'", opts.name, opts.database)
	if opts.showConnectionString {
		output.DatabaseBranchConnectionString(opts.out, branch)
	}
	return nil
}
