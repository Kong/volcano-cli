// Package session resolves the authenticated client and active project for a command run.
package session

import (
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/config"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

// Factory builds API clients and authenticated sessions.
type Factory struct {
	deps cliruntime.Deps
}

// Session contains config and an API client for authenticated workflows.
type Session struct {
	Config            *config.Config
	API               *api.Client
	apiURL            string
	apiClientForToken func(string) (*api.Client, error)
}

// ProjectSession contains config, an API client, and the current project ID.
type ProjectSession struct {
	Config            *config.Config
	API               *api.Client
	ProjectID         uuid.UUID
	APIURL            string
	apiClientForToken func(string) (*api.Client, error)
}

// NewFactory returns a session factory using the provided runtime dependencies.
func NewFactory(deps cliruntime.Deps) Factory {
	return Factory{deps: deps}
}

// APIWithToken constructs an API client for the same API URL and runtime
// dependencies as this project session, using the provided bearer token.
func (s *ProjectSession) APIWithToken(token string) (*api.Client, error) {
	if s.apiClientForToken == nil {
		return nil, errors.New("api client factory is unavailable")
	}
	return s.apiClientForToken(token)
}

// APIWithToken constructs an API client for the same API URL and runtime
// dependencies as this authenticated session, using the provided bearer token.
func (s *Session) APIWithToken(token string) (*api.Client, error) {
	if s == nil || s.apiClientForToken == nil {
		return nil, errors.New("api client factory is unavailable")
	}
	return s.apiClientForToken(token)
}

// Config loads CLI config with runtime overrides.
func (f Factory) Config() (*config.Config, error) {
	loadConfig := config.Load
	if f.deps.ConfigLoader != nil {
		loadConfig = f.deps.ConfigLoader
	}

	cfg, err := loadConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}
	return cfg, nil
}

// APIURL returns the cloud API URL with runtime overrides applied.
func (f Factory) APIURL(cfg *config.Config) string {
	if f.deps.APIBaseURL != "" {
		return f.deps.APIBaseURL
	}
	return cfg.APIURL()
}

// APIClient constructs an API client with runtime overrides. In local mode it
// sends no credential: local is a single-tenant sandbox with no attacker model,
// and the local server defaults an absent credential to the pre-provisioned
// local user, so every local command behaves the same way. Cloud is unaffected.
func (f Factory) APIClient(apiURL, token string) (*api.Client, error) {
	if f.deps.LocalMode {
		token = ""
	}
	opts := make([]api.Option, 0, 1)
	if f.deps.HTTPClient != nil {
		opts = append(opts, api.WithHTTPClient(f.deps.HTTPClient))
	}
	return api.NewClient(apiURL, token, opts...)
}

// Authenticated loads config, requires authentication, and builds an API client.
func (f Factory) Authenticated() (*Session, error) {
	cfg, err := f.Config()
	if err != nil {
		return nil, err
	}

	if err := cfg.RequireAuth(); err != nil {
		return nil, err
	}

	apiURL := f.APIURL(cfg)
	client, err := f.APIClient(apiURL, cfg.Token())
	if err != nil {
		return nil, fmt.Errorf("failed to create api client: %w", err)
	}

	return &Session{
		Config: cfg,
		API:    client,
		apiURL: apiURL,
		apiClientForToken: func(token string) (*api.Client, error) {
			return f.APIClient(apiURL, token)
		},
	}, nil
}

// CurrentProject builds an authenticated API client for the current project.
func (f Factory) CurrentProject() (*ProjectSession, error) {
	authenticated, err := f.Authenticated()
	if err != nil {
		return nil, err
	}

	if err := authenticated.Config.RequireProject(); err != nil {
		return nil, err
	}

	projectIDText := strings.TrimSpace(authenticated.Config.ProjectID())
	projectID, err := uuid.Parse(projectIDText)
	if err != nil {
		return nil, fmt.Errorf("invalid project ID %q: %w", authenticated.Config.ProjectID(), err)
	}

	return &ProjectSession{
		Config:    authenticated.Config,
		API:       authenticated.API,
		ProjectID: projectID,
		APIURL:    authenticated.apiURL,
		apiClientForToken: func(token string) (*api.Client, error) {
			return f.APIClient(authenticated.apiURL, token)
		},
	}, nil
}
