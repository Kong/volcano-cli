package projectconfig

import (
	"context"
	"errors"

	"github.com/Kong/volcano-cli/internal/apiclient"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	clisession "github.com/Kong/volcano-cli/internal/session"
)

// Service uploads and downloads declarative project configuration. All
// reconciliation happens server-side.
type Service struct {
	sessions clisession.Factory
}

// NewService returns a projectconfig service.
func NewService(deps cliruntime.Deps) Service {
	return Service{sessions: clisession.NewFactory(deps)}
}

// Deploy uploads the manifest to the server, which validates and reconciles
// the project configuration. With dryRun the server only reports projected
// actions.
func (s Service) Deploy(ctx context.Context, manifest *Manifest, dryRun bool) (*apiclient.ProjectConfigApplyResult, error) {
	if manifest == nil {
		return nil, errors.New("manifest is required")
	}

	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	body, err := manifest.uploadBody()
	if err != nil {
		return nil, err
	}
	return authenticated.API.ApplyProjectConfig(ctx, authenticated.ProjectID, body, dryRun)
}

// PullResult is a downloaded manifest plus what the CLI had to remove because
// the server should not have exported it.
type PullResult struct {
	Manifest []byte

	// Removed lists the sections, RemovedVariables or
	// RemovedNotificationsWebhook, taken out before the manifest was returned.
	Removed []string
}

// Pull downloads the project's current configuration as the server-rendered
// canonical YAML manifest, with variable values and the webhook URL removed if
// the server included them (see sanitizePulledManifest).
func (s Service) Pull(ctx context.Context) (PullResult, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return PullResult{}, err
	}
	manifest, err := authenticated.API.GetProjectConfigYAML(ctx, authenticated.ProjectID)
	if err != nil {
		return PullResult{}, err
	}
	sanitized, removed, err := sanitizePulledManifest(manifest)
	if err != nil {
		return PullResult{}, err
	}
	return PullResult{Manifest: sanitized, Removed: removed}, nil
}
