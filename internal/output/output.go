package output

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Kong/volcano-cli/internal/apiclient"
	"github.com/Kong/volcano-cli/internal/config"
	"github.com/Kong/volcano-cli/internal/theme"
)

// Success prints a check-marked success line (mark in volcano when color is on).
func Success(w io.Writer, format string, args ...any) {
	on := theme.On(w)
	fmt.Fprintf(w, "%s %s\n", theme.Success("✓", on), fmt.Sprintf(format, args...))
}

// Warning prints a warning line (prefix in amber when color is on).
func Warning(w io.Writer, format string, args ...any) {
	on := theme.On(w)
	fmt.Fprintf(w, "%s %s\n", theme.Warn("Warning:", on), fmt.Sprintf(format, args...))
}

// Note prints an informational note line (dimmed when color is on).
func Note(w io.Writer, format string, args ...any) {
	on := theme.On(w)
	fmt.Fprintf(w, "%s %s\n", theme.Dim("Note:", on), theme.Dim(fmt.Sprintf(format, args...), on))
}

// Errorf prints an error line (prefix in red when color is on).
func Errorf(w io.Writer, format string, args ...any) {
	on := theme.On(w)
	fmt.Fprintf(w, "%s %s\n", theme.Error("Error:", on), fmt.Sprintf(format, args...))
}

// Projects renders one project list page.
func Projects(w io.Writer, cfg *config.Config, page *apiclient.PaginatedProjects) {
	if page == nil {
		page = &apiclient.PaginatedProjects{}
	}

	on := theme.On(w)
	projects := page.Data
	if len(projects) == 0 {
		if page.Total == 0 {
			fmt.Fprintln(w, "No projects found")
			fmt.Fprintln(w, "\nCreate a project in the Volcano dashboard: https://volcano.dev")
		} else {
			fmt.Fprintf(w, "No projects found on page %d\n", page.Page)
		}
		printProjectPageSummary(w, on, page)
		printCurrentProject(w, on, cfg)
		return
	}

	tableHead(w, on, false, 120, "%-38s  %-20s  %-12s  %-10s  %-15s  %-15s", "ID", "Name", "Status", "Plan", "Created", "Updated")

	for _, project := range projects {
		fmt.Fprintf(w, "%-38s  %-20s  %s  %-10s  %-15s  %-15s\n",
			project.Id.String(),
			Truncate(project.Name, 20),
			statusCell(projectStatus(project), 12, on),
			projectPlan(project),
			FormatTimeAgo(project.CreatedAt),
			FormatTimeAgo(project.UpdatedAt),
		)
	}

	printProjectPageSummary(w, on, page)
	if page.HasMore {
		nextPage(w, on, fmt.Sprintf("volcano projects --page %d --limit %d", page.Page+1, page.Limit))
	}
	printCurrentProject(w, on, cfg)
}

func printProjectPageSummary(w io.Writer, on bool, page *apiclient.PaginatedProjects) {
	summary(w, on, "Showing %d of %d project(s) (page %d, limit %d)", len(page.Data), page.Total, page.Page, page.Limit)
}

func printCurrentProject(w io.Writer, on bool, cfg *config.Config) {
	if cfg.CurrentProject != nil {
		summary(w, on, "Current project: %s (%s)", cfg.CurrentProject.Name, cfg.CurrentProject.ID)
	}
}

// Project renders one project.
func Project(w io.Writer, project *apiclient.Project) {
	on := theme.On(w)
	fmt.Fprintf(w, "%s     %s\n", theme.Dim("ID:", on), project.Id.String())
	fmt.Fprintf(w, "%s   %s\n", theme.Dim("Name:", on), project.Name)
	fmt.Fprintf(w, "%s %s\n", theme.Dim("Status:", on), theme.Status(projectStatus(*project), on))
	fmt.Fprintf(w, "%s   %s\n", theme.Dim("Plan:", on), projectPlan(*project))
	fmt.Fprintf(w, "%s %s\n", theme.Dim("Created:", on), FormatTimestamp(project.CreatedAt))
	fmt.Fprintf(w, "%s %s\n", theme.Dim("Updated:", on), FormatTimestamp(project.UpdatedAt))
}

// AnonKeys renders a project's anon keys. The key value is the publishable JWT
// for the frontend/SDK Authorization header, so it is printed in full.
func AnonKeys(w io.Writer, keys []apiclient.AnonKey) {
	if len(keys) == 0 {
		fmt.Fprintln(w, "No anon keys for this project.")
		return
	}
	on := theme.On(w)
	for i := range keys {
		k := keys[i]
		defaultMarker := ""
		if k.IsDefault != nil && *k.IsDefault {
			defaultMarker = " (default)"
		}
		fmt.Fprintf(w, "%s%s\n", theme.Title(k.Name, on), theme.Dim(defaultMarker, on))
		fmt.Fprintf(w, "  %s  %s\n", theme.Dim("ID:", on), k.Id.String())
		fmt.Fprintf(w, "  %s %s\n", theme.Dim("Key:", on), k.KeyValue)
	}
}

// FormatTimestamp returns a local RFC3339 timestamp or "-".
func FormatTimestamp(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Local().Format(time.RFC3339)
}

// FormatTimeAgo returns a compact relative timestamp or "-".
func FormatTimeAgo(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	duration := time.Since(t)
	if duration < time.Minute {
		return fmt.Sprintf("%ds ago", int(duration.Seconds()))
	}
	if duration < time.Hour {
		return fmt.Sprintf("%dm ago", int(duration.Minutes()))
	}
	if duration < 24*time.Hour {
		return fmt.Sprintf("%dh ago", int(duration.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(duration.Hours()/24))
}

// Truncate shortens value to maxLen using an ellipsis when practical.
func Truncate(value string, maxLen int) string {
	if len(value) <= maxLen {
		return value
	}
	if maxLen < 3 {
		return value[:maxLen]
	}
	return value[:maxLen-3] + "..."
}

func projectStatus(project apiclient.Project) string {
	status := strings.TrimSpace(string(project.Status))
	if status == "" {
		return "unknown"
	}
	return status
}

func projectPlan(project apiclient.Project) string {
	if project.Plan == nil {
		return "-"
	}
	plan := strings.TrimSpace(string(*project.Plan))
	if plan == "" {
		return "-"
	}
	return plan
}
