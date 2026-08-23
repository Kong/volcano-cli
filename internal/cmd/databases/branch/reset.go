package branch

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/confirm"
	clidatabase "github.com/Kong/volcano-cli/internal/database"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type resetOptions struct {
	deps     cliruntime.Deps
	database string
	name     string
	yes      bool
	in       io.Reader
	out      io.Writer
}

func newReset(deps cliruntime.Deps) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "reset <database> <branch>",
		Short: "Rewind a branch to its parent",
		Long: `Discard everything a branch has diverged by and re-fork it from the parent's
current state.

The rewind runs in the background, so the branch comes back provisioning; fetch
it until it reports active before connecting again. The branch keeps its name and
connection string, and its lifetime is re-armed.

By default this command prompts for confirmation.
Use --yes to skip the prompt.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			database, name, err := parseTarget(args)
			if err != nil {
				return err
			}
			return runReset(cmd.Context(), resetOptions{
				deps:     deps,
				database: database,
				name:     name,
				yes:      yes,
				in:       cmd.InOrStdin(),
				out:      cmd.OutOrStdout(),
			})
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt")
	return cmd
}

func runReset(ctx context.Context, opts resetOptions) error {
	if !opts.yes {
		confirmed, err := confirm.Action(opts.in, opts.out,
			"Resetting discards every change made on the branch since it was forked. This cannot be undone.",
			fmt.Sprintf("Reset branch '%s' of database '%s'?", opts.name, opts.database))
		if err != nil {
			return err
		}
		if !confirmed {
			return nil
		}
	}

	branch, err := clidatabase.NewService(opts.deps).ResetBranch(ctx, opts.database, opts.name)
	if err != nil {
		return err
	}

	output.Success(opts.out, "Branch '%s' reset to database '%s'; it now expires %s",
		opts.name, opts.database, output.FormatTimestamp(branch.ExpiresAt))
	output.Note(opts.out, "The rewind runs in the background. The branch keeps its connection string "+
		"but does not serve connections until it reports active again.")
	return nil
}
