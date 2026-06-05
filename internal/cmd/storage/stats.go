// Package storage provides storage commands.
package storage

import (
	"context"
	"io"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	clistorage "github.com/Kong/volcano-cli/internal/storage"
)

type statsOptions struct {
	deps cliruntime.Deps
	out  io.Writer
}

func newStats(deps cliruntime.Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "stats",
		Short: "Show aggregate storage usage for the current project",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runStats(cmd.Context(), statsOptions{
				deps: deps,
				out:  cmd.OutOrStdout(),
			})
		},
	}
}

func runStats(ctx context.Context, opts statsOptions) error {
	stats, err := clistorage.NewService(opts.deps).GetStats(ctx)
	if err != nil {
		return err
	}
	output.StorageStats(opts.out, stats)
	return nil
}
