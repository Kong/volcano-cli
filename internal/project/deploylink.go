package project

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/apiclient"
)

// CommitChangedCode is the code of the 409 that says a deploy link's branch
// moved after the preview. Nothing was created.
const CommitChangedCode = "commit_changed"

// ErrCommitChanged reports that the commit a project was to be pinned to is no
// longer the branch's latest.
var ErrCommitChanged = errors.New("the repository changed since the preview")

// IsCommitChanged reports whether err is the refusal to create a project from a
// commit that is no longer the deploy link's latest.
func IsCommitChanged(err error) bool {
	return errors.Is(err, ErrCommitChanged)
}

// PreviewDeployLink returns what deploying a link would create. The route is
// public, so the request carries no credential, but the call still needs a
// signed-in account so a command that cannot go on to create a project stops
// before it reaches the network.
func (s Service) PreviewDeployLink(ctx context.Context, linkID string) (*apiclient.DeployLinkPreview, error) {
	authenticated, err := s.sessions.AccountScoped()
	if err != nil {
		return nil, err
	}
	anonymous, err := authenticated.APIWithToken("")
	if err != nil {
		return nil, err
	}
	preview, err := anonymous.GetDeployLinkPreview(ctx, linkID)
	if err != nil {
		return nil, deployLinkError("preview the deploy link", err)
	}
	return preview, nil
}

// CreateFromDeployLink creates a project from a deploy link, pinned to the
// commit the preview returned. Installation is asynchronous: poll Get until the
// project's template installation is ready or failed.
func (s Service) CreateFromDeployLink(ctx context.Context, name, linkID, commitSHA string) (*apiclient.Project, error) {
	authenticated, err := s.sessions.AccountScoped()
	if err != nil {
		return nil, err
	}
	project, err := authenticated.API.CreateProjectFromDeployLink(ctx, name, linkID, commitSHA)
	if err != nil {
		return nil, deployLinkError(fmt.Sprintf("create project %q from the deploy link", name), err)
	}
	return project, nil
}

// deployLinkError says what failed and what to do about the refusals the API
// documents for deploy links. The API's own message is the only server text
// shown; the advice is the CLI's.
func deployLinkError(action string, err error) error {
	status, code := api.Status(err), api.Code(err)
	switch {
	case status == http.StatusConflict && code == CommitChangedCode:
		return fmt.Errorf("failed to %s: %w: %w", action, ErrCommitChanged, err)
	case status == http.StatusNotFound:
		// Every unavailable link answers the same, so no reason is given.
		return fmt.Errorf("failed to %s: This deploy link is not available.%w", action, hiddenError{err})
	case status == http.StatusBadRequest && code == "feature_unavailable":
		return fmt.Errorf("failed to %s: Deploy links are not available in this environment.%w", action, hiddenError{err})
	case status == http.StatusForbidden:
		return withHint(action, err, "If you have reached your plan's project limit (the Free plan allows one project), "+
			"delete a project you no longer need with `volcano projects delete`, or upgrade your plan, then try again.")
	case status == http.StatusTooManyRequests:
		return withHint(action, err, "Too many requests. Try again later.")
	case status == http.StatusServiceUnavailable:
		return withHint(action, err, "GitHub may be rate limiting or unavailable. Try again shortly.")
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
