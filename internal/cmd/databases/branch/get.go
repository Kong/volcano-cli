package branch

import (
	"context"
	"io"

	"github.com/spf13/cobra"

	clidatabase "github.com/Kong/volcano-cli/internal/database"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type getOptions struct {
	deps                 cliruntime.Deps
	database             string
	name                 string
	showConnectionString bool
	out                  io.Writer
}

func newGet(deps cliruntime.Deps) *cobra.Command {
	var showConnectionString bool
	cmd := &cobra.Command{
		Use:   "get <database> <branch>",
		Short: "Show one branch",
		Long: `Show one branch of a database.

A branch carries a connection string once it reports active. The string holds
the branch's own credentials, so it is only printed with
--show-connection-string.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			database, name, err := parseTarget(args)
			if err != nil {
				return err
			}
			return runGet(cmd.Context(), getOptions{
				deps:                 deps,
				database:             database,
				name:                 name,
				showConnectionString: showConnectionString,
				out:                  cmd.OutOrStdout(),
			})
		},
	}
	cmd.Flags().BoolVar(&showConnectionString, "show-connection-string", false, "Show the branch connection string")
	return cmd
}

func runGet(ctx context.Context, opts getOptions) error {
	branch, err := clidatabase.NewService(opts.deps).GetBranch(ctx, opts.database, opts.name)
	if err != nil {
		return err
	}

	output.DatabaseBranch(opts.out, branch, opts.showConnectionString)
	return nil
}
