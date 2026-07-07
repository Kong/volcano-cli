package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"

	apicommon "github.com/Kong/volcano-cli/internal/apiclient/common"
)

const logStreamScannerMaxTokenSize = 1024 * 1024

// ProjectLogStream reads project log Server-Sent Events.
type ProjectLogStream struct {
	body    io.ReadCloser
	scanner *bufio.Scanner
}

// ProjectLogStreamEvent is one parsed project log stream event.
type ProjectLogStreamEvent struct {
	ID      string
	Type    string
	Data    string
	Log     *apicommon.LogSearchEvent
	Warning string
}

// Close closes the underlying response body.
func (s *ProjectLogStream) Close() error {
	if s == nil || s.body == nil {
		return nil
	}
	return s.body.Close()
}

// Next returns the next non-comment SSE event.
func (s *ProjectLogStream) Next() (*ProjectLogStreamEvent, error) {
	if s == nil || s.scanner == nil {
		return nil, io.EOF
	}
	for {
		event, err := s.nextRaw()
		if err != nil {
			return nil, err
		}
		if event == nil {
			continue
		}
		return parseProjectLogStreamEvent(event)
	}
}

func (s *ProjectLogStream) nextRaw() (*ProjectLogStreamEvent, error) {
	var event ProjectLogStreamEvent
	var data []string
	hasField := false
	for s.scanner.Scan() {
		line := strings.TrimSuffix(s.scanner.Text(), "\r")
		if line == "" {
			if !hasField {
				continue
			}
			event.Data = strings.Join(data, "\n")
			if event.Type == "" {
				event.Type = "message"
			}
			return &event, nil
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		hasField = true
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "id":
			event.ID = value
		case "event":
			event.Type = value
		case "data":
			data = append(data, value)
		}
	}
	if err := s.scanner.Err(); err != nil {
		return nil, err
	}
	if hasField {
		event.Data = strings.Join(data, "\n")
		if event.Type == "" {
			event.Type = "message"
		}
		return &event, nil
	}
	return nil, io.EOF
}

func parseProjectLogStreamEvent(event *ProjectLogStreamEvent) (*ProjectLogStreamEvent, error) {
	switch event.Type {
	case "log":
		var logEvent apicommon.LogSearchEvent
		if err := json.Unmarshal([]byte(event.Data), &logEvent); err != nil {
			return nil, fmt.Errorf("failed to decode log stream event: %w", err)
		}
		event.Log = &logEvent
	case "warning":
		var warning struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal([]byte(event.Data), &warning); err != nil {
			return nil, fmt.Errorf("failed to decode log stream warning: %w", err)
		}
		event.Warning = strings.TrimSpace(warning.Error)
	}
	return event, nil
}

func (c *Client) streamProjectLogs(ctx context.Context, projectID uuid.UUID, body logSearchRequest, lastEventID string) (*ProjectLogStream, error) {
	normalizeLogSearchRequest(&body)
	body.Cursor = nil
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal log stream request: %w", err)
	}

	streamURL, err := c.projectLogsStreamURL(projectID)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, streamURL, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if lastEventID = strings.TrimSpace(lastEventID); lastEventID != "" {
		req.Header.Set("Last-Event-ID", lastEventID)
	}

	resp, err := c.streamHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer func() {
			_ = resp.Body.Close()
		}()
		respBody, _ := io.ReadAll(resp.Body)
		return nil, apiError(resp.StatusCode, respBody)
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), logStreamScannerMaxTokenSize)
	return &ProjectLogStream{body: resp.Body, scanner: scanner}, nil
}

func (c *Client) projectLogsStreamURL(projectID uuid.UUID) (string, error) {
	base, err := url.Parse(c.baseURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse api base url: %w", err)
	}
	base.Path = strings.TrimRight(base.Path, "/") + "/projects/" + projectID.String() + "/logs/stream"
	base.RawPath = ""
	base.RawQuery = ""
	base.Fragment = ""
	return base.String(), nil
}
