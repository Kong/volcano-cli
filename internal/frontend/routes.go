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

// Route is a frontend function route and the function it forwards to.
type Route struct {
	apiclient.FrontendFunctionRoute
	// Function is nil when the target is missing from the function list, such
	// as one deleted after the route was read.
	Function *apiclient.Function
}

// RouteInput is the route a create writes. Exactly one of Function and
// Frontend, each a name or ID, names the target: a function route forwards to
// a function, and a Frontend route mounts another frontend.
type RouteInput struct {
	PathPrefix  string
	Function    string
	Frontend    string
	StripPrefix bool
}

// RouteUpdate changes the fields that are set and keeps the others. Function
// and StripPrefix apply to function routes, Frontend to Frontend routes.
type RouteUpdate struct {
	PathPrefix  *string
	Function    *string
	Frontend    *string
	StripPrefix *bool
}

// RouteResult is one route of either kind; exactly one field is set.
type RouteResult struct {
	Function *Route
	Frontend *Mount
}

// PathPrefix returns the route's path prefix.
func (r RouteResult) PathPrefix() string {
	if r.Frontend != nil {
		return r.Frontend.PathPrefix
	}
	if r.Function != nil {
		return r.Function.PathPrefix
	}
	return ""
}

// Routes are a frontend's function and Frontend routes, each in the order the
// server returns them, most specific first.
type Routes struct {
	Function []Route
	Frontend []Mount
}

// ListRoutes returns a frontend's function and Frontend routes.
func (s Service) ListRoutes(ctx context.Context, identifier string) (*apiclient.Frontend, Routes, error) {
	authenticated, frontend, err := s.currentFrontend(ctx, identifier)
	if err != nil {
		return nil, Routes{}, err
	}
	routes, err := authenticated.API.ListFrontendFunctionRoutes(ctx, authenticated.ProjectID, frontend.Id)
	if err != nil {
		return nil, Routes{}, fmt.Errorf("failed to list routes for frontend %q: %w", frontend.Name, err)
	}
	mounts, err := listMounts(ctx, authenticated, frontend)
	if err != nil {
		return nil, Routes{}, err
	}
	var listed Routes
	if len(routes) > 0 {
		functions, err := listFunctions(ctx, authenticated)
		if err != nil {
			return nil, Routes{}, err
		}
		listed.Function = withFunctions(routes, functions)
	}
	if len(mounts) > 0 {
		frontends, err := listFrontends(ctx, authenticated)
		if err != nil {
			return nil, Routes{}, err
		}
		listed.Frontend = withTargets(mounts, frontends)
	}
	return frontend, listed, nil
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

// CreateRoute forwards a path prefix on a frontend to a function, or mounts
// another frontend there when input names a Frontend.
func (s Service) CreateRoute(ctx context.Context, identifier string, input RouteInput) (*apiclient.Frontend, RouteResult, error) {
	if input.Frontend != "" {
		return s.createMount(ctx, identifier, input)
	}
	authenticated, frontend, err := s.currentFrontend(ctx, identifier)
	if err != nil {
		return nil, RouteResult{}, err
	}
	functions, err := listFunctions(ctx, authenticated)
	if err != nil {
		return nil, RouteResult{}, err
	}
	target, err := routeTarget(ctx, authenticated, functions, input.Function)
	if err != nil {
		return nil, RouteResult{}, err
	}

	stripPrefix := input.StripPrefix
	created, err := authenticated.API.CreateFrontendFunctionRoute(ctx, authenticated.ProjectID, frontend.Id,
		apiclient.CreateFrontendFunctionRouteRequest{
			PathPrefix:  input.PathPrefix,
			FunctionId:  target.Id,
			StripPrefix: &stripPrefix,
		})
	if err != nil {
		return nil, RouteResult{}, fmt.Errorf("failed to create route: %w", refusedTarget(err, target))
	}
	return frontend, RouteResult{Function: &Route{FrontendFunctionRoute: *created, Function: target}}, nil
}

// TargetNotPublicError reports a route refused because the function it
// forwards to is not public, which every route target must be.
type TargetNotPublicError struct {
	Function string
	Err      error
}

func (e *TargetNotPublicError) Error() string { return e.Err.Error() }

func (e *TargetNotPublicError) Unwrap() error { return e.Err }

func refusedTarget(err error, target *apiclient.Function) error {
	if api.Status(err) == http.StatusConflict && target != nil && target.Visibility != apiclient.FunctionVisibilityPublic {
		return &TargetNotPublicError{Function: target.Name, Err: err}
	}
	return err
}

// UpdateRoute changes one route of either kind, found by its path prefix or
// ID. A route keeps its kind.
func (s Service) UpdateRoute(ctx context.Context, identifier, route string, update RouteUpdate) (*apiclient.Frontend, RouteResult, error) {
	authenticated, frontend, found, err := s.findRoute(ctx, identifier, route)
	if err != nil {
		return nil, RouteResult{}, err
	}
	if found.Frontend != nil {
		updated, err := s.updateMount(ctx, authenticated, frontend, *found.Frontend, update)
		return frontend, updated, err
	}
	current := *found.Function
	if update.Frontend != nil {
		return nil, RouteResult{}, fmt.Errorf("route %s on frontend %q forwards to a function, so --frontend does not apply; "+
			"a route cannot change kind: delete it and create a Frontend route instead", current.PathPrefix, frontend.Name)
	}
	functions, err := listFunctions(ctx, authenticated)
	if err != nil {
		return nil, RouteResult{}, err
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
		target, err := routeTarget(ctx, authenticated, functions, *update.Function)
		if err != nil {
			return nil, RouteResult{}, err
		}
		request.FunctionId = target.Id
	}

	updated, err := authenticated.API.UpdateFrontendFunctionRoute(ctx, authenticated.ProjectID, frontend.Id, current.Id, request)
	if err != nil {
		return nil, RouteResult{}, fmt.Errorf("failed to update route %s: %w",
			current.PathPrefix, refusedTarget(err, functionByID(functions, request.FunctionId)))
	}
	return frontend, RouteResult{Function: &withFunctions([]apiclient.FrontendFunctionRoute{*updated}, functions)[0]}, nil
}

// FoundRoute is one route of either kind as the server lists it; exactly one
// field is set.
type FoundRoute struct {
	Function *apiclient.FrontendFunctionRoute
	Frontend *apiclient.FrontendRoute
}

// PathPrefix returns the route's path prefix.
func (r FoundRoute) PathPrefix() string {
	if r.Frontend != nil {
		return r.Frontend.PathPrefix
	}
	return r.Function.PathPrefix
}

// ResolveRoute returns one route of either kind, found by its path prefix or
// ID.
func (s Service) ResolveRoute(ctx context.Context, identifier, route string) (*apiclient.Frontend, FoundRoute, error) {
	_, frontend, found, err := s.findRoute(ctx, identifier, route)
	return frontend, found, err
}

// DeleteRoute stops serving one route's path prefix, whichever kind it is.
func (s Service) DeleteRoute(ctx context.Context, frontendID uuid.UUID, route FoundRoute) error {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return err
	}
	if route.Frontend != nil {
		err = authenticated.API.DeleteFrontendRoute(ctx, authenticated.ProjectID, frontendID, route.Frontend.Id)
	} else {
		err = authenticated.API.DeleteFrontendFunctionRoute(ctx, authenticated.ProjectID, frontendID, route.Function.Id)
	}
	if err != nil {
		return fmt.Errorf("failed to delete route: %w", err)
	}
	return nil
}

// findRoute finds a route by path prefix or ID among a frontend's function and
// Frontend routes, which share one set of paths.
func (s Service) findRoute(
	ctx context.Context, identifier, route string,
) (*clisession.ProjectSession, *apiclient.Frontend, FoundRoute, error) {
	authenticated, frontend, err := s.currentFrontend(ctx, identifier)
	if err != nil {
		return nil, nil, FoundRoute{}, err
	}
	routes, err := authenticated.API.ListFrontendFunctionRoutes(ctx, authenticated.ProjectID, frontend.Id)
	if err != nil {
		return nil, nil, FoundRoute{}, fmt.Errorf("failed to list routes for frontend %q: %w", frontend.Name, err)
	}
	for i := range routes {
		if routeMatches(routes[i].Id, routes[i].PathPrefix, route) {
			return authenticated, frontend, FoundRoute{Function: &routes[i]}, nil
		}
	}
	mounts, err := listMounts(ctx, authenticated, frontend)
	if err != nil {
		return nil, nil, FoundRoute{}, err
	}
	for i := range mounts {
		if routeMatches(mounts[i].Id, mounts[i].PathPrefix, route) {
			return authenticated, frontend, FoundRoute{Frontend: &mounts[i]}, nil
		}
	}
	return nil, nil, FoundRoute{}, fmt.Errorf("route %q %w for frontend %q", route, api.ErrNotFound, frontend.Name)
}

func (s Service) currentFrontend(ctx context.Context, identifier string) (*clisession.ProjectSession, *apiclient.Frontend, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, nil, err
	}
	frontend, err := resolveFrontend(ctx, authenticated, identifier)
	if errors.Is(err, api.ErrNotFound) {
		return nil, nil, fmt.Errorf("frontend %q %w", identifier, api.ErrNotFound)
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

// routeTarget finds the standard function a route forwards to. The function
// list holds standard functions only, so a miss is checked against the durable
// collection to say why a durable name cannot be a target. That lookup only
// refines the message: if it fails, the name is reported not found.
func routeTarget(
	ctx context.Context, authenticated *clisession.ProjectSession, functions []apiclient.Function, identifier string,
) (*apiclient.Function, error) {
	target := strings.TrimSpace(identifier)
	if target == "" {
		return nil, errors.New("function name or ID cannot be empty")
	}
	for i := range functions {
		if functions[i].Name == target || functions[i].Id.String() == target {
			return &functions[i], nil
		}
	}
	if durable, err := authenticated.API.GetDurableFunction(ctx, authenticated.ProjectID, target); err == nil {
		return nil, fmt.Errorf("%q is a durable function: frontend routes forward only to standard functions", durable.Name)
	}
	return nil, fmt.Errorf("function %q %w", identifier, api.ErrNotFound)
}

func routeMatches(id uuid.UUID, pathPrefix, identifier string) bool {
	target := strings.TrimSpace(identifier)
	return id.String() == target || pathPrefix == target || pathPrefix == strings.TrimSuffix(target, "/")
}

func functionByID(functions []apiclient.Function, id uuid.UUID) *apiclient.Function {
	for i := range functions {
		if functions[i].Id == id {
			return &functions[i]
		}
	}
	return nil
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
