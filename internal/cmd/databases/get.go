package databases

import (
	"context"
	"io"
	"strings"

	"github.com/spf13/cobra"

	clidatabase "github.com/Kong/volcano-cli/internal/database"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type getOptions struct {
	deps                 cliruntime.Deps
	name                 string
	showConnectionString bool
	out                  io.Writer
}

func newGet(deps cliruntime.Deps) *cobra.Command {
	var showConnectionString bool
	cmd := &cobra.Command{
		Use:   "get <name>",
		Short: "Get a database",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGet(cmd.Context(), getOptions{
				deps:                 deps,
				name:                 strings.TrimSpace(args[0]),
				showConnectionString: showConnectionString,
				out:                  cmd.OutOrStdout(),
			})
		},
	}
	cmd.Flags().BoolVar(&showConnectionString, "show-connection-string", false, "Show database connection string")
	return cmd
}

func runGet(ctx context.Context, opts getOptions) error {
	database, err := clidatabase.NewService(opts.deps).Get(ctx, opts.name)
	if err != nil {
		return err
	}

	output.Database(opts.out, database, opts.showConnectionString)
	return nil
}
