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
	"github.com/Kong/volcano-cli/internal/config"
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
	authenticated, err := s.accountSession()
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
	authenticated, err := s.accountSession()
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
	authenticated, err := s.accountSession()
	if err != nil {
		return nil, err
	}
	return resolve(ctx, authenticated, identifier)
}

// Usage returns the daily request counts for a token the caller already
// resolved.
//
// It takes the token rather than the name the user typed because Get has
// resolved that by the time usage is wanted: resolving it again costs a second
// paginated walk, and a transient failure there would report a token that does
// not exist moments after reading it.
func (s Service) Usage(ctx context.Context, token *apiclient.ProjectAccessToken, days int) (*apiclient.ProjectAccessTokenUsage, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	usage, err := authenticated.API.GetAccessTokenUsage(ctx, authenticated.ProjectID, token.Id, days)
	if err != nil {
		return nil, fmt.Errorf("failed to get usage for access token %q: %w", token.Name, err)
	}
	return usage, nil
}

// ProjectUsage returns the daily request counts for every access token in the
// current project, revoked tokens included.
//
// The API admits a project access token on the usage reads, so this one is not
// account-scoped: a CI job holding nothing but the pt- token it runs with can
// still report its own consumption.
func (s Service) ProjectUsage(ctx context.Context, days int) ([]apiclient.ProjectAccessTokenUsage, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	usage, err := authenticated.API.ListAccessTokensUsage(ctx, authenticated.ProjectID, days)
	if err != nil {
		return nil, fmt.Errorf("failed to get access token usage: %w", err)
	}
	return usage, nil
}

// Revoke revokes a token the caller already resolved, so the token that was
// confirmed is the token that is revoked.
func (s Service) Revoke(ctx context.Context, token *apiclient.ProjectAccessToken) error {
	authenticated, err := s.accountSession()
	if err != nil {
		return err
	}

	if err := authenticated.API.RevokeAccessToken(ctx, authenticated.ProjectID, token.Id); err != nil {
		return fmt.Errorf("failed to revoke access token %q: %w", token.Name, err)
	}
	return nil
}

// accountSession resolves the current project and rejects a project access
// token. Minting, revoking, and reading a credential's record are account
// operations, so a pt- token would only earn a 403 from the API. The usage
// reads are not among them and use the session directly.
//
// Only the missing credential is named for this group; anything else that went
// wrong resolving the session says enough on its own.
func (s Service) accountSession() (*clisession.ProjectSession, error) {
	authenticated, err := s.sessions.AccountScopedProject()
	if errors.Is(err, config.ErrAccountTokenRequired) {
		return nil, fmt.Errorf("failed to manage access tokens: %w", err)
	}
	if err != nil {
		return nil, err
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

	// A name can be held by more than one token: revoking frees it for reuse, so
	// after a rotation the project has a live `ci-deploy` and the revoked one it
	// replaced. Prefer the usable token explicitly rather than relying on the
	// API returning it first — it does today, newest first, but a silent
	// dependency on that ordering means `revoke ci-deploy` could one day revoke
	// the already-dead token, report success, and leave the live one working.
	var fallback *apiclient.ProjectAccessToken

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
			if tokens.Data[i].Name != target {
				continue
			}
			if tokens.Data[i].Status == apiclient.ProjectAccessTokenStatusActive {
				return &tokens.Data[i], nil
			}
			if fallback == nil {
				fallback = &tokens.Data[i]
			}
		}
		if !tokens.HasMore || len(tokens.Data) == 0 || !progressed {
			// Walked the whole project: an unusable match is still a match, so a
			// repeated revoke reports the token rather than denying it exists.
			if fallback != nil {
				return fallback, nil
			}
			return nil, api.ErrNotFound
		}
	}
	if fallback != nil {
		return fallback, nil
	}
	// Ran out of pages rather than out of tokens, which is a different problem
	// from the name not existing and worth saying so.
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
