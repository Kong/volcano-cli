package executions

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/apiclient"
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
		"Only executions in this status ("+durableExecutionStatuses()+")")
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
//
// What counts as a status comes from the generated enum rather than a list
// written out here, because the API adds one before the CLI hears about it: a
// hand-written list refused `unknown` locally on a request the platform would
// have answered.
func normalizeStatusFilter(value string) (string, error) {
	status := strings.ToLower(strings.TrimSpace(value))
	if status == "" {
		return status, nil
	}
	if apiclient.DurableExecutionStatus(status).Valid() {
		return status, nil
	}
	return "", fmt.Errorf("unknown execution status %q: --status takes one of %s",
		strings.TrimSpace(value), durableExecutionStatuses())
}

// durableExecutionStatuses lists the filter's accepted values for a message or
// a flag's help, in the order the contract declares them.
func durableExecutionStatuses() string {
	statuses := []apiclient.DurableExecutionStatus{
		apiclient.DurableExecutionStatusPending,
		apiclient.DurableExecutionStatusRunning,
		apiclient.DurableExecutionStatusSucceeded,
		apiclient.DurableExecutionStatusFailed,
		apiclient.DurableExecutionStatusTimedOut,
		apiclient.DurableExecutionStatusStopped,
		apiclient.DurableExecutionStatusUnknown,
	}
	names := make([]string, len(statuses))
	for i, status := range statuses {
		names[i] = string(status)
	}
	return strings.Join(names, ", ")
}
