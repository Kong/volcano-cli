package projectconfig

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	apicommon "github.com/Kong/volcano-cli/internal/apiclient/common"
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
func (s Service) Deploy(ctx context.Context, manifest *Manifest, dryRun bool) (*apicommon.ProjectConfigApplyResult, error) {
	if manifest == nil {
		return nil, errors.New("manifest is required")
	}

	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	body, err := json.Marshal(manifest)
	if err != nil {
		return nil, fmt.Errorf("failed to encode manifest: %w", err)
	}
	return authenticated.API.ApplyProjectConfig(ctx, authenticated.ProjectID, body, dryRun)
}

// Pull downloads the project's current configuration as the server-rendered
// canonical YAML manifest.
func (s Service) Pull(ctx context.Context) ([]byte, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}
	return authenticated.API.GetProjectConfigYAML(ctx, authenticated.ProjectID)
}
