package callbacks

import (
	"context"
	"io"

	"github.com/spf13/cobra"

	clidurable "github.com/Kong/volcano-cli/internal/durable"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type heartbeatOptions struct {
	deps       cliruntime.Deps
	token      string
	jsonOutput bool
	out        io.Writer
}

func newHeartbeat(deps cliruntime.Deps) *cobra.Command {
	opts := heartbeatOptions{}
	cmd := &cobra.Command{
		Use:   "heartbeat <token>",
		Short: "Report that a callback's work is still going",
		Long: `Heartbeat one callback, restarting the heartbeat timeout of the step waiting
on the token. A heartbeat never extends the step's own timeout, and a wait
without a heartbeat timeout accepts heartbeats and ignores them.

Heartbeat more often than the wait's heartbeat timeout. When the heartbeats
stop, the wait resolves with null once the heartbeat timeout passes.

A heartbeat after the wait ended is refused like a late answer. One that cannot
reach the execution right now leaves the wait unchanged; retry it with backoff.

--json prints the outcome as the API returns it.`,
		Example: "  " + cliruntime.CommandPath(deps,
			"durable callbacks heartbeat vcb_0000000000000000000000000000000000000000000"),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			token, err := readToken(args[0], cmd.InOrStdin())
			if err != nil {
				return err
			}
			opts.deps = deps
			opts.token = token
			opts.out = cmd.OutOrStdout()
			return runHeartbeat(cmd.Context(), opts)
		},
	}
	cmd.Flags().BoolVar(&opts.jsonOutput, "json", false, "Emit machine-readable JSON")
	return cmd
}

func runHeartbeat(ctx context.Context, opts heartbeatOptions) error {
	outcome, err := clidurable.NewService(opts.deps).HeartbeatCallback(ctx, opts.token)
	if err != nil {
		return err
	}
	if opts.jsonOutput {
		return writeJSON(opts.out, outcome)
	}
	output.Success(opts.out, "Heartbeat sent; the callback is still waiting")
	return nil
}
