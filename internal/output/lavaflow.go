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
		kv(w, on, "Merging", "%s", lavaflowMergeAvailability(*details.MergeAvailable, settings, details.Available))
	}
	switch details.RepositoryState {
	case "connected":
		kv(w, on, "Repository", "%s", clean(details.Repository))
	case "not_connected":
		kv(w, on, "Repository", "none connected")
	}
	kv(w, on, "Ungated workflows", "%s", lavaflowUngatedWorkflows(settings.AllowUngatedWorkflows))
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
	// LavaFlow on starts no run for a repository other than the one it was
	// turned on for, so connecting one takes turning it on again.
	opensFixes := settings.Autonomy == apiclient.OpenPr || settings.Autonomy == apiclient.Merge
	switch {
	case details.RepositoryState != "not_connected" || !opensFixes:
	case settings.CanUpdate:
		Warning(w, "Connect a repository, then turn LavaFlow on again so it can open fix pull requests: %s, then %s",
			theme.Command(details.GitConnectCommand, on), theme.Command(details.EnableCommand, on))
	default:
		Warning(w, "Connect a repository, then have the project's owner turn LavaFlow on again so it can open fix pull requests: %s",
			theme.Command(details.GitConnectCommand, on))
	}
}

// LavaflowSettings renders every setting, with the custom context and system
// prompt in full. available and mergeAvailable are whether LavaFlow takes
// changes and can merge its fixes for the project; a nil mergeAvailable
// leaves the Merging line out.
func LavaflowSettings(w io.Writer, settings *apiclient.LavaflowSettings, available, mergeAvailable *bool) {
	on := theme.On(w)
	kv(w, on, "LavaFlow", "%s", onOff(settings.Enabled))
	kv(w, on, "Autonomy", "%s", lavaflowAutonomy(settings.Autonomy))
	if mergeAvailable != nil {
		kv(w, on, "Merging", "%s", lavaflowMergeAvailability(*mergeAvailable, settings, available))
	}
	lavaflowHumanReviewPaths(w, on, settings.MergePolicy)
	kv(w, on, "Ungated workflows", "%s", lavaflowUngatedWorkflows(settings.AllowUngatedWorkflows))
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
	kv(w, on, "Ungated workflows", "%s", lavaflowUngatedWorkflows(settings.AllowUngatedWorkflows))
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
	window := "from " + stats.WindowStart.Format(time.DateOnly) + " to " + stats.WindowEnd.Format(time.DateOnly)
	if stats.WindowStart.Format(time.DateOnly) == stats.WindowEnd.Format(time.DateOnly) {
		window = "on " + stats.WindowStart.Format(time.DateOnly)
	}
	summary(w, on, "%s and %s opened %s", lavaflowCount(stats.Totals.IssuesOpened, "issue", "issues"),
		lavaflowCount(stats.Totals.PullRequestsOpened, "fix pull request", "fix pull requests"), window)
}

// lavaflowCount is a count with its noun, singular for one.
func lavaflowCount(n int64, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// LavaflowResources renders every function, frontend and durable function with
// what LavaFlow did for it, flagging a sudden jump in issues.
func LavaflowResources(w io.Writer, resources *apiclient.LavaflowResourceList, days int) {
	on := theme.On(w)
	if len(resources.Data) == 0 {
		fmt.Fprintln(w, "No functions, frontends or durable functions")
		return
	}
	tableHead(w, on, false, 101, "%-16s  %-30s  %-17s  %6s  %7s  %s",
		"Type", "Name", "Last analyzed", "Issues", "Fix PRs", "")
	for _, resource := range resources.Data {
		mark := ""
		if resource.Anomaly.Flagged {
			mark = theme.Warn("issue rate jumped", on)
		}
		fmt.Fprintf(w, "%-16s  %-30s  %-17s  %6d  %7d  %s\n",
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

// LavaflowRuns renders one page of runs, newest first. The title comes last
// and whole, so two titles that start alike still read apart.
func LavaflowRuns(w io.Writer, page LavaflowRunsPage) {
	on := theme.On(w)
	if len(page.Runs) == 0 {
		fmt.Fprintln(w, "No runs")
		return
	}
	tableHead(w, on, false, 160, "%-36s  %-31s  %-30s  %-24s  %-17s  %s",
		"ID", "Status", "Outcome", "Resource", "Queued", "Title")
	for _, run := range page.Runs {
		fmt.Fprintf(w, "%-36s  %-31s  %-30s  %-24s  %-17s  %s\n",
			run.Id.String(), Truncate(lavaflowRunLabel(run.Status, run.Merge, run.PullRequest), 31),
			Truncate(lavaflowRunOutcome(run), 30), Truncate(lavaflowResourceName(run.Resource.Name), 24),
			formatLavaflowTime(run.CreatedAt), clean(stringOr(run.Title, "-")))
	}
	summary(w, on, "Showing %d run(s)", len(page.Runs))
	if page.NextCommand != "" {
		nextPage(w, on, clean(page.NextCommand))
	}
}

// LavaflowRunStarted renders a run a person started: its ID and status, and
// the command that follows it.
func LavaflowRunStarted(w io.Writer, runID string, status apiclient.LavaflowRunStatus, showCommand string) {
	Success(w, "LavaFlow run started")
	on := theme.On(w)
	kv(w, on, "Run", "%s", clean(runID))
	kv(w, on, "Status", "%s", lavaflowRunStatus(status))
	fmt.Fprintf(w, "Follow it with %s\n", theme.Command(showCommand, on))
}

// LavaflowRunCommands are the commands a run's next steps name.
type LavaflowRunCommands struct {
	// StartRun starts a new run on the run's issue.
	StartRun string
	// SettingsSet changes LavaFlow's settings; a step adds its flags.
	SettingsSet string
	// List lists the project's runs; a step adds its filters.
	List string
}

// LavaflowRun renders one run: what it worked on, the commit it read, its
// review, what happened to its fix and what to do next, any failure in the
// API's own words, its diagnosis and the diff.
func LavaflowRun(w io.Writer, run *apiclient.LavaflowRun, commands LavaflowRunCommands) {
	on := theme.On(w)
	kv(w, on, "Run", "%s", run.Id)
	kv(w, on, "Status", "%s", lavaflowRunLabel(run.Status, run.Merge, run.PullRequest))
	kv(w, on, "Resource", "%s %s", lavaflowResourceType(string(run.Resource.Type)), lavaflowResourceName(run.Resource.Name))
	if run.Title != nil {
		kv(w, on, "Title", "%s", clean(*run.Title))
	}
	kv(w, on, "Issue", "%s", run.IssueId)
	if source := lavaflowSourceCommit(run.SourceCommit); source != "" {
		kv(w, on, "Source", "%s", source)
	}
	kv(w, on, "Queued", "%s", formatLavaflowTime(run.CreatedAt))
	if run.FinishedAt != nil {
		kv(w, on, "Finished", "%s", formatLavaflowTime(*run.FinishedAt))
	}
	if run.Review != nil {
		verdict := clean(strings.ReplaceAll(string(run.Review.Verdict), "_", " "))
		// A review with no round never had a reviewer that could run.
		switch run.Review.Rounds {
		case 0:
			kv(w, on, "Review", "%s; no reviewer could run", verdict)
		case 1:
			kv(w, on, "Review", "%s after 1 round", verdict)
		default:
			kv(w, on, "Review", "%s after %d rounds", verdict, run.Review.Rounds)
		}
		if run.Review.Stopped != nil {
			kv(w, on, "Review stopped", "%s", lavaflowReviewStopped(*run.Review.Stopped))
		}
		lavaflowOpenFindings(w, on, run.Review.OpenFindings)
		lavaflowPersonReasons(w, on, run.Review.PersonReasons)
	}
	if checks := run.RepositoryChecks; checks != nil {
		kv(w, on, "Checks", "%s", lavaflowRepositoryChecks(checks))
		lavaflowCheckCommands(w, on, checks.Commands)
	}
	// The API lists only the first ungated job on each branch or commit it
	// checked, so a count would understate the jobs that could run the change.
	// Null, no record of them, prints nothing, as none does.
	if workflows := run.UngatedWorkflows; workflows != nil && len(*workflows) > 0 {
		kv(w, on, "Ungated workflows", "these jobs, and maybe others, may run this pull request with the repository's "+
			"secrets before a person reviews it. LavaFlow opened it anyway, as the project's owner allows.")
		for _, workflow := range *workflows {
			fmt.Fprintf(w, "  - %s\n", lavaflowAllowedWorkflow(workflow))
		}
		fmt.Fprintln(w, "  Gate them on a label a person adds.")
	}
	lavaflowFix(w, on, run, commands)
	// The API words each failure code itself, so it is printed as given. The
	// Fix block already says why a failed or cancelled run opened nothing.
	if run.Failure != nil && run.Failure.Message != "" && !lavaflowFailureInFix(run) {
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

// lavaflowReviewStopped says why a review ended without passing the fix. A
// reason the CLI doesn't know reads as other, with its code.
func lavaflowReviewStopped(stopped apiclient.LavaflowReviewStop) string {
	switch stopped {
	case apiclient.LavaflowReviewStopRounds:
		return "The last review round still found problems."
	case apiclient.LavaflowReviewStopNoRevision:
		return "LavaFlow gave no revised fix."
	case apiclient.LavaflowReviewStopBudgetExhausted:
		return "The run used its token budget before LavaFlow could revise the fix, not the daily spend limit."
	case apiclient.LavaflowReviewStopContextLimit:
		return "The fix's conversation grew too long for LavaFlow before it could revise it."
	case apiclient.LavaflowReviewStopTimedOut:
		return "The run's time for review ran out."
	case apiclient.LavaflowReviewStopReviewersFailed:
		return "No reviewer of the last round finished."
	case apiclient.LavaflowReviewStopTooFewReviewers:
		return "Fewer reviewers could run than a round needs to pass the fix."
	case apiclient.LavaflowReviewStopUnreadable:
		return "A retried run couldn't read the review saved with its fix, so a person must review it."
	default:
		return "Other (" + clean(strings.ReplaceAll(string(stopped), "_", " ")) + ")."
	}
}

// lavaflowOpenFindings lists the findings the review left open, in the order
// the API sends them. The file and explanation are the reviewer's words, and
// a sentence Volcano put in place of withheld text prints as given.
func lavaflowOpenFindings(w io.Writer, on bool, findings []apiclient.LavaflowReviewFinding) {
	if len(findings) == 0 {
		return
	}
	fmt.Fprintln(w, theme.Dim("Open findings:", on))
	for _, finding := range findings {
		head := clean(string(finding.Severity))
		if place := lavaflowFindingPlace(finding); place != "" {
			head += ", " + place
		}
		reviewItem(w, head, finding.Explanation)
	}
}

// lavaflowFindingPlace is where a finding points: its file and line, or ""
// for a finding about no file. An empty file is a path Volcano doesn't show.
func lavaflowFindingPlace(finding apiclient.LavaflowReviewFinding) string {
	var place string
	switch {
	case finding.File == nil:
		if finding.Line != nil {
			place = fmt.Sprintf("line %d", *finding.Line)
		}
	case *finding.File == "":
		place = "path not shown"
		if finding.Line != nil {
			place += fmt.Sprintf(", line %d", *finding.Line)
		}
	default:
		place = clean(*finding.File)
		if finding.Line != nil {
			place += fmt.Sprintf(":%d", *finding.Line)
		}
	}
	return place
}

// lavaflowPersonReasons lists what the review leaves to a person to decide
// before the fix merges, in the order the API sends them.
func lavaflowPersonReasons(w io.Writer, on bool, reasons []apiclient.LavaflowReviewPersonReason) {
	if len(reasons) == 0 {
		return
	}
	fmt.Fprintln(w, theme.Dim("For you to decide:", on))
	for _, reason := range reasons {
		reviewItem(w, lavaflowPersonReasonKind(reason.Kind), reason.Question)
	}
}

// lavaflowPersonReasonKind names what a decision is about. The API may add
// kinds, and one the CLI doesn't know prints as the API gives it.
func lavaflowPersonReasonKind(kind string) string {
	switch kind {
	case "trust_boundary":
		return "trust boundary"
	case "breaks_contract":
		return "breaks a contract"
	case "not_reviewable":
		return "not reviewable"
	default:
		return clean(kind)
	}
}

// reviewItem writes one "  - head: text" line of the review's lists. Text on
// more than one line continues indented under it.
func reviewItem(w io.Writer, head, text string) {
	var lines []string
	if strings.TrimSpace(text) != "" {
		lines = strings.Split(strings.TrimRight(text, "\n"), "\n")
	}
	switch {
	case len(lines) == 0 && head == "":
		return
	case len(lines) == 0:
		fmt.Fprintf(w, "  - %s\n", head)
		return
	case head == "":
		fmt.Fprintf(w, "  - %s\n", clean(lines[0]))
	default:
		fmt.Fprintf(w, "  - %s: %s\n", head, clean(lines[0]))
	}
	for _, line := range lines[1:] {
		fmt.Fprintf(w, "    %s\n", clean(line))
	}
}

// lavaflowRepositoryChecks says whether the repository's own checks ran on
// the fix and what they found, in the API's words. Without them, it says so
// from the status, and reads a status it doesn't know as not_run, as the API
// asks.
func lavaflowRepositoryChecks(checks *apiclient.LavaflowRepositoryChecks) string {
	if message := strings.TrimSpace(checks.Message); message != "" {
		return clean(message)
	}
	switch checks.Status {
	case "passed":
		return "The repository's checks passed."
	case "failed":
		// A revised fix is not the version the checks ran on.
		if checks.Revised {
			return "The repository's checks failed on an earlier version of this fix."
		}
		return "The repository's checks failed."
	case "no_checks_found":
		return "LavaFlow found no checks to run in the repository."
	default:
		return "The repository's tests didn't run."
	}
}

// lavaflowCheckCommands lists the commands the checks ran, in the order the
// API sends them, each with how it ended and its exit code. A command's text
// and output are the repository's, so the output is indented under the
// command and marked as its output, and can't pass for the CLI's own lines.
func lavaflowCheckCommands(w io.Writer, on bool, commands []apiclient.LavaflowRepositoryCheckCommand) {
	for _, command := range commands {
		head := lavaflowCheckCommandStatus(command.Status)
		if command.ExitCode != nil {
			head += fmt.Sprintf(", exit code %d", *command.ExitCode)
		}
		fmt.Fprintf(w, "  - %s: %s\n", head, clean(command.Command))
		if command.Output == nil {
			continue
		}
		output := strings.TrimRight(*command.Output, "\n")
		if strings.TrimSpace(output) == "" {
			fmt.Fprintf(w, "    %s %s\n", theme.Dim("Output:", on), theme.Dim("(empty)", on))
			continue
		}
		fmt.Fprintf(w, "    %s\n", theme.Dim("Output:", on))
		for line := range strings.SplitSeq(output, "\n") {
			fmt.Fprintf(w, "      %s\n", clean(line))
		}
	}
}

// lavaflowCheckCommandStatus says how a check command ended. The API asks
// clients to read a status they don't know as failed.
func lavaflowCheckCommandStatus(status string) string {
	switch status {
	case "passed":
		return "passed"
	case "timed_out":
		return "timed out"
	default:
		return "failed"
	}
}

// lavaflowSourceCommit is the commit a run read the code at, short, with
// where it came from, or "" when the run read none.
func lavaflowSourceCommit(commit *apiclient.LavaflowSourceCommit) string {
	if commit == nil || commit.Sha == "" {
		return ""
	}
	sha := commit.Sha
	if len(sha) > 7 {
		sha = sha[:7]
	}
	text := "commit " + clean(sha)
	if commit.Origin == nil {
		return text
	}
	if origin := lavaflowCommitOrigin(*commit.Origin); origin != "" {
		text += " (" + origin + ")"
	}
	return text
}

// lavaflowCommitOrigin says where a run's commit came from, in the words the
// dashboard uses too, or "" for an origin the CLI doesn't know.
func lavaflowCommitOrigin(origin apiclient.LavaflowCommitOrigin) string {
	switch origin {
	case apiclient.LavaflowCommitOriginPush:
		return "deployed from a push"
	case apiclient.LavaflowCommitOriginClientReported:
		return "reported by the deploy; deployed files match"
	case apiclient.LavaflowCommitOriginClientReportedRepository:
		return "reported by the deploy; found on the production branch"
	case apiclient.LavaflowCommitOriginRepository:
		return "production branch; may not be deployed"
	}
	return ""
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
// A bidirectional embedding, override or isolate prints as \u202e and the
// like, so the text shows in the order it was written.
// A newline is escaped too; block splits text into lines before this.
func clean(text string) string {
	var b strings.Builder
	for _, r := range text {
		switch {
		case r == '\t':
			b.WriteRune(r)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, "\\x%02x", r)
		case r >= 0x80 && r <= 0x9f,
			r >= 0x202a && r <= 0x202e,
			r >= 0x2066 && r <= 0x2069:
			fmt.Fprintf(&b, "\\u%04x", r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// lavaflowUngatedWorkflows says whether LavaFlow opens fixes past ungated
// workflows, and what that means. Allowing them applies only to the
// repository connected when a person allowed them: connecting another
// repository, or disconnecting it, turns them off.
func lavaflowUngatedWorkflows(allowed bool) string {
	if allowed {
		return "allowed for the connected repository: LavaFlow opens a fix even when a workflow could run it " +
			"with the repository's secrets before a person approves it"
	}
	return "not allowed: LavaFlow holds a fix that a workflow could run with the repository's secrets before " +
		"a person approves it"
}

// lavaflowAllowedWorkflow names an ungated workflow job a pull request opened
// past: its file, the branch or commit whose workflows run it, and the job
// and the event that runs it, which are absent for a file that couldn't be
// read.
func lavaflowAllowedWorkflow(workflow apiclient.LavaflowAllowedWorkflow) string {
	line := clean(workflow.Path) + " on " + clean(workflow.Source)
	switch {
	case workflow.Job != nil && workflow.Trigger != nil:
		return line + ": job " + clean(*workflow.Job) + ", runs on " + clean(*workflow.Trigger)
	case workflow.Job != nil:
		return line + ": job " + clean(*workflow.Job)
	case workflow.Trigger != nil:
		return line + ": runs on " + clean(*workflow.Trigger)
	default:
		return line + ": couldn't be read"
	}
}

func setOrNot(text string) string {
	if strings.TrimSpace(text) == "" {
		return "not set"
	}
	return fmt.Sprintf("set (%d characters)", len([]rune(text)))
}

// lavaflowMergeAvailability says whether LavaFlow can merge its fixes. The
// API gives no reason when it can't. Available merging stays off at an
// autonomy below merge. While LavaFlow is on and available at autonomy
// merge, merging was on and is paused: a waiting fix merges once merging is
// back, and a new one opens for review. LavaFlow off or unavailable opens no
// new fix, so merging is just not available.
func lavaflowMergeAvailability(mergeAvailable bool, settings *apiclient.LavaflowSettings, available *bool) string {
	switch {
	case mergeAvailable && (settings.Autonomy == apiclient.OpenPr || settings.Autonomy == apiclient.DiagnoseOnly):
		return "available for this project, but off at this autonomy, which never merges; " +
			"merging also needs the base branch to require a check"
	case mergeAvailable:
		// The API doesn't say whether the base branch requires a check, and
		// without one LavaFlow holds every fix with no_required_checks.
		return "available; LavaFlow holds every fix unless the base branch requires a check"
	case settings.Enabled && settings.Autonomy == apiclient.Merge && available != nil && *available:
		return "paused; waiting fixes resume when it's back, new fixes open as pull requests for review"
	default:
		return "not available for this project"
	}
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

// lavaflowFix prints the Fix block, always: the run's pull request, its
// state, where its merge is and when the merged fix went live, or why the
// run has no pull request and what to do next.
func lavaflowFix(w io.Writer, on bool, run *apiclient.LavaflowRun, commands LavaflowRunCommands) {
	fmt.Fprintln(w, theme.Dim("Fix:", on))
	pr := run.PullRequest
	if pr == nil {
		fmt.Fprintf(w, "  %s\n", lavaflowNoFix(run))
		lavaflowNoFixNext(w, on, run, commands)
		return
	}
	fixKV(w, on, "Pull request", "#%d %s", pr.Number, clean(pr.Url))
	fixKV(w, on, "State", "%s", lavaflowPullRequestState(pr, run.Review))
	if pr.OpenedAt != nil {
		fixKV(w, on, "Opened", "%s", formatLavaflowTime(*pr.OpenedAt))
	}
	if run.Merge != nil {
		lavaflowMergeLines(w, on, run.Merge, pr)
	}
	if pr.MergedAt != nil {
		fixKV(w, on, "Merged", "%s", formatLavaflowTime(*pr.MergedAt))
	}
	// Null until LavaFlow finds the merged fix live, and for one that didn't
	// merge.
	if pr.DeployedAt != nil {
		fixKV(w, on, "Deployed", "%s%s", formatLavaflowTime(*pr.DeployedAt), lavaflowIssueSince(run.Issue, *pr.DeployedAt))
	}
}

// lavaflowIssueSince says where the run's issue stands now that its fix is
// live: resolved, or open again because the error came back after the
// deploy. It says nothing when the API sends no issue or neither holds.
func lavaflowIssueSince(issue *apiclient.LavaflowIssueSummary, deployedAt time.Time) string {
	switch {
	case issue == nil:
		return ""
	case issue.Status == apiclient.IssueStatusResolved:
		return "; the issue is resolved"
	case issue.Status == apiclient.IssueStatusOpen && issue.RegressedAt != nil && issue.RegressedAt.After(deployedAt):
		return "; the error came back at " + formatLavaflowTime(*issue.RegressedAt) + ", so the issue is open again"
	default:
		return ""
	}
}

// fixKV writes a "Label: value" line inside the Fix block.
func fixKV(w io.Writer, on bool, label, format string, args ...any) {
	fmt.Fprintf(w, "  %s %s\n", theme.Dim(label+":", on), fmt.Sprintf(format, args...))
}

// lavaflowPullRequestState is open, merged or closed, and draft for an open
// pull request GitHub last reported as a draft, with why when the API says.
// A draft LavaFlow leaves for a person to mark ready reads as one its review
// didn't pass when the review above says so.
func lavaflowPullRequestState(pr *apiclient.LavaflowPullRequest, review *apiclient.LavaflowReview) string {
	if pr.State != apiclient.LavaflowPullRequestStateOpen || pr.Draft == nil || !*pr.Draft {
		return clean(string(pr.State))
	}
	if pr.DraftReason == nil {
		return "draft"
	}
	reason := *pr.DraftReason
	if reason == apiclient.LavaflowDraftReasonOpenPr && !lavaflowReviewPassed(review) {
		reason = apiclient.LavaflowDraftReasonReviewUnresolved
	}
	return "draft (" + lavaflowDraftReason(reason) + ")"
}

// lavaflowReviewPassed is whether the run's review passed its fix, and true
// when no review ran, which leaves nothing to contradict.
func lavaflowReviewPassed(review *apiclient.LavaflowReview) bool {
	return review == nil || (review.Stopped == nil && review.Verdict == apiclient.LavaflowReviewVerdictPassed)
}

func lavaflowDraftReason(reason apiclient.LavaflowDraftReason) string {
	switch reason {
	case apiclient.LavaflowDraftReasonOpenPr:
		return "LavaFlow doesn't merge it; review it and mark it ready"
	case apiclient.LavaflowDraftReasonReviewUnresolved:
		return "LavaFlow's review didn't pass the fix, so LavaFlow won't merge it; decide whether to finish it"
	case apiclient.LavaflowDraftReasonConvertedToDraft:
		return "someone made it a draft after LavaFlow opened it"
	default:
		return clean(strings.ReplaceAll(string(reason), "_", " "))
	}
}

// lavaflowNoFix says why a run has no pull request, and what to do next
// that needs no command. The API's reason wins; without one, the run's
// status says it. A completed run without a reason may have stopped short for
// any reason, so it claims none. lavaflowNoFixNext gives the steps that name
// a command.
func lavaflowNoFix(run *apiclient.LavaflowRun) string {
	readDiagnosis := ""
	fromDiagnosis := ""
	if lavaflowDiagnosisShown(run.Diagnosis) {
		readDiagnosis = " Read the diagnosis below."
		fromDiagnosis = " from the diagnosis below"
	}
	// The diff a run kept its fix in, when show prints it.
	fromSaved := fromDiagnosis
	if run.Diff != nil && strings.TrimSpace(*run.Diff) != "" {
		fromSaved = " from the diff below"
	}
	none := "No fix pull request was opened"
	if run.PullRequestSkipped != nil {
		switch reason := *run.PullRequestSkipped; reason {
		case apiclient.LavaflowSkipReasonNotFixable:
			return none + ": the diagnosis found no change in the code that fixes the error, for example because the cause is outside it." +
				readDiagnosis + " If the repository already has the fix, redeploy it."
		case apiclient.LavaflowSkipReasonLowConfidence:
			return none + ": the diagnosis isn't confident enough to propose a fix." + readDiagnosis
		case apiclient.LavaflowSkipReasonNoPatch:
			if readDiagnosis == "" {
				return none + ": LavaFlow found no change it could make."
			}
			return none + ": LavaFlow found no change it could make. Make the change the diagnosis below describes yourself."
		case apiclient.LavaflowSkipReasonPossibleCredential:
			// Every file of such a fix is withheld, so there is no diff to apply.
			return none + ": the fix or its description held what looks like a credential or a project secret, so LavaFlow pushed nothing. Write the fix yourself" + fromDiagnosis + "."
		case apiclient.LavaflowSkipReasonPossiblePersonalData:
			return none + ": the fix adds an email address the repository lacks, and the repository isn't private, so LavaFlow pushed nothing. Apply the fix yourself."
		case apiclient.LavaflowSkipReasonUngatedWorkflows:
			return lavaflowHeldForWorkflows(run.HeldWorkflows)
		case apiclient.LavaflowSkipReasonConventionUnsatisfiable:
			return none + ": LavaFlow would merge the fix, but no pull request title can meet the repository's commit convention, so it kept the fix on this run. Apply the fix yourself" + fromSaved + ", or relax the convention."
		case apiclient.LavaflowSkipReasonPullRequestOpen:
			return none + ": another LavaFlow pull request for this issue was open when this run finished. Review or close it."
		case apiclient.LavaflowSkipReasonSourceUnavailable:
			return none + ": LavaFlow couldn't read the repository at the deployed commit. Check the project's repository connection and that the commit exists."
		case apiclient.LavaflowSkipReasonPermissionMissing:
			return none + ": the Volcano GitHub App, or the project owner's GitHub account, lacks a permission LavaFlow needs. Grant the App's requested permissions, and make sure the owner can push to the repository."
		case apiclient.LavaflowSkipReasonRepositoryChanged:
			return none + ": the project no longer deploys from the repository the run read. A new run uses the current one."
		case apiclient.LavaflowSkipReasonPublishFailed:
			return none + ": opening the pull request failed. A new run tries again."
		case apiclient.LavaflowSkipReasonLineageCap:
			return none + ": this run follows up earlier LavaFlow fixes, which together opened as many pull requests as LavaFlow allows. Review those pull requests, and write the rest of the fix yourself" + fromDiagnosis + "."
		case apiclient.LavaflowSkipReasonLineageHeld:
			return none + ": an earlier LavaFlow pull request this fix follows up was held, stopped or closed, so LavaFlow opens no more follow-ups to it. Apply the fix yourself" + fromSaved + "."
		case apiclient.LavaflowSkipReasonReviewUnresolved:
			return none + ": LavaFlow's review didn't pass this Code Quality fix." + readDiagnosis
		case apiclient.LavaflowSkipReasonDuplicateInRepository:
			return none + ": another project on this repository has the open Code Quality pull request for the same finding. Review that pull request."
		case apiclient.LavaflowSkipReasonSecurityFinding:
			return none + ": the finding has security impact and the repository isn't private, so a pull request would disclose it. Fix it yourself" + fromDiagnosis + "."
		case apiclient.LavaflowSkipReasonDiagnoseOnly:
			return "Diagnosis only: the project's autonomy was set to diagnose only, so LavaFlow opened no fix." + readDiagnosis
		default:
			return none + ", for a reason this version of the CLI doesn't know: " + clean(string(reason)) + "."
		}
	}
	switch run.Status {
	case apiclient.LavaflowRunStatusQueued, apiclient.LavaflowRunStatusDiagnosing,
		apiclient.LavaflowRunStatusFixing, apiclient.LavaflowRunStatusReviewing:
		return "No fix yet: LavaFlow is still working on this run."
	case apiclient.LavaflowRunStatusCompleted:
		return none + ". LavaFlow didn't record why." + readDiagnosis
	case apiclient.LavaflowRunStatusFailed:
		return none + ": the run failed." + lavaflowFailureSentence(run.Failure)
	case apiclient.LavaflowRunStatusCancelled:
		return none + ": the run was cancelled." + lavaflowFailureSentence(run.Failure)
	default:
		return none + "."
	}
}

// lavaflowFailureSentence is the API's words for why a run failed or was
// cancelled, as a sentence after the Fix line's, or "" without them.
func lavaflowFailureSentence(failure *apiclient.LavaflowFailure) string {
	if failure == nil || strings.TrimSpace(failure.Message) == "" {
		return ""
	}
	return " " + clean(strings.TrimSpace(failure.Message))
}

// lavaflowFailureInFix is whether the Fix block says the run's failure: a
// run that failed or was cancelled without a pull request or a reason.
func lavaflowFailureInFix(run *apiclient.LavaflowRun) bool {
	return run.PullRequest == nil && run.PullRequestSkipped == nil &&
		(run.Status == apiclient.LavaflowRunStatusFailed || run.Status == apiclient.LavaflowRunStatusCancelled)
}

// lavaflowHeldForWorkflows says LavaFlow held a fix because workflows could
// run it with the repository's secrets before a person approves it. With
// the jobs the API lists, it leads into them; without them, it says what
// LavaFlow knows.
func lavaflowHeldForWorkflows(held *apiclient.LavaflowHeldWorkflows) string {
	none := "No fix pull request was opened: "
	switch {
	case held == nil:
		return none + "a workflow could run the fix with the repository's secrets before a person approves it."
	case len(held.Jobs) == 0:
		return none + "a workflow could run the fix with the repository's secrets before a person approves it. " +
			"When LavaFlow read the workflows again, it found no such job, so they may have changed since."
	default:
		return none + "these workflow jobs could run the fix with the repository's secrets before a person approves it:"
	}
}

// lavaflowNoFixNext writes what follows the Fix line of a run with no pull
// request: the jobs that held its fix, and the steps that name a command.
func lavaflowNoFixNext(w io.Writer, on bool, run *apiclient.LavaflowRun, commands LavaflowRunCommands) {
	startRun := theme.Command(commands.StartRun, on)
	if run.PullRequestSkipped != nil {
		switch *run.PullRequestSkipped {
		case apiclient.LavaflowSkipReasonUngatedWorkflows:
			if held := run.HeldWorkflows; held != nil {
				for _, job := range held.Jobs {
					fmt.Fprintf(w, "    - %s\n", lavaflowAllowedWorkflow(job))
				}
				if len(held.Jobs) == 0 {
					fmt.Fprintf(w, "  Start a new run with %s.\n", startRun)
					return
				}
			}
			yourself := "Or apply the fix yourself."
			if run.Diff != nil && strings.TrimSpace(*run.Diff) != "" {
				yourself = "Or apply the fix yourself from the diff below."
			}
			fmt.Fprintf(w, "  Gate those workflows on a label a person adds, or have the project's owner allow ungated workflows with %s; then start a new run with %s. %s\n",
				theme.Command(commands.SettingsSet+" --allow-ungated-workflows true", on), startRun, yourself)
		case apiclient.LavaflowSkipReasonPullRequestOpen:
			fmt.Fprintf(w, "  See the open ones with %s. If that pull request closed without merging, start a new run with %s.\n",
				theme.Command(commands.List+" --status pr_open", on), startRun)
		case apiclient.LavaflowSkipReasonDiagnoseOnly:
			fmt.Fprintf(w, "  To have later runs open fixes, run %s.\n", theme.Command(commands.SettingsSet+" --autonomy open_pr", on))
		}
		return
	}
	// A run cancelled because its issue is gone can't be started again.
	if run.Status == apiclient.LavaflowRunStatusCancelled && (run.Failure == nil || run.Failure.Code != "issue_not_found") {
		fmt.Fprintf(w, "  To try again, start a new run with %s.\n", startRun)
	}
}

// lavaflowDiagnosisShown is whether show prints a diagnosis for a run to
// point to.
func lavaflowDiagnosisShown(diagnosis *apiclient.LavaflowDiagnosis) bool {
	return diagnosis != nil && (diagnosis.Summary != "" || diagnosis.RootCause != "" || len(diagnosis.Evidence) > 0)
}

// lavaflowMergeLines says where a run's merge is, who merged a merged fix,
// and why LavaFlow held or stopped it, in plain words. Both are final: the
// pull request is then the person's, and while it is still open the block
// ends with what they do next. A status or reason the CLI doesn't know
// prints as the API gives it.
func lavaflowMergeLines(w io.Writer, on bool, merge *apiclient.LavaflowMerge, pr *apiclient.LavaflowPullRequest) {
	status := lavaflowMergeStatus(merge.Status)
	// An API older than merged_by_lavaflow leaves it out, so a merged fix
	// then says only that it merged.
	if merge.Status == apiclient.LavaflowMergeStatusMerged && merge.MergedByLavaflow != nil {
		if *merge.MergedByLavaflow {
			status = "merged by LavaFlow"
		} else {
			status = "merged by someone else"
		}
	}
	reason := ""
	if merge.Reason != nil {
		reason = *merge.Reason
	}
	cause, next := lavaflowMergeReason(reason)
	switch {
	case reason == "":
	case reason == "paused":
		// A paused merge keeps the status it had, which no longer says what
		// it waits for: its checks may have finished since.
		status = cause
	case cause == "":
		status += " (" + clean(reason) + ")"
	case merge.Status == apiclient.LavaflowMergeStatusMerged:
		status += "; " + cause
	default:
		status += ": " + cause
	}
	// LavaFlow never merges a fix it held or stopped, so a person ended it.
	if merge.Status == apiclient.LavaflowMergeStatusHeld || merge.Status == apiclient.LavaflowMergeStatusStopped {
		switch pr.State {
		case apiclient.LavaflowPullRequestStateMerged:
			status += ", then someone else merged it"
		case apiclient.LavaflowPullRequestStateClosed:
			status += ", then someone closed it"
		}
	}
	fixKV(w, on, "Merge", "%s", status)
	for _, reason := range merge.HoldReasons {
		fmt.Fprintf(w, "    - %s\n", lavaflowHoldReason(reason))
	}
	if !lavaflowMergeLeftToYou(merge.Status, pr) {
		return
	}
	if next == "" {
		fmt.Fprintln(w, "  The pull request is now yours to review and merge on GitHub.")
		return
	}
	fmt.Fprintf(w, "  The pull request is now yours: %s.\n", next)
}

// lavaflowMergeLeftToYou says whether LavaFlow left the pull request to
// people for good and it is still open: once a person merges or closes it,
// there is nothing left for them to merge.
func lavaflowMergeLeftToYou(status apiclient.LavaflowMergeStatus, pr *apiclient.LavaflowPullRequest) bool {
	if pr == nil || pr.State != apiclient.LavaflowPullRequestStateOpen {
		return false
	}
	return status == apiclient.LavaflowMergeStatusHeld || status == apiclient.LavaflowMergeStatusStopped
}

// lavaflowMergeReason says, for each reason the API documents, why a merge
// stopped or waits, and what the person does next with the open pull
// request when that differs from reviewing and merging it. Both are empty
// for a reason the CLI doesn't know.
func lavaflowMergeReason(reason string) (cause, next string) {
	const (
		thenMerge  = ", then merge it or close it on GitHub"
		followNote = "follow LavaFlow's comment on the pull request to merge it without skipping checks, or close it"
	)
	switch reason {
	case "paused":
		return "paused while merging is off; it resumes once merging is back", ""
	case "review_unresolved":
		return "LavaFlow's review didn't pass the fix", "decide whether to finish it" + thenMerge
	case "checks_failed":
		return "a check failed", "fix the failing check" + thenMerge
	case "checks_timed_out":
		return "the checks didn't finish in time", "rerun the checks" + thenMerge
	case "blocked_by_rules":
		return "the repository's rules block the merge", ""
	case "not_mergeable":
		return "GitHub can't merge the pull request, for example because it conflicts with its base branch",
			"resolve the conflict" + thenMerge
	case "branch_behind":
		return "the base branch requires the fix to be up to date with it, and LavaFlow doesn't update fix branches",
			"update the branch" + thenMerge
	case "removed_from_queue":
		return "the fix left the merge queue, and LavaFlow can't tell why, for example because its checks failed there or someone removed it", ""
	case "reads_failed":
		return "LavaFlow kept failing to read the pull request, or the project's settings or repository",
			"check whether it is still in the merge queue" + thenMerge
	case "handed_off":
		return "someone else took over the pull request, by drafting it, closing it or moving it to another branch", ""
	case "unreadable":
		return "LavaFlow couldn't read the pull request's state on GitHub", ""
	case "permission_missing":
		return "the Volcano GitHub App, or the project owner's GitHub account, lacks a permission the merge needs",
			"review it" + thenMerge + ". So later fixes can merge, grant the App's requested permissions and make sure the owner can push to the repository"
	case "repository_changed":
		return "the project now deploys from another repository", ""
	case "settings_changed":
		return "the project's settings no longer allow LavaFlow to merge", ""
	case "request_rejected":
		return "GitHub refused LavaFlow's request to start the fix's checks", ""
	case "ci_start_commit_mismatch":
		return "GitHub reports a different commit from the one LavaFlow made to start the fix's checks", ""
	case "branch_refused":
		return "LavaFlow starts checks only on its own branch of an open pull request it opened, never the default or production branch, so it didn't start them here", ""
	case "head_unverified":
		return "GitHub doesn't report the fix's head as the signed commit LavaFlow pushed, so LavaFlow didn't start its checks", ""
	case "checks_not_started":
		return "LavaFlow couldn't give the fix a commit message that runs CI", ""
	case "queue_carries_ci_skip":
		return "the repository's merge queue could carry an instruction to skip checks into the base branch", followNote
	case "merge_carries_ci_skip":
		return "merging would carry an instruction to skip checks into the base branch, for example because the repository allows no squash merge", followNote
	case "manual_deploy":
		return "waiting for you to deploy it", ""
	case "paused_too_long":
		return "merging stayed paused for more than 7 days", ""
	case "cancelled":
		return "the project is being deleted, so LavaFlow stopped waiting on the merge", ""
	default:
		return "", ""
	}
}

// lavaflowHoldReason says why LavaFlow holds a fix: the API's question, as
// written to show, then the paths it names unless the question already names
// each one. A reason without a question says what its code means.
func lavaflowHoldReason(reason apiclient.LavaflowHoldReason) string {
	line := clean(reason.Question)
	if strings.TrimSpace(reason.Question) == "" {
		line = lavaflowHoldCode(reason.Code)
	}
	if len(reason.Paths) == 0 {
		return line
	}
	for _, path := range reason.Paths {
		if !strings.Contains(reason.Question, path) {
			return line + " (" + cleanList(reason.Paths) + ")"
		}
	}
	return line
}

// lavaflowHoldCode says what a hold reason's code means, for a reason the
// API sent without a question. Other codes name a concern LavaFlow's
// reviewers raised, and print as the API gives them.
func lavaflowHoldCode(code string) string {
	switch code {
	case "client_reported_commit":
		return "The fix is built on a commit Volcano can't tie to a push."
	case "path":
		return "It changes a path that always needs a person."
	case "sensitive_path":
		return "It changes a path Volcano always holds for a person, such as authentication, secret, CI or dependency files."
	case "label":
		return "The pull request has the needs-human-review label."
	case "no_checks":
		return "No CI ran on it."
	case "required_check_skipped":
		return "A required check was skipped."
	case "no_required_checks":
		return "The base branch requires no check, so passing checks alone don't vouch for the fix."
	case "changes_requested":
		return "A reviewer requested changes."
	case "repository_requires_approval":
		return "The repository requires an approving review."
	case "person_commits":
		return "Someone other than LavaFlow pushed to the fix's branch."
	case "rules_bypass":
		return "The Volcano GitHub App can bypass a ruleset on the base branch."
	case "unchecked_rules":
		return "The base branch has a rule LavaFlow doesn't check, such as required signatures."
	case "commit_convention":
		return "No title can meet the repository's commit convention."
	case "ungated_workflows":
		return "A workflow could run the fix with the repository's secrets before a person approves it, or LavaFlow couldn't check its workflows."
	case "ci_outside_actions":
		return "The repository could run CI, a build or a preview outside GitHub Actions, so LavaFlow never starts this fix's CI."
	case "lineage_undoes_parent":
		return "The fix changes lines an earlier LavaFlow fix added, which could undo it."
	case "lineage_cap":
		return "This fix follows up earlier LavaFlow fixes, which together opened as many pull requests as LavaFlow allows."
	case "lineage_held":
		return "An earlier LavaFlow pull request this fix follows up was held, stopped or closed."
	case "recent_regressions":
		return "Fixes LavaFlow merged in the project recently caused new errors."
	default:
		return clean(strings.ReplaceAll(code, "_", " "))
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

// lavaflowRunLabel labels a run's status the same way in list and show. A
// run whose fix LavaFlow held or stopped says so while its pull request is
// open; it otherwise reads as its run status.
func lavaflowRunLabel(status apiclient.LavaflowRunStatus, merge *apiclient.LavaflowMerge, pr *apiclient.LavaflowPullRequest) string {
	switch {
	case merge == nil || !lavaflowMergeLeftToYou(merge.Status, pr):
		return lavaflowRunStatus(status)
	case merge.Status == apiclient.LavaflowMergeStatusHeld:
		return "Held: yours to merge"
	default:
		return "Stopped: yours to merge"
	}
}

// lavaflowRunStatus labels a run's status. The running stages say what the
// run is doing; a status the CLI doesn't know prints as the API gives it.
func lavaflowRunStatus(status apiclient.LavaflowRunStatus) string {
	switch status {
	case apiclient.LavaflowRunStatusQueued:
		return "Queued: waiting to start"
	case apiclient.LavaflowRunStatusDiagnosing:
		return "Running: finding the root cause"
	case apiclient.LavaflowRunStatusFixing:
		return "Running: writing the fix"
	case apiclient.LavaflowRunStatusReviewing:
		return "Running: reviewing the fix"
	case apiclient.LavaflowRunStatusPrOpen:
		return "PR open"
	case apiclient.LavaflowRunStatusCompleted:
		return "Finished"
	case apiclient.LavaflowRunStatusFailed:
		return "Failed"
	case apiclient.LavaflowRunStatusCancelled:
		return "Cancelled"
	default:
		return clean(string(status))
	}
}

// lavaflowRunOutcome says in a few words what came of a run: its pull
// request and that request's state, or why it opened none. A run still at
// work, or one that failed or was cancelled, has none, which its status says.
func lavaflowRunOutcome(run apiclient.LavaflowRunSummary) string {
	switch {
	case run.PullRequest != nil:
		return lavaflowPullRequestCell(run.PullRequest)
	case run.PullRequestSkipped != nil:
		return lavaflowSkipOutcome(*run.PullRequestSkipped)
	default:
		return "-"
	}
}

// lavaflowSkipOutcome is the list's short form of why a run opened no pull
// request; show says it in full. A reason the CLI doesn't know prints as the
// API gives it.
func lavaflowSkipOutcome(reason apiclient.LavaflowSkipReason) string {
	switch reason {
	case apiclient.LavaflowSkipReasonDiagnoseOnly:
		return "Diagnosed only"
	case apiclient.LavaflowSkipReasonNotFixable:
		return "No PR: no fix in the code"
	case apiclient.LavaflowSkipReasonLowConfidence:
		return "No PR: not confident"
	case apiclient.LavaflowSkipReasonNoPatch:
		return "No PR: no change found"
	case apiclient.LavaflowSkipReasonPossibleCredential:
		return "No PR: possible credential"
	case apiclient.LavaflowSkipReasonPossiblePersonalData:
		return "No PR: adds an email address"
	case apiclient.LavaflowSkipReasonUngatedWorkflows:
		return "No PR: ungated workflows"
	case apiclient.LavaflowSkipReasonConventionUnsatisfiable:
		return "No PR: commit convention"
	case apiclient.LavaflowSkipReasonPullRequestOpen:
		return "No PR: another was open"
	case apiclient.LavaflowSkipReasonSourceUnavailable:
		return "No PR: couldn't read source"
	case apiclient.LavaflowSkipReasonPermissionMissing:
		return "No PR: permission missing"
	case apiclient.LavaflowSkipReasonRepositoryChanged:
		return "No PR: repository changed"
	case apiclient.LavaflowSkipReasonPublishFailed:
		return "No PR: opening it failed"
	case apiclient.LavaflowSkipReasonLineageCap:
		return "No PR: follow-up limit"
	case apiclient.LavaflowSkipReasonLineageHeld:
		return "No PR: earlier fix held"
	case apiclient.LavaflowSkipReasonReviewUnresolved:
		return "No PR: review didn't pass"
	case apiclient.LavaflowSkipReasonDuplicateInRepository:
		return "No PR: another project has it"
	case apiclient.LavaflowSkipReasonSecurityFinding:
		return "No PR: security finding"
	default:
		return "No PR: " + clean(string(reason))
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

// lavaflowResourceName is a run's resource name, or says the run recorded
// none.
func lavaflowResourceName(name string) string {
	if strings.TrimSpace(name) == "" {
		return "(name not recorded)"
	}
	return clean(name)
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

// formatLavaflowTime prints a time in UTC, marked Z, as the API, a run's log
// lines and LavaFlow's pull requests give times.
func formatLavaflowTime(t time.Time) string {
	return t.UTC().Format("2006-01-02 15:04") + "Z"
}

func formatLavaflowTimePtr(t *time.Time, fallback string) string {
	if t == nil {
		return fallback
	}
	return formatLavaflowTime(*t)
}
