package output

import (
	"fmt"
	"io"
	"strings"

	"github.com/Kong/volcano-cli/internal/apiclient"
	"github.com/Kong/volcano-cli/internal/theme"
)

// LavaFlowStatusDetails is what lavaflow status renders.
type LavaFlowStatusDetails struct {
	Settings          *apiclient.LavaFlowSettings
	Repository        string
	RepositoryUnknown bool
	CostNote          string
	EnableCommand     string
	GitConnectCommand string
}

// LavaFlowStatus renders whether LavaFlow is on for a project, whether the
// plan allows it, and whether a repository is connected for fix pull requests.
func LavaFlowStatus(w io.Writer, details LavaFlowStatusDetails) {
	settings := details.Settings
	if settings == nil {
		return
	}
	on := theme.On(w)
	state := "off"
	if settings.Enabled {
		state = "on"
	}
	kv(w, on, "LavaFlow", "%s", state)
	if plan := settings.PlanAllowsLavaflow; plan != nil {
		if *plan {
			kv(w, on, "Plan", "allows LavaFlow")
		} else {
			kv(w, on, "Plan", "does not allow LavaFlow; it needs a paid plan")
		}
	}
	switch {
	case details.RepositoryUnknown:
		kv(w, on, "Repository", "%s", theme.Dim("unknown", on))
	case details.Repository != "":
		kv(w, on, "Repository", "%s", theme.StripControl(details.Repository))
	default:
		kv(w, on, "Repository", "not connected")
	}

	fmt.Fprintln(w)
	for _, limit := range lavaFlowLimits(settings) {
		Note(w, "%s", limit)
	}
	opensPullRequests := settings.MaxStage == apiclient.LavaFlowStagePullRequest
	if settings.Enabled && opensPullRequests && details.Repository == "" && !details.RepositoryUnknown {
		Warning(w, "Connect a repository so LavaFlow can open fix pull requests: %s",
			theme.Command(details.GitConnectCommand, on))
	}
	if settings.PlanAllowsLavaflow != nil && !*settings.PlanAllowsLavaflow {
		Warning(w, "No run starts until the project's plan allows LavaFlow. Upgrade the plan in the dashboard.")
	}
	fmt.Fprintln(w, theme.Dim(details.CostNote, on))
	if !settings.Enabled {
		fmt.Fprintf(w, "%s%s\n", theme.Dim("Turn it on: ", on), theme.Command(details.EnableCommand, on))
	}
}

// LavaFlowEnabled renders LavaFlow turned on: what it now does, which is
// onDescription unless the project configuration narrowed it, and what it
// costs.
func LavaFlowEnabled(w io.Writer, settings *apiclient.LavaFlowSettings, onDescription, costNote string) {
	Success(w, "LavaFlow is on")
	limits := lavaFlowLimits(settings)
	if len(limits) == 0 {
		limits = []string{onDescription}
	}
	for _, limit := range limits {
		Note(w, "%s", limit)
	}
	Note(w, "%s", costNote)
}

// lavaFlowLimits names the settings a project configuration file narrowed.
// LavaFlow on runs on its own and opens pull requests; anything less is
// worth saying, because on/off is all the command shows otherwise.
func lavaFlowLimits(settings *apiclient.LavaFlowSettings) []string {
	if !settings.Enabled {
		return nil
	}
	var limits []string
	if settings.MaxStage == apiclient.LavaFlowStageDiagnose {
		limits = append(limits, "LavaFlow diagnoses issues but opens no pull requests (max_stage: diagnose in the project configuration).")
	}
	if !settings.AutoTrigger {
		limits = append(limits, "LavaFlow runs only when a run is started by hand (auto_trigger: false in the project configuration).")
	}
	return limits
}

// LavaFlowRuns renders one page of runs. nextCommand is the list command
// with its flags, so the next-page hint pages the same set.
func LavaFlowRuns(w io.Writer, page *apiclient.PaginatedLavaFlowRuns, nextCommand string) {
	if page == nil {
		page = &apiclient.PaginatedLavaFlowRuns{}
	}
	on := theme.On(w)
	if len(page.Data) == 0 {
		fmt.Fprintln(w, "No LavaFlow runs")
		return
	}
	tableHead(w, on, false, 140, "%-36s  %-36s  %-10s  %-10s  %-26s  %-12s",
		"ID", "Issue", "Trigger", "Status", "Result", "Created")
	for i := range page.Data {
		run := &page.Data[i]
		fmt.Fprintf(w, "%-36s  %-36s  %-10s  %s  %-26s  %-12s\n",
			run.Id.String(),
			Truncate(lavaFlowIssue(run), 36),
			lavaFlowTrigger(run.Trigger),
			statusCell(string(run.Status), 10, on),
			Truncate(lavaFlowResult(run), 26),
			FormatTimeAgo(run.CreatedAt),
		)
	}
	if page.HasMore && page.NextCursor != nil {
		nextPage(w, on, fmt.Sprintf("%s --cursor %s", nextCommand, *page.NextCursor))
	}
}

// LavaFlowRun renders one run: where it is, what it found, and the pull
// request it opened. The diagnosis is the run's own text, so it is printed
// without control characters.
func LavaFlowRun(w io.Writer, run *apiclient.LavaFlowRun) {
	if run == nil {
		return
	}
	on := theme.On(w)
	kv(w, on, "ID", "%s", run.Id.String())
	kv(w, on, "Issue", "%s", lavaFlowIssueDetail(run))
	kv(w, on, "Trigger", "%s", lavaFlowTrigger(run.Trigger))
	kv(w, on, "Status", "%s", theme.Status(string(run.Status), on))
	if reason := lavaFlowFailure(run); reason != "" {
		kv(w, on, "Reason", "%s", reason)
	}
	kv(w, on, "Created", "%s", FormatTimestamp(run.CreatedAt))
	if run.StartedAt != nil {
		kv(w, on, "Started", "%s", FormatTimestamp(*run.StartedAt))
	}
	if run.CompletedAt != nil {
		kv(w, on, "Completed", "%s", FormatTimestamp(*run.CompletedAt))
	}
	if pr := run.PullRequest; pr != nil {
		kv(w, on, "Pull request", "%s (%s)", theme.StripControl(pr.Url), lavaFlowPullRequestState(pr))
	} else if run.PullRequestSkipped != nil {
		kv(w, on, "Pull request", "none: %s", lavaFlowSkipped(*run.PullRequestSkipped))
	}
	if run.CommitSha != nil {
		kv(w, on, "Commit", "%s", theme.StripControl(*run.CommitSha))
	}
	if diagnosis := run.Diagnosis; diagnosis != nil {
		lavaFlowDiagnosis(w, on, diagnosis)
	}
}

func lavaFlowDiagnosis(w io.Writer, on bool, diagnosis *apiclient.LavaFlowDiagnosis) {
	fmt.Fprintf(w, "\n%s\n", theme.Title("Diagnosis", on))
	kv(w, on, "Summary", "%s", theme.StripControl(diagnosis.Summary))
	kv(w, on, "Root cause", "%s", theme.StripControl(diagnosis.RootCause))
	kv(w, on, "Suggested fix", "%s", theme.StripControl(diagnosis.SuggestedFix))
	kv(w, on, "Confidence", "%s", diagnosis.Confidence)
	if diagnosis.Fixable {
		kv(w, on, "Fixable", "yes")
	} else {
		kv(w, on, "Fixable", "no, a code change would not fix it")
	}
	if len(diagnosis.Files) > 0 {
		files := make([]string, 0, len(diagnosis.Files))
		for _, file := range diagnosis.Files {
			files = append(files, theme.StripControl(file))
		}
		kv(w, on, "Files", "%s", strings.Join(files, ", "))
	}
	for _, evidence := range diagnosis.Evidence {
		where := ""
		if evidence.Location != nil && *evidence.Location != "" {
			where = " (" + theme.StripControl(*evidence.Location) + ")"
		}
		kv(w, on, "Evidence", "%s%s: %s", evidence.Kind, where, theme.StripControl(evidence.Detail))
	}
}

func lavaFlowIssue(run *apiclient.LavaFlowRun) string {
	if run.IssueTitle != nil && *run.IssueTitle != "" {
		return theme.StripControl(*run.IssueTitle)
	}
	return run.IssueId.String()
}

func lavaFlowIssueDetail(run *apiclient.LavaFlowRun) string {
	if run.IssueTitle != nil && *run.IssueTitle != "" {
		return fmt.Sprintf("%s (%s)", theme.StripControl(*run.IssueTitle), run.IssueId)
	}
	return run.IssueId.String()
}

func lavaFlowTrigger(trigger apiclient.LavaFlowRunTrigger) string {
	switch trigger {
	case apiclient.LavaFlowRunTriggerNewIssue:
		return "new issue"
	default:
		return string(trigger)
	}
}

// lavaFlowResult is a run's outcome in a few words: the pull request it
// opened, what it found, or why it stopped.
func lavaFlowResult(run *apiclient.LavaFlowRun) string {
	switch {
	case run.PullRequest != nil:
		return fmt.Sprintf("PR #%d %s", run.PullRequest.Number, lavaFlowPullRequestState(run.PullRequest))
	case run.Status == apiclient.LavaFlowRunStatusSucceeded && run.Diagnosis != nil:
		return "diagnosed"
	case run.FailureCode != nil:
		return lavaFlowFailure(run)
	default:
		return "-"
	}
}

func lavaFlowPullRequestState(pr *apiclient.LavaFlowPullRequest) string {
	if pr.State == apiclient.LavaFlowPullRequestStateMerged && pr.DeployedAt != nil {
		return "merged, deployed"
	}
	return string(pr.State)
}

// lavaFlowFailureText says why a run stopped, or why a queued run waits, in
// LavaFlow's words. The API's failure_message is not printed: --json carries
// it for anyone who needs the raw detail.
var lavaFlowFailureText = map[string]string{
	"feature_unavailable":    "waiting: LavaFlow is unavailable here; the run resumes when it returns",
	"feature_paused":         "waiting: Volcano paused LavaFlow runs; the run resumes when they restart",
	"model_request_rejected": "LavaFlow could not run the diagnosis",
	"model_unavailable":      "LavaFlow was busy; the run is retried",
	"model_refused":          "LavaFlow declined to diagnose this issue",
	"no_diagnosis":           "LavaFlow finished without a diagnosis",
	"budget_exhausted":       "the run reached its limit before a diagnosis",
	"timed_out":              "the run did not finish in time",
	"attempts_exhausted":     "the run failed on every attempt",
	"lavaflow_disabled":      "LavaFlow was turned off",
	"plan_required":          "the plan no longer allows LavaFlow",
	"issue_not_found":        "the issue no longer exists",
	"internal_error":         "Volcano could not run the diagnosis",
}

func lavaFlowFailure(run *apiclient.LavaFlowRun) string {
	if run.FailureCode == nil || *run.FailureCode == "" {
		return ""
	}
	if text, ok := lavaFlowFailureText[*run.FailureCode]; ok {
		return text
	}
	return theme.StripControl(*run.FailureCode)
}

var lavaFlowSkippedText = map[apiclient.LavaFlowPullRequestSkipped]string{
	apiclient.LavaFlowPullRequestSkippedSourceUnavailable: "the source of the deployed code is not available",
	apiclient.LavaFlowPullRequestSkippedPullRequestOpen:   "a LavaFlow pull request for this issue is already open",
	apiclient.LavaFlowPullRequestSkippedNotFixable:        "a code change would not fix it",
	apiclient.LavaFlowPullRequestSkippedLowConfidence:     "the diagnosis is not confident enough",
	apiclient.LavaFlowPullRequestSkippedNoPatch:           "LavaFlow found no fix",
	apiclient.LavaFlowPullRequestSkippedPermissionMissing: "the Volcano GitHub App cannot open pull requests in the repository",
	apiclient.LavaFlowPullRequestSkippedRepositoryChanged: "the connected repository changed during the run",
	apiclient.LavaFlowPullRequestSkippedPublishFailed:     "the pull request could not be opened",
}

func lavaFlowSkipped(reason apiclient.LavaFlowPullRequestSkipped) string {
	if text, ok := lavaFlowSkippedText[reason]; ok {
		return text
	}
	return theme.StripControl(string(reason))
}
