package functions

import (
	"context"
	"io"

	"github.com/spf13/cobra"

	clifunction "github.com/Kong/volcano-cli/internal/function"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type regionsOptions struct {
	deps       cliruntime.Deps
	jsonOutput bool
	out        io.Writer
}

func newRegions(deps cliruntime.Deps) *cobra.Command {
	opts := regionsOptions{deps: deps}
	cmd := &cobra.Command{
		Use:   "regions",
		Short: "List the regions functions can be deployed to",
		Long: `List the regions functions can be deployed to. These are the values the
project's selected_regions in volcano-config.yaml and a scheduler's --regions
accept.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts.out = cmd.OutOrStdout()
			return runRegions(cmd.Context(), opts)
		},
	}
	cmd.Flags().BoolVar(&opts.jsonOutput, "json", false, "Emit machine-readable JSON")
	return cmd
}

func runRegions(ctx context.Context, opts regionsOptions) error {
	regions, err := clifunction.NewService(opts.deps).ListRegions(ctx)
	if err != nil {
		return err
	}
	if opts.jsonOutput {
		return output.JSON(opts.out, regions)
	}
	output.FunctionRegions(opts.out, regions)
	return nil
}
