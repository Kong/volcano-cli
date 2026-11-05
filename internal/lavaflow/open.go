package lavaflow

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/google/uuid"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/apiclient"
	"github.com/Kong/volcano-cli/internal/config"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

// HeldFix is a fix LavaFlow held because a workflow in the repository could
// run it with the repository's secrets before a person approves it, as read
// for a person to open. Open sends exactly these fingerprints, so Volcano
// opens the fix and jobs the person was shown or nothing.
type HeldFix struct {
	Run *apiclient.LavaflowRun
	// DiffSHA256 identifies the fix: its base commit, its files and its
	// whole diff, including any part the diff shown leaves out.
	DiffSHA256 string
	// JobsSHA256 identifies every job in Run.HeldWorkflows and the commits
	// their workflows were read at.
	JobsSHA256 string
}

// ErrFixChanged is the open route's 409 for a fix or job list that changed
// since the person read it.
var ErrFixChanged = errors.New("the fix or its jobs changed since you looked; run it again")

// ErrOpenUnsupported reports an API that has LavaFlow's runs but not the
// route that opens a held fix.
var ErrOpenUnsupported = errors.New("this Volcano API can't open a fix LavaFlow held yet")

// codeRunNotHeld is the open route's 409 for a run that holds no fix to
// open. Its other 409, run_changed, is a fix or job list that changed.
const codeRunNotHeld = "run_not_held"

var fingerprintPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// OpenHeldFix reads a run, refuses one that holds no fix a person can open,
// and has confirm show the fix, its jobs and both fingerprints before it
// opens the fix's pull request with those fingerprints. A project access
// token is refused before anything is sent. An error from confirm stops it
// and is returned as is. It returns the run and the body the API sent it in,
// for --json.
func (s Service) OpenHeldFix(ctx context.Context, runID string, confirm func(*HeldFix) error) (*apiclient.LavaflowRun, []byte, error) {
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
	run, _, err := authenticated.API.GetLavaflowRun(ctx, authenticated.ProjectID, id)
	if err != nil {
		return nil, nil, s.runRefused(runID, fmt.Errorf("failed to get LavaFlow run %s: %w", runID, err))
	}
	held, err := heldFix(runID, run, cliruntime.CommandPath(s.deps, "lavaflow show "+runID))
	if err != nil {
		return nil, nil, err
	}
	if err := confirm(held); err != nil {
		return nil, nil, err
	}
	opened, body, err := authenticated.API.OpenLavaflowRun(ctx, authenticated.ProjectID, id, held.DiffSHA256, held.JobsSHA256)
	if err != nil {
		return nil, nil, s.openRefused(authenticated.Config, runID, err)
	}
	return opened, body, nil
}

// Openable reports whether run holds a fix a person can open with runs open.
func Openable(run *apiclient.LavaflowRun) bool {
	_, err := heldFix(run.Id.String(), run, "")
	return err == nil
}

// heldFix refuses a run that holds no fix a person can open, saying why and
// naming show, the command that shows the run, and otherwise returns the fix
// with its fingerprints.
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
	}
	return &HeldFix{Run: run, DiffSHA256: *run.DiffSha256, JobsSHA256: run.HeldWorkflows.JobsSha256}, nil
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
// keeping the refusal as the cause for the exit code. A 409 with a code this
// CLI doesn't know reads as a change: running the command again shows the
// run as it is now.
func (s Service) openRefused(cfg *config.Config, runID string, err error) error {
	switch {
	case api.Status(err) == http.StatusConflict && api.Code(err) == codeRunNotHeld:
		return api.WithCause(fmt.Errorf("LavaFlow run %s no longer holds a fix to open. See it with '%s'",
			runID, cliruntime.CommandPath(s.deps, "lavaflow show "+runID)), err)
	case api.Status(err) == http.StatusConflict:
		return api.WithCause(ErrFixChanged, err)
	case api.Code(err) == codePersonRequired:
		return openNeedsAPerson(cfg, err)
	case api.RouteMissing(err):
		return api.WithCause(ErrOpenUnsupported, err)
	}
	return s.runRefused(runID, fmt.Errorf("failed to open LavaFlow run %s's fix: %w", runID, err))
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
