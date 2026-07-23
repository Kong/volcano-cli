package output

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Kong/volcano-cli/internal/apiclient"
	"github.com/Kong/volcano-cli/internal/config"
)

// Success prints a check-marked success line.
func Success(w io.Writer, format string, args ...any) {
	fmt.Fprintf(w, "✓ "+format+"\n", args...)
}

// Warning prints a warning line.
func Warning(w io.Writer, format string, args ...any) {
	fmt.Fprintf(w, "Warning: "+format+"\n", args...)
}

// Note prints an informational note line.
func Note(w io.Writer, format string, args ...any) {
	fmt.Fprintf(w, "Note: "+format+"\n", args...)
}

// Projects renders one project list page.
func Projects(w io.Writer, cfg *config.Config, page *apiclient.PaginatedProjects) {
	if page == nil {
		page = &apiclient.PaginatedProjects{}
	}

	projects := page.Data
	if len(projects) == 0 {
		if page.Total == 0 {
			fmt.Fprintln(w, "No projects found")
			fmt.Fprintln(w, "\nCreate a project in the Volcano dashboard: https://volcano.dev")
		} else {
			fmt.Fprintf(w, "No projects found on page %d\n", page.Page)
		}
		printProjectPageSummary(w, page)
		printCurrentProject(w, cfg)
		return
	}

	fmt.Fprintf(w, "%-38s  %-20s  %-12s  %-10s  %-15s  %-15s\n", "ID", "Name", "Status", "Plan", "Created", "Updated")
	fmt.Fprintln(w, strings.Repeat("-", 120))

	for _, project := range projects {
		fmt.Fprintf(w, "%-38s  %-20s  %-12s  %-10s  %-15s  %-15s\n",
			project.Id.String(),
			Truncate(project.Name, 20),
			projectStatus(project),
			projectPlan(project),
			FormatTimeAgo(project.CreatedAt),
			FormatTimeAgo(project.UpdatedAt),
		)
	}

	printProjectPageSummary(w, page)
	if page.HasMore {
		fmt.Fprintf(w, "\nNext page: volcano projects --page %d --limit %d\n", page.Page+1, page.Limit)
	}
	printCurrentProject(w, cfg)
}

func printProjectPageSummary(w io.Writer, page *apiclient.PaginatedProjects) {
	fmt.Fprintf(w, "\nShowing %d of %d project(s) (page %d, limit %d)\n", len(page.Data), page.Total, page.Page, page.Limit)
}

func printCurrentProject(w io.Writer, cfg *config.Config) {
	if cfg.CurrentProject != nil {
		fmt.Fprintf(w, "\nCurrent project: %s (%s)\n", cfg.CurrentProject.Name, cfg.CurrentProject.ID)
	}
}

// Project renders one project.
func Project(w io.Writer, project *apiclient.Project) {
	fmt.Fprintf(w, "ID:     %s\n", project.Id.String())
	fmt.Fprintf(w, "Name:   %s\n", project.Name)
	fmt.Fprintf(w, "Status: %s\n", string(project.Status))
	fmt.Fprintf(w, "Plan:   %s\n", projectPlan(*project))
	fmt.Fprintf(w, "Created: %s\n", FormatTimestamp(project.CreatedAt))
	fmt.Fprintf(w, "Updated: %s\n", FormatTimestamp(project.UpdatedAt))
}

// AnonKeys renders a project's anon keys. The key value is the publishable JWT
// for the frontend/SDK Authorization header, so it is printed in full.
func AnonKeys(w io.Writer, keys []apiclient.AnonKey) {
	if len(keys) == 0 {
		fmt.Fprintln(w, "No anon keys for this project.")
		return
	}
	for i := range keys {
		k := keys[i]
		defaultMarker := ""
		if k.IsDefault != nil && *k.IsDefault {
			defaultMarker = " (default)"
		}
		fmt.Fprintf(w, "%s%s\n", k.Name, defaultMarker)
		fmt.Fprintf(w, "  ID:  %s\n", k.Id.String())
		fmt.Fprintf(w, "  Key: %s\n", k.KeyValue)
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
