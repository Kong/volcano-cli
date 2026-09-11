package executions

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/api"
	clidurable "github.com/Kong/volcano-cli/internal/durable"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type listOptions struct {
	deps     cliruntime.Deps
	function string
	status   string
	page     int
	limit    int
	out      io.Writer
}

func newList(deps cliruntime.Deps) *cobra.Command {
	opts := listOptions{}
	cmd := &cobra.Command{
		Use:   "list <function>",
		Short: "List executions of a durable function",
		Long: `List the executions of one durable function, newest first.

The status each entry carries is the last one Volcano observed. Fetch a single
execution to have its state refreshed.`,
		Example: fmt.Sprintf(`  %s
  %s`,
			cliruntime.CommandPath(deps, "durable executions list order-pipeline"),
			cliruntime.CommandPath(deps, "durable executions list order-pipeline --status running")),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.deps = deps
			opts.function = strings.TrimSpace(args[0])
			opts.out = cmd.OutOrStdout()
			return runList(cmd.Context(), opts)
		},
	}
	cmd.Flags().StringVar(&opts.status, "status", "",
		"Only executions in this status (pending, running, succeeded, failed, timed_out, stopped)")
	cmd.Flags().IntVar(&opts.page, "page", api.DefaultPage, "Page number to fetch")
	cmd.Flags().IntVar(&opts.limit, "limit", api.DefaultLimit, "Number of executions per page")
	return cmd
}

func runList(ctx context.Context, opts listOptions) error {
	status, err := normalizeStatusFilter(opts.status)
	if err != nil {
		return err
	}
	executions, err := clidurable.NewService(opts.deps).ListExecutions(
		ctx, opts.function, status, opts.page, opts.limit)
	if err != nil {
		return err
	}

	output.DurableExecutions(opts.out, opts.function, status, executions, cliruntime.CommandPath(opts.deps, ""))
	return nil
}

// normalizeStatusFilter checks the filter here rather than letting the API
// refuse it, so the answer names what the flag takes. The dashboard labels these
// statuses for reading — pending shows as "Starting" — and a label is not a
// filter value.
func normalizeStatusFilter(value string) (string, error) {
	status := strings.ToLower(strings.TrimSpace(value))
	switch status {
	case "", "pending", "running", "succeeded", "failed", "timed_out", "stopped":
		return status, nil
	default:
		return "", fmt.Errorf(
			"unknown execution status %q: --status takes one of pending, running, succeeded, failed, timed_out, stopped",
			strings.TrimSpace(value))
	}
}
