package branch

import (
	"context"
	"io"
	"time"

	"github.com/spf13/cobra"

	clidatabase "github.com/Kong/volcano-cli/internal/database"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type extendOptions struct {
	deps     cliruntime.Deps
	database string
	name     string
	ttl      time.Duration
	out      io.Writer
}

func newExtend(deps cliruntime.Deps) *cobra.Command {
	var ttl time.Duration
	cmd := &cobra.Command{
		Use:   "extend <database> <branch>",
		Short: "Give a branch a new lifetime",
		Long: `Re-arm a branch's lifetime, counted from now.

This replaces the branch's lifetime rather than adding to it, so extending a
branch by a shorter duration than it has left brings its expiry closer.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			database, name, err := parseTarget(args)
			if err != nil {
				return err
			}
			return runExtend(cmd.Context(), extendOptions{
				deps:     deps,
				database: database,
				name:     name,
				ttl:      ttl,
				out:      cmd.OutOrStdout(),
			})
		},
	}
	cmd.Flags().DurationVar(&ttl, "ttl", 0, "The new lifetime, between 1h and 720h")
	_ = cmd.MarkFlagRequired("ttl")
	return cmd
}

func runExtend(ctx context.Context, opts extendOptions) error {
	branch, err := clidatabase.NewService(opts.deps).ExtendBranch(ctx, opts.database, opts.name, ttlSeconds(opts.ttl))
	if err != nil {
		return err
	}

	output.Success(opts.out, "Branch '%s' now expires %s", opts.name, output.FormatTimestamp(branch.ExpiresAt))
	return nil
}
