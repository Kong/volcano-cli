package output

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Kong/volcano-cli/internal/apiclient"
	"github.com/Kong/volcano-cli/internal/theme"
)

// Functions renders one function list page.
func Functions(w io.Writer, page *apiclient.PaginatedFunctions, commandPrefix ...string) {
	if page == nil {
		page = &apiclient.PaginatedFunctions{}
	}

	on := theme.On(w)
	functions := page.Data
	if len(functions) == 0 {
		if page.Total == 0 {
			fmt.Fprintln(w, "No functions deployed")
		} else {
			fmt.Fprintf(w, "No functions found on page %d\n", page.Page)
		}
		printFunctionPageSummary(w, on, page)
		return
	}

	tableHead(w, on, true, 88, "%-20s  %-15s  %-12s  %-15s  %-15s", "Name", "Runtime", "Status", "Created", "Updated")
	for _, fn := range functions {
		fmt.Fprintf(w, "%-20s  %-15s  %s  %-15s  %-15s\n",
			Truncate(fn.Name, 20),
			blankString(stringPtrValue(fn.Runtime)),
			statusCell(functionStatus(fn), 12, on),
			FormatTimeAgo(fn.CreatedAt),
			FormatTimeAgo(fn.UpdatedAt),
		)
		if invokeURL := stringPtrValue(fn.InvokeUrl); invokeURL != "" {
			fmt.Fprintf(w, "  %s %s\n", theme.Dim("invoke:", on), invokeURL)
		}
	}
	printFunctionPageSummary(w, on, page)
	if page.HasMore {
		nextPage(w, on, fmt.Sprintf("%s functions list --page %d --limit %d", commandPathPrefix(commandPrefix), page.Page+1, page.Limit))
	}
}

func printFunctionPageSummary(w io.Writer, on bool, page *apiclient.PaginatedFunctions) {
	summary(w, on, "Showing %d of %d function(s) (page %d, limit %d)", len(page.Data), page.Total, page.Page, page.Limit)
}

// Function renders one function.
func Function(w io.Writer, fn *apiclient.Function) {
	on := theme.On(w)
	kv(w, on, "ID", "%s", fn.Id.String())
	kv(w, on, "Name", "%s", fn.Name)
	if runtime := stringPtrValue(fn.Runtime); runtime != "" {
		kv(w, on, "Runtime", "%s", runtime)
	}
	if handler := stringPtrValue(fn.Handler); handler != "" {
		kv(w, on, "Handler", "%s", handler)
	}
	kv(w, on, "Status", "%s", theme.Status(functionStatus(*fn), on))
	if len(fn.DeployedRegions) > 0 {
		kv(w, on, "Regions", "%s", strings.Join(fn.DeployedRegions, ", "))
	}
	visibility := "private"
	if fn.IsPublic {
		visibility = "public"
	}
	kv(w, on, "Visibility", "%s", theme.Status(visibility, on))
	if invokeURL := stringPtrValue(fn.InvokeUrl); invokeURL != "" {
		kv(w, on, "Invoke URL", "%s", invokeURL)
	}
	kv(w, on, "Created", "%s", FormatTimestamp(fn.CreatedAt))
	kv(w, on, "Updated", "%s", FormatTimestamp(fn.UpdatedAt))
}

// FunctionRuntimes renders function runtime options.
func FunctionRuntimes(w io.Writer, runtimes []apiclient.FunctionRuntimeOption) {
	if len(runtimes) == 0 {
		fmt.Fprintln(w, "No function runtimes found")
		return
	}

	on := theme.On(w)
	tableHead(w, on, false, 36, "%-15s  %-10s  %-7s", "Runtime", "Language", "Default")
	for _, runtime := range runtimes {
		defaultText := "no"
		if runtime.Default {
			defaultText = "yes"
		}
		fmt.Fprintf(w, "%-15s  %-10s  %s\n", runtime.Name, runtime.Language, statusCell(defaultText, 7, on))
	}
}

// LogEvents renders function log events.
func LogEvents(w io.Writer, events []apiclient.LogEvent) {
	for _, event := range events {
		printLogEvent(w, event.Timestamp, event.Region, event.Message)
	}
}

// LogSearchEvents renders function log search events.
func LogSearchEvents(w io.Writer, events []apiclient.LogSearchEvent) {
	for _, event := range events {
		printLogEvent(w, event.Timestamp, event.Region, event.Message)
	}
}

func printLogEvent(w io.Writer, timestamp time.Time, region *string, message string) {
	on := theme.On(w)
	message = strings.TrimSpace(message)
	formattedTimestamp := theme.Dim(FormatTimestamp(timestamp), on)
	if region := stringPtrValue(region); region != "" {
		fmt.Fprintf(w, "%s  %s %s\n", formattedTimestamp, theme.Dim("["+region+"]", on), message)
		return
	}
	fmt.Fprintf(w, "%s  %s\n", formattedTimestamp, message)
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
	on := theme.On(w)
	if len(resp.Data) == 0 {
		if fn != nil {
			fmt.Fprintf(w, "No schedulers configured for function %q\n", fn.Name)
		} else {
			fmt.Fprintln(w, "No schedulers configured")
		}
		return
	}

	tableHead(w, on, true, 130, "%-36s  %-24s  %-9s  %-15s  %-20s  %-15s", "ID", "Name", "State", "Cron", "Next Run", "Last Run")
	for _, scheduler := range resp.Data {
		fmt.Fprintf(w, "%-36s  %-24s  %s  %-15s  %-20s  %-15s\n",
			schedulerID(scheduler),
			Truncate(blankString(stringPtrValue(scheduler.Name)), 24),
			statusCell(schedulerState(scheduler), 9, on),
			Truncate(blankString(stringPtrValue(scheduler.CronExpression)), 15),
			blankString(timePtrFormat(scheduler.NextRunAt)),
			blankString(timePtrAgo(scheduler.LastStartedAt)),
		)
		if regions := schedulerRegions(scheduler); regions != "" {
			fmt.Fprintf(w, "  %s %s\n", theme.Dim("regions:", on), regions)
		}
	}
}

// Scheduler renders one scheduler.
func Scheduler(w io.Writer, scheduler *apiclient.FunctionScheduler) {
	if scheduler == nil {
		return
	}
	on := theme.On(w)
	kv(w, on, "ID", "%s", schedulerID(*scheduler))
	if name := stringPtrValue(scheduler.Name); name != "" {
		kv(w, on, "Name", "%s", name)
	}
	kv(w, on, "State", "%s", theme.Status(schedulerState(*scheduler), on))
	if cron := stringPtrValue(scheduler.CronExpression); cron != "" {
		kv(w, on, "Cron", "%s", cron)
	}
	if regions := schedulerRegions(*scheduler); regions != "" {
		kv(w, on, "Regions", "%s", regions)
	}
	if next := timePtrFormat(scheduler.NextRunAt); next != "" {
		kv(w, on, "Next Run", "%s", next)
	}
	if last := timePtrFormat(scheduler.LastStartedAt); last != "" {
		kv(w, on, "Last Run", "%s", last)
	}
	if lastErr := stringPtrValue(scheduler.LastError); lastErr != "" {
		kv(w, on, "Last Error", "%s", lastErr)
	}
	if scheduler.CreatedAt != nil {
		kv(w, on, "Created", "%s", FormatTimestamp(*scheduler.CreatedAt))
	}
	if scheduler.UpdatedAt != nil {
		kv(w, on, "Updated", "%s", FormatTimestamp(*scheduler.UpdatedAt))
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
