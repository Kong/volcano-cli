package output

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Kong/volcano-cli/internal/apiclient"
	"github.com/Kong/volcano-cli/internal/theme"
)

// DurableFunctions renders one durable function list page.
func DurableFunctions(w io.Writer, page *apiclient.PaginatedDurableFunctions, commandPrefix ...string) {
	if page == nil {
		page = &apiclient.PaginatedDurableFunctions{}
	}

	on := theme.On(w)
	if len(page.Data) == 0 {
		if page.Total == 0 {
			fmt.Fprintln(w, "No durable functions deployed")
		} else {
			fmt.Fprintf(w, "No durable functions found on page %d\n", page.Page)
		}
		printDurableFunctionPageSummary(w, on, page)
		return
	}

	tableHead(w, on, true, 88, "%-20s  %-15s  %-12s  %-15s  %-15s", "Name", "Runtime", "Status", "Created", "Updated")
	for _, fn := range page.Data {
		fmt.Fprintf(w, "%-20s  %-15s  %s  %-15s  %-15s\n",
			Truncate(fn.Name, 20),
			blankString(stringPtrValue(fn.Runtime)),
			statusCell(durableFunctionStatus(fn), 12, on),
			FormatTimeAgo(fn.CreatedAt),
			FormatTimeAgo(fn.UpdatedAt),
		)
	}
	printDurableFunctionPageSummary(w, on, page)
	if page.HasMore {
		nextPage(w, on, fmt.Sprintf("%s durable list --page %d --limit %d",
			commandPathPrefix(commandPrefix), page.Page+1, page.Limit))
	}
}

func printDurableFunctionPageSummary(w io.Writer, on bool, page *apiclient.PaginatedDurableFunctions) {
	summary(w, on, "Showing %d of %d durable function(s) (page %d, limit %d)",
		len(page.Data), page.Total, page.Page, page.Limit)
}

// DurableFunction renders one durable function.
func DurableFunction(w io.Writer, fn *apiclient.DurableFunction) {
	if fn == nil {
		return
	}
	on := theme.On(w)
	kv(w, on, "ID", "%s", fn.Id.String())
	kv(w, on, "Name", "%s", fn.Name)
	if runtime := stringPtrValue(fn.Runtime); runtime != "" {
		kv(w, on, "Runtime", "%s", runtime)
	}
	if handler := stringPtrValue(fn.Handler); handler != "" {
		kv(w, on, "Handler", "%s", handler)
	}
	kv(w, on, "Status", "%s", theme.Status(durableFunctionStatus(*fn), on))
	if len(fn.DeployedRegions) > 0 {
		kv(w, on, "Regions", "%s", strings.Join(fn.DeployedRegions, ", "))
	}
	// Named "Anon key start" rather than "Visibility": a public durable function
	// is startable with an anon key, never invocable over HTTP the way a public
	// standard function is.
	kv(w, on, "Anon Key Start", "%s", theme.Status(durableVisibility(*fn), on))
	kv(w, on, "Execution Timeout", "%s", formatDurableSeconds(fn.Durable.ExecutionTimeoutSeconds))
	kv(w, on, "Retention", "%d day(s)", fn.Durable.RetentionDays)
	if fn.PendingDeploymentId != nil {
		kv(w, on, "Pending Deployment", "%s", fn.PendingDeploymentId.String())
	}
	kv(w, on, "Created", "%s", FormatTimestamp(fn.CreatedAt))
	kv(w, on, "Updated", "%s", FormatTimestamp(fn.UpdatedAt))
}

// DurableExecutions renders one execution list page for a durable function.
// status is the filter the page was fetched under, so the next-page hint keeps
// paging the same set rather than the unfiltered one.
func DurableExecutions(w io.Writer, functionName, status string, page *apiclient.PaginatedDurableExecutions, commandPrefix ...string) {
	if page == nil {
		page = &apiclient.PaginatedDurableExecutions{}
	}

	on := theme.On(w)
	if len(page.Data) == 0 {
		if page.Total == 0 {
			fmt.Fprintf(w, "No executions started for durable function %q\n", functionName)
		} else {
			fmt.Fprintf(w, "No executions found on page %d\n", page.Page)
		}
		printDurableExecutionPageSummary(w, on, page)
		return
	}

	tableHead(w, on, true, 118, "%-36s  %-24s  %-10s  %-16s  %-15s", "ID", "Name", "Status", "Region", "Started")
	for _, execution := range page.Data {
		fmt.Fprintf(w, "%-36s  %-24s  %s  %-16s  %-15s\n",
			execution.Id.String(),
			Truncate(execution.Name, 24),
			statusCell(string(execution.Status), 10, on),
			Truncate(execution.Region, 16),
			FormatTimeAgo(execution.CreatedAt),
		)
	}
	printDurableExecutionPageSummary(w, on, page)
	if page.HasMore {
		next := fmt.Sprintf("%s durable executions list %s", commandPathPrefix(commandPrefix), functionName)
		if status != "" {
			next += " --status " + status
		}
		nextPage(w, on, fmt.Sprintf("%s --page %d --limit %d", next, page.Page+1, page.Limit))
	}
}

func printDurableExecutionPageSummary(w io.Writer, on bool, page *apiclient.PaginatedDurableExecutions) {
	summary(w, on, "Showing %d of %d execution(s) (page %d, limit %d)",
		len(page.Data), page.Total, page.Page, page.Limit)
}

// DurableExecution renders one execution, including its outcome once it has
// one.
func DurableExecution(w io.Writer, execution *apiclient.DurableExecution) {
	if execution == nil {
		return
	}
	on := theme.On(w)
	kv(w, on, "ID", "%s", execution.Id.String())
	kv(w, on, "Name", "%s", execution.Name)
	kv(w, on, "Status", "%s", theme.Status(string(execution.Status), on))
	kv(w, on, "Region", "%s", execution.Region)
	kv(w, on, "Started", "%s", FormatTimestamp(execution.CreatedAt))
	if execution.CompletedAt != nil {
		kv(w, on, "Completed", "%s", FormatTimestamp(*execution.CompletedAt))
		kv(w, on, "Duration", "%s", formatDurableDuration(execution.CreatedAt, *execution.CompletedAt))
	}
	if execution.Error != nil {
		if errorType := stringPtrValue(execution.Error.Type); errorType != "" {
			kv(w, on, "Error Type", "%s", errorType)
		}
		if message := stringPtrValue(execution.Error.Message); message != "" {
			kv(w, on, "Error", "%s", message)
		}
	}
	printDurableExecutionResult(w, on, execution)
}

// printDurableExecutionResult distinguishes the three things an absent result
// can mean, because the difference is what a caller polling for one needs:
// still running, finished with nothing, or finished too long ago to still hold
// the result.
func printDurableExecutionResult(w io.Writer, on bool, execution *apiclient.DurableExecution) {
	if execution.ResultExpired != nil && *execution.ResultExpired {
		kv(w, on, "Result", "%s", theme.Dim("no longer retained", on))
		return
	}
	if execution.Result == nil {
		// A succeeded execution that returned `null` is reported with a result of
		// null, which decodes to the same nil as no result at all. Printing
		// nothing there reads as "finished with nothing", and the difference is
		// the whole reason a caller is looking.
		if execution.Status == apiclient.DurableExecutionStatusSucceeded {
			kv(w, on, "Result", "null")
		}
		return
	}
	encoded, err := json.MarshalIndent(execution.Result, "", "  ")
	if err != nil {
		kv(w, on, "Result", "%v", execution.Result)
		return
	}
	kv(w, on, "Result", "%s", string(encoded))
}

// DurableSchedulers renders the schedulers of one durable function. A tick
// starts an execution, so the run columns say when the scheduler last started
// one rather than when the function last ran.
func DurableSchedulers(w io.Writer, functionName string, resp *apiclient.FunctionSchedulerListResponse) {
	if resp == nil {
		resp = &apiclient.FunctionSchedulerListResponse{}
	}
	if len(resp.Data) == 0 {
		fmt.Fprintf(w, "No schedulers configured for durable function %q\n", functionName)
		return
	}
	schedulerTable(w, theme.On(w), resp.Data)
}

// DurableApprovals renders one approval page. status is the filter the page
// was fetched under, empty for every status, and nextCommand is the list
// command with its filters, so the next-page hint pages the same set.
func DurableApprovals(w io.Writer, page *apiclient.PaginatedDurableApprovals, status, nextCommand string) {
	if page == nil {
		page = &apiclient.PaginatedDurableApprovals{}
	}

	on := theme.On(w)
	if len(page.Data) == 0 {
		switch {
		case page.Total > 0:
			fmt.Fprintf(w, "No approvals found on page %d\n", page.Page)
		case status != "":
			fmt.Fprintf(w, "No %s approvals\n", status)
		default:
			fmt.Fprintln(w, "No approvals found")
		}
		printDurableApprovalPageSummary(w, on, page)
		return
	}

	tableHead(w, on, true, 140, "%-36s  %-24s  %-18s  %-20s  %-10s  %-10s  %-10s",
		"ID", "Title", "Workflow", "Execution", "Status", "Requested", "Expires")
	for i := range page.Data {
		approval := &page.Data[i]
		fmt.Fprintf(w, "%-36s  %-24s  %-18s  %-20s  %s  %-10s  %-10s\n",
			approval.Id.String(),
			Truncate(theme.StripControl(approval.Title), 24),
			Truncate(theme.StripControl(approval.Function.Name), 18),
			Truncate(theme.StripControl(approval.Execution.Name), 20),
			statusCell(string(approval.Status), 10, on),
			FormatTimeAgo(approval.RequestedAt),
			durableApprovalExpiresCell(approval),
		)
	}
	printDurableApprovalPageSummary(w, on, page)
	if page.HasMore {
		nextPage(w, on, fmt.Sprintf("%s --page %d --limit %d", nextCommand, page.Page+1, page.Limit))
	}
}

func printDurableApprovalPageSummary(w io.Writer, on bool, page *apiclient.PaginatedDurableApprovals) {
	summary(w, on, "Showing %d of %d approval(s) (page %d, limit %d)",
		len(page.Data), page.Total, page.Page, page.Limit)
}

// DurableApproval renders one approval: what the workflow asked, where it
// came from, and the decision once a person has made one. The workflow wrote
// the text, so it is printed without its control characters.
func DurableApproval(w io.Writer, approval *apiclient.DurableApproval) {
	if approval == nil {
		return
	}
	on := theme.On(w)
	kv(w, on, "ID", "%s", approval.Id.String())
	kv(w, on, "Title", "%s", theme.StripControl(approval.Title))
	kv(w, on, "Name", "%s", theme.StripControl(approval.Name))
	kv(w, on, "Status", "%s", theme.Status(string(approval.Status), on))
	kv(w, on, "Workflow", "%s", durableApprovalWorkflow(approval.Function))
	kv(w, on, "Execution", "%s", durableApprovalExecution(approval.Execution))
	kv(w, on, "Requested", "%s", FormatTimestamp(approval.RequestedAt))
	if approval.ExpiresAt != nil {
		kv(w, on, "Expires", "%s", FormatTimestamp(*approval.ExpiresAt))
	} else {
		kv(w, on, "Expires", "%s", theme.Dim("when its execution ends", on))
	}
	if approval.Description != "" {
		kv(w, on, "Description", "%s", theme.StripControl(approval.Description))
	}
	if approval.Details != nil {
		kv(w, on, "Details", "%s", durableApprovalDetails(approval.Details))
	}
	if decision := approval.Decision; decision != nil {
		decider := "a deleted user"
		if decision.DecidedBy != nil && decision.DecidedBy.Email != "" {
			decider = theme.StripControl(decision.DecidedBy.Email)
		}
		kv(w, on, "Decided By", "%s", decider)
		kv(w, on, "Decided At", "%s", FormatTimestamp(decision.DecidedAt))
		if decision.Comment != "" {
			kv(w, on, "Comment", "%s", theme.StripControl(decision.Comment))
		}
	}
}

// durableApprovalDetails renders what the workflow attached as indented JSON.
// The encoder escapes C0 controls inside strings but passes DEL and C1
// through, so each line is stripped too. The only raw line breaks are the
// indentation's.
func durableApprovalDetails(details any) string {
	encoded, err := json.MarshalIndent(details, "", "  ")
	if err != nil {
		return theme.StripControl(fmt.Sprint(details))
	}
	lines := strings.Split(string(encoded), "\n")
	for i := range lines {
		lines[i] = theme.StripControl(lines[i])
	}
	return strings.Join(lines, "\n")
}

// DurableApprovalStats renders approval counts for a window, overall and for
// the workflows that asked most.
func DurableApprovalStats(w io.Writer, stats *apiclient.DurableApprovalStats) {
	if stats == nil {
		return
	}
	on := theme.On(w)
	kv(w, on, "Window", "%s to %s", FormatTimestamp(stats.From), FormatTimestamp(stats.To))
	kv(w, on, "Requested", "%d", stats.Counts.Requested)
	kv(w, on, "Pending", "%d", stats.Counts.Pending)
	kv(w, on, "Approved", "%d", stats.Counts.Approved)
	kv(w, on, "Denied", "%d", stats.Counts.Denied)
	kv(w, on, "Expired", "%d", stats.Counts.Expired)
	kv(w, on, "Cancelled", "%d", stats.Counts.Cancelled)
	// Both are null until something in the window was decided, which is not
	// the same as a rate of zero.
	if stats.ApprovalRate != nil {
		kv(w, on, "Approval Rate", "%.0f%%", *stats.ApprovalRate*100)
	} else {
		kv(w, on, "Approval Rate", "-")
	}
	if stats.MedianSecondsToDecision != nil {
		kv(w, on, "Median Time to Decision", "%s", formatDurableSecondsFraction(*stats.MedianSecondsToDecision))
	} else {
		kv(w, on, "Median Time to Decision", "-")
	}

	if len(stats.Functions) == 0 {
		return
	}
	tableHead(w, on, true, 84, "%-24s  %-9s  %-8s  %-8s  %-8s  %-8s  %-9s",
		"Workflow", "Requested", "Pending", "Approved", "Denied", "Expired", "Cancelled")
	for _, fn := range stats.Functions {
		printDurableApprovalCountsRow(w, durableApprovalWorkflow(fn.Function), fn.Counts)
	}
	if stats.OtherFunctions.Requested > 0 {
		printDurableApprovalCountsRow(w, "(other workflows)", stats.OtherFunctions)
	}
}

func printDurableApprovalCountsRow(w io.Writer, workflow string, counts apiclient.DurableApprovalCounts) {
	fmt.Fprintf(w, "%-24s  %-9d  %-8d  %-8d  %-8d  %-8d  %-9d\n",
		Truncate(workflow, 24), counts.Requested, counts.Pending, counts.Approved,
		counts.Denied, counts.Expired, counts.Cancelled)
}

// durableApprovalWorkflow names the workflow, marking one that has since been
// deleted: its approvals are kept for a year after it is gone.
func durableApprovalWorkflow(fn apiclient.DurableApprovalFunction) string {
	name := theme.StripControl(fn.Name)
	if fn.Id == nil {
		return name + " (deleted)"
	}
	return name
}

func durableApprovalExecution(execution apiclient.DurableApprovalExecution) string {
	name := theme.StripControl(execution.Name)
	if execution.Id == nil {
		return name + " (no longer retained)"
	}
	if execution.Status == nil {
		return fmt.Sprintf("%s (%s)", name, execution.Id)
	}
	return fmt.Sprintf("%s (%s, %s)", name, execution.Id, *execution.Status)
}

// durableApprovalExpiresCell shows the deadline only where it still means
// something: a pending approval's, or when an expired one ran out.
func durableApprovalExpiresCell(approval *apiclient.DurableApproval) string {
	if approval.ExpiresAt == nil {
		return "-"
	}
	switch approval.Status {
	case apiclient.DurableApprovalStatusPending:
		return formatTimeUntil(*approval.ExpiresAt)
	case apiclient.DurableApprovalStatusExpired:
		return FormatTimeAgo(*approval.ExpiresAt)
	default:
		return "-"
	}
}

// formatTimeUntil is FormatTimeAgo for a time still ahead.
func formatTimeUntil(t time.Time) string {
	remaining := time.Until(t)
	switch {
	case remaining <= 0:
		return FormatTimeAgo(t)
	case remaining < time.Minute:
		return fmt.Sprintf("in %ds", int(remaining.Seconds()))
	case remaining < time.Hour:
		return fmt.Sprintf("in %dm", int(remaining.Minutes()))
	case remaining < 24*time.Hour:
		return fmt.Sprintf("in %dh", int(remaining.Hours()))
	default:
		return fmt.Sprintf("in %dd", int(remaining.Hours()/24))
	}
}

func formatDurableSecondsFraction(seconds float64) string {
	elapsed := time.Duration(seconds * float64(time.Second))
	if elapsed < time.Second {
		return elapsed.Round(time.Millisecond).String()
	}
	return elapsed.Round(time.Second).String()
}

func durableFunctionStatus(fn apiclient.DurableFunction) string {
	status := strings.TrimSpace(string(fn.Status))
	if status == "" {
		return "-"
	}
	return status
}

func durableVisibility(fn apiclient.DurableFunction) string {
	if fn.IsPublic {
		return "allowed"
	}
	return "denied"
}

func formatDurableSeconds(seconds int64) string {
	return (time.Duration(seconds) * time.Second).String()
}

func formatDurableDuration(from, to time.Time) string {
	elapsed := to.Sub(from)
	if elapsed < 0 {
		return "-"
	}
	return elapsed.Round(time.Second).String()
}
