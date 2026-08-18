package branch

import (
	"context"
	"errors"
	"io"
	"strings"

	"github.com/spf13/cobra"

	clidatabase "github.com/Kong/volcano-cli/internal/database"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type listOptions struct {
	deps     cliruntime.Deps
	database string
	out      io.Writer
}

func newList(deps cliruntime.Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "list <database>",
		Short: "List a database's branches",
		Long: `List every branch of a database, including branches still provisioning.

The list omits connection strings; fetch a single branch to get one.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runList(cmd.Context(), listOptions{
				deps:     deps,
				database: strings.TrimSpace(args[0]),
				out:      cmd.OutOrStdout(),
			})
		},
	}
}

func runList(ctx context.Context, opts listOptions) error {
	if opts.database == "" {
		return errors.New("database name cannot be empty")
	}

	branches, err := clidatabase.NewService(opts.deps).ListBranches(ctx, opts.database)
	if err != nil {
		return err
	}

	output.DatabaseBranches(opts.out, branches, opts.database)
	return nil
}
