package lavaflow

import (
	"context"
	"io"

	"github.com/spf13/cobra"

	clilavaflow "github.com/Kong/volcano-cli/internal/lavaflow"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

// gitConnectCommand connects a repository. git is a top-level command in
// both trees, so it never takes the cloud prefix.
const gitConnectCommand = "volcano git connect"

type statusOptions struct {
	deps       cliruntime.Deps
	jsonOutput bool
	out        io.Writer
}

func newStatus(deps cliruntime.Deps) *cobra.Command {
	opts := statusOptions{}
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show whether LavaFlow is on",
		Long: `Show whether LavaFlow is on for the current project, how far it goes, whether
a custom context and system prompt are set, and which repository its fix pull
requests go to.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts.deps = deps
			opts.out = cmd.OutOrStdout()
			return runStatus(cmd.Context(), opts)
		},
	}
	cmd.Flags().BoolVar(&opts.jsonOutput, "json", false, "Emit machine-readable JSON")
	return cmd
}

func runStatus(ctx context.Context, opts statusOptions) error {
	status, err := clilavaflow.NewService(opts.deps).Status(ctx)
	if err != nil {
		return err
	}
	if opts.jsonOutput {
		return writeJSON(opts.out, status)
	}
	output.LavaflowStatus(opts.out, output.LavaflowStatusDetails{
		Settings:          status.Settings,
		Available:         status.Available,
		RepositoryState:   status.RepositoryState,
		Repository:        status.Repository,
		Description:       clilavaflow.OnDescription,
		EnableCommand:     cliruntime.CommandPath(opts.deps, "lavaflow enable"),
		GitConnectCommand: gitConnectCommand,
	})
	return nil
}
