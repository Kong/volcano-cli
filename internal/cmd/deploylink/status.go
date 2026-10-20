package deploylink

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/apiclient"
	"github.com/Kong/volcano-cli/internal/deploylink"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

// statusChange is one of the two commands that set a link's status.
type statusChange struct {
	use    string
	short  string
	long   string
	status apiclient.UpdateDeployLinkRequestStatus
	done   string
}

var (
	statusDisable = statusChange{
		use:   "disable <id>",
		short: "Switch a deploy link off",
		long: `Switch a deploy link off. Its address stops deploying until you enable it. This
needs no checks, so you can always do it.`,
		status: apiclient.UpdateDeployLinkRequestStatusDisabled,
		done:   "Disabled",
	}
	statusEnable = statusChange{
		use:   "enable <id>",
		short: "Switch a deploy link on",
		long: `Switch a deploy link on. Volcano verifies it again first, like a new link, so a
link whose repository is private or whose branch is gone stays off until that
is fixed.`,
		status: apiclient.UpdateDeployLinkRequestStatusActive,
		done:   "Enabled",
	}
)

type statusOptions struct {
	deps       cliruntime.Deps
	change     statusChange
	linkID     string
	jsonOutput bool
	out        io.Writer
}

func newSetStatus(deps cliruntime.Deps, change statusChange) *cobra.Command {
	opts := statusOptions{}
	cmd := &cobra.Command{
		Use:   change.use,
		Short: change.short,
		Long:  change.long,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			linkID, err := parseLinkID(args[0])
			if err != nil {
				return err
			}
			opts.deps = deps
			opts.change = change
			opts.linkID = linkID
			opts.out = cmd.OutOrStdout()
			return runSetStatus(cmd.Context(), opts)
		},
	}
	cmd.Flags().BoolVar(&opts.jsonOutput, "json", false, "Emit machine-readable JSON")
	return cmd
}

func runSetStatus(ctx context.Context, opts statusOptions) error {
	link, err := deploylink.NewService(opts.deps).SetStatus(ctx, opts.linkID, opts.change.status)
	if err != nil {
		return err
	}
	if opts.jsonOutput {
		return writeJSON(opts.out, link)
	}
	output.Success(opts.out, "%s deploy link %s", opts.change.done, opts.linkID)
	fmt.Fprintln(opts.out)
	output.DeployLink(opts.out, link)
	return nil
}
