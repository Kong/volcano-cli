package deploylink

import (
	"context"
	"io"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/confirm"
	"github.com/Kong/volcano-cli/internal/deploylink"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type deleteOptions struct {
	deps   cliruntime.Deps
	linkID string
	yes    bool
	in     io.Reader
	out    io.Writer
}

func newDelete(deps cliruntime.Deps) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a deploy link",
		Long: `Delete a deploy link and its recorded click and deploy counts. The address stops
working. Projects people already deployed from it are not affected.

To switch a link off and keep it, use 'disable'.

By default this command prompts for confirmation.
Use --yes to skip the prompt.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			linkID, err := parseLinkID(args[0])
			if err != nil {
				return err
			}
			return runDelete(cmd.Context(), deleteOptions{
				deps:   deps,
				linkID: linkID,
				yes:    yes,
				in:     cmd.InOrStdin(),
				out:    cmd.OutOrStdout(),
			})
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt")
	return cmd
}

func runDelete(ctx context.Context, opts deleteOptions) error {
	if !opts.yes {
		confirmed, err := confirm.Delete(opts.in, opts.out, "deploy link", opts.linkID)
		if err != nil {
			return err
		}
		if !confirmed {
			return nil
		}
	}
	if err := deploylink.NewService(opts.deps).Delete(ctx, opts.linkID); err != nil {
		return err
	}
	output.Success(opts.out, "Deleted deploy link %s", opts.linkID)
	return nil
}
