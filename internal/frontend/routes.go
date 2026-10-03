package frontend

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/apiclient"
	clisession "github.com/Kong/volcano-cli/internal/session"
)

// Route is a frontend function route and the function it forwards to.
type Route struct {
	apiclient.FrontendFunctionRoute
	// Function is nil when the target is missing from the function list, such
	// as one deleted after the route was read.
	Function *apiclient.Function
}

// RouteInput is the route a create writes. Function is a name or ID.
type RouteInput struct {
	PathPrefix  string
	Function    string
	StripPrefix bool
}

// RouteUpdate changes the fields that are set and keeps the others.
type RouteUpdate struct {
	PathPrefix  *string
	Function    *string
	StripPrefix *bool
}

// ListRoutes returns a frontend's function routes in the order they match.
func (s Service) ListRoutes(ctx context.Context, identifier string) (*apiclient.Frontend, []Route, error) {
	authenticated, frontend, err := s.currentFrontend(ctx, identifier)
	if err != nil {
		return nil, nil, err
	}
	routes, err := authenticated.API.ListFrontendFunctionRoutes(ctx, authenticated.ProjectID, frontend.Id)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to list routes for frontend %q: %w", frontend.Name, err)
	}
	functions, err := listFunctions(ctx, authenticated)
	if err != nil {
		return nil, nil, err
	}
	return frontend, withFunctions(routes, functions), nil
}

// RouteTargets pairs routes already read with the functions they forward to.
func (s Service) RouteTargets(ctx context.Context, routes []apiclient.FrontendFunctionRoute) ([]Route, error) {
	if len(routes) == 0 {
		return nil, nil
	}
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}
	functions, err := listFunctions(ctx, authenticated)
	if err != nil {
		return nil, err
	}
	return withFunctions(routes, functions), nil
}

// CreateRoute forwards a path prefix on a frontend to a function.
func (s Service) CreateRoute(ctx context.Context, identifier string, input RouteInput) (*apiclient.Frontend, Route, error) {
	authenticated, frontend, err := s.currentFrontend(ctx, identifier)
	if err != nil {
		return nil, Route{}, err
	}
	functions, err := listFunctions(ctx, authenticated)
	if err != nil {
		return nil, Route{}, err
	}
	target, err := findFunction(functions, input.Function)
	if err != nil {
		return nil, Route{}, err
	}

	stripPrefix := input.StripPrefix
	created, err := authenticated.API.CreateFrontendFunctionRoute(ctx, authenticated.ProjectID, frontend.Id,
		apiclient.CreateFrontendFunctionRouteRequest{
			PathPrefix:  input.PathPrefix,
			FunctionId:  target.Id,
			StripPrefix: &stripPrefix,
		})
	if err != nil {
		return nil, Route{}, fmt.Errorf("failed to create route: %w", err)
	}
	return frontend, Route{FrontendFunctionRoute: *created, Function: target}, nil
}

// UpdateRoute changes one route, found by its path prefix or ID.
func (s Service) UpdateRoute(ctx context.Context, identifier, route string, update RouteUpdate) (*apiclient.Frontend, Route, error) {
	authenticated, frontend, err := s.currentFrontend(ctx, identifier)
	if err != nil {
		return nil, Route{}, err
	}
	routes, err := authenticated.API.ListFrontendFunctionRoutes(ctx, authenticated.ProjectID, frontend.Id)
	if err != nil {
		return nil, Route{}, fmt.Errorf("failed to list routes for frontend %q: %w", frontend.Name, err)
	}
	current, err := findRoute(routes, frontend.Name, route)
	if err != nil {
		return nil, Route{}, err
	}
	functions, err := listFunctions(ctx, authenticated)
	if err != nil {
		return nil, Route{}, err
	}

	request := apiclient.CreateFrontendFunctionRouteRequest{
		PathPrefix:  current.PathPrefix,
		FunctionId:  current.FunctionId,
		StripPrefix: &current.StripPrefix,
	}
	if update.PathPrefix != nil {
		request.PathPrefix = *update.PathPrefix
	}
	if update.StripPrefix != nil {
		request.StripPrefix = update.StripPrefix
	}
	if update.Function != nil {
		target, err := findFunction(functions, *update.Function)
		if err != nil {
			return nil, Route{}, err
		}
		request.FunctionId = target.Id
	}

	updated, err := authenticated.API.UpdateFrontendFunctionRoute(ctx, authenticated.ProjectID, frontend.Id, current.Id, request)
	if err != nil {
		return nil, Route{}, fmt.Errorf("failed to update route %s: %w", current.PathPrefix, err)
	}
	return frontend, withFunctions([]apiclient.FrontendFunctionRoute{*updated}, functions)[0], nil
}

// ResolveRoute returns one route, found by its path prefix or ID.
func (s Service) ResolveRoute(ctx context.Context, identifier, route string) (*apiclient.Frontend, apiclient.FrontendFunctionRoute, error) {
	authenticated, frontend, err := s.currentFrontend(ctx, identifier)
	if err != nil {
		return nil, apiclient.FrontendFunctionRoute{}, err
	}
	routes, err := authenticated.API.ListFrontendFunctionRoutes(ctx, authenticated.ProjectID, frontend.Id)
	if err != nil {
		return nil, apiclient.FrontendFunctionRoute{}, fmt.Errorf("failed to list routes for frontend %q: %w", frontend.Name, err)
	}
	found, err := findRoute(routes, frontend.Name, route)
	if err != nil {
		return nil, apiclient.FrontendFunctionRoute{}, err
	}
	return frontend, found, nil
}

// DeleteRouteByID stops forwarding one route's path prefix.
func (s Service) DeleteRouteByID(ctx context.Context, frontendID, routeID uuid.UUID) error {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return err
	}
	if err := authenticated.API.DeleteFrontendFunctionRoute(ctx, authenticated.ProjectID, frontendID, routeID); err != nil {
		return fmt.Errorf("failed to delete route: %w", err)
	}
	return nil
}

func (s Service) currentFrontend(ctx context.Context, identifier string) (*clisession.ProjectSession, *apiclient.Frontend, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, nil, err
	}
	frontend, err := resolveFrontend(ctx, authenticated, identifier)
	if errors.Is(err, api.ErrNotFound) {
		return nil, nil, fmt.Errorf("frontend %q not found", identifier)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("failed to resolve frontend %q: %w", identifier, err)
	}
	return authenticated, frontend, nil
}

func listFunctions(ctx context.Context, authenticated *clisession.ProjectSession) ([]apiclient.Function, error) {
	var functions []apiclient.Function
	for page := api.DefaultPage; page < api.DefaultPage+maxResolvePages; page++ {
		result, err := authenticated.API.ListFunctions(ctx, authenticated.ProjectID, page, api.DefaultLimit)
		if err != nil {
			return nil, fmt.Errorf("failed to list functions: %w", err)
		}
		if result == nil {
			break
		}
		functions = append(functions, result.Data...)
		if !result.HasMore || len(result.Data) == 0 {
			break
		}
	}
	return functions, nil
}

func findFunction(functions []apiclient.Function, identifier string) (*apiclient.Function, error) {
	target := strings.TrimSpace(identifier)
	if target == "" {
		return nil, errors.New("function name or ID cannot be empty")
	}
	for i := range functions {
		if functions[i].Name == target || functions[i].Id.String() == target {
			return &functions[i], nil
		}
	}
	return nil, fmt.Errorf("function %q not found", identifier)
}

func findRoute(routes []apiclient.FrontendFunctionRoute, frontend, identifier string) (apiclient.FrontendFunctionRoute, error) {
	target := strings.TrimSpace(identifier)
	for _, route := range routes {
		if route.Id.String() == target || route.PathPrefix == target || route.PathPrefix == strings.TrimSuffix(target, "/") {
			return route, nil
		}
	}
	return apiclient.FrontendFunctionRoute{}, fmt.Errorf("frontend %q has no route %q", frontend, identifier)
}

func withFunctions(routes []apiclient.FrontendFunctionRoute, functions []apiclient.Function) []Route {
	byID := make(map[uuid.UUID]*apiclient.Function, len(functions))
	for i := range functions {
		byID[functions[i].Id] = &functions[i]
	}
	result := make([]Route, 0, len(routes))
	for _, route := range routes {
		result = append(result, Route{FrontendFunctionRoute: route, Function: byID[route.FunctionId]})
	}
	return result
}
