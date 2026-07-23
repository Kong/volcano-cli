package api

import (
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"

	"github.com/google/uuid"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

// StorageBucketCreateInput captures fields supported by bucket creation.
type StorageBucketCreateInput struct {
	Name             string
	AllowedMimeTypes []string
	FileSizeLimit    *int64
}

// StorageBucketUpdateInput captures fields supported by bucket update.
type StorageBucketUpdateInput struct {
	AllowedMimeTypes *[]string
	FileSizeLimit    *int64
}

// StoragePolicyCreateInput captures fields supported by policy creation.
type StoragePolicyCreateInput struct {
	Name       string
	Definition string
	Operation  apiclient.CreateStoragePolicyRequestOperation
}

// StorageObjectListOptions controls the object list query.
type StorageObjectListOptions struct {
	Prefix string
	Limit  int
	Cursor string
}

// ListStorageBuckets returns every bucket in the project.
func (c *Client) ListStorageBuckets(ctx context.Context, projectID uuid.UUID) ([]apiclient.StorageBucket, error) {
	resp, err := c.client.ListStorageBucketsWithResponse(ctx, projectID)
	if err != nil {
		return nil, err
	}
	page, err := apiResult(resp.StatusCode(), resp.Body, resp.JSON200)
	if err != nil {
		return nil, err
	}
	if page == nil {
		return nil, nil
	}
	return *page, nil
}

// CreateStorageBucket creates one bucket in the project.
func (c *Client) CreateStorageBucket(ctx context.Context, projectID uuid.UUID, input StorageBucketCreateInput) (*apiclient.StorageBucket, error) {
	body := apiclient.CreateStorageBucketJSONRequestBody{
		Name: strings.TrimSpace(input.Name),
	}
	if len(input.AllowedMimeTypes) > 0 {
		mimes := append([]string(nil), input.AllowedMimeTypes...)
		body.AllowedMimeTypes = &mimes
	}
	if input.FileSizeLimit != nil {
		size := *input.FileSizeLimit
		body.FileSizeLimit = &size
	}

	resp, err := c.client.CreateStorageBucketWithResponse(ctx, projectID, body)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON201)
}

// GetStorageBucket returns one bucket by name.
func (c *Client) GetStorageBucket(ctx context.Context, projectID uuid.UUID, bucketName string) (*apiclient.StorageBucket, error) {
	resp, err := c.client.GetStorageBucketWithResponse(ctx, projectID, bucketName)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200)
}

// UpdateStorageBucket updates one bucket's configuration.
func (c *Client) UpdateStorageBucket(ctx context.Context, projectID uuid.UUID, bucketName string, input StorageBucketUpdateInput) (*apiclient.StorageBucket, error) {
	body := apiclient.UpdateStorageBucketJSONRequestBody{}
	if input.AllowedMimeTypes != nil {
		mimes := append([]string(nil), (*input.AllowedMimeTypes)...)
		body.AllowedMimeTypes = &mimes
	}
	if input.FileSizeLimit != nil {
		size := *input.FileSizeLimit
		body.FileSizeLimit = &size
	}

	resp, err := c.client.UpdateStorageBucketWithResponse(ctx, projectID, bucketName, body)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200)
}

// DeleteStorageBucket removes one bucket from the project.
func (c *Client) DeleteStorageBucket(ctx context.Context, projectID uuid.UUID, bucketName string) error {
	resp, err := c.client.DeleteStorageBucketWithResponse(ctx, projectID, bucketName)
	if err != nil {
		return err
	}
	return apiOK(resp.StatusCode(), resp.Body)
}

// ListStoragePolicies returns every policy attached to a bucket.
func (c *Client) ListStoragePolicies(ctx context.Context, projectID uuid.UUID, bucketName string) ([]apiclient.StoragePolicy, error) {
	resp, err := c.client.ListStoragePoliciesWithResponse(ctx, projectID, bucketName)
	if err != nil {
		return nil, err
	}
	page, err := apiResult(resp.StatusCode(), resp.Body, resp.JSON200)
	if err != nil {
		return nil, err
	}
	if page == nil {
		return nil, nil
	}
	return *page, nil
}

// CreateStoragePolicy attaches a new policy to a bucket.
func (c *Client) CreateStoragePolicy(ctx context.Context, projectID uuid.UUID, bucketName string, input StoragePolicyCreateInput) (*apiclient.StoragePolicy, error) {
	body := apiclient.CreateStoragePolicyJSONRequestBody{
		Name:       strings.TrimSpace(input.Name),
		Definition: input.Definition,
		Operation:  input.Operation,
	}
	resp, err := c.client.CreateStoragePolicyWithResponse(ctx, projectID, bucketName, body)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON201)
}

// DeleteStoragePolicy removes one policy from a bucket.
func (c *Client) DeleteStoragePolicy(ctx context.Context, projectID uuid.UUID, bucketName string, policyID uuid.UUID) error {
	resp, err := c.client.DeleteStoragePolicyWithResponse(ctx, projectID, bucketName, policyID)
	if err != nil {
		return err
	}
	return apiOK(resp.StatusCode(), resp.Body)
}

// ListStorageObjects returns one cursor page of objects in a bucket.
func (c *Client) ListStorageObjects(ctx context.Context, bucketName string, opts StorageObjectListOptions) (*apiclient.StorageListResponse, error) {
	params := &apiclient.ListStorageObjectsParams{}
	if prefix := strings.TrimSpace(opts.Prefix); prefix != "" {
		params.Prefix = &prefix
	}
	if opts.Limit > 0 {
		limit := opts.Limit
		params.Limit = &limit
	}
	if cursor := strings.TrimSpace(opts.Cursor); cursor != "" {
		params.Cursor = &cursor
	}

	resp, err := c.client.ListStorageObjectsWithResponse(ctx, bucketName, params)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200)
}

// UploadStorageObject uploads one object to a bucket via a streaming multipart
// request. The body is assembled lazily through an io.Pipe so the caller's
// reader is consumed in fixed-size chunks rather than buffered in memory.
func (c *Client) UploadStorageObject(ctx context.Context, bucketName, path, contentType string, content io.Reader) (*apiclient.StorageObject, error) {
	pr, pw := io.Pipe()
	writer := multipart.NewWriter(pw)
	multipartContentType := writer.FormDataContentType()

	go func() {
		if err := streamStorageUploadMultipart(writer, path, contentType, content); err != nil {
			_ = pw.CloseWithError(err)
			return
		}
		_ = pw.Close()
	}()

	resp, err := c.client.UploadStorageObjectWithBodyWithResponse(ctx, bucketName, path, &apiclient.UploadStorageObjectParams{}, multipartContentType, pr)
	if err != nil {
		_ = pr.CloseWithError(err)
		return nil, err
	}

	if resp.JSON201 != nil {
		object, err := resp.JSON201.AsStorageObject()
		if err != nil {
			return nil, fmt.Errorf("failed to decode upload response: %w", err)
		}
		return &object, nil
	}
	status := resp.StatusCode()
	if status >= 200 && status < 300 {
		return nil, fmt.Errorf("upload to %q succeeded with status %d but the server returned an empty response body", path, status)
	}
	return nil, apiError(status, resp.Body)
}

// DownloadStorageObject opens a streaming download for one object.
// The caller is responsible for closing the response body.
func (c *Client) DownloadStorageObject(ctx context.Context, bucketName, path string) (*http.Response, error) {
	resp, err := c.client.DownloadStorageObject(ctx, bucketName, path, &apiclient.DownloadStorageObjectParams{})
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return nil, apiError(resp.StatusCode, body)
	}
	return resp, nil
}

// DeleteStorageObject removes one object from a bucket.
func (c *Client) DeleteStorageObject(ctx context.Context, bucketName, path string) error {
	resp, err := c.client.DeleteStorageObjectWithResponse(ctx, bucketName, path, &apiclient.DeleteStorageObjectParams{})
	if err != nil {
		return err
	}
	return apiOK(resp.StatusCode(), resp.Body)
}

// CopyStorageObject copies one object within a bucket.
func (c *Client) CopyStorageObject(ctx context.Context, bucketName, from, to string) (*apiclient.StorageObject, error) {
	body := apiclient.CopyStorageObjectJSONRequestBody{From: from, To: to}
	resp, err := c.client.CopyStorageObjectWithResponse(ctx, bucketName, body)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON201)
}

// MoveStorageObject renames one object within a bucket.
func (c *Client) MoveStorageObject(ctx context.Context, bucketName, from, to string) (*apiclient.StorageObject, error) {
	body := apiclient.MoveStorageObjectJSONRequestBody{From: from, To: to}
	resp, err := c.client.MoveStorageObjectWithResponse(ctx, bucketName, body)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200)
}

// SetStorageObjectVisibility updates the public/private flag for one object.
func (c *Client) SetStorageObjectVisibility(ctx context.Context, bucketName, path string, isPublic bool) (*apiclient.StorageObject, error) {
	body := apiclient.UpdateStorageObjectVisibilityJSONRequestBody{IsPublic: isPublic}
	resp, err := c.client.UpdateStorageObjectVisibilityWithResponse(ctx, bucketName, path, body)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200)
}

// GetStorageStats returns aggregate storage statistics for a project.
func (c *Client) GetStorageStats(ctx context.Context, projectID uuid.UUID) (*apiclient.StorageStats, error) {
	resp, err := c.client.GetStorageStatsWithResponse(ctx, projectID)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200)
}

func streamStorageUploadMultipart(writer *multipart.Writer, path, contentType string, content io.Reader) error {
	filename := path
	if idx := strings.LastIndex(path, "/"); idx >= 0 {
		filename = path[idx+1:]
	}
	if filename == "" {
		filename = "file"
	}

	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name=%q; filename=%q`, "file", filename))
	contentType = strings.TrimSpace(contentType)
	if contentType != "" {
		header.Set("Content-Type", contentType)
	}
	part, err := writer.CreatePart(header)
	if err != nil {
		return fmt.Errorf("failed to create upload part: %w", err)
	}
	if _, err := io.Copy(part, content); err != nil {
		return fmt.Errorf("failed to write upload body: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("failed to finalize multipart body: %w", err)
	}
	return nil
}
