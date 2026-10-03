package function

import (
	"context"
	"fmt"
	"sort"

	"github.com/google/uuid"

	"github.com/Kong/volcano-cli/internal/api"
)

// RouteSource is a frontend path that forwards requests to a function.
type RouteSource struct {
	Frontend   string
	PathPrefix string
}

// RoutedFrom lists the frontend routes that forward to a function.
func (s Service) RoutedFrom(ctx context.Context, functionID uuid.UUID) ([]RouteSource, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	var sources []RouteSource
	for page := api.DefaultPage; page < api.DefaultPage+maxListPages; page++ {
		frontends, err := authenticated.API.ListFrontends(ctx, authenticated.ProjectID, page, api.DefaultLimit)
		if err != nil {
			return nil, fmt.Errorf("failed to list frontend routes: %w", err)
		}
		if frontends == nil {
			break
		}
		for _, frontend := range frontends.Data {
			for _, route := range frontend.FunctionRoutes {
				if route.FunctionId == functionID {
					sources = append(sources, RouteSource{Frontend: frontend.Name, PathPrefix: route.PathPrefix})
				}
			}
		}
		if !frontends.HasMore || len(frontends.Data) == 0 {
			break
		}
	}

	sort.Slice(sources, func(i, j int) bool {
		if sources[i].Frontend != sources[j].Frontend {
			return sources[i].Frontend < sources[j].Frontend
		}
		return sources[i].PathPrefix < sources[j].PathPrefix
	})
	return sources, nil
}
