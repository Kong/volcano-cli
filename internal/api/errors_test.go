package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
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
