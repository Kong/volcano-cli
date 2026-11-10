package function

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

// CandidateRequest is a request to create a Candidate (preview) of a function.
type CandidateRequest struct {
	Function   string
	Name       string
	TTLSeconds int
	Source     []byte
}

// CandidateDeploy is a request to deploy a new source to a Candidate.
type CandidateDeploy struct {
	Function  string
	Candidate string
	// Package returns the source to upload. It runs once the function and the
	// Candidate are found, so a missing preview or a server without previews
	// is reported before any packaging.
	Package func(function *apiclient.Function) ([]byte, error)
}

// ErrPreviewsUnsupported reports a Volcano server older than function
// previews, which has none of their routes. Said in place of the bare 404 it
// answers, which otherwise reads as a missing function or preview.
var ErrPreviewsUnsupported = errors.New("this Volcano server doesn't support function previews yet; ask its operator to upgrade it, or use 'functions deploy'")

// ErrPreviewsUnavailable reports a route's feature_unavailable: function
// previews aren't open to this project yet.
var ErrPreviewsUnavailable = errors.New("function previews aren't available for this project yet")

const codeFeatureUnavailable = "feature_unavailable"

// previewRefusal replaces a refusal that says function previews are not on
// with the error that says so, keeping the API's error so the status still
// reports it.
func previewRefusal(err error) error {
	switch {
	case api.Code(err) == codeFeatureUnavailable:
		return api.WithCause(ErrPreviewsUnavailable, err)
	case api.RouteMissing(err):
		return api.WithCause(ErrPreviewsUnsupported, err)
	}
	return err
}

// CreateCandidate uploads a source archive and starts the Candidate's first
// deployment.
func (s Service) CreateCandidate(ctx context.Context, request CandidateRequest) (*apiclient.FunctionCandidate, error) {
	authenticated, function, err := s.currentFunction(ctx, request.Function)
	if err != nil {
		return nil, err
	}
	created, err := authenticated.API.CreateFunctionCandidate(ctx, authenticated.ProjectID, function.Id, api.FunctionCandidateInput{
		Name: strings.TrimSpace(request.Name), TTLSeconds: request.TTLSeconds, Source: request.Source,
	})
	if err != nil {
		return nil, previewError(fmt.Sprintf("failed to create a preview of function %q", function.Name), err)
	}
	return created, nil
}

// ListCandidates returns a function's Candidates.
func (s Service) ListCandidates(ctx context.Context, functionIdentifier string) (*apiclient.Function, []apiclient.FunctionCandidate, error) {
	authenticated, function, err := s.currentFunction(ctx, functionIdentifier)
	if err != nil {
		return nil, nil, err
	}
	candidates, err := authenticated.API.ListFunctionCandidates(ctx, authenticated.ProjectID, function.Id)
	if err != nil {
		return nil, nil, previewError(fmt.Sprintf("failed to list the previews of function %q", function.Name), err)
	}
	return function, candidates, nil
}

// GetCandidate returns one Candidate of a function, named by name or ID.
func (s Service) GetCandidate(ctx context.Context, functionIdentifier, candidateIdentifier string) (*apiclient.FunctionCandidate, error) {
	authenticated, function, err := s.currentFunction(ctx, functionIdentifier)
	if err != nil {
		return nil, err
	}
	return resolveCandidate(ctx, authenticated, function, candidateIdentifier)
}

// DeployCandidate uploads a new source to an existing Candidate.
func (s Service) DeployCandidate(ctx context.Context, request CandidateDeploy) (*apiclient.FunctionCandidate, *apiclient.FunctionCandidateDeployment, error) {
	authenticated, function, err := s.currentFunction(ctx, request.Function)
	if err != nil {
		return nil, nil, err
	}
	candidate, err := resolveCandidate(ctx, authenticated, function, request.Candidate)
	if err != nil {
		return nil, nil, err
	}
	source, err := request.Package(function)
	if err != nil {
		return nil, nil, err
	}
	deployment, err := authenticated.API.RedeployFunctionCandidate(ctx, authenticated.ProjectID, function.Id, candidate.Id,
		api.FunctionCandidateInput{Source: source})
	if err != nil {
		return nil, nil, previewError(fmt.Sprintf("failed to deploy preview %q", candidate.Name), err)
	}
	return candidate, deployment, nil
}

// DeleteCandidate starts deleting a Candidate once confirm, when set, agrees
// to delete the Candidate it found. It reports whether it deleted it.
func (s Service) DeleteCandidate(ctx context.Context, functionIdentifier, candidateIdentifier string, confirm func(*apiclient.FunctionCandidate) (bool, error)) (*apiclient.FunctionCandidate, bool, error) {
	authenticated, function, err := s.currentFunction(ctx, functionIdentifier)
	if err != nil {
		return nil, false, err
	}
	candidate, err := resolveCandidate(ctx, authenticated, function, candidateIdentifier)
	if err != nil {
		return nil, false, err
	}
	if confirm != nil {
		confirmed, err := confirm(candidate)
		if err != nil || !confirmed {
			return candidate, false, err
		}
	}
	if err := authenticated.API.DeleteFunctionCandidate(ctx, authenticated.ProjectID, function.Id, candidate.Id); err != nil {
		return nil, false, previewError(fmt.Sprintf("failed to delete preview %q", candidate.Name), err)
	}
	return candidate, true, nil
}

// PromoteCandidate points a function's traffic at a Candidate's Lambda without
// deploying anything. It names the serving revision it read, so a competing
// promotion or rollback refuses it.
func (s Service) PromoteCandidate(ctx context.Context, functionIdentifier, candidateIdentifier string) (*apiclient.FunctionCandidate, *apiclient.FunctionServing, error) {
	authenticated, function, err := s.currentFunction(ctx, functionIdentifier)
	if err != nil {
		return nil, nil, err
	}
	candidate, err := resolveCandidate(ctx, authenticated, function, candidateIdentifier)
	if err != nil {
		return nil, nil, err
	}
	current, err := authenticated.API.GetFunctionServing(ctx, authenticated.ProjectID, function.Id)
	if err != nil {
		return nil, nil, previewError(fmt.Sprintf("failed to read what function %q runs", function.Name), err)
	}
	serving, err := authenticated.API.PromoteFunctionCandidate(ctx, authenticated.ProjectID, function.Id, candidate.Id, current.Revision)
	if err != nil {
		return nil, nil, previewError(fmt.Sprintf("failed to promote preview %q", candidate.Name), err)
	}
	return candidate, serving, nil
}

// Serving returns what a function runs.
func (s Service) Serving(ctx context.Context, functionIdentifier string) (*apiclient.Function, *apiclient.FunctionServing, error) {
	authenticated, function, err := s.currentFunction(ctx, functionIdentifier)
	if err != nil {
		return nil, nil, err
	}
	serving, err := authenticated.API.GetFunctionServing(ctx, authenticated.ProjectID, function.Id)
	if err != nil {
		return nil, nil, previewError(fmt.Sprintf("failed to read what function %q runs", function.Name), err)
	}
	return function, serving, nil
}

// RollbackServing returns a function to the build it ran before the last
// promotion or rollback, or to its own build with toProduction.
func (s Service) RollbackServing(ctx context.Context, functionIdentifier string, toProduction bool) (*apiclient.Function, *apiclient.FunctionServing, error) {
	authenticated, function, err := s.currentFunction(ctx, functionIdentifier)
	if err != nil {
		return nil, nil, err
	}
	current, err := authenticated.API.GetFunctionServing(ctx, authenticated.ProjectID, function.Id)
	if err != nil {
		return nil, nil, previewError(fmt.Sprintf("failed to read what function %q runs", function.Name), err)
	}
	serving, err := authenticated.API.RollbackFunctionServing(ctx, authenticated.ProjectID, function.Id, current.Revision, toProduction)
	if err != nil {
		return nil, nil, previewError(fmt.Sprintf("failed to roll back function %q", function.Name), err)
	}
	return function, serving, nil
}

// CandidateName names a Candidate by ID, or returns "" when it cannot be read.
func (s Service) CandidateName(ctx context.Context, function *apiclient.Function, id *uuid.UUID) (string, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil || id == nil {
		return "", err
	}
	candidate, err := authenticated.API.GetFunctionCandidate(ctx, authenticated.ProjectID, function.Id, *id)
	if err != nil {
		return "", previewError(fmt.Sprintf("failed to read preview %s", id), err)
	}
	return candidate.Name, nil
}

func (s Service) currentFunction(ctx context.Context, identifier string) (*clisession.ProjectSession, *apiclient.Function, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, nil, err
	}
	function, err := resolveFunction(ctx, authenticated, identifier)
	if errors.Is(err, api.ErrNotFound) {
		return nil, nil, fmt.Errorf("function %q %w", identifier, api.ErrNotFound)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("failed to resolve function %q: %w", identifier, err)
	}
	return authenticated, function, nil
}

func resolveCandidate(ctx context.Context, authenticated *clisession.ProjectSession, function *apiclient.Function, identifier string) (*apiclient.FunctionCandidate, error) {
	target := strings.TrimSpace(identifier)
	if target == "" {
		return nil, errors.New("preview identifier cannot be empty")
	}
	candidates, err := authenticated.API.ListFunctionCandidates(ctx, authenticated.ProjectID, function.Id)
	if err != nil {
		return nil, previewError(fmt.Sprintf("failed to list the previews of function %q", function.Name), err)
	}
	// An exact ID wins over a name, and a name over a slug, so one preview's
	// slug never shadows another preview named exactly as asked.
	if id, parseErr := uuid.Parse(target); parseErr == nil {
		for i := range candidates {
			if candidates[i].Id == id {
				return &candidates[i], nil
			}
		}
	}
	for i := range candidates {
		if candidates[i].Name == target {
			return &candidates[i], nil
		}
	}
	for i := range candidates {
		if candidates[i].Slug == target {
			return &candidates[i], nil
		}
	}
	return nil, fmt.Errorf("preview %q of function %q %w", target, function.Name, api.ErrNotFound)
}

// previewError says what failed, in the words of previewRefusal when the
// server has no previews for this project.
func previewError(what string, err error) error {
	return fmt.Errorf("%s: %w", what, previewRefusal(err))
}
