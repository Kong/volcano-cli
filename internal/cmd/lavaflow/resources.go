package lavaflow

import (
	"context"
	"io"

	"github.com/spf13/cobra"

	clilavaflow "github.com/Kong/volcano-cli/internal/lavaflow"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type resourcesOptions struct {
	deps       cliruntime.Deps
	days       int
	jsonOutput bool
	out        io.Writer
}

func newResources(deps cliruntime.Deps) *cobra.Command {
	opts := resourcesOptions{}
	cmd := &cobra.Command{
		Use:   "resources",
		Short: "Show what LavaFlow did for each function, frontend and durable function",
		Long: `List every function, frontend and durable function in the current project:
when LavaFlow last analyzed it, and the issues it took on and fix pull
requests it opened in the window. A resource whose issues suddenly jumped is
flagged: at least 3 issues in the last 24 hours, and at least 3 times its daily
average over the 29 days before.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts.deps = deps
			opts.out = cmd.OutOrStdout()
			return runResources(cmd.Context(), opts)
		},
	}
	cmd.Flags().IntVar(&opts.days, "days", clilavaflow.DefaultDays, "Days in the window, today included (1 to 90)")
	cmd.Flags().BoolVar(&opts.jsonOutput, "json", false, "Emit machine-readable JSON")
	return cmd
}

func runResources(ctx context.Context, opts resourcesOptions) error {
	resources, err := clilavaflow.NewService(opts.deps).Resources(ctx, opts.days)
	if err != nil {
		return err
	}
	if opts.jsonOutput {
		return writeJSON(opts.out, resources)
	}
	output.LavaflowResources(opts.out, resources, opts.days)
	return nil
}
