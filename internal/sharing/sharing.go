// Package sharing performs the authenticated workflows behind volcano share.
//
// Sharing a project gives it a share: a link, https://volcano.dev/deploy/<id>,
// that anyone can fork their own copy of the project from. The owner publishes
// a release to it, an immutable snapshot of what the project serves, and
// visitors install exactly that release.
package sharing

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/apiclient"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	clisession "github.com/Kong/volcano-cli/internal/session"
)

// releasesPageSize is the page size the releases listing reads with, the most
// the API serves in one page.
const releasesPageSize = 100

// Service performs share workflows against the current project.
type Service struct {
	sessions clisession.Factory
}

// NewService returns a share service.
func NewService(deps cliruntime.Deps) Service {
	return Service{sessions: clisession.NewFactory(deps)}
}

// List returns the current project's shares, newest first.
func (s Service) List(ctx context.Context) ([]apiclient.Share, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}
	shares, err := authenticated.API.ListShares(ctx, authenticated.ProjectID)
	if err != nil {
		return nil, explain(opRead, "list the project's shares", err)
	}
	return shares, nil
}

// Create creates a draft share for the current project. Fields left nil take
// the API's defaults: no suggested name or description, and copies that may
// not be shared again.
func (s Service) Create(ctx context.Context, request apiclient.ShareCreateRequest) (*apiclient.Share, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}
	request.ProjectId = authenticated.ProjectID
	share, err := authenticated.API.CreateShare(ctx, request)
	if err != nil {
		return nil, explain(opCreate, "create the share", err)
	}
	return share, nil
}

// Get returns one of the current project's shares.
func (s Service) Get(ctx context.Context, shareID string) (*apiclient.Share, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}
	share, err := authenticated.API.GetShare(ctx, authenticated.ProjectID, shareID)
	if err != nil {
		return nil, explain(opRead, fmt.Sprintf("get share %q", shareID), err)
	}
	return share, nil
}

// Stats returns a share's click and fork counts. A days of zero leaves the
// window to the API.
func (s Service) Stats(ctx context.Context, shareID string, days int) (*apiclient.ShareStats, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}
	stats, err := authenticated.API.GetShareStats(ctx, authenticated.ProjectID, shareID, days)
	if err != nil {
		return nil, explain(opRead, fmt.Sprintf("get the counts of share %q", shareID), err)
	}
	return stats, nil
}

// Update changes a share: its description, whether copies may be shared
// again, or its status. Only the fields set are sent.
func (s Service) Update(ctx context.Context, shareID string, request apiclient.ShareUpdateRequest) (*apiclient.Share, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}
	share, err := authenticated.API.UpdateShare(ctx, authenticated.ProjectID, shareID, request)
	if err != nil {
		op, action := opUpdate, "update"
		switch {
		case request.Status != nil && *request.Status == apiclient.ShareUpdateRequestStatusDisabled:
			op, action = opDisable, "switch off"
		case request.Status != nil:
			op, action = opEnable, "switch on"
		}
		return nil, explain(op, fmt.Sprintf("%s share %s", action, shareID), err)
	}
	return share, nil
}

// SetStatus switches a share off or on.
func (s Service) SetStatus(
	ctx context.Context, shareID string, status apiclient.ShareUpdateRequestStatus,
) (*apiclient.Share, error) {
	return s.Update(ctx, shareID, apiclient.ShareUpdateRequest{Status: &status})
}

// Draft returns what publishing to the share would release now. Each draft is
// a full build that counts against the account's hourly release build budget.
func (s Service) Draft(ctx context.Context, shareID string) (*apiclient.ShareReleaseDraft, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}
	draft, err := authenticated.API.GetShareReleaseDraft(ctx, authenticated.ProjectID, shareID)
	if err != nil {
		return nil, explain(opPublish, "build what the share would publish", err)
	}
	return draft, nil
}

// Publish publishes the reviewed draft with every variable it lists and the
// service key functions the owner confirmed. When a fresh build is not that
// draft, nothing is published and the error is a *api.DraftChangedError that
// carries the new draft.
func (s Service) Publish(
	ctx context.Context, shareID string, draft *apiclient.ShareReleaseDraft,
) (*apiclient.ShareRelease, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}
	release, err := authenticated.API.PublishShareRelease(ctx, apiclient.ShareReleaseCreateRequest{
		ShareId:             shareID,
		DraftId:             draft.DraftId,
		Variables:           VariableNames(draft.Variables),
		ServiceKeyFunctions: nonNil(draft.ServiceKeyFunctions),
	})
	if err != nil {
		if _, changed := errors.AsType[*api.DraftChangedError](err); changed {
			return nil, err
		}
		return nil, explain(opPublish, "publish the release", err)
	}
	return release, nil
}

// Releases returns every release of a share, newest first.
func (s Service) Releases(ctx context.Context, shareID string) ([]apiclient.ShareRelease, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}
	releases := []apiclient.ShareRelease{}
	cursor := ""
	for {
		page, err := authenticated.API.ListShareReleases(ctx, authenticated.ProjectID, shareID, releasesPageSize, cursor)
		if err != nil {
			return nil, explain(opRead, fmt.Sprintf("list the releases of share %q", shareID), err)
		}
		releases = append(releases, page.Releases...)
		if page.NextCursor == "" || page.NextCursor == cursor {
			return releases, nil
		}
		cursor = page.NextCursor
	}
}

// Latest returns a share's newest release, and false when it has none.
func (s Service) Latest(ctx context.Context, shareID string) (apiclient.ShareRelease, bool, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return apiclient.ShareRelease{}, false, err
	}
	page, err := authenticated.API.ListShareReleases(ctx, authenticated.ProjectID, shareID, 1, "")
	if err != nil {
		return apiclient.ShareRelease{}, false, explain(opRead, fmt.Sprintf("list the releases of share %q", shareID), err)
	}
	if len(page.Releases) == 0 {
		return apiclient.ShareRelease{}, false, nil
	}
	return page.Releases[0], true, nil
}

// VariableNames returns the names of variables, never nil, as a publish sends
// them.
func VariableNames(variables []apiclient.ShareVariable) []string {
	names := make([]string, len(variables))
	for i, variable := range variables {
		names[i] = variable.Name
	}
	return names
}

func nonNil(names []string) []string {
	if names == nil {
		return []string{}
	}
	return names
}

// operation says what a failed call was, because the same status means
// different things to different calls.
type operation int

const (
	// opRead is a list, get, stats or releases, which a project access token
	// may make.
	opRead operation = iota
	// opCreate adds a share.
	opCreate
	// opEnable switches a share on.
	opEnable
	// opDisable switches a share off.
	opDisable
	// opUpdate changes a share's description or whether copies may be shared.
	opUpdate
	// opPublish builds a release draft or publishes a release.
	opPublish
)

// The codes of the refusals the share routes document.
const (
	// featureUnavailableCode is a closed release gate: a 404 when creating a
	// share, building a draft or publishing, and a 400 when editing or
	// switching one on. Neither is a missing share or a bad request.
	featureUnavailableCode = "feature_unavailable"
	// ReshareNotAllowedCode refuses to share a copy of someone else's shared
	// project whose source does not let copies be shared again.
	ReshareNotAllowedCode = "reshare_not_allowed"
	// SourceUnavailableCode refuses to share a copy whose source share is not
	// available to share from right now.
	SourceUnavailableCode = "source_unavailable"
	// OperatorDisabledCode refuses a change while Volcano has taken one of the
	// project's shares down.
	OperatorDisabledCode = "operator_disabled"
	// editDroppedCode reports a share that was switched off while the other
	// change sent with it was not saved.
	editDroppedCode = "edit_dropped"
	// releaseBuildsLimitedCode is the hourly release build budget, which drafts
	// and publishes share.
	releaseBuildsLimitedCode = "release_builds_limited"
	// buildsBusyCode reports a server whose release builds are all busy.
	buildsBusyCode = "builds_busy"
)

// readOnlyMessage is the API's refusal of a change from an account it holds
// read-only. It carries no code, so the message is what identifies it.
const readOnlyMessage = "user is read-only"

// unavailableError reports that sharing is not offered here. It keeps the
// API's error for callers that read the status.
type unavailableError struct {
	err     error
	publish bool
}

func (e *unavailableError) Error() string {
	if e.publish {
		return "Publishing a shared project's release is not available in this environment."
	}
	return "Sharing projects is not available in this environment."
}

func (e *unavailableError) Unwrap() error { return e.err }

// explain says what was being done and, for the refusals the API documents for
// shares, what to do about it. The API's own message is the only server text
// shown; the advice is the CLI's.
func explain(op operation, action string, err error) error {
	status := api.Status(err)
	if (status == http.StatusNotFound || status == http.StatusBadRequest) && api.Code(err) == featureUnavailableCode {
		return fmt.Errorf("failed to %s: %w", action, &unavailableError{err: err, publish: op == opPublish})
	}
	hint := hintFor(op, status, err, time.Now())
	if hint == "" {
		return fmt.Errorf("failed to %s: %w", action, err)
	}
	return fmt.Errorf("failed to %s: %w\n\n%s", action, err, hint)
}

func hintFor(op operation, status int, err error, now time.Time) string {
	code := api.Code(err)
	switch status {
	case http.StatusBadRequest:
		if code == editDroppedCode {
			return "Sharing was switched off, but the other change sent with it was not saved. " +
				"Run the command again once you can make that change."
		}
		return ""
	case http.StatusUnauthorized:
		return "Sign in again with " + rootCommand("login") + "."
	case http.StatusForbidden:
		return forbiddenHint(op, code, err)
	case http.StatusConflict:
		if op == opCreate {
			return "A project can have at most 20 shares. Choose one with --link, or delete one you no longer " +
				"need in the dashboard."
		}
		return ""
	case http.StatusUnprocessableEntity:
		if op == opPublish {
			return "A release is built from what the project serves now, so the project needs a deployed " +
				"function or frontend with stored source. Redeploy what the message names, then try again."
		}
		return ""
	case http.StatusTooManyRequests:
		if code == releaseBuildsLimitedCode {
			return "Your account built too many releases this hour: sharing builds one to review and one to " +
				"publish, and 30 are allowed an hour. " + RetryAdvice(err, now)
		}
		return "Too many requests. " + RetryAdvice(err, now)
	case http.StatusServiceUnavailable:
		if code == buildsBusyCode {
			return "Volcano is busy building other releases, so nothing was shared. " + RetryAdvice(err, now)
		}
		return "Volcano is not available right now. " + RetryAdvice(err, now)
	default:
		return ""
	}
}

func forbiddenHint(op operation, code string, err error) string {
	switch {
	case code == OperatorDisabledCode:
		return "Volcano took a share of this project down, so the project cannot be shared, and that share cannot " +
			"be switched on again or deleted, until Volcano lifts it. Contact support if you think this is a mistake."
	case code == ReshareNotAllowedCode:
		return "This project is a copy of someone else's shared project, and its source does not let copies be " +
			"shared again. Ask the person who shared it to allow re-sharing."
	case code == SourceUnavailableCode:
		return "This project is a copy of a shared project, and the share it was made from is not available to " +
			"share from right now, so neither is this copy. Try again later."
	case op != opRead && api.LastRefusal().ProjectAccessToken:
		// The refusal records which credential the request carried, so this
		// only says so when it was a project access token and the call is one
		// a person alone may make.
		return "Project access tokens cannot share a project or change how it is shared. " +
			"Run this command signed in with " + rootCommand("login") + "."
	case op != opDisable && strings.Contains(strings.ToLower(errorMessage(err)), readOnlyMessage):
		return "Your account is read-only, so the only change it can make is switching sharing off with " +
			rootCommand("share off") + "."
	default:
		return ""
	}
}

// RetryAdvice says when to try again, from a refusal's Retry-After.
func RetryAdvice(err error, now time.Time) string {
	at, ok := api.RetryAt(err, now)
	if !ok {
		return "Try again shortly."
	}
	wait := at.Sub(now).Round(time.Second)
	if wait <= 0 {
		return "Try again now."
	}
	return fmt.Sprintf("Try again in %s.", wait)
}

// rootCommand names a command that lives at the top of the tree. login and
// share are not part of the cloud tree, so the cloud prefix would send the
// user to a command that does not exist.
func rootCommand(command string) string {
	return cliruntime.CommandPath(cliruntime.Deps{}, command)
}

// errorMessage is the API's message for a refusal, or the error's text.
func errorMessage(err error) string {
	if apiErr, ok := errors.AsType[*api.Error](err); ok {
		return apiErr.Message
	}
	return err.Error()
}

// Optional returns a pointer to a trimmed value, or nil for an empty one, for
// the API's optional fields.
func Optional(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}
