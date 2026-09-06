// Package function builds and packages function archives for upload.
package function

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/apiclient"
	cliconfig "github.com/Kong/volcano-cli/internal/config"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	clisession "github.com/Kong/volcano-cli/internal/session"
)

// InvokeTokenProvider returns the bearer token to use for function invoke routes.
type InvokeTokenProvider func(context.Context, *clisession.ProjectSession) (string, error)

// Service performs authenticated Volcano function workflows.
type Service struct {
	sessions            clisession.Factory
	invokeTokenProvider InvokeTokenProvider
}

// Option configures function workflows.
type Option func(*Service)

// WithInvokeTokenProvider configures the bearer token source for function invoke routes.
func WithInvokeTokenProvider(provider InvokeTokenProvider) Option {
	return func(s *Service) {
		s.invokeTokenProvider = provider
	}
}

// Alias describes one configured function invoke alias.
type Alias struct {
	Name       string
	FunctionID string
}

// NewService returns a function service.
func NewService(deps cliruntime.Deps, opts ...Option) Service {
	service := Service{sessions: clisession.NewFactory(deps)}
	for _, opt := range opts {
		opt(&service)
	}
	return service
}

// ListPage returns one function page in the current project.
func (s Service) ListPage(ctx context.Context, page, limit int) (*apiclient.PaginatedFunctions, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	functions, err := authenticated.API.ListFunctions(ctx, authenticated.ProjectID, page, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to list functions: %w", err)
	}
	return functions, nil
}

// ListRuntimes returns the function runtime catalog.
func (s Service) ListRuntimes(ctx context.Context) ([]apiclient.FunctionRuntimeOption, error) {
	cfg, err := s.sessions.Config()
	if err != nil {
		return nil, err
	}

	client, err := s.sessions.APIClient(s.sessions.APIURL(cfg), cfg.Token())
	if err != nil {
		return nil, fmt.Errorf("failed to create api client: %w", err)
	}

	runtimes, err := client.ListFunctionRuntimes(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list function runtimes: %w", err)
	}
	return runtimes, nil
}

// RuntimeCatalog returns deploy runtime metadata from the function runtime catalog.
func (s Service) RuntimeCatalog(ctx context.Context) (RuntimeCatalog, error) {
	runtimes, err := s.ListRuntimes(ctx)
	if err != nil {
		return RuntimeCatalog{}, err
	}
	return RuntimeCatalogFromOptions(runtimes), nil
}

// DeployPackage deploys one packaged function source archive.
func (s Service) DeployPackage(ctx context.Context, pkg Package) (*apiclient.Function, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	fn, err := authenticated.API.DeployFunction(ctx, authenticated.ProjectID, api.FunctionDeployInput{
		Name:          pkg.Name,
		Runtime:       pkg.Runtime,
		Handler:       pkg.Handler,
		SourceArchive: pkg.ArchiveData,
		VariableScope: pkg.VariableScope,
		Variables:     pkg.Variables,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to deploy function %q: %w", pkg.Name, err)
	}
	return fn, nil
}

// DeployPackageBatch deploys one batch of packaged function source archives.
func (s Service) DeployPackageBatch(ctx context.Context, packages []Package) (*apiclient.BatchFunctionDeployResponse, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	inputs := make([]api.FunctionDeployInput, 0, len(packages))
	for _, pkg := range packages {
		inputs = append(inputs, api.FunctionDeployInput{
			Name:          pkg.Name,
			Runtime:       pkg.Runtime,
			Handler:       pkg.Handler,
			SourceArchive: pkg.ArchiveData,
			VariableScope: pkg.VariableScope,
			Variables:     pkg.Variables,
		})
	}
	resp, err := authenticated.API.DeployFunctionsBatch(ctx, authenticated.ProjectID, inputs)
	if err != nil {
		return nil, fmt.Errorf("failed to deploy functions batch: %w", err)
	}
	return resp, nil
}

// Resolve returns a function by normalized name/path or UUID in the current project.
func (s Service) Resolve(ctx context.Context, identifier string) (*apiclient.Function, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	function, err := resolveFunction(ctx, authenticated, identifier)
	if errors.Is(err, api.ErrNotFound) {
		return nil, fmt.Errorf("function %q not found", identifier)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to resolve function %q: %w", identifier, err)
	}
	return function, nil
}

// Get returns one function by normalized name/path or UUID.
func (s Service) Get(ctx context.Context, identifier string) (*apiclient.Function, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	function, err := resolveFunction(ctx, authenticated, identifier)
	if errors.Is(err, api.ErrNotFound) {
		return nil, fmt.Errorf("function %q not found", identifier)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to resolve function %q: %w", identifier, err)
	}

	got, err := authenticated.API.GetFunction(ctx, authenticated.ProjectID, function.Id)
	if err != nil {
		return nil, fmt.Errorf("failed to get function %q: %w", identifier, err)
	}
	return got, nil
}

// DeleteByID starts deleting one function by ID.
func (s Service) DeleteByID(ctx context.Context, functionID uuid.UUID) error {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return err
	}

	if err := authenticated.API.DeleteFunction(ctx, authenticated.ProjectID, functionID); err != nil {
		return fmt.Errorf("failed to delete function %q: %w", functionID.String(), err)
	}
	return nil
}

// UpdateVisibility updates one function's public/private visibility.
func (s Service) UpdateVisibility(ctx context.Context, identifier string, isPublic bool) (*apiclient.Function, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	function, err := resolveFunction(ctx, authenticated, identifier)
	if errors.Is(err, api.ErrNotFound) {
		return nil, fmt.Errorf("function %q not found", identifier)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to resolve function %q: %w", identifier, err)
	}

	updated, err := authenticated.API.UpdateFunctionVisibility(ctx, authenticated.ProjectID, function.Id, isPublic)
	if err != nil {
		return nil, fmt.Errorf("failed to update function visibility: %w", err)
	}
	return updated, nil
}

// Invoke invokes one function by alias, normalized name/path, or UUID.
func (s Service) Invoke(ctx context.Context, identifier string, payload map[string]any) (*apiclient.FunctionInvocationResponse, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	functionID, err := resolveInvokeFunctionID(ctx, authenticated, identifier)
	if errors.Is(err, api.ErrNotFound) {
		return nil, fmt.Errorf("function %q not found", identifier)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to resolve function %q: %w", identifier, err)
	}

	invokeAPI, err := s.invokeAPI(ctx, authenticated)
	if err != nil {
		return nil, err
	}
	resp, err := invokeAPI.InvokeFunction(ctx, functionID, api.FunctionInvokeInput{Payload: payload})
	if err != nil {
		return nil, fmt.Errorf("failed to invoke function %q: %w", identifier, err)
	}
	return resp, nil
}

// InvokeByID invokes one function by ID without list-based name resolution.
func (s Service) InvokeByID(ctx context.Context, functionID uuid.UUID, payload map[string]any) (*apiclient.FunctionInvocationResponse, error) {
	invokeAPI, err := s.invokeAPIForID(ctx)
	if err != nil {
		return nil, err
	}
	resp, err := invokeAPI.InvokeFunction(ctx, functionID, api.FunctionInvokeInput{Payload: payload})
	if err != nil {
		return nil, fmt.Errorf("failed to invoke function %q: %w", functionID.String(), err)
	}
	return resp, nil
}

func (s Service) invokeAPIForID(ctx context.Context) (*api.Client, error) {
	if s.invokeTokenProvider == nil {
		authenticated, err := s.sessions.Authenticated()
		if err != nil {
			return nil, err
		}
		return authenticated.APIWithToken(authenticated.Config.FunctionInvokeToken())
	}

	project, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}
	return s.invokeAPI(ctx, project)
}

func (s Service) invokeAPI(ctx context.Context, project *clisession.ProjectSession) (*api.Client, error) {
	token, err := s.invokeToken(ctx, project)
	if err != nil {
		return nil, err
	}
	return project.APIWithToken(token)
}

func (s Service) invokeToken(ctx context.Context, project *clisession.ProjectSession) (string, error) {
	if project == nil || project.Config == nil {
		return "", errors.New("project session is required")
	}
	// Cloud supplies the reserved data-plane service key via the provider; local
	// mode has no provider and the session layer drops the credential entirely
	// (see session.Factory.APIClient), so the returned token is unused there.
	if s.invokeTokenProvider != nil {
		return s.invokeTokenProvider(ctx, project)
	}
	return project.Config.FunctionInvokeToken(), nil
}

// ListAliases returns configured function invoke aliases for the current target.
func (s Service) ListAliases(_ context.Context) ([]Alias, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	scope := functionAliasScope(authenticated)
	configured := authenticated.Config.FunctionAliases[scope]
	aliases := make([]Alias, 0, len(configured))
	for name, functionID := range configured {
		aliases = append(aliases, Alias{Name: name, FunctionID: functionID})
	}
	sort.Slice(aliases, func(i, j int) bool {
		return aliases[i].Name < aliases[j].Name
	})
	return aliases, nil
}

// SetAlias configures one function invoke alias for the current target.
func (s Service) SetAlias(_ context.Context, alias, functionIDText string) (Alias, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return Alias{}, err
	}

	alias = normalizeTargetFunction(alias)
	if alias == "" {
		return Alias{}, errors.New("function alias cannot be empty")
	}
	functionID, err := uuid.Parse(strings.TrimSpace(functionIDText))
	if err != nil {
		return Alias{}, fmt.Errorf("invalid function ID %q: %w", functionIDText, err)
	}

	cfg, err := cliconfig.Load()
	if err != nil {
		return Alias{}, err
	}
	cfg.SetFunctionAlias(functionAliasScope(authenticated), alias, functionID.String())
	if err := cfg.Save(); err != nil {
		return Alias{}, err
	}
	return Alias{Name: alias, FunctionID: functionID.String()}, nil
}

// DeleteAlias removes one function invoke alias for the current target.
func (s Service) DeleteAlias(_ context.Context, alias string) error {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return err
	}

	alias = normalizeTargetFunction(alias)
	if alias == "" {
		return errors.New("function alias cannot be empty")
	}

	cfg, err := cliconfig.Load()
	if err != nil {
		return err
	}
	if !cfg.DeleteFunctionAlias(functionAliasScope(authenticated), alias) {
		return fmt.Errorf("function alias %q not found", alias)
	}
	return cfg.Save()
}

// ListSchedulers returns schedulers configured for a function.
func (s Service) ListSchedulers(ctx context.Context, identifier string) (*apiclient.Function, *apiclient.FunctionSchedulerListResponse, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, nil, err
	}

	function, err := resolveFunction(ctx, authenticated, identifier)
	if errors.Is(err, api.ErrNotFound) {
		return nil, nil, fmt.Errorf("function %q not found", identifier)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("failed to resolve function %q: %w", identifier, err)
	}

	schedulers, err := authenticated.API.ListFunctionSchedulers(ctx, authenticated.ProjectID, function.Id)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to list schedulers for function %q: %w", identifier, err)
	}
	return function, schedulers, nil
}

// CreateSchedulerByID creates one scheduler for the given function ID.
func (s Service) CreateSchedulerByID(ctx context.Context, functionID uuid.UUID, input api.FunctionSchedulerInput) (*apiclient.FunctionScheduler, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	scheduler, err := authenticated.API.CreateFunctionScheduler(ctx, authenticated.ProjectID, functionID, input)
	if err != nil {
		return nil, fmt.Errorf("failed to create scheduler: %w", err)
	}
	return scheduler, nil
}

// UpdateSchedulerByID updates one scheduler by ID, preserving the scheduler UUID.
func (s Service) UpdateSchedulerByID(ctx context.Context, functionID, schedulerID uuid.UUID, input api.FunctionSchedulerInput) (*apiclient.FunctionScheduler, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	scheduler, err := authenticated.API.UpdateFunctionScheduler(ctx, authenticated.ProjectID, functionID, schedulerID, input)
	if err != nil {
		return nil, fmt.Errorf("failed to update scheduler: %w", err)
	}
	return scheduler, nil
}

// EnableScheduler enables one scheduler for a function.
func (s Service) EnableScheduler(ctx context.Context, identifier string, schedulerID uuid.UUID) (*apiclient.FunctionScheduler, error) {
	return s.setSchedulerEnabled(ctx, identifier, schedulerID, true)
}

// DisableScheduler disables one scheduler for a function.
func (s Service) DisableScheduler(ctx context.Context, identifier string, schedulerID uuid.UUID) (*apiclient.FunctionScheduler, error) {
	return s.setSchedulerEnabled(ctx, identifier, schedulerID, false)
}

func (s Service) setSchedulerEnabled(ctx context.Context, identifier string, schedulerID uuid.UUID, enabled bool) (*apiclient.FunctionScheduler, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	function, err := resolveFunction(ctx, authenticated, identifier)
	if errors.Is(err, api.ErrNotFound) {
		return nil, fmt.Errorf("function %q not found", identifier)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to resolve function %q: %w", identifier, err)
	}

	flag := enabled
	scheduler, err := authenticated.API.UpdateFunctionScheduler(ctx, authenticated.ProjectID, function.Id, schedulerID, api.FunctionSchedulerInput{
		Enabled: &flag,
	})
	if err != nil {
		verb := "disable"
		if enabled {
			verb = "enable"
		}
		return nil, fmt.Errorf("failed to %s scheduler %q: %w", verb, schedulerID.String(), err)
	}
	return scheduler, nil
}

// DeleteScheduler deletes one scheduler for a function.
func (s Service) DeleteScheduler(ctx context.Context, identifier string, schedulerID uuid.UUID) error {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return err
	}

	function, err := resolveFunction(ctx, authenticated, identifier)
	if errors.Is(err, api.ErrNotFound) {
		return fmt.Errorf("function %q not found", identifier)
	}
	if err != nil {
		return fmt.Errorf("failed to resolve function %q: %w", identifier, err)
	}

	if err := authenticated.API.DeleteFunctionScheduler(ctx, authenticated.ProjectID, function.Id, schedulerID); err != nil {
		return fmt.Errorf("failed to delete scheduler %q: %w", schedulerID.String(), err)
	}
	return nil
}

// ResolveDeployment returns a deployment by ID, paging internally.
func (s Service) ResolveDeployment(ctx context.Context, functionID uuid.UUID, deploymentID string) (*apiclient.FunctionDeployment, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	deploymentID = strings.TrimSpace(deploymentID)
	if deploymentID == "" {
		return nil, api.ErrNotFound
	}

	for page := api.DefaultPage; ; page++ {
		deployments, err := authenticated.API.ListFunctionDeployments(ctx, authenticated.ProjectID, functionID, page, api.DefaultLimit)
		if err != nil {
			return nil, fmt.Errorf("failed to list deployments: %w", err)
		}
		if deployments == nil {
			return nil, api.ErrNotFound
		}
		for i := range deployments.Data {
			if deployments.Data[i].Id.String() == deploymentID {
				return &deployments.Data[i], nil
			}
		}
		if !deployments.HasMore || len(deployments.Data) == 0 {
			return nil, api.ErrNotFound
		}
	}
}

// RuntimeLogs returns one runtime log search page for a function.
func (s Service) RuntimeLogs(ctx context.Context, functionID uuid.UUID, limit int, cursor string) (*apiclient.LogSearchResponse, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	logs, err := authenticated.API.GetFunctionLogs(ctx, authenticated.ProjectID, functionID, limit, cursor)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch runtime logs: %w", err)
	}
	return logs, nil
}

// StreamRuntimeLogs opens a runtime log stream for a function, resuming after
// lastEventID when it is set.
func (s Service) StreamRuntimeLogs(ctx context.Context, functionID uuid.UUID, limit int, lastEventID string) (*api.ProjectLogStream, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	stream, err := authenticated.API.StreamFunctionLogs(ctx, authenticated.ProjectID, functionID, limit, lastEventID)
	if err != nil {
		return nil, fmt.Errorf("failed to stream runtime logs: %w", err)
	}
	return stream, nil
}

// DeploymentLogs returns one build log search page for a function deployment.
func (s Service) DeploymentLogs(ctx context.Context, functionID, deploymentID uuid.UUID, limit int, cursor string) (*apiclient.LogSearchResponse, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	logs, err := authenticated.API.GetFunctionDeploymentLogs(ctx, authenticated.ProjectID, functionID, deploymentID, limit, cursor)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch deployment logs: %w", err)
	}
	return logs, nil
}

// StreamDeploymentLogs opens a build log stream for a function deployment.
func (s Service) StreamDeploymentLogs(ctx context.Context, functionID, deploymentID uuid.UUID, limit int) (*api.ProjectLogStream, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	stream, err := authenticated.API.StreamFunctionDeploymentLogs(ctx, authenticated.ProjectID, functionID, deploymentID, limit, "")
	if err != nil {
		return nil, fmt.Errorf("failed to stream deployment logs: %w", err)
	}
	return stream, nil
}

func resolveInvokeFunctionID(ctx context.Context, authenticated *clisession.ProjectSession, identifier string) (uuid.UUID, error) {
	target := normalizeTargetFunction(identifier)
	if target == "" {
		return uuid.Nil, errors.New("function identifier cannot be empty")
	}

	if functionIDText, ok := authenticated.Config.FunctionAlias(functionAliasScope(authenticated), target); ok {
		functionID, err := uuid.Parse(functionIDText)
		if err != nil {
			return uuid.Nil, fmt.Errorf("function alias %q has invalid function ID %q: %w", target, functionIDText, err)
		}
		return functionID, nil
	}

	function, err := resolveFunction(ctx, authenticated, identifier)
	if err != nil {
		return uuid.Nil, err
	}
	return function.Id, nil
}

func functionAliasScope(authenticated *clisession.ProjectSession) string {
	return cliconfig.FunctionAliasScope(authenticated.APIURL, authenticated.ProjectID.String())
}

func resolveFunction(ctx context.Context, authenticated *clisession.ProjectSession, identifier string) (*apiclient.Function, error) {
	target := normalizeTargetFunction(identifier)
	if target == "" {
		return nil, errors.New("function identifier cannot be empty")
	}

	for page := api.DefaultPage; ; page++ {
		functions, err := authenticated.API.ListFunctions(ctx, authenticated.ProjectID, page, api.DefaultLimit)
		if err != nil {
			return nil, err
		}
		if functions == nil {
			return nil, api.ErrNotFound
		}
		for i := range functions.Data {
			if functions.Data[i].Name == target || functions.Data[i].Id.String() == target {
				return &functions.Data[i], nil
			}
		}
		if !functions.HasMore || len(functions.Data) == 0 {
			return nil, api.ErrNotFound
		}
	}
}

func normalizeTargetFunction(target string) string {
	name := filepath.Base(strings.TrimSpace(target))
	if ext := filepath.Ext(name); ext != "" {
		name = strings.TrimSuffix(name, ext)
	}
	return name
}
