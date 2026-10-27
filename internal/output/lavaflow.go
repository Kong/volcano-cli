package output

import (
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/Kong/volcano-cli/internal/apiclient"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
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
	kv(w, on, "LavaFlow", "%s", lavaFlowState(details))
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
	paused := lavaFlowPaused(settings)

	fmt.Fprintln(w)
	for _, limit := range lavaFlowLimits(settings) {
		if !settings.Enabled {
			limit = "When it is on, " + limit
		}
		Note(w, "%s", limit)
	}
	opensPullRequests := settings.MaxStage == apiclient.LavaFlowStagePullRequest
	if paused {
		Warning(w, "%s", lavaFlowPausedReason(details))
	}
	if settings.Enabled && opensPullRequests && details.Repository == "" && !details.RepositoryUnknown && !paused {
		Warning(w, "Connect a repository so LavaFlow can open fix pull requests: %s",
			theme.Command(details.GitConnectCommand, on))
	}
	if settings.PlanAllowsLavaflow != nil && !*settings.PlanAllowsLavaflow {
		Warning(w, "No run starts until the project's plan allows LavaFlow. Upgrade the plan in the dashboard.")
	}
	fmt.Fprintln(w, theme.Dim(details.CostNote, on))
	if !settings.Enabled {
		// The plan refuses turning it on, so the hint follows the upgrade.
		label := "Turn it on: "
		if settings.PlanAllowsLavaflow != nil && !*settings.PlanAllowsLavaflow {
			label = "After upgrading, turn it on: "
		}
		fmt.Fprintf(w, "%s%s\n", theme.Dim(label, on), theme.Command(details.EnableCommand, on))
	}
	if paused {
		lavaFlowResume(w, details, on)
	}
}

// lavaFlowPaused reports whether LavaFlow is on but paused because the
// project points at another repository than the one it was turned on for, or
// at none.
func lavaFlowPaused(settings *apiclient.LavaFlowSettings) bool {
	return settings.Enabled && settings.RepositoryChanged != nil && *settings.RepositoryChanged
}

// lavaFlowPausedReason says which repository changed and that no run starts
// until LavaFlow is turned on again.
func lavaFlowPausedReason(details LavaFlowStatusDetails) string {
	const until = "so no run starts until someone turns LavaFlow on again."
	enabled := details.Settings.EnabledRepository
	switch {
	case enabled == nil:
		return "A repository was connected after LavaFlow was turned on, " + until
	case details.Repository == "" && !details.RepositoryUnknown:
		return fmt.Sprintf("%s is no longer connected to the project, %s", theme.StripControl(enabled.FullName), until)
	default:
		return fmt.Sprintf("The project's repository changed from %s since LavaFlow was turned on, %s",
			theme.StripControl(enabled.FullName), until)
	}
}

// lavaFlowResume names the command that resumes LavaFlow: turning it on again
// records the repository the project points at now. With none connected, it
// resumes without a repository, or after connecting one.
func lavaFlowResume(w io.Writer, details LavaFlowStatusDetails, on bool) {
	label := "Resume LavaFlow: "
	switch {
	case details.RepositoryUnknown:
	case details.Repository == "":
		label = "Resume LavaFlow without a repository: "
	default:
		label = fmt.Sprintf("Resume LavaFlow with %s: ", theme.StripControl(details.Repository))
	}
	fmt.Fprintf(w, "%s%s\n", theme.Dim(label, on), theme.Command(details.EnableCommand, on))
	if details.Repository == "" && !details.RepositoryUnknown {
		fmt.Fprintf(w, "%s%s\n", theme.Dim("Or connect a repository first: ", on), theme.Command(details.GitConnectCommand, on))
	}
}

// LavaFlowEnabledDetails is what lavaflow enable renders.
type LavaFlowEnabledDetails struct {
	Settings          *apiclient.LavaFlowSettings
	Repository        string
	RepositoryUnknown bool
	// Description says what LavaFlow does, and that it uses AI.
	Description       string
	CostNote          string
	GitConnectCommand string
}

// LavaFlowEnabled renders LavaFlow turned on: what it does, what it does
// next, in one line, and what it costs. Settings already on stay on after the plan stops
// allowing LavaFlow, and saving them again succeeds, so that case says no run
// starts instead.
func LavaFlowEnabled(w io.Writer, details LavaFlowEnabledDetails) {
	settings := details.Settings
	Success(w, "LavaFlow is on")
	Note(w, "%s", details.Description)
	if settings.PlanAllowsLavaflow != nil && !*settings.PlanAllowsLavaflow {
		Warning(w, "No run starts until the project's plan allows LavaFlow. LavaFlow needs a paid plan: "+
			"upgrade the plan in the dashboard.")
		Note(w, "%s", details.CostNote)
		return
	}
	Note(w, "%s", lavaFlowWhatHappensNext(details, theme.On(w)))
	Note(w, "%s", details.CostNote)
}

// lavaFlowWhatHappensNext says what LavaFlow does with the next issue: which
// issues it diagnoses, and where its fix pull request goes. Phase 1 opens
// pull requests for a person to review; nothing here may promise more.
func lavaFlowWhatHappensNext(details LavaFlowEnabledDetails, on bool) string {
	settings := details.Settings
	issue := "the next issue that happens a second time or comes back after you resolve it"
	if !settings.AutoTrigger {
		issue = "each issue you start a run for (auto_trigger: false in the project configuration)"
	}
	switch {
	case settings.MaxStage == apiclient.LavaFlowStageDiagnose:
		return fmt.Sprintf("LavaFlow diagnoses %s, and opens no pull request (max_stage: diagnose in the project configuration).", issue)
	case details.RepositoryUnknown:
		return fmt.Sprintf("LavaFlow diagnoses %s, and opens a fix pull request in your connected repository.", issue)
	case details.Repository == "":
		return fmt.Sprintf("LavaFlow diagnoses %s. It only diagnoses until a repository is connected: %s",
			issue, theme.Command(details.GitConnectCommand, on))
	default:
		return fmt.Sprintf("LavaFlow diagnoses %s, and opens a fix pull request in %s.", issue, theme.StripControl(details.Repository))
	}
}

// lavaFlowState is the status's first row: off, or on and what LavaFlow does
// with an issue, so the state reads at a glance.
func lavaFlowState(details LavaFlowStatusDetails) string {
	settings := details.Settings
	switch {
	case !settings.Enabled:
		return "off"
	case settings.PlanAllowsLavaflow != nil && !*settings.PlanAllowsLavaflow:
		return "on, but no run starts until the plan allows it"
	case lavaFlowPaused(settings):
		return "on, but paused until it is turned on again"
	case settings.MaxStage == apiclient.LavaFlowStageDiagnose:
		return "on, diagnosing only (max_stage: diagnose)"
	case details.RepositoryUnknown:
		return "on"
	case details.Repository == "":
		return "on, diagnosing only until a repository is connected"
	default:
		return "on, opening fix pull requests in " + theme.StripControl(details.Repository)
	}
}

// lavaFlowLimits names the settings a project configuration file narrowed.
// LavaFlow on runs on its own and opens pull requests; anything less is
// worth saying, because on/off is all the command shows otherwise. The saved
// settings apply the next time LavaFlow is turned on, so they count while it
// is off too.
func lavaFlowLimits(settings *apiclient.LavaFlowSettings) []string {
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
// with its flags, so the next-page hint pages the same set. When a run on the
// page ended without a fix, it points to getCommand, which says what to do.
func LavaFlowRuns(w io.Writer, page *apiclient.PaginatedLavaFlowRuns, nextCommand, getCommand string) {
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
			lavaFlowTrigger(run),
			statusCell(theme.StripControl(string(run.Status)), 10, on),
			Truncate(lavaFlowResult(run), 26),
			FormatTimeAgo(run.CreatedAt),
		)
	}
	if slices.ContainsFunc(page.Data, lavaFlowEndedWithoutAFix) {
		fmt.Fprintf(w, "\n%s%s\n", theme.Dim("See why a run stopped and what to do: ", on),
			theme.Command(getCommand+" <run-id>", on))
	}
	if page.HasMore && page.NextCursor != nil {
		nextPage(w, on, fmt.Sprintf("%s --cursor %s", nextCommand, cliruntime.ShellQuote(*page.NextCursor)))
	}
}

// LavaFlowCommands are the commands a run's next step can name, with the
// cloud prefix where it applies.
type LavaFlowCommands struct {
	Enable     string
	GitConnect string
	// RunsStart is followed by --issue and the run's issue ID.
	RunsStart string
}

// LavaFlowRun renders one run: where it is, what it found, and the pull
// request it opened. A run that stopped or opened no pull request says why,
// and ends with what to do about it. The diagnosis is the run's own text, so
// it is printed without control characters.
func LavaFlowRun(w io.Writer, run *apiclient.LavaFlowRun, commands LavaFlowCommands) {
	if run == nil {
		return
	}
	on := theme.On(w)
	kv(w, on, "ID", "%s", run.Id.String())
	issue := lavaFlowRunIssue(run)
	if issue != nil {
		if title := lavaFlowIssueTitle(issue); title != "" {
			kv(w, on, "Issue", "%s (%s)", title, issue.Id)
		} else {
			kv(w, on, "Issue", "%s", issue.Id)
		}
	}
	kv(w, on, "Trigger", "%s", lavaFlowTrigger(run))
	kv(w, on, "Status", "%s", theme.Status(theme.StripControl(string(run.Status)), on))
	failure, failed := lavaFlowFailure(run)
	if failed {
		kv(w, on, "Reason", "%s", failure.why)
	}
	kv(w, on, "Created", "%s", FormatTimestamp(run.CreatedAt))
	if run.StartedAt != nil {
		kv(w, on, "Started", "%s", FormatTimestamp(*run.StartedAt))
	}
	if run.CompletedAt != nil {
		kv(w, on, "Completed", "%s", FormatTimestamp(*run.CompletedAt))
	}
	skip, skipped := lavaFlowSkip(run)
	if pr := run.PullRequest; pr != nil {
		kv(w, on, "Pull request", "%s (%s)", theme.StripControl(pr.Url), lavaFlowPullRequestState(pr))
	} else if skipped {
		kv(w, on, "Pull request", "none: %s", skip.why)
	}
	if run.CommitSha != nil {
		kv(w, on, "Commit", "%s", theme.StripControl(*run.CommitSha))
	}
	if diagnosis := run.Diagnosis; diagnosis != nil {
		lavaFlowDiagnosis(w, on, diagnosis)
	}

	stop := failure
	if !failed {
		stop = skip
	}
	if failed || skipped {
		next := lavaFlowNextCommands{enable: theme.Command(commands.Enable, on), gitConnect: theme.Command(commands.GitConnect, on)}
		if issue != nil {
			next.start = theme.Command(commands.RunsStart+" --issue "+issue.Id.String(), on)
		}
		fmt.Fprintln(w)
		kv(w, on, "Next", "%s", stop.next(next))
	}
}

func lavaFlowDiagnosis(w io.Writer, on bool, diagnosis *apiclient.LavaFlowDiagnosis) {
	fmt.Fprintf(w, "\n%s\n", theme.Title("Diagnosis", on))
	kv(w, on, "Summary", "%s", lavaFlowText(diagnosis.Summary))
	kv(w, on, "Root cause", "%s", lavaFlowText(diagnosis.RootCause))
	kv(w, on, "Suggested fix", "%s", lavaFlowText(diagnosis.SuggestedFix))
	kv(w, on, "Confidence", "%s", theme.StripControl(string(diagnosis.Confidence)))
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
		kv(w, on, "Evidence", "%s%s: %s", theme.StripControl(string(evidence.Kind)), where, lavaFlowText(evidence.Detail))
	}
}

// lavaFlowText prints the diagnosis's free text, which can span lines, such
// as a stack trace or numbered steps. It keeps the line breaks, indenting
// each continuation line under its label, and strips every other control
// character.
func lavaFlowText(s string) string {
	lines := strings.Split(strings.ReplaceAll(strings.TrimSpace(s), "\r\n", "\n"), "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(theme.StripControl(line), " ")
		if i > 0 && lines[i] != "" {
			lines[i] = "  " + lines[i]
		}
	}
	return strings.Join(lines, "\n")
}

// lavaFlowIssueTrigger is the run's trigger when it is an issue run, or nil
// for a trigger type this CLI does not know, which renders as "other"
// without an issue.
func lavaFlowIssueTrigger(run *apiclient.LavaFlowRun) *apiclient.LavaFlowIssueTrigger {
	kind, err := run.Trigger.Discriminator()
	if err != nil || kind != string(apiclient.LavaFlowIssueTriggerTypeIssue) {
		return nil
	}
	trigger, err := run.Trigger.AsLavaFlowIssueTrigger()
	if err != nil {
		return nil
	}
	return &trigger
}

// lavaFlowRunIssue is the issue a run works on, or nil when it is not an
// issue run.
func lavaFlowRunIssue(run *apiclient.LavaFlowRun) *apiclient.LavaFlowTriggerIssue {
	if trigger := lavaFlowIssueTrigger(run); trigger != nil {
		return &trigger.Issue
	}
	return nil
}

func lavaFlowIssue(run *apiclient.LavaFlowRun) string {
	issue := lavaFlowRunIssue(run)
	if issue == nil {
		return "-"
	}
	if title := lavaFlowIssueTitle(issue); title != "" {
		return title
	}
	return issue.Id.String()
}

// lavaFlowIssueTitle is the printable title, empty when the title has nothing
// left to show once control characters are stripped.
func lavaFlowIssueTitle(issue *apiclient.LavaFlowTriggerIssue) string {
	return strings.TrimSpace(theme.StripControl(issue.Title))
}

func lavaFlowTrigger(run *apiclient.LavaFlowRun) string {
	trigger := lavaFlowIssueTrigger(run)
	switch {
	case trigger == nil:
		return "other"
	case trigger.Reason == "":
		return "-"
	case trigger.Reason == apiclient.LavaFlowIssueTriggerReasonNewIssue:
		return "new issue"
	default:
		return theme.StripControl(string(trigger.Reason))
	}
}

// lavaFlowResult is a run's outcome in a few words: the pull request it
// opened, or why it stopped or opened none.
func lavaFlowResult(run *apiclient.LavaFlowRun) string {
	if run.PullRequest != nil {
		return fmt.Sprintf("PR #%d %s", run.PullRequest.Number, lavaFlowPullRequestState(run.PullRequest))
	}
	if failure, ok := lavaFlowFailure(run); ok {
		return failure.short
	}
	if skip, ok := lavaFlowSkip(run); ok {
		return skip.short
	}
	return "-"
}

// lavaFlowEndedWithoutAFix is whether a run is done without the pull request
// it could have opened, so its next step is worth looking up. A paused or
// retrying run carries on by itself, and a diagnose-only run did what it was
// set to do.
func lavaFlowEndedWithoutAFix(run apiclient.LavaFlowRun) bool {
	switch run.Status {
	case apiclient.LavaFlowRunStatusFailed, apiclient.LavaFlowRunStatusCanceled:
		return true
	case apiclient.LavaFlowRunStatusSucceeded:
		return run.PullRequest == nil && run.MaxStage != apiclient.LavaFlowStageDiagnose
	default:
		return false
	}
}

func lavaFlowPullRequestState(pr *apiclient.LavaFlowPullRequest) string {
	if pr.State == apiclient.LavaFlowPullRequestStateMerged && pr.DeployedAt != nil {
		return "merged, deployed"
	}
	return theme.StripControl(string(pr.State))
}

// lavaFlowStop is what the CLI says about a run that stopped, is waiting, or
// opened no pull request: short fits the runs list's Result column, why is
// the whole reason in plain words, and next is what to do about it. A code
// is never the only explanation.
type lavaFlowStop struct {
	short, why string
	next       lavaFlowNext
}

// lavaFlowNext says what to do about a stopped run, naming the commands it
// needs.
type lavaFlowNext func(lavaFlowNextCommands) string

// lavaFlowNextCommands are the commands a next step can name, ready to print.
// start is empty for a run that is not about an issue.
type lavaFlowNextCommands struct {
	start, enable, gitConnect string
}

func say(text string) lavaFlowNext {
	return func(lavaFlowNextCommands) string { return text }
}

func nothingToDo(why string) lavaFlowNext {
	return say("Nothing to do: " + why)
}

func startAgain(c lavaFlowNextCommands) string {
	return withCommand("Start a new run", c.start)
}

func tryLater(c lavaFlowNextCommands) string {
	return withCommand("Try again later", c.start)
}

func startAfterItRecurs(c lavaFlowNextCommands) string {
	return withCommand("Start a new run after the issue occurs again", c.start)
}

func turnOnAndStartAgain(c lavaFlowNextCommands) string {
	return fmt.Sprintf("Turn LavaFlow on (%s), then start a new run.", c.enable)
}

// turnOnAgainAndStartAgain resumes LavaFlow after the project's repository
// changed: turning it on again records the repository the project points at
// now.
func turnOnAgainAndStartAgain(c lavaFlowNextCommands) string {
	return fmt.Sprintf("Turn LavaFlow on again (%s) to use the project's repository now, then start a new run.", c.enable)
}

// connectAndPush names what LavaFlow can fix today. It grows when LavaFlow
// fixes more than a push deployed, and docs/lavaflow.md says the same.
func connectAndPush(c lavaFlowNextCommands) string {
	return fmt.Sprintf("Connect the repository (%s) and deploy by pushing to it. "+
		"LavaFlow fixes code that a push to the connected repository deployed.", c.gitConnect)
}

func withCommand(step, command string) string {
	if command == "" {
		return step + "."
	}
	return step + ": " + command
}

// lavaFlowFailures says why a run stopped, or why a queued run is paused, in
// LavaFlow's words. "Paused" matches the dashboard. A queued run with any
// other code is retrying its last attempt, which lavaFlowFailure puts first,
// like "paused:", so the runs list's narrow Result column keeps it.
var lavaFlowFailures = map[string]lavaFlowStop{
	"feature_unavailable": {
		"paused: not available", "paused: LavaFlow is unavailable here",
		nothingToDo("the run resumes when LavaFlow is available again."),
	},
	"feature_paused": {
		"paused: LavaFlow paused", "paused: LavaFlow is paused",
		nothingToDo("the run carries on when LavaFlow resumes."),
	},
	"model_request_rejected": {"request rejected", "LavaFlow's AI service rejected the request", startAgain},
	"model_unavailable":      {"AI unreachable", "LavaFlow couldn't reach its AI service", tryLater},
	"model_refused": {
		"declined to diagnose", "LavaFlow declined to diagnose this issue",
		say("Look into this issue yourself. A new run would likely decline it too."),
	},
	"no_diagnosis":       {"no diagnosis", "LavaFlow finished without a diagnosis", startAfterItRecurs},
	"budget_exhausted":   {"hit run limit", "LavaFlow reached this run's limit before finding a diagnosis", startAgain},
	"timed_out":          {"timed out", "the run did not finish in time", startAgain},
	"attempts_exhausted": {"out of attempts", "the run did not finish within its attempts", startAgain},
	"lavaflow_disabled":  {"LavaFlow turned off", "LavaFlow was turned off before the run finished", turnOnAndStartAgain},
	"plan_required": {
		"plan doesn't allow it", "the project's plan no longer allows LavaFlow",
		say("Upgrade the project's plan in the dashboard, then start a new run."),
	},
	"issue_not_found": {"issue deleted", "the issue no longer exists", say("Nothing to do.")},
	"internal_error":  {"LavaFlow error", "LavaFlow could not run the diagnosis", startAgain},

	// Older runs keep codes from before LavaFlow was renamed and before
	// Volcano picked the AI service for every run. No new run reports them,
	// and their names are never shown.
	"autofix_disabled":           {"LavaFlow turned off", "LavaFlow was turned off before the run finished", turnOnAndStartAgain},
	"api_key_rejected":           {"request rejected", "LavaFlow's AI service rejected the request", startAgain},
	"settings_changed":           {"settings changed", "LavaFlow's settings changed before the run finished", startAgain},
	"managed_models_unavailable": {"AI unreachable", "LavaFlow couldn't reach its AI service", startAgain},
}

// lavaFlowFailure says why a queued run is waiting, or why a run failed or
// was canceled. A running or finished run's code belongs to an attempt it
// has since moved past. Volcano adds codes over time, so a code missing here
// shows the API's failure_message, or says the run stopped and names the
// code.
func lavaFlowFailure(run *apiclient.LavaFlowRun) (lavaFlowStop, bool) {
	code, message := "", ""
	if run.FailureCode != nil {
		code = theme.StripControl(strings.TrimSpace(*run.FailureCode))
	}
	if run.FailureMessage != nil {
		message = theme.StripControl(strings.TrimSpace(*run.FailureMessage))
	}
	queued := run.Status == apiclient.LavaFlowRunStatusQueued
	switch run.Status {
	case apiclient.LavaFlowRunStatusQueued:
		if code == "" && message == "" {
			return lavaFlowStop{}, false
		}
	case apiclient.LavaFlowRunStatusFailed, apiclient.LavaFlowRunStatusCanceled:
	default:
		return lavaFlowStop{}, false
	}

	stop, ok := lavaFlowFailures[code]
	switch {
	case ok && strings.HasPrefix(stop.why, "paused:"):
		return stop, true
	case ok:
	case message != "":
		stop = lavaFlowStop{short: message, why: message, next: startAgain}
	case code != "" && queued:
		stop = lavaFlowStop{short: "", why: fmt.Sprintf("the last attempt failed (code: %s)", code)}
	case code != "" && run.Status == apiclient.LavaFlowRunStatusCanceled:
		stop = lavaFlowStop{short: "canceled", why: fmt.Sprintf("the run was canceled (code: %s)", code), next: startAgain}
	case code != "":
		stop = lavaFlowStop{short: "stopped", why: fmt.Sprintf("the run stopped (code: %s)", code), next: startAgain}
	case run.Status == apiclient.LavaFlowRunStatusCanceled:
		stop = lavaFlowStop{short: "canceled", why: "the run was canceled", next: startAgain}
	default:
		stop = lavaFlowStop{short: "failed", why: "the run failed", next: startAgain}
	}
	if queued {
		stop.short = strings.TrimSuffix("retrying: "+stop.short, ": ")
		stop.why = "retrying: " + stop.why
		stop.next = nothingToDo("LavaFlow tries again on its own.")
	}
	return stop, true
}

// lavaFlowSkips says why a run opened no pull request.
var lavaFlowSkips = map[apiclient.LavaFlowPullRequestSkipped]lavaFlowStop{
	apiclient.LavaFlowPullRequestSkippedSourceUnavailable: {
		"no PR: source unreadable",
		"LavaFlow couldn't read the deployed source", connectAndPush,
	},
	apiclient.LavaFlowPullRequestSkippedPullRequestOpen: {
		"no PR: one already open",
		"a LavaFlow pull request for this issue is already open", say("Review the pull request that is already open."),
	},
	apiclient.LavaFlowPullRequestSkippedNotFixable: {
		"no PR: not a code fix",
		"a code change would not fix it", say("Act on the diagnosis above; the fix is outside the code."),
	},
	apiclient.LavaFlowPullRequestSkippedLowConfidence: {
		"no PR: low confidence",
		"the diagnosis is not confident enough", say("Check the diagnosis above, and fix the issue by hand if it holds."),
	},
	apiclient.LavaFlowPullRequestSkippedNoPatch: {
		"no PR: no fix found",
		"LavaFlow found no fix", say("Use the suggested fix above as a starting point."),
	},
	apiclient.LavaFlowPullRequestSkippedPermissionMissing: {
		"no PR: app lacks access",
		"the Volcano GitHub App cannot open pull requests in the repository",
		say("Check the Volcano GitHub App's access to the repository in the dashboard's Git settings, then start a new run."),
	},
	apiclient.LavaFlowPullRequestSkippedRepositoryChanged: {
		"no PR: repository changed",
		"the project's repository changed since LavaFlow was turned on", turnOnAgainAndStartAgain,
	},
	apiclient.LavaFlowPullRequestSkippedPublishFailed: {
		"no PR: couldn't open it",
		"the pull request could not be opened", startAgain,
	},
}

// lavaFlowDiagnoseOnly is a finished run whose project configuration stops
// runs at the diagnosis.
var lavaFlowDiagnoseOnly = lavaFlowStop{
	"no PR: diagnose only", "the run only diagnoses (max_stage: diagnose)",
	say("For fix pull requests, set max_stage: pull_request in the project configuration."),
}

// lavaFlowSkip says why a finished run opened no pull request. Volcano adds
// reasons over time, so one missing here says LavaFlow did not open one and
// names the reason.
func lavaFlowSkip(run *apiclient.LavaFlowRun) (lavaFlowStop, bool) {
	if run.PullRequest != nil || run.Status != apiclient.LavaFlowRunStatusSucceeded {
		return lavaFlowStop{}, false
	}
	if run.PullRequestSkipped == nil {
		if run.MaxStage == apiclient.LavaFlowStageDiagnose {
			return lavaFlowDiagnoseOnly, true
		}
		return lavaFlowStop{"no PR", "LavaFlow did not open one", say("Act on the diagnosis above.")}, true
	}
	if stop, ok := lavaFlowSkips[*run.PullRequestSkipped]; ok {
		return stop, true
	}
	why := "LavaFlow did not open one"
	if reason := theme.StripControl(strings.TrimSpace(string(*run.PullRequestSkipped))); reason != "" {
		why += fmt.Sprintf(" (reason: %s)", reason)
	}
	return lavaFlowStop{"no PR", why, say("Act on the diagnosis above.")}, true
}
