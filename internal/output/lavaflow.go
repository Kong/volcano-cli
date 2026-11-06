package output

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Kong/volcano-cli/internal/apiclient"
	clilavaflow "github.com/Kong/volcano-cli/internal/lavaflow"
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

// LavaflowRunStarted renders a run a person started: its ID and status, and
// the command that follows it.
func LavaflowRunStarted(w io.Writer, runID string, status apiclient.LavaflowRunStatus, showCommand string) {
	Success(w, "LavaFlow run started")
	on := theme.On(w)
	kv(w, on, "Run", "%s", clean(runID))
	kv(w, on, "Status", "%s", lavaflowRunStatus(status))
	fmt.Fprintf(w, "Follow it with %s\n", theme.Command(showCommand, on))
}

// LavaflowRun renders one run: what it worked on, the commit it read, its
// review, what happened to its fix, any failure in the API's own words, its
// diagnosis and the diff.
func LavaflowRun(w io.Writer, run *apiclient.LavaflowRun) {
	on := theme.On(w)
	kv(w, on, "Run", "%s", run.Id)
	kv(w, on, "Status", "%s", lavaflowRunStatus(run.Status))
	kv(w, on, "Resource", "%s %s", lavaflowResourceType(string(run.Resource.Type)), clean(run.Resource.Name))
	if run.Title != nil {
		kv(w, on, "Title", "%s", clean(*run.Title))
	}
	kv(w, on, "Issue", "%s", run.IssueId)
	if source := lavaflowSourceCommit(run.SourceCommit); source != "" {
		kv(w, on, "Source", "%s", source)
	}
	kv(w, on, "Started", "%s", formatLavaflowTime(run.CreatedAt))
	if run.FinishedAt != nil {
		kv(w, on, "Finished", "%s", formatLavaflowTime(*run.FinishedAt))
	}
	if run.Review != nil {
		kv(w, on, "Review", "%s after %d round(s)", clean(strings.ReplaceAll(string(run.Review.Verdict), "_", " ")), run.Review.Rounds)
	}
	// The API lists only the first ungated job on each branch or commit it
	// checked, so a count would understate the jobs that could run the change.
	// Null, no record of them, prints nothing, as none does.
	if workflows := run.UngatedWorkflows; workflows != nil && len(*workflows) > 0 {
		kv(w, on, "Ungated workflows", "the pull request opened past these, the first one on each branch or commit checked")
		for _, workflow := range *workflows {
			fmt.Fprintf(w, "  - %s\n", lavaflowAllowedWorkflow(workflow))
		}
	}
	lavaflowFix(w, on, run)
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

// lavaflowUngatedWorkflows says whether LavaFlow opens fixes past ungated
// workflows. Allowing them applies only to the repository connected when a
// person allowed them: connecting another repository, or disconnecting it,
// turns them off.
func lavaflowUngatedWorkflows(allowed bool) string {
	if allowed {
		return "allowed for the connected repository"
	}
	return "not allowed"
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
// API gives no reason when it can't. While LavaFlow is on and available at
// autonomy merge, merging was on and is paused: a waiting fix merges once
// merging is back, and a new one opens for review. LavaFlow off or
// unavailable opens no new fix, so merging is just not available.
func lavaflowMergeAvailability(mergeAvailable bool, settings *apiclient.LavaflowSettings, available *bool) string {
	switch {
	case mergeAvailable:
		return "available"
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
// state and where its merge is, or why the run has no pull request.
func lavaflowFix(w io.Writer, on bool, run *apiclient.LavaflowRun) {
	fmt.Fprintln(w, theme.Dim("Fix:", on))
	pr := run.PullRequest
	if pr == nil {
		fmt.Fprintf(w, "  %s\n", lavaflowNoFix(run))
		return
	}
	fixKV(w, on, "Pull request", "#%d %s", pr.Number, clean(pr.Url))
	fixKV(w, on, "State", "%s", lavaflowPullRequestState(pr))
	if pr.OpenedAt != nil {
		fixKV(w, on, "Opened", "%s", formatLavaflowTime(*pr.OpenedAt))
	}
	if run.Merge != nil {
		lavaflowMergeLines(w, on, run.Merge, pr)
	}
	// The API doesn't say yet whether a merged fix deployed. Print it here
	// once the pull request carries deployed_at.
}

// fixKV writes a "Label: value" line inside the Fix block.
func fixKV(w io.Writer, on bool, label, format string, args ...any) {
	fmt.Fprintf(w, "  %s %s\n", theme.Dim(label+":", on), fmt.Sprintf(format, args...))
}

// lavaflowPullRequestState is open, merged or closed, and draft for an open
// pull request GitHub last reported as a draft, with why when the API says.
func lavaflowPullRequestState(pr *apiclient.LavaflowPullRequest) string {
	if pr.State != apiclient.LavaflowPullRequestStateOpen || pr.Draft == nil || !*pr.Draft {
		return clean(string(pr.State))
	}
	if pr.DraftReason == nil {
		return "draft"
	}
	return "draft (" + lavaflowDraftReason(*pr.DraftReason) + ")"
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

// lavaflowSkipDiagnoseOnly is the code the API is to send for a run that
// wasn't set to open a pull request, named after the autonomy setting.
const lavaflowSkipDiagnoseOnly = "diagnose_only"

// lavaflowNoFix says why a run has no pull request, and what to do next. The
// API's reason wins; without one, the run's status says it. A completed run
// without a reason may have stopped short for any reason, so it claims none.
func lavaflowNoFix(run *apiclient.LavaflowRun) string {
	readDiagnosis := ""
	fromDiagnosis := ""
	if lavaflowDiagnosisShown(run.Diagnosis) {
		readDiagnosis = " Read the diagnosis below."
		fromDiagnosis = " from the diagnosis below"
	}
	none := "No fix pull request was opened"
	if run.PullRequestSkipped != nil {
		switch reason := *run.PullRequestSkipped; reason {
		case apiclient.LavaflowSkipReasonNotFixable:
			return none + ": the diagnosis found no change in the code that fixes the error, for example because the cause is outside it." + readDiagnosis
		case apiclient.LavaflowSkipReasonLowConfidence:
			return none + ": the diagnosis isn't confident enough to propose a fix." + readDiagnosis
		case apiclient.LavaflowSkipReasonNoPatch:
			return none + ": LavaFlow proposed no change." + readDiagnosis
		case apiclient.LavaflowSkipReasonPossibleCredential:
			return none + ": the fix or its description held what looks like a credential or a project secret, so LavaFlow pushed nothing. Apply the fix yourself" + fromDiagnosis + "."
		case apiclient.LavaflowSkipReasonPossiblePersonalData:
			return none + ": the fix adds an email address the repository lacks, and the repository isn't private, so LavaFlow pushed nothing. Apply the fix yourself."
		case apiclient.LavaflowSkipReasonUngatedWorkflows:
			return none + ": a workflow could run the fix with the repository's secrets before a person approves it. Gate that workflow on a label a person adds, or have the project's owner allow ungated workflows."
		case apiclient.LavaflowSkipReasonPullRequestOpen:
			return none + ": another LavaFlow pull request for this issue is open. Review or close it first."
		case apiclient.LavaflowSkipReasonSourceUnavailable:
			return none + ": LavaFlow couldn't read the repository at the deployed commit. Check the project's repository connection and that the commit exists."
		case apiclient.LavaflowSkipReasonPermissionMissing:
			return none + ": the Volcano GitHub App, or the project owner's GitHub account, lacks a permission LavaFlow needs. Grant the App's requested permissions, and make sure the owner can push to the repository."
		case apiclient.LavaflowSkipReasonRepositoryChanged:
			return none + ": the project no longer deploys from the repository the run read. A new run uses the current one."
		case apiclient.LavaflowSkipReasonPublishFailed:
			return none + ": opening the pull request failed. A new run tries again."
		case lavaflowSkipDiagnoseOnly:
			return "Diagnosis only: this run wasn't set to open a fix."
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
		return none + ": the run failed."
	case apiclient.LavaflowRunStatusCancelled:
		return none + ": the run was cancelled."
	default:
		return none + "."
	}
}

// lavaflowDiagnosisShown is whether show prints a diagnosis for a run to
// point to.
func lavaflowDiagnosisShown(diagnosis *apiclient.LavaflowDiagnosis) bool {
	return diagnosis != nil && (diagnosis.Summary != "" || diagnosis.RootCause != "" || len(diagnosis.Evidence) > 0)
}

// lavaflowMergeLines says where a run's merge is, who merged a merged fix,
// and why LavaFlow held or stopped it. Both are final: the pull request is
// then the person's, which is said only while it is still open. A status the
// CLI doesn't know prints as the API gives it.
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
	switch {
	case merge.Reason == nil || *merge.Reason == "":
	case *merge.Reason == "paused":
		// A paused merge keeps the status it had, which no longer says what
		// it waits for: its checks may have finished since.
		status = lavaflowMergeReason(*merge.Reason)
	default:
		status += " (" + lavaflowMergeReason(*merge.Reason) + ")"
	}
	fixKV(w, on, "Merge", "%s", status)
	for _, reason := range merge.HoldReasons {
		line := clean(reason.Question)
		if strings.TrimSpace(line) == "" {
			line = clean(strings.ReplaceAll(reason.Code, "_", " "))
		}
		if len(reason.Paths) > 0 {
			line += " (" + cleanList(reason.Paths) + ")"
		}
		fmt.Fprintf(w, "    - %s\n", line)
	}
	if lavaflowMergeLeftToYou(merge.Status, pr) {
		fmt.Fprintln(w, "  The pull request is now yours to review and merge on GitHub.")
	}
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

// lavaflowRunListStatus marks a run whose fix LavaFlow held or stopped, while
// its pull request is open; it otherwise reads as an open pull request.
func lavaflowRunListStatus(run apiclient.LavaflowRunSummary) string {
	switch {
	case run.Merge == nil || !lavaflowMergeLeftToYou(run.Merge.Status, run.PullRequest):
		return lavaflowRunStatus(run.Status)
	case run.Merge.Status == apiclient.LavaflowMergeStatusHeld:
		return "Held: yours to merge"
	default:
		return "Stopped: yours to merge"
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

// LavaflowHeldFix renders what a person reads before opening a fix LavaFlow
// held: the run, the diff exactly as Volcano served it, every workflow job
// that will get the repository's secrets, the two fingerprints opening sends,
// the branches and commits their workflows were read at, and last, what the
// diff leaves out. Volcano opens the fix only while both fingerprints match,
// so they are printed as given.
func LavaflowHeldFix(w io.Writer, held *clilavaflow.HeldFix) {
	on := theme.On(w)
	run := held.Run
	kv(w, on, "Run", "%s", run.Id)
	kv(w, on, "Resource", "%s %s", lavaflowResourceType(string(run.Resource.Type)), clean(run.Resource.Name))
	if run.Title != nil {
		kv(w, on, "Title", "%s", clean(*run.Title))
	}
	kv(w, on, "Issue", "%s", run.IssueId)
	kv(w, on, "Held", "a workflow could run this fix with the repository's secrets before a person approves it")

	diff := ""
	if run.Diff != nil {
		diff = *run.Diff
	}
	lavaflowServedDiff(w, on, diff)

	var jobs []apiclient.LavaflowAllowedWorkflow
	var readAt []apiclient.LavaflowWorkflowSource
	if run.HeldWorkflows != nil {
		jobs, readAt = run.HeldWorkflows.Jobs, run.HeldWorkflows.ReadAt
	}
	fmt.Fprintf(w, "\n%s\n", theme.Title(fmt.Sprintf("Jobs that will get the repository's secrets (%d)", len(jobs)), on))
	for _, job := range jobs {
		fmt.Fprintf(w, "  - %s\n", lavaflowAllowedWorkflow(job))
	}
	if len(readAt) > 0 {
		fmt.Fprintln(w, theme.Dim("Workflows read at:", on))
		for _, source := range readAt {
			fmt.Fprintf(w, "  - %s\n", lavaflowWorkflowSource(source))
		}
	}

	fmt.Fprintln(w)
	kv(w, on, "Fix fingerprint", "%s", clean(held.DiffSHA256))
	kv(w, on, "Jobs fingerprint", "%s", clean(held.JobsSHA256))

	if held.Reviewable() {
		return
	}
	fmt.Fprintln(w)
	if len(held.Withheld) > 0 {
		if len(held.Withheld) == 1 {
			Warning(w, "1 file withheld: it may contain credentials or is too large to show. You can't review it here.")
		} else {
			Warning(w, "%d files withheld: they may contain credentials or are too large to show. "+
				"You can't review them here.", len(held.Withheld))
		}
		for _, path := range held.Withheld {
			fmt.Fprintf(w, "%s %s\n", theme.Fail("!!", on), lavaflowWithheldNote(path))
		}
	}
	if held.Cut {
		Warning(w, "The diff is cut short past 256 KiB, so it doesn't show the whole fix.")
	}
}

// lavaflowWithheldNote says why a withheld file isn't shown, in the words the
// web uses. A marker without a path is for a file whose path may itself hold
// a credential, or whose stored diff LavaFlow couldn't read back; either way
// no path is printed.
func lavaflowWithheldNote(path string) string {
	if strings.TrimSpace(path) == "" {
		return "a file whose path LavaFlow can't show, or couldn't read back"
	}
	return clean(path) + " withheld: it may contain credentials or is too large to show."
}

// lavaflowServedDiff prints a held fix's diff line by line as Volcano served
// it, escaping control characters as clean does. A line Volcano writes in
// place of a withheld file, or at a cut, prints with !! in the gutter.
func lavaflowServedDiff(w io.Writer, on bool, diff string) {
	fmt.Fprintf(w, "\n%s\n", theme.Title("Diff", on))
	if strings.TrimSpace(diff) == "" {
		fmt.Fprintln(w, theme.Dim("  (no changes shown)", on))
		return
	}
	for line := range strings.SplitSeq(strings.TrimRight(diff, "\n"), "\n") {
		if clilavaflow.WithheldLine(line) || clilavaflow.CutLine(line) {
			fmt.Fprintf(w, "%s %s\n", theme.Fail("!!", on), theme.Error(clean(line), on))
			continue
		}
		fmt.Fprintf(w, "  %s\n", clean(line))
	}
}

// lavaflowWorkflowSource names a branch and the commit its workflows were
// read at, or the commit alone when LavaFlow read a commit.
func lavaflowWorkflowSource(source apiclient.LavaflowWorkflowSource) string {
	if source.Source == source.CommitSha {
		return "commit " + clean(source.CommitSha)
	}
	return clean(source.Source) + " at " + clean(source.CommitSha)
}

// LavaflowOpenHint names the command that shows a held fix and opens its pull
// request, under a run LavaFlow held for its workflows.
func LavaflowOpenHint(w io.Writer, command string) {
	on := theme.On(w)
	fmt.Fprintf(w, "\n%s %s\n", theme.Dim("To read this fix and open its pull request yourself, run", on), theme.Command(command, on))
}

// LavaflowHeldFixQueued says Volcano took the open: the run is queued and
// opens the fix's pull request shortly.
func LavaflowHeldFixQueued(w io.Writer, runID, showCommand string) {
	Success(w, "LavaFlow will open the fix's pull request shortly. Run %s is queued; follow it with '%s'", runID, showCommand)
}
