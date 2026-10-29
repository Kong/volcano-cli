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

// CandidateRequest is a request to create a Candidate of a frontend.
type CandidateRequest struct {
	Frontend   string
	Name       string
	TTLSeconds int
	Archive    []byte
}

// CandidateDeploy is a request to deploy a new archive to a Candidate.
type CandidateDeploy struct {
	Frontend  string
	Candidate string
	Archive   []byte
}

// CreateCandidate uploads a source archive and starts the Candidate's first
// deployment.
func (s Service) CreateCandidate(ctx context.Context, request CandidateRequest) (*apiclient.FrontendCandidate, error) {
	authenticated, frontend, err := s.currentFrontend(ctx, request.Frontend)
	if err != nil {
		return nil, err
	}
	created, err := authenticated.API.CreateFrontendCandidate(ctx, authenticated.ProjectID, frontend.Id, api.FrontendCandidateInput{
		Name: strings.TrimSpace(request.Name), TTLSeconds: request.TTLSeconds, Archive: request.Archive,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create a preview of frontend %q: %w", frontend.Name, err)
	}
	return created, nil
}

// ListCandidates returns a frontend's Candidates.
func (s Service) ListCandidates(ctx context.Context, frontendIdentifier string) (*apiclient.Frontend, []apiclient.FrontendCandidate, error) {
	authenticated, frontend, err := s.currentFrontend(ctx, frontendIdentifier)
	if err != nil {
		return nil, nil, err
	}
	candidates, err := authenticated.API.ListFrontendCandidates(ctx, authenticated.ProjectID, frontend.Id)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to list the previews of frontend %q: %w", frontend.Name, err)
	}
	return frontend, candidates, nil
}

// GetCandidate returns one Candidate of a frontend, named by name or ID.
func (s Service) GetCandidate(ctx context.Context, frontendIdentifier, candidateIdentifier string) (*apiclient.FrontendCandidate, error) {
	authenticated, frontend, err := s.currentFrontend(ctx, frontendIdentifier)
	if err != nil {
		return nil, err
	}
	return resolveCandidate(ctx, authenticated, frontend, candidateIdentifier)
}

// DeployCandidate uploads a new archive to an existing Candidate.
func (s Service) DeployCandidate(ctx context.Context, request CandidateDeploy) (*apiclient.FrontendCandidate, *apiclient.FrontendCandidateDeployment, error) {
	authenticated, frontend, err := s.currentFrontend(ctx, request.Frontend)
	if err != nil {
		return nil, nil, err
	}
	candidate, err := resolveCandidate(ctx, authenticated, frontend, request.Candidate)
	if err != nil {
		return nil, nil, err
	}
	deployment, err := authenticated.API.RedeployFrontendCandidate(ctx, authenticated.ProjectID, frontend.Id, candidate.Id,
		api.FrontendCandidateInput{Archive: request.Archive})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to deploy to preview %q: %w", candidate.Name, err)
	}
	return candidate, deployment, nil
}

// DeleteCandidate starts deleting a Candidate.
func (s Service) DeleteCandidate(ctx context.Context, frontendIdentifier, candidateIdentifier string) (*apiclient.FrontendCandidate, error) {
	authenticated, frontend, err := s.currentFrontend(ctx, frontendIdentifier)
	if err != nil {
		return nil, err
	}
	candidate, err := resolveCandidate(ctx, authenticated, frontend, candidateIdentifier)
	if err != nil {
		return nil, err
	}
	if err := authenticated.API.DeleteFrontendCandidate(ctx, authenticated.ProjectID, frontend.Id, candidate.Id); err != nil {
		return nil, fmt.Errorf("failed to delete preview %q: %w", candidate.Name, err)
	}
	return candidate, nil
}

// Serving reports what a frontend serves.
func (s Service) Serving(ctx context.Context, frontendIdentifier string) (*apiclient.Frontend, *apiclient.FrontendServing, error) {
	authenticated, frontend, err := s.currentFrontend(ctx, frontendIdentifier)
	if err != nil {
		return nil, nil, err
	}
	serving, err := authenticated.API.GetFrontendServing(ctx, authenticated.ProjectID, frontend.Id)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read what frontend %q serves: %w", frontend.Name, err)
	}
	return frontend, serving, nil
}

// PromoteCandidate makes a Candidate what its frontend serves. It names the
// revision it just read, so a promotion or rollback that committed in between
// makes it fail instead of overwriting it.
func (s Service) PromoteCandidate(ctx context.Context, frontendIdentifier, candidateIdentifier string) (*apiclient.FrontendCandidate, *apiclient.FrontendServing, error) {
	authenticated, frontend, err := s.currentFrontend(ctx, frontendIdentifier)
	if err != nil {
		return nil, nil, err
	}
	candidate, err := resolveCandidate(ctx, authenticated, frontend, candidateIdentifier)
	if err != nil {
		return nil, nil, err
	}
	current, err := authenticated.API.GetFrontendServing(ctx, authenticated.ProjectID, frontend.Id)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read what frontend %q serves: %w", frontend.Name, err)
	}
	serving, err := authenticated.API.PromoteFrontendCandidate(ctx, authenticated.ProjectID, frontend.Id, candidate.Id, current.Revision)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to promote preview %q: %w", candidate.Name, err)
	}
	return candidate, serving, nil
}

// RollbackServing returns a frontend to the runtime it served before, or to
// its own runtime when toProduction is set.
func (s Service) RollbackServing(ctx context.Context, frontendIdentifier string, toProduction bool) (*apiclient.Frontend, *apiclient.FrontendServing, error) {
	authenticated, frontend, err := s.currentFrontend(ctx, frontendIdentifier)
	if err != nil {
		return nil, nil, err
	}
	current, err := authenticated.API.GetFrontendServing(ctx, authenticated.ProjectID, frontend.Id)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read what frontend %q serves: %w", frontend.Name, err)
	}
	serving, err := authenticated.API.RollbackFrontendServing(ctx, authenticated.ProjectID, frontend.Id, current.Revision, toProduction)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to roll back frontend %q: %w", frontend.Name, err)
	}
	return frontend, serving, nil
}

// CandidateName returns the name of the Candidate a serving state names, or
// its ID when the frontend has no Candidate of that ID any more.
func (s Service) CandidateName(ctx context.Context, frontend *apiclient.Frontend, id *uuid.UUID) (string, error) {
	if id == nil {
		return "", nil
	}
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return "", err
	}
	candidates, err := authenticated.API.ListFrontendCandidates(ctx, authenticated.ProjectID, frontend.Id)
	if err != nil {
		return id.String(), err
	}
	for i := range candidates {
		if candidates[i].Id == *id {
			return candidates[i].Name, nil
		}
	}
	return id.String(), nil
}

// resolveCandidate finds a Candidate of the frontend by ID or by name.
func resolveCandidate(ctx context.Context, authenticated *clisession.ProjectSession, frontend *apiclient.Frontend, identifier string) (*apiclient.FrontendCandidate, error) {
	target := strings.TrimSpace(identifier)
	if target == "" {
		return nil, errors.New("preview identifier cannot be empty")
	}
	candidates, err := authenticated.API.ListFrontendCandidates(ctx, authenticated.ProjectID, frontend.Id)
	if err != nil {
		return nil, fmt.Errorf("failed to list the previews of frontend %q: %w", frontend.Name, err)
	}
	id, parseErr := uuid.Parse(target)
	for i := range candidates {
		if (parseErr == nil && candidates[i].Id == id) || candidates[i].Name == target {
			return &candidates[i], nil
		}
	}
	return nil, fmt.Errorf("preview %q of frontend %q %w", target, frontend.Name, api.ErrNotFound)
}
