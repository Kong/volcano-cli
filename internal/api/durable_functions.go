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

// DurableFunctionDeployInput contains one packaged durable function source archive.
type DurableFunctionDeployInput struct {
	Name          string
	Runtime       string
	Handler       string
	SourceArchive []byte
	IsPublic      *bool
}

// DurableExecutionStartInput contains one durable execution start request. Name
// is the idempotency key: repeating a start with the same one returns the
// execution that already exists rather than beginning a second.
type DurableExecutionStartInput struct {
	Input map[string]any
	Name  string
}

// ListDurableFunctions lists one durable function page for a project.
func (c *Client) ListDurableFunctions(ctx context.Context, projectID uuid.UUID, page, limit int) (*apiclient.PaginatedDurableFunctions, error) {
	resp, err := c.client.ListDurableFunctionsWithResponse(ctx, projectID, &apiclient.ListDurableFunctionsParams{
		Page:  &page,
		Limit: &limit,
	})
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON400, resp.JSON404)
}

// DeployDurableFunction deploys one durable function source archive. It creates
// the function on the first call for a name and redeploys it after that.
func (c *Client) DeployDurableFunction(ctx context.Context, projectID uuid.UUID, fn DurableFunctionDeployInput) (*apiclient.DurableFunction, error) {
	body, contentType, err := buildDurableFunctionDeployMultipart(fn)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.CreateDurableFunctionWithBodyWithResponse(ctx, projectID, contentType, body)
	if err != nil {
		return nil, err
	}
	if resp.JSON201 != nil {
		return resp.JSON201, nil
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200,
		resp.JSON400, resp.JSON403, resp.JSON409, resp.JSON503)
}

// GetDurableFunction returns one durable function by ID or name.
func (c *Client) GetDurableFunction(ctx context.Context, projectID uuid.UUID, function string) (*apiclient.DurableFunction, error) {
	resp, err := c.client.GetDurableFunctionWithResponse(ctx, projectID, function)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON404)
}

// DeleteDurableFunction starts deleting one durable function by ID or name.
func (c *Client) DeleteDurableFunction(ctx context.Context, projectID uuid.UUID, function string) error {
	resp, err := c.client.DeleteDurableFunctionWithResponse(ctx, projectID, function)
	if err != nil {
		return err
	}
	return apiOK(resp.StatusCode(), resp.Body, resp.JSON404)
}

// StartDurableExecution starts one durable execution and returns its handle.
// A durable execution outlives any request that could wait for it, so this
// never carries a result.
func (c *Client) StartDurableExecution(
	ctx context.Context, projectID uuid.UUID, function string, input DurableExecutionStartInput,
) (*apiclient.DurableExecution, error) {
	params := &apiclient.StartDurableExecutionParams{}
	if input.Name != "" {
		name := input.Name
		params.XVolcanoExecutionName = &name
	}
	// The body is the execution's input itself rather than a wrapper, so an
	// absent input has to be sent as JSON null.
	var body apiclient.StartDurableExecutionJSONRequestBody
	if input.Input != nil {
		body = input.Input
	}

	resp, err := c.client.StartDurableExecutionWithResponse(ctx, projectID, function, params, body)
	if err != nil {
		return nil, err
	}
	if resp.JSON202 != nil {
		return resp.JSON202, nil
	}
	return nil, apiErrorFromGeneratedErrors(resp.StatusCode(), resp.Body,
		resp.JSON400, resp.JSON404, resp.JSON409, resp.JSON413, resp.JSON429, resp.JSON503)
}

// ListDurableExecutions lists one execution page for a durable function. The
// statuses each entry carries are the last the platform observed rather than
// live state; fetch a single execution for that.
func (c *Client) ListDurableExecutions(
	ctx context.Context, projectID uuid.UUID, function, status string, page, limit int,
) (*apiclient.PaginatedDurableExecutions, error) {
	params := &apiclient.ListDurableExecutionsParams{Page: &page, Limit: &limit}
	if status != "" {
		executionStatus := apiclient.DurableExecutionStatus(status)
		params.Status = &executionStatus
	}

	resp, err := c.client.ListDurableExecutionsWithResponse(ctx, projectID, function, params)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON400, resp.JSON404)
}

// GetDurableExecution returns one durable execution, including its result once
// it has finished.
func (c *Client) GetDurableExecution(
	ctx context.Context, projectID uuid.UUID, function string, executionID uuid.UUID,
) (*apiclient.DurableExecution, error) {
	resp, err := c.client.GetDurableExecutionWithResponse(ctx, projectID, function, executionID)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON404, resp.JSON503)
}

// StopDurableExecution stops one durable execution. Completed steps are not
// undone; the execution stops where it is.
func (c *Client) StopDurableExecution(
	ctx context.Context, projectID uuid.UUID, function string, executionID uuid.UUID,
) (*apiclient.DurableExecution, error) {
	resp, err := c.client.StopDurableExecutionWithResponse(ctx, projectID, function, executionID)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON404, resp.JSON409, resp.JSON503)
}

// ListDurableFunctionSchedulers lists schedulers for a durable function.
func (c *Client) ListDurableFunctionSchedulers(
	ctx context.Context, projectID uuid.UUID, function string,
) (*apiclient.FunctionSchedulerListResponse, error) {
	resp, err := c.client.ListDurableFunctionSchedulersWithResponse(ctx, projectID, function)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON404)
}

// CreateDurableFunctionScheduler creates one scheduler for a durable function.
// Each tick starts an execution rather than invoking the function.
func (c *Client) CreateDurableFunctionScheduler(
	ctx context.Context, projectID uuid.UUID, function string, input FunctionSchedulerInput,
) (*apiclient.FunctionScheduler, error) {
	body := apiclient.CreateDurableFunctionSchedulerJSONRequestBody{
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

	resp, err := c.client.CreateDurableFunctionSchedulerWithResponse(ctx, projectID, function, body)
	if err != nil {
		return nil, err
	}
	if resp.JSON201 != nil {
		return resp.JSON201, nil
	}
	return nil, apiErrorFromGeneratedErrors(resp.StatusCode(), resp.Body, resp.JSON400, resp.JSON404)
}

// UpdateDurableFunctionScheduler updates one scheduler of a durable function.
func (c *Client) UpdateDurableFunctionScheduler(
	ctx context.Context, projectID uuid.UUID, function string, schedulerID uuid.UUID, input FunctionSchedulerInput,
) (*apiclient.FunctionScheduler, error) {
	body := apiclient.UpdateDurableFunctionSchedulerJSONRequestBody{
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

	resp, err := c.client.UpdateDurableFunctionSchedulerWithResponse(ctx, projectID, function, schedulerID, body)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON400, resp.JSON404)
}

// DeleteDurableFunctionScheduler deletes one scheduler of a durable function.
func (c *Client) DeleteDurableFunctionScheduler(
	ctx context.Context, projectID uuid.UUID, function string, schedulerID uuid.UUID,
) error {
	resp, err := c.client.DeleteDurableFunctionSchedulerWithResponse(ctx, projectID, function, schedulerID)
	if err != nil {
		return err
	}
	return apiOK(resp.StatusCode(), resp.Body, resp.JSON404)
}

func buildDurableFunctionDeployMultipart(fn DurableFunctionDeployInput) (*bytes.Buffer, string, error) {
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
	if fn.IsPublic != nil {
		if err := writer.WriteField("is_public", strconv.FormatBool(*fn.IsPublic)); err != nil {
			return nil, "", fmt.Errorf("failed to write is_public field: %w", err)
		}
	}
	if err := archive.WriteArchivePart(writer, "code", fn.Name, fn.SourceArchive); err != nil {
		return nil, "", err
	}
	if err := writer.Close(); err != nil {
		return nil, "", fmt.Errorf("failed to finalize multipart body: %w", err)
	}
	return body, writer.FormDataContentType(), nil
}
