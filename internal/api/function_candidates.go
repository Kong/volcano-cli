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

// FunctionCandidateInput is a request to create or redeploy a function
// Candidate. A redeploy sends only the source, so Name and TTLSeconds are empty
// then. TTLSeconds is zero to keep the Candidate until it is deleted.
type FunctionCandidateInput struct {
	Name       string
	TTLSeconds int
	Source     []byte
}

// ListFunctionCandidates lists a function's Candidates.
func (c *Client) ListFunctionCandidates(ctx context.Context, projectID, functionID uuid.UUID) ([]apiclient.FunctionCandidate, error) {
	resp, err := c.client.ListFunctionCandidatesWithResponse(ctx, projectID, functionID)
	if err != nil {
		return nil, err
	}
	list, err := apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON400, resp.JSON401, resp.JSON403, resp.JSON404, resp.JSON500)
	if err != nil {
		return nil, err
	}
	return list.Data, nil
}

// CreateFunctionCandidate uploads a source archive and starts the Candidate's
// first deployment.
func (c *Client) CreateFunctionCandidate(ctx context.Context, projectID, functionID uuid.UUID, input FunctionCandidateInput) (*apiclient.FunctionCandidate, error) {
	body, contentType, err := buildFunctionCandidateMultipart(input)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.CreateFunctionCandidateWithBodyWithResponse(ctx, projectID, functionID, contentType, body)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON202, resp.JSON400, resp.JSON401, resp.JSON403, resp.JSON404, resp.JSON409, resp.JSON429, resp.JSON500, resp.JSON503)
}

// GetFunctionCandidate returns one Candidate.
func (c *Client) GetFunctionCandidate(ctx context.Context, projectID, functionID, candidateID uuid.UUID) (*apiclient.FunctionCandidate, error) {
	resp, err := c.client.GetFunctionCandidateWithResponse(ctx, projectID, functionID, candidateID)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON400, resp.JSON401, resp.JSON403, resp.JSON404, resp.JSON500)
}

// DeleteFunctionCandidate starts deleting a Candidate.
func (c *Client) DeleteFunctionCandidate(ctx context.Context, projectID, functionID, candidateID uuid.UUID) error {
	resp, err := c.client.DeleteFunctionCandidateWithResponse(ctx, projectID, functionID, candidateID)
	if err != nil {
		return err
	}
	return apiOK(resp.StatusCode(), resp.Body, resp.JSON400, resp.JSON401, resp.JSON403, resp.JSON404, resp.JSON409, resp.JSON500)
}

// RedeployFunctionCandidate uploads a new source to an existing Candidate and
// starts a deployment of it.
func (c *Client) RedeployFunctionCandidate(ctx context.Context, projectID, functionID, candidateID uuid.UUID, input FunctionCandidateInput) (*apiclient.FunctionCandidateDeployment, error) {
	body, contentType, err := buildFunctionCandidateMultipart(input)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.RedeployFunctionCandidateWithBodyWithResponse(ctx, projectID, functionID, candidateID, contentType, body)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON202, resp.JSON400, resp.JSON401, resp.JSON403, resp.JSON404, resp.JSON409, resp.JSON429, resp.JSON500)
}

// GetFunctionServing reports what a function runs and the revision a
// promotion or rollback names.
func (c *Client) GetFunctionServing(ctx context.Context, projectID, functionID uuid.UUID) (*apiclient.FunctionServing, error) {
	resp, err := c.client.GetFunctionServingWithResponse(ctx, projectID, functionID)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON400, resp.JSON401, resp.JSON403, resp.JSON404, resp.JSON500)
}

// PromoteFunctionCandidate points a function's traffic at a Candidate's
// Lambda. The revision is the one GetFunctionServing returned.
func (c *Client) PromoteFunctionCandidate(ctx context.Context, projectID, functionID, candidateID uuid.UUID, expectedRevision int64) (*apiclient.FunctionServing, error) {
	resp, err := c.client.PromoteFunctionCandidateWithResponse(ctx, projectID, functionID, candidateID,
		apiclient.PromoteFunctionCandidateJSONRequestBody{ExpectedRevision: &expectedRevision})
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON400, resp.JSON401, resp.JSON403, resp.JSON404, resp.JSON409, resp.JSON500)
}

// RollbackFunctionServing points a function back at what served it before, or
// at its own Lambda when toProduction is set.
func (c *Client) RollbackFunctionServing(ctx context.Context, projectID, functionID uuid.UUID, expectedRevision int64, toProduction bool) (*apiclient.FunctionServing, error) {
	request := apiclient.RollbackFunctionServingJSONRequestBody{ExpectedRevision: &expectedRevision}
	if toProduction {
		request.ToProduction = &toProduction
	}
	resp, err := c.client.RollbackFunctionServingWithResponse(ctx, projectID, functionID, request)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON400, resp.JSON401, resp.JSON403, resp.JSON404, resp.JSON409, resp.JSON500)
}

func buildFunctionCandidateMultipart(input FunctionCandidateInput) (*bytes.Buffer, string, error) {
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
	sourceName := input.Name
	if sourceName == "" {
		sourceName = "function"
	}
	if err := archive.WriteArchivePart(writer, "code", sourceName, input.Source); err != nil {
		return nil, "", err
	}
	if err := writer.Close(); err != nil {
		return nil, "", fmt.Errorf("failed to finalize multipart body: %w", err)
	}
	return body, writer.FormDataContentType(), nil
}
