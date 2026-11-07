package api

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

// ListShares returns the project's shares, newest first.
func (c *Client) ListShares(ctx context.Context, projectID uuid.UUID) ([]apiclient.Share, error) {
	resp, err := c.client.ListSharesWithResponse(ctx, &apiclient.ListSharesParams{ProjectId: projectID})
	if err != nil {
		return nil, err
	}
	shares, err := apiResult(resp.StatusCode(), resp.Body, resp.JSON200,
		resp.JSON400, resp.JSON401, resp.JSON404, resp.JSON500)
	if err != nil {
		return nil, err
	}
	return shares.Shares, nil
}

// CreateShare creates a draft share for the project the request names. A nil
// field leaves that setting to the API's default.
func (c *Client) CreateShare(ctx context.Context, request apiclient.ShareCreateRequest) (*apiclient.Share, error) {
	resp, err := c.client.CreateShareWithResponse(ctx, request)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON201,
		resp.JSON400, resp.JSON401, resp.JSON403, resp.JSON404, resp.JSON409, resp.JSON500)
}

// GetShare returns one of the project's shares.
func (c *Client) GetShare(ctx context.Context, projectID uuid.UUID, shareID string) (*apiclient.Share, error) {
	resp, err := c.client.GetShareWithResponse(ctx, shareID, &apiclient.GetShareParams{ProjectId: &projectID})
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200,
		resp.JSON400, resp.JSON401, resp.JSON404, resp.JSON500)
}

// UpdateShare changes the fields request sets and returns the share.
func (c *Client) UpdateShare(
	ctx context.Context, projectID uuid.UUID, shareID string, request apiclient.ShareUpdateRequest,
) (*apiclient.Share, error) {
	resp, err := c.client.UpdateShareWithResponse(ctx, shareID, &apiclient.UpdateShareParams{ProjectId: &projectID}, request)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200,
		resp.JSON400, resp.JSON401, resp.JSON403, resp.JSON404, resp.JSON500)
}

// GetShareStats returns a share's click and fork counts. A days of zero or
// less leaves the window to the API's default.
func (c *Client) GetShareStats(
	ctx context.Context, projectID uuid.UUID, shareID string, days int,
) (*apiclient.ShareStats, error) {
	params := &apiclient.GetShareStatsParams{ShareId: shareID, ProjectId: &projectID}
	if days > 0 {
		params.Days = &days
	}
	resp, err := c.client.GetShareStatsWithResponse(ctx, params)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200,
		resp.JSON400, resp.JSON401, resp.JSON404, resp.JSON500)
}

// GetSharePreview returns the release a share offers. The route is public, so
// the client may carry no credential. A refusal that says when to try again
// keeps its Retry-After; see RetryAt.
func (c *Client) GetSharePreview(ctx context.Context, shareID string) (*apiclient.SharePreview, error) {
	resp, err := c.client.GetSharePreviewWithResponse(ctx, shareID)
	if err != nil {
		return nil, err
	}
	preview, err := apiResult(resp.StatusCode(), resp.Body, resp.JSON200,
		resp.JSON400, resp.JSON403, resp.JSON404, resp.JSON429, resp.JSON451, resp.JSON500, resp.JSON503)
	if err != nil && resp.HTTPResponse != nil {
		return nil, withRetryAfter(err, resp.HTTPResponse.Header)
	}
	return preview, err
}

// GetShareReleaseDraft returns what publishing to a share would release now.
// Each call is a full build that counts against the account's hourly release
// build budget. A refusal that says when to try again keeps its Retry-After.
func (c *Client) GetShareReleaseDraft(
	ctx context.Context, projectID uuid.UUID, shareID string,
) (*apiclient.ShareReleaseDraft, error) {
	resp, err := c.client.GetShareReleaseDraftWithResponse(ctx,
		&apiclient.GetShareReleaseDraftParams{ShareId: shareID, ProjectId: &projectID})
	if err != nil {
		return nil, err
	}
	draft, err := apiResult(resp.StatusCode(), resp.Body, resp.JSON200,
		resp.JSON400, resp.JSON401, resp.JSON403, resp.JSON404, resp.JSON422, resp.JSON429, resp.JSON500, resp.JSON503)
	if err != nil && resp.HTTPResponse != nil {
		return nil, withRetryAfter(err, resp.HTTPResponse.Header)
	}
	return draft, err
}

// ShareReleasePage is one page of a share's releases, newest first.
type ShareReleasePage struct {
	Releases []apiclient.ShareRelease
	// NextCursor reads the next page; empty on the last one.
	NextCursor string
}

// ListShareReleases returns one page of a share's releases, newest first. A
// limit of zero or less leaves the page size to the API; an empty cursor reads
// the first page. A cursor works only with the limit it was issued for.
func (c *Client) ListShareReleases(
	ctx context.Context, projectID uuid.UUID, shareID string, limit int, cursor string,
) (*ShareReleasePage, error) {
	params := &apiclient.ListShareReleasesParams{ShareId: shareID, ProjectId: &projectID}
	if limit > 0 {
		params.Limit = &limit
	}
	if cursor != "" {
		params.Cursor = &cursor
	}
	resp, err := c.client.ListShareReleasesWithResponse(ctx, params)
	if err != nil {
		return nil, err
	}
	page, err := apiResult(resp.StatusCode(), resp.Body, resp.JSON200,
		resp.JSON400, resp.JSON401, resp.JSON404, resp.JSON500)
	if err != nil {
		return nil, err
	}
	result := &ShareReleasePage{Releases: page.Data}
	if page.HasMore && page.NextCursor != nil {
		result.NextCursor = *page.NextCursor
	}
	return result, nil
}

// DraftChangedCode is the code of the 409 that refuses a publish because a
// fresh build of the project is not the draft that was confirmed.
const DraftChangedCode = "draft_changed"

// DraftChangedError is a refused publish. Draft is what the share would
// release now, with its own draft id, for the owner to review before
// publishing again.
type DraftChangedError struct {
	Err   *Error
	Draft apiclient.ShareReleaseDraft
}

func (e *DraftChangedError) Error() string { return e.Err.Error() }

func (e *DraftChangedError) Unwrap() error { return e.Err }

// PublishShareRelease publishes the reviewed draft with the confirmed variables
// and service key functions. A draft_changed refusal is a *DraftChangedError;
// a refusal that says when to try again keeps its Retry-After.
func (c *Client) PublishShareRelease(
	ctx context.Context, request apiclient.ShareReleaseCreateRequest,
) (*apiclient.ShareRelease, error) {
	resp, err := c.client.PublishShareReleaseWithResponse(ctx, request)
	if err != nil {
		return nil, err
	}
	if changed := resp.JSON409; changed != nil && changed.Code == DraftChangedCode {
		return nil, &DraftChangedError{
			Err: &Error{
				StatusCode: resp.StatusCode(), Message: strings.TrimSpace(changed.Error),
				Code: string(changed.Code), fromAPI: true,
			},
			Draft: changed.Draft,
		}
	}
	release, err := apiResult(resp.StatusCode(), resp.Body, resp.JSON201,
		resp.JSON400, resp.JSON401, resp.JSON403, resp.JSON404, resp.JSON422, resp.JSON429, resp.JSON500, resp.JSON503)
	if err != nil && resp.HTTPResponse != nil {
		return nil, withRetryAfter(err, resp.HTTPResponse.Header)
	}
	return release, err
}

// ForkRequest creates a person's own project from the release a share offers.
type ForkRequest struct {
	Name    string
	ShareID string
	// ReleaseID is the release the person confirmed, from the share's preview.
	ReleaseID uuid.UUID
	// Variables are values for variables the release declares. Every variable
	// left out is created empty.
	Variables map[string]string
	// IdempotencyKey makes the request safe to retry: a repeat with the same key
	// answers with the project the first request created. Empty sends none.
	IdempotencyKey string
}

// CreateFork creates a project from the release a share offers and queues its
// install. The API answers 409 with code release_changed, and creates nothing,
// when the share has a newer release than the one confirmed. A refusal that
// says when to try again keeps its Retry-After; see RetryAt.
func (c *Client) CreateFork(ctx context.Context, request ForkRequest) (*apiclient.Project, error) {
	body := apiclient.CreateForkJSONRequestBody{
		Name:      strings.TrimSpace(request.Name),
		ShareId:   request.ShareID,
		ReleaseId: request.ReleaseID,
	}
	if len(request.Variables) > 0 {
		variables := request.Variables
		body.Variables = &variables
	}
	params := &apiclient.CreateForkParams{}
	if request.IdempotencyKey != "" {
		params.IdempotencyKey = &request.IdempotencyKey
	}
	resp, err := c.client.CreateForkWithResponse(ctx, params, body)
	if err != nil {
		return nil, err
	}
	project, err := apiResult(resp.StatusCode(), resp.Body, resp.JSON201,
		resp.JSON400, resp.JSON401, resp.JSON403, resp.JSON404, resp.JSON409, resp.JSON422,
		resp.JSON429, resp.JSON451, resp.JSON500, resp.JSON503)
	if err != nil && resp.HTTPResponse != nil {
		return nil, withRetryAfter(err, resp.HTTPResponse.Header)
	}
	return project, err
}
