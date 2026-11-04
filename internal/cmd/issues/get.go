package issues

import (
	"context"
	"io"

	"github.com/spf13/cobra"

	cliissues "github.com/Kong/volcano-cli/internal/issues"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	"github.com/Kong/volcano-cli/internal/theme"
)

type getOptions struct {
	deps       cliruntime.Deps
	jsonOutput bool
	out        io.Writer
}

func newGet(deps cliruntime.Deps) *cobra.Command {
	opts := getOptions{}
	cmd := &cobra.Command{
		Use:   "get <issue-id>",
		Short: "Show one issue",
		Long: `Show one issue: its title, status and the function or frontend that raised it,
the error type and the place in your code it was thrown from, how many errors
it counted, and when it was first and last seen, resolved or reopened.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.deps = deps
			opts.out = cmd.OutOrStdout()
			return runGet(cmd.Context(), opts, args[0])
		},
	}
	cmd.Flags().BoolVar(&opts.jsonOutput, "json", false, "Emit machine-readable JSON")
	return cmd
}

func runGet(ctx context.Context, opts getOptions, issueID string) error {
	issue, err := cliissues.NewService(opts.deps).Get(ctx, issueID)
	if err != nil {
		return err
	}
	if opts.jsonOutput {
		return output.ControlSafeJSON(opts.out, issue)
	}
	output.Issue(opts.out, issue)
	// Local development has no notes; they are input to LavaFlow.
	if !opts.deps.LocalMode {
		output.Note(opts.out, "Read its notes with %s.",
			theme.Command(cliruntime.CommandPath(opts.deps, "issues notes "+issue.Id.String()), theme.On(opts.out)))
	}
	return nil
}
