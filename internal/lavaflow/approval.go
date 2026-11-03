package lavaflow

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/apiclient"
	"github.com/Kong/volcano-cli/internal/config"
)

// The approval route's refusal codes the CLI words itself.
const (
	codePermissionMissing     = "permission_missing"
	codeHeadMoved             = "head_moved"
	codeNotWaitingForApproval = "not_waiting_for_approval"
)

// ErrPersonApproves refuses approving a merge with a project access token.
// Whether a fix merges is a person's decision, so the API takes it from a
// person's session only, and the CLI says so before reading the run.
var ErrPersonApproves = errors.New("approving a LavaFlow merge needs a person's session: " +
	"a project access token can't")

// ErrOwnerApproves reports the approval route's code-less 403: the caller
// isn't the project's owner.
var ErrOwnerApproves = errors.New("only the project's owner can approve a LavaFlow merge")

// ErrOwnerCantPush reports permission_missing: LavaFlow merges with the
// owner's GitHub account, which can't push to the repository.
var ErrOwnerCantPush = errors.New("LavaFlow can't merge for this project: " +
	"the project owner's GitHub account can't push to its repository")

// connectGitHubDocs is the docs section on connecting GitHub, which a
// permission_missing refusal points to.
const connectGitHubDocs = "platform/projects/git-deploy.md#1-connect-github"

// pinApprovalToHead sends the run's merge.head_sha with an approval, so a
// pull request that moved after the person read it gets 409 head_moved
// instead of merging what they didn't see. Security requires the pin; this is
// the one line to change if that changes.
const pinApprovalToHead = true

// ErrHeadMoved reports head_moved: the pull request changed after the run
// was read, so the approval no longer names what the person saw.
var ErrHeadMoved = errors.New("the fix's pull request changed since the run was read")

// ErrMergingNotAvailable reports feature_unavailable for merging: LavaFlow
// doesn't merge for this project yet.
var ErrMergingNotAvailable = errors.New("LavaFlow merging isn't available for this project yet")

// ErrMergingNotOnAPI reports an API without the approval route: a platform
// release older than LavaFlow's merging.
var ErrMergingNotOnAPI = errors.New("LavaFlow merging isn't available on this Volcano API")

// RequirePerson refuses a project access token before anything is read, as
// the API refuses it before reading the request.
func (s Service) RequirePerson() error {
	cfg, err := s.sessions.Config()
	if err != nil {
		return err
	}
	if config.IsProjectToken(cfg.Token()) {
		return personRefusal(cfg, ErrPersonApproves, config.ErrAccountTokenRequired)
	}
	return nil
}

// AwaitsApproval says why a run's merge can't be approved, or nil when it
// waits for a person. The error keeps a 409 as its cause, as the API would.
func AwaitsApproval(run *apiclient.LavaflowRun) error {
	switch {
	case run.Merge != nil && run.Merge.Status == apiclient.LavaflowMergeStatusWaitingForApproval:
		return nil
	case run.Merge != nil && run.Merge.Status == apiclient.LavaflowMergeStatusMerged:
		return conflict(fmt.Sprintf("LavaFlow run %s's fix was already merged", run.Id))
	default:
		return conflict(fmt.Sprintf("LavaFlow run %s isn't waiting for approval", run.Id))
	}
}

func conflict(message string) error {
	return api.WithCause(errors.New(message), &api.Error{StatusCode: http.StatusConflict, Message: message})
}

// Approve approves the merge the current project's run waits for, pinned to
// the pull request head the run reported, and returns the run after it.
func (s Service) Approve(ctx context.Context, run *apiclient.LavaflowRun) (*apiclient.LavaflowRun, error) {
	authenticated, err := s.project()
	if err != nil {
		return nil, err
	}
	var head *string
	if pinApprovalToHead && run.Merge != nil {
		head = run.Merge.HeadSha
	}
	approved, err := authenticated.API.ApproveLavaflowMerge(ctx, authenticated.ProjectID, run.Id, head)
	// Read by code, not status. Anything else keeps the API's words.
	switch code := api.Code(err); {
	case err == nil:
		return approved, nil
	case code == codePersonRequired:
		return nil, personRefusal(authenticated.Config, ErrPersonApproves, err)
	case code == codeFeatureUnavailable:
		return nil, api.WithCause(ErrMergingNotAvailable, err)
	case code == codePlanRequired:
		return nil, api.WithCause(ErrPlanRequired, err)
	case code == codePermissionMissing:
		refusal := ErrOwnerCantPush
		if message := api.Message(err); message != "" {
			refusal = fmt.Errorf("%w (%s)", ErrOwnerCantPush, message)
		}
		return nil, api.WithCause(fmt.Errorf("%w. To connect GitHub, see 'volcano docs get %s'", refusal, connectGitHubDocs), err)
	case code == codeHeadMoved:
		return nil, api.WithCause(ErrHeadMoved, err)
	case code == codeNotWaitingForApproval:
		return nil, api.WithCause(fmt.Errorf("LavaFlow run %s isn't waiting for approval", run.Id), err)
	case code == "" && api.Status(err) == http.StatusForbidden:
		return nil, api.WithCause(ErrOwnerApproves, err)
	case api.RouteMissing(err):
		return nil, api.WithCause(ErrMergingNotOnAPI, err)
	default:
		return nil, fmt.Errorf("failed to approve LavaFlow run %s's merge: %w", run.Id, err)
	}
}
