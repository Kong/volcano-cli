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
	// MergeAvailable is whether LavaFlow can merge its fixes for the project;
	// nil when unknown.
	MergeAvailable *bool
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
	if details.MergeAvailable != nil {
		kv(w, on, "Merging", "%s", lavaflowMergeAvailability(*details.MergeAvailable))
	}
	switch details.RepositoryState {
	case "connected":
		kv(w, on, "Repository", "%s", clean(details.Repository))
	case "not_connected":
		kv(w, on, "Repository", "none connected")
	}
	kv(w, on, "Custom context", "%s", setOrNot(settings.CustomContext))
	kv(w, on, "System prompt", "%s", setOrNot(settings.SystemPrompt))
	kv(w, on, "Updated", "%s", formatLavaflowTimePtr(settings.UpdatedAt, "not saved yet"))
	if !settings.CanUpdate {
		kv(w, on, "You can change it", "no: only the project's owner can, from their own session while their account is active")
	}
	if details.Available != nil && !*details.Available {
		Note(w, "LavaFlow isn't available for this project, so it can't be turned on or changed. Its records stay readable, and the owner can still turn it off.")
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
	opensFixes := settings.Autonomy == apiclient.OpenPr || settings.Autonomy == apiclient.Merge
	if details.RepositoryState == "not_connected" && opensFixes {
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
	lavaflowHumanReviewPaths(w, on, settings.MergePolicy)
	kv(w, on, "Updated", "%s", formatLavaflowTimePtr(settings.UpdatedAt, "not saved yet"))
	block(w, on, "Custom context", settings.CustomContext)
	block(w, on, "System prompt", settings.SystemPrompt)
}

// LavaflowSettingsSaved renders the settings a change left.
func LavaflowSettingsSaved(w io.Writer, message string, settings *apiclient.LavaflowSettings) {
	Success(w, "%s", message)
	on := theme.On(w)
	kv(w, on, "LavaFlow", "%s", onOff(settings.Enabled))
	kv(w, on, "Autonomy", "%s", lavaflowAutonomy(settings.Autonomy))
	lavaflowHumanReviewPaths(w, on, settings.MergePolicy)
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
			lavaflowResourceType(string(resource.Resource.Type)), Truncate(clean(resource.Resource.Name), 30),
			formatLavaflowTimePtr(resource.LastAnalyzedAt, "never"),
			resource.IssuesOpened, resource.PullRequestsOpened, mark)
	}
	summary(w, on, "%d resource(s), last %d day(s)", len(resources.Data), days)
	for _, resource := range resources.Data {
		if resource.Anomaly.Flagged && resource.Anomaly.Reason != nil {
			Warning(w, "%s: %s", clean(resource.Resource.Name), clean(*resource.Anomaly.Reason))
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
	tableHead(w, on, false, 140, "%-36s  %-31s  %-24s  %-30s  %-16s  %s",
		"ID", "Status", "Resource", "Title", "Started", "PR")
	for _, run := range page.Runs {
		fmt.Fprintf(w, "%-36s  %-31s  %-24s  %-30s  %-16s  %s\n",
			run.Id.String(), Truncate(lavaflowRunListStatus(run), 31), Truncate(clean(run.Resource.Name), 24),
			Truncate(clean(stringOr(run.Title, "-")), 30), formatLavaflowTime(run.CreatedAt), lavaflowPullRequestCell(run.PullRequest))
	}
	summary(w, on, "Showing %d run(s)", len(page.Runs))
	if page.NextCommand != "" {
		nextPage(w, on, clean(page.NextCommand))
	}
}

// LavaflowRun renders one run: what it worked on, its diagnosis, review,
// pull request, any failure in the API's own words, and the diff.
func LavaflowRun(w io.Writer, run *apiclient.LavaflowRun) {
	on := theme.On(w)
	kv(w, on, "Run", "%s", run.Id)
	kv(w, on, "Status", "%s", lavaflowRunStatus(run.Status))
	kv(w, on, "Resource", "%s %s", lavaflowResourceType(string(run.Resource.Type)), clean(run.Resource.Name))
	if run.Title != nil {
		kv(w, on, "Title", "%s", clean(*run.Title))
	}
	kv(w, on, "Issue", "%s", run.IssueId)
	kv(w, on, "Started", "%s", formatLavaflowTime(run.CreatedAt))
	if run.FinishedAt != nil {
		kv(w, on, "Finished", "%s", formatLavaflowTime(*run.FinishedAt))
	}
	if run.PullRequest != nil {
		kv(w, on, "Pull request", "#%d (%s) %s", run.PullRequest.Number, clean(string(run.PullRequest.State)), clean(run.PullRequest.Url))
		if run.PullRequest.OpenedAt != nil {
			kv(w, on, "Pull request opened", "%s", formatLavaflowTime(*run.PullRequest.OpenedAt))
		}
	}
	if run.Review != nil {
		kv(w, on, "Review", "%s after %d round(s)", clean(strings.ReplaceAll(string(run.Review.Verdict), "_", " ")), run.Review.Rounds)
	}
	if run.Merge != nil {
		lavaflowMergeLines(w, on, run.Merge)
	}
	if run.Failure != nil && run.Failure.Message != "" {
		// The API words each failure code itself, so it is printed as given.
		Warning(w, "%s", clean(run.Failure.Message))
	}
	if diagnosis := run.Diagnosis; diagnosis != nil {
		if diagnosis.Summary != "" {
			block(w, on, "Diagnosis", diagnosis.Summary)
		}
		if diagnosis.RootCause != "" {
			block(w, on, "Root cause", diagnosis.RootCause)
		}
		if len(diagnosis.Evidence) > 0 {
			fmt.Fprintf(w, "\n%s\n", theme.Title("Evidence", on))
			for _, item := range diagnosis.Evidence {
				fmt.Fprintf(w, "  - %s\n", clean(item))
			}
		}
		if len(diagnosis.LogLines) > 0 {
			fmt.Fprintf(w, "\n%s\n", theme.Title("Log lines", on))
			for _, line := range diagnosis.LogLines {
				stamp := ""
				if line.Timestamp != nil {
					stamp = line.Timestamp.UTC().Format(time.RFC3339) + " "
				}
				level := line.Level
				if level == "" {
					level = "-"
				}
				fmt.Fprintf(w, "  %s%s %s\n", stamp, strings.ToUpper(clean(level)), clean(line.Message))
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

// lavaflowMergeAvailability says whether autonomy merge can be chosen. The
// API gives no reason when it can't.
func lavaflowMergeAvailability(available bool) string {
	if available {
		return "available"
	}
	return "not available for this project yet"
}

func lavaflowAutonomy(autonomy apiclient.LavaflowAutonomy) string {
	switch autonomy {
	case apiclient.DiagnoseOnly:
		return "diagnose only: a person opens any fix"
	case apiclient.OpenPr:
		return "open fix pull requests"
	case apiclient.Merge:
		return "open fix pull requests and may merge them"
	default:
		return clean(string(autonomy))
	}
}

// lavaflowHumanReviewPaths prints the paths whose changes always hold a fix
// for a person, and nothing when the API doesn't report a merge policy.
func lavaflowHumanReviewPaths(w io.Writer, on bool, policy apiclient.LavaflowMergePolicy) {
	if policy.HumanReviewPaths == nil {
		return
	}
	paths := "none"
	if len(policy.HumanReviewPaths) > 0 {
		paths = cleanList(policy.HumanReviewPaths)
	}
	kv(w, on, "Human review paths", "%s", paths)
}

// lavaflowMergeLines says where a run's merge is, and why LavaFlow held or
// stopped it. Both are final: the pull request is then the person's. A status
// the CLI doesn't know prints as the API gives it.
func lavaflowMergeLines(w io.Writer, on bool, merge *apiclient.LavaflowMerge) {
	status := lavaflowMergeStatus(merge.Status)
	if merge.Reason != nil && *merge.Reason != "" {
		status += " (" + lavaflowMergeReason(*merge.Reason) + ")"
	}
	kv(w, on, "Merge", "%s", status)
	for _, reason := range merge.HoldReasons {
		line := clean(reason.Question)
		if strings.TrimSpace(line) == "" {
			line = clean(strings.ReplaceAll(reason.Code, "_", " "))
		}
		if len(reason.Paths) > 0 {
			line += " (" + cleanList(reason.Paths) + ")"
		}
		fmt.Fprintf(w, "  - %s\n", line)
	}
	if lavaflowMergeLeftToYou(merge.Status) {
		fmt.Fprintln(w, "The pull request is now yours to review and merge on GitHub.")
	}
}

// lavaflowMergeLeftToYou says whether LavaFlow left the pull request to
// people for good.
func lavaflowMergeLeftToYou(status apiclient.LavaflowMergeStatus) bool {
	return status == apiclient.LavaflowMergeStatusHeld || status == apiclient.LavaflowMergeStatusStopped
}

func lavaflowMergeReason(reason string) string {
	switch reason {
	case "paused":
		return "paused while merging is off"
	default:
		return clean(strings.ReplaceAll(reason, "_", " "))
	}
}

func lavaflowMergeStatus(status apiclient.LavaflowMergeStatus) string {
	switch status {
	case apiclient.LavaflowMergeStatusWaitingForChecks:
		return "waiting for CI"
	case apiclient.LavaflowMergeStatusHeld:
		return "held"
	case apiclient.LavaflowMergeStatusQueued:
		return "queued to merge"
	case apiclient.LavaflowMergeStatusMerged:
		return "merged"
	case apiclient.LavaflowMergeStatusStopped:
		return "stopped"
	default:
		return clean(string(status))
	}
}

func cleanList(items []string) string {
	cleaned := make([]string, len(items))
	for i, item := range items {
		cleaned[i] = clean(item)
	}
	return strings.Join(cleaned, ", ")
}

// lavaflowRunListStatus marks a run whose fix LavaFlow held or stopped, which
// otherwise reads as an open pull request.
func lavaflowRunListStatus(run apiclient.LavaflowRunSummary) string {
	switch {
	case run.Merge != nil && run.Merge.Status == apiclient.LavaflowMergeStatusHeld:
		return "Held: yours to merge"
	case run.Merge != nil && run.Merge.Status == apiclient.LavaflowMergeStatusStopped:
		return "Stopped: yours to merge"
	default:
		return lavaflowRunStatus(run.Status)
	}
}

// lavaflowRunStatus labels a run's status. The running stages say what the
// run is doing; a status the CLI doesn't know prints as the API gives it.
func lavaflowRunStatus(status apiclient.LavaflowRunStatus) string {
	switch status {
	case apiclient.LavaflowRunStatusDiagnosing:
		return "Running: finding the root cause"
	case apiclient.LavaflowRunStatusFixing:
		return "Running: writing the fix"
	case apiclient.LavaflowRunStatusReviewing:
		return "Running: reviewing the fix"
	case apiclient.LavaflowRunStatusPrOpen:
		return "PR open"
	default:
		return clean(string(status))
	}
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
	return fmt.Sprintf("#%d %s", pr.Number, clean(string(pr.State)))
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
