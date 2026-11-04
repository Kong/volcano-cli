package api

import (
	"context"

	"github.com/google/uuid"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

// ListIssues returns one page of a project's issues, most recently seen
// first. A cursor continues only the list with the same filters.
func (c *Client) ListIssues(
	ctx context.Context, projectID uuid.UUID, params apiclient.ListIssuesParams,
) (*apiclient.PaginatedIssues, error) {
	resp, err := c.client.ListIssuesWithResponse(ctx, projectID, &params)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON400, resp.JSON404)
}

// GetIssue returns one of a project's issues.
func (c *Client) GetIssue(ctx context.Context, projectID, issueID uuid.UUID) (*apiclient.Issue, error) {
	resp, err := c.client.GetIssueWithResponse(ctx, projectID, issueID)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON404)
}

// Issue notes are a top-level collection that names one project: project_id
// is a query parameter on the list, a body field on a write, and optional on
// a note's own routes, where naming it answers another project's note as
// unknown.

// ListIssueNotes returns one page of a project's issue notes, newest first
// unless params say otherwise.
func (c *Client) ListIssueNotes(
	ctx context.Context, projectID uuid.UUID, params apiclient.ListIssueNotesParams,
) (*apiclient.PaginatedIssueNotes, error) {
	params.ProjectId = projectID
	resp, err := c.client.ListIssueNotesWithResponse(ctx, &params)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON400, resp.JSON401, resp.JSON404)
}

// CreateIssueNote writes a note on an issue, or on one LavaFlow run of it.
// Only the project's owner may, as a person. The same text sent again within
// a few minutes answers with the note already written.
func (c *Client) CreateIssueNote(
	ctx context.Context, projectID uuid.UUID, body apiclient.IssueNoteCreate,
) (*apiclient.IssueNote, error) {
	body.ProjectId = projectID
	resp, err := c.client.CreateIssueNoteWithResponse(ctx, body)
	if err != nil {
		return nil, err
	}
	note := resp.JSON201
	if note == nil {
		note = resp.JSON200
	}
	note, err = apiResult(resp.StatusCode(), resp.Body, note, resp.JSON400, resp.JSON401, resp.JSON403,
		resp.JSON404, resp.JSON409, resp.JSON413, resp.JSON429)
	if err != nil && resp.HTTPResponse != nil {
		return nil, withRetryAfter(err, resp.HTTPResponse.Header)
	}
	return note, err
}

// UpdateIssueNote replaces a note's text. Only its author may, as a person.
func (c *Client) UpdateIssueNote(
	ctx context.Context, projectID, noteID uuid.UUID, body apiclient.IssueNoteUpdate,
) (*apiclient.IssueNote, error) {
	resp, err := c.client.UpdateIssueNoteWithResponse(ctx, noteID, &apiclient.UpdateIssueNoteParams{ProjectId: &projectID}, body)
	if err != nil {
		return nil, err
	}
	note, err := apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON400, resp.JSON401, resp.JSON403,
		resp.JSON404, resp.JSON413, resp.JSON429)
	if err != nil && resp.HTTPResponse != nil {
		return nil, withRetryAfter(err, resp.HTTPResponse.Header)
	}
	return note, err
}

// DeleteIssueNote deletes a note for good. Its author or the project's owner
// may, as a person.
func (c *Client) DeleteIssueNote(ctx context.Context, projectID, noteID uuid.UUID) error {
	resp, err := c.client.DeleteIssueNoteWithResponse(ctx, noteID, &apiclient.DeleteIssueNoteParams{ProjectId: &projectID})
	if err != nil {
		return err
	}
	return apiOK(resp.StatusCode(), resp.Body, resp.JSON400, resp.JSON401, resp.JSON403, resp.JSON404)
}
