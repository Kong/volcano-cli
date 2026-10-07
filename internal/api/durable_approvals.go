package api

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

// DurableApprovalListInput filters one approval page. Empty and nil fields
// apply no filter, so an empty Status lists approvals in every status.
type DurableApprovalListInput struct {
	Function    string
	Status      string
	ExecutionID *uuid.UUID
	From        *time.Time
	To          *time.Time
	Page        int
	Limit       int
}

// ListDurableApprovals lists one approval page for a project, newest first.
func (c *Client) ListDurableApprovals(
	ctx context.Context, projectID uuid.UUID, input DurableApprovalListInput,
) (*apiclient.PaginatedDurableApprovals, error) {
	params := &apiclient.ListDurableApprovalsParams{
		Page:        &input.Page,
		Limit:       &input.Limit,
		ExecutionId: input.ExecutionID,
		From:        input.From,
		To:          input.To,
	}
	if input.Function != "" {
		function := input.Function
		params.Function = &function
	}
	if input.Status != "" {
		status := apiclient.DurableApprovalStatus(input.Status)
		params.Status = &status
	}

	resp, err := c.client.ListDurableApprovalsWithResponse(ctx, projectID, params)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON400)
}

// GetDurableApproval returns one approval, including its decision once a
// person has made one.
func (c *Client) GetDurableApproval(
	ctx context.Context, projectID, approvalID uuid.UUID,
) (*apiclient.DurableApproval, error) {
	resp, err := c.client.GetDurableApprovalWithResponse(ctx, projectID, approvalID)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON404)
}

// GetDurableApprovalStats counts a project's approvals by outcome from `from`
// until `to`. A nil to is the API's now, and a nil from 30 days before to.
func (c *Client) GetDurableApprovalStats(
	ctx context.Context, projectID uuid.UUID, function string, from, to *time.Time,
) (*apiclient.DurableApprovalStats, error) {
	params := &apiclient.GetDurableApprovalStatsParams{From: from, To: to}
	if function != "" {
		params.Function = &function
	}

	resp, err := c.client.GetDurableApprovalStatsWithResponse(ctx, projectID, params)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON400)
}

// ApproveDurableApproval approves a pending approval, and the workflow
// resumes with the decision. Only a person's credential may decide.
func (c *Client) ApproveDurableApproval(
	ctx context.Context, projectID, approvalID uuid.UUID, comment string,
) (*apiclient.DurableApproval, error) {
	resp, err := c.client.ApproveDurableApprovalWithResponse(
		ctx, projectID, approvalID, durableApprovalDecisionBody(comment))
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200,
		resp.JSON400, resp.JSON403, resp.JSON404, resp.JSON409)
}

// DenyDurableApproval denies a pending approval. A denial resumes the workflow
// too: it is a decision, not a failure.
func (c *Client) DenyDurableApproval(
	ctx context.Context, projectID, approvalID uuid.UUID, comment string,
) (*apiclient.DurableApproval, error) {
	resp, err := c.client.DenyDurableApprovalWithResponse(
		ctx, projectID, approvalID, durableApprovalDecisionBody(comment))
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200,
		resp.JSON400, resp.JSON403, resp.JSON404, resp.JSON409)
}

func durableApprovalDecisionBody(comment string) apiclient.DurableApprovalDecisionRequest {
	if comment == "" {
		return apiclient.DurableApprovalDecisionRequest{}
	}
	return apiclient.DurableApprovalDecisionRequest{Comment: &comment}
}
