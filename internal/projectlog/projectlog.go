// Package projectlog searches, tails and counts a project's runtime logs across
// its functions, frontends or databases, with the filters the log routes share.
package projectlog

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/apiclient"
	clidatabase "github.com/Kong/volcano-cli/internal/database"
	clidurable "github.com/Kong/volcano-cli/internal/durable"
	clifrontend "github.com/Kong/volcano-cli/internal/frontend"
	clifunction "github.com/Kong/volcano-cli/internal/function"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	clisession "github.com/Kong/volcano-cli/internal/session"
)

// Resource types a log read selects, as the API names them.
const (
	TypeFunction = "function"
	TypeFrontend = "frontend"
	TypeDatabase = "database"
)

// Function kinds a function log read can narrow to.
const (
	KindStandard = "standard"
	KindDurable  = "durable"
)

// maxResources is the API's limit on resource IDs in one read.
const maxResources = 25

// Levels are the log levels the API records, lowest first.
var Levels = []string{"trace", "debug", "info", "warn", "error", "fatal"}

// Selector chooses whose logs to read: every resource of one type, or the
// named ones.
type Selector struct {
	Type string
	// Kind narrows functions to standard or durable ones; empty reads both.
	Kind string
	// Names are resource names or IDs. Databases are named only by name.
	Names []string
}

// Filters narrow a read the same way on every log route.
type Filters struct {
	// Query is a log query, such as `timeout OR "connection reset"`.
	Query      string
	Level      string
	Region     string
	Invocation string
	Start      *time.Time
	End        *time.Time
}

// Service reads the current project's logs.
type Service struct {
	deps     cliruntime.Deps
	sessions clisession.Factory
}

// NewService returns a project log service.
func NewService(deps cliruntime.Deps) Service {
	return Service{deps: deps, sessions: clisession.NewFactory(deps)}
}

// Query checks the selector and filters, resolves each name to its ID, and
// returns the read they describe.
func (s Service) Query(ctx context.Context, selector Selector, filters Filters) (api.LogQuery, error) {
	if err := validate(selector, filters); err != nil {
		return api.LogQuery{}, err
	}
	ids := make([]uuid.UUID, 0, len(selector.Names))
	for _, name := range selector.Names {
		id, err := s.resolve(ctx, selector, name)
		if err != nil {
			return api.LogQuery{}, err
		}
		if !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	return api.LogQuery{
		Type:  selector.Type,
		IDs:   ids,
		Kind:  selector.Kind,
		Q:     BuildQuery(filters),
		Start: filters.Start,
		End:   filters.End,
	}, nil
}

func validate(selector Selector, filters Filters) error {
	switch selector.Type {
	case TypeFunction, TypeFrontend, TypeDatabase:
	default:
		return fmt.Errorf("--type must be one of: %s, %s, %s, not %q", TypeFunction, TypeFrontend, TypeDatabase, selector.Type)
	}
	switch {
	case selector.Kind != "" && selector.Type != TypeFunction:
		return errors.New("--kind applies only to --type function")
	case selector.Kind != "" && selector.Kind != KindStandard && selector.Kind != KindDurable:
		return fmt.Errorf("--kind must be %s or %s, not %q", KindStandard, KindDurable, selector.Kind)
	case len(selector.Names) > maxResources:
		return fmt.Errorf("name at most %d resources at once, not %d", maxResources, len(selector.Names))
	case filters.Level != "" && !slices.Contains(Levels, filters.Level):
		return fmt.Errorf("--level must be one of: %s, not %q", strings.Join(Levels, ", "), filters.Level)
	case filters.Start != nil && filters.End != nil && !filters.Start.Before(*filters.End):
		return errors.New("the start of the time window must be before its end")
	}
	return nil
}

func (s Service) resolve(ctx context.Context, selector Selector, name string) (uuid.UUID, error) {
	switch selector.Type {
	case TypeFrontend:
		frontend, err := clifrontend.NewService(s.deps).Resolve(ctx, name)
		if err != nil {
			return uuid.Nil, err
		}
		return frontend.Id, nil
	case TypeDatabase:
		database, err := clidatabase.NewService(s.deps).Get(ctx, name)
		if err != nil {
			return uuid.Nil, err
		}
		return database.Id, nil
	default:
		return s.resolveFunction(ctx, selector.Kind, name)
	}
}

// resolveFunction finds a function of the given kind. With no kind, a name
// that is not a standard function is looked up among durable ones, since the
// read covers both.
func (s Service) resolveFunction(ctx context.Context, kind, name string) (uuid.UUID, error) {
	if kind != KindDurable {
		function, err := clifunction.NewService(s.deps).Resolve(ctx, name)
		if err == nil {
			return function.Id, nil
		}
		if kind == KindStandard || !notFound(err) {
			return uuid.Nil, err
		}
	}
	durable, err := clidurable.NewService(s.deps).Get(ctx, name)
	if err != nil {
		return uuid.Nil, err
	}
	return durable.Id, nil
}

func notFound(err error) bool {
	return errors.Is(err, api.ErrNotFound) || api.Status(err) == http.StatusNotFound
}

// BuildQuery joins the free-text query and the field filters into one log
// query. Terms are ANDed, and the free-text query is parenthesized so an OR in
// it cannot swallow a filter.
func BuildQuery(filters Filters) string {
	var terms []string
	if query := strings.TrimSpace(filters.Query); query != "" {
		terms = append(terms, "("+query+")")
	}
	for _, field := range []struct{ name, value string }{
		{"level", filters.Level},
		{"region", filters.Region},
		{"invocation.id", filters.Invocation},
	} {
		if value := strings.TrimSpace(field.value); value != "" {
			terms = append(terms, field.name+":"+quote(value))
		}
	}
	return strings.Join(terms, " ")
}

func quote(value string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value) + `"`
}

// Search returns one page of matching logs, newest first.
func (s Service) Search(ctx context.Context, query api.LogQuery, limit int, cursor string) (*apiclient.LogSearchResponse, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}
	logs, err := authenticated.API.SearchLogs(ctx, authenticated.ProjectID, query, limit, cursor)
	if err != nil {
		return nil, fmt.Errorf("failed to search logs: %w", err)
	}
	return logs, nil
}

// Stream live-tails matching logs, resuming after lastEventID when it is set.
func (s Service) Stream(ctx context.Context, query api.LogQuery, limit int, lastEventID string) (*api.ProjectLogStream, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}
	stream, err := authenticated.API.StreamLogs(ctx, authenticated.ProjectID, query, limit, lastEventID)
	if err != nil {
		return nil, fmt.Errorf("failed to stream logs: %w", err)
	}
	return stream, nil
}

// Activity returns bucketed counts of matching logs.
func (s Service) Activity(ctx context.Context, query api.LogQuery, buckets int) (*apiclient.LogActivityResponse, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}
	activity, err := authenticated.API.LogActivity(ctx, authenticated.ProjectID, query, buckets)
	if err != nil {
		return nil, fmt.Errorf("failed to count logs: %w", err)
	}
	return activity, nil
}

// maxPage is the API's largest log search page.
const maxPage = 1000

// Each pages through matching logs, newest first, handing each page to visit
// until limit logs have been visited or none remain. It reports whether more
// may match than were visited.
//
// A page can hold fewer logs than asked for, or none, while more remain, so
// paging stops on the API's has_more rather than on a short page.
func (s Service) Each(ctx context.Context, query api.LogQuery, limit int, visit func([]apiclient.LogSearchEvent) error) (bool, error) {
	cursor := ""
	for remaining := limit; remaining > 0; {
		page, err := s.Search(ctx, query, min(remaining, maxPage), cursor)
		if err != nil {
			return false, err
		}
		events := page.Data
		if len(events) > remaining {
			events = events[:remaining]
		}
		if err := visit(events); err != nil {
			return false, err
		}
		remaining -= len(events)
		if !page.HasMore || page.NextCursor == nil || strings.TrimSpace(*page.NextCursor) == "" {
			return false, nil
		}
		cursor = strings.TrimSpace(*page.NextCursor)
	}
	return true, nil
}
