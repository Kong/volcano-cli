package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

func TestApiErrorFallsBackToStatusTextForHTMLBody(t *testing.T) {
	body := []byte("<html><body><h1>502 Bad Gateway</h1></body></html>")
	err := apiError(http.StatusBadGateway, body)
	assert.Equal(t, "HTTP 502: "+http.StatusText(http.StatusBadGateway), err.Error())
	assert.NotContains(t, err.Error(), "<html>")
}

func TestApiErrorFallsBackToStatusTextForOversizedBody(t *testing.T) {
	body := []byte(strings.Repeat("a", maxBodyMessageLen+1))
	err := apiError(http.StatusInternalServerError, body)
	assert.Equal(t, "HTTP 500: "+http.StatusText(http.StatusInternalServerError), err.Error())
}

func TestApiErrorUsesPlainTextBody(t *testing.T) {
	err := apiError(http.StatusBadRequest, []byte("missing required field: name"))
	assert.Equal(t, "HTTP 400: missing required field: name", err.Error())
}

func TestApiErrorPrefersJSONErrorFields(t *testing.T) {
	err := apiError(http.StatusUnauthorized, []byte(`{"error_description":"token expired"}`))
	assert.Equal(t, "HTTP 401: token expired", err.Error())
}

// A caller that acts on which refusal a status stands for needs the body, and
// gets it through the same wrapping Status sees through.
func TestMessageReadsAWrappedError(t *testing.T) {
	err := apiError(http.StatusForbidden, []byte(`{"error":"project access token is read-only"}`))
	assert.Equal(t, "project access token is read-only", Message(fmt.Errorf("failed to do a thing: %w", err)))
	assert.Empty(t, Message(errors.New("boom")))
	assert.Empty(t, Message(nil))
}

// Some proxies send a numeric code. The message is still read, and the code,
// which isn't one of the API's, is dropped.
func TestAPIErrorKeepsTheMessageWithANumericCode(t *testing.T) {
	err := apiError(http.StatusForbidden, []byte(`{"error":"forbidden","code":403}`))
	var apiErr *Error
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, "forbidden", apiErr.Message)
	assert.Empty(t, apiErr.Code)

	err = apiError(http.StatusConflict, []byte(`{"error":"taken","code":"name_taken"}`))
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, "name_taken", apiErr.Code)
}

// The API answers a 404 on every route it serves with its JSON error, so only
// a bare one, such as Go's own, says the route itself is missing.
func TestRouteMissingOnlyForA404TheAPIDidNotSend(t *testing.T) {
	notFound := "Project not found."
	for name, tc := range map[string]struct {
		err  error
		want bool
	}{
		"router 404":       {apiError(http.StatusNotFound, []byte("404 page not found\n")), true},
		"empty 404":        {apiError(http.StatusNotFound, nil), true},
		"API JSON 404":     {apiError(http.StatusNotFound, []byte(`{"error":"Project not found."}`)), false},
		"generated 404":    {apiErrorFromGeneratedErrors(http.StatusNotFound, nil, &apiclient.Error{Error: notFound}), false},
		"plain-text 403":   {apiError(http.StatusForbidden, []byte("forbidden")), false},
		"wrapped router":   {fmt.Errorf("failed: %w", apiError(http.StatusNotFound, []byte("404 page not found"))), true},
		"not an API error": {errors.New("404 page not found"), false},
	} {
		assert.Equal(t, tc.want, RouteMissing(tc.err), name)
	}
}
