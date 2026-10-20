// Package deploylink performs authenticated Volcano deploy link workflows.
//
// A deploy link lets anyone deploy the public GitHub repository the current
// project is connected to. The repository comes from the project's Git
// connection, so none of these calls name one.
package deploylink

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/apiclient"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	clisession "github.com/Kong/volcano-cli/internal/session"
)

// Service performs deploy link workflows against the current project.
type Service struct {
	sessions clisession.Factory
}

// NewService returns a deploy link service.
func NewService(deps cliruntime.Deps) Service {
	return Service{sessions: clisession.NewFactory(deps)}
}

// CreateInput is what a new link follows. An empty field leaves the setting to
// the API: the project's production branch, the repository root, and a name
// Volcano chooses.
type CreateInput struct {
	Branch        string
	Path          string
	SuggestedName string
}

// List returns the current project's deploy links, newest first.
func (s Service) List(ctx context.Context) ([]apiclient.DeployLink, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}
	links, err := authenticated.API.ListDeployLinks(ctx, authenticated.ProjectID)
	if err != nil {
		return nil, s.explain(opRead, "list deploy links", err)
	}
	return links.DeployLinks, nil
}

// Create creates a deploy link for the repository the current project is
// connected to.
func (s Service) Create(ctx context.Context, input CreateInput) (*apiclient.DeployLink, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}
	link, err := authenticated.API.CreateDeployLink(ctx, authenticated.ProjectID, apiclient.CreateDeployLinkRequest{
		Branch:        optional(input.Branch),
		Path:          optional(input.Path),
		SuggestedName: optional(input.SuggestedName),
	})
	if err != nil {
		return nil, s.explain(opCreate, "create the deploy link", err)
	}
	return link, nil
}

// Get returns one deploy link in the current project.
func (s Service) Get(ctx context.Context, linkID string) (*apiclient.DeployLink, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}
	link, err := authenticated.API.GetDeployLink(ctx, authenticated.ProjectID, linkID)
	if err != nil {
		return nil, s.explain(opRead, fmt.Sprintf("get deploy link %q", linkID), err)
	}
	return link, nil
}

// Stats returns a link's click and deploy counts. A days of zero leaves the
// window to the API.
func (s Service) Stats(ctx context.Context, linkID string, days int) (*apiclient.DeployLinkStats, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}
	stats, err := authenticated.API.GetDeployLinkStats(ctx, authenticated.ProjectID, linkID, days)
	if err != nil {
		return nil, s.explain(opRead, fmt.Sprintf("get the stats of deploy link %q", linkID), err)
	}
	return stats, nil
}

// SetStatus switches a link off or on. The API verifies the link again before
// it switches one on.
func (s Service) SetStatus(
	ctx context.Context, linkID string, status apiclient.UpdateDeployLinkRequestStatus,
) (*apiclient.DeployLink, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}
	link, err := authenticated.API.UpdateDeployLink(ctx, authenticated.ProjectID, linkID,
		apiclient.UpdateDeployLinkRequest{Status: &status})
	if err != nil {
		op, action := opEnable, "enable"
		if status == apiclient.UpdateDeployLinkRequestStatusDisabled {
			op, action = opDisable, "disable"
		}
		return nil, s.explain(op, fmt.Sprintf("%s deploy link %q", action, linkID), err)
	}
	return link, nil
}

// Delete deletes a link and its recorded events. Its address stops working.
func (s Service) Delete(ctx context.Context, linkID string) error {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return err
	}
	if err := authenticated.API.DeleteDeployLink(ctx, authenticated.ProjectID, linkID); err != nil {
		return s.explain(opDelete, fmt.Sprintf("delete deploy link %q", linkID), err)
	}
	return nil
}

// operation says what a failed call was, because the same status means
// different things to different calls.
type operation int

const (
	// opRead is a list, get or stats, which a project access token may make.
	opRead operation = iota
	// opCreate adds a link and verifies the repository.
	opCreate
	// opEnable switches a link on, which verifies the repository again.
	opEnable
	// opDisable switches a link off and needs no verification.
	opDisable
	// opDelete removes a link.
	opDelete
)

// featureUnavailableCode is the code a closed release gate answers creating or
// enabling a link with: a 404 when creating, a 400 when editing or enabling.
// Neither is a missing link or a bad request.
const featureUnavailableCode = "feature_unavailable"

// unavailableError reports that deploy links are not offered here. It keeps the
// API's error for callers that read the status.
type unavailableError struct{ err error }

func (e *unavailableError) Error() string {
	return "Deploy links are not available in this environment."
}

func (e *unavailableError) Unwrap() error { return e.err }

// explain says what was being done and, for the refusals the API documents for
// deploy links, what to do about it. The API's own message is the only server
// text shown; the advice is the CLI's.
func (s Service) explain(op operation, action string, err error) error {
	status := api.Status(err)
	if (status == http.StatusNotFound || status == http.StatusBadRequest) && api.Code(err) == featureUnavailableCode {
		return fmt.Errorf("failed to %s: %w", action, &unavailableError{err: err})
	}
	hint := hintFor(op, status)
	if hint == "" {
		return fmt.Errorf("failed to %s: %w", action, err)
	}
	return fmt.Errorf("failed to %s: %w\n\n%s", action, err, hint)
}

func hintFor(op operation, status int) string {
	switch status {
	case http.StatusUnauthorized:
		return "If GitHub is the cause, reconnect GitHub for this project and try again. " +
			"Otherwise sign in again with " + rootCommand("login") + "."
	case http.StatusForbidden:
		// The refusal records which credential the request carried, so this
		// only says so when it was a project access token and the call is one
		// a person alone may make.
		if op != opRead && api.LastRefusal().ProjectAccessToken {
			return "Project access tokens cannot create, change or delete deploy links. " +
				"Run this command signed in with " + rootCommand("login") + "."
		}
		return ""
	case http.StatusConflict:
		hint := "A deploy link needs the project connected to the link's repository, and a project holds at most " +
			"20 links. See " + rootCommand("git status") + " and " + rootCommand("git connect") + "."
		if op == opEnable || op == opDisable {
			hint += " If the link changed while it was being saved, run the command again."
		}
		return hint
	case http.StatusUnprocessableEntity:
		return "A deploy link needs a public repository that the connected GitHub account can push to, " +
			"an existing branch, a valid repository configuration, and a frontend or function to deploy at " +
			"the branch's latest commit. An empty repository cannot be deployed."
	case http.StatusServiceUnavailable:
		return "GitHub is rate limiting or not available right now, or the GitHub integration is not " +
			"configured. Try again shortly."
	default:
		return ""
	}
}

// rootCommand names a command that lives at the top of the tree. login and git
// are not part of the cloud tree, so the cloud prefix would send the user to a
// command that does not exist.
func rootCommand(command string) string {
	return cliruntime.CommandPath(cliruntime.Deps{}, command)
}

func optional(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}
