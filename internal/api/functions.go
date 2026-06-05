package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"strings"

	"github.com/google/uuid"

	"github.com/Kong/volcano-cli/internal/apiclient"
	"github.com/Kong/volcano-cli/internal/archive"
)

// FunctionDeployInput contains one packaged function source archive.
type FunctionDeployInput struct {
	Name          string
	Runtime       string
	Handler       string
	SourceArchive []byte
}

// FunctionSchedulerInput contains one function scheduler create or update request.
type FunctionSchedulerInput struct {
	Name           string
	CronExpression string
	Payload        map[string]any
	Regions        []string
	Enabled        *bool
}

// ListFunctions lists one function page for a project.
func (c *Client) ListFunctions(ctx context.Context, projectID uuid.UUID, page, limit int) (*apiclient.PaginatedFunctions, error) {
	resp, err := c.client.ListFunctionsWithResponse(ctx, projectID, &apiclient.ListFunctionsParams{
		Page:  &page,
		Limit: &limit,
	})
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON404)
}

// DeployFunction deploys one function source archive.
func (c *Client) DeployFunction(ctx context.Context, projectID uuid.UUID, fn FunctionDeployInput) (*apiclient.Function, error) {
	body, contentType, err := buildFunctionDeployMultipart(fn)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.CreateFunctionWithBodyWithResponse(ctx, projectID, contentType, body)
	if err != nil {
		return nil, err
	}
	if resp.JSON201 != nil {
		return resp.JSON201, nil
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON400, resp.JSON403, resp.JSON500)
}

// DeployFunctionsBatch deploys one batch of function source archives.
func (c *Client) DeployFunctionsBatch(ctx context.Context, projectID uuid.UUID, functions []FunctionDeployInput) (*apiclient.BatchFunctionDeployResponse, error) {
	body, contentType, err := buildFunctionsBatchMultipart(functions)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.CreateFunctionsBatchWithBodyWithResponse(ctx, projectID, contentType, body)
	if err != nil {
		return nil, err
	}
	if resp.JSON202 != nil {
		return resp.JSON202, nil
	}
	if resp.JSON207 != nil {
		return resp.JSON207, nil
	}
	return nil, apiErrorFromGeneratedErrors(resp.StatusCode(), resp.Body, resp.JSON400)
}

// GetFunction returns one function by ID.
func (c *Client) GetFunction(ctx context.Context, projectID, functionID uuid.UUID) (*apiclient.Function, error) {
	resp, err := c.client.GetFunctionWithResponse(ctx, projectID, functionID)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON404)
}

// DeleteFunction starts deleting one function by ID.
func (c *Client) DeleteFunction(ctx context.Context, projectID, functionID uuid.UUID) error {
	resp, err := c.client.DeleteFunctionWithResponse(ctx, projectID, functionID)
	if err != nil {
		return err
	}
	return apiOK(resp.StatusCode(), resp.Body, resp.JSON404)
}

// UpdateFunctionVisibility updates one function's public/private visibility.
func (c *Client) UpdateFunctionVisibility(ctx context.Context, projectID, functionID uuid.UUID, isPublic bool) (*apiclient.Function, error) {
	resp, err := c.client.UpdateFunctionWithResponse(ctx, projectID, functionID, apiclient.UpdateFunctionJSONRequestBody{
		IsPublic: isPublic,
	})
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON400, resp.JSON404)
}

// ListFunctionRuntimes returns the function runtime catalog.
func (c *Client) ListFunctionRuntimes(ctx context.Context) ([]apiclient.FunctionRuntimeOption, error) {
	resp, err := c.client.ListFunctionRuntimesWithResponse(ctx)
	if err != nil {
		return nil, err
	}
	runtimes, err := apiResult(resp.StatusCode(), resp.Body, resp.JSON200)
	if err != nil {
		return nil, err
	}
	return runtimes.Runtimes, nil
}

// ListFunctionDeployments lists one deployment page for a function.
func (c *Client) ListFunctionDeployments(ctx context.Context, projectID, functionID uuid.UUID, page, limit int) (*apiclient.PaginatedFunctionDeployments, error) {
	resp, err := c.client.ListFunctionDeploymentsWithResponse(ctx, projectID, functionID, &apiclient.ListFunctionDeploymentsParams{
		Page:  &page,
		Limit: &limit,
	})
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON404)
}

// GetFunctionLogs returns one runtime log page for a function.
func (c *Client) GetFunctionLogs(ctx context.Context, projectID, functionID uuid.UUID, limit int, nextToken string) (*apiclient.GetLogsResponse, error) {
	params := &apiclient.GetFunctionLogsParams{}
	if limit > 0 {
		params.Limit = &limit
	}
	if nextToken = strings.TrimSpace(nextToken); nextToken != "" {
		params.NextToken = &nextToken
	}

	resp, err := c.client.GetFunctionLogsWithResponse(ctx, projectID, functionID, params)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON401, resp.JSON403, resp.JSON404)
}

func buildFunctionDeployMultipart(fn FunctionDeployInput) (*bytes.Buffer, string, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if err := writer.WriteField("name", fn.Name); err != nil {
		return nil, "", fmt.Errorf("failed to write name field: %w", err)
	}
	if err := writer.WriteField("runtime", fn.Runtime); err != nil {
		return nil, "", fmt.Errorf("failed to write runtime field: %w", err)
	}
	if err := writer.WriteField("handler", fn.Handler); err != nil {
		return nil, "", fmt.Errorf("failed to write handler field: %w", err)
	}
	if err := archive.WriteArchivePart(writer, "code", fn.Name, fn.SourceArchive); err != nil {
		return nil, "", err
	}
	if err := writer.Close(); err != nil {
		return nil, "", fmt.Errorf("failed to finalize multipart body: %w", err)
	}
	return body, writer.FormDataContentType(), nil
}

func buildFunctionsBatchMultipart(functions []FunctionDeployInput) (*bytes.Buffer, string, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	type manifestItem struct {
		Name      string `json:"name"`
		Runtime   string `json:"runtime"`
		Handler   string `json:"handler"`
		FileField string `json:"file_field"`
	}
	manifest := make([]manifestItem, 0, len(functions))
	for i, fn := range functions {
		fieldName := fmt.Sprintf("code_%d", i)
		manifest = append(manifest, manifestItem{
			Name:      fn.Name,
			Runtime:   fn.Runtime,
			Handler:   fn.Handler,
			FileField: fieldName,
		})
		if err := archive.WriteArchivePart(writer, fieldName, fn.Name, fn.SourceArchive); err != nil {
			return nil, "", err
		}
	}
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		return nil, "", fmt.Errorf("failed to marshal function manifest: %w", err)
	}
	if err := writer.WriteField("functions", string(manifestBytes)); err != nil {
		return nil, "", fmt.Errorf("failed to write functions field: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, "", fmt.Errorf("failed to finalize multipart body: %w", err)
	}
	return body, writer.FormDataContentType(), nil
}

// ListFunctionSchedulers lists schedulers for a function.
func (c *Client) ListFunctionSchedulers(ctx context.Context, projectID, functionID uuid.UUID) (*apiclient.FunctionSchedulerListResponse, error) {
	resp, err := c.client.ListFunctionSchedulersWithResponse(ctx, projectID, functionID)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200)
}

// CreateFunctionScheduler creates one scheduler for a function.
func (c *Client) CreateFunctionScheduler(ctx context.Context, projectID, functionID uuid.UUID, input FunctionSchedulerInput) (*apiclient.FunctionScheduler, error) {
	body := apiclient.CreateFunctionSchedulerJSONRequestBody{
		Name:    input.Name,
		Enabled: input.Enabled,
		Schedule: apiclient.ScheduleRequest{
			CronExpression: input.CronExpression,
		},
	}
	if input.Payload != nil {
		payload := input.Payload
		body.Payload = &payload
	}
	if input.Regions != nil {
		regions := input.Regions
		body.Regions = &regions
	}
	resp, err := c.client.CreateFunctionSchedulerWithResponse(ctx, projectID, functionID, body)
	if err != nil {
		return nil, err
	}
	if resp.JSON201 != nil {
		return resp.JSON201, nil
	}
	return nil, apiErrorFromGeneratedErrors(resp.StatusCode(), resp.Body, resp.JSON400)
}

// UpdateFunctionScheduler updates one scheduler for a function.
func (c *Client) UpdateFunctionScheduler(ctx context.Context, projectID, functionID, schedulerID uuid.UUID, input FunctionSchedulerInput) (*apiclient.FunctionScheduler, error) {
	body := apiclient.UpdateFunctionSchedulerJSONRequestBody{
		Enabled: input.Enabled,
	}
	if input.Name != "" {
		name := input.Name
		body.Name = &name
	}
	if input.CronExpression != "" {
		body.Schedule = &apiclient.ScheduleRequest{CronExpression: input.CronExpression}
	}
	if input.Payload != nil {
		payload := input.Payload
		body.Payload = &payload
	}
	if input.Regions != nil {
		regions := input.Regions
		body.Regions = &regions
	}
	resp, err := c.client.UpdateFunctionSchedulerWithResponse(ctx, projectID, functionID, schedulerID, body)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200)
}

// DeleteFunctionScheduler deletes one scheduler for a function.
func (c *Client) DeleteFunctionScheduler(ctx context.Context, projectID, functionID, schedulerID uuid.UUID) error {
	resp, err := c.client.DeleteFunctionSchedulerWithResponse(ctx, projectID, functionID, schedulerID)
	if err != nil {
		return err
	}
	return apiOK(resp.StatusCode(), resp.Body)
}

// GetFunctionDeploymentLogs returns one build log page for a function deployment.
func (c *Client) GetFunctionDeploymentLogs(ctx context.Context, projectID, functionID, deploymentID uuid.UUID, limit int, nextToken string) (*apiclient.GetLogsResponse, error) {
	params := &apiclient.GetFunctionDeploymentLogsParams{}
	if limit > 0 {
		params.Limit = &limit
	}
	if nextToken = strings.TrimSpace(nextToken); nextToken != "" {
		params.NextToken = &nextToken
	}

	resp, err := c.client.GetFunctionDeploymentLogsWithResponse(ctx, projectID, functionID, deploymentID, params)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON401, resp.JSON403, resp.JSON404)
}
