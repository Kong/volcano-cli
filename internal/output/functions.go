package output

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Kong/volcano-cli/internal/apiclient"
	apicommon "github.com/Kong/volcano-cli/internal/apiclient/common"
)

// Functions renders one function list page.
func Functions(w io.Writer, page *apiclient.PaginatedFunctions, commandPrefix ...string) {
	if page == nil {
		page = &apiclient.PaginatedFunctions{}
	}

	functions := page.Data
	if len(functions) == 0 {
		if page.Total == 0 {
			fmt.Fprintln(w, "No functions deployed")
		} else {
			fmt.Fprintf(w, "No functions found on page %d\n", page.Page)
		}
		printFunctionPageSummary(w, page)
		return
	}

	fmt.Fprintf(w, "\n%-20s  %-15s  %-12s  %-15s  %-15s\n", "Name", "Runtime", "Status", "Created", "Updated")
	fmt.Fprintln(w, strings.Repeat("-", 88))
	for _, fn := range functions {
		fmt.Fprintf(w, "%-20s  %-15s  %-12s  %-15s  %-15s\n",
			Truncate(fn.Name, 20),
			blankString(stringPtrValue(fn.Runtime)),
			functionStatus(fn),
			FormatTimeAgo(fn.CreatedAt),
			FormatTimeAgo(fn.UpdatedAt),
		)
		if invokeURL := stringPtrValue(fn.InvokeUrl); invokeURL != "" {
			fmt.Fprintf(w, "  invoke: %s\n", invokeURL)
		}
	}
	printFunctionPageSummary(w, page)
	if page.HasMore {
		fmt.Fprintf(w, "\nNext page: %s functions list --page %d --limit %d\n", commandPathPrefix(commandPrefix), page.Page+1, page.Limit)
	}
}

func printFunctionPageSummary(w io.Writer, page *apiclient.PaginatedFunctions) {
	fmt.Fprintf(w, "\nShowing %d of %d function(s) (page %d, limit %d)\n", len(page.Data), page.Total, page.Page, page.Limit)
}

// Function renders one function.
func Function(w io.Writer, fn *apiclient.Function) {
	fmt.Fprintf(w, "ID: %s\n", fn.Id.String())
	fmt.Fprintf(w, "Name: %s\n", fn.Name)
	if runtime := stringPtrValue(fn.Runtime); runtime != "" {
		fmt.Fprintf(w, "Runtime: %s\n", runtime)
	}
	if handler := stringPtrValue(fn.Handler); handler != "" {
		fmt.Fprintf(w, "Handler: %s\n", handler)
	}
	fmt.Fprintf(w, "Status: %s\n", functionStatus(*fn))
	if len(fn.DeployedRegions) > 0 {
		fmt.Fprintf(w, "Regions: %s\n", strings.Join(fn.DeployedRegions, ", "))
	}
	visibility := "private"
	if fn.IsPublic {
		visibility = "public"
	}
	fmt.Fprintf(w, "Visibility: %s\n", visibility)
	if invokeURL := stringPtrValue(fn.InvokeUrl); invokeURL != "" {
		fmt.Fprintf(w, "Invoke URL: %s\n", invokeURL)
	}
	fmt.Fprintf(w, "Created: %s\n", FormatTimestamp(fn.CreatedAt))
	fmt.Fprintf(w, "Updated: %s\n", FormatTimestamp(fn.UpdatedAt))
}

// FunctionRuntimes renders function runtime options.
func FunctionRuntimes(w io.Writer, runtimes []apicommon.FunctionRuntimeOption) {
	if len(runtimes) == 0 {
		fmt.Fprintln(w, "No function runtimes found")
		return
	}

	fmt.Fprintf(w, "%-15s  %-10s  %-7s\n", "Runtime", "Language", "Default")
	fmt.Fprintln(w, strings.Repeat("-", 36))
	for _, runtime := range runtimes {
		defaultText := "no"
		if runtime.Default {
			defaultText = "yes"
		}
		fmt.Fprintf(w, "%-15s  %-10s  %-7s\n", runtime.Name, runtime.Language, defaultText)
	}
}

// LogEvents renders function log events.
func LogEvents(w io.Writer, events []apicommon.LogEvent) {
	for _, event := range events {
		printLogEvent(w, event.Timestamp, event.Region, event.Message)
	}
}

// LogSearchEvents renders function log search events.
func LogSearchEvents(w io.Writer, events []apicommon.LogSearchEvent) {
	for _, event := range events {
		printLogEvent(w, event.Timestamp, event.Region, event.Message)
	}
}

func printLogEvent(w io.Writer, timestamp time.Time, region *string, message string) {
	message = strings.TrimSpace(message)
	if region := stringPtrValue(region); region != "" {
		fmt.Fprintf(w, "%s  [%s] %s\n", timestamp.Format(time.RFC3339), region, message)
		return
	}
	fmt.Fprintf(w, "%s  %s\n", timestamp.Format(time.RFC3339), message)
}

func functionStatus(fn apiclient.Function) string {
	status := strings.TrimSpace(string(fn.Status))
	if status == "" {
		return "-"
	}
	return status
}

// Schedulers renders schedulers configured for a function.
func Schedulers(w io.Writer, fn *apiclient.Function, resp *apiclient.FunctionSchedulerListResponse) {
	if resp == nil {
		resp = &apiclient.FunctionSchedulerListResponse{}
	}
	if len(resp.Data) == 0 {
		if fn != nil {
			fmt.Fprintf(w, "No schedulers configured for function %q\n", fn.Name)
		} else {
			fmt.Fprintln(w, "No schedulers configured")
		}
		return
	}

	fmt.Fprintf(w, "\n%-36s  %-24s  %-9s  %-15s  %-20s  %-15s\n", "ID", "Name", "State", "Cron", "Next Run", "Last Run")
	fmt.Fprintln(w, strings.Repeat("-", 130))
	for _, scheduler := range resp.Data {
		fmt.Fprintf(w, "%-36s  %-24s  %-9s  %-15s  %-20s  %-15s\n",
			schedulerID(scheduler),
			Truncate(blankString(stringPtrValue(scheduler.Name)), 24),
			schedulerState(scheduler),
			Truncate(blankString(stringPtrValue(scheduler.CronExpression)), 15),
			blankString(timePtrFormat(scheduler.NextRunAt)),
			blankString(timePtrAgo(scheduler.LastStartedAt)),
		)
		if regions := schedulerRegions(scheduler); regions != "" {
			fmt.Fprintf(w, "  regions: %s\n", regions)
		}
	}
}

// Scheduler renders one scheduler.
func Scheduler(w io.Writer, scheduler *apiclient.FunctionScheduler) {
	if scheduler == nil {
		return
	}
	fmt.Fprintf(w, "ID: %s\n", schedulerID(*scheduler))
	if name := stringPtrValue(scheduler.Name); name != "" {
		fmt.Fprintf(w, "Name: %s\n", name)
	}
	fmt.Fprintf(w, "State: %s\n", schedulerState(*scheduler))
	if cron := stringPtrValue(scheduler.CronExpression); cron != "" {
		fmt.Fprintf(w, "Cron: %s\n", cron)
	}
	if regions := schedulerRegions(*scheduler); regions != "" {
		fmt.Fprintf(w, "Regions: %s\n", regions)
	}
	if next := timePtrFormat(scheduler.NextRunAt); next != "" {
		fmt.Fprintf(w, "Next Run: %s\n", next)
	}
	if last := timePtrFormat(scheduler.LastStartedAt); last != "" {
		fmt.Fprintf(w, "Last Run: %s\n", last)
	}
	if lastErr := stringPtrValue(scheduler.LastError); lastErr != "" {
		fmt.Fprintf(w, "Last Error: %s\n", lastErr)
	}
	if scheduler.CreatedAt != nil {
		fmt.Fprintf(w, "Created: %s\n", FormatTimestamp(*scheduler.CreatedAt))
	}
	if scheduler.UpdatedAt != nil {
		fmt.Fprintf(w, "Updated: %s\n", FormatTimestamp(*scheduler.UpdatedAt))
	}
}

func schedulerID(scheduler apiclient.FunctionScheduler) string {
	if scheduler.Id == nil {
		return "-"
	}
	return scheduler.Id.String()
}

func schedulerState(scheduler apiclient.FunctionScheduler) string {
	if scheduler.Enabled != nil && !*scheduler.Enabled {
		return "disabled"
	}
	return "enabled"
}

func schedulerRegions(scheduler apiclient.FunctionScheduler) string {
	if scheduler.Regions == nil {
		return ""
	}
	return strings.Join(*scheduler.Regions, ", ")
}

func timePtrFormat(t *time.Time) string {
	if t == nil || t.IsZero() {
		return ""
	}
	return FormatTimestamp(*t)
}

func timePtrAgo(t *time.Time) string {
	if t == nil || t.IsZero() {
		return ""
	}
	return FormatTimeAgo(*t)
}
