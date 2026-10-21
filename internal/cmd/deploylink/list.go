package deploylink

import (
	"context"
	"io"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/apiclient"
	"github.com/Kong/volcano-cli/internal/deploylink"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type listOptions struct {
	deps       cliruntime.Deps
	jsonOutput bool
	out        io.Writer
}

func newList(deps cliruntime.Deps) *cobra.Command {
	opts := listOptions{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the project's deploy links",
		Long:  "List the current project's deploy links, newest first.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts.deps = deps
			opts.out = cmd.OutOrStdout()
			return runList(cmd.Context(), opts)
		},
	}
	cmd.Flags().BoolVar(&opts.jsonOutput, "json", false, "Emit machine-readable JSON")
	return cmd
}

func runList(ctx context.Context, opts listOptions) error {
	links, err := deploylink.NewService(opts.deps).List(ctx)
	if err != nil {
		return err
	}
	if opts.jsonOutput {
		if links == nil {
			links = []apiclient.DeployLink{}
		}
		return writeJSON(opts.out, links)
	}
	output.DeployLinks(opts.out, links)
	return nil
}
