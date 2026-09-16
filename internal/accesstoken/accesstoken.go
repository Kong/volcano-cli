// Package accesstoken holds the project access token workflows behind the
// volcano access-tokens subcommands.
package accesstoken

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/apiclient"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	clisession "github.com/Kong/volcano-cli/internal/session"
)

// Scope values a token can be minted with.
const (
	ScopeFull     = string(apiclient.ProjectAccessTokenScopeFull)
	ScopeReadOnly = string(apiclient.ProjectAccessTokenScopeReadOnly)
)

// DefaultUsageDays matches the API default for the usage series.
const DefaultUsageDays = 30

// MaxUsageDays matches the API's ceiling, which is also how long per-day
// counts are retained.
const MaxUsageDays = 60

// maxResolvePages caps the pagination walk in resolveToken so a server that
// keeps reporting HasMore=true cannot hang the CLI indefinitely.
const maxResolvePages = 1000

// Service performs authenticated Volcano project access token workflows.
type Service struct {
	sessions clisession.Factory
}

// NewService returns a project access token service.
func NewService(deps cliruntime.Deps) Service {
	return Service{sessions: clisession.NewFactory(deps)}
}

// Scopes returns the scopes a token can be minted with, for help text and
// flag validation.
func Scopes() []string {
	return []string{ScopeFull, ScopeReadOnly}
}

// ValidateUsageDays rejects a window the API does not accept.
//
// The lower bound matters more than the upper one: a non-positive value used to
// be dropped from the request entirely, so asking for -5 days quietly returned
// the 30-day default with a success exit code — a plausible answer to a
// different question.
func ValidateUsageDays(days int) error {
	if days < 1 || days > MaxUsageDays {
		return fmt.Errorf("invalid --days %d: expected 1 to %d", days, MaxUsageDays)
	}
	return nil
}

// ValidateScope rejects a scope the API does not define, so a typo fails
// before the request instead of as an opaque 400.
func ValidateScope(scope string) error {
	switch strings.TrimSpace(scope) {
	case ScopeFull, ScopeReadOnly:
		return nil
	default:
		return fmt.Errorf("invalid scope %q: expected %s", scope, strings.Join(Scopes(), " or "))
	}
}

// Create mints one access token in the current project.
func (s Service) Create(ctx context.Context, input api.AccessTokenCreateInput) (*apiclient.CreatedProjectAccessToken, error) {
	authenticated, err := s.session()
	if err != nil {
		return nil, err
	}

	token, err := authenticated.API.CreateAccessToken(ctx, authenticated.ProjectID, input)
	if err != nil {
		return nil, fmt.Errorf("failed to create access token %q: %w", input.Name, err)
	}
	return token, nil
}

// ListPage returns one access token page in the current project.
func (s Service) ListPage(ctx context.Context, input api.AccessTokenListInput) (*apiclient.PaginatedProjectAccessTokens, error) {
	authenticated, err := s.session()
	if err != nil {
		return nil, err
	}

	tokens, err := authenticated.API.ListAccessTokens(ctx, authenticated.ProjectID, input)
	if err != nil {
		return nil, fmt.Errorf("failed to list access tokens: %w", err)
	}
	return tokens, nil
}

// Get returns one access token by name or ID.
func (s Service) Get(ctx context.Context, identifier string) (*apiclient.ProjectAccessToken, error) {
	authenticated, err := s.session()
	if err != nil {
		return nil, err
	}
	return resolve(ctx, authenticated, identifier)
}

// Usage returns the daily request counts for one access token by name or ID.
func (s Service) Usage(ctx context.Context, identifier string, days int) (*apiclient.ProjectAccessTokenUsage, error) {
	authenticated, err := s.session()
	if err != nil {
		return nil, err
	}

	token, err := resolve(ctx, authenticated, identifier)
	if err != nil {
		return nil, err
	}

	usage, err := authenticated.API.GetAccessTokenUsage(ctx, authenticated.ProjectID, token.Id, days)
	if err != nil {
		return nil, fmt.Errorf("failed to get usage for access token %q: %w", identifier, err)
	}
	return usage, nil
}

// ProjectUsage returns the daily request counts for every access token in the
// current project, revoked tokens included.
func (s Service) ProjectUsage(ctx context.Context, days int) ([]apiclient.ProjectAccessTokenUsage, error) {
	authenticated, err := s.session()
	if err != nil {
		return nil, err
	}

	usage, err := authenticated.API.ListAccessTokensUsage(ctx, authenticated.ProjectID, days)
	if err != nil {
		return nil, fmt.Errorf("failed to get access token usage: %w", err)
	}
	return usage, nil
}

// Revoke revokes one access token by name or ID and returns the revoked token.
func (s Service) Revoke(ctx context.Context, identifier string) (*apiclient.ProjectAccessToken, error) {
	authenticated, err := s.session()
	if err != nil {
		return nil, err
	}

	token, err := resolve(ctx, authenticated, identifier)
	if err != nil {
		return nil, err
	}

	if err := authenticated.API.RevokeAccessToken(ctx, authenticated.ProjectID, token.Id); err != nil {
		return nil, fmt.Errorf("failed to revoke access token %q: %w", identifier, err)
	}
	return token, nil
}

// session resolves the current project and rejects a project access token:
// minting and revoking credentials is an account operation, so a pt- token
// would only earn a 403 from the API.
func (s Service) session() (*clisession.ProjectSession, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}
	if err := authenticated.Config.RequireAccountToken(); err != nil {
		return nil, fmt.Errorf("failed to manage access tokens: %w", err)
	}
	return authenticated, nil
}

func resolve(ctx context.Context, authenticated *clisession.ProjectSession, identifier string) (*apiclient.ProjectAccessToken, error) {
	token, err := resolveToken(ctx, authenticated, identifier)
	if errors.Is(err, api.ErrNotFound) {
		return nil, fmt.Errorf("access token %q not found", identifier)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to resolve access token %q: %w", identifier, err)
	}
	return token, nil
}

// resolveToken looks a token up by ID, else by exact name. Revoked tokens are
// included: revoking one twice should report the token, not deny it exists,
// and its usage history outlives the revocation.
func resolveToken(ctx context.Context, authenticated *clisession.ProjectSession, identifier string) (*apiclient.ProjectAccessToken, error) {
	target := strings.TrimSpace(identifier)
	if target == "" {
		return nil, errors.New("access token identifier cannot be empty")
	}

	if id, err := uuid.Parse(target); err == nil {
		token, err := authenticated.API.GetAccessToken(ctx, authenticated.ProjectID, id)
		if err != nil {
			if api.Status(err) == http.StatusNotFound {
				return nil, api.ErrNotFound
			}
			return nil, err
		}
		return token, nil
	}

	seen := make(map[uuid.UUID]struct{})
	for page := api.DefaultPage; page < api.DefaultPage+maxResolvePages; page++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		tokens, err := authenticated.API.ListAccessTokens(ctx, authenticated.ProjectID, api.AccessTokenListInput{
			Page:           page,
			Limit:          api.DefaultLimit,
			Search:         target,
			IncludeRevoked: true,
		})
		if err != nil {
			return nil, err
		}
		if tokens == nil {
			return nil, api.ErrNotFound
		}
		progressed := false
		for i := range tokens.Data {
			if _, ok := seen[tokens.Data[i].Id]; ok {
				continue
			}
			seen[tokens.Data[i].Id] = struct{}{}
			progressed = true
			// search is a substring match, so only an exact name is a hit.
			if tokens.Data[i].Name == target {
				return &tokens.Data[i], nil
			}
		}
		if !tokens.HasMore || len(tokens.Data) == 0 || !progressed {
			return nil, api.ErrNotFound
		}
	}
	return nil, fmt.Errorf("gave up looking for access token after %d pages", maxResolvePages)
}

// ParseExpiry converts an RFC3339 expiry flag into the API's optional
// expires_at. An empty value means the token never expires.
func ParseExpiry(value string) (*time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil //nolint:nilnil // no expiry is a valid, distinct outcome from an error
	}
	expiresAt, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, fmt.Errorf("invalid expiry %q: expected an RFC3339 timestamp such as 2027-01-31T00:00:00Z", value)
	}
	if !expiresAt.After(time.Now()) {
		return nil, fmt.Errorf("expiry %q is in the past", value)
	}
	return &expiresAt, nil
}
