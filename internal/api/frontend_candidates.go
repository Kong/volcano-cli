package api

import (
	"bytes"
	"context"
	"fmt"
	"mime/multipart"
	"strconv"

	"github.com/google/uuid"

	"github.com/Kong/volcano-cli/internal/apiclient"
	"github.com/Kong/volcano-cli/internal/archive"
)

// FrontendCandidateInput is a request to create or redeploy a frontend
// Candidate. A redeploy sends only the archive, so Name and TTLSeconds are empty
// then. TTLSeconds is zero to keep the Candidate until it is deleted.
type FrontendCandidateInput struct {
	Name       string
	TTLSeconds int
	Archive    []byte
}

// ListFrontendCandidates lists a frontend's Candidates.
func (c *Client) ListFrontendCandidates(ctx context.Context, projectID, frontendID uuid.UUID) ([]apiclient.FrontendCandidate, error) {
	resp, err := c.client.ListFrontendCandidatesWithResponse(ctx, projectID, frontendID)
	if err != nil {
		return nil, err
	}
	list, err := apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON400, resp.JSON401, resp.JSON403, resp.JSON404, resp.JSON500)
	if err != nil {
		return nil, err
	}
	return list.Data, nil
}

// CreateFrontendCandidate uploads a source archive and starts the Candidate's
// first deployment.
func (c *Client) CreateFrontendCandidate(ctx context.Context, projectID, frontendID uuid.UUID, input FrontendCandidateInput) (*apiclient.FrontendCandidate, error) {
	body, contentType, err := buildFrontendCandidateMultipart(input)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.CreateFrontendCandidateWithBodyWithResponse(ctx, projectID, frontendID, contentType, body)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON202, resp.JSON400, resp.JSON401, resp.JSON403, resp.JSON404, resp.JSON409, resp.JSON500, resp.JSON503)
}

// GetFrontendCandidate returns one Candidate.
func (c *Client) GetFrontendCandidate(ctx context.Context, projectID, frontendID, candidateID uuid.UUID) (*apiclient.FrontendCandidate, error) {
	resp, err := c.client.GetFrontendCandidateWithResponse(ctx, projectID, frontendID, candidateID)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON400, resp.JSON401, resp.JSON403, resp.JSON404, resp.JSON500)
}

// DeleteFrontendCandidate starts deleting a Candidate.
func (c *Client) DeleteFrontendCandidate(ctx context.Context, projectID, frontendID, candidateID uuid.UUID) error {
	resp, err := c.client.DeleteFrontendCandidateWithResponse(ctx, projectID, frontendID, candidateID)
	if err != nil {
		return err
	}
	return apiOK(resp.StatusCode(), resp.Body, resp.JSON400, resp.JSON401, resp.JSON403, resp.JSON404, resp.JSON500)
}

// RedeployFrontendCandidate uploads a new archive to an existing Candidate and
// starts a deployment of it.
func (c *Client) RedeployFrontendCandidate(ctx context.Context, projectID, frontendID, candidateID uuid.UUID, input FrontendCandidateInput) (*apiclient.FrontendCandidateDeployment, error) {
	body, contentType, err := buildFrontendCandidateMultipart(input)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.RedeployFrontendCandidateWithBodyWithResponse(ctx, projectID, frontendID, candidateID, contentType, body)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON202, resp.JSON400, resp.JSON401, resp.JSON403, resp.JSON404, resp.JSON409, resp.JSON500, resp.JSON503)
}

// GetFrontendServing reports what a frontend serves and the revision a
// promotion or rollback must name.
func (c *Client) GetFrontendServing(ctx context.Context, projectID, frontendID uuid.UUID) (*apiclient.FrontendServing, error) {
	resp, err := c.client.GetFrontendServingWithResponse(ctx, projectID, frontendID)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON400, resp.JSON401, resp.JSON403, resp.JSON404, resp.JSON500)
}

// PromoteFrontendCandidate makes a Candidate what the frontend serves. The
// revision is the one GetFrontendServing returned.
func (c *Client) PromoteFrontendCandidate(ctx context.Context, projectID, frontendID, candidateID uuid.UUID, expectedRevision int64) (*apiclient.FrontendServing, error) {
	resp, err := c.client.PromoteFrontendCandidateWithResponse(ctx, projectID, frontendID, candidateID,
		apiclient.PromoteFrontendCandidateJSONRequestBody{ExpectedRevision: expectedRevision})
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON400, resp.JSON401, resp.JSON403, resp.JSON404, resp.JSON409, resp.JSON500)
}

// RollbackFrontendServing returns a frontend to the runtime it served before,
// or to its own runtime when toProduction is set.
func (c *Client) RollbackFrontendServing(ctx context.Context, projectID, frontendID uuid.UUID, expectedRevision int64, toProduction bool) (*apiclient.FrontendServing, error) {
	request := apiclient.RollbackFrontendServingJSONRequestBody{ExpectedRevision: expectedRevision}
	if toProduction {
		request.ToProduction = &toProduction
	}
	resp, err := c.client.RollbackFrontendServingWithResponse(ctx, projectID, frontendID, request)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON400, resp.JSON401, resp.JSON403, resp.JSON404, resp.JSON409, resp.JSON500)
}

func buildFrontendCandidateMultipart(input FrontendCandidateInput) (*bytes.Buffer, string, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if input.Name != "" {
		if err := writer.WriteField("name", input.Name); err != nil {
			return nil, "", fmt.Errorf("failed to write name field: %w", err)
		}
	}
	if input.TTLSeconds > 0 {
		if err := writer.WriteField("ttl_seconds", strconv.Itoa(input.TTLSeconds)); err != nil {
			return nil, "", fmt.Errorf("failed to write ttl_seconds field: %w", err)
		}
	}
	archiveName := input.Name
	if archiveName == "" {
		archiveName = "frontend"
	}
	if err := archive.WriteArchivePart(writer, "archive", archiveName, input.Archive); err != nil {
		return nil, "", err
	}
	if err := writer.Close(); err != nil {
		return nil, "", fmt.Errorf("failed to finalize multipart body: %w", err)
	}
	return body, writer.FormDataContentType(), nil
}
