package project

import (
	"context"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/output"
	cliproject "github.com/Kong/volcano-cli/internal/project"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type usageOptions struct {
	deps      cliruntime.Deps
	projectID string
	out       io.Writer
}

func newUsage(deps cliruntime.Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "usage [project-id]",
		Short: "Show project usage",
		Long:  "Show current-month and all-time usage totals for a project. Defaults to the selected project.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var projectID string
			if len(args) == 1 {
				projectID = strings.TrimSpace(args[0])
			}
			return runUsage(cmd.Context(), usageOptions{
				deps:      deps,
				projectID: projectID,
				out:       cmd.OutOrStdout(),
			})
		},
	}
}

func runUsage(ctx context.Context, opts usageOptions) error {
	usage, err := cliproject.NewService(opts.deps).Usage(ctx, opts.projectID)
	if err != nil {
		return err
	}
	output.ProjectUsage(opts.out, usage)
	return nil
}
