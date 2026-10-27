package frontend

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/apiclient"
	clisession "github.com/Kong/volcano-cli/internal/session"
)

// Mount is a Frontend route, which serves a path of one frontend from another
// frontend of the project, and the frontend it mounts.
type Mount struct {
	apiclient.FrontendRoute
	// Target is nil when the mounted frontend is missing from the frontend
	// list, such as one deleted after the route was read.
	Target *apiclient.Frontend
}

// MountedBy is a Frontend route on another frontend that mounts a frontend.
type MountedBy struct {
	Frontend   string
	PathPrefix string
}

// MountTargets pairs Frontend routes already read with the frontends they
// mount.
func (s Service) MountTargets(ctx context.Context, routes []apiclient.FrontendRoute) ([]Mount, error) {
	if len(routes) == 0 {
		return nil, nil
	}
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}
	frontends, err := listFrontends(ctx, authenticated)
	if err != nil {
		return nil, err
	}
	return withTargets(routes, frontends), nil
}

// MountedBy returns the Frontend routes on other frontends that mount
// frontendID, which the server requires removed before it deletes the
// frontend.
func (s Service) MountedBy(ctx context.Context, frontendID uuid.UUID) ([]MountedBy, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}
	frontends, err := listFrontends(ctx, authenticated)
	if err != nil {
		return nil, err
	}
	var mounts []MountedBy
	for _, frontend := range frontends {
		for _, route := range frontend.FrontendRoutes {
			if route.TargetFrontendId == frontendID {
				mounts = append(mounts, MountedBy{Frontend: frontend.Name, PathPrefix: route.PathPrefix})
			}
		}
	}
	return mounts, nil
}

func (s Service) createMount(ctx context.Context, identifier string, input RouteInput) (*apiclient.Frontend, RouteResult, error) {
	authenticated, frontend, err := s.currentFrontend(ctx, identifier)
	if err != nil {
		return nil, RouteResult{}, err
	}
	frontends, err := listFrontends(ctx, authenticated)
	if err != nil {
		return nil, RouteResult{}, err
	}
	target, err := mountTarget(frontends, input.Frontend)
	if err != nil {
		return nil, RouteResult{}, err
	}
	created, err := authenticated.API.CreateFrontendRoute(ctx, authenticated.ProjectID, frontend.Id,
		apiclient.CreateFrontendRouteRequest{PathPrefix: input.PathPrefix, TargetFrontendId: target.Id})
	if err != nil {
		return nil, RouteResult{}, fmt.Errorf("failed to create route: %w", err)
	}
	return frontend, RouteResult{Frontend: &Mount{FrontendRoute: *created, Target: target}}, nil
}

func (s Service) updateMount(
	ctx context.Context, authenticated *clisession.ProjectSession, frontend *apiclient.Frontend,
	current apiclient.FrontendRoute, update RouteUpdate,
) (RouteResult, error) {
	if update.Function != nil || update.StripPrefix != nil {
		return RouteResult{}, fmt.Errorf("route %s on frontend %q mounts a frontend, so --function and --strip-prefix do not apply; "+
			"a route cannot change kind: delete it and create a function route instead", current.PathPrefix, frontend.Name)
	}
	frontends, err := listFrontends(ctx, authenticated)
	if err != nil {
		return RouteResult{}, err
	}
	request := apiclient.CreateFrontendRouteRequest{PathPrefix: current.PathPrefix, TargetFrontendId: current.TargetFrontendId}
	if update.PathPrefix != nil {
		request.PathPrefix = *update.PathPrefix
	}
	if update.Frontend != nil {
		target, err := mountTarget(frontends, *update.Frontend)
		if err != nil {
			return RouteResult{}, err
		}
		request.TargetFrontendId = target.Id
	}
	updated, err := authenticated.API.UpdateFrontendRoute(ctx, authenticated.ProjectID, frontend.Id, current.Id, request)
	if err != nil {
		return RouteResult{}, fmt.Errorf("failed to update route %s: %w", current.PathPrefix, err)
	}
	mounts := withTargets([]apiclient.FrontendRoute{*updated}, frontends)
	return RouteResult{Frontend: &mounts[0]}, nil
}

// listMounts lists a frontend's Frontend routes. A server that predates
// Frontend routes has no such endpoint and answers 404 for a frontend that was
// just found, which reads as no routes.
func listMounts(ctx context.Context, authenticated *clisession.ProjectSession, frontend *apiclient.Frontend) ([]apiclient.FrontendRoute, error) {
	routes, err := authenticated.API.ListFrontendRoutes(ctx, authenticated.ProjectID, frontend.Id)
	if api.Status(err) == http.StatusNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to list Frontend routes for frontend %q: %w", frontend.Name, err)
	}
	return routes, nil
}

func listFrontends(ctx context.Context, authenticated *clisession.ProjectSession) ([]apiclient.Frontend, error) {
	var frontends []apiclient.Frontend
	for page := api.DefaultPage; page < api.DefaultPage+maxResolvePages; page++ {
		result, err := authenticated.API.ListFrontends(ctx, authenticated.ProjectID, page, api.DefaultLimit)
		if err != nil {
			return nil, fmt.Errorf("failed to list frontends: %w", err)
		}
		if result == nil {
			break
		}
		frontends = append(frontends, result.Data...)
		if !result.HasMore || len(result.Data) == 0 {
			break
		}
	}
	return frontends, nil
}

// mountTarget finds the frontend a Frontend route mounts. Whether it may be
// mounted, such as not being the frontend itself, is the server's decision.
func mountTarget(frontends []apiclient.Frontend, identifier string) (*apiclient.Frontend, error) {
	target := strings.TrimSpace(identifier)
	if target == "" {
		return nil, errors.New("frontend name or ID cannot be empty")
	}
	for i := range frontends {
		if frontends[i].Name == target || frontends[i].Id.String() == target {
			return &frontends[i], nil
		}
	}
	return nil, fmt.Errorf("frontend %q %w", identifier, api.ErrNotFound)
}

func withTargets(routes []apiclient.FrontendRoute, frontends []apiclient.Frontend) []Mount {
	byID := make(map[uuid.UUID]*apiclient.Frontend, len(frontends))
	for i := range frontends {
		byID[frontends[i].Id] = &frontends[i]
	}
	result := make([]Mount, 0, len(routes))
	for _, route := range routes {
		result = append(result, Mount{FrontendRoute: route, Target: byID[route.TargetFrontendId]})
	}
	return result
}
