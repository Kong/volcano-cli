package deploylink

import (
	"context"
	"errors"
	"io"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/deploylink"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type statsOptions struct {
	deps       cliruntime.Deps
	linkID     string
	days       int
	jsonOutput bool
	out        io.Writer
}

func newStats(deps cliruntime.Deps) *cobra.Command {
	opts := statsOptions{}
	cmd := &cobra.Command{
		Use:   "stats <id>",
		Short: "Show how many people clicked and deployed a link",
		Long: `Show a deploy link's counts: the events and distinct visitors for each kind of
event over the window, then the most recent days.

--days sets how many days back to count. Without it the API counts 30 days.
Events are kept for 90 days, so a longer window counts no more. The counts are
for information only: visitors are counted from a salted hash and no address is
stored.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			linkID, err := parseLinkID(args[0])
			if err != nil {
				return err
			}
			// Zero means "the API's default" to the client, so a --days 0 would
			// silently count 30 days instead of the zero the user asked for.
			if cmd.Flags().Changed("days") && opts.days < 1 {
				return errors.New("--days must be at least 1")
			}
			opts.deps = deps
			opts.linkID = linkID
			opts.out = cmd.OutOrStdout()
			return runStats(cmd.Context(), opts)
		},
	}
	cmd.Flags().IntVar(&opts.days, "days", 0, "Days back to count (default: the API's 30)")
	cmd.Flags().BoolVar(&opts.jsonOutput, "json", false, "Emit machine-readable JSON")
	return cmd
}

func runStats(ctx context.Context, opts statsOptions) error {
	stats, err := deploylink.NewService(opts.deps).Stats(ctx, opts.linkID, opts.days)
	if err != nil {
		return err
	}
	if opts.jsonOutput {
		return writeJSON(opts.out, stats)
	}
	output.DeployLinkStats(opts.out, opts.linkID, stats)
	return nil
}
