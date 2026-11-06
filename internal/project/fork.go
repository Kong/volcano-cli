package project

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/apiclient"
	"github.com/Kong/volcano-cli/internal/sharing"
)

// The codes of the refusals a fork can answer with.
const (
	// ReleaseChangedCode is the 409 that says the share has a newer release
	// than the one the person confirmed. Nothing was created.
	ReleaseChangedCode       = "release_changed"
	idempotencyKeyReusedCode = "idempotency_key_reused"
	requestInProgressCode    = "request_in_progress"
	variablesRequiredCode    = "variables_required"
	variableNotDeclaredCode  = "variable_not_declared"
	proxyRejectedCode        = "proxy_credential_rejected"
	rateLimitUnavailableCode = "rate_limit_unavailable"
	featurePausedCode        = "feature_paused"
	featureUnavailableCode   = "feature_unavailable"
)

// maxInProgressWait caps how long a fork waits for an earlier attempt with the
// same idempotency key to settle before asking once more.
const maxInProgressWait = 30 * time.Second

// ErrReleaseChanged reports that the share moved to a new release after the
// preview, so the release the person confirmed is no longer offered.
var ErrReleaseChanged = errors.New("the share has a new release since the preview")

// IsReleaseChanged reports whether err is the refusal to fork a release the
// share no longer offers.
func IsReleaseChanged(err error) bool {
	return errors.Is(err, ErrReleaseChanged)
}

// PreviewShare returns the release a share offers. The route is public, so the
// request carries no credential, but the call still needs a signed-in account
// so a command that cannot go on to fork stops before it reaches the network.
func (s Service) PreviewShare(ctx context.Context, shareID string) (*apiclient.SharePreview, error) {
	authenticated, err := s.sessions.AccountScoped()
	if err != nil {
		return nil, err
	}
	anonymous, err := authenticated.APIWithToken("")
	if err != nil {
		return nil, err
	}
	preview, err := anonymous.GetSharePreview(ctx, shareID)
	if err != nil {
		return nil, forkError("preview the share", err, time.Now())
	}
	return preview, nil
}

// Fork creates the person's own project from the release a share offers.
// Installation is asynchronous: poll Get until the project's template
// installation is ready or failed.
//
// A request whose answer never arrived is sent once more with the same
// idempotency key. When the API answers that an earlier attempt with that key
// is still settling, as it does after a lost answer, it is sent once more after
// the wait the API names. Either way it creates at most one project.
func (s Service) Fork(ctx context.Context, request api.ForkRequest) (*apiclient.Project, error) {
	authenticated, err := s.sessions.AccountScoped()
	if err != nil {
		return nil, err
	}
	project, err := authenticated.API.CreateFork(ctx, request)
	retryable := request.IdempotencyKey != ""
	if err != nil && retryable && ctx.Err() == nil && api.Status(err) == 0 {
		project, err = authenticated.API.CreateFork(ctx, request)
	}
	if err != nil && retryable && ctx.Err() == nil &&
		api.Status(err) == http.StatusConflict && api.Code(err) == requestInProgressCode {
		sleep := s.sleep
		if sleep == nil {
			sleep = sleepContext
		}
		if waitErr := waitRetryAfter(ctx, err, sleep); waitErr != nil {
			return nil, waitErr
		}
		project, err = authenticated.API.CreateFork(ctx, request)
	}
	if err != nil {
		return nil, forkError(fmt.Sprintf("fork the share as project %q", request.Name), err, time.Now())
	}
	return project, nil
}

// waitRetryAfter waits as long as a refusal's Retry-After says, at most
// maxInProgressWait, or a second when it says nothing.
func waitRetryAfter(ctx context.Context, err error, sleep func(context.Context, time.Duration) error) error {
	now := time.Now()
	wait := time.Second
	if at, ok := api.RetryAt(err, now); ok {
		wait = min(max(at.Sub(now), 0), maxInProgressWait)
	}
	return sleep(ctx, wait)
}

func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// forkError says what failed and what to do about the refusals the API
// documents for previews and forks. The API's own message is the only server
// text shown; the advice is the CLI's.
func forkError(action string, err error, now time.Time) error {
	status, code := api.Status(err), api.Code(err)
	switch {
	case status == http.StatusConflict && code == ReleaseChangedCode:
		return fmt.Errorf("failed to %s: %w: %w", action, ErrReleaseChanged, err)
	case (status == http.StatusNotFound || status == http.StatusBadRequest) && code == featureUnavailableCode:
		return fmt.Errorf("failed to %s: Sharing projects is not available in this environment.%w", action, hiddenError{err})
	case status == http.StatusNotFound:
		// Every unavailable share answers the same, so no reason is given.
		return fmt.Errorf("failed to %s: This share link is not available. It does not exist, was switched off, "+
			"or has no published release.%w", action, hiddenError{err})
	case status == http.StatusConflict && code == idempotencyKeyReusedCode:
		return withHint(action, err, "This request's idempotency key was already used for a different request, "+
			"so nothing was created. Run the command again.")
	case status == http.StatusConflict && code == requestInProgressCode:
		return withHint(action, err, "An earlier attempt of this fork is still being set up. "+
			sharing.RetryAdvice(err, now)+" Running the command again may create a second copy, so check "+
			"`volcano projects list` first.")
	case status == http.StatusUnprocessableEntity && code == variablesRequiredCode:
		return withHint(action, err, "Nothing was created. The release declares variables Volcano could not set up; "+
			"ask the person who shared it to share it again.")
	case status == http.StatusUnprocessableEntity && code == variableNotDeclaredCode:
		return withHint(action, err, "Nothing was created. Pass --var only for the variables the preview lists.")
	case status == http.StatusForbidden && code == proxyRejectedCode:
		return withHint(action, err, "Volcano refused a proxy credential that came with the request. "+
			"It is not about your session; try again without a proxy in between.")
	case status == http.StatusForbidden:
		return withHint(action, err, "Your plan does not allow what this release deploys, or your account may not "+
			"create projects. Delete a project you no longer need with `volcano projects delete`, "+
			"or upgrade your plan, then try again.")
	case status == http.StatusUnavailableForLegalReasons:
		return withHint(action, err, "Volcano cannot serve requests from your location under US sanctions.")
	case status == http.StatusTooManyRequests:
		return withHint(action, err, "Too many requests, so nothing was created. "+sharing.RetryAdvice(err, now))
	case status == http.StatusServiceUnavailable && code == rateLimitUnavailableCode:
		return withHint(action, err, "Volcano cannot count forks right now, so nothing was created. "+
			sharing.RetryAdvice(err, now))
	case status == http.StatusServiceUnavailable && code == featurePausedCode:
		return withHint(action, err, "Durable functions are paused and this release has one, "+
			"so nothing was created. "+sharing.RetryAdvice(err, now))
	case status == http.StatusServiceUnavailable:
		return withHint(action, err, "Volcano is not available right now. "+sharing.RetryAdvice(err, now))
	default:
		return fmt.Errorf("failed to %s: %w", action, err)
	}
}

func withHint(action string, err error, hint string) error {
	return fmt.Errorf("failed to %s: %w\n\n%s", action, err, hint)
}

// hiddenError keeps the API error reachable through errors.As without printing
// it: the message already says everything the user should be told.
type hiddenError struct{ err error }

func (hiddenError) Error() string { return "" }

func (e hiddenError) Unwrap() error { return e.err }
