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
	Details    []apiclient.ErrorDetail
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

// Message returns the API's own message for the *Error wrapped in err, or ""
// if err is nil, does not wrap an *Error, or carries no message. Callers that
// act on which refusal a status stands for need the body, not just the code.
func Message(err error) string {
	var apiErr *Error
	if errors.As(err, &apiErr) {
		return apiErr.Message
	}
	return ""
}

func (e *Error) Error() string {
	message := e.Message
	if e.StatusCode == 0 {
		return formatErrorDetails(message, e.Details)
	}
	if message == "" {
		message = fmt.Sprintf("HTTP %d", e.StatusCode)
	} else {
		message = fmt.Sprintf("HTTP %d: %s", e.StatusCode, message)
	}
	return formatErrorDetails(message, e.Details)
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

func apiErrorWithDetails(statusCode int, message string, details *[]apiclient.ErrorDetail) error {
	var copied []apiclient.ErrorDetail
	if details != nil {
		copied = append(copied, (*details)...)
	}
	return &Error{StatusCode: statusCode, Message: strings.TrimSpace(message), Details: copied}
}

func apiResult[T any](statusCode int, body []byte, result *T, generatedErrors ...*apiclient.Error) (*T, error) {
	if result != nil {
		return result, nil
	}
	return nil, apiErrorFromGeneratedErrors(statusCode, body, generatedErrors...)
}

// apiResultWithoutBody is apiResult for a response that carries a secret.
//
// The generated parser fills the success field only for an exact status and a
// JSON content type, so anything else — a 200 where a 201 was expected, a
// stripped Content-Type from something in the path — falls through to the error
// builder, which surfaces the raw body. For these endpoints that body is the
// plaintext credential, so it would land on stderr and in the CI log while the
// user is told the call failed and has no reason to revoke it.
//
// Dropping the body costs a little diagnostic detail on an unexpected status.
// A redaction invariant that depends on the peer behaving is not an invariant.
func apiResultWithoutBody[T any](statusCode int, result *T, generatedErrors ...*apiclient.Error) (*T, error) {
	return apiResult(statusCode, nil, result, generatedErrors...)
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
			return apiErrorWithDetails(statusCode, generatedError.Error, generatedError.Details)
		}
	}
	return apiError(statusCode, body)
}

func apiError(statusCode int, body []byte) error {
	var payload struct {
		Error            string                  `json:"error"`
		ErrorDescription string                  `json:"error_description"`
		Message          string                  `json:"message"`
		Details          []apiclient.ErrorDetail `json:"details"`
	}
	if len(body) > 0 && json.Unmarshal(body, &payload) == nil {
		switch {
		case payload.ErrorDescription != "":
			return apiErrorWithDetails(statusCode, payload.ErrorDescription, &payload.Details)
		case payload.Error != "":
			return apiErrorWithDetails(statusCode, payload.Error, &payload.Details)
		case payload.Message != "":
			return apiErrorWithDetails(statusCode, payload.Message, &payload.Details)
		}
	}
	message := cleanBody(body)
	if message == "" {
		message = http.StatusText(statusCode)
	}
	return apiErrorWithMessage(statusCode, message)
}

func formatErrorDetails(message string, details []apiclient.ErrorDetail) string {
	if len(details) == 0 {
		return message
	}
	var formatted strings.Builder
	formatted.WriteString(message)
	for _, detail := range details {
		formatted.WriteString("\n  - ")
		formatted.WriteString(detail.Path)
		if detail.Constraint != "" {
			formatted.WriteString(" (")
			formatted.WriteString(detail.Constraint)
			formatted.WriteString(")")
		}
		if detail.Message != "" {
			formatted.WriteString(": ")
			formatted.WriteString(detail.Message)
		}
	}
	return formatted.String()
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
