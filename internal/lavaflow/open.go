package lavaflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/apiclient"
	"github.com/Kong/volcano-cli/internal/config"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	clisession "github.com/Kong/volcano-cli/internal/session"
)

// HeldFix is a fix LavaFlow held because a workflow in the repository could
// run it with the repository's secrets before a person approves it, as read
// for a person to open. Open sends exactly its two fingerprints, as Volcano
// served them, so Volcano opens the diff and jobs the person was shown or
// nothing.
type HeldFix struct {
	Run *apiclient.LavaflowRun
	// DiffSHA256 is the SHA-256 of Run.Diff exactly as served.
	DiffSHA256 string
	// JobsSHA256 identifies every job in Run.HeldWorkflows and the commits
	// their workflows were read at.
	JobsSHA256 string
	// Withheld lists the files the diff leaves out because they may hold a
	// credential, by the path each marker quotes, or "" for one it doesn't
	// name.
	Withheld []string
	// Cut is a diff cut short, which doesn't show the whole fix.
	Cut bool
}

// Reviewable reports a diff that shows the whole fix. LavaFlow opens no
// other.
func (h *HeldFix) Reviewable() bool { return len(h.Withheld) == 0 && !h.Cut }

// OpenPrompt shows a held fix to a person and asks whether to open it.
type OpenPrompt struct {
	// Show prints the fix for round, which counts from 1; a round after the
	// first follows a fix or job list that changed.
	Show func(held *HeldFix, round int)
	// Confirm asks after Show. An error stops the open and is returned as is.
	Confirm func(held *HeldFix) error
	// Reask is whether Confirm can ask again when the fix or its jobs changed
	// before Volcano opened it. Without it, a change ends the open.
	Reask bool
}

// MaxOpenRounds is how many times runs open shows a fix that keeps changing
// before it stops.
const MaxOpenRounds = 3

var (
	// ErrFixChanged is the open route's run_changed: the fix or its jobs
	// changed since the person read them.
	ErrFixChanged = errors.New("the fix or its jobs changed since you looked; run it again")
	// ErrFixKeptChanging stops a fix that changed every time it was shown.
	ErrFixKeptChanging = fmt.Errorf("the fix or its jobs changed each of the %d times you looked, so nothing was opened; "+
		"run it again later", MaxOpenRounds)
	// ErrNotReviewable refuses a fix whose diff leaves out a file or is cut
	// short. Volcano refuses it too, so nothing is sent.
	ErrNotReviewable = errors.New("LavaFlow can't open a fix whose diff isn't fully reviewable, so nothing was opened")
	// ErrOpenUnsupported reports an API that has LavaFlow's runs but not the
	// route that opens a held fix.
	ErrOpenUnsupported = errors.New("this Volcano API can't open a fix LavaFlow held yet")
)

// Codes of the open route's 409s.
const (
	codeRunNotHeld        = "run_not_held"
	codeRunChanged        = "run_changed"
	codeLavaflowDisabled  = "lavaflow_disabled"
	codeRepoRequired      = "repository_required"
	codeRepoChanged       = "repository_changed"
	codeRunInProgress     = "lavaflow_run_in_progress"
	codeAttemptsExhausted = "run_attempts_exhausted"
)

var fingerprintPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// OpenHeldFix reads a run, refuses one that holds no fix a person can open,
// and has prompt show the fix, its jobs and both fingerprints. A fix whose
// diff isn't fully reviewable is refused once shown. Otherwise prompt asks,
// and the fix is opened with the fingerprints it showed. When the fix or its
// jobs changed in the meantime, it reads the run again and shows and asks
// again, up to MaxOpenRounds times, or stops when prompt can't ask again. A
// project access token is refused before anything is sent. It returns the
// queued run and the body the API sent it in, for --json.
func (s Service) OpenHeldFix(ctx context.Context, runID string, prompt OpenPrompt) (*apiclient.LavaflowRun, []byte, error) {
	id, err := uuid.Parse(runID)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid run ID %q: use an ID from '%s'", runID, cliruntime.CommandPath(s.deps, "lavaflow list"))
	}
	authenticated, err := s.project()
	if err != nil {
		return nil, nil, err
	}
	if config.IsProjectToken(authenticated.Config.Token()) {
		return nil, nil, openNeedsAPerson(authenticated.Config, config.ErrAccountTokenRequired)
	}
	for round := 1; round <= MaxOpenRounds; round++ {
		held, err := s.readHeldFix(ctx, authenticated, id, runID)
		if err != nil {
			return nil, nil, err
		}
		prompt.Show(held, round)
		if !held.Reviewable() {
			return nil, nil, ErrNotReviewable
		}
		if err := prompt.Confirm(held); err != nil {
			return nil, nil, err
		}
		opened, body, err := authenticated.API.OpenLavaflowRun(ctx, authenticated.ProjectID, id, held.DiffSHA256, held.JobsSHA256)
		switch {
		case err == nil:
			return opened, body, nil
		case api.Status(err) != http.StatusConflict || api.Code(err) != codeRunChanged:
			return nil, nil, s.openRefused(authenticated.Config, runID, err)
		case !prompt.Reask:
			return nil, nil, api.WithCause(ErrFixChanged, err)
		case round == MaxOpenRounds:
			return nil, nil, api.WithCause(ErrFixKeptChanging, err)
		}
	}
	return nil, nil, ErrFixKeptChanging
}

// readHeldFix reads the run and refuses one that holds no fix a person can
// open.
func (s Service) readHeldFix(ctx context.Context, authenticated *clisession.ProjectSession, id uuid.UUID, runID string) (*HeldFix, error) {
	run, _, err := authenticated.API.GetLavaflowRun(ctx, authenticated.ProjectID, id)
	if err != nil {
		return nil, s.runRefused(runID, fmt.Errorf("failed to get LavaFlow run %s: %w", runID, err))
	}
	return heldFix(runID, run, cliruntime.CommandPath(s.deps, "lavaflow show "+runID))
}

// Openable reports whether run holds a fix a person can open with runs open.
func Openable(run *apiclient.LavaflowRun) bool {
	held, err := heldFix(run.Id.String(), run, "")
	return err == nil && held.Reviewable()
}

// heldFix refuses a run that holds no fix a person can open, saying why and
// naming show, the command that shows the run, and otherwise returns the fix
// with its fingerprints and what its diff leaves out. The diff is checked
// against its fingerprint as served, never changed, so the fix shown is the
// one opened.
func heldFix(runID string, run *apiclient.LavaflowRun, show string) (*HeldFix, error) {
	switch {
	case run.PullRequest != nil:
		return nil, fmt.Errorf("LavaFlow run %s already opened its fix's pull request, #%d, so there's nothing to open. "+
			"See it with '%s'", runID, run.PullRequest.Number, show)
	case running(run.Status):
		return nil, fmt.Errorf("LavaFlow is still working on run %s, so it holds no fix to open yet. "+
			"Follow it with '%s'", runID, show)
	case run.PullRequestSkipped == nil || *run.PullRequestSkipped != apiclient.LavaflowSkipReasonUngatedWorkflows:
		return nil, fmt.Errorf("LavaFlow didn't hold run %s's fix for the repository's workflows, so there's nothing to open. "+
			"See why with '%s'", runID, show)
	case run.Diff == nil || run.DiffSha256 == nil:
		return nil, fmt.Errorf("LavaFlow held run %s's fix before it kept a fix's diff to review, so it can't be opened. "+
			"See the run with '%s'", runID, show)
	case run.HeldWorkflows == nil:
		return nil, fmt.Errorf("LavaFlow couldn't list every workflow job that held run %s's fix, as when there are "+
			"more than 256, so it can't be opened. Gate those workflows on a label a person adds instead", runID)
	case !fingerprintPattern.MatchString(*run.DiffSha256) || !fingerprintPattern.MatchString(run.HeldWorkflows.JobsSha256):
		return nil, fmt.Errorf("the fingerprints Volcano sent for run %s aren't in a form this CLI can check, so nothing was opened", runID)
	case !servedAsHashed(*run.Diff, *run.DiffSha256):
		return nil, fmt.Errorf("the diff Volcano sent for run %s doesn't match its fix fingerprint, so nothing was opened. "+
			"Run this again", runID)
	}
	withheld, cut := DiffNotShown(*run.Diff)
	if len(withheld) == 0 && len(run.DiffWithheld) > 0 {
		withheld = run.DiffWithheld
	}
	return &HeldFix{
		Run: run, DiffSHA256: *run.DiffSha256, JobsSHA256: run.HeldWorkflows.JobsSha256,
		Withheld: withheld, Cut: cut || run.DiffTruncated,
	}, nil
}

// servedAsHashed reports a diff whose bytes, as served, are the ones its
// fingerprint covers. The digest is only compared: it is never shown, sent
// or logged.
func servedAsHashed(diff, fingerprint string) bool {
	sum := sha256.Sum256([]byte(diff))
	return hex.EncodeToString(sum[:]) == fingerprint
}

// withheldMarker starts the line Volcano writes in a diff in place of a file
// it withheld, before the file's quoted path.
const withheldMarker = "# LavaFlow withheld the changes to "

// WithheldLine reports a line Volcano writes in a diff in place of a file it
// withheld. No diff line starts with #, so every such line counts, even one
// worded unlike the marker.
func WithheldLine(line string) bool { return strings.HasPrefix(line, "#") }

// CutLine reports the line Volcano ends a diff it cut short with.
func CutLine(line string) bool { return strings.HasPrefix(line, "[diff truncated") }

// DiffNotShown lists the files a served diff marks as withheld, by the path
// each marker quotes, or "" for one that names none, and reports whether it
// marks a cut.
func DiffNotShown(diff string) (withheld []string, cut bool) {
	for line := range strings.SplitSeq(diff, "\n") {
		switch {
		case WithheldLine(line):
			withheld = append(withheld, withheldPath(line))
		case CutLine(line):
			cut = true
		}
	}
	return withheld, cut
}

// withheldPath is the path a withheld marker quotes, or "" for a marker that
// doesn't name one.
func withheldPath(line string) string {
	rest, ok := strings.CutPrefix(line, withheldMarker)
	if !ok {
		return ""
	}
	quoted, err := strconv.QuotedPrefix(rest)
	if err != nil {
		return ""
	}
	path, err := strconv.Unquote(quoted)
	if err != nil {
		return ""
	}
	return path
}

func running(status apiclient.LavaflowRunStatus) bool {
	switch status {
	case apiclient.LavaflowRunStatusQueued, apiclient.LavaflowRunStatusDiagnosing,
		apiclient.LavaflowRunStatusFixing, apiclient.LavaflowRunStatusReviewing:
		return true
	}
	return false
}

// runRefused says the project has no such run for a 404 the API sent without
// a code, and otherwise why LavaFlow isn't available, keeping the refusal as
// the cause for the exit code.
func (s Service) runRefused(runID string, err error) error {
	if api.Status(err) == http.StatusNotFound && api.Code(err) == "" && !api.RouteMissing(err) {
		return api.WithCause(fmt.Errorf("no LavaFlow run %s in this project: find one with '%s'",
			runID, cliruntime.CommandPath(s.deps, "lavaflow list")), err)
	}
	return unavailable(err)
}

// openRefused says why Volcano didn't open the fix, by the refusal's code,
// keeping the refusal as the cause for the exit code. run_changed is the
// caller's to handle.
func (s Service) openRefused(cfg *config.Config, runID string, err error) error {
	show := cliruntime.CommandPath(s.deps, "lavaflow show "+runID)
	var refusal error
	switch status, code := api.Status(err), api.Code(err); {
	case status == http.StatusConflict:
		refusal = conflictRefusal(code, runID, show, err)
	case code == codePersonRequired:
		return openNeedsAPerson(cfg, err)
	case code == codePlanRequired:
		refusal = ErrPlanRequired
	case status == http.StatusForbidden:
		refusal = fmt.Errorf("LavaFlow didn't open the fix: Volcano refused this credential, as it does a read-only "+
			"access token. Volcano said: %q", api.Message(err))
	case status == http.StatusTooManyRequests || status == http.StatusServiceUnavailable:
		refusal = retryLater(time.Now(), err)
	case api.RouteMissing(err):
		refusal = ErrOpenUnsupported
	default:
		return s.runRefused(runID, fmt.Errorf("failed to open LavaFlow run %s's fix: %w", runID, err))
	}
	return api.WithCause(refusal, err)
}

// conflictRefusal words each 409 but run_changed. A code this CLI doesn't
// know keeps Volcano's words.
func conflictRefusal(code, runID, show string, err error) error {
	switch code {
	case codeRunNotHeld:
		return fmt.Errorf("LavaFlow run %s no longer holds a fix a person can open. See it with '%s'", runID, show)
	case codeLavaflowDisabled:
		return errors.New("LavaFlow is off for this project, so it can't open the fix. " +
			"The project's owner turns it on with 'volcano cloud lavaflow enable'")
	case codeRepoRequired:
		return errors.New("the project has no connected repository, so LavaFlow can't open the fix. " +
			"Connect one with 'volcano git connect'")
	case codeRepoChanged:
		return errors.New("the project no longer deploys from the repository this fix was written for, " +
			"so LavaFlow can't open it. A new run uses the current one")
	case codeRunInProgress:
		return errors.New("another LavaFlow run is in progress for this issue, so LavaFlow can't open this fix now. " +
			"Try again once it finishes")
	case codeAttemptsExhausted:
		return fmt.Errorf("LavaFlow has used every attempt it gets for run %s, so it won't open the fix. See the run with '%s'",
			runID, show)
	}
	return fmt.Errorf("LavaFlow didn't open the fix. Volcano said: %q", api.Message(err))
}

// retryLater says Volcano couldn't take the open now, and when to try again
// when the response said. Nothing is retried for the person.
func retryLater(now time.Time, err error) error {
	refusal := "LavaFlow didn't open the fix: Volcano can't take the request now"
	if message := strings.TrimSuffix(strings.TrimSpace(api.Message(err)), "."); message != "" {
		refusal += fmt.Sprintf(" (%s)", message)
	}
	// A Retry-After that has already passed on this clock says nothing.
	if at, ok := api.RetryAt(err, now); ok {
		if wait := at.Sub(now).Round(time.Second); wait > 0 {
			return fmt.Errorf("%s. Try again in %s", refusal, wait)
		}
	}
	return fmt.Errorf("%s. Try again later", refusal)
}

// openNeedsAPerson says a person opens a held fix, signed in with volcano
// login, in Volcano's words when it gave some. cause is the refusal, the
// API's or the CLI's own, which keeps the exit code of a permission refused.
// A token in VOLCANO_TOKEN is used instead of a volcano login sign-in, so
// that case says to unset it.
func openNeedsAPerson(cfg *config.Config, cause error) error {
	how := "Sign in with 'volcano login' and run this again"
	if cfg.TokenFromEnv() {
		how = "Unset " + config.TokenEnv + " and sign in with 'volcano login', then run this again"
	}
	message := strings.TrimSuffix(strings.TrimSpace(api.Message(cause)), ".")
	switch {
	case config.IsProjectToken(cfg.Token()):
		return api.WithCause(fmt.Errorf("opening a fix LavaFlow held is a person's decision, "+
			"and a project access token can't make it. %s", how), cause)
	case message == "":
		return api.WithCause(fmt.Errorf("opening a fix LavaFlow held needs a person's own sign-in. %s", how), cause)
	case !strings.Contains(message, "volcano login"):
		return api.WithCause(fmt.Errorf("LavaFlow didn't open the fix: %s. %s", message, how), cause)
	case cfg.TokenFromEnv():
		return api.WithCause(fmt.Errorf("LavaFlow didn't open the fix: %s. %s is set, and its token is used instead "+
			"of a 'volcano login' sign-in until you unset it", message, config.TokenEnv), cause)
	}
	return api.WithCause(fmt.Errorf("LavaFlow didn't open the fix: %s", message), cause)
}
