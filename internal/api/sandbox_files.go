package api

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"

	"github.com/google/uuid"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

// SandboxFileArchiveInput is one tar archive, optionally gzip-compressed,
// extracted into Path in a session's workspace.
type SandboxFileArchiveInput struct {
	Project uuid.UUID
	Session uuid.UUID
	Path    string
	Archive []byte
}

// CreateSandboxFileArchive calls the Sandbox CreateSandboxFileArchive
// operation. The form's fields go before the archive, as the API requires, and
// the archive is streamed from input rather than copied into the form.
func (c *Client) CreateSandboxFileArchive(ctx context.Context, input SandboxFileArchiveInput) (*apiclient.SandboxFileArchive, error) {
	var framing bytes.Buffer
	writer := multipart.NewWriter(&framing)
	for _, field := range []struct{ name, value string }{
		{"project_id", input.Project.String()},
		{"session_id", input.Session.String()},
		{"path", input.Path},
	} {
		if err := writer.WriteField(field.name, field.value); err != nil {
			return nil, err
		}
	}
	if _, err := writer.CreateFormFile("archive", "archive.tar.gz"); err != nil {
		return nil, err
	}
	head := framing.Len()
	if err := writer.Close(); err != nil {
		return nil, err
	}
	prefix, suffix := framing.Bytes()[:head], framing.Bytes()[head:]
	body := func() io.Reader {
		return io.MultiReader(bytes.NewReader(prefix), bytes.NewReader(input.Archive), bytes.NewReader(suffix))
	}
	// A MultiReader body has no length of its own; declare it.
	length := func(_ context.Context, req *http.Request) error {
		req.ContentLength = int64(len(prefix) + len(input.Archive) + len(suffix))
		req.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(body()), nil }
		return nil
	}
	response, err := c.client.CreateSandboxFileArchiveWithBodyWithResponse(ctx, writer.FormDataContentType(), body(), length)
	if err != nil {
		return nil, err
	}
	return apiResult(response.StatusCode(), response.Body, response.JSON201, response.JSONDefault)
}

// CreateSandboxFilePart calls the Sandbox CreateSandboxFilePart operation.
func (c *Client) CreateSandboxFilePart(ctx context.Context, body apiclient.SandboxFilePartRequest) (*apiclient.SandboxFilePart, error) {
	response, err := c.client.CreateSandboxFilePartWithResponse(ctx, body)
	if err != nil {
		return nil, err
	}
	return apiResult(response.StatusCode(), response.Body, response.JSON201, response.JSONDefault)
}
