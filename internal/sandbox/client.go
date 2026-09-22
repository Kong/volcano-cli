// Package sandbox implements the versioned Sandbox public control transport.
package sandbox

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

// Version is pinned to the frozen Sandbox public contract.
const Version = "0.2.0"

const maxResponseBytes = 16 << 20

var identifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)

// ValidID reports whether a path identifier is safe and contract-valid.
func ValidID(value string) bool { return identifier.MatchString(value) }

// Client never retries writes or follows redirects with customer credentials.
type Client struct {
	base, token string
	doer        apiclient.HttpRequestDoer
}

// New creates a project-bound client. HTTP is allowed only for loopback tests.
func New(origin, token, project string, doer apiclient.HttpRequestDoer) (*Client, error) {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, errors.New("sandbox endpoint must be an HTTPS origin")
	}
	loopbackHTTP := u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1")
	if u.Scheme != "https" && !loopbackHTTP {
		return nil, errors.New("sandbox endpoint requires HTTPS outside loopback")
	}
	if !ValidID(project) || token == "" || strings.ContainsAny(token, "\r\n") {
		return nil, errors.New("sandbox requires a valid project and authenticated credential")
	}
	if doer == nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		// A CLI request sequence is short. Fresh HTTP/1 connections avoid the
		// transport's implicit idempotency-key replay on stale pooled sockets.
		transport.DisableKeepAlives = true
		transport.ForceAttemptHTTP2 = false
		doer = &http.Client{Transport: transport, CheckRedirect: refuseRedirect}
	} else if original, ok := doer.(*http.Client); ok {
		copyClient := *original
		copyClient.CheckRedirect = refuseRedirect
		doer = &copyClient
	}
	return &Client{base: strings.TrimRight(origin, "/") + "/v1/projects/" + project, token: token, doer: doer}, nil
}

func refuseRedirect(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }

// Request describes one frozen public operation, with no caller-selected URL.
type Request struct {
	Method, Path, Generation, Key string
	Query                         url.Values
	Body                          any
}

// Response keeps decimal strings, nulls and opaque cursors unchanged.
type Response struct {
	Status int
	Body   json.RawMessage
}

// Error contains only bounded machine diagnostics, never a server-supplied message.
type Error struct {
	Code, OperationID string
	Status            int
}

func (e *Error) Error() string {
	return fmt.Sprintf("sandbox %s (HTTP %d, operation %s)", e.Code, e.Status, e.OperationID)
}

// Do performs a bounded request without replaying unknown outcomes.
func (c *Client) Do(ctx context.Context, input Request) (Response, error) {
	ctx, cancel := context.WithTimeout(ctx, 65*time.Second)
	defer cancel()
	response, err := c.send(ctx, input)
	if err != nil {
		return Response{}, err
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil || len(body) > maxResponseBytes {
		return Response{}, errors.New("sandbox response interrupted or exceeded limit; outcome may be unknown, do not resubmit")
	}
	if response.StatusCode == http.StatusNoContent {
		return Response{Status: response.StatusCode}, nil
	}
	if !json.Valid(body) {
		return Response{}, errors.New("invalid sandbox JSON response; do not resubmit a mutation")
	}
	return Response{Status: response.StatusCode, Body: body}, nil
}

func (c *Client) send(ctx context.Context, input Request) (*http.Response, error) {
	if !strings.HasPrefix(input.Path, "/") || strings.ContainsAny(input.Path, "?#") {
		return nil, errors.New("invalid sandbox route")
	}
	var body io.Reader
	if input.Body != nil {
		data, err := json.Marshal(input.Body)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(data)
	}
	address := c.base + input.Path
	if len(input.Query) != 0 {
		address += "?" + input.Query.Encode()
	}
	request, err := http.NewRequestWithContext(ctx, input.Method, address, body)
	if err != nil {
		return nil, errors.New("could not construct sandbox request")
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("X-Volcano-Sandbox-Version", Version)
	request.Header.Set("Content-Type", "application/json")
	if input.Method != http.MethodGet {
		// In particular, never rewind and replay a submitted command body.
		request.GetBody = nil
	}
	if input.Generation != "" {
		request.Header.Set("X-Sandbox-Generation", input.Generation)
	}
	if input.Key != "" {
		request.Header.Set("Idempotency-Key", input.Key)
	}
	response, err := c.doer.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("sandbox request stopped; outcome may be unknown, do not resubmit: %w", ctx.Err())
		}
		return nil, errors.New("sandbox transport failed; outcome may be unknown, do not resubmit")
	}
	if response.StatusCode < 200 || response.StatusCode > 299 {
		defer func() { _ = response.Body.Close() }()
		var failure struct {
			Code        string `json:"code"`
			OperationID string `json:"operation_id"`
		}
		_ = json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&failure)
		if !ValidID(failure.Code) {
			failure.Code = "request_failed"
		}
		if !ValidID(failure.OperationID) {
			failure.OperationID = "unavailable"
		}
		return nil, &Error{Code: failure.Code, OperationID: failure.OperationID, Status: response.StatusCode}
	}
	if len(response.Header.Values("X-Volcano-Sandbox-Version")) != 1 || response.Header.Get("X-Volcano-Sandbox-Version") != Version {
		_ = response.Body.Close()
		return nil, errors.New("incompatible sandbox response version; do not resubmit a mutation")
	}
	return response, nil
}

// Logs consumes bounded NDJSON frames. Follow EOF is a disconnect, not success;
// the last cursor is returned for an explicit, freshly authorized reconnect.
func (c *Client) Logs(ctx context.Context, input Request, follow bool, emit func(json.RawMessage) error) error {
	response, err := c.send(ctx, input)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 4096), (64<<10)+1)
	cursor := ""
	previous := int64(-1)
	for scanner.Scan() {
		line := bytes.Clone(scanner.Bytes())
		var frame struct {
			Cursor    string    `json:"cursor"`
			Sequence  string    `json:"sequence"`
			Timestamp time.Time `json:"timestamp"`
		}
		if json.Unmarshal(line, &frame) != nil || frame.Cursor == "" || len(frame.Cursor) > 8192 || frame.Timestamp.IsZero() || strings.ContainsAny(frame.Cursor, "\r\n\x1b") {
			return errors.New("invalid sandbox log frame")
		}
		sequence, err := strconv.ParseInt(frame.Sequence, 10, 64)
		if err != nil || sequence <= previous || strconv.FormatInt(sequence, 10) != frame.Sequence {
			return errors.New("invalid sandbox log sequence")
		}
		previous = sequence
		cursor = frame.Cursor
		if err := emit(line); err != nil {
			return err
		}
	}
	if ctx.Err() != nil {
		return fmt.Errorf("logs stopped (last cursor %q): %w", cursor, ctx.Err())
	}
	if scanner.Err() != nil || follow {
		return fmt.Errorf("logs disconnected (last cursor %q); reconnect explicitly with --cursor; commands were not replayed", cursor)
	}
	return nil
}
