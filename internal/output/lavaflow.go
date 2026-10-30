package output

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Kong/volcano-cli/internal/apiclient"
	"github.com/Kong/volcano-cli/internal/theme"
)

// LavaflowStatusDetails is what the status command shows.
type LavaflowStatusDetails struct {
	Settings *apiclient.LavaflowSettings
	// Available is whether LavaFlow takes changes for the project; nil when
	// unknown.
	Available *bool
	// RepositoryState is connected, not_connected or unknown.
	RepositoryState string
	Repository      string
	Description     string
	// EnableCommand and GitConnectCommand name what to run next.
	EnableCommand     string
	GitConnectCommand string
}

// LavaflowStatus renders whether LavaFlow is on for the project, how far it
// goes, and where its fix pull requests go.
func LavaflowStatus(w io.Writer, details LavaflowStatusDetails) {
	on := theme.On(w)
	settings := details.Settings
	kv(w, on, "LavaFlow", "%s", onOff(settings.Enabled))
	kv(w, on, "Autonomy", "%s", lavaflowAutonomy(settings.Autonomy))
	switch details.RepositoryState {
	case "connected":
		kv(w, on, "Repository", "%s", clean(details.Repository))
	case "not_connected":
		kv(w, on, "Repository", "none connected")
	}
	kv(w, on, "Custom context", "%s", setOrNot(settings.CustomContext))
	kv(w, on, "System prompt", "%s", setOrNot(settings.SystemPrompt))
	kv(w, on, "Updated", "%s", formatLavaflowTime(settings.UpdatedAt))
	if !settings.CanUpdate {
		kv(w, on, "You can change it", "no: only the project's owner can, from their own session")
	}
	if details.Available != nil && !*details.Available {
		Note(w, "LavaFlow isn't available to your account yet, so it can't be turned on or changed. Its records stay readable, and the owner can still turn it off.")
		return
	}
	if !settings.Enabled {
		if settings.CanUpdate {
			Note(w, "%s Turn it on with %s.", details.Description, theme.Command(details.EnableCommand, on))
		} else {
			Note(w, "%s", details.Description)
		}
		return
	}
	if details.RepositoryState == "not_connected" && settings.Autonomy == apiclient.OpenPr {
		Warning(w, "Connect a repository so LavaFlow can open fix pull requests: %s",
			theme.Command(details.GitConnectCommand, on))
	}
}

// LavaflowSettings renders every setting, with the custom context and system
// prompt in full.
func LavaflowSettings(w io.Writer, settings *apiclient.LavaflowSettings) {
	on := theme.On(w)
	kv(w, on, "LavaFlow", "%s", onOff(settings.Enabled))
	kv(w, on, "Autonomy", "%s", lavaflowAutonomy(settings.Autonomy))
	kv(w, on, "Updated", "%s", formatLavaflowTime(settings.UpdatedAt))
	block(w, on, "Custom context", settings.CustomContext)
	block(w, on, "System prompt", settings.SystemPrompt)
}

// LavaflowSettingsSaved renders the settings a change left.
func LavaflowSettingsSaved(w io.Writer, message string, settings *apiclient.LavaflowSettings) {
	Success(w, "%s", message)
	on := theme.On(w)
	kv(w, on, "LavaFlow", "%s", onOff(settings.Enabled))
	kv(w, on, "Autonomy", "%s", lavaflowAutonomy(settings.Autonomy))
	kv(w, on, "Custom context", "%s", setOrNot(settings.CustomContext))
	kv(w, on, "System prompt", "%s", setOrNot(settings.SystemPrompt))
}

// LavaflowStats renders the issues LavaFlow took on and the fix pull requests
// it opened per day. Merges are never counted.
func LavaflowStats(w io.Writer, stats *apiclient.LavaflowStats) {
	on := theme.On(w)
	tableHead(w, on, false, 44, "%-12s  %14s  %16s", "Date", "Issues opened", "Fix PRs opened")
	for _, day := range stats.Days {
		fmt.Fprintf(w, "%-12s  %14d  %16d\n", day.Date.Format(time.DateOnly), day.IssuesOpened, day.PullRequestsOpened)
	}
	summary(w, on, "%d issue(s) opened and %d fix pull request(s) opened, %s to %s",
		stats.Totals.IssuesOpened, stats.Totals.PullRequestsOpened,
		stats.WindowStart.Format(time.DateOnly), stats.WindowEnd.Format(time.DateOnly))
}

// LavaflowResources renders every function, frontend and durable function with
// what LavaFlow did for it, flagging a sudden jump in issues.
func LavaflowResources(w io.Writer, resources *apiclient.LavaflowResourceList, days int) {
	on := theme.On(w)
	if len(resources.Data) == 0 {
		fmt.Fprintln(w, "No functions, frontends or durable functions")
		return
	}
	tableHead(w, on, false, 100, "%-16s  %-30s  %-16s  %6s  %7s  %s",
		"Type", "Name", "Last analyzed", "Issues", "Fix PRs", "")
	for _, resource := range resources.Data {
		mark := ""
		if resource.Anomaly.Flagged {
			mark = theme.Warn("issue rate jumped", on)
		}
		fmt.Fprintf(w, "%-16s  %-30s  %-16s  %6d  %7d  %s\n",
			lavaflowResourceType(string(resource.Type)), Truncate(clean(resource.Name), 30),
			formatLavaflowTimePtr(resource.LastAnalyzedAt, "never"),
			resource.IssuesOpened, resource.PullRequestsOpened, mark)
	}
	summary(w, on, "%d resource(s), last %d day(s)", len(resources.Data), days)
	for _, resource := range resources.Data {
		if resource.Anomaly.Flagged && resource.Anomaly.Reason != nil {
			Warning(w, "%s: %s", clean(resource.Name), clean(*resource.Anomaly.Reason))
		}
	}
}

// LavaflowRunsPage is one page of runs and the command for the next.
type LavaflowRunsPage struct {
	Runs []apiclient.LavaflowRunSummary
	// NextCommand lists the next page; empty on the last one.
	NextCommand string
}

// LavaflowRuns renders one page of runs, newest first.
func LavaflowRuns(w io.Writer, page LavaflowRunsPage) {
	on := theme.On(w)
	if len(page.Runs) == 0 {
		fmt.Fprintln(w, "No runs")
		return
	}
	tableHead(w, on, false, 120, "%-36s  %-10s  %-24s  %-30s  %-16s  %s",
		"ID", "Status", "Resource", "Title", "Started", "PR")
	for _, run := range page.Runs {
		fmt.Fprintf(w, "%-36s  %-10s  %-24s  %-30s  %-16s  %s\n",
			clean(run.Id), lavaflowRunStatus(run.Status), Truncate(clean(run.Resource.Name), 24),
			Truncate(clean(stringOr(run.Title, "-")), 30), formatLavaflowTime(run.CreatedAt), lavaflowPullRequestCell(run.PullRequest))
	}
	summary(w, on, "Showing %d run(s)", len(page.Runs))
	if page.NextCommand != "" {
		nextPage(w, on, page.NextCommand)
	}
}

// LavaflowRun renders one run: what it worked on, its diagnosis, review,
// pull request, any failure in the API's own words, and the diff.
func LavaflowRun(w io.Writer, run *apiclient.LavaflowRun) {
	on := theme.On(w)
	kv(w, on, "Run", "%s", clean(run.Id))
	kv(w, on, "Status", "%s", lavaflowRunStatus(run.Status))
	kv(w, on, "Resource", "%s %s", lavaflowResourceType(run.Resource.Type), clean(run.Resource.Name))
	if run.Title != nil {
		kv(w, on, "Title", "%s", clean(*run.Title))
	}
	if run.IssueId != nil {
		kv(w, on, "Issue", "%s", clean(*run.IssueId))
	}
	kv(w, on, "Started", "%s", formatLavaflowTime(run.CreatedAt))
	if run.FinishedAt != nil {
		kv(w, on, "Finished", "%s", formatLavaflowTime(*run.FinishedAt))
	}
	if run.PullRequest != nil {
		kv(w, on, "Pull request", "#%d (%s) %s", run.PullRequest.Number, run.PullRequest.State, clean(run.PullRequest.Url))
	}
	if run.Review != nil && run.Review.Verdict != nil {
		rounds := 0
		if run.Review.Rounds != nil {
			rounds = *run.Review.Rounds
		}
		kv(w, on, "Review", "%s after %d round(s)", strings.ReplaceAll(string(*run.Review.Verdict), "_", " "), rounds)
	}
	if run.Failure != nil && run.Failure.Message != nil {
		// The API words each failure code itself, so it is printed as given.
		Warning(w, "%s", clean(*run.Failure.Message))
	}
	if run.DetailsClearedAt != nil {
		Note(w, "Details were cleared after 90 days, on %s", formatLavaflowTime(*run.DetailsClearedAt))
		return
	}
	if run.DetailsExpireAt != nil {
		kv(w, on, "Details kept until", "%s", formatLavaflowTime(*run.DetailsExpireAt))
	}
	if diagnosis := run.Diagnosis; diagnosis != nil {
		if diagnosis.Summary != nil {
			block(w, on, "Diagnosis", *diagnosis.Summary)
		}
		if diagnosis.RootCause != nil {
			block(w, on, "Root cause", *diagnosis.RootCause)
		}
		if diagnosis.Evidence != nil && len(*diagnosis.Evidence) > 0 {
			fmt.Fprintf(w, "\n%s\n", theme.Title("Evidence", on))
			for _, item := range *diagnosis.Evidence {
				fmt.Fprintf(w, "  - %s\n", clean(item))
			}
		}
		if diagnosis.LogLines != nil && len(*diagnosis.LogLines) > 0 {
			fmt.Fprintf(w, "\n%s\n", theme.Title("Log lines", on))
			for _, line := range *diagnosis.LogLines {
				stamp := ""
				if line.Timestamp != nil {
					stamp = line.Timestamp.UTC().Format(time.RFC3339) + " "
				}
				fmt.Fprintf(w, "  %s%s %s\n", stamp, strings.ToUpper(clean(stringOr(line.Level, "-"))), clean(stringOr(line.Message, "")))
			}
		}
	}
	if run.Diff != nil && *run.Diff != "" {
		block(w, on, "Diff", *run.Diff)
	}
}

func block(w io.Writer, on bool, title, text string) {
	fmt.Fprintf(w, "\n%s\n", theme.Title(title, on))
	if strings.TrimSpace(text) == "" {
		fmt.Fprintln(w, theme.Dim("  (not set)", on))
		return
	}
	for line := range strings.SplitSeq(strings.TrimRight(text, "\n"), "\n") {
		fmt.Fprintf(w, "  %s\n", clean(line))
	}
}

// clean prints text from the API, which can come from logs or a repository,
// as plain text: each control character other than tab, such as an escape,
// a carriage return or a C1 code, prints as a visible escape like \x1b, so
// the text can't move the cursor, change colors or set links or the title.
// A newline is escaped too; block splits text into lines before this.
func clean(text string) string {
	var b strings.Builder
	for _, r := range text {
		switch {
		case r == '\t':
			b.WriteRune(r)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, "\\x%02x", r)
		case r >= 0x80 && r <= 0x9f:
			fmt.Fprintf(&b, "\\u%04x", r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func setOrNot(text string) string {
	if strings.TrimSpace(text) == "" {
		return "not set"
	}
	return fmt.Sprintf("set (%d characters)", len([]rune(text)))
}

func lavaflowAutonomy(autonomy apiclient.LavaflowAutonomy) string {
	switch autonomy {
	case apiclient.DiagnoseOnly:
		return "diagnose only: a person opens any fix"
	case apiclient.OpenPr:
		return "open fix pull requests"
	default:
		return clean(string(autonomy))
	}
}

func lavaflowRunStatus(status apiclient.LavaflowRunStatus) string {
	if status == apiclient.LavaflowRunStatusPrOpen {
		return "PR open"
	}
	return clean(string(status))
}

func lavaflowResourceType(kind string) string {
	switch kind {
	case "durable_function":
		return "durable function"
	default:
		return clean(kind)
	}
}

func lavaflowPullRequestCell(pr *apiclient.LavaflowPullRequest) string {
	if pr == nil {
		return "-"
	}
	return fmt.Sprintf("#%d %s", pr.Number, pr.State)
}

func stringOr(value *string, fallback string) string {
	if value == nil {
		return fallback
	}
	return *value
}

func formatLavaflowTime(t time.Time) string {
	return t.Local().Format("2006-01-02 15:04")
}

func formatLavaflowTimePtr(t *time.Time, fallback string) string {
	if t == nil {
		return fallback
	}
	return formatLavaflowTime(*t)
}
