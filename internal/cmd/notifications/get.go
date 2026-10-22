package notifications

import (
	"context"
	"io"

	"github.com/spf13/cobra"

	clinotification "github.com/Kong/volcano-cli/internal/notification"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type getOptions struct {
	deps       cliruntime.Deps
	jsonOutput bool
	out        io.Writer
}

func newGet(deps cliruntime.Deps) *cobra.Command {
	opts := getOptions{}
	cmd := &cobra.Command{
		Use:   "get",
		Short: "Show notification settings",
		Long:  "Show where the current project's alerts go and when a failing scheduler is announced. The webhook signing secret is never shown here.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts.deps = deps
			opts.out = cmd.OutOrStdout()
			return runGet(cmd.Context(), opts)
		},
	}
	cmd.Flags().BoolVar(&opts.jsonOutput, "json", false, "Emit machine-readable JSON")
	return cmd
}

func runGet(ctx context.Context, opts getOptions) error {
	settings, err := clinotification.NewService(opts.deps).Get(ctx)
	if err != nil {
		return err
	}
	if opts.jsonOutput {
		return writeJSON(opts.out, settings)
	}
	output.NotificationSettings(opts.out, settings)
	return nil
}
