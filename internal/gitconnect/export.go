package gitconnect

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/apiclient"
)

// ErrSourceExportUnavailable indicates the API stores no source to export.
// Local mode is the usual cause: it serves functions from a directory on the
// host rather than from stored deployments.
var ErrSourceExportUnavailable = errors.New("source export is not available on this API")

// SourceState reports whether the project's source of truth is Volcano or its
// connected repository, and how far an export has got in between.
func (s Service) SourceState(ctx context.Context) (*apiclient.ProjectSourceExportState, error) {
	authenticated, err := s.current()
	if err != nil {
		return nil, err
	}

	state, err := authenticated.API.GetProjectSourceExport(ctx, authenticated.ProjectID)
	if err != nil {
		return nil, sourceExportError(authenticated.ProjectID, err, "failed to read the project's source of truth")
	}
	return state, nil
}

// ExportSource writes the project's stored source to the repository in
// expected, as the first commit on its production branch, and pushes it.
//
// The request names only the branch, so the platform would export to whatever
// repository is bound when it arrives. Re-reading first keeps the command from
// publishing source to a repository the user was never shown, as Connect and
// Disconnect do; it narrows the window rather than closing it.
//
// The route's 404 covers a missing project, a project with no repository, and
// a GitHub connection or repository that can no longer be reached. The
// platform's message tells them apart, so it is reported as given.
func (s Service) ExportSource(ctx context.Context, expected *apiclient.ProjectGitConnection) (*apiclient.ProjectSourceExport, error) {
	authenticated, err := s.current()
	if err != nil {
		return nil, err
	}

	if err := s.confirmUnchanged(ctx, expected); err != nil {
		return nil, err
	}

	result, err := authenticated.API.ExportProjectSource(ctx, authenticated.ProjectID, expected.ProductionBranch)
	if err != nil {
		if api.Status(err) == http.StatusNotImplemented {
			return nil, ErrSourceExportUnavailable
		}
		return nil, classify(err, "failed to export the project's source")
	}
	return result, nil
}

// CancelSourceExport ends an incomplete export so direct deploys work again.
// The repository, and any commit already pushed to it, are unchanged.
func (s Service) CancelSourceExport(ctx context.Context) error {
	authenticated, err := s.current()
	if err != nil {
		return err
	}

	if err := authenticated.API.CancelProjectSourceExport(ctx, authenticated.ProjectID); err != nil {
		return sourceExportError(authenticated.ProjectID, err, "failed to cancel the source export")
	}
	return nil
}

// sourceExportError classifies a failure on the routes whose only 404 is a
// missing project.
func sourceExportError(projectID uuid.UUID, err error, action string) error {
	switch api.Status(err) {
	case http.StatusNotImplemented:
		return ErrSourceExportUnavailable
	case http.StatusNotFound:
		return api.WithCause(fmt.Errorf("%w: %s", ErrProjectNotFound, projectID), err)
	default:
		return classify(err, action)
	}
}
