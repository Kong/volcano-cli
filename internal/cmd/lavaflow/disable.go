package lavaflow

import (
	"context"
	"io"

	"github.com/spf13/cobra"

	clilavaflow "github.com/Kong/volcano-cli/internal/lavaflow"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type disableOptions struct {
	deps       cliruntime.Deps
	jsonOutput bool
	out        io.Writer
	errOut     io.Writer
}

func newDisable(deps cliruntime.Deps) *cobra.Command {
	opts := disableOptions{}
	cmd := &cobra.Command{
		Use:   "disable",
		Short: "Turn LavaFlow off",
		Long: `Turn LavaFlow off for the current project. No new run starts, and a run that has not
finished is canceled. Past runs and their pull requests stay as they are.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts.deps = deps
			opts.out = cmd.OutOrStdout()
			opts.errOut = cmd.ErrOrStderr()
			return runDisable(cmd.Context(), opts)
		},
	}
	cmd.Flags().BoolVar(&opts.jsonOutput, "json", false, "Emit machine-readable JSON")
	return cmd
}

func runDisable(ctx context.Context, opts disableOptions) error {
	result, err := clilavaflow.NewService(opts.deps).SetEnabled(ctx, false)
	if err != nil {
		return err
	}
	if result.Reset {
		output.Warning(opts.errOut, "The saved settings could not be read, so max_stage and auto_trigger went back "+
			"to their defaults. Deploy the project configuration again to restore them.")
	}
	if opts.jsonOutput {
		return writeJSON(opts.out, result.Settings)
	}
	output.Success(opts.out, "LavaFlow is off")
	return nil
}
