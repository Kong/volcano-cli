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
func DurableExecutions(w io.Writer, functionName string, page *apiclient.PaginatedDurableExecutions, commandPrefix ...string) {
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
		nextPage(w, on, fmt.Sprintf("%s durable executions list %s --page %d --limit %d",
			commandPathPrefix(commandPrefix), functionName, page.Page+1, page.Limit))
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
		return
	}
	encoded, err := json.MarshalIndent(execution.Result, "", "  ")
	if err != nil {
		kv(w, on, "Result", "%v", execution.Result)
		return
	}
	kv(w, on, "Result", "%s", string(encoded))
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
