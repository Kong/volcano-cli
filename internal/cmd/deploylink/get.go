package deploylink

import (
	"context"
	"io"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/deploylink"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type getOptions struct {
	deps       cliruntime.Deps
	linkID     string
	jsonOutput bool
	out        io.Writer
}

func newGet(deps cliruntime.Deps) *cobra.Command {
	opts := getOptions{}
	cmd := &cobra.Command{
		Use:   "get <id>",
		Short: "Show a deploy link",
		Long: `Show a deploy link: its address, the repository and branch it follows, and its
status. A disabled link shows why it is off.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			linkID, err := parseLinkID(args[0])
			if err != nil {
				return err
			}
			opts.deps = deps
			opts.linkID = linkID
			opts.out = cmd.OutOrStdout()
			return runGet(cmd.Context(), opts)
		},
	}
	cmd.Flags().BoolVar(&opts.jsonOutput, "json", false, "Emit machine-readable JSON")
	return cmd
}

func runGet(ctx context.Context, opts getOptions) error {
	link, err := deploylink.NewService(opts.deps).Get(ctx, opts.linkID)
	if err != nil {
		return err
	}
	if opts.jsonOutput {
		return writeJSON(opts.out, link)
	}
	output.DeployLink(opts.out, link)
	return nil
}
