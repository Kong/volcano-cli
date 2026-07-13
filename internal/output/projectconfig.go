package output

import (
	"fmt"
	"io"
	"strings"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

const (
	configActionCreated   = "created"
	configActionUpdated   = "updated"
	configActionDeleted   = "deleted"
	configActionUnchanged = "unchanged"
	configActionError     = "error"
)

// ProjectConfigApplyReport renders the server's config apply (or dry-run)
// report: per-section action counts, per-entry errors and notices, and
// prominent warnings for skipped and missing resources.
func ProjectConfigApplyReport(w io.Writer, result *apiclient.ProjectConfigApplyResult) {
	if result == nil {
		return
	}

	printProjectConfigSectionCounts(w, result.Results)

	summary := result.Summary
	fmt.Fprintf(w, "Summary: %d created, %d updated, %d deleted, %d unchanged\n",
		summary.Created, summary.Updated, summary.Deleted, summary.Unchanged)

	for _, entry := range result.Results {
		if entry.Action == configActionError {
			detail := ""
			if entry.Error != nil {
				detail = *entry.Error
			}
			fmt.Fprintf(w, "Error: %s: %s\n", projectConfigEntryRef(entry), detail)
		}
	}
	for _, entry := range result.Results {
		if entry.Notice != nil && *entry.Notice != "" {
			Note(w, "%s: %s", projectConfigEntryRef(entry), *entry.Notice)
		}
	}

	for _, skipped := range result.Skipped {
		Warning(w, "%s %q is declared in the manifest but %s; its configuration was skipped — deploy or create it first, then re-run config deploy",
			skipped.Type, skipped.Name, skipped.Reason)
	}
	for _, missing := range result.Missing {
		Warning(w, "%s %q exists but is not covered by your manifest", missing.Type, missing.Name)
	}
}

func printProjectConfigSectionCounts(w io.Writer, results []apiclient.ProjectConfigApplyResultEntry) {
	type sectionCounts struct {
		created, updated, deleted, unchanged, errors int
	}
	order := make([]string, 0, len(results))
	bySection := make(map[string]*sectionCounts, len(results))
	for _, entry := range results {
		counts, seen := bySection[entry.Section]
		if !seen {
			counts = &sectionCounts{}
			bySection[entry.Section] = counts
			order = append(order, entry.Section)
		}
		switch string(entry.Action) {
		case configActionCreated:
			counts.created++
		case configActionUpdated:
			counts.updated++
		case configActionDeleted:
			counts.deleted++
		case configActionUnchanged:
			counts.unchanged++
		case configActionError:
			counts.errors++
		}
	}

	for _, section := range order {
		counts := bySection[section]
		parts := make([]string, 0, 5)
		if counts.created > 0 {
			parts = append(parts, fmt.Sprintf("%d created", counts.created))
		}
		if counts.updated > 0 {
			parts = append(parts, fmt.Sprintf("%d updated", counts.updated))
		}
		if counts.deleted > 0 {
			parts = append(parts, fmt.Sprintf("%d deleted", counts.deleted))
		}
		if counts.unchanged > 0 {
			parts = append(parts, fmt.Sprintf("%d unchanged", counts.unchanged))
		}
		if counts.errors > 0 {
			parts = append(parts, fmt.Sprintf("%d failed", counts.errors))
		}
		if len(parts) == 0 {
			continue
		}
		fmt.Fprintf(w, "%s: %s\n", section, strings.Join(parts, ", "))
	}
}

func projectConfigEntryRef(entry apiclient.ProjectConfigApplyResultEntry) string {
	if entry.Name != nil && *entry.Name != "" {
		return fmt.Sprintf("%s %q", entry.Section, *entry.Name)
	}
	return entry.Section
}

// ProjectConfigValidationErrors renders the server's 422 validation error
// list, one line per entry.
func ProjectConfigValidationErrors(w io.Writer, errs []apiclient.ProjectConfigValidationError) {
	if len(errs) == 0 {
		return
	}
	fmt.Fprintf(w, "The server rejected the configuration with %d validation error(s); nothing was applied:\n", len(errs))
	for _, entry := range errs {
		ref := entry.Section
		if entry.Name != nil && *entry.Name != "" {
			ref = fmt.Sprintf("%s %q", entry.Section, *entry.Name)
		}
		fmt.Fprintf(w, "  - %s: %s\n", ref, entry.Message)
	}
}
