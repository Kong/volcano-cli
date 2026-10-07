package approvals

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/apiclient"
	clidurable "github.com/Kong/volcano-cli/internal/durable"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

// statusAll is the --status value that lists approvals in every status. The
// API spells that as no filter at all.
const statusAll = "all"

type listOptions struct {
	deps       cliruntime.Deps
	function   string
	status     string
	execution  string
	since      string
	page       int
	limit      int
	jsonOutput bool
	out        io.Writer
}

func newList(deps cliruntime.Deps) *cobra.Command {
	opts := listOptions{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List durable approvals",
		Long: `List the project's approvals, newest first. By default only the pending ones,
which are the approvals a workflow is waiting on; --status all includes the
decided, expired, and cancelled ones too, which are kept for a year.`,
		Example: fmt.Sprintf(`  %s
  %s
  %s`,
			cliruntime.CommandPath(deps, "durable approvals list"),
			cliruntime.CommandPath(deps, "durable approvals list --function order-pipeline --status all"),
			cliruntime.CommandPath(deps, "durable approvals list --status denied --since 7d")),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts.deps = deps
			opts.out = cmd.OutOrStdout()
			return runList(cmd.Context(), opts)
		},
	}
	cmd.Flags().StringVar(&opts.function, "function", "", "Only approvals requested by this durable function (name or id)")
	cmd.Flags().StringVar(&opts.status, "status", string(apiclient.DurableApprovalStatusPending),
		"Only approvals in this status ("+approvalStatuses()+")")
	cmd.Flags().StringVar(&opts.execution, "execution", "", "Only approvals requested by this execution (id)")
	cmd.Flags().StringVar(&opts.since, "since", "", "Only approvals requested within this window, such as 30d or 24h")
	cmd.Flags().IntVar(&opts.page, "page", api.DefaultPage, "Page number to fetch")
	cmd.Flags().IntVar(&opts.limit, "limit", api.DefaultLimit, "Number of approvals per page")
	cmd.Flags().BoolVar(&opts.jsonOutput, "json", false, "Emit machine-readable JSON")
	return cmd
}

func runList(ctx context.Context, opts listOptions) error {
	status, err := normalizeStatusFilter(opts.status)
	if err != nil {
		return err
	}
	input := api.DurableApprovalListInput{
		Function: strings.TrimSpace(opts.function),
		Status:   status,
		Page:     opts.page,
		Limit:    opts.limit,
	}
	if opts.execution != "" {
		executionID, err := uuid.Parse(strings.TrimSpace(opts.execution))
		if err != nil {
			return fmt.Errorf("invalid execution ID %q: %w", opts.execution, err)
		}
		input.ExecutionID = &executionID
	}
	// No `to`: the list has no window limit to fit, and an end read off this
	// machine's clock would hide approvals requested after it.
	if opts.since != "" {
		input.From, err = sinceTime(opts.since, time.Now())
		if err != nil {
			return err
		}
	}

	approvals, err := clidurable.NewService(opts.deps).ListApprovals(ctx, input)
	if err != nil {
		return err
	}
	if opts.jsonOutput {
		return writeJSON(opts.out, approvals)
	}
	output.DurableApprovals(opts.out, approvals, status, listCommand(opts, status))
	return nil
}

// listCommand rebuilds the command the page was listed with, minus paging, so
// the next-page hint keeps every filter: offset N of the unfiltered set is not
// offset N of a filtered one.
func listCommand(opts listOptions, status string) string {
	parts := []string{cliruntime.CommandPath(opts.deps, "durable approvals list")}
	if function := strings.TrimSpace(opts.function); function != "" {
		parts = append(parts, "--function", function)
	}
	switch status {
	case "":
		parts = append(parts, "--status", statusAll)
	case string(apiclient.DurableApprovalStatusPending):
	default:
		parts = append(parts, "--status", status)
	}
	if opts.execution != "" {
		parts = append(parts, "--execution", strings.TrimSpace(opts.execution))
	}
	if opts.since != "" {
		parts = append(parts, "--since", strings.TrimSpace(opts.since))
	}
	return strings.Join(parts, " ")
}

// normalizeStatusFilter returns the status to send, "" for every status, or
// an error naming what the flag takes. What counts as a status comes from the
// generated enum, so a status the API adds is accepted without a CLI change.
func normalizeStatusFilter(value string) (string, error) {
	status := strings.ToLower(strings.TrimSpace(value))
	if status == statusAll {
		return "", nil
	}
	if apiclient.DurableApprovalStatus(status).Valid() {
		return status, nil
	}
	return "", fmt.Errorf("unknown approval status %q: --status takes one of %s",
		strings.TrimSpace(value), approvalStatuses())
}

// approvalStatuses lists the filter's accepted values, in the order the
// contract declares them.
func approvalStatuses() string {
	statuses := []apiclient.DurableApprovalStatus{
		apiclient.DurableApprovalStatusPending,
		apiclient.DurableApprovalStatusApproved,
		apiclient.DurableApprovalStatusDenied,
		apiclient.DurableApprovalStatusExpired,
		apiclient.DurableApprovalStatusCancelled,
	}
	names := make([]string, 0, len(statuses)+1)
	for _, status := range statuses {
		names = append(names, string(status))
	}
	return strings.Join(append(names, statusAll), ", ")
}
