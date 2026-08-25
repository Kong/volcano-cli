package branch

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	clidatabase "github.com/Kong/volcano-cli/internal/database"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type createOptions struct {
	deps     cliruntime.Deps
	database string
	name     string
	ttl      time.Duration
	ttlSet   bool
	out      io.Writer
}

func newCreate(deps cliruntime.Deps) *cobra.Command {
	var ttl time.Duration
	cmd := &cobra.Command{
		Use:   "create <database> <branch>",
		Short: "Fork a branch off a database",
		Long: fmt.Sprintf(`Fork a database into a new branch.

The branch starts as a copy of the parent's current state and is returned
before it is ready; fetch it until it reports active to get its connection
string.

A branch name may hold lowercase letters, numbers, and underscores.

Examples:
  %s
  %s`,
			cliruntime.CommandPath(deps, "databases branches create app feature_x"),
			cliruntime.CommandPath(deps, "databases branches create app nightly --ttl 24h")),
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			database, name, err := parseTarget(args)
			if err != nil {
				return err
			}
			return runCreate(cmd.Context(), createOptions{
				deps:     deps,
				database: database,
				name:     name,
				ttl:      ttl,
				ttlSet:   cmd.Flags().Changed("ttl"),
				out:      cmd.OutOrStdout(),
			})
		},
	}
	cmd.Flags().DurationVar(&ttl, "ttl", 0, "How long the branch should live, between 1h and 720h (default 168h)")
	return cmd
}

func runCreate(ctx context.Context, opts createOptions) error {
	var ttl *int64
	if opts.ttlSet {
		seconds := ttlSeconds(opts.ttl)
		ttl = &seconds
	}

	branch, err := clidatabase.NewService(opts.deps).CreateBranch(ctx, opts.database, opts.name, ttl)
	if err != nil {
		return err
	}

	output.DatabaseBranchCreated(opts.out, branch, opts.database, cliruntime.CommandPath(opts.deps, ""))
	return nil
}
