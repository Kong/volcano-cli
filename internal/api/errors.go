package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

// ErrNotFound is returned by resolver lookups when no matching resource exists.
// Callers branch on it with errors.Is(err, api.ErrNotFound).
var ErrNotFound = errors.New("not found")

// Error is a normalized error returned for non-successful API responses.
type Error struct {
	StatusCode int
	Message    string
}

// Status returns the HTTP status code carried by an *Error wrapped in err, or
// 0 if err is nil or does not wrap an *Error.
func Status(err error) int {
	var apiErr *Error
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode
	}
	return 0
}

func (e *Error) Error() string {
	if e.StatusCode == 0 {
		return e.Message
	}
	if e.Message == "" {
		return fmt.Sprintf("HTTP %d", e.StatusCode)
	}
	return fmt.Sprintf("HTTP %d: %s", e.StatusCode, e.Message)
}

func oauthError(statusCode int, resp *apiclient.OAuthErrorResponse) error {
	message := resp.Error
	if resp.ErrorDescription != nil && *resp.ErrorDescription != "" {
		message = *resp.ErrorDescription
	}
	return apiErrorWithMessage(statusCode, message)
}

func apiErrorWithMessage(statusCode int, message string) error {
	return &Error{StatusCode: statusCode, Message: strings.TrimSpace(message)}
}

func apiResult[T any](statusCode int, body []byte, result *T, generatedErrors ...*apiclient.Error) (*T, error) {
	if result != nil {
		return result, nil
	}
	return nil, apiErrorFromGeneratedErrors(statusCode, body, generatedErrors...)
}

func apiOK(statusCode int, body []byte, generatedErrors ...*apiclient.Error) error {
	if statusCode >= 200 && statusCode < 300 {
		return nil
	}
	return apiErrorFromGeneratedErrors(statusCode, body, generatedErrors...)
}

func apiErrorFromGeneratedErrors(statusCode int, body []byte, generatedErrors ...*apiclient.Error) error {
	for _, generatedError := range generatedErrors {
		if generatedError != nil {
			return apiErrorWithMessage(statusCode, generatedError.Error)
		}
	}
	return apiError(statusCode, body)
}

func apiError(statusCode int, body []byte) error {
	var payload struct {
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
		Message          string `json:"message"`
	}
	if len(body) > 0 && json.Unmarshal(body, &payload) == nil {
		switch {
		case payload.ErrorDescription != "":
			return apiErrorWithMessage(statusCode, payload.ErrorDescription)
		case payload.Error != "":
			return apiErrorWithMessage(statusCode, payload.Error)
		case payload.Message != "":
			return apiErrorWithMessage(statusCode, payload.Message)
		}
	}
	message := cleanBody(body)
	if message == "" {
		message = http.StatusText(statusCode)
	}
	return apiErrorWithMessage(statusCode, message)
}

// maxBodyMessageLen caps how much of a non-JSON body we'll surface as an
// error message, so a large but otherwise plain-text response doesn't flood
// the terminal.
const maxBodyMessageLen = 500

// cleanBody returns body as a plain-text error message, or "" if it isn't fit
// to print: markup (HTML/XML error pages served by proxies/load balancers in
// front of the API) or a body too long to be a useful one-line message.
//
// ponytail: markup detection is a leading-'<' heuristic, not a Content-Type
// check (none is threaded through here); upgrade to sniffing the response's
// Content-Type header if a real plain-text body starting with '<' shows up.
func cleanBody(body []byte) string {
	message := strings.TrimSpace(string(body))
	if message == "" || len(message) > maxBodyMessageLen || strings.HasPrefix(message, "<") {
		return ""
	}
	return message
}
