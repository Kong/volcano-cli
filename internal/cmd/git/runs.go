package git

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/gitconnect"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

// defaultRunsLimit is how many runs a page shows unless --limit says.
const defaultRunsLimit = 20

func newRuns(deps cliruntime.Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "runs",
		Short: "Show what each push to the connected repository deployed",
		Long: `Each push to the connected repository's production branch records a Git
deployment run: whether the push deployed and, when it failed or was skipped,
why. Read them to find out why a push did not deploy.

These work with a project access token, including a read-only one.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newRunsList(deps))
	cmd.AddCommand(newRunsGet(deps))
	return cmd
}

type runsListOptions struct {
	deps       cliruntime.Deps
	query      gitconnect.RunsQuery
	jsonOutput bool
	out        io.Writer
}

func newRunsList(deps cliruntime.Deps) *cobra.Command {
	opts := runsListOptions{deps: deps}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the Git deployment runs, newest push first",
		Long: fmt.Sprintf(`List the current project's Git deployment runs, newest push first: each run's
status, the branch and commit pushed, and the code of the reason it failed or
was skipped. Show one with '%s'.`,
			cliruntime.CommandPath(deps, "git runs get <run-id>")),
		Example: "  volcano git runs list\n  volcano git runs list --status failed --json",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts.query.Status = strings.TrimSpace(opts.query.Status)
			opts.query.Branch = strings.TrimSpace(opts.query.Branch)
			opts.out = cmd.OutOrStdout()
			return runRunsList(cmd.Context(), opts)
		},
	}
	cmd.Flags().IntVar(&opts.query.Limit, "limit", defaultRunsLimit, "Runs per page (1 to 100)")
	cmd.Flags().StringVar(&opts.query.Cursor, "cursor", "", "Start after this cursor, from the previous page")
	cmd.Flags().StringVar(&opts.query.Status, "status", "",
		"Only runs with this status: pending, running, succeeded, failed, superseded or skipped")
	cmd.Flags().StringVar(&opts.query.Branch, "branch", "", "Only runs for pushes to this branch")
	cmd.Flags().BoolVar(&opts.jsonOutput, "json", false, "Emit machine-readable JSON")
	return cmd
}

func runRunsList(ctx context.Context, opts runsListOptions) error {
	page, err := gitconnect.NewService(opts.deps).ListRuns(ctx, opts.query)
	if err != nil {
		return guide(opts.deps, err)
	}
	if opts.jsonOutput {
		return output.JSON(opts.out, page)
	}
	output.GitRuns(opts.out, output.GitRunsPage{
		Runs:        page.Data,
		NextCommand: runsNextCommand(opts, page.HasMore, page.NextCursor),
		GetCommand:  cliruntime.CommandPath(opts.deps, "git runs get"),
	})
	return nil
}

// runsNextCommand is the command that shows the next page with the same
// filters, which the cursor is bound to.
func runsNextCommand(opts runsListOptions, hasMore bool, cursor *string) string {
	if !hasMore || cursor == nil || *cursor == "" {
		return ""
	}
	command := fmt.Sprintf("git runs list --limit %d --cursor %s", opts.query.Limit, cliruntime.ShellQuote(*cursor))
	if opts.query.Status != "" {
		command += " --status " + cliruntime.ShellQuote(opts.query.Status)
	}
	if opts.query.Branch != "" {
		command += " --branch " + cliruntime.ShellQuote(opts.query.Branch)
	}
	return cliruntime.CommandPath(opts.deps, command)
}

func newRunsGet(deps cliruntime.Deps) *cobra.Command {
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "get <run-id>",
		Short: "Show one Git deployment run",
		Long: `Show one Git deployment run: the commit pushed, where the run got to, the
migration runs it started, why it failed or was skipped in Volcano's own
words, and what applying the push's volcano-config.yaml sections did.`,
		Example: "  volcano git runs get 6f1c2b9a-4e8d-4f7b-8a5c-2e9d1f4b8a6c",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			run, err := gitconnect.NewService(deps).GetRun(cmd.Context(), args[0])
			if err != nil {
				return guide(deps, err)
			}
			if jsonOutput {
				return output.JSON(cmd.OutOrStdout(), run)
			}
			output.GitRun(cmd.OutOrStdout(), run)
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Emit machine-readable JSON")
	return cmd
}
